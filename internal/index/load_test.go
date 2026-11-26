package index_test

import (
	"context"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/search"
)

// fillDB writes a synthetic library straight into meta.db: files, Zipf
// tags (~55 per file), a few hidden tags, one parent and one deleted file.
func fillDB(tb testing.TB, d *db.DB, files, nTags int) {
	tb.Helper()
	ctx := context.Background()
	tx, err := d.W.BeginTx(ctx, nil)
	if err != nil {
		tb.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			tb.Fatal(err)
		}
	}
	exec("INSERT INTO sites (id, name) VALUES (1, 'site')")
	exec("INSERT INTO namespaces (id, name) VALUES (1, 'creator')")
	tagStmt, _ := tx.PrepareContext(ctx, "INSERT INTO tags (id, namespace_id, name) VALUES (?, ?, ?)")
	for i := 1; i <= nTags; i++ {
		ns := 0
		if i%20 == 0 {
			ns = 1
		}
		if _, err := tagStmt.ExecContext(ctx, i, ns, "tag_"+strconv.Itoa(i)); err != nil {
			tb.Fatal(err)
		}
	}
	r := rand.New(rand.NewPCG(7, 7))
	zipf := rand.NewZipf(r, 1.1, 8, uint64(nTags-1))
	fileStmt, _ := tx.PrepareContext(ctx, `INSERT INTO files (id, sha256, md5, size, type, imported_at, modified_at, has_audio)
		VALUES (?, ?, x'00', ?, 6, ?, ?, ?)`)
	linkStmt, _ := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO file_tags (file_id, tag_id, origin) VALUES (?, ?, 1)")
	now := time.Now().UnixMilli()
	for id := 1; id <= files; id++ {
		sha := []byte(strconv.Itoa(id))
		if _, err := fileStmt.ExecContext(ctx, id, sha, r.IntN(1<<28), now-int64(files-id)*1000, now, id%2); err != nil {
			tb.Fatal(err)
		}
		for range 20 + r.IntN(70) {
			if _, err := linkStmt.ExecContext(ctx, id, zipf.Uint64()+1); err != nil {
				tb.Fatal(err)
			}
		}
	}
	exec("INSERT INTO hidden_tags (file_id, tag_id) SELECT file_id, tag_id FROM file_tags WHERE tag_id = 3 LIMIT 10")
	exec("INSERT INTO tags (id, name) VALUES (?, 'group')", nTags+1)
	exec("INSERT INTO tag_parents (child_id, parent_id) VALUES (5, ?), (6, ?)", nTags+1, nTags+1)
	exec("UPDATE files SET deleted_at = 1 WHERE id = 1")
	if err := tx.Commit(); err != nil {
		tb.Fatal(err)
	}
}

func TestLoadFromDB(t *testing.T) {
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fillDB(t, d, 500, 2000)
	ix, err := index.Load(context.Background(), d.R)
	if err != nil {
		t.Fatal(err)
	}
	if s := ix.Stats(); s.Files != 499 {
		t.Errorf("Stats.Files = %d, want 499 (one deleted)", s.Files)
	}
	q, _ := search.Parse("group")
	g := ix.Search(q, time.Now()).Total
	q, _ = search.Parse("( tag_5 ~ tag_6 )")
	if or := ix.Search(q, time.Now()).Total; g == 0 || g != or {
		t.Errorf("parent finds %d, children %d", g, or)
	}
}

// BenchmarkLoad measures start-up: building the index from meta.db.
// VITRINE_BENCH_LOAD_FILES sets the size (default 20k; a large library is
// ~90k files / 5M tag links).
func BenchmarkLoad(b *testing.B) {
	files := 20_000
	if v, err := strconv.Atoi(os.Getenv("VITRINE_BENCH_LOAD_FILES")); err == nil && v > 0 {
		files = v
	}
	d, err := db.Open(context.Background(), filepath.Join(b.TempDir(), "meta.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer d.Close()
	start := time.Now()
	fillDB(b, d, files, max(1000, files*85/100))
	b.Logf("filled %d files in %v", files, time.Since(start).Round(time.Millisecond))
	var links int
	d.R.QueryRow("SELECT count(*) FROM file_tags").Scan(&links)
	b.ResetTimer()
	for b.Loop() {
		if _, err := index.Load(context.Background(), d.R); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(links), "links")
}
