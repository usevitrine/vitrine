package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ehentai reads E-Hentai Galleries (e-hentai.org) and ExHentai (exhentai.org):
// galleries - doujinshi, manga, image sets - each downloaded as a comic.
//
// A search (the site's own syntax, e.g. artist:"some artist$" or a plain
// word) lists galleries newest first, resuming with the list's next=<gid>
// cursor; their details (titles, tags with namespaces, page count, upload
// time, the first version's ID) come from the site's JSON API, 25 at a time.
// A gallery's link (https://e-hentai.org/g/<gid>/<token>/) is a "search" for
// that one gallery - its newest version, so a subscription to it brings the
// new version in (the same comic: its key is the first version's ID).
//
// Pages: the gallery's pages (20 or 40 thumbnails each) give every picture's
// page; each picture page gives the picture (from the site's image servers;
// a reload key asks for another server when one fails). When the account's or
// address's image limit is reached the site serves a "509" picture instead:
// the download stops (ErrImageLimit) and carries on later from that page.
//
// Login (auth.login: ehentai): the e-hentai forum's login form (user name and
// password) - or, when that asks for a captcha, the ipb_member_id and
// ipb_pass_hash cookies pasted from a browser. The same cookies work on
// exhentai.org, which shows a blank page ("sad panda") to anyone else.
type ehentai struct {
	c       *Client
	loginMu sync.Mutex
	login   time.Time // when the cookies were last set (zero: not yet)
	// The galleries' details asked lately (Browse: a page whose galleries were all seen lately needs no API call).
	metaMu sync.Mutex
	metas  map[int64]ehMetaAt
	// When the multi-page viewer was last refused (an account without the site's Hath Perk for it): the thumbnail
	// pages list the pages for a day, then it's tried again.
	mpvMu sync.Mutex
	mpvNo time.Time
}

type ehMetaAt struct {
	m  ehMeta
	at time.Time
}

// ehMetaAge is how long a gallery's details serve a foreground request (Browse) without asking again; downloads and
// refreshes always ask (and keep them fresh).
const ehMetaAge = 30 * time.Minute

// Gallery says that a post is a whole gallery: the downloader downloads its
// pages as a comic instead of one file.
type Gallery struct {
	Token    string
	Title    string
	TitleAlt string // the original-language title (e.g. Japanese)
	Category string
	Pages    int
	// Key stays the same across the gallery's versions (the first version's ID).
	Key string
	// For browsing (the Comics > Browse page): its cover picture on the site, rating (0-5) and uploader.
	Cover    string
	Rating   float64
	Uploader string
	// Every tag with its namespace: a name can be in two (female:glasses, male:glasses), which the post's TagTypes
	// (by name) can't tell apart.
	SiteTags []SiteTag
}

