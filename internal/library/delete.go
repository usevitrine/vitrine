package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
)

// Delete removes files from the library. They leave every search at once and
// get a tombstone (never downloaded or imported again); the originals stay
// in the store until Purge, so Undelete can bring them back.
func (l *Library) Delete(ctx context.Context, files []uint32, reason string) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		now := l.now().UnixMilli()
		type removed struct {
			id   uint32
			tags []uint32
		}
		var done []removed
		for _, id := range files {
			var sha, md5 []byte
			var deletedAt sql.NullInt64
			err := tx.QueryRowContext(ctx, "SELECT sha256, md5, deleted_at FROM files WHERE id = ?", id).
				Scan(&sha, &md5, &deletedAt)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("library: file %d: %w", id, ErrNoFile)
			}
			if err != nil {
				return nil, err
			}
			if deletedAt.Valid {
				continue // already deleted
			}
			tags, err := fileTags(ctx, tx, id)
			if err != nil {
				return nil, err
			}
			// A comic still has it: out of the library, kept as the comic's page (no tombstone).
			var inComic int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM comic_pages p JOIN comics c ON c.id = p.comic_id
				WHERE p.file_id = ? AND c.deleted_at IS NULL`, id).Scan(&inComic); err != nil {
				return nil, err
			}
			if inComic > 0 {
				if _, err := tx.ExecContext(ctx, "UPDATE files SET flags = flags | ? WHERE id = ?", FlagComicOnly, id); err != nil {
					return nil, err
				}
				done = append(done, removed{id, tags})
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE files SET deleted_at = ? WHERE id = ?", now, id); err != nil {
				return nil, err
			}
			// (with the sites it came from: removing a site can wipe its records too - WipeSite)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO tombstones (sha256, md5, deleted_at, reason, sites) VALUES (?1, ?2, ?3, ?4, coalesce((
					SELECT ',' || group_concat(s.name, ',') || ',' FROM sites s WHERE s.id != 0 AND s.id IN (
						SELECT site_id FROM file_urls WHERE file_id = ?5 UNION SELECT origin FROM file_tags WHERE file_id = ?5
						UNION SELECT site_id FROM file_sources WHERE file_id = ?5)), ''))
				ON CONFLICT (sha256) DO UPDATE SET deleted_at = excluded.deleted_at, reason = excluded.reason, sites = excluded.sites`,
				sha, md5, now, nullString(reason), id); err != nil {
				return nil, err
			}
			done = append(done, removed{id, tags})
		}
		return func() error {
			for _, r := range done {
				if err := l.ix.RemoveFile(r.id, r.tags); err != nil {
					return err
				}
			}
			return nil
		}, nil
	})
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ErrPurged means a deleted file's original is already gone; it can't be
// brought back.
var ErrPurged = errors.New("file was already purged")

// Undelete brings deleted files back (within the undo window) and removes
// their tombstones.
func (l *Library) Undelete(ctx context.Context, files []uint32) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		type restored struct {
			f    index.File
			tags []uint32
		}
		var done []restored
		for _, id := range files {
			f, sha, deleted, err := readFile(ctx, tx, id)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("library: file %d: %w", id, ErrPurged)
			}
			if err != nil {
				return nil, err
			}
			if !deleted {
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE files SET deleted_at = NULL WHERE id = ?", id); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM tombstones WHERE sha256 = ?", sha); err != nil {
				return nil, err
			}
			tags, err := fileTags(ctx, tx, id)
			if err != nil {
				return nil, err
			}
			done = append(done, restored{f, tags})
		}
		return func() error {
			for _, r := range done {
				if err := l.ix.AddFile(r.f, r.tags); err != nil {
					return err
				}
			}
			return nil
		}, nil
	})
}

