package download

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/store"
)

// fakeBooru is a gelbooru-0.2 site with posts 1..n (newest = n), each a
// distinct PNG. Post 13's file is missing (404).
type fakeBooru struct {
	srv   *httptest.Server
	files map[int64][]byte

	mu        sync.Mutex
	apiCalls  int
	fileHits  map[int64]int
	fileQuery map[int64]string        // the query each file was last fetched with
	block     chan struct{}           // if set, file downloads wait on it
	n         int64                   // newest post
	types     bool                    // the JSON API answers with tag types (else it fails)
	extra     string                  // appended to every post's tags (" retagged": the site edited them)
	down      bool                    // the API answers 503 (the site is down: it gets paused)
	broken    bool                    // the API answers, but with something unreadable (a real failure, not the connection)
	flaky     map[int64]int           // files that answer 503 this many more times
	garbled   map[int64]int           // files sent with a byte wrong this many more times (a corrupted download)
	wrongMD5  map[int64]bool          // posts the site lists with another md5 than their file's
	ranges    bool                    // files answer Range requests (206 with the rest from that byte)
	ranged    map[int64]int           // Range requests per post
	hold      map[int64]chan struct{} // these files wait until their channel closes
	partial   map[int64]chan struct{} // these send half the file, then wait until their channel closes
	hangOn    string                  // searches with this tag never get an answer (until the request is canceled)
	hidden    map[int64]bool          // posts searches don't list (yet: tagged later)
	favs      []int64                 // user 42's favorites (index.php?page=favorites&s=view&id=42), 10 a page
	active    int                     // file downloads in flight now
	peak      int                     // the most at once
}

func makePNG(id int64) []byte {
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	img.Pix[0], img.Pix[1] = byte(id), byte(id>>8)
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// upload adds k new posts.
func (fb *fakeBooru) upload(k int) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	for range k {
		fb.n++
		fb.files[fb.n] = makePNG(fb.n)
	}
}

