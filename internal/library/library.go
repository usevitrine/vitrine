// Package library is the library's write side: it changes meta.db and the
// file store, then updates the in-memory index to match.
//
// Every change runs as one database transaction; the index is updated after
// the commit, under the same lock, so the index never shows something the
// database doesn't have and changes reach it in commit order.
package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

// ErrInvalid marks a request the library refuses (a relation loop, an empty
// tag, ...) - the caller's mistake, not a failure.
var ErrInvalid = errors.New("invalid")

// ErrDeleted means the file was deleted from the library before; deleted
// files are never imported again.
var ErrDeleted = errors.New("file was deleted from the library before")

// Library is the file store, its database and the search index.
type Library struct {
	db     *db.DB
	store  *store.Store
	thumbs *thumbs.Store
	tools  media.Tools
	ix     *index.Index
	now    func() time.Time

	mu sync.Mutex // serializes writes: DB transaction + index update

	mediaWake chan struct{} // new files to probe and thumbnail (see RunMedia)

	// The comics index (see ComicIndex): built again after writes.
	writes  atomic.Int64 // bumped by every write
	comicMu sync.Mutex
	comicIx *index.Index
	comicAt int64 // writes when comicIx was built
	// The trash's indexes (see TrashIndex, ComicsTrashIndex): built again after writes too.
	trashMu      sync.Mutex
	trashIx      *index.Index
	trashAt      int64
	comicTrashIx *index.Index
	comicTrashAt int64
	// Bumped when the blacklist or the download rules change: a running download reads them again (see
	// DownloadListsVersion).
	listsVer atomic.Int64
}

// Deps are what a library is built from.
type Deps struct {
	DB    *db.DB       // meta.db (required)
	Store *store.Store // originals (required)
	// Thumbs and Tools enable media processing (probing, thumbnails); a
	// library without them still imports, tags and searches.
	Thumbs *thumbs.Store
	Tools  media.Tools
}

// Open builds the search index from the database and returns the library.
func Open(ctx context.Context, d Deps) (*Library, error) {
	if err := fixEscapedTags(ctx, d.DB.W); err != nil {
		return nil, err
	}
	if err := fixCaretTags(ctx, d.DB.W); err != nil {
		return nil, err
	}
	ix, err := index.Load(ctx, d.DB.R)
	if err != nil {
		return nil, err
	}
	return &Library{db: d.DB, store: d.Store, thumbs: d.Thumbs, tools: d.Tools, ix: ix, now: time.Now, mediaWake: make(chan struct{}, 1)}, nil
}

// Index is the search index (read-only use: searches, autocomplete).
func (l *Library) Index() *index.Index { return l.ix }

// File is the outcome of an import.
type File struct {
	ID     uint32
	SHA256 store.Hash
	MD5    [16]byte
	Type   media.Type
	Size   int64
	// New is false if the library already had the file.
	New bool
}

// Import adds the content of r. Write order: the original is stored (temp,
// sync, rename) before the database row is committed, so a row always points
// at a complete file.
func (l *Library) Import(ctx context.Context, r io.Reader) (File, error) {
	return l.ImportAs(ctx, r, true)
}

// ImportAs imports a file, saying whether it comes in bulk (a one-off
// download, a subscription's first check, an import) or from a subscription's
// regular check - "new files" can leave bulk ones out.
func (l *Library) ImportAs(ctx context.Context, r io.Reader, bulk bool) (File, error) {
	return l.ImportChecked(ctx, r, bulk)
}

// ImportChecked is ImportAs that keeps the file only when its md5 is one of want (none given: any) - a download
// checked against the md5 its site gave. A mismatch adds nothing and returns a *store.MD5MismatchError.
func (l *Library) ImportChecked(ctx context.Context, r io.Reader, bulk bool, want ...[16]byte) (File, error) {
	if len(want) == 0 {
		return l.ImportVerified(ctx, r, bulk, nil)
	}
	return l.ImportVerified(ctx, r, bulk, func(got [16]byte, _ io.ReaderAt, _ int64) bool { return slices.Contains(want, got) })
}

// ImportVerified is ImportAs that keeps the file only when ok (nil: always) accepts it (its md5 and content), asked
// once it's all read.
// A refusal adds nothing and returns a *store.MD5MismatchError.
func (l *Library) ImportVerified(ctx context.Context, r io.Reader, bulk bool, ok func(md5 [16]byte, content io.ReaderAt, size int64) bool) (File, error) {
	return l.ImportPosted(ctx, r, bulk, 0, 0, ok)
}

