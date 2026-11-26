package library

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Newest first, a search run again moves up instead of showing twice (its case and spaces aside; another order is
// another search), each page has its own, only the newest MaxRecent are kept, one can be taken out, all cleared.
func TestRecentSearches(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	clock := time.UnixMilli(1_000_000)
	l.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	add := func(q, sort, scope string) {
		t.Helper()
		if err := l.AddRecentSearch(ctx, q, sort, scope); err != nil {
			t.Fatal(err)
		}
	}
	queries := func(scope string) []string {
		t.Helper()
		list, err := l.RecentSearches(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range list {
			out = append(out, r.Query+"|"+r.Sort)
		}
		return out
	}

	add("blue_sky", "", "")
	add("some_artist  -cat", "", "")
	add("blue_sky", "uploaded", "") // (another order: another search)
	add(" Blue_Sky ", "", "")       // (the first again)
	add("blue_sky", "", "comics")   // (the Comics page's own)
	if got := fmt.Sprint(queries("")); got != "[Blue_Sky| blue_sky|uploaded some_artist -cat|]" {
		t.Fatalf("library: %s", got)
	}
	if got := fmt.Sprint(queries("comics")); got != "[blue_sky|]" {
		t.Fatalf("comics: %s", got)
	}

	if err := l.AddRecentSearch(ctx, "   ", "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty: %v", err)
	}
	if err := l.AddRecentSearch(ctx, "x", "", "elsewhere"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("scope: %v", err)
	}

	if err := l.RemoveRecentSearch(ctx, "SOME_ARTIST -cat", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(queries("")); got != "[Blue_Sky| blue_sky|uploaded]" {
		t.Fatalf("after removing: %s", got)
	}

	for i := range MaxRecent + 5 {
		add(fmt.Sprintf("tag_%d", i), "", "")
	}
	got := queries("")
	if len(got) != MaxRecent || got[0] != fmt.Sprintf("tag_%d|", MaxRecent+4) || got[MaxRecent-1] != "tag_5|" {
		t.Fatalf("kept: %d %v", len(got), got)
	}
	var n int
	l.db.R.QueryRow("SELECT count(*) FROM recent_searches WHERE scope = ''").Scan(&n)
	if n != MaxRecent {
		t.Fatalf("rows kept: %d", n)
	}

	if err := l.ClearRecentSearches(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := queries(""); len(got) != 0 {
		t.Fatalf("cleared: %v", got)
	}
	if got := queries("comics"); len(got) != 1 {
		t.Fatalf("comics after clearing the library's: %v", got)
	}
}
