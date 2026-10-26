// Package thumbs stores thumbnails in thumbs.db, one WebP per file ID.
package thumbs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/usevitrine/vitrine/internal/db"
)

// ErrNotFound means a file has no thumbnail (yet).
var ErrNotFound = errors.New("no thumbnail")

// Store is an open thumbs.db.
type Store struct {
	db *db.DB
}

// Open opens or creates thumbs.db at path.
func Open(ctx context.Context, path string) (*Store, error) {
	d, err := db.OpenThumbs(ctx, path)
	if err != nil {
		return nil, err
	}
	return &Store{db: d}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Thumb is a stored thumbnail.
type Thumb struct {
	Version       int
	Width, Height int
	Data          []byte // WebP
}

// Put stores (or replaces) a file's thumbnail.
func (s *Store) Put(ctx context.Context, file uint32, t Thumb) error {
	_, err := s.db.W.ExecContext(ctx, `
		INSERT INTO thumbs (file_id, version, width, height, data) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (file_id) DO UPDATE SET version = excluded.version, width = excluded.width,
			height = excluded.height, data = excluded.data`,
		file, t.Version, t.Width, t.Height, t.Data)
	if err != nil {
		return fmt.Errorf("thumbs: %w", err)
	}
	return nil
}

// Get loads a file's thumbnail.
func (s *Store) Get(ctx context.Context, file uint32) (Thumb, error) {
	var t Thumb
	err := s.db.R.QueryRowContext(ctx, "SELECT version, width, height, data FROM thumbs WHERE file_id = ?", file).
		Scan(&t.Version, &t.Width, &t.Height, &t.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, fmt.Errorf("thumbs: %w", err)
	}
	return t, nil
}

// Delete removes thumbnails and preview frames (after their files are purged) - all in one transaction (emptying a
// trash of thousands: one commit, not two per file).
func (s *Store) Delete(ctx context.Context, files ...uint32) error {
	if len(files) == 0 {
		return nil
	}
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("thumbs: %w", err)
	}
	defer tx.Rollback()
	for _, f := range files {
		if _, err := tx.ExecContext(ctx, "DELETE FROM thumbs WHERE file_id = ?", f); err != nil {
			return fmt.Errorf("thumbs: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM sprites WHERE file_id = ?", f); err != nil {
			return fmt.Errorf("thumbs: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("thumbs: %w", err)
	}
	return nil
}

// Count returns how many thumbnails are stored.
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.R.QueryRowContext(ctx, "SELECT count(*) FROM thumbs").Scan(&n)
	return n, err
}

// Sprite is a video's stored preview frames (see media.Sprite).
type Sprite struct {
	Version      int
	Frames, Cols int
	TileW, TileH int
	IntervalMS   int64
	Data         []byte // WebP; empty when only the layout was asked for
}

// PutSprite stores (or replaces) a video's preview frames.
func (s *Store) PutSprite(ctx context.Context, file uint32, sp Sprite) error {
	_, err := s.db.W.ExecContext(ctx, `
		INSERT INTO sprites (file_id, version, frames, cols, tile_w, tile_h, interval_ms, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (file_id) DO UPDATE SET version = excluded.version, frames = excluded.frames, cols = excluded.cols,
			tile_w = excluded.tile_w, tile_h = excluded.tile_h, interval_ms = excluded.interval_ms, data = excluded.data`,
		file, sp.Version, sp.Frames, sp.Cols, sp.TileW, sp.TileH, sp.IntervalMS, sp.Data)
	if err != nil {
		return fmt.Errorf("thumbs: %w", err)
	}
	return nil
}

// OldSprites lists the files whose preview frames were made by an older recipe.
func (s *Store) OldSprites(ctx context.Context, version int) ([]uint32, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT file_id FROM sprites WHERE version < ?", version)
	if err != nil {
		return nil, fmt.Errorf("thumbs: %w", err)
	}
	defer rows.Close()
	var ids []uint32
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("thumbs: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetSprite loads a video's preview frames (withData = false: only the layout).
func (s *Store) GetSprite(ctx context.Context, file uint32, withData bool) (Sprite, error) {
	var sp Sprite
	data := "x''"
	if withData {
		data = "data"
	}
	err := s.db.R.QueryRowContext(ctx, "SELECT version, frames, cols, tile_w, tile_h, interval_ms, "+data+
		" FROM sprites WHERE file_id = ?", file).
		Scan(&sp.Version, &sp.Frames, &sp.Cols, &sp.TileW, &sp.TileH, &sp.IntervalMS, &sp.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return sp, ErrNotFound
	}
	if err != nil {
		return sp, fmt.Errorf("thumbs: %w", err)
	}
	return sp, nil
}

// Compact rewrites thumbs.db without the space removed thumbnails left (db.Compact).
func (s *Store) Compact(ctx context.Context) error { return s.db.Compact(ctx) }
