package download

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/store"
)

// A site whose list pages give only IDs (HTML): while a file downloads, the next post's page is opened - each post's
// page no longer waits for the file before it. A post seen before has no page opened ahead.
func TestPostPagesOpenedAhead(t *testing.T) {
	ctx := context.Background()
	files := map[int64][]byte{}
	sums := map[int64]string{}
	for id := int64(1); id <= 4; id++ {
		files[id] = makePNG(3000 + id)
		s := md5.Sum(files[id])
		sums[id] = hex.EncodeToString(s[:])
	}
	var mu sync.Mutex
	type span struct{ from, to time.Time }
	fileAt := map[int64]span{}
	pageAt := map[int64]time.Time{}
	idBelow := regexp.MustCompile(`id:<(\d+)`)
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if strings.HasPrefix(r.URL.Path, "/images/") {
			id, _ := strconv.ParseInt(strings.SplitN(strings.TrimPrefix(r.URL.Path, "/images/"), ".", 2)[0], 10, 64)
			from := time.Now()
			time.Sleep(300 * time.Millisecond) // (a file takes a while)
			w.Header().Set("Content-Type", "image/png")
			w.Write(files[id])
			mu.Lock()
			fileAt[id] = span{from, time.Now()}
			mu.Unlock()
			return
		}
		switch q.Get("s") {
		case "list":
			below := int64(5)
			if m := idBelow.FindStringSubmatch(q.Get("tags")); m != nil {
				below, _ = strconv.ParseInt(m[1], 10, 64)
			}
			var sb strings.Builder
			for id := below - 1; id >= 1; id-- {
				fmt.Fprintf(&sb, `<div class="col thumb"><a id="p%d" href="%s/index.php?page=post&amp;s=view&amp;id=%d"><img src="%s/thumbnails/x/thumbnail_%s.jpg" title="t"/></a></div>`,
					id, srv.URL, id, srv.URL, sums[id])
			}
			io.WriteString(w, "<html><body>"+sb.String()+"</body></html>")
		case "view":
			id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
			mu.Lock()
			pageAt[id] = time.Now()
			mu.Unlock()
			fmt.Fprintf(w, `<html><body><div id="tagLink"><a class="tag-type-general" href="index.php?page=post&amp;s=list&amp;tags=common">c</a></div>
<img id="image" src="%s//images/%d.png" /><a href="%s//images/%d.png">Original</a></body></html>`, srv.URL, id, srv.URL, id)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	d, _ := db.Open(ctx, filepath.Join(dir, "meta.db"))
	defer d.Close()
	s, _ := store.Open(filepath.Join(dir, "files"))
	lib, err := library.Open(ctx, library.Deps{DB: d, Store: s})
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.Parse([]byte("id: html\nengine: gelbooru-v02-html\nbase: " + srv.URL +
		"\npost_url: https://html.test/post/{id}\nlimits: {requests_per_sec: 1000, connections: 1}\nratings: {\"\": explicit}\n"))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := New(ctx, lib, d, map[string]plugin.Manifest{"html": m}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	dl.Site("html").Client.SetHTTPClient(srv.Client())
	// Post 3's file is in the library already: its page is opened (its tags) but it has no file to wait for.
	if _, err := lib.Import(ctx, strings.NewReader(string(files[3]))); err != nil {
		t.Fatal(err)
	}
	id, _ := dl.Add(ctx, "html", "common", 0)
	if err := dl.RunQueue(ctx, "html"); err != nil {
		t.Fatal(err)
	}
	if sr, _ := dl.Get(ctx, id); sr.Downloaded != 3 || sr.Already != 1 {
		t.Fatalf("search: %+v", sr)
	}
	// Post 4's file downloading: post 3's page opened meanwhile (not after); post 2's while post... and so on.
	for _, c := range [][2]int64{{4, 3}, {2, 1}} {
		f, page := fileAt[c[0]], pageAt[c[1]]
		if page.IsZero() || !page.Before(f.to) {
			t.Errorf("post %d's page at %v, post %d's file %v-%v: not opened while it downloaded", c[1], page, c[0], f.from, f.to)
		}
	}
}