// ImportPosted is ImportVerified for a download whose upload time is known (posted, Unix ms; 0 = unknown): a new file
// goes in with it - and with its post (postID: a day-only time's order within that day) - so it's never sorted, not
// for a moment, by its import time: until the site's post was recorded after it, a new download sat at the top of a
// library in upload order, and stayed there in the grid's list until the next arrivals (2026-10-29, the user: "new
// downloads are added to the top of the media page even if they're not newest uploaded").
func (l *Library) ImportPosted(ctx context.Context, r io.Reader, bulk bool, posted, postID int64, ok func(md5 [16]byte, content io.ReaderAt, size int64) bool) (File, error) {
	res, err := l.store.PutVerified(r, ok)
	if err != nil {
		return File{}, err
	}
	f := File{SHA256: res.SHA256, MD5: res.MD5, Type: res.Type, Size: res.Size}

	l.mu.Lock()
	var now int64
	f.ID, f.New, now, err = l.addRow(ctx, res, bulk, posted)
	if errors.Is(err, ErrDeleted) && !res.Existed {
		// Don't keep a copy of a deleted file we just wrote.
		if rmErr := l.store.Remove(res.SHA256, res.Type); rmErr != nil {
			err = errors.Join(err, rmErr)
		}
	}
	if err == nil && f.New {
		in := index.File{ID: f.ID, Size: f.Size, Type: f.Type, ImportedAt: now, ModifiedAt: now, Bulk: bulk, PostedAt: posted}
		if posted > 0 && posted%dayMS == 0 {
			in.PostID = postID // (a day only: its post orders it within the day - as readFile does once the post is recorded)
		}
		err = l.ix.AddFile(in, nil)
	}
	l.mu.Unlock()
	if err == nil && !f.New && l.isComicOnly(ctx, f.ID) {
		err = l.joinLibrary(ctx, f.ID) // (a comic had it: now the library too)
	}
	return f, err
}

// ImportFile imports the file at path (the original is left in place).
func (l *Library) ImportFile(ctx context.Context, path string) (File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return File{}, err
	}
	defer fh.Close()
	return l.Import(ctx, fh)
}

// addRow records a stored file. The tombstone check runs inside the write
// transaction, so it can't race a delete.
func (l *Library) addRow(ctx context.Context, res store.Result, bulk bool, posted int64) (id uint32, isNew bool, now int64, err error) {
	tx, err := l.db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, 0, fmt.Errorf("library: %w", err)
	}
	defer tx.Rollback()

	var one int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM tombstones WHERE sha256 = ?", res.SHA256[:]).Scan(&one)
	switch {
	case err == nil:
		return 0, false, 0, ErrDeleted
	case !errors.Is(err, sql.ErrNoRows):
		return 0, false, 0, fmt.Errorf("library: %w", err)
	}

	now = l.now().UnixMilli()
	err = tx.QueryRowContext(ctx, `
		INSERT INTO files (sha256, md5, size, type, imported_at, modified_at, bulk, posted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, nullif(?, 0))
		ON CONFLICT (sha256) DO NOTHING
		RETURNING id`,
		res.SHA256[:], res.MD5[:], res.Size, int(res.Type), now, now, bulk, posted).Scan(&id)
	switch {
	case err == nil:
		isNew = true
		if err := bumpGeneration(ctx, tx); err != nil {
			return 0, false, 0, err
		}
	case errors.Is(err, sql.ErrNoRows):
		// Already in the library.
		if err := tx.QueryRowContext(ctx,
			"SELECT id FROM files WHERE sha256 = ?", res.SHA256[:]).Scan(&id); err != nil {
			return 0, false, 0, fmt.Errorf("library: %w", err)
		}
	default:
		return 0, false, 0, fmt.Errorf("library: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, false, 0, fmt.Errorf("library: %w", err)
	}
	return id, isNew, now, nil
}

// bumpGeneration marks the library as changed (an index snapshot made
// before this point is stale).
func bumpGeneration(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, "UPDATE meta SET value = value + 1 WHERE key = 'generation'"); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}

// write runs fn in a write transaction and, after a successful commit,
// apply (which updates the index). The library lock is held throughout.
func (l *Library) write(ctx context.Context, fn func(tx *sql.Tx) (apply func() error, err error)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	tx, err := l.db.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	defer tx.Rollback()
	apply, err := fn(tx)
	if err != nil {
		return err
	}
	if err := bumpGeneration(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	l.writes.Add(1)
	if apply == nil {
		return nil
	}
	if err := apply(); err != nil {
		return fmt.Errorf("library: index update: %w", err)
	}
	return nil
}