func newFakeBooru(t *testing.T, n int) *fakeBooru {
	t.Helper()
	fb := &fakeBooru{files: map[int64][]byte{}, fileHits: map[int64]int{}, fileQuery: map[int64]string{}, flaky: map[int64]int{}, garbled: map[int64]int{}, wrongMD5: map[int64]bool{}, ranged: map[int64]int{}, hidden: map[int64]bool{}, hold: map[int64]chan struct{}{}, partial: map[int64]chan struct{}{}}
	fb.upload(n)
	idBelow := regexp.MustCompile(`id:<(\d+)`)
	idExact := regexp.MustCompile(`(?:^|\s)id:(\d+)(?:\s|$)`)
	fb.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/img/") {
			id, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/img/"), ".png"), 10, 64)
			fb.mu.Lock()
			fb.fileHits[id]++
			fb.fileQuery[id] = r.URL.RawQuery
			block := fb.block
			hold := fb.hold[id]
			fb.active++
			fb.peak = max(fb.peak, fb.active)
			fb.mu.Unlock()
			defer func() {
				fb.mu.Lock()
				fb.active--
				fb.mu.Unlock()
			}()
			if block != nil {
				<-block
			}
			if hold != nil {
				select {
				case <-hold:
				case <-r.Context().Done():
					return
				}
			}
			fb.mu.Lock()
			file := fb.files[id]
			flaky := fb.flaky[id] > 0
			if flaky {
				fb.flaky[id]--
			}
			fb.mu.Unlock()
			if flaky {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			if id == 13 || file == nil {
				http.NotFound(w, r)
				return
			}
			fb.mu.Lock()
			partial := fb.partial[id]
			if fb.garbled[id] > 0 {
				file = append([]byte(nil), file...)
				file[len(file)-1] ^= byte(fb.garbled[id]) // (differently each time, as corruption is)
				fb.garbled[id]--
			}
			fb.mu.Unlock()
			if partial != nil {
				w.Header().Set("Content-Length", strconv.Itoa(len(file)))
				w.Write(file[:len(file)/2])
				w.(http.Flusher).Flush()
				select {
				case <-partial:
				case <-r.Context().Done():
					return
				}
				w.Write(file[len(file)/2:])
				return
			}
			if from, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok && fb.ranges {
				n, _ := strconv.Atoi(strings.TrimSuffix(from, "-"))
				fb.mu.Lock()
				fb.ranged[id]++
				fb.mu.Unlock()
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", n, len(file)-1, len(file)))
				w.WriteHeader(http.StatusPartialContent)
				w.Write(file[n:])
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(file))) // (as file servers do; Go adds it only to small bodies)
			w.Write(file)
			return
		}
		fb.mu.Lock()
		hang := fb.hangOn != "" && strings.Contains(r.URL.Query().Get("tags"), fb.hangOn)
		fb.mu.Unlock()
		if hang {
			<-r.Context().Done() // a site that never answers
			return
		}
		fb.mu.Lock()
		defer fb.mu.Unlock()
		fb.apiCalls++
		if fq := r.URL.Query(); fq.Get("page") == "favorites" {
			pid, _ := strconv.Atoi(fq.Get("pid"))
			var sb strings.Builder
			sb.WriteString("<html><body>")
			for i := pid; i < len(fb.favs) && i < pid+10 && fq.Get("id") == "42"; i++ {
				fmt.Fprintf(&sb, `<span class="thumb"><a href="index.php?page=post&amp;s=view&amp;id=%d" id="p%d"><img src="/thumbnails/thumbnail_x.jpg"/></a></span>`, fb.favs[i], fb.favs[i])
			}
			sb.WriteString("</body></html>")
			io.WriteString(w, sb.String())
			return
		}
		n := fb.n
		if fb.down {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		if fb.broken {
			io.WriteString(w, "<<< not an answer >>>")
			return
		}
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		below := n + 1
		if m := idBelow.FindStringSubmatch(q.Get("tags")); m != nil {
			below, _ = strconv.ParseInt(m[1], 10, 64)
		}
		var ids []int64
		if m := idExact.FindStringSubmatch(q.Get("tags")); m != nil {
			if id, _ := strconv.ParseInt(m[1], 10, 64); id >= 1 && id <= n {
				ids = append(ids, id)
			}
		} else {
			for id := below - 1; id >= 1 && len(ids) < limit; id-- {
				if fb.hidden[id] {
					continue
				}
				ids = append(ids, id)
			}
		}
		if q.Get("json") == "1" {
			if !fb.types || q.Get("fields") != "tag_info" {
				http.Error(w, "no", http.StatusInternalServerError)
				return
			}
			var parts []string
			for _, id := range ids {
				sum := md5.Sum(fb.files[id])
				parts = append(parts, fmt.Sprintf(`{"id":%d,"hash":"%s","file_url":"%s/img/%d.png","tags":"tag_%d common","rating":"s",`+
					`"source":"https://src.example.test/%d","width":8,"height":8,`+
					`"tag_info":[{"tag":"tag_%d","type":"artist"},{"tag":"common","type":"tag"}]}`,
					id, hex.EncodeToString(sum[:]), fb.srv.URL, id, id, id, id))
			}
			io.WriteString(w, strings.ReplaceAll("["+strings.Join(parts, ",")+"]", " common\"", " common"+fb.extra+"\""))
			return
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, `<posts count="%d" offset="0">`, n)
		for _, id := range ids {
			sum := md5.Sum(fb.files[id])
			if fb.wrongMD5[id] {
				sum = md5.Sum([]byte("another version"))
			}
			fmt.Fprintf(&sb, `<post id="%d" md5="%s" file_url="%s/img/%d.png" tags="tag_%d common" rating="s" source="https://src.example.test/%d" width="8" height="8" created_at="%s"/>`,
				id, hex.EncodeToString(sum[:]), fb.srv.URL, id, id, id, postedAt(id).Format("Mon Jan 02 15:04:05 -0700 2006"))
		}
		sb.WriteString("</posts>")
		io.WriteString(w, strings.ReplaceAll(sb.String(), " common\"", " common"+fb.extra+"\""))
	}))
	t.Cleanup(fb.srv.Close)
	return fb
}

