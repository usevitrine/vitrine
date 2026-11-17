package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// redgifs reads RedGIFs' API (api.redgifs.com/v2), with the temporary token
// the site hands to anyone (/v2/auth/temporary, kept for a day, fetched again
// when refused). A search is nothing (the newest of all), tags (all of them: the site's tags=a,b; no
// leaving out, no OR), "@name" (that user's clips, the user as the creator), or
// a clip's link (that clip). Tag searches give videos unless "type:images";
// a user's gives both (or what type: says). Newest first.
//
// Clips have word IDs ("happyfluffycat"), Vitrine's posts numbers in upload
// order: a post's ID is the upload time in seconds << 20 plus 20 bits of a hash
// of the clip's ID (same clip, same number). The site pages by page number
// (80 a page; a tag search ends after 10,000 clips), so resuming below a post
// finds its page: remembered from the page before, else by halving.
type redgifs struct {
	c *Client

	mu      sync.Mutex
	tokenAt time.Time
	next    map[string]int // query + "\x00" + the last ID handed out -> the page after it
}

const redgifsPer = 80

type redgifsGif struct {
	ID         string   `json:"id"`
	CreateDate int64    `json:"createDate"`
	Type       int      `json:"type"` // 1 video, 2 image
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Tags       []string `json:"tags"`
	UserName   string   `json:"userName"`
	URLs       struct {
		HD string `json:"hd"`
		SD string `json:"sd"`
	} `json:"urls"`
}

type redgifsQuery struct {
	clip, user string
	tags       []string
	typ        string // "", "g" or "i"
}

var (
	redgifsClipPath = regexp.MustCompile(`^/(?:watch|ifr|i)/([A-Za-z]+)`)
	redgifsUserPath = regexp.MustCompile(`^/users/([^/?#]+)`)
)

// redgifsID is a clip's post ID: its upload time, then 20 bits of its ID's hash.
func redgifsID(id string, created int64) int64 {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(id)))
	return created<<20 | int64(h.Sum32()&0xfffff)
}

func (r *redgifs) parse(q string) (redgifsQuery, error) {
	var out redgifsQuery
	q = strings.TrimSpace(q)
	if strings.HasPrefix(strings.ToLower(q), "https://") || strings.HasPrefix(strings.ToLower(q), "http://") {
		u, err := url.Parse(q)
		if err == nil {
			if mm := redgifsClipPath.FindStringSubmatch(u.Path); mm != nil {
				out.clip = strings.ToLower(mm[1])
				return out, nil
			}
			if mm := redgifsUserPath.FindStringSubmatch(u.Path); mm != nil {
				out.user = mm[1]
				return out, nil
			}
		}
		return out, fmt.Errorf("%s: that link isn't a clip or a user: open one and copy its address", r.c.m.Site)
	}
	if _, ok := Favorites(q); ok {
		return out, fmt.Errorf("%s: favorites need a sign-in, which Vitrine doesn't do there", r.c.m.Site)
	}
	for _, w := range strings.Fields(q) {
		lw := strings.ToLower(w)
		switch {
		case lw == "type:images" || lw == "type:image":
			out.typ = "i"
		case lw == "type:videos" || lw == "type:video":
			out.typ = "g"
		case strings.HasPrefix(w, "-") || strings.HasPrefix(w, "~"):
			return out, fmt.Errorf("%s searches for clips with all the tags: it can't leave a tag out or search for either", r.c.m.Site)
		case strings.HasPrefix(w, "@") && len(w) > 1:
			if out.user != "" {
				return out, fmt.Errorf("%s: one @user at a time", r.c.m.Site)
			}
			out.user = w[1:]
		default:
			out.tags = append(out.tags, strings.ReplaceAll(w, "_", " "))
		}
	}
	if out.user != "" && len(out.tags) > 0 {
		return out, fmt.Errorf("%s: a user's clips can't be searched by tag - @user alone, or tags alone", r.c.m.Site)
	}
	if out.user == "" && out.typ == "" {
		out.typ = "g" // (the site's tag search gives one kind at a time)
	}
	return out, nil
}

// get asks the API, with a fresh temporary token when there's none, it's a day old, or the site refused it.
func (r *redgifs) get(ctx context.Context, path string, v url.Values) ([]byte, error) {
	u, err := url.Parse(strings.TrimRight(r.c.m.Base, "/") + path)
	if err != nil {
		return nil, err
	}
	u.RawQuery = v.Encode()
	for try := 0; ; try++ {
		if err := r.token(ctx, try > 0); err != nil {
			return nil, err
		}
		body, err := r.c.API(ctx, u)
		var se *StatusError
		if errors.As(err, &se) && se.Code == 401 && try == 0 {
			continue
		}
		return body, err
	}
}

