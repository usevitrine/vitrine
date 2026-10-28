package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
)

// newLibrary makes a meta.db with one setting in it.
func newLibrary(t *testing.T, dir, value string) string {
	t.Helper()
	path := filepath.Join(dir, "meta.db")
	d, err := db.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.W.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('probe', ?, 0)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, value); err != nil {
		t.Fatal(err)
	}
	return path
}

func probe(t *testing.T, path string) string {
	t.Helper()
	d, err := db.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var v string
	if err := d.R.QueryRow("SELECT value FROM settings WHERE key = 'probe'").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestCreateListKeepRestore(t *testing.T) {
	dir := t.TempDir()
	path := newLibrary(t, dir, `"first"`)
	m := New(path, filepath.Join(dir, "backups"))
	clock := time.Date(2026, 10, 16, 3, 0, 0, 0, time.Local)
	m.now = func() time.Time { return clock }

	var made []Info
	for i := 0; i < 4; i++ {
		b, err := m.Create(context.Background(), 3)
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, b)
		clock = clock.Add(24 * time.Hour)
	}
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Name != made[3].Name || list[2].Name != made[1].Name {
		t.Fatalf("kept %+v", list)
	}
	if list[0].Size == 0 || list[0].CreatedAt != made[3].CreatedAt {
		t.Fatalf("info %+v", list[0])
	}
	if _, err := m.Path("../meta.db"); err != ErrBadName {
		t.Fatalf("a path got through: %v", err)
	}

	// Change the library, then restore the backup: staged now, in place on the next start.
	newLibrary(t, dir, `"second"`)
	if err := m.StageBackup(list[0].Name); err != nil {
		t.Fatal(err)
	}
	if !m.Pending() {
		t.Fatal("not pending")
	}
	if probe(t, path) != `"second"` {
		t.Fatal("restored before the restart")
	}
	did, err := ApplyPending(path)
	if err != nil || !did {
		t.Fatalf("apply: %v %v", did, err)
	}
	if v := probe(t, path); v != `"first"` {
		t.Fatalf("after restore: %s", v)
	}
	if v := probe(t, path+".before-restore"); v != `"second"` {
		t.Fatalf("the replaced database: %s", v)
	}
	if did, _ := ApplyPending(path); did {
		t.Fatal("applied twice")
	}
}

func TestStageRejects(t *testing.T) {
	dir := t.TempDir()
	path := newLibrary(t, dir, `"x"`)
	m := New(path, filepath.Join(dir, "backups"))
	if err := m.Stage(strings.NewReader("not a database at all")); err == nil {
		t.Fatal("garbage accepted")
	}
	// A database from a newer Vitrine.
	other := filepath.Join(t.TempDir(), "meta.db")
	newLibrary(t, filepath.Dir(other), `"y"`)
	d, err := db.Open(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	d.W.Exec("PRAGMA user_version = 9999")
	d.Close()
	f, err := os.Open(other)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := m.Stage(f); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("newer schema: %v", err)
	}
	if m.Pending() {
		t.Fatal("a rejected file was staged")
	}
	// A plain (not gzipped) meta.db is fine.
	g, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if err := m.Stage(g); err != nil {
		t.Fatalf("plain meta.db: %v", err)
	}
	if err := m.CancelRestore(); err != nil || m.Pending() {
		t.Fatalf("cancel: %v", err)
	}
}

// Each backup is read back as a restore would read it: a sound one passes, one cut short (a full disk, a crash while
// writing) doesn't - and leaves no unpacked copy behind.
func TestVerify(t *testing.T) {
	dir := t.TempDir()
	m := New(newLibrary(t, dir, `"first"`), filepath.Join(dir, "backups"))
	b, err := m.Create(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(m.Dir(), b.Name)
	if err := verify(good); err != nil {
		t.Fatalf("a sound backup: %v", err)
	}
	data, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(m.Dir(), "vitrine-20261016-030000.db.gz")
	if err := os.WriteFile(bad, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verify(bad); err == nil {
		t.Fatal("a backup cut short passed")
	}
	if left, _ := filepath.Glob(filepath.Join(m.Dir(), ".verify-*")); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}
