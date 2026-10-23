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

// A site whose pages say only the day ("Posted at Oct, 14 2026") and whose files' Last-Modified is the moment:
// files downloaded get it at once; files downloaded before (midnight) get it from the one-off pass, which asks for
// the files' headers only and remembers it's done. A Last-Modified on another day isn't taken.
func TestPostedMomentFromFiles(t *testing.T) {
	ctx := context.Background()
	files := map[int64][]byte{}
	sums := map[int64]string{}
	for id := int64(1); id <= 4; id++ {
		files[id] = makePNG(2000 + id)
		s := md5.Sum(files[id])
		sums[id] = hex.EncodeToString(s[:])
	}
	moment := func(id int64) time.Time {
		if id == 2 {
			return time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC) // (re-saved another day: not the upload)
		}
		return time.Date(2026, 10, 14, int(id)+10, 30, 0, 0, time.UTC)
	}
	var mu sync.Mutex
	idBelow := regexp.MustCompile(`id:<(\d+)`)
	dated := false // files sent with their Last-Modified
	fileGets := 0
	newest := int64(3)
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/images/") {
			id, _ := strconv.ParseInt(strings.SplitN(strings.TrimPrefix(r.URL.Path, "/images/"), ".", 2)[0], 10, 64)
			fileGets++
			w.Header().Set("Content-Type", "image/png")
			if dated {
				w.Header().Set("Last-Modified", moment(id).Format(http.TimeFormat))
			}
			w.Write(files[id])
			return
		}
		switch q.Get("s") {
		case "list":
			below := newest + 1
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
			fmt.Fprintf(w, `<html><body><div id="tagLink"><span>Posted at Oct, 14 2026 by x</span>
<a class="tag-type-general" href="index.php?page=post&amp;s=list&amp;tags=common">c</a></div>
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
		"\npost_url: https://html.test/post/{id}\nlimits: {requests_per_sec: 1000}\nratings: {\"\": explicit}\n"))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := New(ctx, lib, d, map[string]plugin.Manifest{"html": m}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	dl.Site("html").Client.SetHTTPClient(srv.Client())
	posted := func() map[int64]time.Time {
		out := map[int64]time.Time{}
		rows, _ := d.R.Query(`SELECT f.id, coalesce(f.posted_at, 0), u.url FROM files f JOIN file_urls fu ON fu.file_id = f.id AND fu.kind = 3
			JOIN urls u ON u.id = fu.url_id`)
		defer rows.Close()
		for rows.Next() {
			var fid, at int64
			var u string
			rows.Scan(&fid, &at, &u)
			post, _ := strconv.ParseInt(strings.TrimSuffix(u[strings.LastIndex(u, "/")+1:], ".png"), 10, 64)
			out[post] = time.UnixMilli(at).UTC()
		}
		return out
	}
	day := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

	// Downloaded while the site sent no Last-Modified: the day only.
	dl.Add(ctx, "html", "common", 0)
	if err := dl.RunQueue(ctx, "html"); err != nil {
		t.Fatal(err)
	}
	if n := len(posted()); n != 3 {
		t.Fatalf("%d files downloaded, want 3", n)
	}
	for post, at := range posted() {
		if !at.Equal(day) {
			t.Fatalf("post %d: %v, want the day", post, at)
		}
	}
	// Now it does: a new post downloaded gets its moment at once...
	mu.Lock()
	dated = true
	newest = 4
	mu.Unlock()
	id2, _ := dl.Add(ctx, "html", "common", 0)
	dl.RunQueue(ctx, "html")
	if sr, _ := dl.Get(ctx, id2); sr.Downloaded != 1 {
		t.Fatalf("second search: %+v", sr)
	}
	if got := posted()[4]; !got.Equal(moment(4)) {
		t.Fatalf("post 4 downloaded: %v, want %v", got, moment(4))
	}
	// ... and the one-off pass gives the earlier ones theirs (post 2's is another day: left as it was).
	mu.Lock()
	fileGets = 0
	mu.Unlock()
	for i := 0; ; i++ {
		more, err := dl.FixPosted(ctx, "html")
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		if i > 10 {
			t.Fatal("never done")
		}
	}
	got := posted()
	if !got[1].Equal(moment(1)) || !got[3].Equal(moment(3)) || !got[2].Equal(day) || !got[4].Equal(moment(4)) {
		t.Fatalf("after the pass: %v", got)
	}
	if fileGets != 3 {
		t.Fatalf("%d file requests, want 3 (posts 1-3: day only)", fileGets)
	}
	// Done: never again.
	if more, _ := dl.FixPosted(ctx, "html"); more || fileGets != 3 {
		t.Fatalf("ran again: more %v, %d requests", more, fileGets)
	}
}
