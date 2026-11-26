package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// moebooru reads a Moebooru site's JSON API (yande.re, konachan, sakugabooru;
// Danbooru 1's API): /post.json with api_version=2&include_tags=1, so each page
// comes with its tags' types ({"posts": [...], "tags": {"name": "artist"}}).
// Newest first; resumed with "id:<N" added to the search (posts below that one:
// new uploads can't shift pages). The total comes from /post.xml's count
// (first page only). No account: "fav:<name>" is the site's vote:3:<name>
// (that user's favorites); "~a ~b" is its OR.
type moebooru struct {
	c *Client
}

type moebooruPost struct {
	ID        int64  `json:"id"`
	CreatedAt int64  `json:"created_at"` // Unix seconds
	Rating    string `json:"rating"`     // s, q, e
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MD5       string `json:"md5"`
	FileURL   string `json:"file_url"` // missing when the site hides the file
	Source    string `json:"source"`
	Tags      string `json:"tags"`
}

type moebooruPage struct {
	Posts []moebooruPost    `json:"posts"`
	Tags  map[string]string `json:"tags"` // name -> type (general, artist, copyright, character, circle, ...)
}

var moebooruCount = regexp.MustCompile(`<posts\b[^>]*\bcount="(\d+)"`)

func (d *moebooru) url(path string, q url.Values) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(d.c.m.Base, "/") + path)
	if err != nil {
		return nil, err
	}
	u.RawQuery = q.Encode()
	return u, nil
}

// query turns a search into the site's: fav:<name> is its vote:3:<name>.
func (d *moebooru) query(q string) (string, error) {
	q = strings.TrimSpace(q)
	if user, ok := Favorites(q); ok {
		if strings.EqualFold(user, "me") {
			return "", fmt.Errorf("%s: type fav:<your user name> (Vitrine doesn't sign in to %s)", d.c.m.Site, d.c.m.Site)
		}
		return "vote:3:" + user, nil
	}
	return q, nil
}

// list asks for one page of posts with their tag types.
func (d *moebooru) list(ctx context.Context, tags string, limit int) (moebooruPage, error) {
	u, err := d.url("/post.json", url.Values{"api_version": {"2"}, "include_tags": {"1"}, "limit": {strconv.Itoa(limit)}, "tags": {tags}})
	if err != nil {
		return moebooruPage{}, err
	}
	body, err := d.c.API(ctx, u)
	if err != nil {
		return moebooruPage{}, err
	}
	var p moebooruPage
	if err := json.Unmarshal(body, &p); err != nil {
		return moebooruPage{}, fmt.Errorf("%s: %s", d.c.m.Site, moebooruMessage(body))
	}
	return p, nil
}

func (d *moebooru) Search(ctx context.Context, query string, before int64) (Page, error) {
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
		tags := q
		if before > 0 {
			tags = strings.TrimSpace(q + " id:<" + strconv.FormatInt(before, 10))
		}
		p, err := d.list(ctx, tags, min(m.Search.PerPage, 1000))
		if err != nil {
			return Page{}, err
		}
		if len(p.Posts) == 0 {
			break
		}
		types := moebooruTypes(p.Tags)
		for _, mp := range p.Posts {
			if post, ok := d.post(mp); ok {
				post.TagTypes = types
				page.Posts = append(page.Posts, post)
			}
		}
		if len(page.Posts) > 0 {
			break
		}
		before = p.Posts[len(p.Posts)-1].ID
	}
	if len(page.Posts) == 0 {
		page.Total = 0
	}
	return page, nil
}

func moebooruTypes(tags map[string]string) map[string]string {
	types := make(map[string]string, len(tags))
	for name, typ := range tags {
		types[CleanTag(name)] = typ
	}
	return types
}

// count asks how many posts match (-1 if the site won't say): the XML API's count.
func (d *moebooru) count(ctx context.Context, q string) int {
	u, err := d.url("/post.xml", url.Values{"limit": {"1"}, "tags": {q}})
	if err != nil {
		return -1
	}
	body, err := d.c.API(ctx, u)
	if err != nil {
		return -1
	}
	mm := moebooruCount.FindSubmatch(body)
	if mm == nil {
		return -1
	}
	n, err := strconv.Atoi(string(mm[1]))
	if err != nil {
		return -1
	}
	return n
}

// post turns an API post into a Post (false: its file is hidden from us).
func (d *moebooru) post(p moebooruPost) (Post, bool) {
	m := d.c.m
	if p.ID <= 0 || p.FileURL == "" {
		return Post{}, false
	}
	post := Post{
		ID: p.ID, FileURL: p.FileURL, Tags: Tags(p.Tags), Sources: SourceLinks(p.Source), Rating: m.Rating(p.Rating),
		Width: p.Width, Height: p.Height, PostURL: m.Post(p.ID), MD5: ParseMD5(p.MD5),
	}
	if p.CreatedAt > 0 {
		post.Posted = time.Unix(p.CreatedAt, 0).UTC()
	}
	return post, true
}

// Lookup fetches one post (nil = the site doesn't have it, or hides its file): the search id:N.
func (d *moebooru) Lookup(ctx context.Context, id int64) (*Post, error) {
	p, err := d.list(ctx, "id:"+strconv.FormatInt(id, 10), 1)
	var se *StatusError
	if errors.As(err, &se) && (se.Code == 404 || se.Code == 410) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, mp := range p.Posts {
		if post, ok := d.post(mp); ok && post.ID == id {
			post.TagTypes = moebooruTypes(p.Tags)
			return &post, nil
		}
	}
	return nil, nil
}

// moebooruMessage is a Moebooru error reply's reason, else the reply's start.
func moebooruMessage(body []byte) string {
	var e struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(body, &e) == nil && e.Reason != "" {
		return e.Reason
	}
	return replyMessage(body)
}