type env struct {
	lib *library.Library
	db  *db.DB
	dl  *Downloader
	fb  *fakeBooru
}

func newEnv(t *testing.T, n int) *env {
	t.Helper()
	return newEnvWith(t, n, "")
}

// newEnvWith adds manifest lines to the fake site's plugin.
func newEnvWith(t *testing.T, n int, manifest string) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, _ := store.Open(filepath.Join(dir, "files"))
	lib, err := library.Open(ctx, library.Deps{DB: d, Store: s}) // no thumbnails needed here
	if err != nil {
		t.Fatal(err)
	}
	fb := newFakeBooru(t, n)
	m, err := plugin.Parse([]byte("id: fake\nengine: gelbooru-v02\nbase: " + fb.srv.URL +
		"\npost_url: https://fake.example.test/post/{id}\nlimits: {requests_per_sec: 1000}\nsearch: {per_page: 10}\nratings: {s: general}\n" + manifest))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dl, err := New(ctx, lib, d, map[string]plugin.Manifest{"fake": m}, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	dl.jitter = func() float64 { return 0 } // exact times in tests (TestCheckJitter tests the spread)
	dl.pageRetryWait = time.Millisecond
	dl.Site("fake").Client.SetHTTPClient(fb.srv.Client())
	return &env{lib: lib, db: d, dl: dl, fb: fb}
}

func (e *env) count(t *testing.T, q string) int {
	t.Helper()
	pq, _ := search.Parse(q)
	return e.lib.Index().Search(pq, time.Now()).Total
}

