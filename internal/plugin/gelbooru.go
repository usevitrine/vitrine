package plugin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// Post is one post from a site.
type Post struct {
	ID      int64
	MD5     [16]byte
	FileURL string
	Tags    []string // verbatim
	Sources []string // the post's source links (often the artist's post)
	Rating  media.Rating
	Width   int
	Height  int
	PostURL string
	Posted  time.Time // when it was posted on the site (zero if unknown)
	// TagTypes maps tag names to the site's tag types (artist, character,
	// ...; "tag" for general ones) when the plugin learns them; nil if
	// unknown. Shared by the posts of one page.
	TagTypes map[string]string
	// Partial: only the ID and md5 are known (an HTML list page); the rest
	// comes from Lookup, asked for only when the post is needed.
	Partial bool
	// AltFileURLs are tried in order when FileURL fails (e.g. a video's webm
	// when its mp4 is missing or empty).
	AltFileURLs []string
	// Gallery: the post is a whole gallery (a comic) - its pages, not one file (GalleryEngine).
	Gallery *Gallery
}

// Page is one search request's posts, newest first.
type Page struct {
	Posts []Post
	Total int // posts matching the query on the site (-1 if unknown)
}

// Engine searches a site.
type Engine interface {
	// Search returns the newest posts matching query with an ID below
	// before (0 = from the newest).
	Search(ctx context.Context, query string, before int64) (Page, error)
	// Lookup fetches one post by ID (nil = the site doesn't have it), for tag
	// refreshes. Posts from Lookup have no upload time.
	Lookup(ctx context.Context, id int64) (*Post, error)
}

// NewEngine returns the engine for a client's plugin.
func NewEngine(c *Client) (Engine, error) {
	switch c.m.Engine {
	case "gelbooru-v02":
		return &gelbooru{c: c}, nil
	case "gelbooru-v02-html":
		return &gelbooruHTML{c: c}, nil
	case "shimmie2":
		return &shimmie{c: c}, nil
	case "mastodon":
		return &mastodon{c: c}, nil
	case "danbooru":
		return &danbooru{c: c}, nil
	case "ehentai":
		return &ehentai{c: c}, nil
	}
	return nil, fmt.Errorf("plugin %s: unknown engine %q", c.m.ID, c.m.Engine)
}

// gelbooru talks to the Gelbooru 0.2 "dapi" (also used by many boorus,
// safebooru.org and others). Resuming uses the site's own id:<N filter, so
// new uploads can't shift pages under a running search.
type gelbooru struct {
	c   *Client
	fav favLists
}

type gelbooruPosts struct {
	Count int `xml:"count,attr"`
	Posts []struct {
		ID      int64  `xml:"id,attr"`
		MD5     string `xml:"md5,attr"`
		FileURL string `xml:"file_url,attr"`
		Tags    string `xml:"tags,attr"`
		Source  string `xml:"source,attr"`
		Rating  string `xml:"rating,attr"`
		Width   int    `xml:"width,attr"`
		Height  int    `xml:"height,attr"`
		Created string `xml:"created_at,attr"` // e.g. "Wed Oct 14 13:00:17 +0200 2026"
	} `xml:"post"`
}

func (g *gelbooru) Search(ctx context.Context, query string, before int64) (Page, error) {
	if user, ok := Favorites(query); ok {
		return g.favorites(ctx, user, before)
	}
	m := g.c.m
	u, err := url.Parse(strings.TrimRight(m.Base, "/") + "/index.php")
	if err != nil {
		return Page{}, err
	}
	tags := strings.TrimSpace(query)
	if before > 0 {
		tags += " id:<" + strconv.FormatInt(before, 10)
	}
	q := url.Values{
		"page": {"dapi"}, "s": {"post"}, "q": {"index"},
		"limit": {strconv.Itoa(m.Search.PerPage)}, "pid": {"0"}, "tags": {strings.TrimSpace(tags)},
	}
	u.RawQuery = q.Encode()
	body, err := g.c.API(ctx, u)
	if err != nil {
		return Page{}, err
	}
	var res gelbooruPosts
	if err := xml.Unmarshal(body, &res); err != nil {
		// Errors come back as other XML/HTML (e.g. a missing api_key).
		return Page{}, fmt.Errorf("%s: unexpected reply: %s", m.Site, snippet(body))
	}
	page := Page{Total: res.Count, Posts: make([]Post, 0, len(res.Posts))}
	for _, p := range res.Posts {
		post := Post{
			ID: p.ID, FileURL: p.FileURL, Tags: Tags(p.Tags), Sources: SourceLinks(p.Source),
			Rating: m.Rating(p.Rating), Width: p.Width, Height: p.Height, PostURL: m.Post(p.ID),
		}
		if b, err := hex.DecodeString(p.MD5); err == nil && len(b) == 16 {
			copy(post.MD5[:], b)
		}
		if t, err := time.Parse(gelbooruTime, p.Created); err == nil {
			post.Posted = t
		}
		if strings.HasPrefix(post.FileURL, "//") {
			post.FileURL = "https:" + post.FileURL
		}
		page.Posts = append(page.Posts, post)
	}
	if m.Tags.Types == "tag_info" && len(page.Posts) > 0 {
		// Best effort: without types the posts still download, and the
		// downloader looks the types up later.
		if types := g.tagInfo(ctx, u, q); types != nil {
			for i := range page.Posts {
				page.Posts[i].TagTypes = types
			}
		}
	}
	return page, nil
}

