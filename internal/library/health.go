package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
)

// The library's health check (2026-10-28): every week the database is checked (PRAGMA quick_check) and every original
// looked for; every day a share of the originals is read back and hashed against its SHA-256, a whole pass about every
// four weeks, slowly (the disk is also serving and downloading). A failing disk or a damaged file is then found while
// the backups still hold a good copy.

// Health is what the checks found (kept in meta as "health").
type Health struct {
	DBCheckedAt    int64    `json:"db_checked_at"` // the last database check (Unix ms; 0: never)
	DBProblem      string   `json:"db_problem,omitempty"`
	FilesCheckedAt int64    `json:"files_checked_at"`  // the last look for every original
	Missing        []uint32 `json:"missing,omitempty"` // files whose original is gone (the first healthListMax)
	MissingCount   int      `json:"missing_count"`
	Damaged        []uint32 `json:"damaged,omitempty"` // originals that no longer match their SHA-256 (ditto)
	DamagedCount   int      `json:"damaged_count"`
	// The read-back: the last day's, the next file ID, when this pass began, when the last whole pass ended, files
	// read in this one.
	ReadAt        int64  `json:"read_at"`
	Cursor        uint32 `json:"cursor"`
	PassStartedAt int64  `json:"pass_started_at"`
	LastPassAt    int64  `json:"last_pass_at"`
	Verified      int64  `json:"verified"`
}

const healthListMax = 1000

// Health returns what the checks found so far.
func (l *Library) Health(ctx context.Context) (Health, error) {
	var h Health
	var raw string
	err := l.db.R.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'health'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	return h, json.Unmarshal([]byte(raw), &h)
}

func (l *Library) saveHealth(ctx context.Context, h Health) error {
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	_, err = l.db.W.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES ('health', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", string(b))
	return err
}

// CheckDatabase runs SQLite's quick check on meta.db ("" when sound).
func (l *Library) CheckDatabase(ctx context.Context) (string, error) {
	rows, err := l.db.R.QueryContext(ctx, "PRAGMA quick_check(20)")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var problem string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", err
		}
		if s != "ok" {
			if problem != "" {
				problem += "; "
			}
			problem += s
		}
	}
	return problem, rows.Err()
}

// storedFile is a file as the health check reads it.
type storedFile struct {
	id   uint32
	sha  store.Hash
	typ  media.Type
	size int64
}

// filesFrom reads up to n files (in the library or comics, not deleted) from ID from on.
func (l *Library) filesFrom(ctx context.Context, from uint32, n int) ([]storedFile, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT id, sha256, type, size FROM files WHERE id >= ? AND deleted_at IS NULL ORDER BY id LIMIT ?", from, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedFile
	for rows.Next() {
		var f storedFile
		var sha []byte
		if err := rows.Scan(&f.id, &sha, &f.typ, &f.size); err != nil {
			return nil, err
		}
		copy(f.sha[:], sha)
		out = append(out, f)
	}
	return out, rows.Err()
}

// FindMissing looks for every file's original; it returns the files whose original is gone.
func (l *Library) FindMissing(ctx context.Context) ([]uint32, error) {
	var missing []uint32
	for from := uint32(0); ; {
		batch, err := l.filesFrom(ctx, from, 2000)
		if err != nil {
			return missing, err
		}
		for _, f := range batch {
			if _, err := os.Stat(l.store.Path(f.sha, f.typ)); errors.Is(err, os.ErrNotExist) {
				missing = append(missing, f.id)
			}
		}
		if len(batch) < 2000 {
			return missing, nil
		}
		from = batch[len(batch)-1].id + 1
	}
}

// errDamaged: an original that no longer matches its SHA-256.
var errDamaged = errors.New("the original no longer matches its checksum")

