package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeMastodon serves statuses 30 (two images, tags blue_sky), 20 (a boost,
// skipped), 10 (a video and an audio file, tag sunset) - by some_artist.
func fakeMastodon(t *testing.T, token string) (Manifest, *httptest.Server, *[]*http.Request) {
	t.Helper()
	var reqs []*http.Request
	status := func(id, tags string, media []map[string]any, reblog bool) map[string]any {
		var ts []map[string]string
		for _, n := range strings.Fields(tags) {
			ts = append(ts, map[string]string{"name": n})
		}
		st := map[string]any{"id": id, "created_at": "2026-10-14T17:06:12.365Z", "sensitive": true, "url": "https://example.test/@Some_Artist/" + id,
			"account": map[string]string{"acct": "Some_Artist"}, "tags": ts, "media_attachments": media}
		if reblog {
			st["reblog"] = map[string]any{"id": "5"}
		}
		return st
	}
	img := func(n string) map[string]any {
		return map[string]any{"type": "image", "url": "https://media.SITE/" + n + ".png", "meta": map[string]any{"original": map[string]int{"width": 800, "height": 600}}}
	}
	all := []map[string]any{
		status("30", "Blue_Sky art", []map[string]any{img("a"), img("b")}, false),
		status("20", "blue_sky", []map[string]any{img("c")}, true),
		status("10", "sunset", []map[string]any{{"type": "video", "url": "https://media.SITE/v.mp4"}, {"type": "audio", "url": "https://media.SITE/x.mp3"}}, false),
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r)
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("request without the token: %s", r.URL)
		}
		host := r.Host
		write := func(v any) {
			b, _ := json.Marshal(v)
			w.Write([]byte(strings.ReplaceAll(string(b), "media.SITE", host)))
		}
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/v1/accounts/lookup":
			if strings.EqualFold(q.Get("acct"), "some_artist") {
				write(map[string]string{"id": "77"})
				return
			}
			w.WriteHeader(404)
			write(map[string]string{"error": "Record not found"})
		case r.URL.Path == "/api/v1/accounts/77/statuses" || strings.HasPrefix(r.URL.Path, "/api/v1/timelines/tag/"):
			var out []map[string]any
			for _, st := range all {
				if max := q.Get("max_id"); max != "" && st["id"].(string) >= max {
					continue
				}
				out = append(out, st)
			}
			write(out)
		case r.URL.Path == "/api/v1/statuses/30":
			write(all[0])
		default:
			w.WriteHeader(404)
			write(map[string]string{"error": "Record not found"})
		}
	}))
	t.Cleanup(srv.Close)
	auth := ""
	if token != "" {
		auth = "auth: {fields: [access_token], login: bearer}\n"
	}
	m, err := Parse([]byte(`
id: masto_test
engine: mastodon
base: ` + srv.URL + `
post_url: ` + srv.URL + `/web/statuses/{id}
` + auth + `limits: {requests_per_sec: 100}
ratings: {sensitive: explicit, safe: general}
tags: {types: page, namespaces: {artist: creator}}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, srv, &reqs
}

func TestMastodonQuery(t *testing.T) {
	for in, want := range map[string]string{
		"art":                                     "{ [art] [] []}",
		"#Art furry ~cute ~cool -gore":            "{ [art furry] [cute cool] [gore]}",
		"@Some_Artist":                            "{Some_Artist [] [] []}",
		"@someone@other.test blue_sky":            "{someone@other.test [blue_sky] [] []}",
		"https://example.test/tags/art":           "{ [art] [] []}",
		"https://example.test/@Some_Artist/media": "{Some_Artist [] [] []}",
		"#art #furry ~#cute -#gore @Some_Artist":  "{Some_Artist [art furry] [cute] [gore]}",
		"~a ~b":                                   "{ [a] [b] []}",
	} {
		mq, err := parseMastoQuery(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := fmtQuery(mq); got != want {
			t.Errorf("%q -> %s, want %s", in, got, want)
		}
	}
	if _, err := parseMastoQuery("-gore"); err == nil {
		t.Error("a search with only an exclusion passed")
	}
}

func fmtQuery(mq mastoQuery) string {
	j := func(l []string) string { return "[" + strings.Join(l, " ") + "]" }
	return "{" + mq.account + " " + j(mq.tags) + " " + j(mq.any) + " " + j(mq.none) + "}"
}

func TestMastodonTagSearch(t *testing.T) {
	m, srv, reqs := fakeMastodon(t, "")
	e, _ := NewEngine(testClient(m, srv, nil))
	page, err := e.Search(context.Background(), "blue_sky art ~cute -gore", 0)
	if err != nil {
		t.Fatal(err)
	}
	// Status 30: two files, its last first (IDs 301, 300: always going down, as the resume point needs); the
	// boost is skipped; status 10: its video only.
	var ids []int64
	for _, p := range page.Posts {
		ids = append(ids, p.ID)
	}
	if len(ids) != 3 || ids[0] != 301 || ids[1] != 300 || ids[2] != 100 {
		t.Fatalf("ids %v", ids)
	}
	p := page.Posts[1]
	if strings.Join(p.Tags, " ") != "blue_sky art some_artist" || p.TagTypes["some_artist"] != "artist" || p.Width != 800 ||
		p.Rating != m.Rating("sensitive") || p.PostURL != "https://example.test/@Some_Artist/30" || p.Posted.IsZero() {
		t.Fatalf("post %+v", p)
	}
	if m.Namespace(p.TagTypes["some_artist"]) != "creator" {
		t.Fatalf("the author isn't a creator: %q", m.Namespace(p.TagTypes["some_artist"]))
	}
	q := (*reqs)[0].URL
	if q.Path != "/api/v1/timelines/tag/blue_sky" || q.Query().Get("local") != "true" || q.Query().Get("all[]") != "art" ||
		q.Query().Get("any[]") != "cute" || q.Query().Get("none[]") != "gore" || q.Query().Get("only_media") != "true" {
		t.Fatalf("request %s", q)
	}
	// Resuming below file 300 asks for statuses older than 30.
	if _, err := e.Search(context.Background(), "blue_sky", 300); err != nil {
		t.Fatal(err)
	}
	if got := (*reqs)[len(*reqs)-1].URL.Query().Get("max_id"); got != "30" {
		t.Fatalf("max_id %q, want 30", got)
	}
	// Resuming below file 301 (stopped part-way through status 30: its file 300 still to come) asks for status 30
	// again - before, it asked for older ones and file 300 was skipped for good.
	if _, err := e.Search(context.Background(), "blue_sky", 301); err != nil {
		t.Fatal(err)
	}
	if got := (*reqs)[len(*reqs)-1].URL.Query().Get("max_id"); got != "31" {
		t.Fatalf("max_id %q, want 31", got)
	}
}

func TestMastodonProfile(t *testing.T) {
	m, srv, reqs := fakeMastodon(t, "")
	e, _ := NewEngine(testClient(m, srv, nil))
	ctx := context.Background()

	// A profile and one of its hashtags: the API filters by the first; the
	// rest is checked here (only status 10 has sunset).
	page, err := e.Search(ctx, "@Some_Artist sunset", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 || page.Posts[0].ID != 100 || !strings.HasSuffix(page.Posts[0].FileURL, "/v.mp4") {
		t.Fatalf("posts %+v", page.Posts)
	}
	var statuses *url.URL
	for _, r := range *reqs {
		if strings.HasSuffix(r.URL.Path, "/statuses") {
			statuses = r.URL
		}
	}
	if statuses == nil || statuses.Query().Get("exclude_reblogs") != "true" || statuses.Query().Get("tagged") != "sunset" {
		t.Fatalf("statuses request %v", statuses)
	}
	if _, err := e.Search(ctx, "@nobody", 0); err == nil || !strings.Contains(err.Error(), "no profile @nobody") {
		t.Fatalf("unknown profile: %v", err)
	}
	// Lookup: one file of a status; a gone status or file is nil.
	p, err := e.Lookup(ctx, 301)
	if err != nil || p == nil || !strings.HasSuffix(p.FileURL, "/b.png") || p.TagTypes["some_artist"] != "artist" {
		t.Fatalf("lookup 301: %+v %v", p, err)
	}
	if p, err := e.Lookup(ctx, 309); err != nil || p != nil {
		t.Fatalf("lookup 309: %+v %v", p, err)
	}
	if p, err := e.Lookup(ctx, 990); err != nil || p != nil {
		t.Fatalf("lookup gone: %+v %v", p, err)
	}
}

func TestMastodonToken(t *testing.T) {
	m, srv, reqs := fakeMastodon(t, "T0K3N")
	e, _ := NewEngine(testClient(m, srv, map[string]string{"access_token": "T0K3N"}))
	if _, err := e.Search(context.Background(), "blue_sky", 0); err != nil {
		t.Fatal(err)
	}
	q := (*reqs)[0].URL
	if q.Query().Get("local") != "" || strings.Contains(q.RawQuery, "T0K3N") {
		t.Fatalf("with a token: %s", q)
	}
}