func TestFullRun(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	id, err := e.dl.Add(ctx, "fake", "common", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	s, _ := e.dl.Get(ctx, id)
	if s.State != StateDone || s.Handled != 25 || s.Downloaded != 24 || s.Failed != 1 || s.SiteTotal != 25 {
		t.Fatalf("search: %+v", s)
	}
	if n := e.count(t, ""); n != 24 {
		t.Fatalf("%d files in the library, want 24", n)
	}
	if n := e.count(t, "common system:rating=general"); n != 24 {
		t.Fatalf("site tags/rating: %d", n)
	}
	if n := e.count(t, "tag_7"); n != 1 {
		t.Fatalf("tag_7: %d", n)
	}
	// 25 posts at 10 per page: 3 pages + 1 empty page.
	if e.fb.apiCalls != 4 {
		t.Errorf("%d API calls, want 4", e.fb.apiCalls)
	}

	// Links were stored.
	pq, _ := search.Parse("tag_7")
	fid := e.lib.Index().Search(pq, time.Now()).IDs[0]
	urls, _ := e.lib.FileURLs(ctx, fid)
	kinds := map[int]string{}
	for _, u := range urls {
		kinds[u.Kind] = u.URL
	}
	if kinds[library.URLPost] != "https://fake.example.test/post/7" || kinds[library.URLSource] != "https://src.example.test/7" ||
		!strings.HasSuffix(kinds[library.URLFile], "/img/7.png") {
		t.Fatalf("urls: %+v", urls)
	}

	// The same query again: everything is already known, nothing downloaded.
	hits := len(e.fb.fileHits)
	id2, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	s2, _ := e.dl.Get(ctx, id2)
	if s2.Already != 24 || s2.Downloaded != 0 || len(e.fb.fileHits) != hits {
		t.Fatalf("second run: %+v, file hits %d -> %d", s2, hits, len(e.fb.fileHits))
	}
}

func TestResumeAfterInterruption(t *testing.T) {
	e := newEnv(t, 25)
	ctx, cancel := context.WithCancel(context.Background())
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.OnPost = func(s Search, p plugin.Post, outcome int, err error) {
		if s.Handled == 12 {
			cancel() // stop after 12 posts, as if the power went out
		}
	}
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(context.Background(), id)
	if s.State != StateRunning || s.Handled != 12 || s.LowID != 14 {
		t.Fatalf("after interruption: %+v", s)
	}

	e.dl.OnPost = nil
	if err := e.dl.RunQueue(context.Background(), "fake"); err != nil {
		t.Fatal(err)
	}
	s, _ = e.dl.Get(context.Background(), id)
	if s.State != StateDone || s.Handled != 25 || s.Downloaded != 24 {
		t.Fatalf("after resume: %+v", s)
	}
	// No file was fetched twice.
	for id, n := range e.fb.fileHits {
		if n != 1 {
			t.Errorf("post %d fetched %d times", id, n)
		}
	}
}

func TestInterruptedMidFile(t *testing.T) {
	e := newEnv(t, 5)
	ctx, cancel := context.WithCancel(context.Background())
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	block := make(chan struct{})
	e.fb.mu.Lock()
	e.fb.block = block
	e.fb.mu.Unlock()
	done := make(chan struct{})
	go func() { e.dl.RunQueue(ctx, "fake"); close(done) }()
	time.Sleep(200 * time.Millisecond) // the first file download is now waiting
	cancel()
	close(block)
	<-done
	s, _ := e.dl.Get(context.Background(), id)
	if s.Handled != 0 || s.LowID != 0 || e.count(t, "") != 0 {
		t.Fatalf("interrupted file was recorded: %+v", s)
	}
	e.fb.mu.Lock()
	e.fb.block = nil
	e.fb.mu.Unlock()
	e.dl.RunQueue(context.Background(), "fake")
	if n := e.count(t, ""); n != 5 {
		t.Fatalf("after resume: %d files", n)
	}
}

func TestMaxPostsAndQueueOrder(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	a, _ := e.dl.Add(ctx, "fake", "common", 5)
	b, _ := e.dl.Add(ctx, "fake", "common", 3)
	var order []int64
	e.dl.OnPost = func(s Search, p plugin.Post, outcome int, err error) {
		if len(order) == 0 || order[len(order)-1] != s.ID {
			order = append(order, s.ID)
		}
	}
	e.dl.RunQueue(ctx, "fake")
	sa, _ := e.dl.Get(ctx, a)
	sb, _ := e.dl.Get(ctx, b)
	if sa.Handled != 5 || sa.State != StateDone || sb.Handled != 3 || sb.Already != 3 {
		t.Fatalf("a %+v, b %+v", sa, sb)
	}
	if !slices.Equal(order, []int64{a, b}) {
		t.Fatalf("order %v", order)
	}
}

func TestSkipKnownAndDeleted(t *testing.T) {
	e := newEnv(t, 6)
	ctx := context.Background()
	// The library has post 6's file already (imported by hand) and deleted
	// post 5's.
	f6, _ := e.lib.Import(ctx, bytes.NewReader(e.fb.files[6]))
	f5, _ := e.lib.Import(ctx, bytes.NewReader(e.fb.files[5]))
	e.lib.Delete(ctx, []uint32{f5.ID}, "")

	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(ctx, id)
	if s.Already != 1 || s.Deleted != 1 || s.Downloaded != 4 {
		t.Fatalf("search: %+v", s)
	}
	if e.fb.fileHits[6] != 0 || e.fb.fileHits[5] != 0 {
		t.Fatalf("known files downloaded: %v", e.fb.fileHits)
	}
	// The hand-imported file got the site's tags.
	tags, _ := e.lib.FileTags(ctx, f6.ID)
	if len(tags) != 2 {
		t.Fatalf("tags on the known file: %+v", tags)
	}
}

func TestSiteFailure(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.fb.srv.Close() // the site is down: asked a few times, then paused - the search waits (not failed)
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(ctx, id)
	if s.State != StateRunning || s.Error != "" || e.dl.Site("fake").Client.Paused().Until.IsZero() {
		t.Fatalf("search: %+v, pause %+v", s, e.dl.Site("fake").Client.Paused())
	}
	e2 := newEnv(t, 5)
	e2.fb.broken = true // the site answers nonsense: that fails (it isn't paused)
	id2, _ := e2.dl.Add(ctx, "fake", "common", 0)
	e2.dl.RunQueue(ctx, "fake")
	if s, _ := e2.dl.Get(ctx, id2); s.State != StateFailed || s.Error == "" || !e2.dl.Site("fake").Client.Paused().Until.IsZero() {
		t.Fatalf("broken answer: %+v", s)
	}
	if _, err := e.dl.Add(ctx, "nope", "x", 0); err == nil {
		t.Fatal("unknown site accepted")
	}
}

// postedAt is a fake upload time, deliberately not in post-ID order.
func postedAt(id int64) time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration((id*7)%25) * time.Hour)
}

