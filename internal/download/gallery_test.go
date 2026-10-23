package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/store"
)

// fakeGalleries is a gallery site (the ehentai engine's shape): gallery 300
// has 5 pages (two per page of thumbnails); version 301 of it (same first
// version, 250) has 6, version 302 a new first page before those 6. A page's
// key (in its link) is its picture's number. limitAfter: picture pages serve
// the image-limit picture after that many (0 = never).
type fakeGalleries struct {
	mu         sync.Mutex
	srv        *httptest.Server
	newest     int64 // the newest gallery the list shows (300, then 301)
	limitAfter int
	pictures   int // picture pages served
}

// pictures lists a gallery's pages: each one's picture number.
func (f *fakeGalleries) layout(gid int64) []int {
	switch gid {
	case 301:
		return []int{1, 2, 3, 4, 5, 6}
	case 302:
		return []int{99, 1, 2, 3, 4, 5, 6}
	case 400: // (another gallery: one picture the first has, one of its own)
		return []int{3, 77}
	}
	return []int{1, 2, 3, 4, 5}
}

func (f *fakeGalleries) pages(gid int64) int { return len(f.layout(gid)) }

func newGalleryEnv(t *testing.T) (*env, *fakeGalleries) {
	t.Helper()
	ctx := context.Background()
	f := &fakeGalleries{newest: 300}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "https://" + r.Host
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/":
			if r.URL.Query().Get("next") != "" {
				io.WriteString(w, `<html>Found about 1 result.</html>`)
				return
			}
			newest := f.newest
			if strings.Contains(r.URL.Query().Get("f_search"), "other") {
				newest = 400
			}
			fmt.Fprintf(w, `<html>Found about 1 result. <a href="%s/g/%d/aaaaaaaaaa/">x</a></html>`, base, newest)
		case r.URL.Path == "/api.php":
			var req struct {
				Gidlist [][]any `json:"gidlist"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			var out []map[string]any
			for _, g := range req.Gidlist {
				gid := int64(g[0].(float64))
				first := "250"
				if gid == 400 {
					first = "400"
				}
				out = append(out, map[string]any{"gid": gid, "token": "aaaaaaaaaa", "title": "Some comic", "category": "Manga",
					"posted": "1700000000", "filecount": strconv.Itoa(f.pages(gid)), "first_gid": first,
					"tags": []string{"artist:some artist", "female:glasses", "male:glasses"}})
			}
			json.NewEncoder(w).Encode(map[string]any{"gmetadata": out})
		case strings.HasPrefix(r.URL.Path, "/g/"):
			gid, _ := strconv.ParseInt(strings.Split(r.URL.Path, "/")[2], 10, 64)
			p, _ := strconv.Atoi(r.URL.Query().Get("p"))
			io.WriteString(w, "<html>")
			for n := p*2 + 1; n <= min(p*2+2, f.pages(gid)); n++ {
				fmt.Fprintf(w, `<a href="%s/s/%010d/%d-%d"><img></a>`, base, f.layout(gid)[n-1], gid, n)
			}
			io.WriteString(w, "</html>")
		case strings.HasPrefix(r.URL.Path, "/s/"):
			f.pictures++
			if f.limitAfter > 0 && f.pictures > f.limitAfter {
				io.WriteString(w, `<html><img id="img" src="https://ehgt.org/g/509.gif" /></html>`)
				return
			}
			key := strings.Split(r.URL.Path, "/")[2]
			fmt.Fprintf(w, `<html><img id="img" src="%s/img/%s.png" /></html>`, base, key)
		case strings.HasPrefix(r.URL.Path, "/img/"):
			// (by the picture's number: the same picture in every version)
			n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/img/"), ".png"))
			w.Write(makePNG(int64(100 + n)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
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
	m, err := plugin.Parse([]byte("id: galleries\nengine: ehentai\nbase: " + f.srv.URL + "\npost_url: " + f.srv.URL +
		"/g/{id}/\nauth: {fields: [username, password, ipb_member_id, ipb_pass_hash], login: ehentai}\nlimits: {requests_per_sec: 1000}\n" +
		"tags: {namespaces: {artist: creator}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	dl, err := New(ctx, lib, d, map[string]plugin.Manifest{"galleries": m}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	dl.jitter = func() float64 { return 0 }
	h := *f.srv.Client()
	dl.Site("galleries").Client.SetHTTPClient(&h)
	return &env{lib: lib, db: d, dl: dl}, f
}

func TestGalleryDownload(t *testing.T) {
	e, f := newGalleryEnv(t)
	ctx := context.Background()
	comics := func() []library.Comic {
		t.Helper()
		list, _, err := e.lib.Comics(ctx, library.ComicQuery{})
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	// The image limit after 3 pictures: the search stops, the gallery isn't counted as done.
	f.mu.Lock()
	f.limitAfter = 3
	f.mu.Unlock()
	id, err := e.dl.Add(ctx, "galleries", "some artist", 0)
	if err != nil {
		t.Fatal(err)
	}
	e.dl.RunQueue(ctx, "galleries")
	s, _ := e.dl.Get(ctx, id)
	if s.State != StateFailed || !strings.Contains(s.Error, "image limit") || s.Handled != 0 {
		t.Fatalf("after the limit: %+v", s)
	}
	if s.Pages != 3 {
		t.Fatalf("pages counted on the download: %d, want 3", s.Pages)
	}
	list := comics()
	if len(list) != 1 || list[0].Have != 3 || list[0].Pages != 5 || list[0].Title != "Some comic" || list[0].Site != "galleries" {
		t.Fatalf("comic after the limit: %+v", list)
	}
	if strings.Join(list[0].Tags, " ") != "category:manga creator:some_artist female:glasses male:glasses" {
		t.Fatalf("tags: %v", list[0].Tags)
	}
	// The limit refilled: it goes on from page 4.
	f.mu.Lock()
	f.limitAfter, f.pictures = 0, 0
	f.mu.Unlock()
	if err := e.dl.Resume(ctx, id); err != nil {
		t.Fatal(err)
	}
	e.dl.RunQueue(ctx, "galleries")
	s, _ = e.dl.Get(ctx, id)
	if s.State != StateDone || s.Downloaded != 1 {
		t.Fatalf("after going on: %+v", s)
	}
	f.mu.Lock()
	served := f.pictures
	f.mu.Unlock()
	if c := comics()[0]; c.Have != 5 || served != 2 {
		t.Fatalf("comic: %d pages (want 5); %d picture pages fetched (want 2: pages 4-5)", c.Have, served)
	}
	if n := e.lib.Index().Stats().Files; n != 0 {
		t.Fatalf("%d files in the library: comic pages never are", n)
	}

	// A newer version (301, same first version, a page added): the same comic, now 6 pages - only the new one fetched.
	f.mu.Lock()
	f.newest, f.pictures = 301, 0
	f.mu.Unlock()
	id2, _ := e.dl.Add(ctx, "galleries", "some artist", 0)
	e.dl.RunQueue(ctx, "galleries")
	if s, _ := e.dl.Get(ctx, id2); s.State != StateDone {
		t.Fatalf("the new version's search: %+v", s)
	}
	list = comics()
	if len(list) != 1 || list[0].Have != 6 || list[0].Pages != 6 || !strings.Contains(list[0].URL, "/g/301/") {
		t.Fatalf("after the new version: %+v", list)
	}
	f.mu.Lock()
	served = f.pictures
	f.mu.Unlock()
	if served != 1 {
		t.Fatalf("the new version: %d picture pages fetched (want 1: the new page)", served)
	}

	// Downloaded again ("Download again": its pages cleared): a gallery seen before is fetched again.
	if err := e.lib.ClearComicPages(ctx, []int64{list[0].ID}); err != nil {
		t.Fatal(err)
	}
	if c := comics()[0]; c.Have != 0 {
		t.Fatalf("cleared: %d pages", c.Have)
	}
	id3, _ := e.dl.Add(ctx, "galleries", "some artist", 0)
	e.dl.RunQueue(ctx, "galleries")
	if s, _ := e.dl.Get(ctx, id3); s.State != StateDone || s.Downloaded != 1 {
		t.Fatalf("downloaded again: %+v", s)
	}
	if c := comics()[0]; c.Have != 6 || c.Cover == 0 {
		t.Fatalf("downloaded again: %+v", c)
	}

	// A newer version with a page added at the start (302): the pages move along by their keys - only the new
	// picture is asked for.
	before, _ := e.lib.ComicPageKeys(ctx, list[0].ID)
	f.mu.Lock()
	f.newest, f.pictures = 302, 0
	f.mu.Unlock()
	id4, _ := e.dl.Add(ctx, "galleries", "some artist", 0)
	e.dl.RunQueue(ctx, "galleries")
	if s, _ := e.dl.Get(ctx, id4); s.State != StateDone {
		t.Fatalf("a page added at the start: %+v", s)
	}
	f.mu.Lock()
	served = f.pictures
	f.mu.Unlock()
	after, _ := e.lib.ComicPageKeys(ctx, list[0].ID)
	if served != 1 || len(after) != 7 || after[0].Key != "0000000099" || after[1].Key != "0000000001" ||
		after[1].File != before[0].File || after[6].File != before[5].File {
		t.Fatalf("a page added at the start: %d picture pages fetched (want 1); pages %+v (were %+v)", served, after, before)
	}

	// Another gallery with a picture the library has (in another comic): used without asking.
	f.mu.Lock()
	f.pictures = 0
	f.mu.Unlock()
	id5, _ := e.dl.Add(ctx, "galleries", "other artist", 0)
	e.dl.RunQueue(ctx, "galleries")
	if s, _ := e.dl.Get(ctx, id5); s.State != StateDone {
		t.Fatalf("another gallery: %+v", s)
	}
	f.mu.Lock()
	served = f.pictures
	f.mu.Unlock()
	list = comics()
	var other library.Comic
	for _, c := range list {
		if strings.Contains(c.URL, "/g/400/") {
			other = c
		}
	}
	k, _ := e.lib.ComicPageKeys(ctx, other.ID)
	if other.ID == 0 || served != 1 || len(k) != 2 || k[0].File != after[3].File || k[1].Key != "0000000077" {
		t.Fatalf("another gallery: %d picture pages fetched (want 1); pages %+v", served, k)
	}
}
