package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// e621 reads e621's JSON API (e621.net and its safe-for-work mirror e926.net;
// https://e621.net/help/api): /posts.json, newest first, up to 320 a page,
// resumed with page=b<ID> ("before this post", as on Danbooru, whose code it
// grew from). Each post carries its tags by category (artist, copyright,
// character, species, meta, lore, ...), so tag types come free. An account is
// optional (login + api_key in the query); without one the site hides some
// posts' files - those posts are passed over. "fav:me" is the login's
// favorites (the site's own fav:<name>); "~a ~b" is its OR. The site has no
// count of a search's posts: a one-tag search's total is the tag's post count.
type e621 struct {
	c *Client
}

type e621Post struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"` // e.g. "2026-11-16T22:18:37.025-05:00"
	Rating    string `json:"rating"`     // s, q, e
	File      struct {
		Width  int    `json:"width"`
		Height int    `json:"height"`
		MD5    string `json:"md5"`
		URL    string `json:"url"` // null when the site hides the file (deleted, or for accounts only)
	} `json:"file"`
	Sources []string            `json:"sources"`
	Tags    map[string][]string `json:"tags"` // category -> tags
}

func (d *e621) url(path string, q url.Values) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(d.c.m.Base, "/") + path)
	if err != nil {
		return nil, err
	}
	u.RawQuery = q.Encode()
	return u, nil
}

// query turns a search into the site's: fav:me / fav:<name> are its own fav: search.
func (d *e621) query(q string) (string, error) {
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

func (d *e621) Search(ctx context.Context, query string, before int64) (Page, error) {
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
		v := url.Values{"limit": {strconv.Itoa(min(m.Search.PerPage, 320))}, "tags": {q}}
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
		var r struct {
			Posts []e621Post `json:"posts"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return Page{}, fmt.Errorf("%s: %s", m.Site, replyMessage(body))
		}
		if len(r.Posts) == 0 {
			break
		}
		types := map[string]string{}
		for _, p := range r.Posts {
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
		before = r.Posts[len(r.Posts)-1].ID
	}
	if len(page.Posts) == 0 {
		page.Total = 0
	}
	return page, nil
}

// count is a one-tag search's total: the tag's post count (-1 for any other
// search - the site has no count of a search's posts - and on e926, whose
// tag counts include the posts it doesn't show).
func (d *e621) count(ctx context.Context, q string) int {
	if strings.ContainsAny(q, " \t:~*") || strings.HasPrefix(q, "-") || q == "" {
		return -1
	}
	if u, err := url.Parse(d.c.m.Base); err != nil || strings.HasPrefix(bareHost(u.Hostname()), "e926.") {
		return -1
	}
	u, err := d.url("/tags.json", url.Values{"search[name]": {q}, "limit": {"1"}})
	if err != nil {
		return -1
	}
	body, err := d.c.API(ctx, u)
	if err != nil {
		return -1
	}
	var tags []struct {
		Name  string `json:"name"`
		Count *int   `json:"post_count"`
	}
	if json.Unmarshal(body, &tags) != nil || len(tags) != 1 || tags[0].Count == nil || !strings.EqualFold(tags[0].Name, q) {
		return -1
	}
	return *tags[0].Count
}

// post turns an API post into a Post (false: its file is hidden from us).
func (d *e621) post(p e621Post, types map[string]string) (Post, bool) {
	m := d.c.m
	if p.ID <= 0 || p.File.URL == "" {
		return Post{}, false
	}
	post := Post{
		ID: p.ID, FileURL: p.File.URL, Sources: SourceLinks(strings.Join(p.Sources, " ")), Rating: m.Rating(p.Rating),
		Width: p.File.Width, Height: p.File.Height, PostURL: m.Post(p.ID), MD5: ParseMD5(p.File.MD5),
	}
	if t, err := time.Parse(time.RFC3339Nano, p.CreatedAt); err == nil {
		post.Posted = t
	}
	for _, typ := range e621Categories(p.Tags) {
		for _, t := range p.Tags[typ] {
			t = CleanTag(t)
			post.Tags = append(post.Tags, t)
			types[t] = typ
		}
	}
	return post, true
}

// e621Categories are a post's tag categories in a steady order (the site's, then any new ones by name).
func e621Categories(tags map[string][]string) []string {
	order := []string{"artist", "contributor", "copyright", "character", "species", "general", "meta", "lore", "invalid"}
	out := slices.DeleteFunc(slices.Clone(order), func(c string) bool { _, ok := tags[c]; return !ok })
	var other []string
	for c := range tags {
		if !slices.Contains(order, c) {
			other = append(other, c)
		}
	}
	slices.Sort(other)
	return append(out, other...)
}

// Lookup fetches one post (nil = the site doesn't have it, or hides its file).
func (d *e621) Lookup(ctx context.Context, id int64) (*Post, error) {
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
	var r struct {
		Post e621Post `json:"post"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("%s: %s", d.c.m.Site, replyMessage(body))
	}
	types := map[string]string{}
	post, ok := d.post(r.Post, types)
	if !ok || post.ID != id {
		return nil, nil
	}
	post.TagTypes = types
	return &post, nil
}

// siteError says what a refused login means (a 401 with the site's own "SessionLoader::AuthenticationFailure").
func (d *e621) siteError(err error) error {
	var se *StatusError
	if errors.As(err, &se) {
		switch se.Code {
		case 401, 403:
			return fmt.Errorf("%s refused the login: check the user name and API key in Settings > Sites", d.c.m.Site)
		}
	}
	return err
}
