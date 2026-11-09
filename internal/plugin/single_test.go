package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func singleManifest(engine, base, post string) Manifest {
	m := Manifest{ID: "x", Site: "example.test", Engine: engine, Base: base, PostURL: post, Domains: []string{"example.test"}}
	m.Limits.RequestsPerSec = 1000
	m.Limits.Connections = 1
	return m
}

func TestSinglePost(t *testing.T) {
	gel := singleManifest("gelbooru-v02", "https://api.example.test", "https://example.test/index.php?page=post&s=view&id={id}")
	html := singleManifest("gelbooru-v02-html", "https://example.test", "https://example.test/index.php?page=post&s=view&id={id}")
	shm := singleManifest("shimmie2", "https://example.test", "https://example.test/post/view/{id}")
	dan := singleManifest("danbooru", "https://one.example.test", "https://one.example.test/posts/{id}")
	mast := singleManifest("mastodon", "https://example.test", "")
	eh := singleManifest("ehentai", "https://e-hentai.org", "https://e-hentai.org/g/{id}/")
	for _, c := range []struct {
		name   string
		m      Manifest
		q      string
		id     int64
		single bool
		err    string
	}{
		{"id", gel, "id:123", 123, true, ""},
		{"id, any case and spaces", gel, "  ID:123 ", 123, true, ""},
		{"link with extra query", gel, "https://example.test/index.php?page=post&s=view&id=19293757&tags=rating%3Aquestionable+", 19293757, true, ""},
		{"www link", gel, "https://www.example.test/index.php?id=5&page=post&s=view", 5, true, ""},
		{"html engine", html, "https://example.test/index.php?page=post&s=view&id=7", 7, true, ""},
		{"a list page", gel, "https://example.test/index.php?page=post&s=list&tags=blue_sky", 0, false, "isn't a post"},
		{"favorites stay a search", gel, "https://example.test/index.php?page=favorites&s=view&id=9", 0, false, ""},
		{"wrong site", gel, "https://other.test/index.php?page=post&s=view&id=7", 0, false, "that link is for other.test, not example.test"},
		{"a search", gel, "blue_sky", 0, false, ""},
		{"id with tags stays a search", gel, "id:5 blue_sky", 0, false, ""},
		{"resume filter stays a search", gel, "id:<5", 0, false, ""},
		{"zero", gel, "id:0", 0, false, "isn't a post ID"},
		{"too big", gel, "id:99999999999999999999", 0, false, "isn't a post ID"},
		{"shimmie", shm, "https://example.test/post/view/42?search=blue_sky", 42, true, ""},
		{"shimmie list", shm, "https://example.test/post/list/blue_sky/1", 0, false, "isn't a post"},
		{"danbooru", dan, "https://one.example.test/posts/8?q=blue_sky", 8, true, ""},
		{"danbooru's sister site", dan, "https://two.example.test/posts/8", 0, false, "that link is for two.example.test"},
		{"mastodon post", mast, "https://example.test/@some_artist/1234567", 1234567, true, ""},
		{"mastodon web", mast, "https://example.test/web/statuses/99", 99, true, ""},
		{"mastodon id", mast, "id:99", 99, true, ""},
		{"mastodon profile stays a search", mast, "https://example.test/@some_artist", 0, false, ""},
		{"mastodon tag stays a search", mast, "https://example.test/tags/blue_sky", 0, false, ""},
		{"mastodon other server", mast, "https://other.test/@a/1", 0, false, "that link is for other.test"},
		{"gallery link is the engine's", eh, "https://e-hentai.org/g/1/abcdef0123/", 0, false, ""},
		{"exhentai link on e-hentai", eh, "https://exhentai.org/g/1/abcdef0123/", 0, false, ""},
		{"gallery by id", eh, "id:1", 0, false, "by its link"},
		{"gallery site, wrong host", eh, "https://other.test/g/1/abcdef0123/", 0, false, "that link is for other.test"},
	} {
		t.Run(c.name, func(t *testing.T) {
			id, single, err := SinglePost(c.m, c.q)
			if c.err != "" {
				if !IsPostRef(err) || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil || id != c.id || single != c.single {
				t.Fatalf("got %d %v %v, want %d %v", id, single, err, c.id, c.single)
			}
		})
	}
}

// fakeEngine answers Lookup for the IDs it has.
type fakeEngine struct {
	has   map[int64]bool
	calls int
}

func (f *fakeEngine) Search(context.Context, string, int64) (Page, error) { return Page{}, nil }
func (f *fakeEngine) Lookup(_ context.Context, id int64) (*Post, error) {
	f.calls++
	if !f.has[id] {
		return nil, nil
	}
	return &Post{ID: id, FileURL: "https://example.test/f"}, nil
}

func TestFetchSingle(t *testing.T) {
	m := singleManifest("shimmie2", "https://example.test", "")
	ctx := context.Background()
	e := &fakeEngine{has: map[int64]bool{5: true}}
	page, err := FetchSingle(ctx, m, e, 5, 0)
	if err != nil || len(page.Posts) != 1 || page.Total != 1 || page.Posts[0].ID != 5 {
		t.Fatalf("%+v %v", page, err)
	}
	calls := e.calls
	if page, err := FetchSingle(ctx, m, e, 5, 5); err != nil || len(page.Posts) != 0 || e.calls != calls {
		t.Fatalf("resumed past the post: %+v %v, %d calls", page, err, e.calls)
	}
	_, err = FetchSingle(ctx, m, e, 6, 0)
	if !IsNoPost(err) || !strings.Contains(err.Error(), "example.test has no post 6") {
		t.Fatalf("err = %v", err)
	}
}

// A site's placeholder post (an ID echoed back, no file) is no post.
func TestGelbooruSinglePlaceholder(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<posts count="1"><post id="9" file_url="https://example.test/images//" md5="" tags=""/></posts>`))
	}))
	defer srv.Close()
	m := singleManifest("gelbooru-v02", srv.URL, "")
	m.Domains = []string{"127.0.0.1"}
	m.Search.PerPage = 10
	c := NewClient(m, nil)
	c.SetHTTPClient(srv.Client())
	e, _ := NewEngine(c)
	if _, err := FetchSingle(context.Background(), m, e, 9, 0); !IsNoPost(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestMastodonSingle(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/statuses/77" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"id":"77","url":"https://example.test/@a/77","account":{"acct":"a"},"tags":[{"name":"blue_sky"}],
			"media_attachments":[{"type":"image","url":"https://example.test/1.png"},{"type":"image","url":"https://example.test/2.png"}]}`))
	}))
	defer srv.Close()
	m := singleManifest("mastodon", srv.URL, "")
	m.Domains = []string{"127.0.0.1"}
	c := NewClient(m, nil)
	c.SetHTTPClient(srv.Client())
	e, _ := NewEngine(c)
	ctx := context.Background()
	page, err := FetchSingle(ctx, m, e, 77, 0)
	if err != nil || len(page.Posts) != 2 || page.Posts[0].ID != 771 || page.Posts[1].ID != 770 {
		t.Fatalf("%+v %v", page, err)
	}
	// Resumed part-way through the post: its remaining file.
	if page, err = FetchSingle(ctx, m, e, 77, 771); err != nil || len(page.Posts) != 1 || page.Posts[0].ID != 770 {
		t.Fatalf("%+v %v", page, err)
	}
	if _, err = FetchSingle(ctx, m, e, 78, 0); !IsNoPost(err) {
		t.Fatalf("err = %v", err)
	}
}
