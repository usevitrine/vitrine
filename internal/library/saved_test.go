package library

import (
	"context"
	"errors"
	"testing"
)

func TestSavedSearches(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 3)
	l.AddTags(ctx, ids[:2], tags("some_artist"))

	a, err := l.SaveSearch(ctx, "  My   artist ", " some_artist  ", "uploaded", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := l.SaveSearch(ctx, "", "system:type=image", "", "") // no name: the search is the name
	if _, err := l.SaveSearch(ctx, "", "  ", "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty saved search: %v", err)
	}
	if _, err := l.SaveSearch(ctx, "x", "system:size>banana", "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad search: %v", err)
	}
	list, err := l.SavedSearches(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "My artist" || list[0].Query != "some_artist" || list[0].Sort != "uploaded" ||
		list[0].Count != 2 || list[1].Name != "system:type=image" || list[1].Count != 3 {
		t.Fatalf("list: %+v", list)
	}

	name, q := "Artist", "some_artist -system:has_audio"
	if err := l.UpdateSavedSearch(ctx, a, SavedChange{Name: &name, Query: &q}); err != nil {
		t.Fatal(err)
	}
	if err := l.ReorderSavedSearches(ctx, []int64{b}); err != nil {
		t.Fatal(err)
	}
	list, _ = l.SavedSearches(ctx, "")
	if list[0].ID != b || list[1].Name != "Artist" || list[1].Query != q || list[1].Sort != "uploaded" {
		t.Fatalf("after edits: %+v", list)
	}
	if err := l.DeleteSavedSearch(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := l.DeleteSavedSearch(ctx, b); !errors.Is(err, ErrNoSaved) {
		t.Fatalf("deleting twice: %v", err)
	}
	if err := l.UpdateSavedSearch(ctx, 999, SavedChange{Name: &name}); !errors.Is(err, ErrNoSaved) {
		t.Fatalf("update missing: %v", err)
	}
	if err := l.ReorderSavedSearches(ctx, []int64{999}); !errors.Is(err, ErrNoSaved) {
		t.Fatalf("reorder missing: %v", err)
	}
}