func (r *redgifs) token(ctx context.Context, again bool) error {
	r.mu.Lock()
	fresh := !again && !r.tokenAt.IsZero() && time.Since(r.tokenAt) < 20*time.Hour
	r.mu.Unlock()
	if fresh {
		return nil
	}
	u, err := url.Parse(strings.TrimRight(r.c.m.Base, "/") + "/v2/auth/temporary")
	if err != nil {
		return err
	}
	r.c.SetToken("")
	body, err := r.c.API(ctx, u)
	if err != nil {
		return fmt.Errorf("%s: no access token: %w", r.c.m.Site, err)
	}
	var t struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(body, &t) != nil || t.Token == "" {
		return fmt.Errorf("%s: no access token (%s)", r.c.m.Site, snippet(body))
	}
	r.c.SetToken(t.Token)
	r.mu.Lock()
	r.tokenAt = time.Now()
	r.mu.Unlock()
	return nil
}

// page asks for one page of a user's or a tag search's clips.
func (r *redgifs) page(ctx context.Context, q redgifsQuery, n int) (gifs []redgifsGif, pages, total int, err error) {
	v := url.Values{"order": {"new"}, "count": {strconv.Itoa(redgifsPer)}, "page": {strconv.Itoa(n)}}
	path := "/v2/users/" + url.PathEscape(strings.ToLower(q.user)) + "/search"
	if q.user == "" {
		path = "/v2/gifs/search"
		v.Set("order", "latest")
		if len(q.tags) > 0 { // (none: the newest of all, as an empty search is on every site)
			v.Set("tags", strings.Join(q.tags, ","))
		}
	}
	if q.typ != "" {
		v.Set("type", q.typ)
	}
	body, err := r.get(ctx, path, v)
	if err != nil {
		return nil, 0, 0, r.siteError(err, q)
	}
	var res struct {
		Gifs  []redgifsGif `json:"gifs"`
		Pages int          `json:"pages"`
		Total int          `json:"total"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, 0, 0, fmt.Errorf("%s: unexpected reply: %s", r.c.m.Site, snippet(body))
	}
	return res.Gifs, res.Pages, res.Total, nil
}

func (r *redgifs) siteError(err error, q redgifsQuery) error {
	var se *StatusError
	if errors.As(err, &se) && se.Code == 404 && q.user != "" {
		return fmt.Errorf("%s has no user %q", r.c.m.Site, q.user)
	}
	return err
}

func (r *redgifs) Search(ctx context.Context, query string, before int64) (Page, error) {
	q, err := r.parse(query)
	if err != nil {
		return Page{}, err
	}
	if q.clip != "" {
		if before > 0 {
			return Page{Total: 1}, nil
		}
		p, err := r.clip(ctx, q.clip)
		if err != nil || p == nil {
			return Page{Total: 0}, err
		}
		return Page{Posts: []Post{*p}, Total: 1}, nil
	}
	key := strings.ToLower(query) + "\x00"
	n := 1
	cache := map[int][]redgifsGif{}
	pages, total := 0, -1
	if before > 0 {
		r.mu.Lock()
		at, ok := r.next[key+strconv.FormatInt(before, 10)]
		r.mu.Unlock()
		if ok {
			n = at
		} else if n, err = r.find(ctx, q, before, cache); err != nil {
			return Page{}, err
		}
	}
	page := Page{Total: -1}
	// The site's tag search is loose: a tag it doesn't know is left out of the search without a word (a search
	// for a made-up tag gives every clip), and a known one sometimes matches clips without it. Only clips with every
	// tag searched for are kept - so a page can come back with none, and more pages are looked at before the end.
	dropped, sawAny := 0, false
	tries := 10
	if len(q.tags) > 0 {
		tries = 25
	}
	for try := 0; try < tries; try++ {
		gifs, ok := cache[n]
		if !ok {
			var p, t int
			if gifs, p, t, err = r.page(ctx, q, n); err != nil {
				// (past a tag search's last page the site answers 500: the end)
				var se *StatusError
				if before > 0 && errors.As(err, &se) && se.Code >= 500 {
					return page, nil
				}
				return Page{}, err
			}
			pages, total = p, t
		}
		if before == 0 && try == 0 {
			page.Total = total
		}
		types := map[string]string{}
		for _, g := range gifs {
			sawAny = true
			if !redgifsHasAll(g, q.tags) {
				dropped++
				continue
			}
			if p, ok := r.post(g, types); ok && (before == 0 || p.ID < before) {
				page.Posts = append(page.Posts, p)
			}
		}
		for i := range page.Posts {
			page.Posts[i].TagTypes = types
		}
		if len(page.Posts) > 0 || len(gifs) == 0 || (pages > 0 && n >= pages) {
			break
		}
		n++ // (a page of clips already handed out: new uploads pushed them down)
	}
	if before == 0 && dropped > 0 {
		page.Total = -1 // (the site's count is of its loose search)
	}
	if before == 0 && len(page.Posts) == 0 && sawAny && len(q.tags) > 0 {
		return Page{}, fmt.Errorf("%s has no clips with all of %s: check the tags' spelling (the site ignores tags it doesn't know)",
			r.c.m.Site, strings.Join(q.tags, ", "))
	}
	if len(page.Posts) > 0 {
		r.mu.Lock()
		if r.next == nil || len(r.next) > 1000 {
			r.next = map[string]int{}
		}
		r.next[key+strconv.FormatInt(page.Posts[len(page.Posts)-1].ID, 10)] = n + 1
		r.mu.Unlock()
	}
	return page, nil
}

// redgifsHasAll reports whether a clip has every tag (in any case, "big_smile" = "Big Smile").
func redgifsHasAll(g redgifsGif, tags []string) bool {
	norm := func(t string) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(CleanTag(t), "_", " ")), " "))
	}
	have := make(map[string]bool, len(g.Tags))
	for _, t := range g.Tags {
		have[norm(t)] = true
	}
	for _, t := range tags {
		if !have[norm(t)] {
			return false
		}
	}
	return true
}

// find is the first page with a clip below before: by halving the pages (newest first, so a page's last clip
// says whether the ones below before begin there or later).
func (r *redgifs) find(ctx context.Context, q redgifsQuery, before int64, cache map[int][]redgifsGif) (int, error) {
	last := func(n int) (int64, int, error) {
		gifs, pages, _, err := r.page(ctx, q, n)
		if err != nil {
			return 0, 0, err
		}
		cache[n] = gifs
		if len(gifs) == 0 {
			return 0, pages, nil // (empty: past the end)
		}
		g := gifs[len(gifs)-1]
		return redgifsID(g.ID, g.CreateDate), pages, nil
	}
	id, pages, err := last(1)
	if err != nil || id < before || pages <= 1 {
		return 1, err
	}
	lo, hi := 2, pages
	for lo < hi {
		mid := (lo + hi) / 2
		id, _, err := last(mid)
		var se *StatusError
		if errors.As(err, &se) && se.Code >= 500 {
			hi = mid // (past a tag search's last page)
			continue
		}
		if err != nil {
			return 0, err
		}
		if id < before {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, nil
}

// clip fetches one clip by its word ID (nil: the site doesn't have it).
func (r *redgifs) clip(ctx context.Context, id string) (*Post, error) {
	body, err := r.get(ctx, "/v2/gifs/"+url.PathEscape(id), nil)
	var se *StatusError
	if errors.As(err, &se) && (se.Code == 404 || se.Code == 410) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var res struct {
		Gif redgifsGif `json:"gif"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("%s: unexpected reply: %s", r.c.m.Site, snippet(body))
	}
	types := map[string]string{}
	p, ok := r.post(res.Gif, types)
	if !ok {
		return nil, nil
	}
	p.TagTypes = types
	return &p, nil
}

