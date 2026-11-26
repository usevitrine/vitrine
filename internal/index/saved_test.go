package index_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/search"
)

// The saved tags give the index a full load gives - every tag's files (hidden site tags left out, a deleted file
// left out), the parents found through them, the files' tag counts - and CheckTags finds a database that moved on.
func TestSavedTags(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	const nTags = 600
	fillDB(t, d, 300, nTags)
	full, err := index.Load(ctx, d.R)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := full.SaveTags(&buf, "tok:1"); err != nil {
		t.Fatal(err)
	}
	if tok, err := index.SavedToken(bytes.NewReader(buf.Bytes())); err != nil || tok != "tok:1" {
		t.Fatalf("token %q, %v", tok, err)
	}
	saved, err := index.LoadSaved(ctx, d.R, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if full.Stats() != saved.Stats() {
		t.Fatalf("stats: full %+v, saved %+v", full.Stats(), saved.Stats())
	}
	count := func(ix *index.Index, q string) int {
		p, err := search.Parse(q)
		if err != nil {
			t.Fatal(err)
		}
		return ix.Search(p, time.Now()).Total
	}
	queries := []string{"group", "system:tags>60", "system:tags<30", "-tag_1"}
	for i := 1; i <= nTags; i++ {
		queries = append(queries, "tag_"+strconv.Itoa(i))
	}
	for _, q := range queries {
		if a, b := count(full, q), count(saved, q); a != b {
			t.Fatalf("%q: full load %d files, saved %d", q, a, b)
		}
	}

	// The database as it was: no tag differs. A link added since: CheckTags finds it.
	sums := saved.SavedSums()
	if len(sums) == 0 {
		t.Fatal("no checksums from the save")
	}
	check := func() int {
		tx, err := d.R.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		bad, err := index.CheckTags(ctx, tx, saved.Alive(), sums)
		if err != nil {
			t.Fatal(err)
		}
		return bad
	}
	if bad := check(); bad != 0 {
		t.Fatalf("%d tags differ from the database it was saved from", bad)
	}
	if _, err := d.W.Exec("INSERT OR IGNORE INTO file_tags (file_id, tag_id, origin) VALUES (2, ?, 1)", nTags-1); err != nil {
		t.Fatal(err)
	}
	if bad := check(); bad != 1 {
		t.Fatalf("%d tags differ after a link was added, want 1", bad)
	}

	// Damaged: refused.
	broken := bytes.Clone(buf.Bytes())
	broken[len(broken)/2] ^= 0xff
	if _, err := index.LoadSaved(ctx, d.R, broken); !errors.Is(err, index.ErrSavedTags) {
		t.Fatalf("a damaged save: %v", err)
	}
}
