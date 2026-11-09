package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// One post as a one-time download: its page's link pasted in, or "id:N" typed. The downloader asks for that post
// alone, whatever the engine; a link for another site says so at once instead of searching for it.

// PostRefError is a link or ID that can't name a post on this site: its text is for the user.
type PostRefError struct{ Msg string }

func (e *PostRefError) Error() string { return e.Msg }

// NoPostError means the site doesn't have the post (or hides its file).
type NoPostError struct {
	Site string
	ID   int64
}

func (e *NoPostError) Error() string {
	return fmt.Sprintf("%s has no post %d with a file (it may have been deleted)", e.Site, e.ID)
}

// SingleEngine is an engine with its own way to fetch one post (the default: Lookup).
type SingleEngine interface {
	// Single returns the post's files, newest first (empty: the site doesn't have it).
	Single(ctx context.Context, id int64) ([]Post, error)
}

var (
	singleID        = regexp.MustCompile(`(?i)^id:(\d+)$`)
	shimmiePostPath = regexp.MustCompile(`/post/view/(\d+)/?$`)
	danbooruPath    = regexp.MustCompile(`^/posts/(\d+)(?:\.json)?/?$`)
	mastodonPaths   = []*regexp.Regexp{
		regexp.MustCompile(`^/@[^/]+/(\d+)/?$`),               // a post's page
		regexp.MustCompile(`^/web/@[^/]+/(\d+)/?$`),           // (older web app)
		regexp.MustCompile(`^/web/statuses/(\d+)/?$`),         // (older web app)
		regexp.MustCompile(`^/users/[^/]+/statuses/(\d+)/?$`), // (a post's permanent address)
	}
)

// CanSinglePost reports whether the site can fetch one post by its link or "id:N". (A gallery site's posts are
// galleries, which need their token: a gallery's link is already a search of its own there.)
func (m Manifest) CanSinglePost() bool { return m.Engine != "ehentai" }

// SinglePost reads a search as one post: its ID, or ok false for an ordinary search. A link or ID that can't name
// a post on this site is a *PostRefError. Nothing is asked of the site.
func SinglePost(m Manifest, query string) (id int64, ok bool, err error) {
	q := strings.TrimSpace(query)
	if strings.ContainsAny(q, " \t\r\n") {
		return 0, false, nil
	}
	if _, fav := Favorites(q); fav {
		return 0, false, nil
	}
	if mm := singleID.FindStringSubmatch(q); mm != nil {
		if !m.CanSinglePost() {
			return 0, false, &PostRefError{fmt.Sprintf("%s fetches a gallery by its link, not by an ID", m.Site)}
		}
		return parsePostID(m, mm[1])
	}
	lower := strings.ToLower(q)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
		return 0, false, nil
	}
	u, perr := url.Parse(q)
	if perr != nil || u.Hostname() == "" {
		return 0, false, &PostRefError{"that link can't be read: paste a post's whole address"}
	}
	if !m.ownHost(u.Hostname()) && !(m.Engine == "ehentai" && ehSiteHost(u.Hostname())) {
		return 0, false, &PostRefError{fmt.Sprintf("that link is for %s, not %s", u.Hostname(), m.Site)}
	}
	if !m.CanSinglePost() {
		return 0, false, nil // (a gallery's link: the engine's own search)
	}
	switch m.Engine {
	case "gelbooru-v02", "gelbooru-v02-html":
		v := u.Query()
		if (u.Path == "" || u.Path == "/" || strings.HasSuffix(u.Path, "/index.php")) && v.Get("page") == "post" && v.Get("s") == "view" && v.Get("id") != "" {
			return parsePostID(m, v.Get("id"))
		}
	case "shimmie2":
		if mm := shimmiePostPath.FindStringSubmatch(u.Path); mm != nil {
			return parsePostID(m, mm[1])
		}
	case "danbooru":
		if mm := danbooruPath.FindStringSubmatch(u.Path); mm != nil {
			return parsePostID(m, mm[1])
		}
	case "mastodon":
		for _, re := range mastodonPaths {
			if mm := re.FindStringSubmatch(u.Path); mm != nil {
				return parsePostID(m, mm[1])
			}
		}
		return 0, false, nil // (a /tags/ or /@profile link is a search there)
	}
	return 0, false, &PostRefError{fmt.Sprintf("that link isn't a post on %s: open the post and copy its address", m.Site)}
}

