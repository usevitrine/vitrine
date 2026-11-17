package download

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/store"
)

// A RedGIFs post can't be looked up by its number: a refresh asks for the clip behind the post link the file keeps.
func TestRedgifsRefreshByLink(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	extra := ""
	var asked []string
	var srv *httptest.Server
	clip := func(i int) string {
		return fmt.Sprintf(`{"id":"clip%c","createDate":%d,"type":2,"width":8,"height":8,"tags":["Blue Sky"%s],"userName":"some_artist",`+
			`"urls":{"hd":"%s/file/%d.png"}}`, 'a'+i, 1794900000-60*i, extra, srv.URL, i)
	}
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, r.URL.Path)
		switch {
		case r.URL.Path == "/v2/auth/temporary":
			io.WriteString(w, `{"token":"tok"}`)
		case strings.HasPrefix(r.URL.Path, "/file/"):
			var i int64
			fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/file/"), "%d.png", &i)
			w.Write(makePNG(i))
		case r.URL.Path == "/v2/gifs/clipb":
			io.WriteString(w, `{"gif":`+clip(1)+`}`)
		case r.URL.Path == "/v2/gifs/search":
			fmt.Fprintf(w, `{"gifs":[%s,%s],"page":1,"pages":1,"total":2}`, clip(0), clip(1))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, _ := store.Open(filepath.Join(dir, "files"))
	lib, err := library.Open(ctx, library.Deps{DB: d, Store: s})
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.Parse([]byte("id: rg\nengine: redgifs\nbase: " + srv.URL + "\nlimits: {requests_per_sec: 1000}\n" +
		"ratings: {\"\": explicit}\ntags: {types: post, namespaces: {artist: creator}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := New(ctx, lib, d, map[string]plugin.Manifest{"rg": m}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	dl.Site("rg").Client.SetHTTPClient(srv.Client())
	e := &env{lib: lib, db: d, dl: dl}
	if _, err := dl.Add(ctx, "rg", "blue_sky", 0); err != nil {
		t.Fatal(err)
	}
	if err := dl.RunQueue(ctx, "rg"); err != nil {
		t.Fatal(err)
	}
	if c := e.count(t, "blue_sky"); c != 2 {
		t.Fatalf("downloaded: %d", c)
	}
	if c := e.count(t, "creator:some_artist"); c != 2 {
		t.Fatalf("creator: %d", c)
	}

	// The site retags clip b: "Refresh tags" on its file asks for clip b by its link.
	mu.Lock()
	extra = `,"Big Smile"`
	asked = nil
	mu.Unlock()
	ids := lib.Index().Search(mustParse(t, "blue_sky"), dl.now()).IDs
	var fileB uint32
	for _, id := range ids {
		urls, _ := lib.FileURLs(ctx, id)
		for _, u := range urls {
			if u.Kind == library.URLPost && strings.HasSuffix(u.URL, "/watch/clipb") {
				fileB = id
			}
		}
	}
	if fileB == 0 {
		t.Fatal("no file with clip b's link")
	}
	if found, _, err := dl.RefreshFile(ctx, fileB); err != nil || found != 1 {
		t.Fatalf("refresh: %d %v", found, err)
	}
	if c := e.count(t, "big_smile"); c != 1 {
		t.Fatalf("big_smile after the refresh: %d (asked %v)", c, asked)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(asked, "/v2/gifs/clipb") {
		t.Fatalf("asked %v", asked)
	}
}
