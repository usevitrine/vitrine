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

// bluesky reads Bluesky's API (the AppView, https://docs.bsky.app): a profile's media posts work as a guest
// (public.api.bsky.app); hashtag searches need an account (Bluesky's search refuses guests), signed in with the
// handle and an app password (com.atproto.server.createSession at the base, bsky.social - every request then goes
// there with the session's token).
//
// Searches: "@handle" (the profile's own posts with media, no reposts), "@handle #tag" (theirs with that hashtag),
// "#tag" / "#tag #other" (posts with all of them; a word without # is a hashtag too), "-#tag" (without), a pasted
// profile, hashtag or post link. No "either" search (~).
//
// Each image or video of a post is one Vitrine post. Post keys are text (the author's DID and the record's key), so
// a post's ID is its time in milliseconds << 20, then 18 bits of a hash of the post's address, then the file's place
// (0-3): the same post always gets the same numbers, newer posts higher ones. The time is the one Bluesky sorts by
// (the earlier of the post's own time and when Bluesky saw it), so resuming below an ID asks for posts before its time.
// A number can't be turned back into the post: refreshes use the post's link (URLEngine).
//
// Files are the originals from the author's server (com.atproto.sync.getBlob on their PDS, found through
// plc.directory), with Bluesky's CDN copy (a JPEG, smaller) as the fallback for images.
type bluesky struct {
	c *Client

	mu        sync.Mutex
	session   string // the credentials the token is for ("" = none yet)
	sessionAt time.Time
	pds       map[string]string // DID -> its server ("" = not one we may download from)
	next      map[string]string // query + "\x00" + the last ID handed out -> the cursor after it
}

// (variables: the tests point them at a fake server)
var (
	blueskyPublic = "https://public.api.bsky.app"
	blueskyCDN    = "https://cdn.bsky.app/img/feed_fullsize/plain/"
	blueskyPLC    = "https://plc.directory/"
)

const blueskyPer = 100

type bskyPost struct {
	URI    string `json:"uri"`
	Author struct {
		DID    string `json:"did"`
		Handle string `json:"handle"`
		Labels []struct {
			Val string `json:"val"`
		} `json:"labels"`
	} `json:"author"`
	IndexedAt time.Time `json:"indexedAt"`
	Labels    []struct {
		Val string `json:"val"`
	} `json:"labels"`
	Record struct {
		CreatedAt string    `json:"createdAt"`
		Embed     bskyEmbed `json:"embed"`
		Tags      []string  `json:"tags"`
		Facets    []struct {
			Features []struct {
				Type string `json:"$type"`
				Tag  string `json:"tag"`
			} `json:"features"`
		} `json:"facets"`
	} `json:"record"`
}

type bskyBlob struct {
	Ref struct {
		Link string `json:"$link"`
	} `json:"ref"`
	MimeType string `json:"mimeType"`
}

type bskyRatio struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type bskyEmbed struct {
	Type   string `json:"$type"`
	Images []struct {
		Image       bskyBlob  `json:"image"`
		AspectRatio bskyRatio `json:"aspectRatio"`
	} `json:"images"`
	Video       *bskyBlob  `json:"video"`
	AspectRatio bskyRatio  `json:"aspectRatio"`
	Media       *bskyEmbed `json:"media"` // (recordWithMedia: a quote with the poster's own files)
}

type bskyQuery struct {
	post       string // an at:// address: that post
	actor      string // a handle or DID
	tags, none []string
}

var (
	bskyPostPath    = regexp.MustCompile(`^/profile/([^/]+)/post/([^/?#]+)/?$`)
	bskyProfilePath = regexp.MustCompile(`^/profile/([^/?#]+)/?$`)
	bskyTagPath     = regexp.MustCompile(`^/hashtag/([^/?#]+)/?$`)
)

