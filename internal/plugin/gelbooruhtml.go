package plugin

import (
	"context"
	"encoding/hex"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gelbooruHTML reads a Gelbooru 0.2 site through its web pages, for sites
// that switched their API off. A list page gives each post's ID and md5 (in
// the thumbnail's name), so posts the library has or deleted are skipped
// without opening them; a post's own page gives the file, tags (with their
// types), source and upload date. Resuming uses the site's id:<N search, like
// the API engine.
type gelbooruHTML struct {
	c       *Client
	fav     favLists
	loginMu sync.Mutex
}

// gelLoggedIn: a page for a signed-in user (its Logout link).
var gelLoggedIn = regexp.MustCompile(`(?i)page=account(&amp;|&)s=logout|>\s*Log ?out\s*<`)

// login signs in through the site's Gelbooru 0.2 login form when the plugin uses it ("gelbooru") and an account
// is set, and again when the session has run out (its cookies user_id and pass_hash are kept).
func (g *gelbooruHTML) login(ctx context.Context) error {
	c := g.c
	if c.m.Auth.Login != "gelbooru" || !c.HasCredentials() {
		return nil
	}
	g.loginMu.Lock()
	defer g.loginMu.Unlock()
	if c.hasSession() {
		return nil
	}
	c.credMu.RLock()
	user, pass := c.creds[c.m.Auth.Fields[0]], c.creds[c.m.Auth.Fields[1]]
	c.credMu.RUnlock()
	u, err := g.page(url.Values{"page": {"account"}, "s": {"login"}, "code": {"00"}})
	if err != nil {
		return err
	}
	body, err := c.PostForm(ctx, u, url.Values{"user": {user}, "pass": {pass}, "submit": {"Log in"}})
	if err != nil {
		return fmt.Errorf("%s: logging in: %w", c.m.Site, err)
	}
	if !c.hasSession() && !gelLoggedIn.Match(body) {
		return fmt.Errorf("%s: %w", c.m.Site, ErrLoginRefused)
	}
	return nil
}

var (
	// <a id="p123" href="...s=view&amp;id=123"><img src=".../thumbnail_<md5>.jpg"
	htmlListItem = regexp.MustCompile(`<a id="p(\d+)"[^>]*>\s*<img[^>]*?src="[^"]*?thumbnail_([0-9a-f]{32})\.`)
	htmlPid      = regexp.MustCompile(`[?&;]pid=(\d+)`)
	// <a class="artist" href="index.php?page=post&amp;s=list&amp;tags=name">
	htmlTag    = regexp.MustCompile(`<a class="([a-z][a-z_-]*)" href="index\.php\?page=post&amp;s=list&amp;tags=([^"&]+)"`)
	htmlSource = regexp.MustCompile(`<input[^>]*\bid="source"[^>]*\bvalue="([^"]*)"`)
	htmlVideo  = regexp.MustCompile(`<source src="([^"]+)" type="video/(mp4|webm)"`)
	htmlImage  = regexp.MustCompile(`<img[^>]*?\bsrc="([^"]+)"[^>]*\bid="image"`)
	htmlOrig   = regexp.MustCompile(`href="([^"]+)"[^>]*>Original<`)
	htmlPosted = regexp.MustCompile(`Posted at ([A-Z][a-z]{2}, \d{1,2} \d{4})`)
	htmlFileMD = regexp.MustCompile(`/([0-9a-f]{32})\.[a-z0-9]+(?:\?|$)`)
)

func (g *gelbooruHTML) page(q url.Values) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(g.c.m.Base, "/") + "/index.php")
	if err != nil {
		return nil, err
	}
	u.RawQuery = q.Encode()
	return u, nil
}

func (g *gelbooruHTML) Search(ctx context.Context, query string, before int64) (Page, error) {
	if err := g.login(ctx); err != nil {
		return Page{}, err
	}
	if user, ok := Favorites(query); ok {
		return g.favorites(ctx, user, before)
	}
	m := g.c.m
	tags := strings.TrimSpace(query)
	if before > 0 {
		tags += " id:<" + strconv.FormatInt(before, 10)
	}
	tags = strings.TrimSpace(tags)
	if tags == "" {
		tags = "all"
	}
	u, err := g.page(url.Values{"page": {"post"}, "s": {"list"}, "tags": {tags}, "pid": {"0"}})
	if err != nil {
		return Page{}, err
	}
	body, err := g.c.API(ctx, u)
	if err != nil {
		return Page{}, err
	}
	text := string(body)
	if !strings.Contains(text, "<html") && !strings.Contains(text, "<HTML") {
		return Page{}, fmt.Errorf("%s: unexpected reply: %s", m.Site, snippet(body))
	}
	page := Page{Total: -1}
	seen := map[int64]bool{}
	for _, mm := range htmlListItem.FindAllStringSubmatch(text, -1) {
		id, _ := strconv.ParseInt(mm[1], 10, 64)
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		p := Post{ID: id, PostURL: m.Post(id), Partial: true, Rating: m.Rating("")}
		if b, err := hex.DecodeString(mm[2]); err == nil && len(b) == 16 {
			copy(p.MD5[:], b)
		}
		page.Posts = append(page.Posts, p)
	}
	// The total, roughly: the last page's offset (pagination links) plus a page.
	last := 0
	for _, mm := range htmlPid.FindAllStringSubmatch(text, -1) {
		last = max(last, atoiSafe(mm[1]))
	}
	if len(page.Posts) > 0 {
		page.Total = last + len(page.Posts)
	} else {
		page.Total = 0
	}
	return page, nil
}

