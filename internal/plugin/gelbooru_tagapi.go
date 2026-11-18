package plugin

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Gelbooru itself (gelbooru.com, 2026-11-18) runs a newer Gelbooru than the 0.2 sites: its XML has each post's fields
// as child elements (<post><id>1</id>...) where 0.2 has attributes, and its JSON API has no fields=tag_info - tag
// types come from the tag API (tags.types: tag_api), as numbers.

// gelbooruElemPosts is the newer XML: the same fields as gelbooruPosts, as elements.
type gelbooruElemPosts struct {
	Count int `xml:"count,attr"`
	Posts []struct {
		ID      int64  `xml:"id"`
		MD5     string `xml:"md5"`
		FileURL string `xml:"file_url"`
		Tags    string `xml:"tags"`
		Source  string `xml:"source"`
		Rating  string `xml:"rating"`
		Width   int    `xml:"width"`
		Height  int    `xml:"height"`
		Created string `xml:"created_at"`
	} `xml:"post"`
}

// parseGelbooruPosts reads either XML: attributes (Gelbooru 0.2) or, when those are missing, elements.
func parseGelbooruPosts(body []byte) (gelbooruPosts, error) {
	var res gelbooruPosts
	if err := xml.Unmarshal(body, &res); err != nil {
		return res, err
	}
	if len(res.Posts) == 0 || res.Posts[0].ID > 0 {
		return res, nil
	}
	var el gelbooruElemPosts
	if err := xml.Unmarshal(body, &el); err != nil {
		return res, err
	}
	res.Posts = res.Posts[:0]
	for _, p := range el.Posts {
		res.Posts = append(res.Posts, struct {
			ID      int64  `xml:"id,attr"`
			MD5     string `xml:"md5,attr"`
			FileURL string `xml:"file_url,attr"`
			Tags    string `xml:"tags,attr"`
			Source  string `xml:"source,attr"`
			Rating  string `xml:"rating,attr"`
			Width   int    `xml:"width,attr"`
			Height  int    `xml:"height,attr"`
			Created string `xml:"created_at,attr"`
		}(p))
	}
	return res, nil
}

// gelbooruTypeNames are the tag API's type numbers, by the names the plugin's namespaces use (as Gelbooru 0.2's
// tag_info names them).
var gelbooruTypeNames = map[int]string{0: "general", 1: "artist", 3: "copyright", 4: "character", 5: "metadata", 6: "deprecated"}

// gelbooruTagBatch is how many tags one tag API request asks for.
const gelbooruTagBatch = 100

// gelbooruTypeCache keeps what the tag API said, so later pages ask only for tags not seen yet. A tag's type rarely
// changes; the cache lasts until Vitrine restarts (it is dropped when it grows past gelbooruTypeCacheMax).
type gelbooruTypeCache struct {
	mu    sync.Mutex
	types map[string]string
}

const gelbooruTypeCacheMax = 200_000

// tagAPI finds the types of the posts' tags: from the cache, the rest from the tag API in batches. A tag the site
// doesn't know stays out. Returns nil if no type could be found (the downloader looks them up later).
func (g *gelbooru) tagAPI(ctx context.Context, posts []Post) map[string]string {
	g.tc.mu.Lock()
	if g.tc.types == nil || len(g.tc.types) > gelbooruTypeCacheMax {
		g.tc.types = map[string]string{}
	}
	out := map[string]string{}
	var missing []string
	seen := map[string]bool{}
	for _, p := range posts {
		for _, t := range p.Tags {
			if seen[t] {
				continue
			}
			seen[t] = true
			if typ, ok := g.tc.types[t]; ok {
				out[t] = typ
			} else {
				missing = append(missing, t)
			}
		}
	}
	g.tc.mu.Unlock()
	sort.Strings(missing)
	m := g.c.m
	for len(missing) > 0 {
		batch := missing[:min(len(missing), gelbooruTagBatch)]
		missing = missing[len(batch):]
		u, err := url.Parse(strings.TrimRight(m.Base, "/") + "/index.php")
		if err != nil {
			break
		}
		u.RawQuery = url.Values{
			"page": {"dapi"}, "s": {"tag"}, "q": {"index"}, "json": {"1"},
			"limit": {strconv.Itoa(len(batch))}, "names": {strings.Join(batch, " ")},
		}.Encode()
		body, err := g.c.API(ctx, u)
		if err != nil {
			break // best effort, as tag_info
		}
		var res struct {
			Tag []struct {
				Name string `json:"name"`
				Type int    `json:"type"`
			} `json:"tag"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			break
		}
		g.tc.mu.Lock()
		for _, t := range res.Tag {
			name := CleanTag(t.Name)
			typ, ok := gelbooruTypeNames[t.Type]
			if !ok {
				typ = strconv.Itoa(t.Type)
			}
			g.tc.types[name] = typ
			if seen[name] {
				out[name] = typ
			}
		}
		g.tc.mu.Unlock()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
