package plugin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// gelbooruNewPosts is gelbooru.com's XML (2026-11-18): the post's fields as elements.
const gelbooruNewPosts = `<?xml version="1.0" encoding="UTF-8"?>
<posts limit="2" offset="0" count="302435"><post><id>15284454</id><created_at>Tue Nov 17 23:46:14 -0600 2026</created_at><score>0</score><width>2048</width><height>967</height><md5>a5343d5a5b0cef50049a58b8283c5d51</md5><rating>general</rating><source>https://example.test/some_artist/1</source><tags>blue_sky cloud some_artist highres</tags><title/><file_url>https://img4.example.test/images/a5/34/a5343d5a5b0cef50049a58b8283c5d51.jpg</file_url></post><post><id>15284436</id><created_at>Tue Nov 17 23:45:53 -0600 2026</created_at><width>10</width><height>10</height><md5>6bba41922e254fae89e4903bbf9e619f</md5><rating>explicit</rating><source/><tags>blue_sky some_series</tags><file_url>https://img4.example.test/images/6b/ba/b.png</file_url></post></posts>`

func TestGelbooruTagAPI(t *testing.T) {
	var tagAsks atomic.Int32
	var names []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("s") == "tag":
			tagAsks.Add(1)
			names = append(names, q.Get("names"))
			io.WriteString(w, `{"@attributes":{"limit":100,"offset":0,"count":4},"tag":[{"id":1,"name":"some_artist","count":5,"type":1,"ambiguous":0},{"id":2,"name":"highres","count":9,"type":5,"ambiguous":0},{"id":3,"name":"blue_sky","count":9,"type":0,"ambiguous":0},{"id":4,"name":"some_series","count":9,"type":3,"ambiguous":0}]}`)
		case q.Get("tags") == "id:1":
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<posts limit="100" offset="0" count="0"/>`)
		default:
			io.WriteString(w, gelbooruNewPosts)
		}
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte(`
id: gelbooru_test
engine: gelbooru-v02
base: ` + srv.URL + `
post_url: ` + srv.URL + `/post/{id}
limits: {requests_per_sec: 100}
ratings: {general: general, explicit: explicit}
tags:
  types: tag_api
  namespaces: {artist: creator, copyright: series, metadata: meta}
`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(testClient(m, srv, nil))
	ctx := context.Background()
	page, err := e.Search(ctx, "blue_sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 302435 || len(page.Posts) != 2 {
		t.Fatalf("total %d, posts %d", page.Total, len(page.Posts))
	}
	p := page.Posts[0]
	if p.ID != 15284454 || p.Width != 2048 || p.FileURL != "https://img4.example.test/images/a5/34/a5343d5a5b0cef50049a58b8283c5d51.jpg" ||
		strings.Join(p.Tags, " ") != "blue_sky cloud some_artist highres" || strings.Join(p.Sources, " ") != "https://example.test/some_artist/1" ||
		!p.Posted.Equal(time.Date(2026, 11, 18, 5, 46, 14, 0, time.UTC)) {
		t.Fatalf("first post %+v", p)
	}
	if p.TagTypes["some_artist"] != "artist" || p.TagTypes["highres"] != "metadata" || p.TagTypes["blue_sky"] != "general" ||
		page.Posts[1].TagTypes["some_series"] != "copyright" {
		t.Fatalf("types %v", p.TagTypes)
	}
	if _, ok := p.TagTypes["cloud"]; ok {
		t.Error("a tag the site didn't type got one")
	}
	if names[0] != "blue_sky cloud highres some_artist some_series" {
		t.Errorf("asked for %q", names[0])
	}
	// The next page: only cloud is still unknown (the site said nothing about it).
	if _, err := e.Search(ctx, "blue_sky", 15284436); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[1] != "cloud" {
		t.Errorf("second page asked for %q", names)
	}
	if got, err := e.Lookup(ctx, 15284454); err != nil || got == nil || got.TagTypes["some_artist"] != "artist" {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	if got, err := e.Lookup(ctx, 1); got != nil || err != nil {
		t.Fatalf("a missing post: %+v %v", got, err)
	}
}

// Posts the engine can't read (no ID) are an error, not an endless run of empty pages.
func TestGelbooruUnreadablePosts(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<posts count="5"><post><nothing/></post></posts>`)
	}))
	t.Cleanup(srv.Close)
	m, _ := Parse([]byte("id: t\nengine: gelbooru-v02\nbase: " + srv.URL + "\nlimits: {requests_per_sec: 100}\n"))
	e, _ := NewEngine(testClient(m, srv, nil))
	if _, err := e.Search(context.Background(), "blue_sky", 0); err == nil {
		t.Fatal("no error")
	}
}
