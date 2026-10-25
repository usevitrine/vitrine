package index

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/search"
)

var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

const day = int64(24 * time.Hour / time.Millisecond)

// fixture: a small library.
//
//	tags: 1 blue  2 red  3 creator:some_artist  4 creator:other_artist  5 favorite_artists
//	      6 9:16  7 creator:blue (same name as 1, other namespace)  8 smiling  9 smiling_softly
//	files:
//	  1: blue, some_artist, smiling         jpeg  1 MB   imported 10 days ago
//	  2: red, other_artist                     mp4   50 MB  audio, 30 s, imported 2 days ago, explicit
//	  3: blue, red, 9:16                 png   2 MB   imported 1 hour ago
//	  4: creator:blue, smiling_softly  webm 200 MB audio, 90 s, imported 400 days ago
func fixture(t *testing.T) *Index {
	t.Helper()
	ix := New()
	ix.AddNamespace(1, "creator")
	ix.AddTags(
		Tag{1, 0, "blue"}, Tag{2, 0, "red"}, Tag{3, 1, "some_artist"}, Tag{4, 1, "other_artist"},
		Tag{5, 0, "favorite_artists"}, Tag{6, 0, "9:16"}, Tag{7, 1, "blue"},
		Tag{8, 0, "smiling"}, Tag{9, 0, "smiling_softly"},
	)
	ms := now.UnixMilli()
	files := []struct {
		f    File
		tags []uint32
	}{
		{File{ID: 1, Size: 1 << 20, Type: media.JPEG, Width: 800, Height: 600, ImportedAt: ms - 10*day, ModifiedAt: ms - 10*day}, []uint32{1, 3, 8}},
		{File{ID: 2, Size: 50 << 20, Type: media.MP4, DurationMS: 30000, HasAudio: true, ImportedAt: ms - 2*day, ModifiedAt: ms - 2*day, Rating: media.RatingExplicit}, []uint32{2, 4}},
		{File{ID: 3, Size: 2 << 20, Type: media.PNG, Width: 1920, Height: 1080, ImportedAt: ms - 3600_000, ModifiedAt: ms - 3600_000}, []uint32{1, 2, 6}},
		{File{ID: 4, Size: 200 << 20, Type: media.WebM, DurationMS: 90000, HasAudio: true, ImportedAt: ms - 400*day, ModifiedAt: ms - 400*day}, []uint32{7, 9}},
	}
	for _, f := range files {
		if err := ix.AddFile(f.f, f.tags); err != nil {
			t.Fatal(err)
		}
	}
	return ix
}

