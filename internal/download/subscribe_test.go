package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/search"
)

// clock lets a test move the downloader's time forward.
func (e *env) clock() func(time.Duration) {
	now := time.Now()
	e.dl.now = func() time.Time { return now }
	return func(d time.Duration) { now = now.Add(d) }
}

// runDue starts the due checks and works the queue.
func (e *env) runDue(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := e.dl.StartDue(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.dl.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
}

func (e *env) sub(t *testing.T, id int64) Subscription {
	t.Helper()
	s, err := e.dl.GetSubscription(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *env) searchCount(t *testing.T) int {
	t.Helper()
	l, err := e.dl.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(l)
}

func TestSubscriptionChecks(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	advance := e.clock()
	id, err := e.dl.Subscribe(ctx, "fake", "common", 2*time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.dl.Subscribe(ctx, "fake", " common ", 2*time.Hour, 0); !errors.Is(err, ErrExists) {
		t.Fatalf("subscribing twice: %v", err)
	}
	if _, err := e.dl.Subscribe(ctx, "fake", "x", time.Minute, 0); err == nil {
		t.Fatal("a one-minute interval was accepted")
	}

	// The first check goes through the whole search.
	e.runDue(t)
	s := e.sub(t, id)
	if s.Checks != 1 || s.Downloaded != 24 || s.LastNew != 24 || s.highID != 25 || s.Check != nil || s.Error != "" {
		t.Fatalf("after the first check: %+v", s)
	}
	if n := e.searchCount(t); n != 0 {
		t.Fatalf("%d searches left after the check", n)
	}
	if want := e.dl.now().Add(2 * time.Hour).UnixMilli(); s.NextCheckAt != want {
		t.Fatalf("next check at %d, want %d", s.NextCheckAt, want)
	}

	// Not due yet: nothing happens.
	e.fb.upload(3)
	e.runDue(t)
	if s := e.sub(t, id); s.Checks != 1 {
		t.Fatalf("checked before it was due: %+v", s)
	}

	// Due: only the 3 new posts are looked at - one page, no older file fetched again.
	advance(2 * time.Hour)
	calls, hits := e.fb.apiCalls, len(e.fb.fileHits)
	e.runDue(t)
	s = e.sub(t, id)
	if s.Checks != 2 || s.LastNew != 3 || s.Downloaded != 27 || s.highID != 28 {
		t.Fatalf("after the second check: %+v", s)
	}
	if e.fb.apiCalls-calls != 1 || len(e.fb.fileHits)-hits != 3 {
		t.Fatalf("second check: %d API calls, %d files fetched", e.fb.apiCalls-calls, len(e.fb.fileHits)-hits)
	}
	if n := e.count(t, ""); n != 27 {
		t.Fatalf("%d files, want 27", n)
	}
	// The first check came in bulk; the second check's 3 files did not ("new" in the strict sense).
	var bulk, regular int
	if err := e.dl.db.R.QueryRow("SELECT count(*) FILTER (WHERE bulk = 1), count(*) FILTER (WHERE bulk = 0) FROM files").Scan(&bulk, &regular); err != nil {
		t.Fatal(err)
	}
	if bulk != 24 || regular != 3 {
		t.Fatalf("bulk %d, regular %d; want 24, 3", bulk, regular)
	}

	// Nothing new: a check still counts, with 0 new.
	advance(2 * time.Hour)
	e.runDue(t)
	if s := e.sub(t, id); s.Checks != 3 || s.LastNew != 0 || s.highID != 28 {
		t.Fatalf("empty check: %+v", s)
	}
}

func TestSubscriptionInitialMax(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	advance := e.clock()
	id, _ := e.dl.Subscribe(ctx, "fake", "common", time.Hour, 5)
	e.runDue(t)
	if s := e.sub(t, id); s.Downloaded != 5 || s.highID != 25 {
		t.Fatalf("first check: %+v", s)
	}
	e.fb.upload(2)
	advance(time.Hour)
	e.runDue(t)
	if s := e.sub(t, id); s.LastNew != 2 || s.Downloaded != 7 {
		t.Fatalf("second check: %+v", s)
	}
	if n := e.count(t, ""); n != 7 {
		t.Fatalf("%d files, want 7 (older posts are never fetched)", n)
	}
}

func TestSubscribeSearch(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	e.clock()

	// A finished search: the subscription starts where it stopped.
	sid, _ := e.dl.Add(ctx, "fake", "common", 10)
	e.dl.RunQueue(ctx, "fake")
	id, err := e.dl.SubscribeSearch(ctx, sid, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := e.dl.Get(ctx, sid); s.State != StateDone {
		t.Fatalf("the search changed: %+v", s)
	}
	calls := e.fb.apiCalls
	e.runDue(t)
	if s := e.sub(t, id); s.Checks != 1 || s.LastNew != 0 || s.highID != 25 || e.fb.apiCalls-calls != 1 {
		t.Fatalf("check after a finished search: %+v, %d calls", s, e.fb.apiCalls-calls)
	}
	if _, err := e.dl.SubscribeSearch(ctx, sid, time.Hour); !errors.Is(err, ErrExists) {
		t.Fatalf("subscribing twice: %v", err)
	}

	// A search that hasn't started is replaced by the first check (keeping its max).
	sid2, _ := e.dl.Add(ctx, "fake", "tag_3", 4)
	id2, err := e.dl.SubscribeSearch(ctx, sid2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.dl.Get(ctx, sid2); err == nil {
		t.Fatal("the unstarted search is still there")
	}
	if s := e.sub(t, id2); s.InitialMax != 4 || s.highID != 0 {
		t.Fatalf("subscription from an unstarted search: %+v", s)
	}
}

func TestSubscriptionPauseAndRemove(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	advance := e.clock()
	id, _ := e.dl.Subscribe(ctx, "fake", "common", 3*time.Hour, 0)
	if err := e.dl.StartDue(ctx); err != nil {
		t.Fatal(err)
	}

	// Pausing the subscription pauses its check; nothing downloads.
	paused, resumed := true, false
	if err := e.dl.UpdateSubscription(ctx, id, SubscriptionChange{Paused: &paused}); err != nil {
		t.Fatal(err)
	}
	e.runDue(t)
	s := e.sub(t, id)
	if !s.Paused || s.Check == nil || s.Check.State != StatePaused || s.Check.Handled != 0 {
		t.Fatalf("paused: %+v %+v", s, s.Check)
	}
	if err := e.dl.UpdateSubscription(ctx, id, SubscriptionChange{Paused: &resumed}); err != nil {
		t.Fatal(err)
	}
	e.runDue(t)
	if s := e.sub(t, id); s.Checks != 1 || s.Downloaded != 24 {
		t.Fatalf("resumed: %+v", s)
	}

	// Removing a check skips it: the next one is a full interval later.
	advance(3 * time.Hour)
	if err := e.dl.StartDue(ctx); err != nil {
		t.Fatal(err)
	}
	c := e.sub(t, id).Check
	if c == nil {
		t.Fatal("no check started")
	}
	if err := e.dl.Remove(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if s := e.sub(t, id); s.Check != nil || s.NextCheckAt != e.dl.now().Add(3*time.Hour).UnixMilli() {
		t.Fatalf("after removing the check: %+v", s)
	}

	// Check now ignores the schedule.
	e.fb.upload(1)
	if err := e.dl.CheckNow(ctx, id); err != nil {
		t.Fatal(err)
	}
	e.dl.RunQueue(ctx, "fake")
	if s := e.sub(t, id); s.Checks != 2 || s.LastNew != 1 {
		t.Fatalf("check now: %+v", s)
	}

	// A longer interval moves the next check.
	day := 24 * time.Hour
	e.dl.UpdateSubscription(ctx, id, SubscriptionChange{Interval: &day})
	if s := e.sub(t, id); s.Interval != 86400 || s.NextCheckAt != s.LastCheckAt+86400_000 {
		t.Fatalf("interval: %+v", s)
	}

	// Unsubscribing removes it and its check; the files stay.
	e.dl.CheckNow(ctx, id)
	if err := e.dl.Unsubscribe(ctx, id); err != nil {
		t.Fatal(err)
	}
	if n := e.searchCount(t); n != 0 {
		t.Fatalf("%d searches left", n)
	}
	if _, err := e.dl.GetSubscription(ctx, id); !errors.Is(err, ErrNoSubscription) {
		t.Fatalf("still there: %v", err)
	}
	if n := e.count(t, ""); n != 25 {
		t.Fatalf("%d files, want 25", n)
	}
}

func TestSubscriptionCheckFails(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	advance := e.clock()
	id, _ := e.dl.Subscribe(ctx, "fake", "common", 6*time.Hour, 0)
	e.fb.broken = true // the site answers nonsense
	e.runDue(t)
	s := e.sub(t, id)
	if s.Error == "" || s.Check == nil || s.Check.State != StateFailed || s.Checks != 0 {
		t.Fatalf("failed check: %+v", s)
	}
	// Retried after an hour (shorter than the interval).
	if want := e.dl.now().Add(time.Hour).UnixMilli(); s.NextCheckAt != want {
		t.Fatalf("retry at %d, want %d", s.NextCheckAt, want)
	}
	advance(time.Hour)
	if err := e.dl.StartDue(ctx); err != nil {
		t.Fatal(err)
	}
	if s := e.sub(t, id); s.Check == nil || s.Check.State != StateQueued {
		t.Fatalf("not retried: %+v", s.Check)
	}
}

// Checks are scheduled a little late at random (up to a tenth of the
// interval, at most 30 min), so subscriptions added together drift apart.
func TestCheckJitter(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	advance := e.clock()
	f := 0.5
	e.dl.jitter = func() float64 { return f }
	hourly, _ := e.dl.Subscribe(ctx, "fake", "common", time.Hour, 0)
	daily, _ := e.dl.Subscribe(ctx, "fake", "tag_1", 24*time.Hour, 0)
	e.runDue(t)
	now := e.dl.now()
	if got, want := e.sub(t, hourly).NextCheckAt, now.Add(time.Hour+3*time.Minute).UnixMilli(); got != want {
		t.Fatalf("hourly: next check %d, want %d (+3 min)", got, want)
	}
	if got, want := e.sub(t, daily).NextCheckAt, now.Add(24*time.Hour+15*time.Minute).UnixMilli(); got != want {
		t.Fatalf("daily: next check %d, want %d (+15 min: half of the 30 min cap)", got, want)
	}
	// Different draws: two subscriptions checked together come due apart.
	draws := []float64{0.1, 0.9}
	e.dl.jitter = func() float64 { v := draws[0]; draws = draws[1:]; return v }
	advance(25 * time.Hour)
	e.runDue(t)
	a, b := e.sub(t, hourly).NextCheckAt, e.sub(t, daily).NextCheckAt
	if a == 0 || b == 0 || a == b {
		t.Fatalf("not spread: %d %d", a, b)
	}
}

// A due check waits behind downloads already queued; Check now goes first. A
// first check (maybe the whole search) is always last.
func TestCheckQueuePlace(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	advance := e.clock()
	sid, _ := e.dl.Subscribe(ctx, "fake", "common", time.Hour, 0)
	e.runDue(t) // the first check, done
	order := func() []string {
		list, _ := e.dl.List(ctx)
		var out []string
		for _, s := range list {
			if s.State == StateQueued {
				kind := "download"
				if s.SubscriptionID != 0 {
					kind = "check"
				}
				out = append(out, kind)
			}
		}
		return out
	}
	e.dl.Add(ctx, "fake", "common", 0)
	advance(2 * time.Hour)
	if err := e.dl.StartDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := order(); !slices.Equal(got, []string{"download", "check"}) {
		t.Fatalf("due check: %v, want it after the download", got)
	}
	// Check now: first in line.
	if c, ok, _ := e.dl.check(ctx, sid); ok {
		e.dl.Remove(ctx, c.ID)
	}
	if err := e.dl.CheckNow(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if got := order(); !slices.Equal(got, []string{"check", "download"}) {
		t.Fatalf("check now: %v, want it first", got)
	}
}

// A subscription to a user's favorites: each check goes through all of them - a
// post favorited since can be older than the newest one the last check saw.
func TestFavoritesSubscription(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	advance := e.clock()
	e.fb.mu.Lock()
	e.fb.favs = []int64{10, 20}
	e.fb.mu.Unlock()
	id, err := e.dl.SubscribeTo(ctx, "fake", "fav:42", 2*time.Hour, 0, true) // and add them to the library's favorites
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := e.dl.GetGroup(ctx, e.sub(t, id).GroupID); g.Name != FavoritesGroup {
		t.Fatalf("in group %q, want its own %q", g.Name, FavoritesGroup)
	}
	e.runDue(t)
	if s := e.sub(t, id); s.Downloaded != 2 || s.Error != "" {
		t.Fatalf("after the first check: %+v", s)
	}
	e.fb.mu.Lock()
	e.fb.favs = []int64{22, 5, 20, 10} // favorited since: a newer post and an older one
	e.fb.mu.Unlock()
	advance(2 * time.Hour)
	e.runDue(t)
	if s := e.sub(t, id); s.Downloaded != 4 || s.LastNew != 2 {
		t.Fatalf("after the second check: %+v", s)
	}
	if n := e.count(t, "tag_5"); n != 1 {
		t.Fatalf("the older favorite: %d files", n)
	}
	if n := e.count(t, "system:favorite"); n != 4 {
		t.Fatalf("%d favorites in the library, want the 4 downloaded", n)
	}
	// Removed from the site's favorites: the next complete check marks them gone,
	// and Downloads asks - keep them (not asked again)...
	setFavs := func(ids ...int64) {
		e.fb.mu.Lock()
		e.fb.favs = ids
		e.fb.mu.Unlock()
	}
	check := func() {
		t.Helper()
		advance(2 * time.Hour)
		e.runDue(t)
	}
	setFavs(22, 5)
	check()
	if gone, _ := e.dl.FavGone(ctx, id); len(gone) != 2 || e.sub(t, id).FavGone != 2 {
		t.Fatalf("gone after 10 and 20 left the favorites: %v (%d)", gone, e.sub(t, id).FavGone)
	}
	if err := e.dl.FavGoneKeep(ctx, id); err != nil {
		t.Fatal(err)
	}
	if gone, _ := e.dl.FavGone(ctx, id); len(gone) != 0 {
		t.Fatalf("kept, still gone: %v", gone)
	}
	// ... or delete them (the undo window, like any delete).
	setFavs(22)
	check()
	if gone, _ := e.dl.FavGone(ctx, id); len(gone) != 1 {
		t.Fatalf("gone after 5 left: %v", gone)
	}
	if n, err := e.dl.FavGoneDelete(ctx, id); err != nil || n != 1 || e.count(t, "tag_5") != 0 {
		t.Fatalf("delete: %d %v (tag_5: %d files)", n, err, e.count(t, "tag_5"))
	}
	// An empty list (the site failing, more likely than every favorite removed) marks nothing.
	setFavs()
	check()
	if gone, _ := e.dl.FavGone(ctx, id); len(gone) != 0 {
		t.Fatalf("an empty list marked %v gone", gone)
	}
	// Without the setting: nothing becomes a favorite.
	if _, err := e.dl.Add(ctx, "fake", "tag_1", 0); err != nil {
		t.Fatal(err)
	}
	e.runDue(t)
	if n := e.count(t, "system:favorite"); n != 3 { // (the 4, less the one deleted)
		t.Fatalf("%d favorites after a plain download", n)
	}
}

func TestCheckDepth(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	advance := e.clock()
	e.fb.mu.Lock()
	e.fb.hidden[20], e.fb.hidden[5] = true, true // not in the search yet
	e.fb.mu.Unlock()
	id, err := e.dl.Subscribe(ctx, "fake", "common", time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	e.runDue(t)
	if s := e.sub(t, id); s.Downloaded != 22 || s.Depth != 0 {
		t.Fatalf("first check: %+v", s)
	}
	// Both tagged since (older than the newest post the check saw). Depth 0: missed.
	e.fb.mu.Lock()
	clear(e.fb.hidden)
	e.fb.mu.Unlock()
	e.fb.upload(1)
	advance(time.Hour)
	e.runDue(t)
	if s := e.sub(t, id); s.LastNew != 1 {
		t.Fatalf("depth 0 check: %+v", s)
	}
	// The setting for all: 10 posts past it finds 20, not 5.
	if err := e.lib.SetSettings(ctx, map[string]json.RawMessage{"checkDepth": json.RawMessage("10")}); err != nil {
		t.Fatal(err)
	}
	if s := e.sub(t, id); s.Depth != 10 {
		t.Fatalf("depth from the setting for all: %d", s.Depth)
	}
	advance(time.Hour)
	e.runDue(t)
	if s := e.sub(t, id); s.LastNew != 1 || e.count(t, "tag_20") != 1 || e.count(t, "tag_5") != 0 {
		t.Fatalf("depth 10 check: %+v", s)
	}
	// Site, then group, then its own: each wins over the one before.
	if err := e.lib.SetSettings(ctx, map[string]json.RawMessage{"siteCheckDepths": json.RawMessage(`{"fake":3}`)}); err != nil {
		t.Fatal(err)
	}
	if s := e.sub(t, id); s.Depth != 3 {
		t.Fatalf("depth from the site: %d", s.Depth)
	}
	g, err := e.dl.CreateGroup(ctx, "fake", "Some group")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dl.SetGroup(ctx, id, g); err != nil {
		t.Fatal(err)
	}
	if err := e.dl.SetGroupCheckDepth(ctx, g, 7); err != nil {
		t.Fatal(err)
	}
	if s := e.sub(t, id); s.Depth != 7 {
		t.Fatalf("depth from the group: %d", s.Depth)
	}
	n := 100
	if err := e.dl.UpdateSubscription(ctx, id, SubscriptionChange{CheckDepth: &n}); err != nil {
		t.Fatal(err)
	}
	if s := e.sub(t, id); s.Depth != 100 || s.CheckDepth == nil {
		t.Fatalf("its own depth: %+v", s)
	}
	advance(time.Hour)
	e.runDue(t)
	if s := e.sub(t, id); s.LastNew != 1 || e.count(t, "tag_5") != 1 {
		t.Fatalf("depth 100 check: %+v", s)
	}
	// Back to the group's.
	n = -1
	if err := e.dl.UpdateSubscription(ctx, id, SubscriptionChange{CheckDepth: &n}); err != nil {
		t.Fatal(err)
	}
	if s := e.sub(t, id); s.Depth != 7 || s.CheckDepth != nil {
		t.Fatalf("back to the group's: %+v", s)
	}
	if err := e.lib.SetSettings(ctx, map[string]json.RawMessage{"checkDepth": json.RawMessage("-1")}); err == nil {
		t.Fatal("a negative depth was accepted")
	}
}

// A site's pause is kept over a restart: a new downloader (the same library) starts with the site still paused, its
// next try as it was; it answering again clears what was kept.
func TestSitePauseKeptOverRestart(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.clock()
	e.dl.SetPauseAfter(1)
	e.fb.mu.Lock()
	e.fb.down = true
	e.fb.mu.Unlock()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	p := e.dl.Site("fake").Client.Paused()
	if p.Until.IsZero() {
		t.Fatal("not paused")
	}

	m := e.dl.Site("fake").Client.Manifest()
	dl2, err := New(ctx, e.lib, e.db, map[string]plugin.Manifest{"fake": m}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	q := dl2.Site("fake").Client.Paused()
	if !q.Until.Equal(p.Until.Truncate(time.Millisecond)) || q.Reason != p.Reason || q.Since.IsZero() {
		t.Fatalf("after a restart: %+v, want %+v", q, p)
	}

	// Back: the next try answers, the pause ends - and isn't kept any more.
	dl2.Site("fake").Client.SetHTTPClient(e.fb.srv.Client())
	e.fb.mu.Lock()
	e.fb.down = false
	e.fb.mu.Unlock()
	dl2.TryNow("fake")
	if err := dl2.RunQueue(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	if !dl2.Site("fake").Client.Paused().Until.IsZero() {
		t.Fatal("still paused")
	}
	if kept, _ := e.lib.SitePauses(ctx); len(kept) != 0 {
		t.Fatalf("still kept: %+v", kept)
	}
}

// The list says when each subscription last brought in a new file (the Downloads page marks quiet ones).
func TestSubscriptionLastFound(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	advance := e.clock()
	id, err := e.dl.Subscribe(ctx, "fake", "common", 2*time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := func() int64 {
		list, err := e.dl.Subscriptions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range list {
			if s.ID == id {
				return s.LastFoundAt
			}
		}
		t.Fatal("not listed")
		return 0
	}
	if f := found(); f != 0 {
		t.Fatalf("found at %d before any check", f)
	}
	e.runDue(t)
	first := e.dl.now().UnixMilli()
	if f := found(); f != first {
		t.Fatalf("found at %d, want %d", f, first)
	}
	advance(2 * time.Hour) // a check that finds nothing new
	e.runDue(t)
	if s := e.sub(t, id); s.Checks != 2 {
		t.Fatalf("not checked again: %+v", s)
	}
	if f := found(); f != first {
		t.Fatalf("found at %d after a check with nothing new, want %d", f, first)
	}
}

// A subscription check that comes up while a big download runs goes first: the download steps aside after the post
// in flight, goes back in the queue, and carries on from where it was once the check is done (Settings > Downloads;
// off: the check waits its turn). A subscription's first check (it takes everything) is a big download too.
func TestChecksGoAheadOfDownloads(t *testing.T) {
	for _, bigSub := range []bool{false, true} {
		for _, first := range []bool{true, false} {
			e := newEnv(t, 25)
			ctx := context.Background()
			e.dl.asideEvery = 0
			e.dl.SetChecksFirst(first)
			// The subscription whose check comes up: its first check done already (a check for what's new stops at its newest post). Posts
			// come in after it: the check has some to get.
			e.fb.n = 20
			subID, err := e.dl.Subscribe(ctx, "fake", "rare", 2*time.Hour, 3)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.dl.CheckNow(ctx, subID); err != nil {
				t.Fatal(err)
			}
			if err := e.dl.RunQueue(ctx, "fake"); err != nil {
				t.Fatal(err)
			}
			e.fb.n = 25
			var mu sync.Mutex
			var order []int64 // the search of each post handled, in turn
			e.dl.OnPost = func(s Search, p plugin.Post, outcome int, err error) {
				mu.Lock()
				order = append(order, s.ID)
				n := len(order)
				mu.Unlock()
				if n == 3 { // the download under way: a check comes up
					if err := e.dl.CheckNow(ctx, subID); err != nil {
						t.Error(err)
					}
				}
			}
			var big, bigSubID int64
			if bigSub {
				id, err := e.dl.Subscribe(ctx, "fake", "common", 2*time.Hour, 0)
				if err != nil {
					t.Fatal(err)
				}
				bigSubID = id
				if err := e.dl.CheckNow(ctx, id); err != nil {
					t.Fatal(err)
				}
				if err := e.db.R.QueryRowContext(ctx, "SELECT id FROM searches WHERE subscription_id = ?", id).Scan(&big); err != nil {
					t.Fatal(err)
				}
			} else {
				big, _ = e.dl.Add(ctx, "fake", "common", 0)
			}
			if err := e.dl.RunQueue(ctx, "fake"); err != nil {
				t.Fatal(err)
			}
			what := fmt.Sprintf("checksFirst=%v, a first check=%v", first, bigSub)
			if bigSub { // (a check done: its search goes)
				if sub := e.sub(t, bigSubID); sub.Checks != 1 || sub.Downloaded == 0 {
					t.Fatalf("%s: the first check: %+v", what, sub)
				}
			} else if s, _ := e.dl.Get(ctx, big); s.State != StateDone || s.Handled == 0 {
				t.Fatalf("%s: the download: %+v", what, s)
			}
			if sub := e.sub(t, subID); sub.Checks != 2 {
				t.Fatalf("%s: the check didn't run: %+v", what, sub)
			}
			// Where the check's posts came: before the download's last one (first), or after all of them (not).
			lastBig, firstCheck := -1, -1
			for i, id := range order {
				if id == big {
					lastBig = i
				} else if firstCheck < 0 {
					firstCheck = i
				}
			}
			if first && (firstCheck < 0 || firstCheck > lastBig) {
				t.Fatalf("%s: the check came at %d, the download's last post at %d", what, firstCheck, lastBig)
			}
			if !first && firstCheck >= 0 && firstCheck < lastBig {
				t.Fatalf("%s: the check came at %d, before the download's last post at %d", what, firstCheck, lastBig)
			}
		}
	}
}

// A subscription or search named (a gallery's title for its link); one without a name, to a gallery the library
// has: the comic's title.
func TestLabels(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	e.clock()
	id, err := e.dl.Subscribe(ctx, "fake", "https://site.test/g/1/abc/", time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dl.LabelSubscription(ctx, id, "Some Comic"); err != nil {
		t.Fatal(err)
	}
	if s, _ := e.dl.GetSubscription(ctx, id); s.Label != "Some Comic" || s.Query != "https://site.test/g/1/abc/" {
		t.Fatalf("labeled: %+v", s)
	}
	sid, _ := e.dl.Add(ctx, "fake", "https://site.test/g/2/def/", 0)
	if s, _ := e.dl.Get(ctx, sid); s.Label != "" {
		t.Fatalf("no name, no comic: %q", s.Label)
	}
	if _, _, err := e.lib.AddComic(ctx, library.ComicInfo{Title: "Other Comic", Site: "fake", Gallery: "2", URL: "https://site.test/g/2/def"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := e.dl.Get(ctx, sid); s.Label != "Other Comic" {
		t.Fatalf("the comic's title: %q", s.Label)
	}
}

// What the Downloads page shows of a subscription: its newest files, how many came since a time; and
// system:found_by= finds the files it downloaded.
func TestSubscriptionLooks(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	e.clock()
	before := e.dl.now().UnixMilli() - 1
	id, err := e.dl.Subscribe(ctx, "fake", "common", 2*time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	e.runDue(t)
	looks, err := e.dl.SubscriptionLooks(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	l := looks[id]
	if l == nil || len(l.Recent) != 4 || l.Fresh != 24 || l.Comic != 0 {
		t.Fatalf("looks: %+v", l)
	}
	if looks, _ := e.dl.SubscriptionLooks(ctx, 0); looks[id].Fresh != 0 {
		t.Fatalf("since nothing: %d fresh", looks[id].Fresh)
	}
	// A subscription whose posts were all in the library already (downloaded by the first): its newest still show.
	other, err := e.dl.Subscribe(ctx, "fake", "rare", 2*time.Hour, 0)
	if err != nil {
		t.Fatal(err)
	}
	e.runDue(t)
	if looks, _ := e.dl.SubscriptionLooks(ctx, 0); len(looks[other].Recent) != 4 || looks[other].Recent[0] != l.Recent[0] {
		t.Fatalf("had them all: %+v (the first: %v)", looks[other], l.Recent)
	}
	q, err := search.Parse("system:found_by=" + strconv.FormatInt(id, 10))
	if err != nil {
		t.Fatal(err)
	}
	// (the card's pictures: its newest posts - found by it too)
	if res := e.lib.Index().Search(q, time.Now()); res.Total != 24 || !slices.Contains(res.IDs, l.Recent[0]) {
		t.Fatalf("found_by: %d files, without the newest %d", res.Total, l.Recent[0])
	}
	var newest uint32
	e.db.R.QueryRowContext(ctx, `SELECT f.id FROM file_sources fs JOIN files f ON f.id = fs.file_id WHERE fs.subscription_id = ?
		ORDER BY f.posted_at DESC, f.id DESC LIMIT 1`, id).Scan(&newest)
	if l.Recent[0] != newest {
		t.Fatalf("the first picture: file %d, the newest post: file %d", l.Recent[0], newest)
	}
	q, _ = search.Parse("system:found_by=999")
	if res := e.lib.Index().Search(q, time.Now()); res.Total != 0 {
		t.Fatalf("another subscription's: %d", res.Total)
	}
}
