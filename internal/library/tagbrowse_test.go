package library

import (
	"context"
	"errors"
	"testing"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/tag"
)

func TestTagListAndRename(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 4)
	site, _ := l.Site(ctx, "safebooru")
	l.EnsureNamespaces(ctx, []string{"creator"})
	l.SetSitePost(ctx, ids[0], site, SitePost{Tags: []string{"some_artist", "blue_sky"}, Namespaces: map[string]string{"some_artist": "creator"}})
	l.SetSitePost(ctx, ids[1], site, SitePost{Tags: []string{"blue_sky"}})
	l.AddTags(ctx, ids[1:4], tags("favorit", "blue_sky"))
	l.AddTags(ctx, ids[3:4], tags("favorite"))

	total, list, err := l.TagList(ctx, "", "", false, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || list[0].Tag != "blue_sky" || list[0].Count != 4 || !list[0].FromSites || list[1].Tag != "favorit" || list[1].FromSites {
		t.Fatalf("list: %d %+v", total, list)
	}
	if total, list, _ = l.TagList(ctx, "fav", "", true, 0, 10); total != 2 || list[0].Tag != "favorit" || list[1].Tag != "favorite" {
		t.Fatalf("filtered by name: %d %+v", total, list)
	}
	if total, list, _ = l.TagList(ctx, "", "creator", false, 0, 10); total != 1 || list[0].Tag != "creator:some_artist" || list[0].Namespace != "creator" {
		t.Fatalf("namespace: %d %+v", total, list)
	}
	if total, _, _ = l.TagList(ctx, "", index.NoNamespace, false, 0, 10); total != 3 {
		t.Fatalf("no namespace: %d", total)
	}
	if total, list, _ = l.TagList(ctx, "", "", false, 3, 10); total != 4 || len(list) != 1 {
		t.Fatalf("offset: %d %+v", total, list)
	}

	// Fix a typo: favorit -> favorite (merges into the existing tag).
	n, err := l.RenameTag(ctx, tag.Tag{Name: "favorit"}, tag.Tag{Name: "favorite"})
	if err != nil || n != 3 {
		t.Fatalf("rename: %d %v", n, err)
	}
	want(t, l, "favorite", ids[3], ids[2], ids[1])
	want(t, l, "favorit")
	if total, _, _ = l.TagList(ctx, "favorit", "", false, 0, 10); total != 1 {
		t.Fatalf("the old name is still listed: %d", total)
	}
	// A plain rename to a new name.
	if n, err = l.RenameTag(ctx, tag.Tag{Name: "favorite"}, tag.Tag{Name: "faves"}); err != nil || n != 3 {
		t.Fatalf("rename to new: %d %v", n, err)
	}
	want(t, l, "faves", ids[3], ids[2], ids[1])

	if _, err := l.RenameTag(ctx, tag.Tag{Name: "blue_sky"}, tag.Tag{Name: "sky"}); !errors.Is(err, ErrSiteTag) {
		t.Fatalf("site tag: %v", err)
	}
	l.AddParent(ctx, tag.Tag{Name: "faves"}, tag.Tag{Name: "liked"})
	if _, err := l.RenameTag(ctx, tag.Tag{Name: "faves"}, tag.Tag{Name: "x"}); !errors.Is(err, ErrHasRelations) {
		t.Fatalf("with a parent: %v", err)
	}
	if _, err := l.RenameTag(ctx, tag.Tag{Name: "nope"}, tag.Tag{Name: "x"}); !errors.Is(err, ErrNoTag) {
		t.Fatalf("missing: %v", err)
	}
	checkReload(t, l)
}

// The selection's tags: each with how many of the files have it.
func TestTagsOf(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 3)
	l.EnsureNamespaces(ctx, []string{"creator"})
	l.AddTags(ctx, ids, tags("blue_sky"))
	l.AddTags(ctx, ids[:1], tags("creator:some_artist"))
	l.AddTags(ctx, ids[2:], tags("cloud"))
	got, err := l.TagsOf(ctx, ids[:2])
	if err != nil {
		t.Fatal(err)
	}
	want := []TagCount{{"blue_sky", 2}, {"creator:some_artist", 1}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got, _ := l.TagsOf(ctx, nil); len(got) != 0 {
		t.Fatalf("no files: %+v", got)
	}
}
