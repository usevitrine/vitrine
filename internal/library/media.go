package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
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

// (the condition word for word as in the files_pending_media index - migration 0046 - so only waiting files are read)
const pendingMediaSQL = "SELECT id FROM files WHERE deleted_at IS NULL AND flags & 3 != 3"

func (l *Library) pendingMedia(ctx context.Context, retryFailed bool, limit int) ([]uint32, error) {
	q := pendingMediaSQL
	if !retryFailed {
		q += fmt.Sprintf(" AND flags & %d = 0", FlagMediaError)
	}
	q += " ORDER BY id DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	return db.Column[uint32](ctx, l.db.R, q)
}

// ---- Videos in the library from before their pixels' shape counted (media.displayWidth): read once more, in the
// background, one at a time; one whose shape is now another goes back to the media worker (shape and thumbnail) and gets
// new preview frames. Picks up where it was after a restart (meta video_shapes: the last ID read; -1: done).

const videoShapesPause = 25 * time.Millisecond

// RunVideoShapes goes through the library's videos until done or ctx ends; done says how many changed.
func (l *Library) RunVideoShapes(ctx context.Context, done func(read, changed int)) error {
	read, changed := 0, 0
	for ctx.Err() == nil {
		n, c, more, err := l.videoShapes(ctx, 100)
		read += n
		changed += c
		l.stepRepair(ctx, "video_shapes", int64(n), int64(c))
		if err != nil {
			return err
		}
		if !more {
			if done != nil && read > 0 {
				done(read, changed)
			}
			return nil
		}
	}
	return ctx.Err()
}

// videoShapes reads the next videos (at most limit); more: there may be others.
func (l *Library) videoShapes(ctx context.Context, limit int) (read, changed int, more bool, err error) {
	if l.tools.Check() != nil {
		return 0, 0, false, ErrNoMediaTools
	}
	var last int64
	err = l.db.R.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'video_shapes'").Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, fmt.Errorf("library: %w", err)
	}
	if last < 0 {
		return 0, 0, false, nil
	}
	rows, err := l.db.R.QueryContext(ctx, `SELECT id, sha256, type, width, height FROM files
		WHERE id > ? AND type BETWEEN 6 AND 9 AND deleted_at IS NULL AND flags & ? != 0 ORDER BY id LIMIT ?`, last, FlagProbed, limit)
	if err != nil {
		return 0, 0, false, fmt.Errorf("library: %w", err)
	}
	type video struct {
		id   uint32
		sha  []byte
		typ  uint8
		w, h int
	}
	var list []video
	for rows.Next() {
		var v video
		if err := rows.Scan(&v.id, &v.sha, &v.typ, &v.w, &v.h); err != nil {
			rows.Close()
			return 0, 0, false, fmt.Errorf("library: %w", err)
		}
		list = append(list, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, false, fmt.Errorf("library: %w", err)
	}
	for _, v := range list {
		var h store.Hash
		copy(h[:], v.sha)
		last = int64(v.id)
		read++
		info, err := l.tools.Probe(ctx, l.store.Path(h, media.Type(v.typ)), media.Type(v.typ))
		if ctx.Err() != nil {
			return read, changed, false, ctx.Err()
		}
		if err == nil && (info.Width != v.w || info.Height != v.h) {
			l.mu.Lock()
			_, err = l.db.W.ExecContext(ctx, "UPDATE files SET flags = flags & ~? WHERE id = ?", FlagThumbnail|FlagSprite|FlagSpriteErr, v.id)
			l.mu.Unlock()
			if err != nil {
				return read, changed, false, fmt.Errorf("library: %w", err)
			}
			changed++
			l.WakeMedia()
		}
		select {
		case <-time.After(videoShapesPause):
		case <-ctx.Done():
			return read, changed, false, ctx.Err()
		}
	}
	more = len(list) == limit
	if !more {
		last = -1
	}
	l.mu.Lock()
	_, err = l.db.W.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES ('video_shapes', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", last)
	l.mu.Unlock()
	if err != nil {
		return read, changed, false, fmt.Errorf("library: %w", err)
	}
	return read, changed, more, nil
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
