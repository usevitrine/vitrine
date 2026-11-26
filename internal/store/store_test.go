package store

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/usevitrine/vitrine/internal/media"
)

// fakeJPEG is a JPEG signature followed by n filler bytes - enough for the store,
// which only sniffs the header.
func fakeJPEG(n int, fill byte) []byte {
	b := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{fill}, n)...)
	return b
}

func tmpEntries(t *testing.T, s *Store) int {
	t.Helper()
	e, err := os.ReadDir(filepath.Join(s.Root(), tmpDir))
	if err != nil {
		t.Fatal(err)
	}
	return len(e)
}

func TestPut(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := fakeJPEG(100_000, 7)

	res, err := s.Put(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if res.SHA256 != sha256.Sum256(data) || res.MD5 != md5.Sum(data) {
		t.Fatal("hash mismatch")
	}
	if res.Size != int64(len(data)) || res.Type != media.JPEG || res.Existed {
		t.Fatalf("unexpected result %+v", res)
	}

	p := s.Path(res.SHA256, res.Type)
	if filepath.Base(filepath.Dir(p)) != res.SHA256.String()[:2] || filepath.Ext(p) != ".jpg" {
		t.Fatalf("unexpected path %s", p)
	}
	got, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("stored content differs (err %v)", err)
	}
	if !s.Has(res.SHA256, res.Type) {
		t.Fatal("Has = false")
	}
	if n := tmpEntries(t, s); n != 0 {
		t.Fatalf("%d temp files left", n)
	}

	// Same content again: reported as existing, no temp left behind.
	res2, err := s.Put(bytes.NewReader(data))
	if err != nil || !res2.Existed || res2.SHA256 != res.SHA256 {
		t.Fatalf("second put: %+v, %v", res2, err)
	}
	if n := tmpEntries(t, s); n != 0 {
		t.Fatalf("%d temp files left after duplicate", n)
	}

	if err := s.Remove(res.SHA256, res.Type); err != nil || s.Has(res.SHA256, res.Type) {
		t.Fatalf("remove failed: %v", err)
	}
	if err := s.Remove(res.SHA256, res.Type); err != nil {
		t.Fatalf("removing a missing file: %v", err)
	}
}

func TestPutSmallFile(t *testing.T) {
	s, _ := Open(t.TempDir())
	data := fakeJPEG(3, 1) // shorter than SniffLen
	res, err := s.Put(bytes.NewReader(data))
	if err != nil || res.Size != int64(len(data)) {
		t.Fatalf("small file: %+v, %v", res, err)
	}
}

func TestPutUnsupported(t *testing.T) {
	s, _ := Open(t.TempDir())
	_, err := s.Put(bytes.NewReader([]byte("just some text, not media")))
	if !errors.Is(err, media.ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
	if n := tmpEntries(t, s); n != 0 {
		t.Fatalf("%d temp files left", n)
	}
}

type failingReader struct {
	r     io.Reader
	after int
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.after <= 0 {
		return 0, errors.New("connection reset")
	}
	p = p[:min(len(p), f.after)]
	n, err := f.r.Read(p)
	f.after -= n
	return n, err
}

func TestPutInterrupted(t *testing.T) {
	s, _ := Open(t.TempDir())
	data := fakeJPEG(1<<20, 3)
	_, err := s.Put(&failingReader{r: bytes.NewReader(data), after: 500_000})
	if err == nil {
		t.Fatal("expected error")
	}
	if s.Has(sha256.Sum256(data), media.JPEG) {
		t.Fatal("partial file stored under its real name")
	}
	if n := tmpEntries(t, s); n != 0 {
		t.Fatalf("%d temp files left", n)
	}
}

func TestCleanTemp(t *testing.T) {
	s, _ := Open(t.TempDir())
	if err := os.WriteFile(filepath.Join(s.Root(), tmpDir, "put-123"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanTemp(); err != nil {
		t.Fatal(err)
	}
	if n := tmpEntries(t, s); n != 0 {
		t.Fatalf("%d temp files left", n)
	}
}

func TestParseHash(t *testing.T) {
	h := Hash(sha256.Sum256([]byte("x")))
	got, err := ParseHash(h.String())
	if err != nil || got != h {
		t.Fatalf("round trip failed: %v", err)
	}
	for _, bad := range []string{"", "abc", h.String()[:63] + "g"} {
		if _, err := ParseHash(bad); err == nil {
			t.Errorf("ParseHash(%q) accepted", bad)
		}
	}
}

func BenchmarkPut(b *testing.B) {
	s, _ := Open(b.TempDir())
	data := fakeJPEG(8<<20, 0)
	b.SetBytes(int64(len(data)))
	for i := 0; b.Loop(); i++ {
		data[100] = byte(i) // a new file each time
		data[101] = byte(i >> 8)
		if _, err := s.Put(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}