func (e *bluesky) parse(q string) (bskyQuery, error) {
	var bq bskyQuery
	if _, ok := Favorites(q); ok {
		return bq, fmt.Errorf("%s: likes aren't supported yet (search a @profile or #hashtags)", e.c.m.Site)
	}
	for _, w := range strings.Fields(q) {
		if u, err := url.Parse(w); err == nil && u.Scheme != "" && u.Host != "" {
			p := u.EscapedPath()
			if mm := bskyPostPath.FindStringSubmatch(p); mm != nil {
				actor, _ := url.PathUnescape(mm[1])
				bq.post = "at://" + actor + "/app.bsky.feed.post/" + mm[2]
			} else if mm := bskyProfilePath.FindStringSubmatch(p); mm != nil {
				bq.actor, _ = url.PathUnescape(mm[1])
			} else if mm := bskyTagPath.FindStringSubmatch(p); mm != nil {
				t, _ := url.PathUnescape(mm[1])
				bq.tags = append(bq.tags, t)
			} else {
				return bq, fmt.Errorf("%s: that link isn't a profile, hashtag or post", e.c.m.Site)
			}
			continue
		}
		switch {
		case strings.HasPrefix(w, "@"):
			bq.actor = strings.TrimPrefix(w, "@")
		case strings.HasPrefix(w, "~"):
			return bq, fmt.Errorf("%s has no \"either\" search: search each hashtag on its own", e.c.m.Site)
		case strings.HasPrefix(w, "-"):
			bq.none = append(bq.none, strings.TrimPrefix(strings.TrimPrefix(w, "-"), "#"))
		default:
			bq.tags = append(bq.tags, strings.TrimPrefix(w, "#"))
		}
	}
	for _, l := range [][]string{bq.tags, bq.none} {
		for i, t := range l {
			l[i] = strings.ToLower(t)
		}
	}
	if bq.post != "" && (bq.actor != "" || len(bq.tags) > 0 || len(bq.none) > 0) {
		return bq, fmt.Errorf("%s: a post's link is a search on its own", e.c.m.Site)
	}
	if bq.post == "" && bq.actor == "" && len(bq.tags) == 0 {
		return bq, errors.New("type a #hashtag or a @profile")
	}
	return bq, nil
}

// keeps reports whether a post has every hashtag searched for and none of the left-out ones (checked here: a
// profile's feed has no tag filter, and the search's own matching is loose).
func (bq bskyQuery) keeps(tags []string) bool {
	has := map[string]bool{}
	for _, t := range tags {
		has[t] = true
	}
	for _, t := range bq.tags {
		if !has[t] {
			return false
		}
	}
	for _, t := range bq.none {
		if has[t] {
			return false
		}
	}
	return true
}

// signedIn reports whether there's an account to use (a handle and an app password).
func (e *bluesky) signedIn() bool {
	return e.c.credential("handle") != "" && e.c.credential("app_password") != ""
}

// login gets a session token for the account (again: the last one was refused). Bluesky's tokens last about two
// hours: a new one after 100 minutes, so an expired one is rarely sent (sign-ins are limited: 30 per 5 minutes).
func (e *bluesky) login(ctx context.Context, again bool) error {
	handle, pass := strings.TrimPrefix(strings.TrimSpace(e.c.credential("handle")), "@"), e.c.credential("app_password")
	key := handle + "\x00" + pass
	e.mu.Lock()
	fresh := !again && e.session == key && time.Since(e.sessionAt) < 100*time.Minute
	e.mu.Unlock()
	if fresh {
		return nil
	}
	u, err := url.Parse(strings.TrimRight(e.c.m.Base, "/") + "/xrpc/com.atproto.server.createSession")
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"identifier": handle, "password": pass})
	e.c.SetToken("")
	reply, err := e.c.PostJSON(ctx, u, body)
	var se *StatusError
	if errors.As(err, &se) && (se.Code == 400 || se.Code == 401) {
		return fmt.Errorf("%s: the handle or app password was refused (an account on another server than bsky.social can't sign in here)", e.c.m.Site)
	}
	if err != nil {
		return fmt.Errorf("%s: couldn't sign in: %w", e.c.m.Site, err)
	}
	var s struct {
		AccessJwt string `json:"accessJwt"`
	}
	if json.Unmarshal(reply, &s) != nil || s.AccessJwt == "" {
		return fmt.Errorf("%s: couldn't sign in (%s)", e.c.m.Site, snippet(reply))
	}
	e.c.SetToken(s.AccessJwt)
	e.mu.Lock()
	e.session, e.sessionAt = key, time.Now()
	e.mu.Unlock()
	return nil
}

