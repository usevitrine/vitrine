package download

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFailedSearchRetries(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	advance := e.clock()
	e.dl.retryWaits = []time.Duration{time.Minute, 5 * time.Minute}
	e.dl.Site("fake").Client.SetTooManyWait(time.Millisecond)
	e.fb.mu.Lock()
	e.fb.broken = true
	e.fb.mu.Unlock()
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(ctx, id)
	if s.State != StateFailed || s.Attempts != 1 || s.RetryAt != e.dl.now().Add(time.Minute).UnixMilli() {
		t.Fatalf("first failure: %+v", s)
	}
	// Not yet due: stays failed.
	e.dl.retryFailed(ctx)
	if s, _ = e.dl.Get(ctx, id); s.State != StateFailed {
		t.Fatalf("retried too early: %+v", s)
	}
	// Due: queued again, fails again, waits longer.
	advance(time.Minute)
	e.dl.retryFailed(ctx)
	if s, _ = e.dl.Get(ctx, id); s.State != StateQueued || s.Error != "" {
		t.Fatalf("not queued: %+v", s)
	}
	e.dl.RunQueue(ctx, "fake")
	if s, _ = e.dl.Get(ctx, id); s.Attempts != 2 || s.RetryAt != e.dl.now().Add(5*time.Minute).UnixMilli() {
		t.Fatalf("second failure: %+v", s)
	}
	// Out of automatic tries: waits for the user.
	advance(5 * time.Minute)
	e.dl.retryFailed(ctx)
	e.dl.RunQueue(ctx, "fake")
	if s, _ = e.dl.Get(ctx, id); s.Attempts != 3 || s.RetryAt != 0 || s.State != StateFailed {
		t.Fatalf("third failure: %+v", s)
	}
	// The site is back; Retry (by hand) finishes it and clears the count.
	e.fb.mu.Lock()
	e.fb.broken = false
	e.fb.mu.Unlock()
	if err := e.dl.Resume(ctx, id); err != nil {
		t.Fatal(err)
	}
	e.dl.RunQueue(ctx, "fake")
	if s, _ = e.dl.Get(ctx, id); s.State != StateDone || s.Attempts != 0 || s.Downloaded != 5 {
		t.Fatalf("after the site came back: %+v", s)
	}
}

func TestFileRetries(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.dl.fileWaits = []time.Duration{time.Millisecond, time.Millisecond}
	e.dl.Site("fake").Client.SetTooManyWait(time.Millisecond)
	e.fb.mu.Lock()
	e.fb.flaky[4] = 2 // two 503s, then the file
	e.fb.flaky[2] = 5 // keeps failing: given up after three tries
	e.fb.mu.Unlock()
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(ctx, id)
	if s.Downloaded != 4 || s.Failed != 1 {
		t.Fatalf("search: %+v", s)
	}
	if e.fb.fileHits[4] != 3 || e.fb.fileHits[2] != 3 {
		t.Fatalf("file hits: 4=%d 2=%d (want 3 each)", e.fb.fileHits[4], e.fb.fileHits[2])
	}
	if e.fb.fileHits[13] != 0 {
		t.Fatal("unexpected")
	}
}

// A file that comes out wrong (its md5 isn't the one the site gave) is downloaded again; if it keeps coming out
// different it fails. One the site sends the same way twice is kept: the site's md5 is wrong for it, not the download.
func TestFileMD5Checked(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.dl.fileWaits = []time.Duration{time.Millisecond, time.Millisecond}
	e.fb.mu.Lock()
	e.fb.garbled[4] = 1     // wrong once, then right
	e.fb.garbled[2] = 5     // wrong every time, differently: fails after three tries
	e.fb.wrongMD5[3] = true // the site lists another md5; its file always comes the same
	e.fb.mu.Unlock()
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(ctx, id)
	// 4: the second try is right. 3: the same file twice - kept. 2: never right - failed (listed, retried later).
	if s.Downloaded != 4 || s.Failed != 1 {
		t.Fatalf("search: %+v", s)
	}
	if e.fb.fileHits[4] != 2 || e.fb.fileHits[3] != 2 || e.fb.fileHits[2] != 3 || e.fb.fileHits[1] != 1 {
		t.Fatalf("file hits: 4=%d 3=%d 2=%d 1=%d (want 2, 2, 3, 1)", e.fb.fileHits[4], e.fb.fileHits[3], e.fb.fileHits[2], e.fb.fileHits[1])
	}
	// The one that came right is the site's file, not the garbled copy.
	want := md5.Sum(e.fb.files[4])
	if m, err := e.lib.FindMD5(ctx, want); err != nil || m.FileID == 0 {
		t.Fatalf("file 4 kept garbled: %+v, %v", m, err)
	}
	if failed, _ := e.dl.FailedPosts(ctx); len(failed) != 1 || failed[0].Post != 2 || !strings.Contains(failed[0].Error, "md5") {
		t.Fatalf("failed: %+v", failed)
	}
}

