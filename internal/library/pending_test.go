package library

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// The work still waiting is found through the partial indexes (migration 0046), not by walking every file: on a big
// library the walk was half of Vitrine's processor time. A condition worded differently from its index would quietly
// fall back to the walk, so the plans are checked; and what each finds is checked against the flags.
func TestPendingUsesIndexes(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	for _, c := range []struct{ sql, index string }{
		{pendingMediaSQL + " AND flags & 4 = 0 ORDER BY id DESC LIMIT 32", "files_pending_media"},
		{pendingMediaSQL + " ORDER BY id DESC", "files_pending_media"},
		{strings.ReplaceAll(pendingPHashSQL, "?", "500"), "files_pending_phash"},
		{strings.ReplaceAll(pendingSpritesSQL, "?", "50"), "files_pending_sprite"},
	} {
		rows, err := l.db.R.QueryContext(ctx, "EXPLAIN QUERY PLAN "+c.sql)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		if p := strings.Join(plan, "; "); !strings.Contains(p, c.index) {
			t.Errorf("%s:\n  plan %q, want %s", c.sql, p, c.index)
		}
	}

	// What they find: 1 probed, 2 thumbnail, 4 media error, 8 / 16 preview frames done / failed, 32 hash, 64 comic.
	for i, f := range []struct {
		typ, flags int
		deleted    bool
		dur        int
	}{
		{1, 0, false, 0},         // 1: an image, nothing done: media
		{1, 3, false, 0},         // 2: done, no hash: phash
		{1, 3 | 32, false, 0},    // 3: done and hashed: nothing
		{1, 3 | 64, false, 0},    // 4: comic-only: no hash wanted
		{6, 3, false, 5000},      // 5: a video: phash and preview frames
		{6, 3 | 8, false, 5000},  // 6: frames made: phash only
		{6, 3 | 16, false, 5000}, // 7: frames failed: phash only
		{1, 1 | 4, false, 0},     // 8: failed: media only when retrying
		{6, 1, true, 5000},       // 9: deleted: nothing
	} {
		var del any
		if f.deleted {
			del = 1
		}
		if _, err := l.db.W.ExecContext(ctx, `INSERT INTO files (sha256, md5, size, type, imported_at, modified_at, flags, duration_ms, deleted_at)
			VALUES (?, ?, 1, ?, 1, 1, ?, nullif(?, 0), ?)`, []byte{byte(i), 1}, []byte{byte(i), 2}, f.typ, f.flags, f.dur, del); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(got []uint32, err error) []uint32 {
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := ids(l.pendingMedia(ctx, false, 0)); !slices.Equal(got, []uint32{1}) {
		t.Errorf("media: %v", got)
	}
	if got := ids(l.pendingMedia(ctx, true, 0)); !slices.Equal(got, []uint32{8, 1}) {
		t.Errorf("media with the failed: %v", got)
	}
	if got := ids(l.PendingPHash(ctx, 50)); !slices.Equal(got, []uint32{7, 6, 5, 2}) {
		t.Errorf("phash: %v", got)
	}
	if got := ids(l.PendingSprites(ctx, 50)); !slices.Equal(got, []uint32{5}) {
		t.Errorf("preview frames: %v", got)
	}
}
