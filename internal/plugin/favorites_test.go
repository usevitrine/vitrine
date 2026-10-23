package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFavoritesQuery(t *testing.T) {
	for q, want := range map[string]string{
		"fav:me":    "me",
		" FAV:123 ": "123",
		"https://site.test/index.php?page=favorites&s=view&id=77":                "77",
		"https://site.test/index.php?page=favorites&amp;s=view&amp;id=78&pid=50": "78",
		"fav:me blue_sky":  "",
		"blue_sky":         "",
		"favorited_by=bob": "",
	} {
		got, ok := Favorites(q)
		if got != want || ok != (want != "") {
			t.Errorf("Favorites(%q) = %q, %v; want %q", q, got, ok, want)
		}
	}
	if !IsFavorites("fav:me") || !IsFavorites("favorited_by=bob") || IsFavorites("blue_sky") {
		t.Error("IsFavorites")
	}
}

// A Gelbooru 0.2 site's favorites pages (made up): two pages, then an empty one.
func favPage(posts ...string) string {
	var b strings.Builder
	b.WriteString("<html><body><div class=\"content\">")
	for _, p := range posts {
		id, md5, _ := strings.Cut(p, ":")
		fmt.Fprintf(&b, `<span id="s%[1]s" class="thumb"><a href="index.php?page=post&amp;s=view&amp;id=%[1]s" id="p%[1]s"><img src="https://site.test/thumbnails/1/thumbnail_%[2]s.jpg?%[1]s" alt="x"/></a><br/><a href="#" onclick="remove(%[1]s)">Remove</a></span>`, id, md5)
	}
	b.WriteString(`</div><div id="paginator"><a href="?page=favorites&amp;s=view&amp;id=42&amp;pid=2">2</a></div></body></html>`)
	return b.String()
}