// tagInfo asks the JSON API for the same page with fields=tag_info, which
// adds each tag's type (the XML API has none). Returns nil if that fails.
func (g *gelbooru) tagInfo(ctx context.Context, u *url.URL, q url.Values) map[string]string {
	q.Set("json", "1")
	q.Set("fields", "tag_info")
	ju := *u
	ju.RawQuery = q.Encode()
	body, err := g.c.API(ctx, &ju)
	if err != nil {
		return nil
	}
	var posts []struct {
		TagInfo []struct {
			Tag  string `json:"tag"`
			Type string `json:"type"`
		} `json:"tag_info"`
	}
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil
	}
	types := map[string]string{}
	for _, p := range posts {
		for _, t := range p.TagInfo {
			if t.Tag != "" {
				types[CleanTag(t.Tag)] = t.Type
			}
		}
	}
	if len(types) == 0 {
		return nil
	}
	return types
}

// gelbooruTime is the created_at format of the gelbooru 0.2 API.
const gelbooruTime = "Mon Jan 02 15:04:05 -0700 2006"

// snippet is the start of a reply, for error messages (credentials never
// appear in replies, only in requests).
func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return strconv.Quote(s)
}

// Lookup fetches one post by ID: with tag types, one JSON request (it has
// everything but the upload time, which the library keeps); otherwise an
// id:N search. nil means the site doesn't have the post (any more).
func (g *gelbooru) Lookup(ctx context.Context, id int64) (*Post, error) {
	m := g.c.m
	if m.Tags.Types != "tag_info" {
		page, err := g.Search(ctx, "id:"+strconv.FormatInt(id, 10), 0)
		if err != nil {
			return nil, err
		}
		for i := range page.Posts {
			if page.Posts[i].ID == id {
				return &page.Posts[i], nil
			}
		}
		return nil, nil
	}
	u, err := url.Parse(strings.TrimRight(m.Base, "/") + "/index.php")
	if err != nil {
		return nil, err
	}
	u.RawQuery = url.Values{
		"page": {"dapi"}, "s": {"post"}, "q": {"index"}, "json": {"1"}, "fields": {"tag_info"},
		"limit": {"1"}, "pid": {"0"}, "tags": {"id:" + strconv.FormatInt(id, 10)},
	}.Encode()
	body, err := g.c.API(ctx, u)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, nil // no results: an empty reply
	}
	var posts []struct {
		ID      int64  `json:"id"`
		Hash    string `json:"hash"`
		FileURL string `json:"file_url"`
		Tags    string `json:"tags"`
		Source  string `json:"source"`
		Rating  string `json:"rating"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
		TagInfo []struct {
			Tag  string `json:"tag"`
			Type string `json:"type"`
		} `json:"tag_info"`
	}
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil, fmt.Errorf("%s: unexpected reply: %s", m.Site, snippet(body))
	}
	for _, p := range posts {
		if p.ID != id {
			continue
		}
		post := &Post{
			ID: p.ID, FileURL: p.FileURL, Tags: Tags(p.Tags), Sources: SourceLinks(p.Source),
			Rating: m.Rating(p.Rating), Width: p.Width, Height: p.Height, PostURL: m.Post(p.ID),
			TagTypes: map[string]string{},
		}
		if b, err := hex.DecodeString(p.Hash); err == nil && len(b) == 16 {
			copy(post.MD5[:], b)
		}
		if strings.HasPrefix(post.FileURL, "//") {
			post.FileURL = "https:" + post.FileURL
		}
		for _, t := range p.TagInfo {
			post.TagTypes[CleanTag(t.Tag)] = t.Type
		}
		return post, nil
	}
	return nil, nil
}