// readFile loads a file's indexed values.
func readFile(ctx context.Context, tx *sql.Tx, id uint32) (f index.File, sha []byte, deleted bool, err error) {
	var typ, rating uint8
	var deletedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id, sha256, size, type, coalesce(width, 0), coalesce(height, 0), coalesce(duration_ms, 0),
		       coalesce(has_audio, 0), imported_at, modified_at, coalesce(site_rating, 0), deleted_at,
		       coalesce(posted_at, 0), bulk
		FROM files WHERE id = ?`, id).
		Scan(&f.ID, &sha, &f.Size, &typ, &f.Width, &f.Height, &f.DurationMS, &f.HasAudio,
			&f.ImportedAt, &f.ModifiedAt, &rating, &deletedAt, &f.PostedAt, &f.Bulk)
	f.Type, f.Rating = media.Type(typ), media.Rating(rating)
	if err == nil && f.PostedAt != 0 && f.PostedAt%(24*60*60*1000) == 0 {
		// (a day only: its post ID orders it within that day - index.postedKey)
		tx.QueryRowContext(ctx, "SELECT coalesce(max(post_id), 0) FROM seen_posts WHERE file_id = ?", id).Scan(&f.PostID)
	}
	return f, sha, deletedAt.Valid, err
}

// TrashIndex is the trash's search index (deleted files not removed for good yet; index.LoadTrash), built again
// after writes.
func (l *Library) TrashIndex(ctx context.Context) (*index.Index, error) {
	l.trashMu.Lock()
	defer l.trashMu.Unlock()
	at := l.writes.Load()
	if l.trashIx != nil && l.trashAt == at {
		return l.trashIx, nil
	}
	ix, err := index.LoadTrash(ctx, l.db.R)
	if err != nil {
		return nil, err
	}
	l.trashIx, l.trashAt = ix, at
	return ix, nil
}

// ComicsTrashIndex is the comics trash's search index (deleted comics; index.LoadComicsTrash), built again after
// writes.
func (l *Library) ComicsTrashIndex(ctx context.Context) (*index.Index, error) {
	l.trashMu.Lock()
	defer l.trashMu.Unlock()
	at := l.writes.Load()
	if l.comicTrashIx != nil && l.comicTrashAt == at {
		return l.comicTrashIx, nil
	}
	ix, err := index.LoadComicsTrash(ctx, l.db.R)
	if err != nil {
		return nil, err
	}
	l.comicTrashIx, l.comicTrashAt = ix, at
	return ix, nil
}

// PurgeComics removes deleted comics for good (from the trash): the comics, and their pages' files that nothing else
// has (deleted with them: only that comic's pages). Comics that aren't deleted are left alone. It returns how many
// comics went.
func (l *Library) PurgeComics(ctx context.Context, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	marks := "?" + strings.Repeat(",?", len(ids)-1)
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var files []any
	var n int
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		// (the pages only these comics have, deleted with them)
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT p.file_id FROM comic_pages p JOIN files f ON f.id = p.file_id
			WHERE p.comic_id IN (`+marks+`) AND f.deleted_at IS NOT NULL AND f.flags & 64 != 0
			AND NOT EXISTS (SELECT 1 FROM comic_pages o WHERE o.file_id = p.file_id AND o.comic_id NOT IN (`+marks+`))`, append(args, args...)...)
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			files = append(files, id)
		}
		rows.Close()
		res, err := tx.ExecContext(ctx, "DELETE FROM comics WHERE deleted_at IS NOT NULL AND id IN ("+marks+")", args...)
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		k, _ := res.RowsAffected()
		n = int(k)
		return nil, purgeEmptyCollections(ctx, tx)
	})
	if err != nil || len(files) == 0 {
		return n, err
	}
	_, err = l.purge(ctx, "deleted_at IS NOT NULL AND id IN (?"+strings.Repeat(",?", len(files)-1)+")", files, -1)
	return n, err
}

// EmptyComicsTrash removes every deleted comic for good (see PurgeComics).
func (l *Library) EmptyComicsTrash(ctx context.Context) (int, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT id FROM comics WHERE deleted_at IS NOT NULL")
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	return l.PurgeComics(ctx, ids)
}

