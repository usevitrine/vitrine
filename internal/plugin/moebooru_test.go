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

// A made-up Moebooru post (fileURL "" = the site hides its file).
func moePost(id int64, fileURL string) string {
	return fmt.Sprintf(`{"id":%d,"tags":"blue_sky some_artist a_series some_character","created_at":1794827516,"rating":"q",`+
		`"width":800,"height":600,"md5":"00112233445566778899aabbccddeeff","file_url":%q,"source":"https://example.test/art/%d"}`, id, fileURL, id)
}

func moePage(posts ...string) string {
	return `{"posts":[` + strings.Join(posts, ",") + `],"tags":{"blue_sky":"general","some_artist":"artist","a_series":"copyright","some_character":"character"}}`
}

func fakeMoebooru(t *testing.T) (Manifest, *httptest.Server, *[]*http.Request) {
	t.Helper()
	var reqs []*http.Request
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r)
		file := func(id int64) string { return fmt.Sprintf("%s/image/%d.jpg", srv.URL, id) }
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/post.xml":
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<posts count="2" offset="0"><post id="299"/></posts>`)
		case r.URL.Path == "/post.json" && q.Get("api_version") == "2" && q.Get("include_tags") == "1":
			switch q.Get("tags") {
			case "blue_sky", "vote:3:some_user":
				io.WriteString(w, moePage(moePost(300, ""), moePost(299, file(299)), moePost(298, file(298))))
			case "blue_sky id:<500":
				io.WriteString(w, moePage(moePost(450, ""))) // every file hidden: passed over
			case "blue_sky id:<450":
				io.WriteString(w, moePage(moePost(449, file(449))))
			case "id:299":
				io.WriteString(w, moePage(moePost(299, file(299))))
			default:
				io.WriteString(w, `{"posts":[],"tags":{}}`)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte(`
id: moetest
engine: moebooru
base: ` + srv.URL + `
post_url: ` + srv.URL + `/post/show/{id}
limits: {requests_per_sec: 100}
search: {per_page: 1000}
ratings: {s: general, q: questionable, e: explicit}
tags: {types: post, namespaces: {artist: creator, copyright: series, character: character}}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, srv, &reqs
}

func TestMoebooruSearch(t *testing.T) {
	m, srv, reqs := fakeMoebooru(t)
	e, _ := NewEngine(testClient(m, srv, nil))
	ctx := context.Background()
	page, err := e.Search(ctx, "blue_sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Posts) != 2 || page.Posts[0].ID != 299 || page.Posts[1].ID != 298 {
		t.Fatalf("page (post 300's file is hidden): %+v", page)
	}
	p := page.Posts[0]
	if p.Rating != media.RatingQuestionable || p.MD5[0] != 0x00 || p.MD5[15] != 0xff || p.Width != 800 || p.Height != 600 ||
		p.PostURL != srv.URL+"/post/show/299" || len(p.Sources) != 1 || len(p.Tags) != 4 {
		t.Fatalf("post: %+v", p)
	}
	if want := time.Unix(1794827516, 0); !p.Posted.Equal(want) {
		t.Fatalf("posted %v, want %v", p.Posted, want)
	}
	if p.TagTypes["some_artist"] != "artist" || p.TagTypes["a_series"] != "copyright" || p.TagTypes["some_character"] != "character" ||
		p.TagTypes["blue_sky"] != "general" {
		t.Fatalf("tag types: %v", p.TagTypes)
	}
	var list *http.Request
	for _, r := range *reqs {
		if r.URL.Path == "/post.json" {
			list = r
		}
	}
	if q := list.URL.Query(); q.Get("tags") != "blue_sky" || q.Get("limit") != "1000" {
		t.Fatalf("list query: %v", q)
	}

	// Resuming: id:<N added to the search; the end is an empty page.
	if page, err = e.Search(ctx, "blue_sky", 298); err != nil || len(page.Posts) != 0 {
		t.Fatalf("past the end: %+v %v", page, err)
	}
	if q := (*reqs)[len(*reqs)-1].URL.Query(); q.Get("tags") != "blue_sky id:<298" {
		t.Fatalf("resume query: %v", q)
	}
	// A page of hidden files only is passed over, not taken as the end.
	if page, _ = e.Search(ctx, "blue_sky", 500); len(page.Posts) != 1 || page.Posts[0].ID != 449 {
		t.Fatalf("over a hidden page: %+v", page)
	}

	// Favorites: the site's vote:3:<name>; fav:me can't be known without signing in.
	if page, _ = e.Search(ctx, "fav:some_user", 0); len(page.Posts) != 2 {
		t.Fatalf("favorites: %+v", page)
	}
	if _, err := e.Search(ctx, "fav:me", 0); err == nil || !strings.Contains(err.Error(), "fav:<your user name>") {
		t.Fatalf("fav:me: %v", err)
	}
}

func TestMoebooruLookup(t *testing.T) {
	m, srv, _ := fakeMoebooru(t)
	e, _ := NewEngine(testClient(m, srv, nil))
	p, err := e.Lookup(context.Background(), 299)
	if err != nil || p == nil || p.ID != 299 || p.TagTypes["some_artist"] != "artist" {
		t.Fatalf("lookup: %+v %v", p, err)
	}
	if p, err := e.Lookup(context.Background(), 7); p != nil || err != nil {
		t.Fatalf("a missing post: %+v %v", p, err)
	}
}

func TestMoebooruManifest(t *testing.T) {
	m, _, _ := fakeMoebooru(t)
	if tmpl, sep := m.OrQuery(); tmpl != "~{tags}" || sep != " ~" {
		t.Fatalf("OR search: %q %q", tmpl, sep)
	}
	if by, me := FavoritesBy(m, nil); by != "name" || me {
		t.Fatalf("favorites: %q %v", by, me)
	}
}

// Against a real Moebooru site (konachan.net, its safe-for-work side, no account):
// only with VITRINE_LIVE=1 - the other tests never leave the machine.
func TestMoebooruLive(t *testing.T) {
	if os.Getenv("VITRINE_LIVE") != "1" {
		t.Skip("set VITRINE_LIVE=1 to ask the real site")
	}
	m, err := Parse([]byte(`
id: konachan-net
engine: moebooru
base: https://konachan.net
post_url: https://konachan.net/post/show/{id}
limits: {requests_per_sec: 1}
search: {per_page: 5}
ratings: {s: general, q: questionable, e: explicit}
tags: {types: post, namespaces: {artist: creator, copyright: series, character: character}}
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
	if len(page.Posts) == 0 || page.Total <= 0 {
		t.Fatalf("page: %+v", page)
	}
	p := page.Posts[0]
	t.Logf("%d posts on the site; newest %d: %s, %d tags, %d typed, rating %v, posted %v", page.Total, p.ID, p.FileURL, len(p.Tags), len(p.TagTypes), p.Rating, p.Posted)
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