func ids(t *testing.T, ix *Index, text string) []uint32 {
	t.Helper()
	q, err := search.Parse(text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	r := ix.Search(q, now)
	if r.Total != len(r.IDs) && q.Limit == 0 {
		t.Fatalf("%q: total %d != %d ids", text, r.Total, len(r.IDs))
	}
	return r.IDs
}

func expect(t *testing.T, ix *Index, text string, want ...uint32) {
	t.Helper()
	got := ids(t, ix, text)
	if want == nil {
		want = []uint32{}
	}
	if got == nil {
		got = []uint32{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q = %v, want %v", text, got, want)
	}
}

func TestTagSearch(t *testing.T) {
	ix := fixture(t)
	expect(t, ix, "", 4, 3, 2, 1)  // everything, newest first
	expect(t, ix, "blue", 4, 3, 1) // plain name: every namespace
	expect(t, ix, "creator:blue", 4)
	expect(t, ix, "BLUE red", 3)
	expect(t, ix, "blue -red", 4, 1)
	expect(t, ix, "-blue", 2)
	expect(t, ix, "some_artist", 1)
	expect(t, ix, "9:16", 3) // "9" is not a namespace: literal tag
	expect(t, ix, "nosuchtag")
	expect(t, ix, "blue nosuchtag")
	expect(t, ix, "blue -nosuchtag", 4, 3, 1)

	q, _ := search.Parse("blue nosuchtag")
	if r := ix.Search(q, now); !slices.Equal(r.Unknown, []string{"nosuchtag"}) {
		t.Errorf("Unknown = %v", r.Unknown)
	}
}

func TestOrGroups(t *testing.T) {
	ix := fixture(t)
	expect(t, ix, "( some_artist ~ other_artist )", 2, 1)
	expect(t, ix, "( smiling ~ smiling_softly )", 4, 1)
	expect(t, ix, "( some_artist ~ -blue )", 2, 1)
	expect(t, ix, "( some_artist ~ system:has_audio ) -red", 4, 1)
	expect(t, ix, "( some_artist ~ other_artist ) ( blue ~ 9:16 )", 1)
}

func TestWildcards(t *testing.T) {
	ix := fixture(t)
	expect(t, ix, "smil*", 4, 1)
	expect(t, ix, "*soft*", 4)
	expect(t, ix, "*r_artist", 2)
	expect(t, ix, "*i*g*", 4, 1) // smiling, smiling_softly
	expect(t, ix, "creator:*", 4, 2, 1)
	expect(t, ix, "creator:s*", 1)
	expect(t, ix, "-*g*", 3, 2) // files without any tag containing "g"
}

func TestSystemFilters(t *testing.T) {
	ix := fixture(t)
	expect(t, ix, "system:imported<5d", 3, 2)
	expect(t, ix, "system:imported<1d", 3)
	expect(t, ix, "system:imported>1y", 4)
	expect(t, ix, "system:imported<=2d", 3, 2)
	expect(t, ix, "system:imported=2026-10-04", 1) // 10 days before now
	expect(t, ix, "system:imported>=2026-10-12", 3, 2)
	expect(t, ix, "system:imported<2026-10-12", 4, 1)
	expect(t, ix, "system:imported!=2026-10-14", 4, 2, 1)
	expect(t, ix, "system:has_audio", 4, 2)
	expect(t, ix, "-system:has_audio", 3, 1)
	expect(t, ix, "system:size>10MB", 4, 2)
	expect(t, ix, "system:size<=2MB", 3, 1)
	expect(t, ix, "system:duration>=30s", 4, 2)
	expect(t, ix, "system:duration>1m", 4)
	expect(t, ix, "system:width>=1920", 3)
	expect(t, ix, "system:type=video", 4, 2)
	expect(t, ix, "system:type=image", 3, 1)
	expect(t, ix, "system:type!=jpg,mp4", 4, 3)
	expect(t, ix, "system:rating=explicit", 2)
	expect(t, ix, "system:rating!=explicit", 4, 3, 1)
	expect(t, ix, "system:tags=3", 3, 1)
	expect(t, ix, "blue system:has_audio", 4)
	expect(t, ix, "-system:type=video -system:imported<1d", 1)
}

func TestSorting(t *testing.T) {
	ix := fixture(t)
	expect(t, ix, "sort:imported_asc", 1, 2, 3, 4)
	expect(t, ix, "sort:size", 4, 2, 3, 1)
	expect(t, ix, "sort:size_asc", 1, 3, 2, 4)
	expect(t, ix, "sort:duration", 4, 2, 3, 1) // ties (0) by ID, descending
	expect(t, ix, "sort:duration_asc", 1, 3, 2, 4)
	expect(t, ix, "sort:tags", 3, 1, 4, 2)
	expect(t, ix, "sort:size limit:2", 4, 2)
	expect(t, ix, "limit:1", 4)

	q, _ := search.Parse("sort:random_99")
	a, b := ix.Search(q, now), ix.Search(q, now)
	if !slices.Equal(a.IDs, b.IDs) || len(a.IDs) != 4 || a.Seed != 99 {
		t.Errorf("random with a seed isn't repeatable: %v %v", a.IDs, b.IDs)
	}
	q, _ = search.Parse("sort:random")
	r := ix.Search(q, now)
	q.Sort.Seed = r.Seed
	if again := ix.Search(q, now); r.Seed == 0 || !slices.Equal(r.IDs, again.IDs) {
		t.Errorf("random seed not reported: %d", r.Seed)
	}
}

// TestLargeSort checks that the presorted path gives exactly the direct
// sort's order (with ties) for every field.
func TestLargeSort(t *testing.T) {
	ix := New()
	ix.AddTags(Tag{1, 0, "all"}, Tag{2, 0, "even"})
	for id := uint32(1); id <= 20000; id++ {
		tags := []uint32{1}
		if id%2 == 0 {
			tags = append(tags, 2)
		}
		f := File{ID: id, Size: int64(id*7919) % 1000, DurationMS: id % 13, Width: id % 5,
			ModifiedAt: int64(id % 97), ImportedAt: int64(id)}
		if err := ix.AddFile(f, tags); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"size", "duration", "width", "modified", "tags"} {
		for _, dir := range []string{"", "_asc"} {
			for _, base := range []string{"all", "even"} {
				q, _ := search.Parse(base + " sort:" + field + dir)
				got := ix.Search(q, now).IDs
				want := ix.Search(search.Query{Terms: q.Terms}, now).IDs
				key := ix.sortKey(q.Sort.Field)
				slices.SortFunc(want, func(a, b uint32) int {
					c := cmp.Or(cmp.Compare(key(a), key(b)), cmp.Compare(a, b))
					if dir == "" {
						c = -c
					}
					return c
				})
				if !slices.Equal(got, want) {
					t.Fatalf("%s: presorted order differs from direct sort", q)
				}
				q.Limit = 10
				if got := ix.Search(q, now).IDs; !slices.Equal(got, want[:10]) {
					t.Fatalf("%s limit 10: %v, want %v", q, got, want[:10])
				}
			}
		}
	}
	// A change must invalidate the presorted list.
	q, _ := search.Parse("all sort:modified")
	ix.UpdateTags(5, nil, nil, 1_000_000)
	if got := ix.Search(q, now).IDs; got[0] != 5 {
		t.Fatalf("after update, first by modified = %d, want 5", got[0])
	}
}

func TestParentsAndSiblings(t *testing.T) {
	ix := fixture(t)
	// favorite_artists is a group tag over two artists.
	parents := []Relation{{3, 5}, {4, 5}}
	if err := ix.SetRelations(parents, nil); err != nil {
		t.Fatal(err)
	}
	expect(t, ix, "favorite_artists", 2, 1)
	expect(t, ix, "-favorite_artists", 4, 3)
	expect(t, ix, "favorite_artists -red", 1)

	// A sibling: smiling_softly -> smiling. Either finds both.
	if err := ix.SetRelations(parents, []Relation{{9, 8}}); err != nil {
		t.Fatal(err)
	}
	expect(t, ix, "smiling", 4, 1)
	expect(t, ix, "smiling_softly", 4, 1)

	// Updates keep parent bitmaps right.
	if err := ix.UpdateTags(3, []uint32{3}, nil, 0); err != nil { // file 3 gets some_artist
		t.Fatal(err)
	}
	expect(t, ix, "favorite_artists", 3, 2, 1)
	if err := ix.UpdateTags(2, nil, []uint32{4}, 0); err != nil { // file 2 loses other_artist
		t.Fatal(err)
	}
	expect(t, ix, "favorite_artists", 3, 1)

	// A file with two children of the same parent keeps the parent until
	// both are gone.
	ix.UpdateTags(1, []uint32{4}, nil, 0)
	ix.UpdateTags(1, nil, []uint32{3}, 0)
	expect(t, ix, "favorite_artists", 3, 1)
	ix.UpdateTags(1, nil, []uint32{4}, 0)
	expect(t, ix, "favorite_artists", 3)

	// Removing relations restores plain bitmaps.
	if err := ix.SetRelations(nil, nil); err != nil {
		t.Fatal(err)
	}
	expect(t, ix, "favorite_artists")
	expect(t, ix, "smiling", 1)
}

func TestNestedParents(t *testing.T) {
	ix := fixture(t)
	// some_artist -> favorite_artists -> red  (red implied by favorite_artists)
	if err := ix.SetRelations([]Relation{{3, 5}, {5, 2}}, nil); err != nil {
		t.Fatal(err)
	}
	expect(t, ix, "red", 3, 2, 1)
}

func TestRelationLoops(t *testing.T) {
	ix := fixture(t)
	bad := []struct {
		name              string
		parents, siblings []Relation
		wantErrSubstring  string
	}{
		{"self parent", []Relation{{1, 1}}, nil, "parent"},
		{"parent loop", []Relation{{1, 2}, {2, 5}, {5, 1}}, nil, "loop"},
		{"sibling loop", nil, []Relation{{1, 2}, {2, 1}}, "loop"},
		{"two ideals", nil, []Relation{{1, 2}, {1, 5}}, "already"},
		{"parent of own sibling", []Relation{{1, 2}}, []Relation{{1, 2}}, "same tag or siblings"},
		{"loop through sibling", []Relation{{1, 5}, {6, 2}}, []Relation{{5, 6}, {2, 1}}, "loop"},
		{"unknown tag", []Relation{{1, 99}}, nil, "unknown"},
	}
	for _, c := range bad {
		err := ix.CheckRelations(c.parents, c.siblings)
		if err == nil || !strings.Contains(err.Error(), c.wantErrSubstring) {
			t.Errorf("%s: got %v, want error with %q", c.name, err, c.wantErrSubstring)
		}
		if err := ix.SetRelations(c.parents, c.siblings); err == nil {
			t.Errorf("%s: SetRelations accepted", c.name)
		}
	}
	// Index unchanged after the rejected sets.
	expect(t, ix, "red", 3, 2)
}

func TestRemoveAndRestore(t *testing.T) {
	ix := fixture(t)
	ix.SetRelations([]Relation{{3, 5}}, nil)
	if err := ix.RemoveFile(1, []uint32{1, 3, 8}); err != nil {
		t.Fatal(err)
	}
	expect(t, ix, "", 4, 3, 2)
	expect(t, ix, "blue", 4, 3)
	expect(t, ix, "favorite_artists")
	expect(t, ix, "-red", 4)
	if s := ix.Stats(); s.Files != 3 {
		t.Errorf("Stats.Files = %d", s.Files)
	}
	// Tag edits on a deleted file are ignored (undo brings back its DB tags).
	ix.UpdateTags(1, []uint32{2}, nil, 0)
	expect(t, ix, "red", 3, 2)

	f := File{ID: 1, Size: 1 << 20, Type: media.JPEG, ImportedAt: now.UnixMilli() - 10*day}
	if err := ix.AddFile(f, []uint32{1, 3, 8}); err != nil {
		t.Fatal(err)
	}
	expect(t, ix, "favorite_artists", 1)
	expect(t, ix, "", 4, 3, 2, 1)
}

func TestAutocomplete(t *testing.T) {
	ix := fixture(t)
	ix.SetRelations(nil, []Relation{{9, 8}})
	got := ix.Autocomplete("smi", 10, "")
	want := []Suggestion{
		{Tag: "smiling", Count: 2},
		{Tag: "smiling_softly", Count: 2, Ideal: "smiling"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("smi: %+v", got)
	}

	got = ix.Autocomplete("BL", 10, "")
	if len(got) != 2 || got[0].Tag != "blue" || got[0].Count != 2 || got[1].Tag != "creator:blue" {
		t.Errorf("bl: %+v", got)
	}
	if got := ix.Autocomplete("creator:", 10, ""); len(got) != 3 {
		t.Errorf("creator: %+v", got)
	}
	if got := ix.Autocomplete("creator:o", 10, ""); len(got) != 1 || got[0].Tag != "creator:other_artist" {
		t.Errorf("creator:o: %+v", got)
	}
	if got := ix.Autocomplete("*16", 10, ""); len(got) != 1 || got[0].Tag != "9:16" {
		t.Errorf("*16: %+v", got)
	}
	if got := ix.Autocomplete("fav", 10, ""); len(got) != 0 {
		t.Errorf("unused tag suggested: %+v", got)
	}
	if got := ix.Autocomplete("b", 1, ""); len(got) != 1 {
		t.Errorf("limit ignored: %+v", got)
	}

	// New tags are found right away.
	ix.AddTags(Tag{10, 0, "aardvark"})
	ix.UpdateTags(2, []uint32{10}, nil, 0)
	if got := ix.Autocomplete("aa", 10, ""); len(got) != 1 || got[0].Count != 1 {
		t.Errorf("new tag: %+v", got)
	}

	// Not only the start: a word's start next (most used first within each), then inside a word.
	ix.AddTags(Tag{11, 0, "call_of_duty_modern_warfare"}, Tag{12, 0, "modern_warfare_2"}, Tag{13, 0, "postmodern_art"})
	ix.UpdateTags(1, []uint32{11, 13}, nil, 0)
	ix.UpdateTags(2, []uint32{11, 12, 13}, nil, 0)
	got = ix.Autocomplete("modern", 10, "")
	if len(got) != 3 || got[0].Tag != "modern_warfare_2" || got[1].Tag != "call_of_duty_modern_warfare" || got[2].Tag != "postmodern_art" {
		t.Errorf("modern: %+v", got)
	}
	if got := ix.Autocomplete("modern_warfare", 10, ""); len(got) != 2 || got[1].Tag != "call_of_duty_modern_warfare" {
		t.Errorf("modern_warfare: %+v", got)
	}
	for _, g := range ix.Autocomplete("o", 10, "") {
		if _, name, _ := strings.Cut(g.Tag, ":"); !strings.HasPrefix(name, "o") && !strings.HasPrefix(g.Tag, "o") {
			t.Errorf("one character: only names starting with it: %+v", g)
		}
	}
}

func TestUnknownTagIDs(t *testing.T) {
	ix := fixture(t)
	if err := ix.AddFile(File{ID: 9}, []uint32{42}); err == nil {
		t.Error("AddFile accepted an unknown tag")
	}
	if err := ix.UpdateTags(1, []uint32{42}, nil, 0); err == nil {
		t.Error("UpdateTags accepted an unknown tag")
	}
}

// A tag stored as "model:jane" before "model" was a namespace is still found
// by typing it.
func TestLiteralTagBeforeNamespace(t *testing.T) {
	ix := fixture(t)
	ix.AddTags(Tag{20, 0, "model:jane"})
	ms := now.UnixMilli()
	if err := ix.AddFile(File{ID: 5, Type: media.PNG, ImportedAt: ms, ModifiedAt: ms}, []uint32{20}); err != nil {
		t.Fatal(err)
	}
	expect(t, ix, "model:jane", 5) // "model" isn't a namespace yet: literal
	ix.AddNamespace(2, "model")
	ix.AddTags(Tag{21, 2, "jane"})
	expect(t, ix, "model:jane", 5) // now a namespace: the old literal tag still counts
	expect(t, ix, "jane")          // a plain name doesn't match the literal "model:jane"
}

// New files first: files imported after "since" go to the front in their own
// order; the rest keep the search's order.
func TestPinNew(t *testing.T) {
	ix := fixture(t)
	q, _ := search.Parse("sort:size") // largest first: 4, 2, 3, 1
	res := ix.Search(q, now)
	since := now.UnixMilli() - 3*day // files 2 (2 days ago) and 3 (an hour ago) are new
	got, n := ix.PinNew(res.IDs, NewOpts{Since: since}, search.Sort{})
	if n != 2 || !reflect.DeepEqual(got, []uint32{3, 2, 4, 1}) {
		t.Fatalf("newest first: %v (%d)", got, n)
	}
	got, _ = ix.PinNew(res.IDs, NewOpts{Since: since}, search.Sort{Asc: true})
	if !reflect.DeepEqual(got, []uint32{2, 3, 4, 1}) {
		t.Fatalf("oldest first: %v", got)
	}
	if c := ix.CountNew(res.IDs, NewOpts{Since: since}); c != 2 {
		t.Fatalf("count %d", c)
	}
	if got, n := ix.PinNew(res.IDs, NewOpts{Since: now.UnixMilli()}, search.Sort{}); n != 0 || !reflect.DeepEqual(got, res.IDs) {
		t.Fatalf("nothing new: %v %d", got, n)
	}
}

// system:new (the NEW files: imported after Since) and system:animated (a duration: videos, animated pictures), as
// filters - alone, negated, with other terms and in an OR group.
func TestNewAndAnimatedFilters(t *testing.T) {
	ix := fixture(t)
	ms := now.UnixMilli()
	if err := ix.AddFile(File{ID: 5, Size: 1, Type: media.GIF, DurationMS: 1200, ImportedAt: ms - 60_000, ModifiedAt: ms, Bulk: true}, nil); err != nil {
		t.Fatal(err)
	}
	ids := func(text string, since int64, skipBulk bool) []uint32 {
		t.Helper()
		q, err := search.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		for i := range q.Terms {
			for j := range q.Terms[i].Any {
				if a := &q.Terms[i].Any[j]; a.Sys.Field == search.FieldNew {
					a.Sys.Since, a.Sys.SkipBulk = since, skipBulk
				}
			}
		}
		got := ix.Search(q, now).IDs
		slices.Sort(got)
		return got
	}
	since := ms - 3*day // new: 2, 3 and 5 (bulk)
	for _, c := range []struct {
		text     string
		since    int64
		skipBulk bool
		want     []uint32
	}{
		{"system:new", since, false, []uint32{2, 3, 5}},
		{"system:new", since, true, []uint32{2, 3}},
		{"system:new", 0, false, nil}, // (never marked as seen: nothing is NEW, as the pills)
		{"-system:new", since, false, []uint32{1, 4}},
		{"system:animated", since, false, []uint32{2, 4, 5}},
		{"-system:animated", since, false, []uint32{1, 3}},
		{"system:new system:animated", since, false, []uint32{2, 5}},
		{"( system:new ~ system:animated )", since, false, []uint32{2, 3, 4, 5}},
	} {
		if got := ids(c.text, c.since, c.skipBulk); !slices.Equal(got, c.want) {
			t.Errorf("%s (since %d, skip bulk %v): %v, want %v", c.text, c.since, c.skipBulk, got, c.want)
		}
	}
	for _, text := range []string{"system:new", "system:animated", "-system:animated"} {
		if q, _ := search.Parse(text); q.String() != text {
			t.Errorf("%s reads back as %s", text, q.String())
		}
	}
	if _, err := search.Parse("system:animated>1"); err == nil {
		t.Error("system:animated took a value")
	}
}

// Without bulk files: only files from subscriptions' regular checks are new.
func TestPinNewSkipBulk(t *testing.T) {
	ix := fixture(t)
	f := File{ID: 5, Size: 1, Type: media.JPEG, ImportedAt: now.UnixMilli() - 60_000, ModifiedAt: now.UnixMilli(), Bulk: true}
	if err := ix.AddFile(f, nil); err != nil {
		t.Fatal(err)
	}
	q, _ := search.Parse("")
	res := ix.Search(q, now)
	since := now.UnixMilli() - 3*day // new: 2, 3 and 5 (bulk)
	if n := ix.CountNew(res.IDs, NewOpts{Since: since}); n != 3 {
		t.Fatalf("all new: %d", n)
	}
	got, n := ix.PinNew(res.IDs, NewOpts{Since: since, SkipBulk: true}, search.Sort{})
	if n != 2 || !reflect.DeepEqual(got[:2], []uint32{3, 2}) {
		t.Fatalf("without bulk: %v (%d)", got, n)
	}
}

// A random order keeps its files' order when a file is added: the new one
// lands somewhere in it (a whole-list shuffle would reshuffle everything).
func TestRandomStableWhenFilesArrive(t *testing.T) {
	ix := fixture(t)
	q, _ := search.Parse("sort:random_7")
	before := ix.Search(q, now).IDs
	ms := now.UnixMilli()
	if err := ix.AddFile(File{ID: 5, Type: media.PNG, ImportedAt: ms, ModifiedAt: ms}, []uint32{1}); err != nil {
		t.Fatal(err)
	}
	after := ix.Search(q, now).IDs
	if len(after) != len(before)+1 {
		t.Fatalf("after = %v", after)
	}
	rest := slices.DeleteFunc(slices.Clone(after), func(id uint32) bool { return id == 5 })
	if !slices.Equal(rest, before) {
		t.Errorf("the others reshuffled: %v, then %v", before, after)
	}
}

// With sites picked (system:site=): only the tags of files from those sites, counted on them.
func TestAutocompleteSites(t *testing.T) {
	ix := fixture(t)
	ix.AddSiteAlias("site_a", 1)
	ix.AddSiteAlias("site_b", 2)
	ix.AddFileSite(1, 1) // blue, some_artist, smiling
	ix.AddFileSite(3, 2) // blue, red, 9:16
	ix.AddFileSite(2, 2) // red, other_artist
	if got := ix.Autocomplete("blue", 10, ""); len(got) != 2 || got[0].Count != 2 {
		t.Fatalf("all sites: %+v", got)
	}
	if got := ix.Autocomplete("blue", 10, "site_a"); len(got) != 1 || got[0].Tag != "blue" || got[0].Count != 1 {
		t.Fatalf("site_a: %+v", got)
	}
	if got := ix.Autocomplete("r", 10, "site_a"); len(got) != 0 {
		t.Fatalf("red isn't on site_a's files: %+v", got)
	}
	if got := ix.Autocomplete("r", 10, "site_b"); len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("site_b: %+v", got)
	}
	if got := ix.Autocomplete("blue", 10, "site_a,site_b"); len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("both: %+v", got)
	}
	if got := ix.Autocomplete("blue", 10, "nowhere"); len(got) != 2 {
		t.Fatalf("a site without files (not downloaded from yet): every tag - %+v", got)
	}
}

// A day-only upload time (the site gives no more) sorts by post ID within that day, and stays on that day.
func TestPostedKeyDayOnly(t *testing.T) {
	day := time.Date(2019, 9, 16, 0, 0, 0, 0, time.UTC).UnixMilli()
	older := File{ID: 9, PostedAt: day, PostID: 812_000}
	newer := File{ID: 3, PostedAt: day, PostID: 812_500} // (downloaded first: a lower file ID)
	moment := File{ID: 4, PostedAt: day + 5*60*60*1000, PostID: 1}
	unknown := File{ID: 5, ImportedAt: 1234}
	if !(postedKey(older) < postedKey(newer)) {
		t.Fatalf("same day: post %d at %d, post %d at %d", older.PostID, postedKey(older), newer.PostID, postedKey(newer))
	}
	if k := postedKey(newer); k-day >= dayMS {
		t.Fatalf("left its day: %d", k)
	}
	if postedKey(moment) != moment.PostedAt || postedKey(unknown) != 1234 {
		t.Fatal("a moment or an unknown time changed")
	}
}
