package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func openTest(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meta.db")
	d, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, path
}

func TestOpenMigrates(t *testing.T) {
	d, path := openTest(t)
	ctx := context.Background()

	var v int
	if err := d.R.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != SchemaVersion() || v < 1 {
		t.Fatalf("user_version = %d, want %d", v, SchemaVersion())
	}

	var mode string
	if err := d.R.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}

	// Reopening an up-to-date database is a no-op.
	d.Close()
	d2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	d2.Close()
}

func TestNewerSchemaRefused(t *testing.T) {
	d, path := openTest(t)
	if _, err := d.W.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	d.Close()
	_, err := Open(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected newer-schema error, got %v", err)
	}
}

func TestReadPoolIsReadOnly(t *testing.T) {
	d, _ := openTest(t)
	if _, err := d.R.Exec("INSERT INTO urls (url) VALUES ('x')"); err == nil {
		t.Fatal("write through the read pool succeeded")
	}
}

// Each database's connections, both pools, get its page cache size (and the shared pragmas still apply).
func TestCacheSizes(t *testing.T) {
	ctx := context.Background()
	meta, _ := openTest(t)
	thumbs, err := OpenThumbs(ctx, filepath.Join(t.TempDir(), "thumbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer thumbs.Close()
	for _, c := range []struct {
		name string
		d    *DB
		kb   int
	}{{"meta.db", meta, metaCacheKB}, {"thumbs.db", thumbs, thumbsCacheKB}} {
		for pool, p := range map[string]interface {
			QueryRowContext(context.Context, string, ...any) *sql.Row
		}{"W": c.d.W, "R": c.d.R} {
			var size, fk int
			if err := p.QueryRowContext(ctx, "SELECT (SELECT cache_size FROM pragma_cache_size), (SELECT foreign_keys FROM pragma_foreign_keys)").Scan(&size, &fk); err != nil {
				t.Fatal(err)
			}
			if size != -c.kb || fk != 1 {
				t.Errorf("%s %s: cache_size %d (want %d), foreign_keys %d", c.name, pool, size, -c.kb, fk)
			}
		}
	}
}

func TestConstraints(t *testing.T) {
	d, _ := openTest(t)
	ctx := context.Background()
	exec := func(q string, args ...any) error {
		_, err := d.W.ExecContext(ctx, q, args...)
		return err
	}

	// Foreign keys are on: a tag link to a missing file fails.
	if err := exec("INSERT INTO tags (id, name) VALUES (1, 'a'), (2, 'b')"); err != nil {
		t.Fatal(err)
	}
	if err := exec("INSERT INTO file_tags (file_id, tag_id, origin) VALUES (42, 1, 0)"); err == nil {
		t.Fatal("foreign key not enforced")
	}
	// A tag can't be its own parent or sibling.
	if err := exec("INSERT INTO tag_parents VALUES (1, 1)"); err == nil {
		t.Fatal("self-parent accepted")
	}
	if err := exec("INSERT INTO tag_siblings VALUES (2, 2)"); err == nil {
		t.Fatal("self-sibling accepted")
	}
	// STRICT tables reject wrongly typed values.
	if err := exec("INSERT INTO tags (name) VALUES (?)", []byte{1, 2}); err == nil {
		t.Fatal("STRICT not enforced")
	}
}

func TestFileIDsNeverReused(t *testing.T) {
	d, _ := openTest(t)
	ins := func(sha byte) int64 {
		res, err := d.W.Exec(`INSERT INTO files (sha256, md5, size, type, imported_at, modified_at)
			VALUES (?, x'00', 1, 1, 0, 0)`, []byte{sha})
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	a := ins(1)
	b := ins(2)
	if _, err := d.W.Exec("DELETE FROM files WHERE id = ?", b); err != nil {
		t.Fatal(err)
	}
	if c := ins(3); a != 1 || b != 2 || c != 3 {
		t.Fatalf("ids %d %d %d, want 1 2 3", a, b, c)
	}
}
