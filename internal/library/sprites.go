package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

// Preview frames for the video player's scrub bar (media.Sprite), made in
// the background for every video: new downloads and the existing library.

// PendingSprites lists live, probed videos without preview frames yet,
// newest first (at most limit).
func (l *Library) PendingSprites(ctx context.Context, limit int) ([]uint32, error) {
	rows, err := l.db.R.QueryContext(ctx, `SELECT id FROM files
		WHERE deleted_at IS NULL AND type BETWEEN ? AND ? AND duration_ms > 0 AND flags & ? = ? AND flags & ? = 0
		ORDER BY id DESC LIMIT ?`,
		int(media.MP4), int(media.Matroska), FlagProbed, FlagProbed, FlagSprite|FlagSpriteErr, limit)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var ids []uint32
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RequeueOldSprites marks videos whose preview frames were made by an older
// recipe as needing new ones (the background maker redoes them, newest first;
// the old ones are served until then). Deleted files are left alone (their
// old frames are leftovers). Returns how many.
func (l *Library) RequeueOldSprites(ctx context.Context) (int, error) {
	if l.thumbs == nil {
		return 0, nil
	}
	ids, err := l.thumbs.OldSprites(ctx, media.SpriteVersion)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	tx, err := l.db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	defer tx.Rollback()
	n := 0
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, "UPDATE files SET flags = flags & ~? WHERE id = ? AND deleted_at IS NULL", FlagSprite|FlagSpriteErr, id)
		if err != nil {
			return 0, fmt.Errorf("library: %w", err)
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, tx.Commit()
}

// MakeSprite makes a video's preview frames; what it took comes back (for the log). A failure is remembered, so the
// file isn't tried again on every pass.
func (l *Library) MakeSprite(ctx context.Context, id uint32) (media.Run, error) {
	if l.thumbs == nil || l.tools.Check() != nil {
		return media.Run{}, ErrNoMediaTools
	}
	var sha []byte
	var typ uint8
	var w, h int
	var dur int64
	var codec string
	err := l.db.R.QueryRowContext(ctx, "SELECT sha256, type, width, height, duration_ms, coalesce(codec, '') FROM files WHERE id = ?", id).
		Scan(&sha, &typ, &w, &h, &dur, &codec)
	if errors.Is(err, sql.ErrNoRows) {
		return media.Run{}, fmt.Errorf("library: file %d: %w", id, ErrNoFile)
	}
	if err != nil {
		return media.Run{}, fmt.Errorf("library: %w", err)
	}
	var hash store.Hash
	copy(hash[:], sha)
	path := l.store.Path(hash, media.Type(typ))
	sp, err := l.tools.MakeSprite(ctx, path, media.Info{Width: w, Height: h, DurationMS: dur, Codec: codec})
	flag := FlagSprite
	if err == nil {
		err = l.thumbs.PutSprite(ctx, id, thumbs.Sprite{Version: media.SpriteVersion, Frames: sp.Frames, Cols: sp.Cols,
			TileW: sp.TileW, TileH: sp.TileH, IntervalMS: sp.IntervalMS, Data: sp.Data})
	}
	if err != nil {
		if ctx.Err() != nil {
			return sp.Took, ctx.Err()
		}
		flag = FlagSpriteErr
	}
	l.mu.Lock()
	_, ferr := l.db.W.ExecContext(ctx, "UPDATE files SET flags = flags | ? WHERE id = ?", flag, id)
	l.mu.Unlock()
	if err != nil {
		return sp.Took, fmt.Errorf("file %d (%s): preview frames: %w", id, path, err)
	}
	return sp.Took, ferr
}

// Sprite returns a video's preview frames (withData = false: only the layout).
func (l *Library) Sprite(ctx context.Context, id uint32, withData bool) (thumbs.Sprite, error) {
	if l.thumbs == nil {
		return thumbs.Sprite{}, thumbs.ErrNotFound
	}
	return l.thumbs.GetSprite(ctx, id, withData)
}

// LargeStills lists live still images (not GIFs: they may be animated) whose
// long side is over minSide, newest first.
func (l *Library) LargeStills(ctx context.Context, minSide int) ([]uint32, error) {
	rows, err := l.db.R.QueryContext(ctx, `SELECT id FROM files
		WHERE deleted_at IS NULL AND type BETWEEN ? AND ? AND type != ? AND max(width, height) > ?
		ORDER BY id DESC`, int(media.JPEG), int(media.AVIF), int(media.GIF), minSide)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var ids []uint32
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