// A file that came whole over HTTPS with another md5 than the site lists (a file replaced on the site since) is kept
// once two small pieces asked for again are the same - not downloaded a second time. One with a byte wrong (a broken
// copy, different each time) isn't: its pieces differ, so it's downloaded again and the right one kept.
func TestWrongMD5CheckedByPieces(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.dl.fileWaits = []time.Duration{time.Millisecond, time.Millisecond}
	e.fb.mu.Lock()
	e.fb.ranges = true
	for _, id := range []int64{3, 4} { // big enough for two pieces (a PNG with bytes after its end still is one)
		e.fb.files[id] = append(e.fb.files[id], bytes.Repeat([]byte{byte(id)}, 3*sampleBytes)...)
	}
	e.fb.wrongMD5[3] = true // the site lists another md5; its file always comes the same
	e.fb.garbled[4] = 2     // its last byte wrong twice, differently
	e.fb.mu.Unlock()
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if s, _ := e.dl.Get(ctx, id); s.Downloaded != 5 || s.Failed != 0 {
		t.Fatalf("search: %+v", s)
	}
	if e.fb.fileHits[3] != 3 || e.fb.ranged[3] != 2 {
		t.Fatalf("the replaced file: %d requests, %d of them for a piece (want 3, 2: one download, two pieces)", e.fb.fileHits[3], e.fb.ranged[3])
	}
	if m, err := e.lib.FindMD5(ctx, md5.Sum(e.fb.files[4])); err != nil || m.FileID == 0 {
		t.Fatalf("the broken copy kept, not the site's file: %+v, %v", m, err)
	}
}

// A failed file is listed (with why and the search that met it) and can be
// tried again on its own once the site is better.
func TestRetryFailedPost(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.dl.fileWaits = []time.Duration{time.Millisecond, time.Millisecond}
	e.dl.Site("fake").Client.SetTooManyWait(time.Millisecond)
	e.fb.mu.Lock()
	e.fb.flaky[2] = 3 // fails all three tries of the search
	e.fb.mu.Unlock()
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if s, _ := e.dl.Get(ctx, id); s.Failed != 1 {
		t.Fatalf("search: %+v", s)
	}
	failed, err := e.dl.FailedPosts(ctx)
	if err != nil || len(failed) != 1 || failed[0].Post != 2 || failed[0].Site != "fake" || failed[0].Query != "common" || failed[0].Error == "" || failed[0].URL == "" {
		t.Fatalf("failed posts: %+v, %v", failed, err)
	}
	if _, err := e.dl.RetryPost(ctx, "fake", 3); !errors.Is(err, ErrNotFailed) {
		t.Fatalf("a post that didn't fail: %v", err)
	}
	outcome, err := e.dl.RetryPost(ctx, "fake", 2) // the site answers now
	if err != nil || outcome != "downloaded" {
		t.Fatalf("retry: %q, %v", outcome, err)
	}
	if failed, _ := e.dl.FailedPosts(ctx); len(failed) != 0 {
		t.Fatalf("still listed: %+v", failed)
	}
}

// A retry looks the post up on its own - on a site whose answer for one post has no upload time (rule34's JSON),
// the file still gets one (from the site's search for that post), and it's "found by" the search that failed it.
func TestRetryFailedPostKeepsDateAndSearch(t *testing.T) {
	e := newEnvWith(t, 5, "tags: {types: tag_info}")
	ctx := context.Background()
	e.dl.fileWaits = []time.Duration{time.Millisecond, time.Millisecond}
	e.dl.Site("fake").Client.SetTooManyWait(time.Millisecond)
	e.fb.mu.Lock()
	e.fb.types = true
	e.fb.flaky[2] = 3
	e.fb.mu.Unlock()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if outcome, err := e.dl.RetryPost(ctx, "fake", 2); err != nil || outcome != "downloaded" {
		t.Fatalf("retry: %q, %v", outcome, err)
	}
	var posted sql.NullInt64
	var query string
	e.db.R.QueryRow(`SELECT f.posted_at, coalesce((SELECT query FROM file_sources WHERE file_id = f.id), '')
		FROM files f JOIN seen_posts sp ON sp.file_id = f.id AND sp.post_id = 2`).Scan(&posted, &query)
	if !posted.Valid || posted.Int64 != postedAt(2).UnixMilli() {
		t.Fatalf("upload time %v, want %v", posted, postedAt(2).UnixMilli())
	}
	if query != "common" {
		t.Fatalf("found by %q", query)
	}
}