// post turns a clip into a Post (false: no file to fetch). Tags become Vitrine's way ("Big Smile" ->
// "big_smile"); the user is the creator.
func (r *redgifs) post(g redgifsGif, types map[string]string) (Post, bool) {
	file, alt := g.URLs.HD, g.URLs.SD
	if file == "" {
		file, alt = alt, ""
	}
	if g.ID == "" || g.CreateDate <= 0 || file == "" {
		return Post{}, false
	}
	p := Post{
		ID: redgifsID(g.ID, g.CreateDate), FileURL: file, Rating: r.c.m.Rating(""), Width: g.Width, Height: g.Height,
		PostURL: "https://www.redgifs.com/watch/" + strings.ToLower(g.ID), Posted: time.Unix(g.CreateDate, 0).UTC(),
	}
	if alt != "" && alt != file {
		p.AltFileURLs = []string{alt}
	}
	seen := map[string]bool{}
	for _, t := range g.Tags {
		t = strings.ToLower(strings.Join(strings.Fields(CleanTag(t)), "_"))
		if t != "" && !seen[t] {
			seen[t] = true
			p.Tags = append(p.Tags, t)
			types[t] = "general" // (every tag typed: none is looked up later - a post can't be)
		}
	}
	if u := strings.ToLower(g.UserName); u != "" {
		if !seen[u] {
			p.Tags = append(p.Tags, u)
		}
		types[u] = "artist"
	}
	return p, true
}

// LookupURL fetches the clip behind a post's link (a refresh: the number alone can't be turned back into the clip).
func (r *redgifs) LookupURL(ctx context.Context, postURL string) (*Post, error) {
	q, err := r.parse(postURL)
	if err != nil || q.clip == "" {
		return nil, fmt.Errorf("%s: %q isn't a clip's link", r.c.m.Site, postURL)
	}
	return r.clip(ctx, q.clip)
}

// Lookup can't: a post's number doesn't give back the clip's word ID (LookupURL can, from its link).
func (r *redgifs) Lookup(ctx context.Context, id int64) (*Post, error) {
	return nil, ErrNoLookup
}
