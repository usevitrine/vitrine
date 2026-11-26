package download

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// Counting waiting searches: each gets its site's total (one request, nothing downloaded); one counted already isn't
// asked again; the page hears of each (Changed).
func TestCountSearches(t *testing.T) {
	e := newEnv(t, 37)
	ctx := context.Background()
	a, _ := e.dl.Add(ctx, "fake", "common", 0)
	b, _ := e.dl.Add(ctx, "fake", "tag_3", 0)
	changes, stop := e.dl.Watch()
	defer stop()
	n, err := e.dl.CountSearches(ctx, nil)
	if err != nil || n != 2 {
		t.Fatalf("counting %d, %v; want 2", n, err)
	}
	total := func(id int64) int {
		s, err := e.dl.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return s.SiteTotal
	}
	deadline := time.Now().Add(5 * time.Second)
	for (total(a) < 0 || total(b) < 0) && time.Now().Before(deadline) {
		select {
		case <-changes:
		case <-time.After(50 * time.Millisecond):
		}
	}
	if total(a) != 37 || total(b) != 37 { // (the fake site answers every search with all its posts)
		t.Fatalf("totals %d and %d, want 37", total(a), total(b))
	}
	if e.count(t, "") != 0 {
		t.Fatal("counting downloaded files")
	}
	if n, _ := e.dl.CountSearches(ctx, nil); n != 0 {
		t.Fatalf("counting %d searches counted already", n)
	}
	for len(e.dl.Counting()) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := e.dl.Counting(); len(got) != 0 {
		t.Fatalf("still counting: %v", got)
	}
}

// Counting while a search of that site downloads (2026-11-17): the waiting searches are counted all the same, while its
// files are in flight.
func TestCountSearchesWhileRunning(t *testing.T) {
	e := newEnv(t, 37)
	ctx := context.Background()
	block := make(chan struct{})
	e.fb.mu.Lock()
	e.fb.block = block // (the running search's files wait)
	e.fb.mu.Unlock()
	run, _ := e.dl.Add(ctx, "fake", "common", 0)
	wait, _ := e.dl.Add(ctx, "fake", "tag_3", 0)
	done := make(chan error, 1)
	go func() { done <- e.dl.RunQueue(ctx, "fake") }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := e.dl.Get(ctx, run); s.State == StateRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n, err := e.dl.CountSearches(ctx, nil); err != nil || n != 1 {
		t.Fatalf("counting %d, %v; want 1 (the waiting one)", n, err)
	}
	for time.Now().Before(deadline) {
		if s, _ := e.dl.Get(ctx, wait); s.SiteTotal >= 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, _ := e.dl.Get(ctx, wait)
	close(block)
	<-done
	if s.SiteTotal != 37 {
		t.Fatalf("the waiting search's count %d while another downloaded, want 37", s.SiteTotal)
	}
}

// The queue sorted by the searches' counts runs that way (2026-11-17; before, the sort was only a view): fewest (or
// most) first, ones not counted last; a running download steps aside for one the sort now puts first. (The fake site
// gives every search all its posts as its total - 25 here, set again by a search's own first page - so the counts below
// stay apart from it; the one without a count is one the site doesn't say for (-2): sorted, a new search is counted in
// the background.)
func TestQueueSortedByCount(t *testing.T) {
	e := newEnv(t, 25)
	ctx := context.Background()
	var mu sync.Mutex
	var runs []int64 // each search in turn as its posts come
	e.dl.OnPost = func(s Search, p plugin.Post, outcome int, err error) {
		mu.Lock()
		defer mu.Unlock()
		if len(runs) == 0 || runs[len(runs)-1] != s.ID {
			runs = append(runs, s.ID)
		}
	}
	add := func(totals ...int) []int64 {
		var ids []int64
		for i, n := range totals {
			id, err := e.dl.Add(ctx, "fake", fmt.Sprintf("tag_%d", i+1), 0)
			if err != nil {
				t.Fatal(err)
			}
			e.db.W.ExecContext(ctx, "UPDATE searches SET site_total = ? WHERE id = ?", n, id)
			ids = append(ids, id)
		}
		return ids
	}
	run := func(sort string, want func(ids []int64) []int64, totals ...int) {
		t.Helper()
		e.dl.SetQueueSort(sort)
		ids := add(totals...)
		mu.Lock()
		runs = nil
		mu.Unlock()
		if err := e.dl.RunQueue(ctx, "fake"); err != nil {
			t.Fatal(err)
		}
		if w := want(ids); !slices.Equal(runs, w) {
			t.Fatalf("sort %q: ran %v, want %v", sort, runs, w)
		}
	}
	// Fewest first: 4 (25 once it runs), 50, 900, then the one not counted.
	run("files_asc", func(x []int64) []int64 { return []int64{x[1], x[0], x[3], x[2]} }, 50, 4, -2, 900)
	// Most first: 900, 500 (25 once it runs - still ahead of 10), 10, not counted.
	run("files", func(x []int64) []int64 { return []int64{x[3], x[0], x[1], x[2]} }, 500, 10, -2, 900)
	// Queue order: as added.
	run("", func(x []int64) []int64 { return x }, 500, 10, -2, 900)

	// While one runs: it steps aside when the sort puts a waiting one first.
	ids := add(50, 4)
	e.db.W.ExecContext(ctx, "UPDATE searches SET state = 'running' WHERE id = ?", ids[0])
	site := e.dl.Site("fake").ID
	for _, x := range []struct {
		sort  string
		aside bool
	}{{"", false}, {"files", false}, {"files_asc", true}} {
		e.dl.SetQueueSort(x.sort)
		if got := e.dl.aheadWaiting(ctx, site, ids[0]); got != x.aside {
			t.Fatalf("sort %q: the running one (50) steps aside for 4: %v, want %v", x.sort, got, x.aside)
		}
	}
}

// Sorted by count, a search added is counted on its site in the background - not only when the Downloads page asks
// (2026-11-17); in queue order, nothing is asked. A check isn't counted (it's not sorted by it).
func TestQueueSortedCountsNewSearches(t *testing.T) {
	e := newEnv(t, 37)
	ctx := context.Background()
	total := func(id int64) int {
		s, err := e.dl.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return s.SiteTotal
	}
	plain, _ := e.dl.Add(ctx, "fake", "common", 0)
	time.Sleep(200 * time.Millisecond)
	if n := total(plain); n != -1 {
		t.Fatalf("queue order: counted %d, want -1 (not asked)", n)
	}
	e.dl.SetQueueSort("files_asc") // (picking it counts what waits)
	added, _ := e.dl.Add(ctx, "fake", "tag_3", 0)
	deadline := time.Now().Add(5 * time.Second)
	for (total(plain) < 0 || total(added) < 0) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if total(plain) != 37 || total(added) != 37 {
		t.Fatalf("sorted: totals %d and %d, want 37 (counted without the page)", total(plain), total(added))
	}
	for len(e.dl.Counting()) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}