func TestUploadTime(t *testing.T) {
	e := newEnv(t, 10)
	ctx := context.Background()
	e.dl.Add(ctx, "fake", "common", 0)
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}

	// sort:uploaded orders by the site's upload time, newest first.
	q, _ := search.Parse("sort:uploaded")
	ids := e.lib.Index().Search(q, time.Now()).IDs
	var got []time.Time
	for _, id := range ids {
		var ms int64
		e.db.R.QueryRow("SELECT posted_at FROM files WHERE id = ?", id).Scan(&ms)
		got = append(got, time.UnixMilli(ms).UTC())
	}
	if len(got) != 10 || !slices.IsSortedFunc(got, func(a, b time.Time) int { return b.Compare(a) }) {
		t.Fatalf("uploaded order wrong: %v", got)
	}
	tags, _ := search.Parse("tag_7")
	f7 := e.lib.Index().Search(tags, time.Now()).IDs[0]
	var ms int64
	e.db.R.QueryRow("SELECT posted_at FROM files WHERE id = ?", f7).Scan(&ms)
	if !time.UnixMilli(ms).Equal(postedAt(7)) {
		t.Fatalf("post 7 posted_at = %v, want %v", time.UnixMilli(ms).UTC(), postedAt(7))
	}

	// Files downloaded before upload times were kept get them when a search
	// runs again (seen posts are refreshed, not skipped).
	if _, err := e.db.W.Exec("UPDATE files SET posted_at = NULL"); err != nil {
		t.Fatal(err)
	}
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	var missing int
	e.db.R.QueryRow("SELECT count(*) FROM files WHERE posted_at IS NULL").Scan(&missing)
	if missing != 0 {
		t.Fatalf("%d files still without an upload time after the rerun", missing)
	}
}

func TestBlacklist(t *testing.T) {
	e := newEnv(t, 10)
	ctx := context.Background()
	if _, err := e.lib.SetBlacklist(ctx, []string{"TAG_3", " tag_1* "}); err != nil {
		t.Fatal(err)
	}
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	s, _ := e.dl.Get(ctx, id)
	// tag_3, tag_1 and tag_10 are blacklisted.
	if s.Downloaded != 7 || s.Blacklisted != 3 || e.count(t, "common") != 7 {
		t.Fatalf("downloaded %d, blacklisted %d, files %d; want 7, 3, 7", s.Downloaded, s.Blacklisted,
			e.count(t, "common"))
	}

	// Blacklisted posts are checked again on a rerun: still skipped while
	// blacklisted, downloaded once the entry is removed.
	id, _ = e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if s, _ = e.dl.Get(ctx, id); s.Blacklisted != 3 {
		t.Fatalf("rerun blacklisted %d, want 3", s.Blacklisted)
	}
	e.lib.SetBlacklist(ctx, nil)
	id, _ = e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if s, _ = e.dl.Get(ctx, id); s.Downloaded != 3 || e.count(t, "common") != 10 {
		t.Fatalf("after removing: downloaded %d, files %d; want 3, 10", s.Downloaded, e.count(t, "common"))
	}

	// Files already in the library: found, deleted (undoably), never downloaded again.
	e.lib.SetBlacklist(ctx, []string{"tag_5", "tag_7"})
	m, err := e.lib.BlacklistedFiles(ctx, nil)
	if err != nil || m.Total != 2 || len(m.Counts) != 2 || m.Counts[0].Files != 1 || m.Size <= 0 {
		t.Fatalf("matches = %+v, %v", m, err)
	}
	only5, _ := e.lib.BlacklistedFiles(ctx, []string{"tag_5"})
	if only5.Total != 1 {
		t.Fatalf("tag_5 alone matches %d", only5.Total)
	}
	ids, err := e.lib.DeleteBlacklisted(ctx, nil)
	if err != nil || len(ids) != 2 || e.count(t, "common") != 8 {
		t.Fatalf("deleted %v (%v), files left %d", ids, err, e.count(t, "common"))
	}
	if err := e.lib.Undelete(ctx, ids[:1]); err != nil || e.count(t, "common") != 9 {
		t.Fatalf("undelete: %v, files %d", err, e.count(t, "common"))
	}
}

