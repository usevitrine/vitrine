package library

import (
	"context"
	"testing"
)

// Tags saved with HTML character codes ("some&#039;s_tag") are renamed, or
// merged into the tag that already has the right name.
func TestFixEscapedTags(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 2)
	site, err := l.Site(ctx, "safebooru")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetSiteTags(ctx, ids[0], site, tags("artist&#039;s_hat", "blue_sky")); err != nil {
		t.Fatal(err)
	}
	if err := l.SetSiteTags(ctx, ids[1], site, tags("some_artist&#039;s_hat", "some_artist's_hat")); err != nil {
		t.Fatal(err)
	}
	if err := fixEscapedTags(ctx, l.db.W); err != nil {
		t.Fatal(err)
	}
	var left int
	l.db.R.QueryRow("SELECT count(*) FROM tags WHERE name LIKE '%&%;%'").Scan(&left)
	if left != 0 {
		t.Fatalf("%d escaped tags left", left)
	}
	names := func(id uint32) map[string]bool {
		out := map[string]bool{}
		fts, err := l.FileTags(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, ft := range fts {
			out[ft.Tag] = true
		}
		return out
	}
	if a := names(ids[0]); !a["artist's_hat"] || !a["blue_sky"] || len(a) != 2 {
		t.Errorf("renamed: %v", a)
	}
	if b := names(ids[1]); !b["some_artist's_hat"] || len(b) != 1 { // merged: one tag
		t.Errorf("merged: %v", b)
	}
	if err := fixEscapedTags(ctx, l.db.W); err != nil { // nothing to do the second time
		t.Fatal(err)
	}
}

// Tags saved with Shimmie's caret codes ("bloom_^a_rage") are decoded; real
// carets ("^_^") stay.
func TestFixCaretTags(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 2)
	site, err := l.Site(ctx, "safebooru")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetSiteTags(ctx, ids[0], site, tags("series:bloom_^a_rage", "^_^", "a^sb")); err != nil {
		t.Fatal(err)
	}
	if err := l.SetSiteTags(ctx, ids[1], site, tags("series:bloom_&_rage")); err != nil {
		t.Fatal(err)
	}
	if err := fixCaretTags(ctx, l.db.W); err != nil {
		t.Fatal(err)
	}
	var names []string
	rows, _ := l.db.R.Query("SELECT name FROM tags ORDER BY name")
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	want := []string{"^_^", "a/b", "bloom_&_rage"}
	if len(names) != len(want) {
		t.Fatalf("tags: %q, want %q", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tags: %q, want %q", names, want)
		}
	}
	var n int
	l.db.R.QueryRow("SELECT count(DISTINCT file_id) FROM file_tags ft JOIN tags t ON t.id = ft.tag_id WHERE t.name = 'bloom_&_rage'").Scan(&n)
	if n != 2 {
		t.Fatalf("merged tag on %d files, want both", n)
	}
}
