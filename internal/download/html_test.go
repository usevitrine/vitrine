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

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/store"
)

// A Gelbooru-0.2 site without an API: list pages (thumbnails named by md5)
// and post pages. Post 3's mp4 is missing (its webm works).
func TestHTMLSite(t *testing.T) {
	ctx := context.Background()
	files := map[int64][]byte{}
	sums := map[int64]string{}
	for id := int64(1); id <= 5; id++ {
		files[id] = makePNG(1000 + id)
		s := md5.Sum(files[id])
		sums[id] = hex.EncodeToString(s[:])
	}
	var mu sync.Mutex
	views := map[int64]int{}
	idBelow := regexp.MustCompile(`id:<(\d+)`)
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if strings.HasPrefix(r.URL.Path, "/images/") {
			name := strings.TrimPrefix(r.URL.Path, "/images/")
			id, _ := strconv.ParseInt(strings.SplitN(name, ".", 2)[0], 10, 64)
			if id == 3 && strings.HasSuffix(name, ".mp4") {
				http.NotFound(w, r)
				return
			}
			w.Write(files[id])
			return
		}
		switch q.Get("s") {
		case "list":
			below := int64(6)
			if m := idBelow.FindStringSubmatch(q.Get("tags")); m != nil {
				below, _ = strconv.ParseInt(m[1], 10, 64)
			}
			var sb strings.Builder
			sb.WriteString("<html><body>")
			for id := below - 1; id >= 1; id-- {
				fmt.Fprintf(&sb, `<div class="col thumb"><a id="p%d" href="%s/index.php?page=post&amp;s=view&amp;id=%d"><img src="%s/thumbnails/x/thumbnail_%s.jpg" title="t"/></a></div>`,
					id, srv.URL, id, srv.URL, sums[id])
			}
			sb.WriteString("</body></html>")
			io.WriteString(w, sb.String())
		case "view":
			id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
			mu.Lock()
			views[id]++
			mu.Unlock()
			fmt.Fprintf(w, `<html><body><div id="tagLink"><span>Posted at Oct, 14 2026 by x</span>
<a class="model" href="index.php?page=post&amp;s=list&amp;tags=person_%d">p</a><a class="tag-type-general" href="index.php?page=post&amp;s=list&amp;tags=common">c</a></div>
<video><source src="%s//images/%d.mp4" type="video/mp4" /><source src="%s//images/%d.webm" type="video/webm" /></video>
<input type="text" name="source" id="source" value="https://src.test/%d" /></body></html>`, id, srv.URL, id, srv.URL, id, id)
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
		"\npost_url: https://html.test/post/{id}\nlimits: {requests_per_sec: 1000}\nratings: {\"\": explicit}\n" +
		"tags: {types: page, namespaces: {model: model}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := New(ctx, lib, d, map[string]plugin.Manifest{"html": m}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	dl.Site("html").Client.SetHTTPClient(srv.Client())

	// File 2 was deleted before: never opened, never downloaded.
	f2, _ := lib.Import(ctx, strings.NewReader(string(files[2])))
	lib.Delete(ctx, []uint32{f2.ID}, "test")

	id, _ := dl.Add(ctx, "html", "common", 0)
	if err := dl.RunQueue(ctx, "html"); err != nil {
		t.Fatal(err)
	}
	sr, _ := dl.Get(ctx, id)
	if sr.State != StateDone || sr.Downloaded != 4 || sr.Deleted != 1 {
		t.Fatalf("search: %+v", sr)
	}
	if views[2] != 0 || views[1] != 1 {
		t.Fatalf("post pages opened: %v (the deleted one must not be)", views)
	}
	pq := mustParse(t, "model:person_3 common system:rating=explicit")
	if n := lib.Index().Search(pq, dl.now()).Total; n != 1 {
		t.Fatalf("post 3 (webm fallback), with namespaced tags and rating: %d", n)
	}

	// Again: everything seen - no post page opened.
	before := len(views)
	total := 0
	for _, n := range views {
		total += n
	}
	dl.Add(ctx, "html", "common", 0)
	dl.RunQueue(ctx, "html")
	after := 0
	for _, n := range views {
		after += n
	}
	if after != total || len(views) != before {
		t.Fatalf("a rerun opened post pages: %d -> %d", total, after)
	}
}
