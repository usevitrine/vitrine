package library

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/tag"
)

func tags(names ...string) []tag.Tag {
	out := make([]tag.Tag, len(names))
	for i, n := range names {
		ns, name, ok := strings.Cut(n, ":")
		if ok {
			out[i] = tag.Tag{Namespace: ns, Name: name}
		} else {
			out[i] = tag.Tag{Name: n}
		}
	}
	return out
}

func importN(t *testing.T, l *Library, n int) []uint32 {
	t.Helper()
	var ids []uint32
	for i := 0; i < n; i++ {
		f, err := l.Import(context.Background(), bytes.NewReader(fakePNG(byte(100+i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
	}
	return ids
}

func find(t *testing.T, l *Library, text string) []uint32 {
	t.Helper()
	q, err := search.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	ids := l.Index().Search(q, time.Now()).IDs
	if ids == nil {
		ids = []uint32{}
	}
	return ids
}

func want(t *testing.T, l *Library, text string, ids ...uint32) {
	t.Helper()
	if ids == nil {
		ids = []uint32{}
	}
	if got := find(t, l, text); !reflect.DeepEqual(got, ids) {
		t.Errorf("%q = %v, want %v", text, got, ids)
	}
}

func TestImportIsSearchable(t *testing.T) {
	l := newTest(t)
	ids := importN(t, l, 3)
	want(t, l, "", ids[2], ids[1], ids[0])
	want(t, l, "system:type=png system:imported<1h", ids[2], ids[1], ids[0])
}

func TestUserAndSiteTags(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 2)
	a, b := ids[0], ids[1]
	site, err := l.Site(ctx, "safebooru")
	if err != nil {
		t.Fatal(err)
	}

	if err := l.SetSiteTags(ctx, a, site, tags("blue_sky", "cloud", "creator:some_artist")); err != nil {
		t.Fatal(err)
	}
	if err := l.AddTags(ctx, []uint32{a, b}, tags("favorite")); err != nil {
		t.Fatal(err)
	}
	want(t, l, "cloud", a)
	want(t, l, "favorite", b, a)
	want(t, l, "creator:some_artist", a)
	want(t, l, "some_artist", a)

	// The user removes a site tag: hidden, and a re-sync doesn't restore it.
	if err := l.RemoveTags(ctx, []uint32{a}, tags("cloud")); err != nil {
		t.Fatal(err)
	}
	want(t, l, "cloud")
	if err := l.SetSiteTags(ctx, a, site, tags("blue_sky", "cloud", "creator:some_artist", "new_tag")); err != nil {
		t.Fatal(err)
	}
	want(t, l, "cloud")
	want(t, l, "new_tag", a)

	// The site drops a tag; user tags survive the re-sync.
	if err := l.SetSiteTags(ctx, a, site, tags("blue_sky")); err != nil {
		t.Fatal(err)
	}
	want(t, l, "new_tag")
	want(t, l, "favorite", b, a)

	// Adding a hidden tag by hand brings it back.
	if err := l.AddTags(ctx, []uint32{a}, tags("cloud")); err != nil {
		t.Fatal(err)
	}
	want(t, l, "cloud", a)

	// Removing a tag that doesn't exist is a no-op and creates nothing.
	if err := l.RemoveTags(ctx, []uint32{a}, tags("never_seen")); err != nil {
		t.Fatal(err)
	}
	var n int
	l.db.R.QueryRow("SELECT count(*) FROM tags WHERE name = 'never_seen'").Scan(&n)
	if n != 0 {
		t.Error("RemoveTags created a tag")
	}

	if err := l.AddTags(ctx, []uint32{999}, tags("x")); !errors.Is(err, ErrNoFile) {
		t.Errorf("tagging a missing file: %v", err)
	}
	checkReload(t, l)
}

func TestModifiedTime(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	id := importN(t, l, 1)[0]
	clock := time.Now().Add(time.Hour)
	l.now = func() time.Time { return clock }

	modified := func() int64 {
		var m int64
		l.db.R.QueryRow("SELECT modified_at FROM files WHERE id = ?", id).Scan(&m)
		return m
	}
	before := modified()
	l.AddTags(ctx, []uint32{id}, tags("a"))
	if modified() != clock.UnixMilli() || modified() == before {
		t.Fatal("modified time not updated on a tag change")
	}
	clock = clock.Add(time.Hour)
	l.AddTags(ctx, []uint32{id}, tags("a")) // no change
	if modified() == clock.UnixMilli() {
		t.Fatal("modified time updated without a change")
	}
}

// A site's tag refresh doesn't count as a change to the file ("modified recently" filled up with retagged files);
// a site new to the file (the same file from another site) does, as the user's own edits do.
func TestModifiedTimeSiteTags(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	id := importN(t, l, 1)[0]
	one, err := l.Site(ctx, "safebooru.org")
	if err != nil {
		t.Fatal(err)
	}
	two, err := l.Site(ctx, "example.org")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now().Add(time.Hour)
	l.now = func() time.Time { return clock }
	modified := func() int64 {
		var m int64
		l.db.R.QueryRow("SELECT modified_at FROM files WHERE id = ?", id).Scan(&m)
		return m
	}
	if err := l.SetSiteTags(ctx, id, one, tags("blue_sky")); err != nil {
		t.Fatal(err)
	}
	if modified() != clock.UnixMilli() {
		t.Fatal("a site's first tags didn't count")
	}
	first := modified()
	clock = clock.Add(time.Hour)
	if err := l.SetSiteTags(ctx, id, one, tags("blue_sky", "cloud")); err != nil { // a refresh
		t.Fatal(err)
	}
	if modified() != first {
		t.Fatal("a tag refresh changed the modified time")
	}
	want(t, l, "cloud", id) // (the new tag is there all the same)
	clock = clock.Add(time.Hour)
	if err := l.SetSiteTags(ctx, id, two, tags("some_artist")); err != nil { // another site
		t.Fatal(err)
	}
	if modified() != clock.UnixMilli() {
		t.Fatal("a new site's tags didn't count")
	}
	clock = clock.Add(time.Hour)
	if err := l.AddTags(ctx, []uint32{id}, tags("favorite_artists")); err != nil {
		t.Fatal(err)
	}
	if modified() != clock.UnixMilli() {
		t.Fatal("the user's own tag didn't count")
	}
}

func TestRelations(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 3)
	l.AddTags(ctx, ids[:1], tags("some_artist"))
	l.AddTags(ctx, ids[1:2], tags("other_artist"))
	l.AddTags(ctx, ids[2:3], tags("smiling_softly"))

	if err := l.AddParent(ctx, tag.Tag{Name: "some_artist"}, tag.Tag{Name: "favorite_artists"}); err != nil {
		t.Fatal(err)
	}
	if err := l.AddParent(ctx, tag.Tag{Name: "other_artist"}, tag.Tag{Name: "favorite_artists"}); err != nil {
		t.Fatal(err)
	}
	want(t, l, "favorite_artists", ids[1], ids[0])

	err := l.AddParent(ctx, tag.Tag{Name: "favorite_artists"}, tag.Tag{Name: "some_artist"})
	if err == nil || !strings.Contains(err.Error(), "loop") {
		t.Fatalf("loop accepted: %v", err)
	}
	if err := l.AddParent(ctx, tag.Tag{Name: "x"}, tag.Tag{Name: "x"}); err == nil {
		t.Fatal("self parent accepted")
	}

	if err := l.SetSibling(ctx, tag.Tag{Name: "smiling_softly"}, tag.Tag{Name: "smiling"}); err != nil {
		t.Fatal(err)
	}
	want(t, l, "smiling", ids[2])
	sugg := l.Index().Autocomplete("smil", 5, "")
	if len(sugg) != 2 || sugg[1].Ideal != "smiling" {
		t.Errorf("autocomplete with sibling: %+v", sugg)
	}
	checkReload(t, l)

	if err := l.RemoveParent(ctx, tag.Tag{Name: "some_artist"}, tag.Tag{Name: "favorite_artists"}); err != nil {
		t.Fatal(err)
	}
	want(t, l, "favorite_artists", ids[1])
	if err := l.RemoveSibling(ctx, tag.Tag{Name: "smiling_softly"}); err != nil {
		t.Fatal(err)
	}
	want(t, l, "smiling")
	if err := l.RemoveParent(ctx, tag.Tag{Name: "nope"}, tag.Tag{Name: "favorite_artists"}); err != nil {
		t.Fatalf("removing a relation of an unknown tag: %v", err)
	}
	checkReload(t, l)
}

func TestDeleteUndoPurge(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 2)
	l.AddTags(ctx, ids, tags("shared"))
	l.AddTags(ctx, ids[:1], tags("only_first"))

	if err := l.Delete(ctx, ids[:1], "test"); err != nil {
		t.Fatal(err)
	}
	want(t, l, "", ids[1])
	want(t, l, "shared", ids[1])
	if s := l.Index().Autocomplete("only", 5, ""); len(s) != 0 {
		t.Errorf("deleted file's tag still suggested: %+v", s)
	}
	// Deleted files can't be imported again.
	if _, err := l.Import(ctx, bytes.NewReader(fakePNG(100))); !errors.Is(err, ErrDeleted) {
		t.Fatalf("re-import of a deleted file: %v", err)
	}
	checkReload(t, l)

	if err := l.Undelete(ctx, ids[:1]); err != nil {
		t.Fatal(err)
	}
	want(t, l, "only_first", ids[0])
	want(t, l, "", ids[1], ids[0])
	checkReload(t, l)

	// Purge: only files deleted before the cutoff.
	l.Delete(ctx, ids[:1], "")
	n, err := l.Purge(ctx, time.Now().Add(-time.Hour))
	if err != nil || n != 0 {
		t.Fatalf("purged %d (%v), want 0", n, err)
	}
	n, err = l.Purge(ctx, time.Now().Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("purged %d (%v), want 1", n, err)
	}
	if err := l.Undelete(ctx, ids[:1]); !errors.Is(err, ErrPurged) {
		t.Fatalf("undelete after purge: %v", err)
	}
	var tomb int
	l.db.R.QueryRow("SELECT count(*) FROM tombstones").Scan(&tomb)
	if tomb != 1 {
		t.Fatalf("%d tombstones, want 1", tomb)
	}
	checkReload(t, l)
}

// checkReload builds a fresh index from the database and checks it answers
// like the live one, which was kept up to date change by change.
func checkReload(t *testing.T, l *Library) {
	t.Helper()
	fresh, err := index.Load(context.Background(), l.db.R)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	queries := []string{
		"", "sort:modified", "sort:tags", "system:tags>0", "-system:tags>0",
		"shared", "only_first", "cloud", "blue_sky", "favorite", "some_artist", "creator:some_artist",
		"favorite_artists", "smiling", "smiling_softly", "-shared", "*a*", "system:favorite", "-system:favorite", "sort:favorite", "system:came>0", "sort:came",
	}
	for _, text := range queries {
		q, _ := search.Parse(text)
		live, want := l.Index().Search(q, now), fresh.Search(q, now)
		if !reflect.DeepEqual(live.IDs, want.IDs) {
			t.Errorf("after reload, %q: live %v, from DB %v", text, live.IDs, want.IDs)
		}
	}
	if a, b := l.Index().Stats(), fresh.Stats(); a != b {
		t.Errorf("stats: live %+v, from DB %+v", a, b)
	}
	for _, p := range []string{"s", "c", "k", "b"} {
		if a, b := l.Index().Autocomplete(p, 50, ""), fresh.Autocomplete(p, 50, ""); !reflect.DeepEqual(a, b) {
			t.Errorf("autocomplete %q: live %+v, from DB %+v", p, a, b)
		}
	}
}

func TestSiteTagTypesMoveTags(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 3)
	a, b, c := ids[0], ids[1], ids[2]
	site, _ := l.Site(ctx, "safebooru")
	if err := l.EnsureNamespaces(ctx, []string{"creator", "meta"}); err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureNamespaces(ctx, []string{"creator"}); err != nil { // again: no-op
		t.Fatal(err)
	}
	if got := l.Namespaces(); !reflect.DeepEqual(got, []string{"creator", "meta"}) {
		t.Fatalf("namespaces: %v", got)
	}
	// Before types were known: plain tags, a user tag and a parent on some_artist.
	l.SetSitePost(ctx, a, site, SitePost{Tags: []string{"some_artist", "blue_sky"}})
	l.AddTags(ctx, []uint32{c}, tags("some_artist"))
	if err := l.AddParent(ctx, tag.Tag{Name: "some_artist"}, tag.Tag{Name: "favorite_artists"}); err != nil {
		t.Fatal(err)
	}
	// A post with types: some_artist is an artist, highres is meta.
	err := l.SetSitePost(ctx, b, site, SitePost{Tags: []string{"some_artist", "blue_sky", "highres"},
		Namespaces: map[string]string{"some_artist": "creator", "highres": "meta"}})
	if err != nil {
		t.Fatal(err)
	}
	// One tag, now creator:some_artist everywhere: earlier files, the user's tag, the parent.
	want(t, l, "creator:some_artist", c, b, a)
	want(t, l, "some_artist", c, b, a)
	want(t, l, "favorite_artists", c, b, a)
	want(t, l, "meta:highres", b)
	want(t, l, "blue_sky", b, a)
	var n int
	l.db.R.QueryRow("SELECT count(*) FROM tags WHERE name = 'some_artist'").Scan(&n)
	if n != 1 {
		t.Fatalf("%d some_artist tags, want 1", n)
	}
	ft, _ := l.FileTags(ctx, a)
	if ft[len(ft)-1].Tag != "creator:some_artist" && ft[0].Tag != "creator:some_artist" {
		t.Fatalf("file tags: %+v", ft)
	}
	if got := l.Index().Autocomplete("som", 5, ""); len(got) != 1 || got[0].Tag != "creator:some_artist" {
		t.Fatalf("autocomplete: %+v", got)
	}
	checkReload(t, l)
}