// GalleryThumb is one page's thumbnail on a gallery's page: a part (x, y, w, h in pixels) of a picture holding
// several (the site's sprites; a whole picture when x = y = 0 and it holds one).
type GalleryThumb struct {
	Page int    `json:"page"` // 0-based
	URL  string `json:"url"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
}

// BrowseEngine is a gallery engine that can show a gallery's page thumbnails (browsing before downloading).
type BrowseEngine interface {
	// GalleryThumbs reads page p (0-based) of a gallery's thumbnails: the thumbnails and how many such pages it has.
	GalleryThumbs(ctx context.Context, gid int64, token string, p int) ([]GalleryThumb, int, error)
	// Sorts lists the site's other orders than its search's (lists of their own: popular, toplists...).
	Sorts() []string
	// Ranked reads page p (0-based) of one of them: its galleries, and whether there are more.
	Ranked(ctx context.Context, sort string, p int) (Page, bool, error)
	// TagSuggest offers the site's tags starting with the text typed.
	TagSuggest(ctx context.Context, text string) ([]SiteTag, error)
	// Warm gets ready for a search (logs in ahead: the first one after a start waited for it, ~16 s via
	// FlareSolverr).
	Warm(ctx context.Context) error
}

// GalleryEngine is an engine whose posts can be galleries.
type GalleryEngine interface {
	// GalleryPages lists the picture pages of a gallery in order, from page from
	// (0-based) on: each is passed to each (its index, its URL, and the picture's
	// key - the same wherever the site shows that picture, "" if it has none) as
	// soon as the site lists it, so they can be fetched while the rest are listed.
	// An error from each stops the listing (and is returned).
	GalleryPages(ctx context.Context, gid int64, g *Gallery, from int, each func(i int, pageURL, key string) error) error
	// PageImage reads a picture page: the picture's URL, and a key that asks
	// for another server if that one fails (pass it as reload next time).
	PageImage(ctx context.Context, pageURL, reload string) (image, nextReload string, err error)
}

var (
	// ErrImageLimit means the site's image limit is reached for now (it refills over time).
	ErrImageLimit = errors.New("the site's image limit is reached: the download carries on later from where it stopped " +
		"(it refills over time; logged in, the account's own limit applies - see e-hentai.org/home.php)")
	// ErrSadPanda means exhentai.org showed its blank page: the account has no access (yet).
	ErrSadPanda = errors.New("exhentai.org shows a blank page: the account has no ExHentai access - it needs an e-hentai account (logged in) " +
		"that's been around a while; check the login in Settings > Sites")
	// ErrBanned means the site banned this address for a while for loading too many pages too fast.
	ErrBanned = errors.New("the site banned this address for a while for loading too many pages too fast: the download carries on later " +
		"from where it stopped")
	// ErrRegionBlocked means the site refuses the country the request comes from.
	ErrRegionBlocked = errors.New("the site blocks the country Vitrine connects from (in the United Kingdom: the Online Safety Act) - " +
		"it needs to reach the site from elsewhere, e.g. through a VPN")
	// errEHCloudflare: the forum's login refused the request (a Cloudflare check).
	errEHCloudflare = errors.New("the e-hentai forum's login is behind a Cloudflare check that refused Vitrine: set up FlareSolverr " +
		"(Settings > Sites, Cloudflare) - or paste the ipb_member_id and ipb_pass_hash cookies from a browser where you're logged in instead")
	// ErrNoLookup means the engine can't open a post by its ID alone.
	ErrNoLookup = errors.New("this site's posts can't be looked up by ID")
)

var (
	ehGalleryLink = regexp.MustCompile(`/g/(\d+)/([0-9a-f]{10})/`)
	ehGalleryURL  = regexp.MustCompile(`^https?://[^/\s]+/g/(\d+)/([0-9a-f]{10})`) // (a gallery's link)
	ehFound       = regexp.MustCompile(`Found (?:about )?([\d,]+) results?`)
	ehPageLink    = regexp.MustCompile(`https?://[^"'\s]+/s/([0-9a-f]{10})/(\d+)-(\d+)`)
	ehImage       = regexp.MustCompile(`<img id="img" src="([^"]+)"`)
	ehReload      = regexp.MustCompile(`nl\('([^']+)'\)`)
	ehNewer       = regexp.MustCompile(`(?s)<div id="gnd">(.*?)</div>`)
)

func (e *ehentai) base() string { return strings.TrimRight(e.c.m.Base, "/") }

func (e *ehentai) exhentai() bool {
	u, err := url.Parse(e.c.m.Base)
	return err == nil && strings.HasSuffix(u.Hostname(), "exhentai.org")
}

// real: the plugin talks to the real sites (else a test server: everything on its base).
func (e *ehentai) real() bool {
	u, err := url.Parse(e.c.m.Base)
	return err == nil && (strings.HasSuffix(u.Hostname(), "e-hentai.org") || strings.HasSuffix(u.Hostname(), "exhentai.org"))
}

func (e *ehentai) apiURL() string {
	if e.real() && !e.exhentai() {
		return "https://api.e-hentai.org/api.php"
	}
	return e.base() + "/api.php"
}

func (e *ehentai) loginURL() string {
	if e.real() {
		return "https://forums.e-hentai.org/index.php?act=Login&CODE=01"
	}
	return e.base() + "/forums/index.php?act=Login&CODE=01"
}

// cookieHosts are the hosts the login cookies go to.
func (e *ehentai) cookieHosts() []string {
	if !e.real() {
		return []string{e.base() + "/"}
	}
	hosts := []string{"https://e-hentai.org/", "https://api.e-hentai.org/", "https://forums.e-hentai.org/"}
	if e.exhentai() {
		hosts = append(hosts, e.base()+"/")
	}
	return hosts
}

func (e *ehentai) setCookies(cookies ...*http.Cookie) {
	if e.c.http.Jar == nil {
		return
	}
	for _, h := range e.cookieHosts() {
		u, _ := url.Parse(h)
		cs := make([]*http.Cookie, len(cookies))
		for i, ck := range cookies {
			cs[i] = &http.Cookie{Name: ck.Name, Value: ck.Value, Path: "/"}
		}
		e.c.http.Jar.SetCookies(u, cs)
	}
}

