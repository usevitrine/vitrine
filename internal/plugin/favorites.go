package plugin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A user's favorites, on any site that has them: the search "fav:<user>"
// ("fav:me": the account in the site's credentials), or the favorites page's
// address pasted in. How the user is named depends on the site: a Gelbooru
// 0.2 site wants the user's ID number, Shimmie2 the user name, and Mastodon
// only shows your own (fav:me, with an access token).
//
// Favorites come in the order they were added, not by post: the list is read
// whole (and kept a while), then handed out newest post first like any
// search, so resuming by post ID works. A subscription's check reads the
// whole list again (a newly favorited post can be old: it isn't at the top) -
// see IsFavorites.

var (
	favQuery = regexp.MustCompile(`(?i)^fav:(\S+)$`)
	// index.php?page=favorites&s=view&id=123 (a Gelbooru 0.2 favorites page)
	favPageURL = regexp.MustCompile(`[?&]page=favorites(?:&|&amp;)s=view(?:&|&amp;)id=(\d+)`)
)

// Favorites reports whose favorites a search is ("me" or a user), if it is one.
func Favorites(query string) (user string, ok bool) {
	q := strings.TrimSpace(query)
	if m := favQuery.FindStringSubmatch(q); m != nil {
		return m[1], true
	}
	if !strings.ContainsAny(q, " \t") {
		if m := favPageURL.FindStringSubmatch(q); m != nil {
			return m[1], true
		}
	}
	return "", false
}

// IsFavorites reports whether a search is a user's favorites (Shimmie2's own
// favorited_by= too): a subscription's check then goes through the whole list,
// not just down to the newest post the last check saw.
func IsFavorites(query string) bool {
	if _, ok := Favorites(query); ok {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(query)), "favorited_by=")
}

// favLists keeps favorites lists for a while (the downloader asks for them page
// by page; a fresh start - before = 0 - reads the list again).
type favLists struct {
	mu    sync.Mutex
	lists map[string]favList
}

type favList struct {
	at    time.Time
	posts []Post // newest post first
}

const favKeep = 30 * time.Minute

// page is the posts below before (0 = from the newest), at most per.
func (f *favLists) page(key string, before int64, per int, load func() ([]Post, error)) (Page, error) {
	f.mu.Lock()
	l, ok := f.lists[key]
	f.mu.Unlock()
	if !ok || before == 0 || time.Since(l.at) > favKeep {
		posts, err := load()
		if err != nil {
			return Page{}, err
		}
		slices.SortFunc(posts, func(a, b Post) int {
			switch {
			case a.ID > b.ID:
				return -1
			case a.ID < b.ID:
				return 1
			}
			return 0
		})
		posts = slices.CompactFunc(posts, func(a, b Post) bool { return a.ID == b.ID })
		l = favList{at: time.Now(), posts: posts}
		f.mu.Lock()
		if f.lists == nil {
			f.lists = map[string]favList{}
		}
		for k, old := range f.lists { // (old lists go)
			if time.Since(old.at) > favKeep {
				delete(f.lists, k)
			}
		}
		f.lists[key] = l
		f.mu.Unlock()
	}
	start := 0
	if before > 0 {
		start, _ = slices.BinarySearchFunc(l.posts, before, func(p Post, id int64) int {
			switch {
			case p.ID > id:
				return -1
			case p.ID < id:
				return 1
			}
			return 0
		})
		if start < len(l.posts) && l.posts[start].ID == before {
			start++
		}
	}
	end := min(len(l.posts), start+max(1, per))
	return Page{Posts: slices.Clone(l.posts[start:end]), Total: len(l.posts)}, nil
}

