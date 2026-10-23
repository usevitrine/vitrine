package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

// File flags (files.flags). Media processing state lives here so pending
// work survives restarts without a queue table.
const (
	FlagProbed     = 1 << 0 // dimensions, duration, audio known
	FlagThumbnail  = 1 << 1 // thumbnail in thumbs.db, placeholder set
	FlagMediaError = 1 << 2 // probing or thumbnailing failed; not retried automatically
	FlagSprite     = 1 << 3 // a video's preview frames are in thumbs.db
	FlagSpriteErr  = 1 << 4 // making them failed; not retried automatically
	FlagPHash      = 1 << 5 // the perceptual hash was made (or the picture is flat: none)
)

// ErrNoMediaTools means ffmpeg/ffprobe aren't available.
var ErrNoMediaTools = errors.New("media tools (ffmpeg, ffprobe) not available")

// ProcessMedia probes a file (dimensions, duration, audio) and makes its
// thumbnail. On failure the file is flagged so it isn't retried on every
// start.
func (l *Library) ProcessMedia(ctx context.Context, id uint32) error {
	if l.thumbs == nil || l.tools.Check() != nil {
		return ErrNoMediaTools
	}
	var sha []byte
	var typ uint8
	err := l.db.R.QueryRowContext(ctx, "SELECT sha256, type FROM files WHERE id = ?", id).Scan(&sha, &typ)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("library: file %d: %w", id, ErrNoFile)
	}
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	var h store.Hash
	copy(h[:], sha)
	t := media.Type(typ)
	path := l.store.Path(h, t)

	// The slow part runs without the library lock.
	info, thumb, placeholder, err := l.makeMedia(ctx, path, t)
	if err != nil {
		if ctx.Err() == nil {
			l.flagError(ctx, id)
		}
		return fmt.Errorf("file %d (%s): %w", id, path, err)
	}
	tw, th := media.ThumbSize(info.Width, info.Height)
	if err := l.thumbs.Put(ctx, id, thumbs.Thumb{Version: media.ThumbVersion, Width: tw, Height: th, Data: thumb}); err != nil {
		return err
	}

	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		_, err := tx.ExecContext(ctx, `
			UPDATE files SET width = ?, height = ?, duration_ms = ?, frames = ?, has_audio = ?, codec = ?,
				placeholder = ?, flags = (flags | ?) & ~?
			WHERE id = ?`,
			info.Width, info.Height, info.DurationMS, info.Frames, info.HasAudio, info.Codec,
			placeholder, FlagProbed|FlagThumbnail, FlagMediaError, id)
		if err != nil {
			return nil, err
		}
		f, _, deleted, err := readFile(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return func() error {
			if !deleted {
				l.ix.UpdateFile(f)
			}
			return nil
		}, nil
	})
}

func (l *Library) makeMedia(ctx context.Context, path string, t media.Type) (media.Info, []byte, []byte, error) {
	info, err := l.tools.Probe(ctx, path, t)
	if err != nil {
		return info, nil, nil, err
	}
	thumb, err := l.tools.Thumbnail(ctx, path, t, info)
	if err != nil {
		return info, nil, nil, err
	}
	placeholder, err := media.Placeholder(thumb)
	if err != nil {
		return info, nil, nil, err
	}
	return info, thumb, placeholder, nil
}

func (l *Library) flagError(ctx context.Context, id uint32) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.db.W.ExecContext(ctx, "UPDATE files SET flags = flags | ? WHERE id = ?", FlagMediaError, id)
}

// PendingMedia lists live files still waiting for probing or a thumbnail,
// newest first. With retryFailed, files that failed before are included.
func (l *Library) PendingMedia(ctx context.Context, retryFailed bool) ([]uint32, error) {
	return l.pendingMedia(ctx, retryFailed, 0)
}

func (l *Library) pendingMedia(ctx context.Context, retryFailed bool, limit int) ([]uint32, error) {
	q := "SELECT id FROM files WHERE deleted_at IS NULL AND flags & ? != ?"
	if !retryFailed {
		q += fmt.Sprintf(" AND flags & %d = 0", FlagMediaError)
	}
	q += " ORDER BY id DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := l.db.R.QueryContext(ctx, q, FlagProbed|FlagThumbnail, FlagProbed|FlagThumbnail)
	if err != nil {
		return nil, err
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

// ProcessMany runs ProcessMedia over files with several workers (newest
// first, as given). report is called after each file, from one goroutine at a
// time. It stops early if ctx is canceled.
func (l *Library) ProcessMany(ctx context.Context, ids []uint32, workers int, report func(id uint32, err error)) {
	jobs := make(chan uint32)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for range max(1, workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				err := l.ProcessMedia(ctx, id)
				if report != nil {
					mu.Lock()
					report(id, err)
					mu.Unlock()
				}
			}
		}()
	}
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
}

// WakeMedia tells the media worker there's a new file to probe and thumbnail
// (it never waits: downloads and imports go on at their own pace).
func (l *Library) WakeMedia() {
	select {
	case l.mediaWake <- struct{}{}:
	default: // already woken
	}
}

// RunMedia probes and thumbnails files as they come in until ctx ends, with
// several workers: the newest waiting files first, a small batch at a time,
// so new downloads get their thumbnails soon even while older files catch up.
// done is called after each batch (how many files, how many failed); failed
// files are flagged and not tried again here.
func (l *Library) RunMedia(ctx context.Context, workers int, onErr func(id uint32, err error), done func(files, failed int)) {
	const batch = 32
	for ctx.Err() == nil {
		ids, err := l.pendingMedia(ctx, false, batch)
		if err == nil && len(ids) > 0 && l.tools.Check() == nil && l.thumbs != nil {
			failed := 0
			l.ProcessMany(ctx, ids, workers, func(id uint32, err error) {
				if err != nil && ctx.Err() == nil {
					failed++
					if onErr != nil {
						onErr(id, err)
					}
				}
			})
			if done != nil && ctx.Err() == nil {
				done(len(ids), failed)
			}
			continue
		}
		// Nothing waiting (or no media tools): until a new file comes, or a while.
		select {
		case <-l.mediaWake:
		case <-time.After(time.Minute):
		case <-ctx.Done():
			return
		}
	}
}
