package library

import (
	"bytes"
	"context"
	"os"
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

// The saved tags: a stop saves them, the next start reads them (the same searches), and any change to what decides a
// tag's files since - a tag added, a file deleted, whatever program does it - or a damaged file, or another schema,
// means reading the database again. A save that differs from the database (the index and the database apart) is found
// by the check after the start and thrown away.
func TestIndexSave(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	save := filepath.Join(dir, "index-tags.bin")
	d, err := db.Open(ctx, filepath.Join(dir, "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, err := store.Open(filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	th, err := thumbs.Open(ctx, filepath.Join(dir, "thumbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { th.Close() })
	open := func() *Library {
		t.Helper()
		l, err := Open(ctx, Deps{DB: d, Store: s, Thumbs: th, Tools: media.FindTools("", "", ""), IndexSave: save})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	count := func(l *Library, q string) int {
		p, _ := search.Parse(q)
		return l.Index().Search(p, time.Now()).Total
	}
	l := open()
	if l.FromSave() {
		t.Fatal("from a save that doesn't exist")
	}
	var ids []uint32
	for i := range 4 {
		f, err := l.Import(ctx, bytes.NewReader(fakePNG(byte(10+i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
	}
	if err := l.AddTags(ctx, ids[:3], []tag.Tag{{Name: "blue_sky"}}); err != nil {
		t.Fatal(err)
	}
	if err := l.AddTags(ctx, ids[1:], []tag.Tag{{Name: "landscape"}}); err != nil {
		t.Fatal(err)
	}
	saveNow := func(l *Library) {
		t.Helper()
		if n, err := l.SaveIndex(ctx, save); err != nil || n == 0 {
			t.Fatalf("save: %d bytes, %v", n, err)
		}
	}
	checked := func(l *Library) int {
		t.Helper()
		done := make(chan int, 1)
		l.StartIndexCheck(ctx, save, func(bad int, _ time.Duration, err error) {
			if err != nil {
				t.Error(err)
			}
			done <- bad
		})
		select {
		case bad := <-done:
			return bad
		case <-time.After(10 * time.Second):
			t.Fatal("no check")
			return -1
		}
	}

	// Saved, opened again: from the save, the same searches, and the check agrees.
	saveNow(l)
	l2 := open()
	if !l2.FromSave() {
		t.Fatal("not from the save")
	}
	for _, q := range []string{"blue_sky", "landscape", "blue_sky landscape", "-blue_sky", "system:tags=2"} {
		if a, b := count(l, q), count(l2, q); a != b {
			t.Fatalf("%q: %d before, %d from the save", q, a, b)
		}
	}
	if bad := checked(l2); bad != 0 {
		t.Fatalf("the check found %d tags differing", bad)
	}

	// A tag added since: the save isn't used.
	if err := l2.AddTags(ctx, ids[3:], []tag.Tag{{Name: "blue_sky"}}); err != nil {
		t.Fatal(err)
	}
	if open().FromSave() {
		t.Fatal("from a save older than a tag added")
	}
	// Saved again, then a file deleted: not used.
	saveNow(l2)
	if err := l2.Delete(ctx, ids[:1], ""); err != nil {
		t.Fatal(err)
	}
	if open().FromSave() {
		t.Fatal("from a save older than a deletion")
	}
	// A link written by another program (no library): its trigger takes the token away too.
	saveNow(l2)
	if _, err := d.W.Exec("DELETE FROM file_tags WHERE file_id = ?", ids[2]); err != nil {
		t.Fatal(err)
	}
	if open().FromSave() {
		t.Fatal("from a save older than a link removed outside the library")
	}

	// Damaged: not used.
	l3 := open()
	saveNow(l3)
	data, _ := os.ReadFile(save)
	data[len(data)/2] ^= 0xff
	os.WriteFile(save, data, 0o644)
	if open().FromSave() {
		t.Fatal("from a damaged save")
	}
	// Another schema: not used.
	saveNow(l3)
	d.W.Exec("UPDATE meta SET value = 'abc:1' WHERE key = 'index_save'")
	if open().FromSave() {
		t.Fatal("from a save made under another schema")
	}

	// The index and the database apart (a link the index never heard of), the token put back by hand: the start uses
	// the save, and the check finds the difference and throws the save away.
	saveNow(l3)
	var tok string
	d.R.QueryRow("SELECT value FROM meta WHERE key = 'index_save'").Scan(&tok)
	// (a live file whose links were removed above)
	if _, err := d.W.Exec("INSERT INTO file_tags (file_id, tag_id, origin) SELECT ?, id, 0 FROM tags WHERE name = 'landscape'", ids[2]); err != nil {
		t.Fatal(err)
	}
	d.W.Exec("INSERT INTO meta (key, value) VALUES ('index_save', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", tok)
	l4 := open()
	if !l4.FromSave() {
		t.Fatal("not from the save")
	}
	if bad := checked(l4); bad == 0 {
		t.Fatal("the check missed a tag the database has more files for")
	}
	if _, err := os.Stat(save); !os.IsNotExist(err) {
		t.Fatal("the save wasn't thrown away")
	}
	if open().FromSave() {
		t.Fatal("from a save the check threw away")
	}
}