func TestPlainNameFindsNamespacedTag(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 2)
	site, _ := l.Site(ctx, "safebooru")
	l.EnsureNamespaces(ctx, []string{"creator"})
	l.SetSitePost(ctx, ids[0], site, SitePost{Tags: []string{"some_artist"}, Namespaces: map[string]string{"some_artist": "creator"}})
	// Typed or sent without a namespace: the same tag, not a new plain one.
	l.AddTags(ctx, ids[1:], tags("some_artist"))
	l.SetSitePost(ctx, ids[1], site, SitePost{Tags: []string{"some_artist", "other"}})
	var n int
	l.db.R.QueryRow("SELECT count(*) FROM tags WHERE name = 'some_artist'").Scan(&n)
	if n != 1 {
		t.Fatalf("%d some_artist tags, want 1", n)
	}
	want(t, l, "creator:some_artist", ids[1], ids[0])
	l.RemoveTags(ctx, ids[1:], tags("some_artist"))
	want(t, l, "creator:some_artist", ids[0])
	checkReload(t, l)
}

// A comic's tag of the same name (other:3d from a gallery site) doesn't make a plain site tag ambiguous: it still
// lands on the library's meta:3d, and re-syncing a file changes nothing (its modified time stays).
func TestComicTagsDontSplitSiteTags(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 1)
	site, _ := l.Site(ctx, "safebooru")
	l.EnsureNamespaces(ctx, []string{"meta"})
	l.SetSitePost(ctx, ids[0], site, SitePost{Tags: []string{"3d"}, Namespaces: map[string]string{"3d": "meta"}})
	if _, _, err := l.AddComic(ctx, ComicInfo{Title: "Some comic", Site: "examplebooru", Gallery: "1/abc", Pages: 1,
		Tags: []tag.Tag{{Namespace: "other", Name: "3d"}}}); err != nil {
		t.Fatal(err)
	}
	var before int64
	l.db.R.QueryRow("SELECT modified_at FROM files WHERE id = ?", ids[0]).Scan(&before)
	other, _ := l.Site(ctx, "examplebooru")
	l.SetSitePost(ctx, ids[0], other, SitePost{Tags: []string{"3d"}}) // (a site without tag types)
	var plain int
	l.db.R.QueryRow("SELECT count(*) FROM tags WHERE name = '3d' AND namespace_id = 0").Scan(&plain)
	if plain != 0 {
		t.Fatal("a plain 3d was made")
	}
	var after int64
	l.db.R.QueryRow("SELECT modified_at FROM files WHERE id = ?", ids[0]).Scan(&after)
	if after != before {
		t.Fatal("the file counts as modified")
	}
	want(t, l, "meta:3d", ids[0])
}