// Retry pressed again while it runs: it waits for the running one (the file is fetched once).
func TestRetryOnceAtATime(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.dl.fileWaits = []time.Duration{time.Millisecond, time.Millisecond}
	e.dl.Site("fake").Client.SetTooManyWait(time.Millisecond)
	e.fb.mu.Lock()
	e.fb.flaky[2] = 3
	e.fb.mu.Unlock()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	hold := make(chan struct{})
	e.fb.mu.Lock()
	e.fb.hold[2] = hold
	hits := e.fb.fileHits[2]
	e.fb.mu.Unlock()
	results := make(chan string, 3)
	for range 3 {
		go func() {
			o, _ := e.dl.RetryPost(ctx, "fake", 2)
			results <- o
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(hold)
	for range 3 {
		if o := <-results; o != "downloaded" {
			t.Fatalf("outcome %q", o)
		}
	}
	if n := e.fb.fileHits[2] - hits; n != 1 {
		t.Fatalf("fetched %d times", n)
	}
}

// A retry in the background: started at once (pressed again: still one), listed with its progress while it runs,
// then with its result.
func TestStartRetry(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.dl.fileWaits = []time.Duration{time.Millisecond, time.Millisecond}
	e.dl.Site("fake").Client.SetTooManyWait(time.Millisecond)
	e.fb.mu.Lock()
	e.fb.flaky[2] = 3
	e.fb.mu.Unlock()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if err := e.dl.StartRetry(ctx, "fake", 3); !errors.Is(err, ErrNotFailed) {
		t.Fatalf("a post that didn't fail: %v", err)
	}
	hold := make(chan struct{})
	e.fb.mu.Lock()
	e.fb.hold[2] = hold
	hits := e.fb.fileHits[2]
	e.fb.mu.Unlock()
	for range 2 {
		if err := e.dl.StartRetry(ctx, "fake", 2); err != nil {
			t.Fatal(err)
		}
	}
	if r := e.dl.Retries(); len(r) != 1 || r[0].Finished != 0 || r[0].Query != "common" {
		t.Fatalf("while running: %+v", r)
	}
	close(hold)
	deadline := time.Now().Add(3 * time.Second)
	for {
		r := e.dl.Retries()
		if len(r) == 1 && r[0].Finished != 0 {
			if r[0].Outcome != "downloaded" || r[0].Error != "" {
				t.Fatalf("result: %+v", r[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not finished: %+v", r)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := e.fb.fileHits[2] - hits; n != 1 {
		t.Fatalf("fetched %d times", n)
	}
}

// Dismissed failed posts leave the list (forgotten: a later search tries them again).
func TestDismissFailedPosts(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.dl.fileWaits = []time.Duration{time.Millisecond, time.Millisecond}
	e.dl.Site("fake").Client.SetTooManyWait(time.Millisecond)
	e.fb.mu.Lock()
	e.fb.flaky[2] = 3
	e.fb.mu.Unlock()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if failed, _ := e.dl.FailedPosts(ctx); len(failed) != 1 {
		t.Fatalf("failed posts: %+v", failed)
	}
	n, err := e.dl.DismissFailed(ctx, []PostRef{{Site: "fake", Post: 3}, {Site: "fake", Post: 2}, {Site: "nowhere", Post: 2}})
	if err != nil || n != 1 {
		t.Fatalf("dismissed %d, %v (want 1: post 3 didn't fail, nowhere isn't a site)", n, err)
	}
	if failed, _ := e.dl.FailedPosts(ctx); len(failed) != 0 {
		t.Fatalf("still listed: %+v", failed)
	}
	// Met again by a search: tried again (the site answers now).
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if s, _ := e.dl.Get(ctx, id); s.Downloaded != 1 {
		t.Fatalf("search after dismissing: %+v", s)
	}
}