func TestGelbooruFavorites(t *testing.T) {
	var reqs []url.Values
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		reqs = append(reqs, q)
		if q.Get("page") != "favorites" {
			t.Errorf("unexpected request %v", q)
		}
		switch q.Get("pid") {
		case "0":
			io.WriteString(w, favPage("120:00112233445566778899aabbccddeeff", "300:ffeeddccbbaa99887766554433221100"))
		case "2":
			io.WriteString(w, favPage("50:0123456789abcdef0123456789abcdef"))
		default:
			io.WriteString(w, favPage())
		}
	}))
	defer srv.Close()
	m, err := Parse([]byte(`
id: favsite
engine: gelbooru-v02
base: ` + srv.URL + `
post_url: ` + srv.URL + `/index.php?page=post&s=view&id={id}
auth: {fields: [api_key, user_id], required: true}
limits: {requests_per_sec: 100}
search: {per_page: 2}
`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(testClient(m, srv, map[string]string{"api_key": "SECRET", "user_id": "42"}))
	ctx := context.Background()
	page, err := e.Search(ctx, "fav:me", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Posts) != 2 || page.Posts[0].ID != 300 || page.Posts[1].ID != 120 || !page.Posts[0].Partial ||
		page.Posts[0].MD5[0] != 0xff || page.Posts[1].PostURL != srv.URL+"/index.php?page=post&s=view&id=120" {
		t.Fatalf("first page: %+v", page)
	}
	for _, q := range reqs {
		if q.Get("id") != "42" || q.Has("api_key") || q.Has("user_id") {
			t.Fatalf("favorites page asked with %v (no credentials in a web page)", q)
		}
	}
	if len(reqs) != 3 {
		t.Fatalf("%d favorites pages read, want 3 (two, then the empty one)", len(reqs))
	}
	// The next page from the list kept (no new requests), then the end.
	page, err = e.Search(ctx, "fav:me", 120)
	if err != nil || len(page.Posts) != 1 || page.Posts[0].ID != 50 || len(reqs) != 3 {
		t.Fatalf("second page: %+v %v (%d requests)", page, err, len(reqs))
	}
	if page, _ = e.Search(ctx, "fav:me", 50); len(page.Posts) != 0 {
		t.Fatalf("past the end: %+v", page)
	}
	// Someone else's (by ID), and the mistakes.
	if _, err := e.Search(ctx, "fav:7", 0); err != nil || reqs[len(reqs)-1].Get("id") != "7" {
		t.Fatalf("fav:7: %v %v", err, reqs[len(reqs)-1])
	}
	if _, err := e.Search(ctx, "fav:bob", 0); err == nil || !strings.Contains(err.Error(), "ID number") {
		t.Fatalf("fav:bob: %v", err)
	}
	e2, _ := NewEngine(testClient(m, srv, map[string]string{"api_key": "SECRET"}))
	if _, err := e2.Search(ctx, "fav:me", 0); err == nil || !strings.Contains(err.Error(), "user ID") {
		t.Fatalf("fav:me without a user ID: %v", err)
	}
}

func TestShimmieFavorites(t *testing.T) {
	m, err := Parse([]byte("id: shm\nengine: shimmie2\nbase: https://s.test\nauth: {fields: [username, password]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := &shimmie{c: NewClient(m, map[string]string{"username": "some_user"})}
	if q, err := s.favQuery("me"); err != nil || q != "favorited_by=some_user" {
		t.Fatalf("me: %q %v", q, err)
	}
	if q, _ := s.favQuery("bob"); q != "favorited_by=bob" {
		t.Fatalf("bob: %q", q)
	}
	s2 := &shimmie{c: NewClient(m, nil)}
	if _, err := s2.favQuery("me"); err == nil {
		t.Fatal("fav:me without an account")
	}
}

func TestMastodonFavorites(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer T0K3N" || r.URL.Path != "/api/v1/favourites" {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"The access token is invalid"}`)
			return
		}
		status := func(id string) string {
			return `{"id":"` + id + `","url":"https://m.test/@a/` + id + `","account":{"acct":"some_artist"},"tags":[{"name":"Blue_Sky"}],` +
				`"media_attachments":[{"type":"image","url":"https://m.test/` + id + `.png","meta":{"original":{"width":10,"height":20}}}]}`
		}
		switch r.URL.Query().Get("max_id") {
		case "":
			w.Header().Set("Link", `<`+srv.URL+`/api/v1/favourites?limit=40&max_id=900>; rel="next", <`+srv.URL+`/api/v1/favourites?min_id=999>; rel="prev"`)
			io.WriteString(w, "["+status("500")+","+status("700")+"]")
		case "900":
			w.Header().Set("Link", `<`+srv.URL+`/api/v1/favourites?limit=40&max_id=800>; rel="next"`)
			io.WriteString(w, "["+status("100")+"]")
		default:
			io.WriteString(w, "[]")
		}
	}))
	defer srv.Close()
	m, err := Parse([]byte(`
id: masto
engine: mastodon
base: ` + srv.URL + `
auth: {fields: [access_token], login: bearer}
limits: {requests_per_sec: 100}
`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := NewEngine(testClient(m, srv, map[string]string{"access_token": "T0K3N"}))
	page, err := e.Search(context.Background(), "fav:me", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Posts) != 3 || page.Posts[0].ID != 7000 || page.Posts[2].ID != 1000 ||
		page.Posts[0].TagTypes["some_artist"] != "artist" {
		t.Fatalf("favourites: %+v", page)
	}
	if _, err := e.Search(context.Background(), "fav:bob", 0); err == nil || !strings.Contains(err.Error(), "only your own") {
		t.Fatalf("fav:bob: %v", err)
	}
	e2, _ := NewEngine(testClient(m, srv, nil))
	if _, err := e2.Search(context.Background(), "fav:me", 0); err == nil || !strings.Contains(err.Error(), "access token") {
		t.Fatalf("without a token: %v", err)
	}
}