// FavoritesBy says how a site names whose favorites ("id": a user ID number,
// "name": a user name, "me": only your own; "" = no favorites) and whether
// fav:me works with the credentials set.
func FavoritesBy(m Manifest, set map[string]bool) (by string, me bool) {
	switch m.Engine {
	case "gelbooru-v02", "gelbooru-v02-html":
		// (signed in through the site's form: the session says who)
		return "id", set["user_id"] || (m.Auth.Login == "gelbooru" && len(m.Auth.Fields) == 2 && set[m.Auth.Fields[0]])
	case "shimmie2":
		return "name", set["username"]
	case "mastodon":
		return "me", set["access_token"]
	case "danbooru":
		return "name", set["login"]
	}
	return "", false
}

// ---- Gelbooru 0.2: the favorites pages (index.php?page=favorites&s=view&id=N),
// 50 posts each; the posts' details come from Lookup when they're needed.

var (
	// <a href="index.php?page=post&amp;s=view&amp;id=123" id="p123"><img src=".../thumbnail_<md5>.jpg?123"
	favThumb   = regexp.MustCompile(`<a\b[^>]*\bhref="[^"]*?s=view&amp;id=(\d+)"[^>]*>\s*<img\b[^>]*?\bsrc="([^"]*)"`)
	favThumbID = regexp.MustCompile(`<a\b[^>]*\bid="p(\d+)"`)
	thumbMD5   = regexp.MustCompile(`thumbnail_([0-9a-f]{32})\.`)
)

// gelbooruFavUser is the user ID a Gelbooru 0.2 favorites search means.
func gelbooruFavUser(c *Client, user string) (string, error) {
	if strings.EqualFold(user, "me") {
		if id := c.credential("user_id"); id != "" {
			return id, nil
		}
		if id := c.sessionCookie("user_id"); id != "" { // (signed in: the session's user)
			return id, nil
		}
		if c.m.Auth.Login == "gelbooru" {
			return "", fmt.Errorf("%s: fav:me needs your account in Settings > Sites (or type fav:<user ID>)", c.m.Site)
		}
		return "", fmt.Errorf("%s: fav:me needs your user ID in Settings > Sites (or type fav:<user ID>)", c.m.Site)
	}
	if _, err := strconv.ParseInt(user, 10, 64); err != nil {
		return "", fmt.Errorf("%s: fav: needs the user's ID number (from their favorites page's address: ...&id=123)", c.m.Site)
	}
	return user, nil
}

// webBase is the site's own web address (an API on its own host - api.site -
// has the pages on the site's: from the post address, when the plugin may
// fetch from there).
func webBase(m Manifest) string {
	if u, err := url.Parse(m.PostURL); err == nil && u.Scheme != "" && u.Host != "" && m.allows(u.Hostname()) {
		return u.Scheme + "://" + u.Host
	}
	return strings.TrimRight(m.Base, "/")
}

// gelbooruFavorites reads every favorites page of a user: their posts, partial.
func gelbooruFavorites(ctx context.Context, c *Client, userID string) ([]Post, error) {
	m := c.m
	var out []Post
	seen := map[int64]bool{}
	for pid, pages := 0, 0; pages < 1000; pages++ {
		u, err := url.Parse(webBase(m) + "/index.php")
		if err != nil {
			return nil, err
		}
		u.RawQuery = url.Values{"page": {"favorites"}, "s": {"view"}, "id": {userID}, "pid": {strconv.Itoa(pid)}}.Encode()
		body, err := c.WebPage(ctx, u)
		if err != nil {
			return nil, err
		}
		text := string(body)
		if !strings.Contains(text, "<html") && !strings.Contains(text, "<HTML") {
			return nil, fmt.Errorf("%s: unexpected reply: %s", m.Site, snippet(body))
		}
		found := 0
		add := func(id int64, md5 string) {
			if id <= 0 || seen[id] {
				return
			}
			seen[id] = true
			found++
			p := Post{ID: id, PostURL: m.Post(id), Partial: true, Rating: m.Rating("")}
			if b, err := hex.DecodeString(md5); err == nil && len(b) == 16 {
				copy(p.MD5[:], b)
			}
			out = append(out, p)
		}
		for _, mm := range favThumb.FindAllStringSubmatch(text, -1) {
			id, _ := strconv.ParseInt(mm[1], 10, 64)
			md5 := ""
			if t := thumbMD5.FindStringSubmatch(mm[2]); t != nil {
				md5 = t[1]
			}
			add(id, md5)
		}
		if found == 0 { // another layout: the thumbnails' id="p123"
			for _, mm := range favThumbID.FindAllStringSubmatch(text, -1) {
				id, _ := strconv.ParseInt(mm[1], 10, 64)
				add(id, "")
			}
		}
		if found == 0 {
			break // past the last page
		}
		pid += found
	}
	return out, nil
}