// ensureLogin sets the login cookies before a request: pasted ones, or the
// forum's login with the user name and password (once; again after the
// credentials change).
func (e *ehentai) ensureLogin(ctx context.Context) error {
	c := e.c
	e.loginMu.Lock()
	defer e.loginMu.Unlock()
	if j, ok := c.http.Jar.(*sessionJar); ok && !e.login.IsZero() && e.hasCookie(j, "ipb_member_id") {
		return nil
	}
	e.setCookies(&http.Cookie{Name: "nw", Value: "1"}) // (no "offensive content" interstitial before a gallery)
	member, hash := c.credential("ipb_member_id"), c.credential("ipb_pass_hash")
	user, pass := c.credential("username"), c.credential("password")
	switch {
	case member != "" && hash != "":
		e.setCookies(&http.Cookie{Name: "ipb_member_id", Value: member}, &http.Cookie{Name: "ipb_pass_hash", Value: hash})
	case user != "" && pass != "":
		// The forum's login is behind a Cloudflare check: the login page first (passed
		// with FlareSolverr; its clearance then goes with the form).
		if e.real() {
			lp, _ := url.Parse("https://forums.e-hentai.org/index.php?act=Login")
			if _, err := c.WebPage(ctx, lp); err != nil {
				var cf *CloudflareError
				if errors.As(err, &cf) {
					return fmt.Errorf("%s: %w", c.m.Site, errEHCloudflare)
				}
				return fmt.Errorf("%s: opening the login page: %w", c.m.Site, err)
			}
		}
		u, _ := url.Parse(e.loginURL())
		body, err := c.PostForm(ctx, u, url.Values{
			"referer": {"https://forums.e-hentai.org/index.php?"}, "b": {""}, "bt": {""},
			"UserName": {user}, "PassWord": {pass}, "CookieDate": {"1"},
		})
		var se *StatusError
		if errors.As(err, &se) && se.Code == http.StatusForbidden {
			return fmt.Errorf("%s: %w", c.m.Site, errEHCloudflare) // (the check refused the form even so)
		}
		if err != nil {
			return fmt.Errorf("%s: logging in: %w", c.m.Site, err)
		}
		j, _ := c.http.Jar.(*sessionJar)
		forum := "https://forums.e-hentai.org/"
		if !e.real() {
			forum = e.base() + "/"
		}
		if j == nil || !e.hasCookieAt(j, forum, "ipb_member_id") {
			if bytes.Contains(body, []byte("captcha")) || bytes.Contains(body, []byte("recaptcha")) {
				return fmt.Errorf("%s: the e-hentai login asks for a captcha: paste the ipb_member_id and ipb_pass_hash cookies "+
					"from a browser where you're logged in instead (Settings > Sites)", c.m.Site)
			}
			return fmt.Errorf("%s: %w", c.m.Site, ErrLoginRefused)
		}
		// The forum's cookies, for the gallery hosts too.
		fu, _ := url.Parse(forum)
		var keep []*http.Cookie
		for _, ck := range j.Cookies(fu) {
			if ck.Name == "ipb_member_id" || ck.Name == "ipb_pass_hash" {
				keep = append(keep, ck)
			}
		}
		e.setCookies(keep...)
	default:
		if e.exhentai() {
			return ErrNoCredentials
		}
		e.login = time.Now()
		return nil
	}
	e.login = time.Now()
	return nil
}

func (e *ehentai) hasCookie(j *sessionJar, name string) bool {
	return e.hasCookieAt(j, e.base()+"/", name)
}

func (e *ehentai) hasCookieAt(j *sessionJar, host, name string) bool {
	u, _ := url.Parse(host)
	for _, ck := range j.Cookies(u) {
		if ck.Name == name {
			return true
		}
	}
	return false
}

// page fetches one of the site's pages (logged in, if credentials are set).
func (e *ehentai) page(ctx context.Context, rawURL string) ([]byte, error) {
	if err := e.ensureLogin(ctx); err != nil {
		return nil, err
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%s: bad URL", e.c.m.Site)
	}
	body, err := e.c.WebPage(ctx, u)
	var se *StatusError
	if errors.As(err, &se) && se.Code == http.StatusUnavailableForLegalReasons {
		return nil, fmt.Errorf("%s: %w", e.c.m.Site, ErrRegionBlocked)
	}
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(body, []byte("</html>")) {
		// A short plain-text answer: a ban for loading pages too fast (it says for how long),
		// or ExHentai's blank page (no access).
		text := plainText(body)
		low := strings.ToLower(text)
		switch {
		case strings.Contains(low, "banned") || strings.Contains(low, "excessive"):
			return nil, fmt.Errorf("%s: %w (the site: %q)", e.c.m.Site, ErrBanned, text)
		case text != "": // (e.g. "Invalid page.": not the blank page)
			return nil, fmt.Errorf("%s: the site answered %q", e.c.m.Site, text)
		case e.exhentai():
			return nil, fmt.Errorf("%s: %w", e.c.m.Site, ErrSadPanda)
		}
	}
	return body, nil
}