// xrpc calls one of the API's methods: as the account when there is one (a fresh session when the token is
// refused or expired), else as a guest.
func (e *bluesky) xrpc(ctx context.Context, method string, v url.Values) ([]byte, error) {
	signed := e.signedIn()
	host := blueskyPublic
	if signed {
		host = strings.TrimRight(e.c.m.Base, "/")
	}
	u, err := url.Parse(host + "/xrpc/" + method)
	if err != nil {
		return nil, err
	}
	u.RawQuery = v.Encode()
	for try := 0; ; try++ {
		if signed {
			if err := e.login(ctx, try > 0); err != nil {
				return nil, err
			}
		}
		body, err := e.c.API(ctx, u)
		var se *StatusError
		// (a refused token: 401 - a new session once)
		if signed && errors.As(err, &se) && se.Code == 401 && try == 0 {
			continue
		}
		if errors.As(err, &se) && se.Code == 400 {
			return nil, fmt.Errorf("%s: no such profile or post, or Bluesky refused the search (400)", e.c.m.Site)
		}
		if errors.As(err, &se) && se.Code == 403 && !signed && method == "app.bsky.feed.searchPosts" {
			return nil, fmt.Errorf("%s: searching hashtags needs an account: add your handle and an app password in Settings > Sites", e.c.m.Site)
		}
		return body, err
	}
}

// postTime is the time Bluesky sorts a post by: the earlier of its own time and when Bluesky saw it.
func (p bskyPost) postTime() time.Time {
	t := p.IndexedAt
	if c, err := time.Parse(time.RFC3339Nano, p.Record.CreatedAt); err == nil && c.Before(t) && c.Year() > 2000 {
		t = c
	}
	return t
}

// blueskyID is a post's file's ID: its time in ms << 20, 18 bits of its address's hash, its place.
func blueskyID(uri string, t time.Time, place int) int64 {
	h := fnv.New32a()
	h.Write([]byte(uri))
	return t.UnixMilli()<<20 | int64(h.Sum32()&0x3ffff)<<2 | int64(place&3)
}

// cursorBefore is a cursor (a time) giving the posts at or before an ID's millisecond; the ones at or above the ID
// itself are dropped afterwards.
func cursorBefore(before int64) string {
	return time.UnixMilli(before>>20 + 1).UTC().Format("2006-01-02T15:04:05.000Z")
}

func (e *bluesky) Search(ctx context.Context, query string, before int64) (Page, error) {
	bq, err := e.parse(query)
	if err != nil {
		return Page{}, err
	}
	if bq.post != "" {
		if before > 0 {
			return Page{Total: 0}, nil
		}
		posts, err := e.getPosts(ctx, bq.post)
		if err != nil {
			return Page{}, err
		}
		page := Page{Total: 0}
		if len(posts) > 0 {
			types := map[string]string{}
			page.Posts = e.posts(ctx, posts[0], types)
			for i := range page.Posts {
				page.Posts[i].TagTypes = types
			}
			page.Total = len(page.Posts)
		}
		return page, nil
	}
	search := len(bq.tags) > 0 && (bq.actor == "" || e.signedIn())
	if search && !e.signedIn() {
		return Page{}, fmt.Errorf("%s: searching hashtags needs an account: add your handle and an app password in Settings > Sites", e.c.m.Site)
	}
	key := strings.ToLower(query) + "\x00"
	cursor := ""
	if before > 0 {
		e.mu.Lock()
		c, ok := e.next[key+strconv.FormatInt(before, 10)]
		e.mu.Unlock()
		if !ok {
			c = cursorBefore(before)
		}
		cursor = c
	}
	page := Page{Total: -1}
	types := map[string]string{}
	// Pages with nothing to take (no files, other hashtags, already handed out) are passed over, so an empty page
	// means the end.
	for tries := 0; tries < 10; tries++ {
		var posts []bskyPost
		var next string
		if search {
			posts, next, err = e.searchPage(ctx, bq, cursor, before > 0 && tries == 0 && !e.knows(key, before))
		} else {
			posts, next, err = e.feedPage(ctx, bq.actor, cursor)
		}
		if err != nil {
			return Page{}, err
		}
		for _, p := range posts {
			for _, fp := range e.posts(ctx, p, types) {
				if before == 0 || fp.ID < before {
					if bq.keeps(fp.Tags) {
						page.Posts = append(page.Posts, fp)
					}
				}
			}
		}
		cursor = next
		if len(page.Posts) > 0 || next == "" || len(posts) == 0 {
			break
		}
	}
	for i := range page.Posts {
		page.Posts[i].TagTypes = types
	}
	if len(page.Posts) == 0 {
		page.Total = 0
	} else if cursor != "" {
		e.mu.Lock()
		if e.next == nil || len(e.next) > 1000 {
			e.next = map[string]string{}
		}
		e.next[key+strconv.FormatInt(page.Posts[len(page.Posts)-1].ID, 10)] = cursor
		e.mu.Unlock()
	}
	return page, nil
}