func (g *gelbooru) favorites(ctx context.Context, user string, before int64) (Page, error) {
	id, err := gelbooruFavUser(g.c, user)
	if err != nil {
		return Page{}, err
	}
	return g.fav.page("fav:"+id, before, g.c.m.Search.PerPage, func() ([]Post, error) { return gelbooruFavorites(ctx, g.c, id) })
}

func (g *gelbooruHTML) favorites(ctx context.Context, user string, before int64) (Page, error) {
	if err := g.login(ctx); err != nil {
		return Page{}, err
	}
	id, err := gelbooruFavUser(g.c, user)
	if err != nil {
		return Page{}, err
	}
	return g.fav.page("fav:"+id, before, 42, func() ([]Post, error) { return gelbooruFavorites(ctx, g.c, id) })
}

// ---- Shimmie2: its own favorited_by=<name> search.

func (s *shimmie) favQuery(user string) (string, error) {
	if strings.EqualFold(user, "me") {
		name := s.c.credential("username")
		if name == "" {
			return "", fmt.Errorf("%s: fav:me needs your account in Settings > Sites (or type fav:<user name>)", s.c.m.Site)
		}
		user = name
	}
	return "favorited_by=" + user, nil
}

// ---- Mastodon: your own favourites (/api/v1/favourites, with an access token).

// mastoNext is a Link header's rel="next" address.
var mastoNext = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func (e *mastodon) favorites(ctx context.Context, user string, before int64) (Page, error) {
	if !strings.EqualFold(user, "me") {
		return Page{}, fmt.Errorf("%s: Mastodon shows only your own favourites: fav:me", e.c.m.Site)
	}
	if len(e.c.m.Auth.Fields) == 0 || !e.c.HasCredentials() {
		return Page{}, fmt.Errorf("%s: fav:me needs an access token in Settings > Sites", e.c.m.Site)
	}
	types := map[string]string{}
	load := func() ([]Post, error) {
		var out []Post
		u, err := url.Parse(strings.TrimRight(e.c.m.Base, "/") + "/api/v1/favourites?limit=40")
		if err != nil {
			return nil, err
		}
		for pages := 0; u != nil && pages < 500; pages++ {
			body, h, err := e.c.APIHeader(ctx, u)
			var se *StatusError
			if errors.As(err, &se) && se.Code == 401 {
				return nil, fmt.Errorf("%s: the access token was refused (it needs the \"read\" scope)", e.c.m.Site)
			}
			if err != nil {
				return nil, err
			}
			var sts []mastoStatus
			if err := json.Unmarshal(body, &sts); err != nil {
				return nil, fmt.Errorf("%s: unexpected reply: %s", e.c.m.Site, snippet(body))
			}
			if len(sts) == 0 {
				break
			}
			for _, st := range sts {
				out = append(out, e.posts(st, types)...)
			}
			u = nil
			if m := mastoNext.FindStringSubmatch(h.Get("Link")); m != nil {
				if next, err := url.Parse(m[1]); err == nil && e.c.m.allows(next.Hostname()) {
					u = next
				}
			}
		}
		return out, nil
	}
	page, err := e.fav.page("fav:me", before, 40, load)
	for i := range page.Posts {
		if page.Posts[i].TagTypes == nil {
			page.Posts[i].TagTypes = types
		}
	}
	return page, err
}
