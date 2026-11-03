package download

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
)

func setYear(t *testing.T, e *env, y string) {
	t.Helper()
	if err := e.lib.SetSettings(context.Background(), map[string]json.RawMessage{"skipUploadedYear": json.RawMessage(y)}); err != nil {
		t.Fatal(err)
	}
}

// The fake site's posts are all from 2026. A year before that stops nothing; 2026 skips them without fetching a file;
// taking the limit back lets the same posts download.
func TestAgeLimit(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	count := func() (n int) {
		e.db.R.QueryRow("SELECT count(*) FROM seen_posts WHERE outcome = ?", TooOld).Scan(&n)
		return n
	}
	setYear(t, e, "2026")
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(ctx, id)
	if s.State != StateDone || s.Downloaded != 0 || s.Skipped != 5 || len(e.fb.fileHits) != 0 || count() != 5 {
		t.Fatalf("with the limit: %+v, %d files fetched, %d too old", s, len(e.fb.fileHits), count())
	}
	// Raised, they stay too old; lowered, they're forgotten.
	setYear(t, e, "2030")
	if count() != 5 {
		t.Fatalf("raised: %d too old", count())
	}
	setYear(t, e, "2025")
	if count() != 0 {
		t.Fatalf("lowered: %d too old", count())
	}
	id, _ = e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if s, _ = e.dl.Get(ctx, id); s.Downloaded != 5 {
		t.Fatalf("lowered: %+v", s)
	}
	// Files it has stay, whatever the limit: the site's post is still added to them.
	setYear(t, e, "2026")
	id, _ = e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if s, _ = e.dl.Get(ctx, id); s.Already != 5 || s.Skipped != 0 {
		t.Fatalf("files it has: %+v", s)
	}
}

// A run of old posts ends the search: older ones follow. Not a favorites list, which goes through whole.
func TestAgeLimitStopsTheSearch(t *testing.T) {
	defer func(n int) { oldRunStop = n }(oldRunStop)
	oldRunStop = 5
	e := newEnv(t, 40)
	ctx := context.Background()
	setYear(t, e, "2026")
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(ctx, id)
	if s.State != StateDone || s.Handled < 5 || s.Handled >= 40 || len(e.fb.fileHits) != 0 {
		t.Fatalf("%+v, %d files fetched", s, len(e.fb.fileHits))
	}
}

// A post from a list with no dates: too old last time, it isn't opened again while the limit stands.
func TestAgeLimitNoRequestForASkippedPost(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	st := e.dl.Site("fake")
	rules := library.DownloadRules{UploadedBefore: library.YearEnd(2026)}
	if _, err := e.db.W.Exec("INSERT INTO seen_posts (site_id, post_id, outcome, seen_at) VALUES (?, 3, ?, 1)", st.ID, TooOld); err != nil {
		t.Fatal(err)
	}
	calls := e.fb.apiCalls
	outcome, _, err := e.dl.handle(ctx, st, plugin.Post{ID: 3, Partial: true}, nil, rules, true)
	if err != nil || outcome != TooOld || e.fb.apiCalls != calls {
		t.Fatalf("outcome %d (%v), %d requests", outcome, err, e.fb.apiCalls-calls)
	}
	// No need to open it ahead either.
	if e.dl.needsPage(ctx, st, plugin.Post{ID: 3, Partial: true}) {
		t.Fatal("its page would be opened ahead")
	}
}

// The limit's edges, and a post with no date: not too old.
func TestTooOld(t *testing.T) {
	rules := library.DownloadRules{UploadedBefore: library.YearEnd(2019)}
	for _, c := range []struct {
		at  time.Time
		old bool
	}{
		{time.Date(2019, 12, 31, 23, 59, 59, 0, time.UTC), true},
		{time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{time.Time{}, false},
	} {
		if rules.TooOld(c.at) != c.old {
			t.Errorf("%v: too old %v", c.at, !c.old)
		}
	}
	if (library.DownloadRules{}).TooOld(time.Unix(1, 0)) {
		t.Error("no limit: too old")
	}
}
