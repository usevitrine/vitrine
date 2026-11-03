package library

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
)

// Old files (2026-11-03, the user: delete "files uploaded from x year and older"): every file whose upload time - the
// date its site gives, posted_at - falls in a chosen year or before, through the normal delete: trash, tombstones,
// restore. A file with no upload time is never picked: nothing says it's old. Years are UTC.
//
// All of it reads the database, not the index: the index's upload time is the import time for a file without one.

// oldReason starts the tombstones' reason, which says what such a delete took: it can be restored or removed for good
// as one, also after a restart.
const oldReason = "uploaded in or before "

// YearEnd is the first moment after a year (Unix ms): files uploaded in it or before are earlier than this.
func YearEnd(year int) int64 {
	return time.Date(year+1, time.January, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
}

// YearFiles is how many files a year has.
type YearFiles struct {
	Year      int   `json:"year"`
	Files     int   `json:"files"`
	Bytes     int64 `json:"bytes"`
	Favorites int   `json:"favorites,omitempty"`
}

// UploadYears counts the library's files per upload year (oldest first), and those with no upload time.
func (l *Library) UploadYears(ctx context.Context) (years []YearFiles, undated SectionSize, err error) {
	rows, err := l.db.R.QueryContext(ctx, `SELECT CASE WHEN posted_at > 0 THEN CAST(strftime('%Y', posted_at / 1000, 'unixepoch') AS INTEGER) ELSE 0 END AS y,
		count(*), coalesce(sum(size), 0), coalesce(sum(id IN (SELECT file_id FROM favorites)), 0)
		FROM files WHERE deleted_at IS NULL AND flags & 64 = 0 GROUP BY y ORDER BY y`)
	if err != nil {
		return nil, undated, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	years = []YearFiles{}
	for rows.Next() {
		var y YearFiles
		if err := rows.Scan(&y.Year, &y.Files, &y.Bytes, &y.Favorites); err != nil {
			return nil, undated, fmt.Errorf("library: %w", err)
		}
		if y.Year == 0 {
			undated = SectionSize{Items: y.Files, Bytes: y.Bytes}
			continue
		}
		years = append(years, y)
	}
	return years, undated, rows.Err()
}

// OldInTrash lists what deletes of old files have left in the trash, per year chosen.
func (l *Library) OldInTrash(ctx context.Context) ([]YearFiles, error) {
	rows, err := l.db.R.QueryContext(ctx, `SELECT t.reason, count(*), coalesce(sum(f.size), 0) FROM files f JOIN tombstones t ON t.sha256 = f.sha256
		WHERE f.deleted_at IS NOT NULL AND f.flags & 64 = 0 AND t.reason LIKE ? GROUP BY t.reason ORDER BY t.reason`, oldReason+"%")
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := []YearFiles{}
	for rows.Next() {
		var reason string
		var y YearFiles
		if err := rows.Scan(&reason, &y.Files, &y.Bytes); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if y.Year, err = strconv.Atoi(strings.TrimPrefix(reason, oldReason)); err == nil {
			out = append(out, y)
		}
	}
	return out, rows.Err()
}

// inBatches runs fn over ids a batch at a time (one long write would hold every other write up), saying how far it
// has come. It stops between batches when ctx ends, and returns how many went.
func inBatches(ctx context.Context, ids []uint32, size int, progress func(done, total int), fn func([]uint32) error) (int, error) {
	done, total := 0, len(ids)
	progress(0, total)
	for len(ids) > 0 {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		n := min(len(ids), size)
		if err := fn(ids[:n]); err != nil {
			return done, err
		}
		ids = ids[n:]
		done += n
		progress(done, total)
	}
	return done, nil
}

// DeleteOld moves the files uploaded in the year or before into the trash (Delete: never downloaded again unless
// restored). Stopped part-way, what went stays in the trash. It returns how many went.
func (l *Library) DeleteOld(ctx context.Context, year int, progress func(done, total int)) (int, error) {
	ids, err := db.Column[uint32](ctx, l.db.R, "SELECT id FROM files WHERE deleted_at IS NULL AND flags & 64 = 0 AND posted_at > 0 AND posted_at < ?", YearEnd(year))
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	reason := oldReason + strconv.Itoa(year)
	return inBatches(ctx, ids, 2000, progress, func(b []uint32) error { return l.Delete(ctx, b, reason) })
}

// oldTrashed lists the trash's files that a delete of that year took (year 0: everything in the trash).
func (l *Library) oldTrashed(ctx context.Context, year int) ([]uint32, error) {
	var ids []uint32
	var err error
	if year == 0 {
		ids, err = db.Column[uint32](ctx, l.db.R, "SELECT id FROM files WHERE deleted_at IS NOT NULL AND flags & 64 = 0")
	} else {
		ids, err = db.Column[uint32](ctx, l.db.R, `SELECT f.id FROM files f JOIN tombstones t ON t.sha256 = f.sha256
			WHERE f.deleted_at IS NOT NULL AND f.flags & 64 = 0 AND t.reason = ?`, oldReason+strconv.Itoa(year))
	}
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	return ids, nil
}

// RestoreOld brings back what a delete of that year left in the trash.
func (l *Library) RestoreOld(ctx context.Context, year int, progress func(done, total int)) (int, error) {
	ids, err := l.oldTrashed(ctx, year)
	if err != nil {
		return 0, err
	}
	return inBatches(ctx, ids, 2000, progress, func(b []uint32) error { return l.Undelete(ctx, b) })
}

// PurgeOld removes for good what a delete of that year left in the trash (year 0: the whole trash), saying how far it
// has come - the trash page's own turns would take hours at this size. This is what frees the disk.
func (l *Library) PurgeOld(ctx context.Context, year int, progress func(done, total int)) (int, error) {
	ids, err := l.oldTrashed(ctx, year)
	if err != nil {
		return 0, err
	}
	return inBatches(ctx, ids, purgeBatch, progress, func(b []uint32) error {
		_, err := l.PurgeFiles(ctx, b)
		return err
	})
}
