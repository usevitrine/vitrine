package library

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/tag"
)

func liveFiles(t *testing.T, l *Library) int {
	t.Helper()
	return l.Index().Stats().Files
}

func TestComicPagesStayOutOfTheLibrary(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	id, isNew, err := l.AddComic(ctx, ComicInfo{
		Title: "Some comic", Site: "examplebooru", Gallery: "1/abc", Pages: 3,
		Tags: []tag.Tag{{Namespace: "creator", Name: "some_artist"}, {Namespace: "female", Name: "glasses"}},
	})
	if err != nil || !isNew {
		t.Fatalf("add: %d %v %v", id, isNew, err)
	}
	for p := range 3 {
		if _, err := l.AddComicPage(ctx, id, p, bytes.NewReader(fakePNG(byte(10+p)))); err != nil {
			t.Fatal(err)
		}
	}
	if n := liveFiles(t, l); n != 0 {
		t.Fatalf("%d files in the library, want none (comic pages only)", n)
	}
	pages, err := l.ComicPages(ctx, id)
	if err != nil || len(pages) != 3 {
		t.Fatalf("pages: %v %v", pages, err)
	}
	c, err := l.Comic(ctx, id)
	if err != nil || c.Have != 3 || c.Pages != 3 || c.Cover != pages[0] || len(c.Tags) != 2 || c.Site != "examplebooru" {
		t.Fatalf("comic: %+v %v", c, err)
	}
	// The same gallery again: the same comic (site tags replaced).
	again, isNew, err := l.AddComic(ctx, ComicInfo{Title: "Some comic", Site: "examplebooru", Gallery: "1/abc", Pages: 3,
		Tags: []tag.Tag{{Namespace: "female", Name: "glasses"}}})
	if err != nil || isNew || again != id {
		t.Fatalf("again: %d %v %v", again, isNew, err)
	}
	if c, _ := l.Comic(ctx, id); len(c.Tags) != 1 {
		t.Fatalf("tags after the site's update: %v", c.Tags)
	}

	// The same file downloaded for the library: it joins it (and stays the comic's page).
	f, err := l.Import(ctx, bytes.NewReader(fakePNG(11)))
	if err != nil || f.New || f.ID != pages[1] {
		t.Fatalf("import of a page: %+v %v", f, err)
	}
	if n := liveFiles(t, l); n != 1 {
		t.Fatalf("%d files in the library after the import, want 1", n)
	}
	// Deleted from the library: out of it again, but still the comic's page (no tombstone).
	if err := l.Delete(ctx, []uint32{f.ID}, ""); err != nil {
		t.Fatal(err)
	}
	if n := liveFiles(t, l); n != 0 {
		t.Fatalf("%d files in the library after the delete, want 0", n)
	}
	if p, _ := l.ComicPages(ctx, id); len(p) != 3 {
		t.Fatalf("the comic lost a page: %v", p)
	}
	if m, _ := l.FindMD5(ctx, md5Of(t, l, f.ID)); m.Deleted {
		t.Fatal("a tombstone for a file a comic still has")
	}

	// A fresh index (as after a restart) agrees: no comic pages in it.
	fresh, err := index.Load(ctx, l.db.R)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Stats().Files != 0 {
		t.Fatalf("a fresh index has %d files", fresh.Stats().Files)
	}
}

func md5Of(t *testing.T, l *Library, id uint32) [16]byte {
	t.Helper()
	var b []byte
	if err := l.db.R.QueryRow("SELECT md5 FROM files WHERE id = ?", id).Scan(&b); err != nil {
		t.Fatal(err)
	}
	var m [16]byte
	copy(m[:], b)
	return m
}

