package library

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/usevitrine/vitrine/internal/tag"
)

// tagNames lists a file's tags as "ns:name" (no duplicates, sorted).
func tagNames(t *testing.T, l *Library, id uint32) []string {
	t.Helper()
	ft, err := l.FileTags(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var out []string
	for _, x := range ft {
		s := x.Tag
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func TestSetNamespaceInPlaceAndSiteRefresh(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 2)
	site, _ := l.Site(ctx, "safebooru")
	// A site calls it a series; the user says it's a creator.
	if err := l.SetSiteTags(ctx, ids[0], site, tags("series:some_artist", "blue_sky")); err != nil {
		t.Fatal(err)
	}
	if err := l.AddTags(ctx, []uint32{ids[1]}, tags("series:some_artist")); err != nil {
		t.Fatal(err)
	}
	got, n, err := l.SetNamespace(ctx, tag.Tag{Namespace: "series", Name: "some_artist"}, "creator")
	if err != nil || got.String() != "creator:some_artist" || n != 2 {
		t.Fatalf("moved: %v %d %v", got, n, err)
	}
	want(t, l, "creator:some_artist", ids[1], ids[0])
	want(t, l, "series:some_artist")
	want(t, l, "some_artist", ids[1], ids[0]) // a plain name: any namespace
	// The site's next refresh still calls it a series: it stays the creator tag (no second tag).
	if err := l.SetSiteTags(ctx, ids[0], site, tags("series:some_artist", "blue_sky")); err != nil {
		t.Fatal(err)
	}
	if g := tagNames(t, l, ids[0]); len(g) != 2 || g[0] != "blue_sky" || g[1] != "creator:some_artist" {
		t.Fatalf("after a refresh: %v", g)
	}
	want(t, l, "series:some_artist")
	// Back to general: sites calling it a series land there now.
	if got, _, err := l.SetNamespace(ctx, tag.Tag{Namespace: "creator", Name: "some_artist"}, ""); err != nil || got.String() != "some_artist" {
		t.Fatalf("to general: %v %v", got, err)
	}
	if err := l.SetSiteTags(ctx, ids[0], site, tags("series:some_artist")); err != nil {
		t.Fatal(err)
	}
	if g := tagNames(t, l, ids[0]); len(g) != 1 || g[0] != "some_artist" {
		t.Fatalf("after moving to general and a refresh: %v", g)
	}
}

func TestSetNamespaceMerges(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 3)
	// (The plain one first: a plain name alone resolves to the one namespaced tag of that name.)
	l.AddTags(ctx, []uint32{ids[1], ids[2]}, tags("some_artist"))
	l.AddTags(ctx, []uint32{ids[0], ids[2]}, tags("creator:some_artist"))
	got, n, err := l.SetNamespace(ctx, tag.Tag{Name: "some_artist"}, "creator")
	if err != nil || got.String() != "creator:some_artist" || n != 3 {
		t.Fatalf("merged: %v %d %v", got, n, err)
	}
	want(t, l, "creator:some_artist", ids[2], ids[1], ids[0])
	if g := tagNames(t, l, ids[2]); len(g) != 1 || g[0] != "creator:some_artist" {
		t.Fatalf("one tag, not two: %v", g)
	}
	// A tag with parents or siblings isn't merged (they'd have to be untangled).
	l.AddTags(ctx, []uint32{ids[0]}, tags("red", "meta:red"))
	if err := l.AddParent(ctx, tag.Tag{Name: "red"}, tag.Tag{Name: "favorite_colors"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.SetNamespace(ctx, tag.Tag{Name: "red"}, "meta"); !errors.Is(err, ErrHasRelations) {
		t.Fatalf("with a parent: %v", err)
	}
	if _, _, err := l.SetNamespace(ctx, tag.Tag{Name: "red"}, "Not Valid"); !errors.Is(err, ErrBadNamespace) {
		t.Fatalf("bad namespace: %v", err)
	}
	if _, _, err := l.SetNamespace(ctx, tag.Tag{Name: "nowhere"}, "meta"); !errors.Is(err, ErrNoTag) {
		t.Fatalf("no such tag: %v", err)
	}
}
