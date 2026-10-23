package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// A made-up Danbooru post (fileURL "" = the site hides its file).
func dbPost(id int64, fileURL string) string {
	return fmt.Sprintf(`{"id":%d,"created_at":"2026-10-14T13:00:17.123-04:00","rating":"q","image_width":800,"image_height":600,`+
		`"md5":"00112233445566778899aabbccddeeff","file_url":%q,"source":"https://example.test/art/%d",`+
		`"tag_string":"blue_sky some_artist a_series some_character highres",`+
		`"tag_string_general":"blue_sky","tag_string_artist":"some_artist","tag_string_copyright":"a_series",`+
		`"tag_string_character":"some_character","tag_string_meta":"highres"}`, id, fileURL, id)
}

func fakeDanbooru(t *testing.T) (Manifest, *httptest.Server, *[]*http.Request) {
	t.Helper()
	var reqs []*http.Request
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r)
		file := func(id int64) string { return fmt.Sprintf("%s/original/%d.jpg", srv.URL, id) }
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/counts/posts.json":
			io.WriteString(w, `{"counts":{"posts":2}}`)
		case r.URL.Path == "/posts/299.json":
			io.WriteString(w, dbPost(299, file(299)))
		case strings.HasPrefix(r.URL.Path, "/posts/"):
			w.WriteHeader(404)
			io.WriteString(w, `{"success":false,"message":"That record was not found."}`)
		case r.URL.Path == "/posts.json" && strings.Count(q.Get("tags"), " ") >= 2:
			w.WriteHeader(422)
			io.WriteString(w, `{"success":false,"message":"You cannot search for more than 2 tags at a time."}`)
		case r.URL.Path == "/posts.json":
			switch q.Get("page") {
			case "":
				io.WriteString(w, "["+dbPost(300, "")+","+dbPost(299, file(299))+","+dbPost(298, file(298))+"]")
			case "b500":
				io.WriteString(w, "["+dbPost(450, "")+"]") // every file hidden: passed over
			case "b450":
				io.WriteString(w, "["+dbPost(449, file(449))+"]")
			default:
				io.WriteString(w, "[]")
			}
		}
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte(`
id: dbtest
engine: danbooru
base: ` + srv.URL + `
post_url: ` + srv.URL + `/posts/{id}
auth: {fields: [login, api_key]}
limits: {requests_per_sec: 100}
search: {per_page: 200}
ratings: {g: general, s: sensitive, q: questionable, e: explicit}
tags: {types: post, namespaces: {artist: creator, copyright: series, character: character, meta: meta}}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, srv, &reqs
}

func TestDanbooruSearch(t *testing.T) {
	m, srv, reqs := fakeDanbooru(t)
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
		p.PostURL != srv.URL+"/posts/299" || len(p.Sources) != 1 || len(p.Tags) != 5 {
		t.Fatalf("post: %+v", p)
	}
	if want := time.Date(2026, 10, 14, 17, 0, 17, 123e6, time.UTC); !p.Posted.Equal(want) {
		t.Fatalf("posted %v, want %v", p.Posted, want)
	}
	if p.TagTypes["some_artist"] != "artist" || p.TagTypes["a_series"] != "copyright" || p.TagTypes["some_character"] != "character" ||
		p.TagTypes["highres"] != "meta" || p.TagTypes["blue_sky"] != "general" {
		t.Fatalf("tag types: %v", p.TagTypes)
	}
	var list *http.Request
	for _, r := range *reqs {
		if r.URL.Path == "/posts.json" {
			list = r
		}
	}
	if q := list.URL.Query(); q.Get("tags") != "blue_sky" || q.Get("limit") != "200" || q.Get("login") != "some_user" || q.Get("api_key") != "S3CRET" || q.Has("page") {
		t.Fatalf("list query: %v", q)
	}

	// Resuming: the site's own page=b<ID>; the end is an empty page.
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

	// Favorites: the site's fav:<name> (fav:me = the login).
	e.Search(ctx, "fav:me", 0)
	if q := (*reqs)[len(*reqs)-1].URL.Query(); q.Get("tags") != "fav:some_user" {
		t.Fatalf("fav:me query: %v", q)
	}
	anon, _ := NewEngine(testClient(m, srv, nil))
	if _, err := anon.Search(ctx, "fav:me", 0); err == nil || !strings.Contains(err.Error(), "account") {
		t.Fatalf("fav:me without an account: %v", err)
	}
	// Too many tags: the site's refusal, explained.
	if _, err := anon.Search(ctx, "a b c", 0); err == nil || !strings.Contains(err.Error(), "two tags") {
		t.Fatalf("three tags: %v", err)
	}
}

func TestDanbooruLookup(t *testing.T) {
	m, srv, reqs := fakeDanbooru(t)
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

func TestDanbooruManifest(t *testing.T) {
	m, _, _ := fakeDanbooru(t)
	if tmpl, sep := m.OrQuery(); tmpl != "~{tags}" || sep != " ~" {
		t.Fatalf("OR search: %q %q", tmpl, sep)
	}
	if by, me := FavoritesBy(m, map[string]bool{"login": true}); by != "name" || !me {
		t.Fatalf("favorites: %q %v", by, me)
	}
	if _, err := url.Parse(m.Post(1)); err != nil {
		t.Fatal(err)
	}
}

// Against a real Danbooru site (safebooru.donmai.us, safe for work, no account):
// only with VITRINE_LIVE=1 - the other tests never leave the machine.
func TestDanbooruLive(t *testing.T) {
	if os.Getenv("VITRINE_LIVE") != "1" {
		t.Skip("set VITRINE_LIVE=1 to ask the real site")
	}
	m, err := Parse([]byte(`
id: safebooru-donmai
engine: danbooru
base: https://safebooru.donmai.us
post_url: https://safebooru.donmai.us/posts/{id}
limits: {requests_per_sec: 1}
search: {per_page: 5}
ratings: {g: general, s: sensitive, q: questionable, e: explicit}
tags: {types: post, namespaces: {artist: creator, copyright: series, character: character, meta: meta}}
`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(NewClient(m, nil))
	ctx := context.Background()
	page, err := e.Search(ctx, "blue_sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) == 0 || page.Total <= 0 {
		t.Fatalf("page: %+v", page)
	}
	p := page.Posts[0]
	t.Logf("%d posts on the site; newest %d: %s, %d tags, %d typed, rating %v, posted %v", page.Total, p.ID, p.FileURL, len(p.Tags), len(p.TagTypes), p.Rating, p.Posted)
	if p.FileURL == "" || p.MD5 == [16]byte{} || len(p.Tags) == 0 || len(p.TagTypes) == 0 || p.Posted.IsZero() {
		t.Fatalf("post: %+v", p)
	}
	next, err := e.Search(ctx, "blue_sky", page.Posts[len(page.Posts)-1].ID)
	if err != nil || len(next.Posts) == 0 || next.Posts[0].ID >= page.Posts[len(page.Posts)-1].ID {
		t.Fatalf("next page: %+v %v", next, err)
	}
	got, err := e.Lookup(ctx, p.ID)
	if err != nil || got == nil || got.ID != p.ID || got.FileURL != p.FileURL {
		t.Fatalf("lookup: %+v %v", got, err)
	}
}
