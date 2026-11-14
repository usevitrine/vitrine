package download

import (
	"context"
	"testing"
	"time"
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
