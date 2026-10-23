package plugin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// danbooru reads a Danbooru site's JSON API (danbooru.donmai.us and the many
// sites running the same software; https://danbooru.donmai.us/wiki_pages/help:api):
// /posts.json, newest first, resumed with the site's own page=b<ID> ("before
// this post", so new uploads can't shift pages under a running search). Each
// post carries its tags by type (artist, copyright, character, meta, general),
// so tag types come free. An account is optional (login + api_key in the
// query); without one the site allows two tags per search and hides some
// posts' files - those posts are passed over. "fav:me" is the logged-in
// user's favorites (the site's own fav:<name> search); "~a ~b" is its OR.
type danbooru struct {
	c *Client
}

type danbooruPost struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"` // e.g. "2026-10-14T13:00:17.123-04:00"
	Rating    string `json:"rating"`     // g, s, q, e
	Width     int    `json:"image_width"`
	Height    int    `json:"image_height"`
	MD5       string `json:"md5"`
	FileURL   string `json:"file_url"` // missing when the site hides the file (banned, or for accounts only)
	Source    string `json:"source"`
	Tags      string `json:"tag_string"`
	General   string `json:"tag_string_general"`
	Artist    string `json:"tag_string_artist"`
	Copyright string `json:"tag_string_copyright"`
	Character string `json:"tag_string_character"`
	Meta      string `json:"tag_string_meta"`
}

func (d *danbooru) url(path string, q url.Values) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(d.c.m.Base, "/") + path)
	if err != nil {
		return nil, err
	}
	u.RawQuery = q.Encode()
	return u, nil
}

// query turns a search into the site's: fav:me / fav:<name> are its own fav: search.
func (d *danbooru) query(q string) (string, error) {
	q = strings.TrimSpace(q)
	if user, ok := Favorites(q); ok {
		if strings.EqualFold(user, "me") {
			user = d.c.credential("login")
			if user == "" {
				return "", fmt.Errorf("%s: fav:me needs your account in Settings > Sites (or type fav:<user name>)", d.c.m.Site)
			}
		}
		return "fav:" + user, nil
	}
	return q, nil
}

func (d *danbooru) Search(ctx context.Context, query string, before int64) (Page, error) {
	m := d.c.m
	q, err := d.query(query)
	if err != nil {
		return Page{}, err
	}
	page := Page{Total: -1}
	if before == 0 {
		page.Total = d.count(ctx, q) // (best effort: the progress bar's total)
	}
	// A page whose posts all have their files hidden is passed over (an empty
	// page means the end).
	for tries := 0; tries < 10; tries++ {
		v := url.Values{"limit": {strconv.Itoa(min(m.Search.PerPage, 200))}, "tags": {q}}
		if before > 0 {
			v.Set("page", "b"+strconv.FormatInt(before, 10))
		}
		u, err := d.url("/posts.json", v)
		if err != nil {
			return Page{}, err
		}
		body, err := d.c.API(ctx, u)
		if err != nil {
			return Page{}, d.siteError(err)
		}
		var posts []danbooruPost
		if err := json.Unmarshal(body, &posts); err != nil {
			return Page{}, fmt.Errorf("%s: %s", m.Site, replyMessage(body))
		}
		if len(posts) == 0 {
			break
		}
		types := map[string]string{}
		for _, p := range posts {
			if post, ok := d.post(p, types); ok {
				page.Posts = append(page.Posts, post)
			}
		}
		for i := range page.Posts {
			page.Posts[i].TagTypes = types
		}
		if len(page.Posts) > 0 {
			break
		}
		before = posts[len(posts)-1].ID
	}
	if len(page.Posts) == 0 {
		page.Total = 0
	}
	return page, nil
}

// count asks how many posts match (-1 if the site won't say).
func (d *danbooru) count(ctx context.Context, q string) int {
	u, err := d.url("/counts/posts.json", url.Values{"tags": {q}})
	if err != nil {
		return -1
	}
	body, err := d.c.API(ctx, u)
	if err != nil {
		return -1
	}
	var r struct {
		Counts struct {
			Posts *int `json:"posts"`
		} `json:"counts"`
	}
	if json.Unmarshal(body, &r) != nil || r.Counts.Posts == nil {
		return -1
	}
	return *r.Counts.Posts
}

// post turns an API post into a Post (false: its file is hidden from us).
func (d *danbooru) post(p danbooruPost, types map[string]string) (Post, bool) {
	m := d.c.m
	if p.ID <= 0 || p.FileURL == "" {
		return Post{}, false
	}
	post := Post{
		ID: p.ID, FileURL: p.FileURL, Tags: Tags(p.Tags), Sources: SourceLinks(p.Source), Rating: m.Rating(p.Rating),
		Width: p.Width, Height: p.Height, PostURL: m.Post(p.ID),
	}
	if b, err := hex.DecodeString(p.MD5); err == nil && len(b) == 16 {
		copy(post.MD5[:], b)
	}
	if t, err := time.Parse(time.RFC3339Nano, p.CreatedAt); err == nil {
		post.Posted = t
	}
	for typ, list := range map[string]string{"general": p.General, "artist": p.Artist, "copyright": p.Copyright, "character": p.Character, "meta": p.Meta} {
		for _, t := range strings.Fields(list) {
			types[CleanTag(t)] = typ
		}
	}
	return post, true
}

// Lookup fetches one post (nil = the site doesn't have it, or hides its file).
func (d *danbooru) Lookup(ctx context.Context, id int64) (*Post, error) {
	u, err := d.url("/posts/"+strconv.FormatInt(id, 10)+".json", nil)
	if err != nil {
		return nil, err
	}
	body, err := d.c.API(ctx, u)
	var se *StatusError
	if errors.As(err, &se) && (se.Code == 404 || se.Code == 410) {
		return nil, nil
	}
	if err != nil {
		return nil, d.siteError(err)
	}
	var p danbooruPost
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("%s: %s", d.c.m.Site, replyMessage(body))
	}
	types := map[string]string{}
	post, ok := d.post(p, types)
	if !ok || post.ID != id {
		return nil, nil
	}
	post.TagTypes = types
	return &post, nil
}

// siteError says what a refused search means (Danbooru answers 422 with a
// message, e.g. too many tags for an account without a membership).
func (d *danbooru) siteError(err error) error {
	var se *StatusError
	if errors.As(err, &se) {
		switch se.Code {
		case 422:
			return fmt.Errorf("%s refused the search (an account without a membership can search two tags at once)", d.c.m.Site)
		case 401, 403:
			return fmt.Errorf("%s refused the login: check the user name and API key in Settings > Sites", d.c.m.Site)
		}
	}
	return err
}

// replyMessage is a Danbooru error reply's message, else the reply's start.
func replyMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return "unexpected reply: " + snippet(body)
}