func TestComicSearchAndSort(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	add := func(title string, tags ...string) int64 {
		var tt []tag.Tag
		for _, s := range tags {
			tt = append(tt, tag.Split(s, func(ns string) bool { return true }))
		}
		id, _, err := l.AddComic(ctx, ComicInfo{Title: title, Tags: tt})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := add("Blue Sky Days", "creator:some_artist", "female:glasses")
	b := add("Red Morning", "creator:other_artist", "language:english")
	c := add("Blue Night", "creator:some_artist", "language:english")
	ids := func(search, sort string) []int64 {
		t.Helper()
		list, _, err := l.Comics(ctx, ComicQuery{Search: search, Sort: sort})
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, x := range list {
			out = append(out, x.ID)
		}
		return out
	}
	eq := func(got []int64, want ...int64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	}
	eq(ids("", ""), c, b, a)                                 // newest first
	eq(ids("some_artist", ""), c, a)                         // a tag in any namespace
	eq(ids("language:english -creator:other_artist", ""), c) // namespaced, excluded
	eq(ids("blue", "title"), c, a)                           // part of the title (A-Z)
	eq(ids("*artist", ""), c, b, a)                          // a wildcard
	eq(ids("/blue", "title"), c, a)                          // "/": titles only
	eq(ids("/glasses", ""))                                  // (a tag, not in a title)
	eq(ids("/blue_*days -/night", ""), a)                    // a wildcard inside, excluded
	if err := l.SetComicRead(ctx, b, 4); err != nil {
		t.Fatal(err)
	}
	eq(ids("", "read")[:1], b) // last read first
	if err := l.SetComicRead(ctx, 999, 1); !errors.Is(err, ErrNoComic) {
		t.Fatalf("an unknown comic: %v", err)
	}
}

func TestComicDeleteUndoPurge(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	one, _, _ := l.AddComic(ctx, ComicInfo{Title: "One"})
	two, _, _ := l.AddComic(ctx, ComicInfo{Title: "Two"})
	shared, _ := l.AddComicPage(ctx, one, 0, bytes.NewReader(fakePNG(1)))
	own, _ := l.AddComicPage(ctx, one, 1, bytes.NewReader(fakePNG(2)))
	if _, err := l.AddComicPage(ctx, two, 0, bytes.NewReader(fakePNG(1))); err != nil { // the same picture in another comic
		t.Fatal(err)
	}
	if err := l.DeleteComics(ctx, []int64{one}); err != nil {
		t.Fatal(err)
	}
	if list, total, _ := l.Comics(ctx, ComicQuery{}); total != 1 || list[0].ID != two {
		t.Fatalf("after the delete: %v", list)
	}
	deleted := func(id uint32) bool {
		var d bool
		l.db.R.QueryRow("SELECT deleted_at IS NOT NULL FROM files WHERE id = ?", id).Scan(&d)
		return d
	}
	if !deleted(own.ID) || deleted(shared.ID) {
		t.Fatalf("own page deleted %v (want true), shared page deleted %v (want false)", deleted(own.ID), deleted(shared.ID))
	}
	if err := l.UndeleteComics(ctx, []int64{one}); err != nil {
		t.Fatal(err)
	}
	if p, _ := l.ComicPages(ctx, one); len(p) != 2 || deleted(own.ID) {
		t.Fatalf("after undo: pages %v", p)
	}
	// Deleted again and purged: the comic and its own page are gone; the other comic keeps the shared one.
	l.DeleteComics(ctx, []int64{one})
	if _, err := l.Purge(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Comic(ctx, one); !errors.Is(err, ErrNoComic) {
		t.Fatalf("purged comic: %v", err)
	}
	if p, _ := l.ComicPages(ctx, two); len(p) != 1 {
		t.Fatalf("the other comic's pages: %v", p)
	}
}

func TestImportComic(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "Some Comic")
	os.MkdirAll(dir, 0o755)
	for i, name := range []string{"page10.png", "page2.png", "page1.png", "notes.txt", "._page3.png"} {
		os.WriteFile(filepath.Join(dir, name), fakePNG(byte(40+i)), 0o644)
	}
	id, n, err := l.ImportComic(ctx, dir, "")
	if err != nil || n != 3 {
		t.Fatalf("folder: %d pages, %v", n, err)
	}
	c, _ := l.Comic(ctx, id)
	if c.Title != "Some Comic" || c.Pages != 3 {
		t.Fatalf("comic: %+v", c)
	}
	pages, _ := l.ComicPages(ctx, id)
	rows, _ := l.Files(ctx, pages)
	want := []byte{42, 41, 40} // page1, page2, page10
	for i, r := range rows {
		b, _ := os.ReadFile(r.Path)
		if b[10] != want[i] {
			t.Fatalf("page %d is the wrong file", i)
		}
	}
	// A zip (cbz) with a folder inside.
	zpath := filepath.Join(t.TempDir(), "Other Comic.cbz")
	zf, _ := os.Create(zpath)
	zw := zip.NewWriter(zf)
	for i, name := range []string{"ch1/02.png", "ch1/01.png"} {
		w, _ := zw.Create(name)
		w.Write(fakePNG(byte(60 + i)))
	}
	zw.Close()
	zf.Close()
	id2, n, err := l.ImportComic(ctx, zpath, "")
	if err != nil || n != 2 {
		t.Fatalf("zip: %d pages, %v", n, err)
	}
	if c, _ := l.Comic(ctx, id2); c.Title != "Other Comic" {
		t.Fatalf("zip title %q", c.Title)
	}
	if liveFiles(t, l) != 0 {
		t.Fatal("imported comic pages are in the library")
	}
}

func TestNaturalCompare(t *testing.T) {
	names := []string{"p10.jpg", "p2.jpg", "P1.jpg", "p02b.jpg", "a.jpg"}
	slices.SortFunc(names, naturalCompare)
	want := []string{"a.jpg", "P1.jpg", "p2.jpg", "p02b.jpg", "p10.jpg"}
	if !slices.Equal(names, want) {
		t.Fatalf("got %v", names)
	}
}

func TestComicSiteTags(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	l.AddComic(ctx, ComicInfo{Title: "A", Site: "galleries", Gallery: "1", Tags: []tag.Tag{{Namespace: "female", Name: "glasses"}, {Namespace: "creator", Name: "some_artist"}}})
	l.AddComic(ctx, ComicInfo{Title: "B", Site: "galleries", Gallery: "2", Tags: []tag.Tag{{Namespace: "female", Name: "glasses"}}})
	l.AddComic(ctx, ComicInfo{Title: "C", Site: "other", Gallery: "3", Tags: []tag.Tag{{Namespace: "female", Name: "gloves"}}})
	got, err := l.ComicSiteTags(ctx, "gl", "", 10)
	if err != nil || len(got) != 2 || got[0].Tag != "female:glasses" || got[0].Count != 2 {
		t.Fatalf("all: %+v %v", got, err)
	}
	if got, _ := l.ComicSiteTags(ctx, "gl", "galleries", 10); len(got) != 1 {
		t.Fatalf("one site: %+v", got)
	}
	if got, _ := l.ComicSiteTags(ctx, "creator:so", "", 10); len(got) != 1 || got[0].Tag != "creator:some_artist" {
		t.Fatalf("namespaced: %+v", got)
	}
	// Not only the start: a word's start next, then inside a word.
	if got, _ := l.ComicSiteTags(ctx, "artist", "", 10); len(got) != 1 || got[0].Tag != "creator:some_artist" {
		t.Fatalf("a word's start: %+v", got)
	}
	if got, _ := l.ComicSiteTags(ctx, "lass", "", 10); len(got) != 1 || got[0].Tag != "female:glasses" {
		t.Fatalf("inside: %+v", got)
	}
	// Never in the library's own suggestions.
	if s := l.Index().Autocomplete("gl", 10, ""); len(s) != 0 {
		t.Fatalf("the library suggests comic tags: %+v", s)
	}
	if ns := l.Namespaces(); slices.Contains(ns, "female") {
		t.Fatalf("a comics-only namespace in the library's list: %v", ns)
	}
}

// Collections: comics put together (by name; a comic in one at most), its first by title (naturally) its cover;
// renamed, emptied (it goes), taken apart (its comics stay).
func TestCollections(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	add := func(title, gallery string) int64 {
		id, _, err := l.AddComic(ctx, ComicInfo{Title: title, Site: "examplebooru", Gallery: gallery, Pages: 1})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	v10, v2, other := add("Some Series Vol 10", "1/a"), add("Some Series Vol 2", "2/b"), add("Something Else", "3/c")
	k, err := l.CreateCollection(ctx, "Some Series", []int64{v10, v2})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := l.Collections(ctx)
	if len(list) != 1 || list[0].Name != "Some Series" || list[0].Count != 2 || list[0].Cover != v2 {
		t.Fatalf("collections: %+v (want Vol 2, the first naturally, as the cover)", list)
	}
	of, _ := l.CollectionOf(ctx)
	if of[v10] != k || of[v2] != k || of[other] != 0 {
		t.Fatalf("collection of: %v", of)
	}
	if err := l.RenameCollection(ctx, k, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.CreateCollection(ctx, "  ", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no name: %v", err)
	}
	// Moved to another collection: one at most; the first one emptied goes.
	k2, _ := l.CreateCollection(ctx, "Another", []int64{v10, v2})
	if list, _ = l.Collections(ctx); len(list) != 1 || list[0].ID != k2 || list[0].Count != 2 {
		t.Fatalf("after moving: %+v", list)
	}
	if err := l.DeleteCollection(ctx, k2); err != nil {
		t.Fatal(err)
	}
	if list, _ = l.Collections(ctx); len(list) != 0 {
		t.Fatalf("taken apart: %+v", list)
	}
	if c, _ := l.ComicsByID(ctx, []int64{v10}); len(c) != 1 || c[0].Collection != 0 {
		t.Fatalf("its comics stay, on their own: %+v", c)
	}
	// Its own order: by title naturally at first; placed by the user; a comic added later goes after the placed ones.
	ko, _ := l.CreateCollection(ctx, "Ordered", []int64{v10, v2})
	if o, _ := l.CollectionOrder(ctx, ko); len(o) != 2 || o[0] != v2 || o[1] != v10 {
		t.Fatalf("natural order: %v", o)
	}
	if err := l.OrderCollection(ctx, ko, []int64{v10, v2, other}); err != nil { // (other isn't in it: skipped)
		t.Fatal(err)
	}
	if o, _ := l.CollectionOrder(ctx, ko); len(o) != 2 || o[0] != v10 || o[1] != v2 {
		t.Fatalf("placed: %v", o)
	}
	if list, _ = l.Collections(ctx); list[0].Cover != v10 {
		t.Fatalf("its cover follows its order: %+v", list)
	}
	l.SetCollection(ctx, []int64{other}, ko)
	if o, _ := l.CollectionOrder(ctx, ko); len(o) != 3 || o[2] != other {
		t.Fatalf("added later: %v", o)
	}
	l.SetCollection(ctx, []int64{v10}, ko) // (already in it: keeps its place)
	if o, _ := l.CollectionOrder(ctx, ko); o[0] != v10 {
		t.Fatalf("added again: %v", o)
	}
	if err := l.OrderCollection(ctx, 9999, nil); !errors.Is(err, ErrNoCollection) {
		t.Fatalf("no such collection: %v", err)
	}
	l.DeleteCollection(ctx, ko)

	// Deleted with its comics: hidden while they're in the trash, back with one restored, gone once they're purged.
	k3, _ := l.CreateCollection(ctx, "Third", []int64{v10, v2})
	ids, err := l.DeleteCollectionComics(ctx, k3)
	if err != nil || len(ids) != 2 {
		t.Fatalf("deleted with its comics: %v %v", ids, err)
	}
	if list, _ = l.Collections(ctx); len(list) != 0 {
		t.Fatalf("its comics in the trash: %+v", list)
	}
	if err := l.UndeleteComics(ctx, []int64{v2}); err != nil {
		t.Fatal(err)
	}
	if list, _ = l.Collections(ctx); len(list) != 1 || list[0].Count != 1 {
		t.Fatalf("one restored: %+v", list)
	}
	l.DeleteComics(ctx, []int64{v2})
	if _, err := l.EmptyComicsTrash(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	l.db.R.QueryRowContext(ctx, "SELECT count(*) FROM collections").Scan(&n)
	if n != 0 {
		t.Fatalf("%d collections left after its comics were purged", n)
	}
	if _, err := l.DeleteCollectionComics(ctx, k3); !errors.Is(err, ErrNoCollection) {
		t.Fatalf("a gone collection: %v", err)
	}
}
