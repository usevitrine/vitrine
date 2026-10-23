package library

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/tag"
)

func TestClimaxes(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 3)
	if err := l.AddTags(ctx, ids[:2], tags("blue_sky", "creator:some_artist", "character:some_character", "series:some_series")); err != nil {
		t.Fatal(err)
	}
	day := 24 * time.Hour
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	press := func(file uint32, at time.Time) int64 {
		l.now = func() time.Time { return at }
		id, _, err := l.AddClimax(ctx, file)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	press(ids[0], now.Add(-10*day)) // outside the week
	press(ids[0], now.Add(-2*day))
	press(ids[1], now.Add(-1*day))
	press(ids[0], now)
	undo := press(ids[2], now)
	if _, n, err := l.UndoClimax(ctx, undo); err != nil || n != 0 {
		t.Fatalf("undo: %d, %v", n, err)
	}
	l.now = func() time.Time { return now }
	want(t, l, "system:came>0 sort:came", ids[0], ids[1])
	want(t, l, "system:came>2", ids[0])
	if rows, _ := l.Files(ctx, ids[:1]); rows[0].Climaxes != 3 {
		t.Fatalf("file row: %+v", rows[0])
	}
	r, err := l.Rankings(ctx, now.Add(-7*day).UnixMilli(), 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 3 || len(r.Days) != 7 || r.Days[6] != 1 || r.Days[5] != 1 || r.Days[4] != 1 || r.Streak != 3 {
		t.Fatalf("week: total %d, days %v, streak %d", r.Total, r.Days, r.Streak)
	}
	if len(r.Files) != 2 || r.Files[0].ID != ids[0] || r.Files[0].Count != 2 {
		t.Fatalf("files %+v", r.Files)
	}
	if len(r.Creators) != 1 || r.Creators[0].Tag != "creator:some_artist" || r.Creators[0].Count != 3 || len(r.Tags) != 1 || r.Tags[0].Tag != "blue_sky" {
		t.Fatalf("creators %+v, tags %+v", r.Creators, r.Tags)
	}
	if len(r.Characters) != 1 || r.Characters[0].Tag != "character:some_character" || len(r.Series) != 1 || r.Series[0].Tag != "series:some_series" {
		t.Fatalf("characters %+v, series %+v", r.Characters, r.Series)
	}
	all, _ := l.Rankings(ctx, 0, 0, 0)
	if all.Total != 4 || all.First != now.Add(-10*day).UnixMilli() {
		t.Fatalf("all time: %+v", all)
	}
	// Taken off: the latest press, then the rest of a file; then everything.
	if n, err := l.RemoveClimaxes(ctx, ids[0], false); err != nil || n != 2 {
		t.Fatalf("remove one: %d, %v", n, err)
	}
	if n, err := l.RemoveClimaxes(ctx, ids[0], true); err != nil || n != 0 {
		t.Fatalf("reset: %d, %v", n, err)
	}
	want(t, l, "system:came>0", ids[1])
	if n, err := l.ClearClimaxes(ctx); err != nil || n != 1 {
		t.Fatalf("clear: %d, %v", n, err)
	}
	want(t, l, "system:came>0")
	checkReload(t, l)
}

// "I came" in the comic reader: counted on a page; the Rankings show that page (with the comic's title), apart from
// the files, and the comic's tags count with the others.
func TestComicClimaxes(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	id, _, err := l.AddComic(ctx, ComicInfo{Title: "Some comic", Site: "examplebooru", Gallery: "9/abc", Pages: 3,
		Tags: []tag.Tag{{Namespace: "creator", Name: "some_artist"}}})
	if err != nil {
		t.Fatal(err)
	}
	for p := range 3 {
		if _, err := l.AddComicPage(ctx, id, p, bytes.NewReader(fakePNG(byte(30+p)))); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if _, _, err := l.AddComicClimax(ctx, id, 2); err != nil {
			t.Fatal(err)
		}
	}
	press, n, err := l.AddComicClimax(ctx, id, 0)
	if err != nil || n != 1 {
		t.Fatalf("page 1: %d %v", n, err)
	}
	if _, _, err := l.AddComicClimax(ctx, id, 7); err == nil {
		t.Fatal("a page the comic doesn't have was counted")
	}
	by, err := l.ComicClimaxes(ctx, id)
	if err != nil || by[2] != 2 || by[0] != 1 {
		t.Fatalf("by page: %v %v", by, err)
	}
	r, err := l.Rankings(ctx, 0, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 3 || len(r.Files) != 0 || len(r.Pages) != 2 || r.Pages[0].Page != 2 || r.Pages[0].Count != 2 || r.Pages[0].Title != "Some comic" || r.Pages[0].File == 0 {
		t.Fatalf("rankings: %+v", r)
	}
	if len(r.Creators) != 1 || r.Creators[0].Tag != "creator:some_artist" || r.Creators[0].Count != 3 {
		t.Fatalf("creators: %+v", r.Creators)
	}
	// Undo: the page's press goes.
	if _, _, err := l.UndoClimax(ctx, press); err != nil {
		t.Fatal(err)
	}
	if by, _ := l.ComicClimaxes(ctx, id); by[0] != 0 {
		t.Fatalf("after undo: %v", by)
	}
}