func TestPauseResumeRemove(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	a, _ := e.dl.Add(ctx, "fake", "common", 0)
	b, _ := e.dl.Add(ctx, "fake", "tag_3", 0)

	// Pause a after 5 posts: it stops before the next post, and the queue moves on to b.
	e.dl.OnPost = func(s Search, p plugin.Post, outcome int, err error) {
		if s.ID == a && s.Handled == 5 {
			if err := e.dl.Pause(ctx, a); err != nil {
				t.Error(err)
			}
		}
	}
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	sa, _ := e.dl.Get(ctx, a)
	sb, _ := e.dl.Get(ctx, b)
	if sa.State != StatePaused || sa.Handled != 5 || sb.State != StateDone {
		t.Fatalf("after pause: a %s/%d, b %s", sa.State, sa.Handled, sb.State)
	}
	if err := e.dl.Resume(ctx, b); !errors.Is(err, ErrState) {
		t.Fatalf("resuming a finished search: %v, want ErrState", err)
	}

	// Resume: carries on from post 6, nothing fetched twice.
	e.dl.OnPost = nil
	if err := e.dl.Resume(ctx, a); err != nil {
		t.Fatal(err)
	}
	e.dl.RunQueue(ctx, "fake")
	if sa, _ = e.dl.Get(ctx, a); sa.State != StateDone || sa.Handled != 25 {
		t.Fatalf("after resume: %+v", sa)
	}
	for id, n := range e.fb.fileHits {
		if n != 1 && id != 13 { // 13 is missing on the site: failed posts are tried again
			t.Errorf("post %d fetched %d times", id, n)
		}
	}

	// Remove while running: stops before the next post; the files stay.
	c, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.OnPost = func(s Search, p plugin.Post, outcome int, err error) {
		if s.ID == c && s.Handled == 2 {
			e.dl.Remove(ctx, c)
		}
	}
	e.dl.RunQueue(ctx, "fake")
	if _, err := e.dl.Get(ctx, c); err == nil {
		t.Fatal("removed search still listed")
	}
	if n := e.count(t, "common"); n != 24 {
		t.Fatalf("files after removing a search: %d, want 24", n)
	}
	if err := e.dl.Remove(ctx, c); !errors.Is(err, ErrNoSearch) {
		t.Fatalf("removing twice: %v", err)
	}
}

func TestClearAndReorder(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	done, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	x, _ := e.dl.Add(ctx, "fake", "tag_1", 0)
	y, _ := e.dl.Add(ctx, "fake", "tag_2", 0)
	z, _ := e.dl.Add(ctx, "fake", "tag_3", 0)
	e.dl.Pause(ctx, x)
	e.dl.Pause(ctx, y)
	e.dl.Pause(ctx, z)

	if err := e.dl.Reorder(ctx, "fake", []int64{z, x}); err != nil {
		t.Fatal(err)
	}
	list, _ := e.dl.List(ctx)
	var order []int64
	for _, s := range list {
		order = append(order, s.ID)
	}
	if !slices.Equal(order, []int64{z, x, done, y}) {
		t.Fatalf("order %v, want %v", order, []int64{z, x, done, y})
	}
	if err := e.dl.Reorder(ctx, "fake", []int64{999}); !errors.Is(err, ErrNoSearch) {
		t.Fatalf("unknown ID: %v", err)
	}

	n, err := e.dl.ClearFinished(ctx)
	if err != nil || n != 1 {
		t.Fatalf("cleared %d (%v), want 1", n, err)
	}
	if list, _ = e.dl.List(ctx); len(list) != 3 {
		t.Fatalf("%d searches left, want 3 (paused ones stay)", len(list))
	}
}

