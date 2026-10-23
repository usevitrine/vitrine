package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// A comic page's key on its site (e-hentai: the start of the original picture's SHA-1, in the page's link - the same
// in every gallery): a download asks the site only for the pages the library doesn't have by their keys.

// PageKey is a stored comic page's key on its site ("" = none kept) and its file.
type PageKey struct {
	Key  string
	File uint32
}

// ComicPageKeys lists a comic's stored pages (0-based) with their keys.
func (l *Library) ComicPageKeys(ctx context.Context, comic int64) (map[int]PageKey, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT p.page, coalesce(p.site_key, ''), p.file_id FROM comic_pages p JOIN files f ON f.id = p.file_id AND f.deleted_at IS NULL
		WHERE p.comic_id = ?`, comic)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := map[int]PageKey{}
	for rows.Next() {
		var p int
		var k PageKey
		if err := rows.Scan(&p, &k.Key, &k.File); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		out[p] = k
	}
	return out, rows.Err()
}

// PageByKey finds a stored comic page's file by its key, in any comic (0 = none).
func (l *Library) PageByKey(ctx context.Context, key string) (uint32, error) {
	if key == "" {
		return 0, nil
	}
	var id uint32
	err := l.db.R.QueryRowContext(ctx, `
		SELECT p.file_id FROM comic_pages p JOIN files f ON f.id = p.file_id AND f.deleted_at IS NULL
		WHERE p.site_key = ? LIMIT 1`, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	return id, nil
}

// SetComicPageKey keeps a stored page's key on its site.
func (l *Library) SetComicPageKey(ctx context.Context, comic int64, page int, key string) error {
	_, err := l.db.W.ExecContext(ctx, "UPDATE comic_pages SET site_key = ? WHERE comic_id = ? AND page = ?", nullString(key), comic, page)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}

// LinkComicPage makes a file the library has a comic's page (0-based), with its key on its site - nothing to fetch.
func (l *Library) LinkComicPage(ctx context.Context, comic int64, page int, file uint32, key string) error {
	if page < 0 {
		return fmt.Errorf("library: %w: page %d", ErrInvalid, page)
	}
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if err := comicExists(ctx, tx, comic); err != nil {
			return nil, err
		}
		return nil, placePage(ctx, tx, comic, page, file, key, l.now().UnixMilli())
	})
}

// placePage stores a file as a comic's page (replacing the one there) and updates the comic: its page count, its
// cover (the first page), when it changed.
func placePage(ctx context.Context, tx *sql.Tx, comic int64, page int, file uint32, key string, now int64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO comic_pages (comic_id, page, file_id, site_key) VALUES (?, ?, ?, ?)
		ON CONFLICT (comic_id, page) DO UPDATE SET file_id = excluded.file_id, site_key = excluded.site_key`,
		comic, page, file, nullString(key)); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE comics SET updated_at = ?, pages = max(pages, ? + 1),
			cover = CASE WHEN ? = 0 OR cover IS NULL THEN ? ELSE cover END WHERE id = ?`,
		now, page, page, file, comic); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}