// ComicsTrash counts the deleted comics and their own pages' size.
func (l *Library) ComicsTrash(ctx context.Context) (comics int, bytes int64, err error) {
	err = l.db.R.QueryRowContext(ctx, `SELECT count(*), coalesce((SELECT sum(f.size) FROM files f WHERE f.deleted_at IS NOT NULL AND f.flags & 64 != 0), 0)
		FROM comics WHERE deleted_at IS NOT NULL`).Scan(&comics, &bytes)
	return comics, bytes, err
}

// PurgeFiles removes deleted files for good (from the trash): their rows, tags and originals. Files that aren't
// deleted are left alone. It returns how many went.
func (l *Library) PurgeFiles(ctx context.Context, ids []uint32) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return l.purge(ctx, "deleted_at IS NOT NULL AND flags & 64 = 0 AND id IN (?"+strings.Repeat(",?", len(ids)-1)+")", args, -1)
}

// EmptyTrash removes every deleted library file for good (comics keep their own: see Purge).
func (l *Library) EmptyTrash(ctx context.Context) (int, error) {
	return l.purge(ctx, "deleted_at IS NOT NULL AND flags & 64 = 0", nil, -1)
}

// Purge permanently removes files deleted before cutoff: their rows go (with
// tags and URLs) and their originals are deleted from the store. Tombstones
// stay. It returns how many files were purged.
func (l *Library) Purge(ctx context.Context, cutoff time.Time) (int, error) {
	return l.purge(ctx, "deleted_at IS NOT NULL AND deleted_at < ?", []any{cutoff.UnixMilli()}, cutoff.UnixMilli())
}

// purgeBatch is how many files one purge transaction removes: a big trash goes in turns, so other changes (a
// setting, a notice) aren't kept waiting behind it - once one waited 11 s.
var purgeBatch = 500 // (a test makes it smaller)

// purge removes the deleted files the condition picks (and, with comicsBefore >= 0, comics deleted before then), in
// batches of purgeBatch.
func (l *Library) purge(ctx context.Context, where string, whereArgs []any, comicsBefore int64) (int, error) {
	total := 0
	var errs []error
	for {
		n, err := l.purgeSome(ctx, where, whereArgs, comicsBefore)
		total += n
		if err != nil {
			errs = append(errs, err)
			if n == 0 {
				break
			}
		}
		comicsBefore = -1 // (the comics went with the first batch)
		if n < purgeBatch || ctx.Err() != nil {
			break
		}
	}
	return total, errors.Join(errs...)
}

// purgeSome removes up to purgeBatch of the files the condition picks: their rows in one transaction, then their
// originals and thumbnails.
func (l *Library) purgeSome(ctx context.Context, where string, whereArgs []any, comicsBefore int64) (int, error) {
	type gone struct {
		id  uint32
		sha store.Hash
		typ media.Type
	}
	var purged []gone
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if comicsBefore >= 0 {
			if err := purgeComics(ctx, tx, comicsBefore); err != nil {
				return nil, err
			}
		}
		rows, err := tx.QueryContext(ctx, "SELECT id, sha256, type FROM files WHERE "+where+" LIMIT "+strconv.Itoa(purgeBatch), whereArgs...)
		if err != nil {
			return nil, err
		}
		var ids []uint32
		for rows.Next() {
			var id uint32
			var sha []byte
			var typ uint8
			if err := rows.Scan(&id, &sha, &typ); err != nil {
				rows.Close()
				return nil, err
			}
			g := gone{id: id}
			copy(g.sha[:], sha)
			g.typ = media.Type(typ)
			ids = append(ids, id)
			purged = append(purged, g)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE id = ?", id); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	if err != nil {
		return 0, err
	}
	// Originals go only after the rows are committed: a crash in between
	// leaves an unreferenced file, never a row without its file.
	var errs []error
	ids := make([]uint32, 0, len(purged))
	for _, g := range purged {
		if err := l.store.Remove(g.sha, g.typ); err != nil {
			errs = append(errs, err)
		}
		ids = append(ids, g.id)
	}
	if l.thumbs != nil { // (their thumbnails in one go: a trash of thousands in seconds, not minutes)
		if err := l.thumbs.Delete(ctx, ids...); err != nil {
			errs = append(errs, err)
		}
	}
	return len(purged), errors.Join(errs...)
}
