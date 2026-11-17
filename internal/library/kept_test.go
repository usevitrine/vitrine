package library

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/tag"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

// A file kept despite the blacklist: the clean-up neither counts nor deletes it, system:kept finds it - also after a
// restart - and taking the keep off puts it back with the others.
func TestKeptDespiteBlacklist(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, _ := store.Open(filepath.Join(dir, "files"))
	th, _ := thumbs.Open(ctx, filepath.Join(dir, "thumbs.db"))
	t.Cleanup(func() { th.Close() })
	open := func() *Library {
		t.Helper()
		l, err := Open(ctx, Deps{DB: d, Store: s, Thumbs: th, Tools: media.FindTools("", "", "")})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	count := func(l *Library, q string) int {
		t.Helper()
		p, err := search.Parse(q)
		if err != nil {
			t.Fatal(err)
		}
		return l.Index().Search(p, time.Now()).Total
	}
	l := open()
	var ids []uint32
	for i := range 3 {
		f, err := l.Import(ctx, bytes.NewReader(fakePNG(byte(90+i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
	}
	if err := l.AddTags(ctx, ids[:2], []tag.Tag{{Name: "furry"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.SetBlacklist(ctx, []string{"furry"}); err != nil {
		t.Fatal(err)
	}
	if n, err := l.SetKept(ctx, ids[:1], true); err != nil || n != 1 {
		t.Fatalf("SetKept = %d, %v", n, err)
	}
	if n, _ := l.SetKept(ctx, ids[:1], true); n != 0 {
		t.Fatalf("kept again: %d changed, want 0", n)
	}
	m, err := l.BlacklistedFiles(ctx, nil)
	if err != nil || m.Total != 1 || m.Kept != 1 || m.Counts[0].Files != 1 {
		t.Fatalf("matches = %+v, %v; want 1 to delete, 1 kept", m, err)
	}
	if !l.IsKept(ids[0]) || l.IsKept(ids[1]) {
		t.Fatal("IsKept wrong")
	}
	if f, _ := l.Files(ctx, ids[:1]); !f[0].Kept {
		t.Fatal("the file's row doesn't say kept")
	}

	l2 := open() // (read again from the database)
	if a, b := count(l2, "system:kept"), count(l2, "furry -system:kept"); a != 1 || b != 1 {
		t.Fatalf("after a restart: system:kept %d, furry -system:kept %d; want 1, 1", a, b)
	}
	deleted, err := l2.DeleteBlacklisted(ctx, nil)
	if err != nil || len(deleted) != 1 || deleted[0] != ids[1] {
		t.Fatalf("deleted %v (%v), want only %d", deleted, err, ids[1])
	}

	if n, err := l2.SetKept(ctx, ids[:1], false); err != nil || n != 1 {
		t.Fatalf("SetKept off = %d, %v", n, err)
	}
	if m, _ := l2.BlacklistedFiles(ctx, nil); m.Total != 1 || m.Kept != 0 {
		t.Fatalf("after the keep is taken off: %+v", m)
	}
	if q, _ := search.Parse("system:kept"); q.String() != "system:kept" {
		t.Fatalf("String() = %q", q.String())
	}
}