// knows reports whether the cursor after a post handed out was kept (else it's made from the post's time).
func (e *bluesky) knows(key string, before int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.next[key+strconv.FormatInt(before, 10)]
	return ok
}

// feedPage is a page of a profile's own posts with files.
func (e *bluesky) feedPage(ctx context.Context, actor, cursor string) ([]bskyPost, string, error) {
	v := url.Values{"actor": {actor}, "filter": {"posts_with_media"}, "limit": {strconv.Itoa(blueskyPer)}}
	if cursor != "" {
		v.Set("cursor", cursor)
	}
	body, err := e.xrpc(ctx, "app.bsky.feed.getAuthorFeed", v)
	if err != nil {
		return nil, "", err
	}
	var r struct {
		Feed []struct {
			Post   bskyPost         `json:"post"`
			Reason *json.RawMessage `json:"reason"`
		} `json:"feed"`
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, "", fmt.Errorf("%s: unexpected reply: %s", e.c.m.Site, snippet(body))
	}
	var out []bskyPost
	for _, it := range r.Feed {
		if it.Reason != nil && string(*it.Reason) != "null" {
			continue // (a repost)
		}
		out = append(out, it.Post)
	}
	if len(r.Feed) == 0 {
		r.Cursor = ""
	}
	return out, r.Cursor, nil
}

// searchPage is a page of a hashtag search (newest first), from a profile's posts if the search names one. A cursor
// the engine made itself (resuming after a restart) is a time: the search's "until".
func (e *bluesky) searchPage(ctx context.Context, bq bskyQuery, cursor string, isTime bool) ([]bskyPost, string, error) {
	q := make([]string, len(bq.tags))
	for i, t := range bq.tags {
		q[i] = "#" + t
	}
	v := url.Values{"q": {strings.Join(q, " ")}, "sort": {"latest"}, "limit": {strconv.Itoa(blueskyPer)}, "tag": bq.tags}
	if bq.actor != "" {
		v.Set("author", bq.actor)
	}
	if isTime {
		v.Set("until", cursor)
	} else if cursor != "" {
		v.Set("cursor", cursor)
	}
	body, err := e.xrpc(ctx, "app.bsky.feed.searchPosts", v)
	if err != nil {
		return nil, "", err
	}
	var r struct {
		Posts  []bskyPost `json:"posts"`
		Cursor string     `json:"cursor"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, "", fmt.Errorf("%s: unexpected reply: %s", e.c.m.Site, snippet(body))
	}
	if len(r.Posts) == 0 {
		r.Cursor = ""
	}
	return r.Posts, r.Cursor, nil
}

func (e *bluesky) getPosts(ctx context.Context, uri string) ([]bskyPost, error) {
	body, err := e.xrpc(ctx, "app.bsky.feed.getPosts", url.Values{"uris": {uri}})
	if err != nil {
		return nil, err
	}
	var r struct {
		Posts []bskyPost `json:"posts"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("%s: unexpected reply: %s", e.c.m.Site, snippet(body))
	}
	return r.Posts, nil
}

// server is the address of a DID's own server, where its files are ("" = unknown, or not one of the plugin's
// domains: the CDN's copies then).
func (e *bluesky) server(ctx context.Context, did string) string {
	e.mu.Lock()
	s, ok := e.pds[did]
	e.mu.Unlock()
	if ok {
		return s
	}
	if !strings.HasPrefix(did, "did:plc:") {
		return "" // (did:web: its document is on its own host)
	}
	u, err := url.Parse(blueskyPLC + did)
	if err != nil {
		return ""
	}
	body, err := e.c.API(ctx, u)
	if err != nil {
		return "" // (not remembered: asked again next time)
	}
	var doc struct {
		Service []struct {
			ID       string `json:"id"`
			Endpoint string `json:"serviceEndpoint"`
		} `json:"service"`
	}
	json.Unmarshal(body, &doc)
	for _, sv := range doc.Service {
		if sv.ID == "#atproto_pds" {
			if su, err := url.Parse(sv.Endpoint); err == nil && su.Scheme == "https" && e.c.m.allows(su.Hostname()) {
				s = strings.TrimRight(sv.Endpoint, "/")
			}
		}
	}
	e.mu.Lock()
	if e.pds == nil || len(e.pds) > 10000 {
		e.pds = map[string]string{}
	}
	e.pds[did] = s
	e.mu.Unlock()
	return s
}

