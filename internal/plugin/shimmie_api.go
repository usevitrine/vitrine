package plugin

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Shimmie2's Danbooru-style API (api: danbooru in the plugin; ext/danbooru_api): /api/danbooru/find_posts answers a
// search, or one post (?id=N), as Danbooru 1's XML - each post's ID, md5, file, tags, source, rating, size and
// upload time in one request, where the web pages need a list page and then each post's page. Searches and resuming
// are the same as through the pages (searchTags).

type shimmieAPIPosts struct {
	Count int `xml:"count,attr"`
	Posts []struct {
		ID      int64  `xml:"id,attr"`
		MD5     string `xml:"md5,attr"`
		FileURL string `xml:"file_url,attr"`
		Tags    string `xml:"tags,attr"`
		Source  string `xml:"source,attr"`
		Rating  string `xml:"rating,attr"` // s, q, e; "?" unrated
		Width   int    `xml:"width,attr"`
		Height  int    `xml:"height,attr"`
		Date    string `xml:"date,attr"` // "2026-11-18 00:53:59", UTC
	} `xml:"post"`
}

// shimmieAPITime is the API's upload time (in UTC).
const shimmieAPITime = "2006-01-02 15:04:05"

// shimmieAPIRatings are Shimmie's rating letters, as the plugin's ratings name them.
var shimmieAPIRatings = map[string]string{"s": "safe", "q": "questionable", "e": "explicit", "?": "unrated"}

func (s *shimmie) apiSearch(ctx context.Context, query string, before int64) (Page, error) {
	per := s.c.m.Search.PerPage
	if per <= 0 {
		per = 100
	}
	return s.apiFind(ctx, url.Values{
		"tags":  {strings.Join(searchTags(query, before), " ")},
		"limit": {strconv.Itoa(per)},
	})
}

// apiFind asks find_posts (a search, or ?id=N for one post).
func (s *shimmie) apiFind(ctx context.Context, q url.Values) (Page, error) {
	m := s.c.m
	u, err := url.Parse(strings.TrimRight(m.Base, "/") + "/api/danbooru/find_posts")
	if err != nil {
		return Page{}, err
	}
	u.RawQuery = q.Encode()
	body, err := s.c.API(ctx, u)
	if err != nil {
		return Page{}, err
	}
	// HTML entities in tags and sources (&oacute;: rule34.paheal.net, 2026-11-18) - XML has only five.
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Entity = xml.HTMLEntity
	var res shimmieAPIPosts
	if err := d.Decode(&res); err != nil || !strings.Contains(string(body[:min(len(body), 200)]), "<posts") {
		return Page{}, fmt.Errorf("%s: unexpected reply: %s", m.Site, snippet(body))
	}
	page := Page{Total: res.Count, Posts: make([]Post, 0, len(res.Posts))}
	for _, p := range res.Posts {
		if p.ID <= 0 || p.FileURL == "" {
			continue
		}
		rating := strings.ToLower(p.Rating)
		if name, ok := shimmieAPIRatings[rating]; ok {
			rating = name
		}
		post := Post{
			ID: p.ID, FileURL: absURL(m.Base, p.FileURL), Tags: strings.Fields(strings.ToLower(p.Tags)),
			Sources: SourceLinks(p.Source), Rating: m.Rating(rating), Width: p.Width, Height: p.Height,
			PostURL: m.Post(p.ID), MD5: ParseMD5(p.MD5),
		}
		if t, err := time.Parse(shimmieAPITime, p.Date); err == nil {
			post.Posted = t
		}
		page.Posts = append(page.Posts, post)
	}
	return page, nil
}
