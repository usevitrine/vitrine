package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// A made-up e621 post (fileURL "" = the site hides its file: null).
func e6Post(id int64, fileURL string) string {
	u := "null"
	if fileURL != "" {
		u = fmt.Sprintf("%q", fileURL)
	}
	return fmt.Sprintf(`{"id":%d,"created_at":"2026-11-16T22:18:37.025-05:00","rating":"q",`+
		`"file":{"width":800,"height":600,"ext":"webm","md5":"00112233445566778899aabbccddeeff","url":%s},`+
		`"sources":["https://example.test/art/%d","x.com/some_artist/status/1"],`+
		`"tags":{"general":["blue_sky"],"artist":["some_artist"],"contributor":[],"copyright":["a_series"],"character":["some_character"],`+
		`"species":["some_species"],"invalid":[],"meta":["webm"],"lore":[]}}`, id, u, id)
}

func fakeE621(t *testing.T) (Manifest, *httptest.Server, *[]*http.Request) {
	t.Helper()
	var reqs []*http.Request
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r)
		file := func(id int64) string { return fmt.Sprintf("%s/data/%d.webm", srv.URL, id) }
		q := r.URL.Query()
		if q.Get("api_key") == "WRONG" {
			w.WriteHeader(401)
			io.WriteString(w, `{"success":false,"message":"SessionLoader::AuthenticationFailure","code":null}`)
			return
		}
		switch {
		case r.URL.Path == "/tags.json":
			io.WriteString(w, `[{"id":1,"name":"blue_sky","post_count":2,"category":0}]`)
		case r.URL.Path == "/posts/299.json":
			io.WriteString(w, `{"post":`+e6Post(299, file(299))+`}`)
		case strings.HasPrefix(r.URL.Path, "/posts/"):
			w.WriteHeader(404)
			io.WriteString(w, `{"success":false,"reason":"not found"}`)
		case r.URL.Path == "/posts.json":
			switch q.Get("page") {
			case "":
				io.WriteString(w, `{"posts":[`+e6Post(300, "")+","+e6Post(299, file(299))+","+e6Post(298, file(298))+"]}")
			case "b500":
				io.WriteString(w, `{"posts":[`+e6Post(450, "")+"]}") // every file hidden: passed over
			case "b450":
				io.WriteString(w, `{"posts":[`+e6Post(449, file(449))+"]}")
			default:
				io.WriteString(w, `{"posts":[]}`)
			}
		}
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte(`
id: e6test
engine: e621
base: ` + srv.URL + `
post_url: ` + srv.URL + `/posts/{id}
auth: {fields: [login, api_key]}
limits: {requests_per_sec: 100}
search: {per_page: 320}
ratings: {s: general, q: questionable, e: explicit}
tags: {types: post, namespaces: {artist: creator, copyright: series, character: character, species: species, meta: meta}}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, srv, &reqs
}

func TestE621Search(t *testing.T) {
	m, srv, reqs := fakeE621(t)
	e, _ := NewEngine(testClient(m, srv, map[string]string{"login": "some_user", "api_key": "S3CRET"}))
	ctx := context.Background()
	page, err := e.Search(ctx, "blue_sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Posts) != 2 || page.Posts[0].ID != 299 || page.Posts[1].ID != 298 {
		t.Fatalf("page (post 300's file is hidden): %+v", page)
	}
	p := page.Posts[0]
	if p.Rating != media.RatingQuestionable || p.MD5[0] != 0x00 || p.MD5[15] != 0xff || p.Width != 800 ||
		p.PostURL != srv.URL+"/posts/299" || len(p.Sources) != 2 || p.Sources[1] != "https://x.com/some_artist/status/1" {
		t.Fatalf("post: %+v", p)
	}
	if want := []string{"some_artist", "a_series", "some_character", "some_species", "blue_sky", "webm"}; strings.Join(p.Tags, " ") != strings.Join(want, " ") {
		t.Fatalf("tags %v, want %v", p.Tags, want)
	}
	if want := time.Date(2026, 11, 17, 3, 18, 37, 25e6, time.UTC); !p.Posted.Equal(want) {
		t.Fatalf("posted %v, want %v", p.Posted, want)
	}
	if p.TagTypes["some_artist"] != "artist" || p.TagTypes["a_series"] != "copyright" || p.TagTypes["some_character"] != "character" ||
		p.TagTypes["some_species"] != "species" || p.TagTypes["webm"] != "meta" || p.TagTypes["blue_sky"] != "general" {
		t.Fatalf("tag types: %v", p.TagTypes)
	}
	var list *http.Request
	for _, r := range *reqs {
		if r.URL.Path == "/posts.json" {
			list = r
		}
	}
	if q := list.URL.Query(); q.Get("tags") != "blue_sky" || q.Get("limit") != "320" || q.Get("login") != "some_user" || q.Get("api_key") != "S3CRET" || q.Has("page") {
		t.Fatalf("list query: %v", q)
	}

	// Resuming: page=b<ID>; the end is an empty page.
	if page, err = e.Search(ctx, "blue_sky", 298); err != nil || len(page.Posts) != 0 {
		t.Fatalf("past the end: %+v %v", page, err)
	}
	if q := (*reqs)[len(*reqs)-1].URL.Query(); q.Get("page") != "b298" {
		t.Fatalf("resume query: %v", q)
	}
	// A page of hidden files only is passed over, not taken as the end.
	if page, _ = e.Search(ctx, "blue_sky", 500); len(page.Posts) != 1 || page.Posts[0].ID != 449 {
		t.Fatalf("over a hidden page: %+v", page)
	}

	// Totals: a one-tag search only (the tag's count); any other search: the site doesn't say.
	n := len(*reqs)
	if page, _ = e.Search(ctx, "blue_sky -cloud", 0); page.Total != -1 {
		t.Fatalf("two tags' total: %d", page.Total)
	}
	for _, r := range (*reqs)[n:] {
		if r.URL.Path == "/tags.json" {
			t.Fatalf("asked a tag's count for two tags")
		}
	}

	// Favorites: the site's fav:<name> (fav:me = the login).
	e.Search(ctx, "fav:me", 0)
	if q := (*reqs)[len(*reqs)-1].URL.Query(); q.Get("tags") != "fav:some_user" {
		t.Fatalf("fav:me query: %v", q)
	}
	anon, _ := NewEngine(testClient(m, srv, nil))
	if _, err := anon.Search(ctx, "fav:me", 0); err == nil || !strings.Contains(err.Error(), "account") {
		t.Fatalf("fav:me without an account: %v", err)
	}
	// A wrong key: the site's 401, explained.
	bad, _ := NewEngine(testClient(m, srv, map[string]string{"login": "some_user", "api_key": "WRONG"}))
	if _, err := bad.Search(ctx, "blue_sky", 0); err == nil || !strings.Contains(err.Error(), "refused the login") {
		t.Fatalf("wrong key: %v", err)
	}
}

func TestE621Lookup(t *testing.T) {
	m, srv, reqs := fakeE621(t)
	e, _ := NewEngine(testClient(m, srv, nil))
	p, err := e.Lookup(context.Background(), 299)
	if err != nil || p == nil || p.ID != 299 || p.TagTypes["some_artist"] != "artist" {
		t.Fatalf("lookup: %+v %v", p, err)
	}
	if (*reqs)[0].URL.Path != "/posts/299.json" {
		t.Fatalf("asked %s", (*reqs)[0].URL)
	}
	if p, err := e.Lookup(context.Background(), 7); p != nil || err != nil {
		t.Fatalf("a missing post: %+v %v", p, err)
	}
}

func TestE621Manifest(t *testing.T) {
	m, _, _ := fakeE621(t)
	if tmpl, sep := m.OrQuery(); tmpl != "~{tags}" || sep != " ~" {
		t.Fatalf("OR search: %q %q", tmpl, sep)
	}
	if by, me := FavoritesBy(m, map[string]bool{"login": true}); by != "name" || !me {
		t.Fatalf("favorites: %q %v", by, me)
	}
}

// Against the real site (e926.net, e621's safe-for-work side, no account):
// only with VITRINE_LIVE=1 - the other tests never leave the machine.
func TestE621Live(t *testing.T) {
	if os.Getenv("VITRINE_LIVE") != "1" {
		t.Skip("set VITRINE_LIVE=1 to ask the real site")
	}
	m, err := Parse([]byte(`
id: e926
engine: e621
base: https://e926.net
post_url: https://e926.net/posts/{id}
limits: {requests_per_sec: 1}
search: {per_page: 5}
ratings: {s: general, q: questionable, e: explicit}
tags: {types: post, namespaces: {artist: creator, copyright: series, character: character, species: species, meta: meta}}
`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(NewClient(m, nil))
	ctx := context.Background()
	page, err := e.Search(ctx, "sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) == 0 {
		t.Fatalf("page: %+v", page)
	}
	p := page.Posts[0]
	t.Logf("total %d; newest %d: %s, %d tags, %d typed, rating %v, posted %v", page.Total, p.ID, p.FileURL, len(p.Tags), len(p.TagTypes), p.Rating, p.Posted)
	if p.FileURL == "" || p.MD5 == [16]byte{} || len(p.Tags) == 0 || len(p.TagTypes) == 0 || p.Posted.IsZero() {
		t.Fatalf("post: %+v", p)
	}
	last := page.Posts[len(page.Posts)-1].ID
	next, err := e.Search(ctx, "sky", last)
	if err != nil || len(next.Posts) == 0 || next.Posts[0].ID >= last {
		t.Fatalf("next page: %+v %v", next, err)
	}
	got, err := e.Lookup(ctx, p.ID)
	if err != nil || got == nil || got.ID != p.ID || got.FileURL != p.FileURL {
		t.Fatalf("lookup: %+v %v", got, err)
	}
}