// plainText is a short answer as one line of text (tags out, at most 200 characters).
func plainText(b []byte) string {
	s := strings.Join(strings.Fields(html.UnescapeString(ehTags.ReplaceAllString(string(b), " "))), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

var ehTags = regexp.MustCompile(`<[^>]*>`)

// The site's categories: their f_cats bits (a search's category:<name> terms; "artist_cg" for "artist cg").
var ehCategories = map[string]int{"misc": 1, "doujinshi": 2, "manga": 4, "artist cg": 8, "game cg": 16, "image set": 32,
	"cosplay": 64, "asian porn": 128, "non-h": 256, "western": 512}

// ehSearch is a search as the site's list page takes it: its filters as terms of their own - category:<name> (one
// or more: f_cats, the ones left out), rating:<2-5> (the least stars), pages:<from>-<to> (either end may be left
// out), filter:expunged (expunged galleries too), filter:torrent (only galleries with a torrent) - the rest its
// search (f_search).
func ehSearch(q string) url.Values {
	v := url.Values{}
	keep, rest, adv := 0, []string{}, false
	for _, w := range strings.Fields(q) {
		low := strings.ToLower(w)
		if name, ok := strings.CutPrefix(low, "category:"); ok {
			keep |= ehCategories[strings.ReplaceAll(strings.Trim(name, `"`), "_", " ")]
			continue
		}
		if n, ok := strings.CutPrefix(low, "rating:"); ok {
			if k, err := strconv.Atoi(n); err == nil && k >= 2 && k <= 5 {
				v.Set("f_srdd", n)
				adv = true
			}
			continue
		}
		if r, ok := strings.CutPrefix(low, "pages:"); ok {
			from, to, _ := strings.Cut(r, "-")
			if k, err := strconv.Atoi(from); err == nil && k > 0 {
				v.Set("f_spf", from)
				adv = true
			}
			if k, err := strconv.Atoi(to); err == nil && k > 0 {
				v.Set("f_spt", to)
				adv = true
			}
			continue
		}
		switch low {
		case "filter:expunged":
			v.Set("f_sh", "on")
			adv = true
			continue
		case "filter:torrent":
			v.Set("f_sto", "on")
			adv = true
			continue
		}
		rest = append(rest, w)
	}
	q = strings.Join(rest, " ")
	if keep > 0 {
		v.Set("f_cats", strconv.Itoa(1023&^keep))
	}
	if adv {
		v.Set("advsearch", "1")
	}
	if q != "" {
		v.Set("f_search", q)
	}
	return v
}

// The site's other orders (Comics > Browse's sort): lists of their own, not a search - galleries popular now, and
// the toplists (e-hentai.org's, galleries that ExHentai has too).
var ehToplists = map[string]int{"top-day": 15, "top-month": 13, "top-year": 12, "top-all": 11}

// Warm logs in ahead (a search right after doesn't wait for it).
func (e *ehentai) Warm(ctx context.Context) error {
	return e.ensureLogin(ctx)
}

// Sorts lists the site's other orders, in the order to offer them.
func (e *ehentai) Sorts() []string {
	return []string{"popular", "top-day", "top-month", "top-year", "top-all"}
}

// Ranked reads page p (0-based) of one of the site's other orders: its galleries in that order, and whether there
// are more.
func (e *ehentai) Ranked(ctx context.Context, sort string, p int) (Page, bool, error) {
	var u string
	switch tl := ehToplists[sort]; {
	case sort == "popular":
		if p > 0 {
			return Page{Total: -1}, false, nil // (one page)
		}
		u = e.base() + "/popular"
	case tl > 0:
		u = fmt.Sprintf("https://e-hentai.org/toplist.php?tl=%d", tl)
		if p > 0 {
			u += "&p=" + strconv.Itoa(p)
		}
	default:
		return Page{}, false, fmt.Errorf("%s: no such order: %q", e.c.m.Site, sort)
	}
	body, err := e.page(ctx, u)
	if err != nil {
		return Page{}, false, err
	}
	var list [][2]string
	seen := map[string]bool{}
	for _, m := range ehGalleryLink.FindAllSubmatch(body, -1) {
		if gid := string(m[1]); !seen[gid] {
			seen[gid] = true
			list = append(list, [2]string{gid, string(m[2])})
		}
	}
	posts, err := e.details(ctx, list)
	if err != nil {
		return Page{}, false, err
	}
	more := sort != "popular" && len(list) > 0 && bytes.Contains(body, []byte("p="+strconv.Itoa(p+1)))
	return Page{Posts: posts, Total: -1}, more, nil
}

// Search lists galleries, newest first: a search on the site, or one gallery by its link.
func (e *ehentai) Search(ctx context.Context, query string, before int64) (Page, error) {
	q := strings.TrimSpace(query)
	if m := ehGalleryURL.FindStringSubmatch(q); m != nil {
		if before > 0 {
			return Page{Total: 1}, nil // (one gallery: no next page)
		}
		gid, _ := strconv.ParseInt(m[1], 10, 64)
		gid, token, err := e.newest(ctx, gid, m[2])
		if err != nil {
			return Page{}, err
		}
		posts, err := e.details(ctx, [][2]string{{strconv.FormatInt(gid, 10), token}})
		if err != nil {
			return Page{}, err
		}
		return Page{Posts: posts, Total: 1}, nil
	}
	v := ehSearch(q)
	if before > 0 {
		v.Set("next", strconv.FormatInt(before, 10))
	}
	u := e.base() + "/"
	if len(v) > 0 {
		u += "?" + v.Encode()
	}
	body, err := e.page(ctx, u)
	if err != nil {
		return Page{}, err
	}
	total := -1
	if m := ehFound.FindSubmatch(body); m != nil {
		total, _ = strconv.Atoi(strings.ReplaceAll(string(m[1]), ",", ""))
	}
	var list [][2]string
	seen := map[string]bool{}
	for _, m := range ehGalleryLink.FindAllSubmatch(body, -1) {
		gid := string(m[1])
		if seen[gid] {
			continue
		}
		seen[gid] = true
		if n, _ := strconv.ParseInt(gid, 10, 64); before > 0 && n >= before {
			continue
		}
		list = append(list, [2]string{gid, string(m[2])})
	}
	posts, err := e.details(ctx, list)
	if err != nil {
		return Page{}, err
	}
	return Page{Posts: posts, Total: total}, nil
}

// newest follows a gallery to its newest version (the gallery page lists newer ones).
func (e *ehentai) newest(ctx context.Context, gid int64, token string) (int64, string, error) {
	body, err := e.page(ctx, fmt.Sprintf("%s/g/%d/%s/", e.base(), gid, token))
	if err != nil {
		return 0, "", err
	}
	if m := ehNewer.FindSubmatch(body); m != nil {
		links := ehGalleryLink.FindAllSubmatch(m[1], -1)
		if len(links) > 0 {
			last := links[len(links)-1]
			if n, err := strconv.ParseInt(string(last[1]), 10, 64); err == nil && n > gid {
				return n, string(last[2]), nil
			}
		}
	}
	return gid, token, nil
}

type ehMeta struct {
	GID      int64    `json:"gid"`
	Token    string   `json:"token"`
	Title    string   `json:"title"`
	TitleJpn string   `json:"title_jpn"`
	Category string   `json:"category"`
	Posted   string   `json:"posted"`
	Files    string   `json:"filecount"`
	Expunged bool     `json:"expunged"`
	Tags     []string `json:"tags"`
	FirstGID string   `json:"first_gid"`
	Thumb    string   `json:"thumb"`
	Rating   string   `json:"rating"`
	Uploader string   `json:"uploader"`
	Error    string   `json:"error"`
}

// details asks the API about galleries (25 at a time), in the given order.
func (e *ehentai) details(ctx context.Context, list [][2]string) ([]Post, error) {
	fg, _ := ctx.Value(foregroundKey{}).(bool)
	got := map[int64]ehMeta{}
	var missing [][2]string
	e.metaMu.Lock()
	for _, g := range list {
		n, _ := strconv.ParseInt(g[0], 10, 64)
		if c, ok := e.metas[n]; fg && ok && c.m.Token == g[1] && time.Since(c.at) < ehMetaAge {
			got[n] = c.m
		} else {
			missing = append(missing, g)
		}
	}
	e.metaMu.Unlock()
	for i := 0; i < len(missing); i += 25 {
		chunk := missing[i:min(i+25, len(missing))]
		ids := make([][]any, len(chunk))
		for k, g := range chunk {
			n, _ := strconv.ParseInt(g[0], 10, 64)
			ids[k] = []any{n, g[1]}
		}
		req, _ := json.Marshal(map[string]any{"method": "gdata", "gidlist": ids, "namespace": 1})
		u, _ := url.Parse(e.apiURL())
		if err := e.ensureLogin(ctx); err != nil {
			return nil, err
		}
		body, err := e.c.PostJSON(ctx, u, req)
		if err != nil {
			return nil, err
		}
		var res struct {
			Meta []ehMeta `json:"gmetadata"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, fmt.Errorf("%s: the API's answer doesn't read: %w", e.c.m.Site, err)
		}
		e.metaMu.Lock()
		if e.metas == nil || len(e.metas) > 20000 {
			e.metas = map[int64]ehMetaAt{} // (bounded: a fresh start now and then)
		}
		for _, m := range res.Meta {
			if m.Error != "" || m.GID == 0 {
				continue
			}
			got[m.GID] = m
			e.metas[m.GID] = ehMetaAt{m, time.Now()}
		}
		e.metaMu.Unlock()
	}
	out := make([]Post, 0, len(list))
	for _, g := range list { // (in the page's order)
		n, _ := strconv.ParseInt(g[0], 10, 64)
		if m, ok := got[n]; ok {
			out = append(out, e.toPost(m))
		}
	}
	return out, nil
}

func (e *ehentai) toPost(m ehMeta) Post {
	pages, _ := strconv.Atoi(m.Files)
	key := m.FirstGID
	if key == "" {
		key = strconv.FormatInt(m.GID, 10)
	}
	p := Post{
		ID:      m.GID,
		PostURL: fmt.Sprintf("%s/g/%d/%s/", e.base(), m.GID, m.Token),
		Gallery: &Gallery{
			Token: m.Token, Title: html.UnescapeString(m.Title), TitleAlt: html.UnescapeString(m.TitleJpn),
			Category: m.Category, Pages: pages, Key: key, Cover: m.Thumb, Uploader: html.UnescapeString(m.Uploader),
		},
	}
	p.Gallery.Rating, _ = strconv.ParseFloat(m.Rating, 64)
	if s, err := strconv.ParseInt(m.Posted, 10, 64); err == nil && s > 0 {
		p.Posted = time.Unix(s, 0)
	}
	// "female:big breasts" -> female:big_breasts (Vitrine's tags have no spaces).
	types := map[string]string{}
	for _, t := range m.Tags {
		ns, name, ok := strings.Cut(t, ":")
		if !ok {
			ns, name = "", t
		}
		name = strings.ReplaceAll(strings.TrimSpace(html.UnescapeString(name)), " ", "_")
		if name == "" {
			continue
		}
		p.Tags = append(p.Tags, name)
		p.Gallery.SiteTags = append(p.Gallery.SiteTags, SiteTag{NS: ns, Name: name})
		if ns != "" {
			types[name] = ns
		}
	}
	p.TagTypes = types
	return p
}

// Lookup can't open a gallery by its ID alone (the site needs its token too).
func (e *ehentai) Lookup(ctx context.Context, id int64) (*Post, error) {
	return nil, ErrNoLookup
}

// GalleryPages lists a gallery's picture pages: all at once from the
// multi-page viewer (one request - for an account with the site's Hath Perk for
// it), else from its own pages of thumbnails (20 or 40 to a page, the account's
// setting). From a later page: the first thumbnail page says how many it holds,
// then the listing jumps to the one with that page. A page's key is the one in
// its link (the start of the original picture's SHA-1, the same in every
// gallery and on both sites).
func (e *ehentai) GalleryPages(ctx context.Context, gid int64, g *Gallery, from int, each func(i int, pageURL, key string) error) error {
	if keys, err := e.mpv(ctx, gid, g.Token); err != nil {
		return err
	} else if keys != nil {
		for i, k := range keys {
			if i >= from {
				if err := each(i, fmt.Sprintf("%s/s/%s/%d-%d", e.base(), k, gid, i+1), k); err != nil {
					return err
				}
			}
		}
		return nil
	}
	list := func(p int) ([]pageLink, error) {
		u := fmt.Sprintf("%s/g/%d/%s/", e.base(), gid, g.Token)
		if p > 0 {
			u += "?p=" + strconv.Itoa(p)
		}
		body, err := e.page(ctx, u)
		if err != nil {
			return nil, err
		}
		// Only the thumbnails (comments often link pages too - a chapter index), each
		// placed by the page number in its link.
		body = thumbnailArea(body)
		var out []pageLink
		seen := map[int]bool{}
		for _, m := range ehPageLink.FindAllSubmatch(body, -1) {
			n, err := strconv.Atoi(string(m[3]))
			if string(m[2]) != strconv.FormatInt(gid, 10) || err != nil || n < 1 || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, pageLink{n - 1, fmt.Sprintf("%s/s/%s/%s-%s", e.base(), m[1], m[2], m[3]), string(m[1])})
		}
		return out, nil
	}
	first, err := list(0)
	if err != nil {
		return err
	}
	if len(first) == 0 {
		return fmt.Errorf("%s: the gallery shows no pages", e.c.m.Site)
	}
	per := len(first) // (a gallery of one thumbnail page: all of it)
	p, links := 0, first
	if from >= per {
		p = from / per
		if links, err = list(p); err != nil {
			return err
		}
	}
	for ; ; p++ {
		if links == nil {
			if links, err = list(p); err != nil {
				return err
			}
		}
		if len(links) == 0 {
			return nil // past the last page
		}
		last := 0
		for _, l := range links {
			if l.i >= from {
				if err := each(l.i, l.url, l.key); err != nil {
					return err
				}
			}
			last = max(last, l.i)
		}
		if len(links) < per || (g.Pages > 0 && last+1 >= g.Pages) || p >= 2000 {
			return nil
		}
		links = nil
	}
}

var (
	// A thumbnail: its picture page's link, then a box with its size and a part of a picture (the site's sprites) -
	// logged in, the box inside another (with the page number under it).
	ehThumb = regexp.MustCompile(`(?s)<a href="[^"]*/s/[0-9a-f]{10}/\d+-(\d+)"[^>]*>\s*(?:<div>\s*)?<div[^>]*style="width:(\d+)px;\s*height:(\d+)px;[^"]*?url\(([^)]+)\)\s*(-?\d+)(?:px)?\s*(-?\d+)(?:px)?`)
	// The pager: the highest page number linked.
	ehPagerNum = regexp.MustCompile(`[?&]p=(\d+)"`)
)

// GalleryThumbs reads one page of a gallery's thumbnails (20 or 40, the account's setting).
func (e *ehentai) GalleryThumbs(ctx context.Context, gid int64, token string, p int) ([]GalleryThumb, int, error) {
	u := fmt.Sprintf("%s/g/%d/%s/", e.base(), gid, token)
	if p > 0 {
		u += "?p=" + strconv.Itoa(p)
	}
	body, err := e.page(ctx, u)
	if err != nil {
		return nil, 0, err
	}
	return parseThumbs(e.c.m.Site, body)
}

// parseThumbs reads a gallery page's thumbnails and how many thumbnail pages the gallery has.
func parseThumbs(site string, body []byte) ([]GalleryThumb, int, error) {
	pages := 1
	for _, m := range ehPagerNum.FindAllSubmatch(body, -1) {
		if n, err := strconv.Atoi(string(m[1])); err == nil {
			pages = max(pages, n+1)
		}
	}
	var out []GalleryThumb
	for _, m := range ehThumb.FindAllSubmatch(thumbnailArea(body), -1) {
		n, _ := strconv.Atoi(string(m[1]))
		w, _ := strconv.Atoi(string(m[2]))
		h, _ := strconv.Atoi(string(m[3]))
		x, _ := strconv.Atoi(string(m[5]))
		y, _ := strconv.Atoi(string(m[6]))
		if n < 1 || w <= 0 || h <= 0 {
			continue
		}
		out = append(out, GalleryThumb{Page: n - 1, URL: html.UnescapeString(strings.Trim(string(m[4]), `'"`)), X: -x, Y: -y, W: w, H: h})
	}
	if len(out) == 0 {
		return nil, pages, fmt.Errorf("%s: the gallery shows no thumbnails", site)
	}
	return out, pages, nil
}

// pageLink is a picture page of a gallery: its index (0-based) and URL.
type pageLink struct {
	i        int
	url, key string
}

var ehMPVList = regexp.MustCompile(`var imagelist = ([.*?]);`)

// mpv reads a gallery's page keys, in order, from the multi-page viewer: nil (no error) when the account can't use
// it (it's remembered for a day) - the thumbnail pages list them then.
func (e *ehentai) mpv(ctx context.Context, gid int64, token string) ([]string, error) {
	e.mpvMu.Lock()
	no := time.Since(e.mpvNo) < 24*time.Hour
	e.mpvMu.Unlock()
	if no || !e.signedIn() {
		return nil, nil
	}
	body, err := e.page(ctx, fmt.Sprintf("%s/mpv/%d/%s/", e.base(), gid, token))
	if err != nil && (ctx.Err() != nil || errors.Is(err, ErrBanned) || errors.Is(err, ErrRegionBlocked) || errors.Is(err, ErrSadPanda) ||
		errors.Is(err, ErrLoginRefused) || errors.Is(err, ErrNoCredentials)) {
		return nil, err
	}
	var list []struct {
		K string `json:"k"`
	}
	if err == nil {
		if m := ehMPVList.FindSubmatch(body); m == nil || json.Unmarshal(m[1], &list) != nil {
			list = nil
		}
	}
	keys := make([]string, 0, len(list))
	for _, x := range list {
		if !ehKey.MatchString(x.K) {
			keys = nil
			break
		}
		keys = append(keys, x.K)
	}
	if len(keys) == 0 { // ("eeenope": no Hath Perk for it)
		e.mpvMu.Lock()
		e.mpvNo = time.Now()
		e.mpvMu.Unlock()
		e.c.logInfo("the multi-page viewer isn't open to this account: pages are listed from the thumbnail pages", "site", e.c.m.ID)
		return nil, nil
	}
	return keys, nil
}

var ehKey = regexp.MustCompile(`^[0-9a-f]{10}$`)

// thumbnailArea is the part of a gallery page that holds its thumbnails (the
// whole page if it isn't found).
func thumbnailArea(body []byte) []byte {
	start := bytes.Index(body, []byte(`id="gdt"`))
	if start < 0 {
		return body
	}
	rest := body[start:]
	for _, end := range []string{`id="cdiv"`, `class="gtb"`} {
		if i := bytes.Index(rest, []byte(end)); i > 0 {
			rest = rest[:i]
		}
	}
	return rest
}

// PageImage reads a picture page.
func (e *ehentai) PageImage(ctx context.Context, pageURL, reload string) (string, string, error) {
	u := pageURL
	if reload != "" {
		u += "?nl=" + url.QueryEscape(reload)
	}
	body, err := e.page(ctx, u)
	if err != nil {
		return "", "", err
	}
	m := ehImage.FindSubmatch(body)
	if m == nil {
		return "", "", fmt.Errorf("%s: the picture's page has no picture", e.c.m.Site)
	}
	img := html.UnescapeString(string(m[1]))
	if strings.HasSuffix(img, "/509.gif") || strings.Contains(img, "/509s.gif") {
		return "", "", fmt.Errorf("%s: %w", e.c.m.Site, ErrImageLimit)
	}
	next := ""
	if r := ehReload.FindSubmatch(body); r != nil {
		next = string(r[1])
	}
	return img, next, nil
}

// SiteTag is one of a site's tags with its namespace (the site's own) - a gallery's (Browse), or offered while typing.
type SiteTag struct {
	NS   string
	Name string
}

// TagSuggest asks the site for tags starting with the text typed ("ns:text" narrows it to a namespace), the best
// first: names starting with the text, then shorter ones.
func (e *ehentai) TagSuggest(ctx context.Context, text string) ([]SiteTag, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "_", " "))
	if len([]rune(text)) < 2 {
		return nil, nil
	}
	req, _ := json.Marshal(map[string]any{"method": "tagsuggest", "text": text})
	u, _ := url.Parse(e.apiURL())
	if err := e.ensureLogin(ctx); err != nil {
		return nil, err
	}
	body, err := e.c.PostJSON(ctx, u, req)
	if err != nil {
		return nil, err
	}
	return parseTagSuggest(text, body)
}

// parseTagSuggest reads the API's tag suggestions: {"tags": {"<id>": {"ns": "female", "tn": "blue sky"}}} (an empty
// list when none).
func parseTagSuggest(text string, body []byte) ([]SiteTag, error) {
	var res struct {
		Tags json.RawMessage `json:"tags"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("the tag suggestions don't read: %w", err)
	}
	var tags map[string]struct {
		NS string `json:"ns"`
		TN string `json:"tn"`
	}
	if json.Unmarshal(res.Tags, &tags) != nil {
		return nil, nil // ([]: none)
	}
	_, word, ok := strings.Cut(text, ":")
	if !ok {
		word = text
	}
	word = strings.ToLower(word)
	out := make([]SiteTag, 0, len(tags))
	for _, t := range tags {
		if name := strings.ReplaceAll(strings.TrimSpace(html.UnescapeString(t.TN)), " ", "_"); name != "" {
			out = append(out, SiteTag{NS: t.NS, Name: name})
		}
	}
	w := strings.ReplaceAll(word, " ", "_")
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.HasPrefix(out[i].Name, w), strings.HasPrefix(out[j].Name, w)
		if a != b {
			return a
		}
		if len(out[i].Name) != len(out[j].Name) {
			return len(out[i].Name) < len(out[j].Name)
		}
		return out[i].NS+out[i].Name < out[j].NS+out[j].Name
	})
	return out, nil
}

// signedIn tells whether the site has an account to log in with (a user name, or pasted cookies).
func (e *ehentai) signedIn() bool {
	return e.c.credential("username") != "" || e.c.credential("ipb_member_id") != ""
}