// Lookup reads a post's own page; nil if the site doesn't have it.
func (g *gelbooruHTML) Lookup(ctx context.Context, id int64) (*Post, error) {
	if err := g.login(ctx); err != nil {
		return nil, err
	}
	m := g.c.m
	u, err := g.page(url.Values{"page": {"post"}, "s": {"view"}, "id": {strconv.FormatInt(id, 10)}})
	if err != nil {
		return nil, err
	}
	body, err := g.c.API(ctx, u)
	if err != nil {
		return nil, err
	}
	return parsePostPage(m, id, string(body))
}

// parsePostPage reads a Gelbooru 0.2 post page. A page without a file (a
// removed post shows an empty page or a notice) is "not there": nil.
func parsePostPage(m Manifest, id int64, text string) (*Post, error) {
	p := &Post{ID: id, PostURL: m.Post(id), Rating: m.Rating(""), TagTypes: map[string]string{}}

	// The file: a video's mp4 first (plays everywhere), then its webm; an
	// image's original. Each with fallbacks, tried in turn when one fails: the
	// Original link sometimes answers 404 while the file shown on the page
	// (the embedded image, GIF or video) is there.
	var mp4, webm, orig, shown string
	for _, mm := range htmlVideo.FindAllStringSubmatch(text, -1) {
		switch mm[2] {
		case "mp4":
			mp4 = absURL(m.Base, mm[1])
		case "webm":
			webm = absURL(m.Base, mm[1])
		}
	}
	if mm := htmlOrig.FindStringSubmatch(text); mm != nil {
		orig = absURL(m.Base, mm[1])
	}
	if mm := htmlImage.FindStringSubmatch(text); mm != nil {
		shown = absURL(m.Base, mm[1])
	}
	var urls []string
	add := func(u string) {
		if u != "" && !slices.Contains(urls, u) {
			urls = append(urls, u)
		}
	}
	if mp4 != "" || webm != "" {
		add(mp4)
		add(webm)
		if isVideoLink(orig) {
			add(orig) // a video's Original: one more try
		}
	} else {
		add(orig)
		add(shown)
	}
	if len(urls) > 0 {
		p.FileURL, p.AltFileURLs = urls[0], urls[1:]
	}
	if p.FileURL == "" {
		return nil, nil
	}
	// The md5 is the original's name (for a video, the original upload - its
	// webm, or whichever the Original link names).
	md5From := p.FileURL
	if mm := htmlOrig.FindStringSubmatch(text); mm != nil {
		md5From = html.UnescapeString(mm[1])
	}
	if mm := htmlFileMD.FindStringSubmatch(md5From); mm != nil {
		if b, err := hex.DecodeString(mm[1]); err == nil && len(b) == 16 {
			copy(p.MD5[:], b)
		}
	}

	seen := map[string]bool{}
	for _, mm := range htmlTag.FindAllStringSubmatch(text, -1) {
		name, err := url.QueryUnescape(html.UnescapeString(mm[2]))
		if err != nil || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		p.Tags = append(p.Tags, name)
		typ := strings.TrimPrefix(mm[1], "tag-type-")
		p.TagTypes[name] = typ
	}
	if mm := htmlSource.FindStringSubmatch(text); mm != nil {
		p.Sources = SourceLinks(html.UnescapeString(mm[1]))
	}
	if mm := htmlPosted.FindStringSubmatch(text); mm != nil {
		if t, err := time.Parse("Jan, 2 2006", mm[1]); err == nil {
			p.Posted = t
		}
	}
	return p, nil
}

// absURL resolves a link on the site's page ("//images/..." doubled slashes
// included) against the site's base.
func absURL(base, link string) string {
	link = html.UnescapeString(link)
	b, err := url.Parse(base)
	if err != nil {
		return link
	}
	l, err := url.Parse(link)
	if err != nil {
		return link
	}
	u := b.ResolveReference(l)
	// "https://site//images/x" -> "https://site/images/x" (same file; tidier).
	u.Path = strings.ReplaceAll(u.Path, "//", "/")
	return u.String()
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// isVideoLink: a link to a video file (by its extension).
func isVideoLink(u string) bool {
	u = strings.ToLower(u)
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	return strings.HasSuffix(u, ".mp4") || strings.HasSuffix(u, ".webm") || strings.HasSuffix(u, ".mov") || strings.HasSuffix(u, ".mkv")
}