// The limit counts the posts gone through - blacklisted and had ones too - with one connection or several.
func TestMaxPostsCountsPosts(t *testing.T) {
	for _, conns := range []int{1, 3} {
		e := newEnv(t, 25)
		ctx := context.Background()
		if conns > 1 {
			m := e.dl.Site("fake").Client.Manifest()
			m.Limits.Connections = conns
			c := plugin.NewClient(m, nil)
			c.SetHTTPClient(e.fb.srv.Client())
			eng, _ := plugin.NewEngine(c)
			e.dl.putSite("fake", &Site{ID: e.dl.all()["fake"].ID, Client: c, Engine: eng})
		}
		// Posts 25 and 23 are blacklisted; post 22's file is in the library already.
		e.lib.SetBlacklist(ctx, []string{"tag_25", "tag_23"})
		e.lib.Import(ctx, bytes.NewReader(e.fb.files[22]))
		id, _ := e.dl.Add(ctx, "fake", "common", 5)
		e.dl.RunQueue(ctx, "fake")
		s, _ := e.dl.Get(ctx, id)
		// 25 blacklisted, 24 new, 23 blacklisted, 22 had, 21 new: five posts, then it stops.
		if s.State != StateDone || s.Handled != 5 || s.Downloaded != 2 || s.Already != 1 || s.Blacklisted != 2 {
			t.Fatalf("%d connections: %+v", conns, s)
		}
	}
}

func TestOtherHosts(t *testing.T) {
	e := newEnvWith(t, 0, "domains: [example.test]")
	got := e.dl.OtherHosts("img.example.test")
	if !slices.Contains(got, "fake.example.test") {
		t.Fatalf("other hosts %v: want the post pages' host", got)
	}
	if got := e.dl.OtherHosts("fake.example.test"); slices.Contains(got, "fake.example.test") {
		t.Fatalf("%v: lists the host itself", got)
	}
	if got := e.dl.OtherHosts("unrelated.example.org"); len(got) != 0 {
		t.Fatalf("unrelated site: %v", got)
	}
}

// Removing a running search whose site hangs frees its queue at once (the page
// request is canceled; it doesn't wait for the request to time out).
func TestRemoveHungSearchFreesQueue(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	e.fb.mu.Lock()
	e.fb.hangOn = "stuck"
	e.fb.mu.Unlock()
	stuck, err := e.dl.Add(ctx, "fake", "stuck", 0)
	if err != nil {
		t.Fatal(err)
	}
	next, err := e.dl.Add(ctx, "fake", "common", 0)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		e.dl.RunQueue(ctx, "fake")
		close(done)
	}()
	// Running: its page request waits for an answer that never comes.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if s, err := e.dl.Get(ctx, stuck); err == nil && s.State == StateRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	if err := e.dl.Remove(ctx, stuck); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// (where everything is: macOS on CI got here - the stacks say what waits)
		buf := make([]byte, 1<<20)
		t.Logf("goroutines:\n%s", buf[:runtime.Stack(buf, true)])
		t.Fatal("the queue stayed blocked after removing the hung search")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("took %v", took)
	}
	if s, err := e.dl.Get(ctx, next); err != nil || s.State != StateDone {
		t.Fatalf("the next search: %+v %v (want done)", s, err)
	}
}

// A plugin's file_query: files are fetched with it ({id}: the post), the plain URL is what's kept.
func TestFileQuery(t *testing.T) {
	e := newEnvWith(t, 3, "file_query: \"{id}\"")
	ctx := context.Background()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	e.fb.mu.Lock()
	q := e.fb.fileQuery[2]
	e.fb.mu.Unlock()
	if q != "2" {
		t.Fatalf("fetched with %q", q)
	}
	var kept string
	e.db.R.QueryRow(`SELECT u.url FROM file_urls fu JOIN urls u ON u.id = fu.url_id JOIN seen_posts sp ON sp.file_id = fu.file_id
		WHERE sp.post_id = 2 AND fu.kind = 3`).Scan(&kept)
	if !strings.HasSuffix(kept, "/img/2.png") {
		t.Fatalf("kept %q", kept)
	}
}

