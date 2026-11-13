package db

import (
	"context"
	"database/sql"
	"fmt"
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

// Migration 0055: source links kept with "&amp;" become their decoded form, merged with it where a file has both.
func TestSourceAmpMigration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	setup := `
CREATE TABLE urls (id INTEGER PRIMARY KEY, url TEXT NOT NULL UNIQUE);
CREATE TABLE file_urls (file_id INTEGER NOT NULL, url_id INTEGER NOT NULL, kind INTEGER NOT NULL, site_id INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (file_id, url_id, kind));
INSERT INTO urls (id, url) VALUES (1, 'https://x.example/s?a=1&amp;b=2'), (2, 'https://x.example/s?a=1&b=2'),
  (3, 'https://y.example/t?c=3&amp;d=4'), (4, 'https://post.example/p?id=9');
-- file 1: both forms (the refresh's duplicate); file 2: the coded one only; file 3: the coded one and a post link.
INSERT INTO file_urls VALUES (1, 1, 2, 7), (1, 2, 2, 7), (2, 1, 2, 7), (3, 3, 2, 7), (3, 4, 1, 7);`
	if _, err := db.Exec(setup); err != nil {
		t.Fatal(err)
	}
	mig, err := migrationFiles.ReadFile("migrations/0055_source_amp.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(mig)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT fu.file_id, u.url, fu.kind, fu.site_id FROM file_urls fu JOIN urls u ON u.id = fu.url_id ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var f, k, s int
		var u string
		rows.Scan(&f, &u, &k, &s)
		got = append(got, fmt.Sprintf("%d %s %d %d", f, u, k, s))
	}
	want := []string{
		"1 https://x.example/s?a=1&b=2 2 7",
		"2 https://x.example/s?a=1&b=2 2 7",
		"3 https://post.example/p?id=9 1 7",
		"3 https://y.example/t?c=3&d=4 2 7",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("after 0055:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var coded int
	db.QueryRow(`SELECT count(*) FROM urls WHERE url LIKE '%&amp;%'`).Scan(&coded)
	if coded != 0 {
		t.Fatalf("%d coded links left in urls", coded)
	}
}
