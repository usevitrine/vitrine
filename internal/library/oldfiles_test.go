package library

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/search"
)

// A delete of old files goes by the site's date: the last moment of the year is in, the first of the next isn't, and a
// file with no date is never picked (though the index gives it its import time). Restored or removed for good as one.
func TestOldFiles(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	dates := []time.Time{
		time.Date(2015, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2019, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		{}, // none
	}
	var ids []uint32
	for i, at := range dates {
		f, err := l.Import(ctx, bytes.NewReader(fakePNG(byte(90+i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
		if !at.IsZero() {
			if _, err := l.db.W.ExecContext(ctx, "UPDATE files SET posted_at = ? WHERE id = ?", at.UnixMilli(), f.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := l.SetFavorite(ctx, ids[:1], true); err != nil {
		t.Fatal(err)
	}
	years, undated, err := l.UploadYears(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(years) != 3 || years[0].Year != 2015 || years[0].Favorites != 1 || years[1].Year != 2019 || years[2].Year != 2020 || undated.Items != 1 || undated.Bytes == 0 {
		t.Fatalf("years %+v, undated %+v", years, undated)
	}
	live := func() int {
		q, _ := search.Parse("")
		return len(l.ix.Search(q, l.now()).IDs)
	}

	var last [2]int
	n, err := l.DeleteOld(ctx, 2019, func(done, total int) { last = [2]int{done, total} })
	if err != nil || n != 2 || last != [2]int{2, 2} || live() != 2 {
		t.Fatalf("deleted %d (%v), progress %v, %d left", n, err, last, live())
	}
	var deleted, tombs int
	l.db.R.QueryRowContext(ctx, "SELECT count(*) FROM files WHERE deleted_at IS NOT NULL AND id IN (?, ?)", ids[0], ids[1]).Scan(&deleted)
	l.db.R.QueryRowContext(ctx, "SELECT count(*) FROM tombstones").Scan(&tombs)
	if deleted != 2 || tombs != 2 {
		t.Fatalf("%d deleted, %d tombstones", deleted, tombs)
	}
	in, err := l.OldInTrash(ctx)
	if err != nil || len(in) != 1 || in[0].Year != 2019 || in[0].Files != 2 {
		t.Fatalf("in the trash: %+v (%v)", in, err)
	}
	// A file deleted another way isn't part of it.
	if err := l.Delete(ctx, ids[3:], ""); err != nil {
		t.Fatal(err)
	}

	noop := func(int, int) {}
	if n, err := l.RestoreOld(ctx, 2019, noop); err != nil || n != 2 || live() != 3 {
		t.Fatalf("restored %d (%v), %d live", n, err, live())
	}
	if n, _ := l.DeleteOld(ctx, 2019, noop); n != 2 {
		t.Fatalf("deleted again: %d", n)
	}
	// Stopped: nothing more goes.
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if n, err := l.PurgeOld(stopped, 2019, noop); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped: %d (%v)", n, err)
	}
	if n, err := l.PurgeOld(ctx, 2019, noop); err != nil || n != 2 {
		t.Fatalf("purged %d (%v)", n, err)
	}
	var rows int
	l.db.R.QueryRowContext(ctx, "SELECT count(*) FROM files").Scan(&rows)
	if rows != 2 { // (2020's, and the one deleted another way)
		t.Fatalf("%d files left", rows)
	}
	if in, _ := l.OldInTrash(ctx); len(in) != 0 {
		t.Fatalf("still in the trash: %+v", in)
	}
	// Year 0: the rest of the trash.
	if n, err := l.PurgeOld(ctx, 0, noop); err != nil || n != 1 {
		t.Fatalf("emptied %d (%v)", n, err)
	}
}
