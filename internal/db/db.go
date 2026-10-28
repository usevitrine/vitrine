// Package db opens Vitrine's SQLite databases (meta.db, thumbs.db) and keeps their schemas
// up to date.
//
// SQLite is only the durable store; searches run from the in-memory index.
// Writes go through a single connection (W) so they never fight over the write
// lock; reads use a separate pool (R).
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql thumbs/*.sql
var migrationFiles embed.FS

// DB is an open database: meta.db or thumbs.db.
type DB struct {
	// W is the only connection that writes. Its transactions take the write
	// lock up front (BEGIN IMMEDIATE).
	W *sql.DB
	// R is a pool of read-only connections.
	R *sql.DB
}

// pragmas applied to every connection.
var pragmas = []string{
	"busy_timeout(10000)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"foreign_keys(1)",
	"temp_store(MEMORY)",
	"mmap_size(268435456)",
}

// Page cache per connection (KB). thumbs.db's is small: its pictures are read past the cache (blobs straight from the
// file) except the newest, still in the WAL, which every connection copied in up to 64 MB - measured with thumbnails
// written while 8 connections read them: 154 MB of SQLite memory at 64 MB, 44 MB at 16, writes and reads as fast
// (2026-10-28, the user: optimize "for speed and system resources").
const (
	metaCacheKB   = 65536
	thumbsCacheKB = 16384
)

func dsn(path string, cacheKB int, extra ...string) string {
	extra = append(extra, "cache_size(-"+strconv.Itoa(cacheKB)+")")
	q := url.Values{}
	for _, p := range append(pragmas, extra...) {
		q.Add("_pragma", p)
	}
	return path + "?" + q.Encode()
}

// Open opens (creating if needed) meta.db at path and applies any
// pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	return open(ctx, path, "migrations", metaCacheKB)
}

// OpenThumbs opens (creating if needed) thumbs.db, the thumbnail store.
func OpenThumbs(ctx context.Context, path string) (*DB, error) {
	d, err := open(ctx, path, "thumbs", thumbsCacheKB)
	if err != nil {
		return nil, err
	}
	// Its readers stay open between bursts: a screen of the grid asks for dozens of thumbnails at once, and with Go's
	// default of 2 kept, each burst opened (and set up) up to 6 connections again and closed them - measured, 4000
	// thumbnails in bursts of 40: 602 connections, 147 ms; kept, 8 and 69 ms, the same memory (2026-10-28).
	d.R.SetMaxIdleConns(8)
	return d, nil
}

func open(ctx context.Context, path, schema string, cacheKB int) (*DB, error) {
	w, err := sql.Open("sqlite", dsn(path, cacheKB)+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	w.SetMaxOpenConns(1)
	w.SetConnMaxIdleTime(0)

	d := &DB{W: w}
	if err := d.migrate(ctx, schema); err != nil {
		w.Close()
		return nil, err
	}

	r, err := sql.Open("sqlite", dsn(path, cacheKB, "query_only(1)"))
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("db: %w", err)
	}
	r.SetMaxOpenConns(8)
	d.R = r
	return d, nil
}

// Close closes both pools.
func (d *DB) Close() error {
	var errR error
	if d.R != nil {
		errR = d.R.Close()
	}
	if err := d.W.Close(); err != nil {
		return err
	}
	return errR
}

// SchemaVersion is the schema version this build knows.
func SchemaVersion() int {
	ms, err := migrations("migrations")
	if err != nil || len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].version
}

type migration struct {
	version int
	name    string
	sql     string
}

// migrations lists dir/NNNN_name.sql in order.
func migrations(dir string) ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, dir)
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("db: bad migration file name %q", e.Name())
		}
		b, err := migrationFiles.ReadFile(dir + "/" + e.Name())
		if err != nil {
			return nil, err
		}
		ms = append(ms, migration{v, e.Name(), string(b)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i, m := range ms {
		if m.version != i+1 {
			return nil, fmt.Errorf("db: migrations must be numbered 1, 2, 3...; found %s", m.name)
		}
	}
	return ms, nil
}

// migrate brings the schema up to date, one transaction per migration.
func (d *DB) migrate(ctx context.Context, dir string) error {
	ms, err := migrations(dir)
	if err != nil {
		return err
	}
	var current int
	if err := d.W.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	if current > len(ms) {
		return fmt.Errorf("db: database schema version %d is newer than this Vitrine (%d) - update Vitrine",
			current, len(ms))
	}
	for _, m := range ms[current:] {
		tx, err := d.W.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("db: %w", err)
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("db: migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("db: migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("db: migration %s: %w", m.name, err)
		}
	}
	return nil
}

// Compact rewrites the database without its free pages (VACUUM) and empties the write-ahead log: what was deleted is
// gone from the disk too, not just unlinked (a site wiped from the server). Writes wait meanwhile; it needs free
// space about the database's size.
func (d *DB) Compact(ctx context.Context) error {
	if _, err := d.W.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("db: vacuum: %w", err)
	}
	if _, err := d.W.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("db: checkpoint: %w", err)
	}
	return nil
}