func parsePostID(m Manifest, s string) (int64, bool, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false, &PostRefError{fmt.Sprintf("%q isn't a post ID on %s", s, m.Site)}
	}
	return id, true, nil
}

// ownHost: a host of the site itself - post_url's or base's, with or without www. / api. (not every host in domains:
// two sites can share one, as danbooru.donmai.us and safebooru.donmai.us do).
func (m Manifest) ownHost(host string) bool {
	host = bareHost(host)
	for _, raw := range []string{m.PostURL, m.Base} {
		if u, err := url.Parse(strings.ReplaceAll(raw, "{id}", "0")); err == nil && u.Hostname() != "" && bareHost(u.Hostname()) == host {
			return true
		}
	}
	return false
}

func bareHost(h string) string {
	h = strings.ToLower(h)
	for _, p := range []string{"www.", "api."} {
		h = strings.TrimPrefix(h, p)
	}
	return h
}

// ehSiteHost: e-hentai and exhentai take each other's gallery links.
func ehSiteHost(h string) bool {
	h = bareHost(h)
	return h == "e-hentai.org" || h == "exhentai.org"
}

// FetchSingle is one post as a page of a search (before: a resume point, as in Search). A post the site doesn't have
// is a *NoPostError on a fresh start, and simply the end when resuming.
func FetchSingle(ctx context.Context, m Manifest, e Engine, id, before int64) (Page, error) {
	if before > 0 && before <= id {
		return Page{Total: 1}, nil // (nothing below the one post; a Mastodon post's files have higher IDs: asked for)
	}
	var posts []Post
	if s, ok := e.(SingleEngine); ok {
		list, err := s.Single(ctx, id)
		if err != nil {
			return Page{}, err
		}
		posts = list
	} else {
		p, err := e.Lookup(ctx, id)
		if err != nil {
			return Page{}, err
		}
		if p != nil {
			posts = []Post{*p}
		}
	}
	if len(posts) == 0 {
		if before > 0 {
			return Page{Total: 1}, nil
		}
		return Page{}, &NoPostError{Site: m.Site, ID: id}
	}
	page := Page{Total: len(posts)}
	for _, p := range posts {
		if before == 0 || p.ID < before {
			page.Posts = append(page.Posts, p)
		}
	}
	return page, nil
}

// IsNoPost reports whether err says the site doesn't have the post.
func IsNoPost(err error) bool {
	var n *NoPostError
	return errors.As(err, &n)
}

// IsPostRef reports whether err is a link or ID that can't name a post on the site.
func IsPostRef(err error) bool {
	var p *PostRefError
	return errors.As(err, &p)
}

// Single: the site's id:N search - unlike the one-post JSON answer, it has the upload time.
func (g *gelbooru) Single(ctx context.Context, id int64) ([]Post, error) {
	page, err := g.Search(ctx, "id:"+strconv.FormatInt(id, 10), 0)
	if err != nil {
		return nil, err
	}
	for _, p := range page.Posts {
		// (safebooru.org answers id:999999999 with an empty placeholder post, its file URL only ".../images//")
		if p.ID == id && p.FileURL != "" && !strings.HasSuffix(p.FileURL, "/") {
			return []Post{p}, nil
		}
	}
	return nil, nil
}

// Single: every file of the status - id is the status's own ID here, as in its link.
func (e *mastodon) Single(ctx context.Context, id int64) ([]Post, error) {
	body, err := e.api(ctx, "/api/v1/statuses/"+strconv.FormatInt(id, 10), nil)
	var se *StatusError
	if errors.As(err, &se) && (se.Code == 404 || se.Code == 410) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st, err := e.decodeStatus(body)
	if err != nil {
		return nil, err
	}
	types := map[string]string{}
	posts := e.posts(st, types)
	for i := range posts {
		posts[i].TagTypes = types
	}
	return posts, nil
}