func TestSiteFilter(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 4)
	r34, _ := l.Site(ctx, "safebooru")
	rb, _ := l.Site(ctx, "examplebooru")
	l.Index().AddSiteAlias("safebooru.org", r34)
	l.SetSitePost(ctx, ids[0], r34, SitePost{Tags: []string{"a"}, PostURL: "https://r34.test/1"})
	l.SetSitePost(ctx, ids[1], rb, SitePost{Tags: []string{"a"}, PostURL: "https://rb.test/1"})
	l.SetSitePost(ctx, ids[2], r34, SitePost{Tags: []string{"b"}, PostURL: "https://r34.test/2"})
	l.SetSitePost(ctx, ids[2], rb, SitePost{Tags: []string{"b"}, PostURL: "https://rb.test/2"}) // on both sites
	want(t, l, "system:site=safebooru", ids[2], ids[0])
	want(t, l, "system:site=safebooru.org", ids[2], ids[0]) // by address
	want(t, l, "system:site=EXAMPLEBOORU a", ids[1])
	want(t, l, "system:site!=safebooru", ids[3], ids[1])
	want(t, l, "-system:site=examplebooru", ids[3], ids[0])
	want(t, l, "( system:site=safebooru ~ system:site=examplebooru )", ids[2], ids[1], ids[0])
	want(t, l, "system:site=nowhere")
	// Several sites: from any of them (and != none of them).
	want(t, l, "system:site=safebooru,examplebooru", ids[2], ids[1], ids[0])
	want(t, l, "system:site=nowhere,examplebooru", ids[2], ids[1])
	want(t, l, "system:site!=safebooru.org,examplebooru", ids[3])
	want(t, l, "system:site=safebooru,examplebooru a", ids[1], ids[0])
	want(t, l, "system:site=nowhere,elsewhere")
	if c := l.Index().SiteCounts(find(t, l, "")); c[r34] != 2 || c[rb] != 2 {
		t.Fatalf("counts: %v", c)
	}
	// From the database: the same answers (the address alias is the downloader's to add).
	fresh, err := index.Load(ctx, l.db.R)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := search.Parse("system:site=examplebooru")
	if got := fresh.Search(q, time.Now()).IDs; !reflect.DeepEqual(got, []uint32{ids[2], ids[1]}) {
		t.Fatalf("after reload: %v", got)
	}
	if _, err := search.Parse("sort:site"); err == nil {
		t.Fatal("sort:site accepted")
	}
}

// A day-only upload time read again (a tag refresh of a page that says only the day) leaves the moment the download
// found on that day; an earlier day still wins (the earliest posting).
func TestPostedAtDayKeepsMoment(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	id := importN(t, l, 1)[0]
	site, err := l.Site(ctx, "safebooru.org")
	if err != nil {
		t.Fatal(err)
	}
	posted := func() int64 {
		var p int64
		l.db.R.QueryRow("SELECT coalesce(posted_at, 0) FROM files WHERE id = ?", id).Scan(&p)
		return p
	}
	day := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC).UnixMilli()
	moment := time.Date(2026, 10, 21, 20, 4, 25, 0, time.UTC).UnixMilli()
	set := func(at int64) {
		t.Helper()
		if err := l.SetSitePost(ctx, id, site, SitePost{Tags: []string{"blue_sky"}, PostedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	set(moment)
	set(day)
	if p := posted(); p != moment {
		t.Fatalf("posted %d after the day again, want the moment %d", p, moment)
	}
	earlier := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC).UnixMilli()
	set(earlier)
	if p := posted(); p != earlier {
		t.Fatalf("posted %d, want the earlier day %d", p, earlier)
	}
}
