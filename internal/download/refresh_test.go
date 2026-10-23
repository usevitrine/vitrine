package download

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/search"
)

func TestTagRefresh(t *testing.T) {
	e := newEnvWith(t, 25, typesManifest)
	e.fb.types = true
	ctx := context.Background()
	advance := e.clock()
	e.dl.Add(ctx, "fake", "common", 0)
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}

	// Just downloaded: nothing due.
	if more, err := e.dl.RefreshDue(ctx, "fake"); more || err != nil {
		t.Fatalf("due right after downloading: %v %v", more, err)
	}
	st, _ := e.dl.RefreshStatus(ctx)
	if len(st) != 1 || st[0].Posts != 24 || st[0].Due != 0 {
		t.Fatalf("status: %+v", st)
	}

	// The posts went up on 1 January 2026; ~9 months old, so due about 3 months later.
	e.fb.extra = " retagged"
	advance(60 * 24 * time.Hour)
	if more, _ := e.dl.RefreshDue(ctx, "fake"); more {
		t.Fatal("due too early")
	}
	advance(40 * 24 * time.Hour) // a quarter of its age by then (~370 days)
	st, _ = e.dl.RefreshStatus(ctx)
	if st[0].Due != 24 {
		t.Fatalf("status when due: %+v", st)
	}
	calls := e.fb.apiCalls
	n := 0
	for {
		more, err := e.dl.RefreshDue(ctx, "fake")
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
		if n++; n > 30 {
			t.Fatal("refresh doesn't finish")
		}
	}
	if n != 24 || e.fb.apiCalls-calls != 24 {
		t.Fatalf("%d refreshes, %d requests; want 24 each (one JSON request per post)", n, e.fb.apiCalls-calls)
	}
	if c := e.count(t, "retagged"); c != 24 {
		t.Fatalf("retagged: %d", c)
	}
	if c := e.count(t, "creator:tag_7"); c != 1 {
		t.Fatalf("creator:tag_7: %d", c)
	}

	// "Refresh tags" on a selection: due at once.
	e.fb.extra = " retagged again"
	ids := e.lib.Index().Search(mustParse(t, "tag_3"), e.dl.now()).IDs
	if k, err := e.dl.ScheduleRefresh(ctx, ids); err != nil || k != 1 {
		t.Fatalf("schedule: %d %v", k, err)
	}
	if more, err := e.dl.RefreshDue(ctx, "fake"); !more || err != nil {
		t.Fatalf("scheduled post not refreshed: %v %v", more, err)
	}
	if c := e.count(t, "again"); c != 1 {
		t.Fatalf("again: %d", c)
	}

	// Refreshing one file at once; then the site loses the post (tags stay).
	ids = e.lib.Index().Search(mustParse(t, "tag_20"), e.dl.now()).IDs
	if k, q, err := e.dl.RefreshFile(ctx, ids[0]); err != nil || k != 1 || q != 0 {
		t.Fatalf("refresh file: %d %d %v", k, q, err)
	}
	e.fb.mu.Lock()
	e.fb.n = 10
	e.fb.mu.Unlock()
	if k, _, err := e.dl.RefreshFile(ctx, ids[0]); err != nil || k != 0 {
		t.Fatalf("refresh a gone post: %d %v", k, err)
	}
	if c := e.count(t, "creator:tag_20"); c != 1 {
		t.Fatalf("tags of a gone post: %d", c)
	}
	f, _ := e.lib.Import(ctx, bytesReader(makePNG(9999)))
	if _, _, err := e.dl.RefreshFile(ctx, f.ID); !errors.Is(err, ErrNoPosts) {
		t.Fatalf("a file from no site: %v", err)
	}
}

// A paused site (it kept failing to answer): refreshes still queue up - a selection's, and one file's "Refresh tags"
// (queued, not asked) - but nothing is asked of the site until it answers again; then the queue is worked through.
func TestTagRefreshWaitsForAPausedSite(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	e.dl.SetPauseAfter(1)
	e.fb.mu.Lock()
	e.fb.down = true
	e.fb.mu.Unlock()
	ids := e.lib.Index().Search(mustParse(t, "tag_2"), e.dl.now()).IDs
	e.dl.RefreshFile(ctx, ids[0]) // (fails: the site pauses)
	if e.dl.Site("fake").Client.Paused().Until.IsZero() {
		t.Fatal("not paused")
	}
	calls := e.fb.apiCalls
	all := e.lib.Index().Search(mustParse(t, "common"), e.dl.now()).IDs
	if k, err := e.dl.ScheduleRefresh(ctx, all); err != nil || k != 5 {
		t.Fatalf("schedule while paused: %d %v", k, err)
	}
	if k, q, err := e.dl.RefreshFile(ctx, ids[0]); err != nil || k != 0 || q != 1 {
		t.Fatalf("refresh one file while paused: found %d, queued %d, %v", k, q, err)
	}
	e.dl.background(ctx, "fake")
	if e.fb.apiCalls != calls {
		t.Fatalf("%d requests to a paused site", e.fb.apiCalls-calls)
	}
	st, _ := e.dl.RefreshStatus(ctx)
	if len(st) != 1 || st[0].Due != 5 || !st[0].Paused {
		t.Fatalf("status: %+v", st)
	}
	// Back: the queued refreshes go.
	e.fb.mu.Lock()
	e.fb.down = false
	e.fb.extra = " retagged"
	e.fb.mu.Unlock()
	e.dl.TryNow("fake")
	for range 10 {
		if more, err := e.dl.RefreshDue(ctx, "fake"); !more || err != nil {
			break
		}
	}
	if c := e.count(t, "retagged"); c != 5 {
		t.Fatalf("refreshed after the site came back: %d", c)
	}
}

func mustParse(t *testing.T, q string) search.Query {
	t.Helper()
	pq, err := search.Parse(q)
	if err != nil {
		t.Fatal(err)
	}
	return pq
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// A file without an upload time (from a lookup that had none) is first in the tag refresh after a start, and the
// refresh gets the time from the site's search for that post (its answer for one post has none).
func TestRefreshFillsUploadTime(t *testing.T) {
	e := newEnvWith(t, 3, "tags: {types: tag_info}")
	ctx := context.Background()
	e.fb.mu.Lock()
	e.fb.types = true
	e.fb.mu.Unlock()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	var file uint32
	e.db.R.QueryRow("SELECT file_id FROM seen_posts WHERE post_id = 2").Scan(&file)
	e.db.W.Exec("UPDATE files SET posted_at = NULL WHERE id = ?", file)
	if n, err := e.dl.scheduleUndated(ctx); err != nil || n != 1 {
		t.Fatalf("scheduled %d, %v", n, err)
	}
	if ok, err := e.dl.RefreshDue(ctx, "fake"); !ok || err != nil {
		t.Fatalf("refresh: %v %v", ok, err)
	}
	var posted int64
	e.db.R.QueryRow("SELECT coalesce(posted_at, 0) FROM files WHERE id = ?", file).Scan(&posted)
	if posted != postedAt(2).UnixMilli() {
		t.Fatalf("upload time %d, want %d", posted, postedAt(2).UnixMilli())
	}
}
