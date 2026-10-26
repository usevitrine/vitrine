package library

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/search"
)

func TestWipeSite(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 5)
	only, shared, other, trashed, purged := ids[0], ids[1], ids[2], ids[3], ids[4]
	one, err := l.Site(ctx, "blue_site")
	if err != nil {
		t.Fatal(err)
	}
	two, err := l.Site(ctx, "other_site")
	if err != nil {
		t.Fatal(err)
	}
	l.Index().AddSiteAlias("blue.example", one)
	post := func(f, site uint32, url string, tagNames ...string) {
		t.Helper()
		if err := l.SetSitePost(ctx, f, site, SitePost{Tags: tagNames, PostURL: url}); err != nil {
			t.Fatal(err)
		}
		l.Index().AddFileSite(f, site)
	}
	post(only, one, "https://blue.example/1", "blue_sky", "some_artist")
	post(shared, one, "https://blue.example/2", "blue_sky")
	post(shared, two, "https://other.example/2", "cloud")
	post(other, two, "https://other.example/3", "cloud")
	post(trashed, one, "https://blue.example/4", "blue_sky")
	post(purged, one, "https://blue.example/5", "blue_sky")
	if err := l.Delete(ctx, []uint32{trashed, purged}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := l.PurgeFiles(ctx, []uint32{purged}); err != nil {
		t.Fatal(err)
	}
	// A record of the other site's, and one from before the sites were kept.
	if err := l.Delete(ctx, []uint32{other}, ""); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := l.db.W.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO tombstones (sha256, md5, deleted_at) VALUES (x'00', x'00', 1)")
	exec("INSERT INTO seen_posts (site_id, post_id, file_id, outcome, seen_at) VALUES (?, 1, ?, 1, 1)", one, only)
	if _, err := l.SaveSearch(ctx, "Blue", "system:site=blue_site", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := l.SaveSearch(ctx, "Both", "cloud system:site=blue.example,other_site", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := l.SetSettings(ctx, map[string]json.RawMessage{"siteCheckDepths": json.RawMessage(`{"blue_site":3,"other_site":5}`)}); err != nil {
		t.Fatal(err)
	}

	tr, err := l.SiteTraces(ctx, "blue_site")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Files != 2 || tr.Shared != 1 || tr.Records != 1 || tr.Unknown != 1 || !tr.Any() {
		t.Fatalf("traces = %+v, want 2 files (one in the trash), 1 shared, 1 record, 1 unknown", tr)
	}

	w, err := l.WipeSite(ctx, "blue_site")
	if err != nil {
		t.Fatal(err)
	}
	if w.Files != 2 || w.Shared != 1 || w.Records != 1 || len(w.Hashes) != 2 {
		t.Fatalf("wiped = %+v", w)
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := l.db.R.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("SELECT count(*) FROM files WHERE id IN (?, ?)", only, trashed); n != 0 {
		t.Errorf("%d of its own files left", n)
	}
	if n := count("SELECT count(*) FROM files WHERE id = ?", shared); n != 1 {
		t.Errorf("the shared file went")
	}
	want(t, l, "cloud", shared)
	want(t, l, "blue_sky")
	if n := count("SELECT count(*) FROM tags WHERE name IN ('blue_sky', 'some_artist')"); n != 0 {
		t.Errorf("its tags are still there (%d)", n)
	}
	if n := count("SELECT count(*) FROM urls WHERE url LIKE '%blue.example%'"); n != 0 {
		t.Errorf("its links are still there (%d)", n)
	}
	if n := count("SELECT count(*) FROM tombstones"); n != 2 {
		t.Errorf("%d deletion records, want 2 (the other site's, the old one)", n)
	}
	if n := count("SELECT count(*) FROM tombstones WHERE instr(sites, 'blue_site') > 0"); n != 0 {
		t.Errorf("a record still names it")
	}
	for _, q := range []string{"SELECT count(*) FROM sites WHERE name = 'blue_site'", "SELECT count(*) FROM seen_posts"} {
		if n := count(q); n != 0 {
			t.Errorf("%s = %d", q, n)
		}
	}
	saved, err := l.SavedSearches(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Query != "cloud system:site=other_site" {
		t.Errorf("saved searches = %+v", saved)
	}
	set, _ := l.Settings(ctx)
	if string(set["siteCheckDepths"]) != `{"other_site":5}` {
		t.Errorf("siteCheckDepths = %s", set["siteCheckDepths"])
	}
	q, _ := search.Parse("system:site=blue.example")
	if got := l.Index().Search(q, time.Now()).IDs; len(got) != 0 {
		t.Errorf("system:site= still finds %v", got)
	}
	// Nothing left: nothing to wipe.
	if tr, err := l.SiteTraces(ctx, "blue_site"); err != nil || tr.Any() {
		t.Errorf("after: %+v, %v", tr, err)
	}
}

func TestDropSite(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"system:site=a", ""},
		{"cat system:site=a,b", "cat system:site=b"},
		{"System:Site!=A dog", "dog"},
		{"( x ~ system:site=a ) y", "( x ) y"},
		{"( system:site=a ~ system:site=b ) y", "( system:site=b ) y"},
		{"( system:site=a ) y", "y"},
		{"cat", "cat"},
	} {
		if got := search.DropSite(c.in, []string{"a"}); got != c.want {
			t.Errorf("DropSite(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
