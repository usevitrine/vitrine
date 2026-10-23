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
	"cache_size(-65536)", // 64 MB
}

func dsn(path string, extra ...string) string {
	q := url.Values{}
	for _, p := range append(pragmas, extra...) {
		q.Add("_pragma", p)
	}
	return path + "?" + q.Encode()
}

// Open opens (creating if needed) meta.db at path and applies any
// pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	return open(ctx, path, "migrations")
}

// OpenThumbs opens (creating if needed) thumbs.db, the thumbnail store.
func OpenThumbs(ctx context.Context, path string) (*DB, error) {
	return open(ctx, path, "thumbs")
}

func open(ctx context.Context, path, schema string) (*DB, error) {
	w, err := sql.Open("sqlite", dsn(path)+"&_txlock=immediate")
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

	r, err := sql.Open("sqlite", dsn(path, "query_only(1)"))
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