// A post whose file was removed for good after it was handled (the trash emptied meanwhile) is recorded without it:
// the foreign key failed the whole check (2026-10-28, on the main instance).
func TestRecordFileRemovedMeanwhile(t *testing.T) {
	e := newEnv(t, 1)
	ctx := context.Background()
	id, err := e.dl.Add(ctx, "fake", "common", 0)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := e.dl.Get(ctx, id)
	site := e.dl.Site("fake").ID
	if _, err := e.dl.record(s, site, 77, 999999, Already, 77, nil); err != nil {
		t.Fatalf("recording a post whose file is gone: %v", err)
	}
	var file sql.NullInt64
	if err := e.db.R.QueryRow("SELECT file_id FROM seen_posts WHERE site_id = ? AND post_id = 77", site).Scan(&file); err != nil || file.Valid {
		t.Fatalf("seen post: file %v, %v", file, err)
	}
}

// A tag blacklisted while a download runs applies from its next post, not only from the next download (2026-10-28,
// the user: "when you add new blacklisted tags, the changes [don't] apply to running downloads").
func TestBlacklistMidDownload(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	var once sync.Once
	e.dl.OnPost = func(Search, plugin.Post, int, error) {
		once.Do(func() {
			if _, err := e.lib.SetBlacklist(ctx, []string{"tag_3"}); err != nil {
				t.Error(err)
			}
		})
	}
	id, err := e.dl.Add(ctx, "fake", "common", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	s, _ := e.dl.Get(ctx, id)
	if s.Blacklisted != 1 || e.count(t, "tag_3") != 0 || e.count(t, "tag_4") != 1 {
		t.Fatalf("blacklisted %d, tag_3 %d, tag_4 %d: %+v", s.Blacklisted, e.count(t, "tag_3"), e.count(t, "tag_4"), s)
	}
}

// A download's run time counts only while it works: paused (or waiting of any kind), the clock stands; it shows when
// its run now began (a page counts on from there) and keeps what came before.
func TestRunTime(t *testing.T) {
	e := newEnv(t, 12)
	ctx := context.Background()
	advance := e.clock()
	id, err := e.dl.Add(ctx, "fake", "common", 0)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	worked := time.Duration(0) // the time the clock moved while it ran
	var sawSince bool
	var liveErr string
	e.dl.OnPost = func(s Search, p plugin.Post, outcome int, err error) {
		mu.Lock()
		defer mu.Unlock()
		advance(time.Minute)
		worked += time.Minute
		got, _ := e.dl.Get(ctx, id)
		if got.RunningSince > 0 {
			sawSince = true
			// (live: the run time up to this moment - run_ms already holds the part up to the last post, not twice)
			if got.RunNow != worked.Milliseconds() && liveErr == "" {
				liveErr = fmt.Sprintf("run_now %d ms after %v of work", got.RunNow, worked)
			}
		}
		if worked == 4*time.Minute {
			e.dl.Pause(ctx, id)
		}
	}
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	if liveErr != "" {
		t.Fatal(liveErr)
	}
	s, _ := e.dl.Get(ctx, id)
	if s.State != StatePaused || s.RunningSince != 0 || s.RunNow != 0 || !sawSince {
		t.Fatalf("paused: %+v (running since seen: %v)", s, sawSince)
	}
	if s.RunMS != worked.Milliseconds() {
		t.Fatalf("run time when paused: %d ms, want %d", s.RunMS, worked.Milliseconds())
	}
	advance(time.Hour) // (paused: not counted)
	if err := e.dl.Resume(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	s, _ = e.dl.Get(ctx, id)
	if s.State != StateDone || s.RunMS != worked.Milliseconds() {
		t.Fatalf("done: %+v, run time %d ms, want %d (the paused hour left out)", s.State, s.RunMS, worked.Milliseconds())
	}
}