// bskyRating is the strongest of the post's and its author's adult-content labels ("safe" without one).
func bskyRating(p bskyPost) string {
	rank := map[string]int{"graphic-media": 1, "nudity": 2, "sexual": 3, "porn": 4}
	best := "safe"
	check := func(v string) {
		if rank[v] > rank[best] {
			best = v
		}
	}
	for _, l := range p.Labels {
		check(l.Val)
	}
	for _, l := range p.Author.Labels {
		check(l.Val)
	}
	return best
}

// posts makes one post per image or video of a Bluesky post (its last first: IDs going down).
func (e *bluesky) posts(ctx context.Context, p bskyPost, types map[string]string) []Post {
	did, rkey, ok := strings.Cut(strings.TrimPrefix(p.URI, "at://"), "/app.bsky.feed.post/")
	if !ok || did == "" || rkey == "" || p.IndexedAt.IsZero() {
		return nil
	}
	emb := p.Record.Embed
	if emb.Media != nil {
		emb = *emb.Media
	}
	type file struct {
		cid, mime string
		ratio     bskyRatio
		video     bool
	}
	var files []file
	for _, im := range emb.Images {
		files = append(files, file{cid: im.Image.Ref.Link, mime: im.Image.MimeType, ratio: im.AspectRatio})
	}
	if emb.Video != nil {
		files = append(files, file{cid: emb.Video.Ref.Link, mime: emb.Video.MimeType, ratio: emb.AspectRatio, video: true})
	}
	if len(files) == 0 {
		return nil
	}
	tags := []string{}
	seen := map[string]bool{}
	add := func(t string) {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "#")))
		if t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	for _, f := range p.Record.Facets {
		for _, ft := range f.Features {
			if ft.Type == "app.bsky.richtext.facet#tag" {
				add(ft.Tag)
			}
		}
	}
	for _, t := range p.Record.Tags {
		add(t)
	}
	if h := strings.ToLower(p.Author.Handle); h != "" && h != "handle.invalid" {
		add(h)
		types[h] = "artist"
	}
	at := p.postTime()
	posted, err := time.Parse(time.RFC3339Nano, p.Record.CreatedAt)
	if err != nil {
		posted = p.IndexedAt
	}
	pds := e.server(ctx, did)
	postURL := "https://bsky.app/profile/" + did + "/post/" + rkey
	rating := e.c.m.Rating(bskyRating(p))
	var out []Post
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		if f.cid == "" || i > 3 {
			continue
		}
		cdn := blueskyCDN + did + "/" + f.cid + "@jpeg"
		post := Post{ID: blueskyID(p.URI, at, i), Tags: tags, PostURL: postURL, Posted: posted, Rating: rating,
			Width: f.ratio.Width, Height: f.ratio.Height}
		switch {
		case pds != "":
			post.FileURL = pds + "/xrpc/com.atproto.sync.getBlob?" + url.Values{"did": {did}, "cid": {f.cid}}.Encode()
			if !f.video {
				post.AltFileURLs = []string{cdn}
			}
		case !f.video:
			post.FileURL = cdn
		default:
			continue // (a video on a server we may not download from: the CDN has only its stream)
		}
		out = append(out, post)
	}
	return out
}

// LookupURL reads a post again from its link (a refresh); id says which of its files.
func (e *bluesky) LookupURL(ctx context.Context, postURL string, id int64) (*Post, error) {
	bq, err := e.parse(postURL)
	if err != nil || bq.post == "" {
		return nil, fmt.Errorf("%s: %q isn't a post's link", e.c.m.Site, postURL)
	}
	posts, err := e.getPosts(ctx, bq.post)
	if err != nil || len(posts) == 0 {
		return nil, err
	}
	types := map[string]string{}
	for _, p := range e.posts(ctx, posts[0], types) {
		if p.ID&3 == id&3 {
			p.TagTypes = types
			return &p, nil
		}
	}
	return nil, nil
}

// Lookup can't: a post's number doesn't give back its address (LookupURL can, from its link).
func (e *bluesky) Lookup(ctx context.Context, id int64) (*Post, error) {
	return nil, ErrNoLookup
}