// verifyOriginal reads a file's original back at no more than rate bytes a second (0: as fast as it goes) and checks
// its SHA-256.
func (l *Library) verifyOriginal(ctx context.Context, f storedFile, rate int64) error {
	fh, err := os.Open(l.store.Path(f.sha, f.typ))
	if err != nil {
		return err
	}
	defer fh.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	start, read := time.Now(), int64(0)
	for {
		n, err := fh.Read(buf)
		h.Write(buf[:n])
		read += int64(n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if rate > 0 { // (slowly: the same disk serves the library and takes downloads)
			if ahead := time.Duration(float64(read)/float64(rate)*float64(time.Second)) - time.Since(start); ahead > 0 {
				select {
				case <-time.After(ahead):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
	if store.Hash(h.Sum(nil)) != f.sha {
		return errDamaged
	}
	return nil
}

// HealthFinding is one file a check found a problem with.
type HealthFinding struct {
	ID      uint32
	Path    string
	Missing bool // gone (else: damaged)
}

// HealthCheck runs what's due: the database and the look for every original once a week, and once a day reading back
// up to budget bytes of originals (from where the last day stopped) at rate bytes a second. It reports each file found
// newly missing or damaged to found, and returns what's known now. A file found fine again (put back) leaves the
// lists at the next weekly look.
func (l *Library) HealthCheck(ctx context.Context, budget, rate int64, found func(HealthFinding)) (Health, error) {
	h, err := l.Health(ctx)
	if err != nil {
		return h, err
	}
	now := time.Now()
	week := 7 * 24 * time.Hour
	if now.Sub(time.UnixMilli(h.DBCheckedAt)) >= week {
		problem, err := l.CheckDatabase(ctx)
		if err != nil {
			return h, fmt.Errorf("checking the database: %w", err)
		}
		h.DBProblem, h.DBCheckedAt = problem, now.UnixMilli()
		if err := l.saveHealth(ctx, h); err != nil {
			return h, err
		}
	}
	known := map[uint32]bool{}
	for _, id := range append(append([]uint32{}, h.Missing...), h.Damaged...) {
		known[id] = true
	}
	if now.Sub(time.UnixMilli(h.FilesCheckedAt)) >= week {
		missing, err := l.FindMissing(ctx)
		if err != nil {
			return h, fmt.Errorf("looking for the originals: %w", err)
		}
		for _, id := range missing {
			if !known[id] {
				found(HealthFinding{ID: id, Path: l.pathOf(ctx, id), Missing: true})
			}
		}
		h.MissingCount, h.Missing = len(missing), missing[:min(len(missing), healthListMax)]
		h.FilesCheckedAt = now.UnixMilli()
		if err := l.saveHealth(ctx, h); err != nil {
			return h, err
		}
	}
	if budget <= 0 || now.Sub(time.UnixMilli(h.ReadAt)) < 23*time.Hour {
		return h, nil
	}
	h.ReadAt = now.UnixMilli()
	if h.Cursor == 0 {
		h.PassStartedAt, h.Verified = now.UnixMilli(), 0
		h.Damaged, h.DamagedCount = nil, 0 // (a new pass finds them again)
	}
	defer func() { l.saveHealth(context.WithoutCancel(ctx), h) }()
	for spent := int64(0); spent < budget; {
		batch, err := l.filesFrom(ctx, h.Cursor, 100)
		if err != nil {
			return h, err
		}
		if len(batch) == 0 { // a whole pass done
			h.Cursor, h.LastPassAt = 0, time.Now().UnixMilli()
			return h, nil
		}
		for _, f := range batch {
			if spent >= budget {
				break
			}
			err := l.verifyOriginal(ctx, f, rate)
			switch {
			case ctx.Err() != nil:
				return h, ctx.Err()
			case errors.Is(err, errDamaged):
				h.DamagedCount++
				if len(h.Damaged) < healthListMax {
					h.Damaged = append(h.Damaged, f.id)
				}
				if !known[f.id] {
					found(HealthFinding{ID: f.id, Path: l.store.Path(f.sha, f.typ)})
				}
			case err != nil && !errors.Is(err, os.ErrNotExist): // (missing: the weekly look reports it)
				return h, err
			}
			spent += f.size
			h.Verified++
			h.Cursor = f.id + 1
		}
	}
	return h, nil
}

// pathOf is a file's original's path ("" if the file isn't known).
func (l *Library) pathOf(ctx context.Context, id uint32) string {
	batch, err := l.filesFrom(ctx, id, 1)
	if err != nil || len(batch) == 0 || batch[0].id != id {
		return ""
	}
	return l.store.Path(batch[0].sha, batch[0].typ)
}
