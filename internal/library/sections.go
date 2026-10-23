package library

import (
	"context"
	"fmt"
)

// The app's two sections (2026-10-23, the user: "an option to disable library and comics in the settings ... prompt
// the user if they want to delete all the data"): Media - the library's files - and Comics. Switching one off can
// delete everything in it for good.

// SectionSize is what a section holds (live, not in the trash).
type SectionSize struct {
	Items int   `json:"items"` // files, or comics
	Bytes int64 `json:"bytes"`
}

// MediaSize is the library's files (comics' own pages apart).
func (l *Library) MediaSize(ctx context.Context) (SectionSize, error) {
	var s SectionSize
	err := l.db.R.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(size), 0) FROM files WHERE deleted_at IS NULL AND flags & 64 = 0").Scan(&s.Items, &s.Bytes)
	return s, err
}

// ComicsSize is the comics and their pages.
func (l *Library) ComicsSize(ctx context.Context) (SectionSize, error) {
	var s SectionSize
	err := l.db.R.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM comics WHERE deleted_at IS NULL),
		coalesce((SELECT sum(f.size) FROM files f WHERE f.deleted_at IS NULL AND f.id IN
			(SELECT p.file_id FROM comic_pages p JOIN comics c ON c.id = p.comic_id WHERE c.deleted_at IS NULL)), 0)`).Scan(&s.Items, &s.Bytes)
	return s, err
}

// DeleteAllMedia deletes every library file for good (into the trash, then the trash emptied); comics keep their
// pages. It returns how many went.
func (l *Library) DeleteAllMedia(ctx context.Context) (int, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT id FROM files WHERE deleted_at IS NULL AND flags & 64 = 0")
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	var ids []uint32
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for len(ids) > 0 { // (in batches: one long write would hold every other write up)
		n := min(len(ids), 2000)
		if err := l.Delete(ctx, ids[:n], ""); err != nil {
			return 0, err
		}
		ids = ids[n:]
	}
	return l.EmptyTrash(ctx)
}

// DeleteAllComics deletes every comic for good (and their own pages). It returns how many went.
func (l *Library) DeleteAllComics(ctx context.Context) (int, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT id FROM comics WHERE deleted_at IS NULL")
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
	for len(ids) > 0 { // (in batches, as the files: one long write would hold every other write up)
		n := min(len(ids), 500)
		if err := l.DeleteComics(ctx, ids[:n]); err != nil {
			return 0, err
		}
		ids = ids[n:]
	}
	return l.EmptyComicsTrash(ctx)
}
