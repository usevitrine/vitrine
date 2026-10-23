package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

func newTest(t testing.TB) *Library {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, err := store.Open(filepath.Join(dir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	th, err := thumbs.Open(context.Background(), filepath.Join(dir, "thumbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { th.Close() })
	l, err := Open(context.Background(), Deps{DB: d, Store: s, Thumbs: th, Tools: media.FindTools("", "", "")})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func fakePNG(seed byte) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{seed}, 5000)...)
}

func generation(t *testing.T, l *Library) int {
	t.Helper()
	var g int
	if err := l.db.R.QueryRow("SELECT value FROM meta WHERE key = 'generation'").Scan(&g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestImport(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()

	a, err := l.Import(ctx, bytes.NewReader(fakePNG(1)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.Import(ctx, bytes.NewReader(fakePNG(2)))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 || b.ID != 2 || !a.New || !b.New || a.Type != media.PNG {
		t.Fatalf("unexpected results %+v %+v", a, b)
	}
	if g := generation(t, l); g != 2 {
		t.Fatalf("generation = %d, want 2", g)
	}

	// The same file again keeps its ID and doesn't bump the generation.
	again, err := l.Import(ctx, bytes.NewReader(fakePNG(1)))
	if err != nil || again.ID != a.ID || again.New {
		t.Fatalf("re-import: %+v, %v", again, err)
	}
	if g := generation(t, l); g != 2 {
		t.Fatalf("generation = %d after re-import", g)
	}

	var size, typ int64
	var md5 []byte
	if err := l.db.R.QueryRow("SELECT size, type, md5 FROM files WHERE id = ?", a.ID).Scan(&size, &typ, &md5); err != nil {
		t.Fatal(err)
	}
	if size != a.Size || media.Type(typ) != media.PNG || len(md5) != 16 {
		t.Fatalf("row: size %d type %d md5 %x", size, typ, md5)
	}
}

func TestImportTombstoned(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	data := fakePNG(9)
	sum := sha256.Sum256(data)
	if _, err := l.db.W.Exec("INSERT INTO tombstones (sha256, deleted_at) VALUES (?, 0)", sum[:]); err != nil {
		t.Fatal(err)
	}

	_, err := l.Import(ctx, bytes.NewReader(data))
	if !errors.Is(err, ErrDeleted) {
		t.Fatalf("got %v, want ErrDeleted", err)
	}
	if l.store.Has(sum, media.PNG) {
		t.Fatal("copy of a deleted file kept in the store")
	}
	var n int
	l.db.R.QueryRow("SELECT count(*) FROM files").Scan(&n)
	if n != 0 {
		t.Fatalf("%d file rows, want 0", n)
	}
}

func TestImportUnsupported(t *testing.T) {
	l := newTest(t)
	if _, err := l.Import(context.Background(), bytes.NewReader([]byte("plain text"))); !errors.Is(err, media.ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}

// BenchmarkImport measures a full import of a small file: store (with fsync)
// plus one database transaction.
func BenchmarkImport(b *testing.B) {
	l := newTest(b)
	ctx := context.Background()
	data := fakePNG(0)
	for i := 0; b.Loop(); i++ {
		data[100], data[101], data[102] = byte(i), byte(i>>8), byte(i>>16)
		if _, err := l.Import(ctx, bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBlacklistEntries(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	got, err := l.SetBlacklist(ctx, []string{"Furry", "ai generated", "furry", "", "ai_*"})
	if err != nil || !slices.Equal(got, []string{"ai_*", "ai_generated", "furry"}) {
		t.Fatalf("SetBlacklist = %v, %v", got, err)
	}
	for _, bad := range []string{"-furry", "system:limit=5", "*", "( a"} {
		if _, err := l.SetBlacklist(ctx, []string{bad}); !errors.Is(err, ErrBadPattern) {
			t.Errorf("%q: err = %v, want ErrBadPattern", bad, err)
		}
	}
	b, _ := l.Blacklist(ctx)
	if len(b.Patterns()) != 3 {
		t.Fatalf("a rejected set changed the blacklist: %v", b.Patterns())
	}
	cases := map[string]string{"AI_Art": "ai_*", "furry": "furry", "furry_female": "", "blue_sky": ""}
	for tg, want := range cases {
		if m := b.Match([]string{"solo", tg}); m != want {
			t.Errorf("Match(%q) = %q, want %q", tg, m, want)
		}
	}
	// An entry with a namespace also stops the bare tag (what image boards send); not another namespace's.
	l.SetBlacklist(ctx, []string{"creator:some_artist"})
	b, _ = l.Blacklist(ctx)
	for tg, want := range map[string]string{"some_artist": "creator:some_artist", "creator:some_artist": "creator:some_artist", "series:some_artist": "", "other": ""} {
		if m := b.Match([]string{tg}); m != want {
			t.Errorf("namespaced entry: Match(%q) = %q, want %q", tg, m, want)
		}
	}
}

// Emptying the trash goes in batches (each its own transaction): all of it still goes, and only the trash.
func TestEmptyTrashInBatches(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	defer func(n int) { purgeBatch = n }(purgeBatch)
	purgeBatch = 2
	var ids []uint32
	for i := range 6 {
		f, err := l.Import(ctx, bytes.NewReader(fakePNG(byte(60+i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
	}
	if err := l.Delete(ctx, ids[:5], ""); err != nil { // (the sixth stays)
		t.Fatal(err)
	}
	n, err := l.EmptyTrash(ctx)
	if err != nil || n != 5 {
		t.Fatalf("emptied %d (%v), want 5", n, err)
	}
	var left int
	l.db.R.QueryRowContext(ctx, "SELECT count(*) FROM files").Scan(&left)
	if left != 1 {
		t.Fatalf("%d files left, want 1 (the one not deleted)", left)
	}
	if n, _ := l.EmptyTrash(ctx); n != 0 {
		t.Fatalf("an empty trash emptied %d", n)
	}
}
