// Package store keeps original files, addressed by their SHA-256.
//
// A file lives at <root>/<first 2 hex digits>/<sha256 hex>.<ext> and is never
// modified. New files are written to <root>/tmp, synced to disk and then
// renamed into place, so a file under its real name is always complete.
package store

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/usevitrine/vitrine/internal/media"
)

// Hash is a file's SHA-256.
type Hash [sha256.Size]byte

func (h Hash) String() string { return hex.EncodeToString(h[:]) }

// ParseHash reads a 64-digit hex SHA-256.
func ParseHash(s string) (Hash, error) {
	var h Hash
	if len(s) != 2*len(h) {
		return h, fmt.Errorf("bad sha256 %q", s)
	}
	if _, err := hex.Decode(h[:], []byte(s)); err != nil {
		return h, fmt.Errorf("bad sha256 %q: %w", s, err)
	}
	return h, nil
}

// Store is a directory of original files.
type Store struct {
	root string
}

const tmpDir = "tmp"

// Open uses root as the store, creating it if needed.
func Open(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, tmpDir), 0o755); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return &Store{root: root}, nil
}

// Root is the store's directory.
func (s *Store) Root() string { return s.root }

// Path is where the file with this hash and type lives (or would live).
func (s *Store) Path(h Hash, t media.Type) string {
	hx := h.String()
	return filepath.Join(s.root, hx[:2], hx+"."+t.Ext())
}

// Result describes a file passed to Put.
type Result struct {
	SHA256 Hash
	MD5    [md5.Size]byte
	Size   int64
	Type   media.Type
	// Existed is true if the store already had this file; the new copy was
	// discarded.
	Existed bool
}

// Put copies r into the store, hashing it on the way. The type is detected
// from the content; unsupported files are rejected with media.ErrUnsupported
// and leave nothing behind.
func (s *Store) Put(r io.Reader) (Result, error) {
	return s.PutChecked(r)
}

// MD5MismatchError: the content's md5 (Got) isn't one PutChecked was told to expect - a download that came out wrong.
type MD5MismatchError struct{ Got [16]byte }

func (e *MD5MismatchError) Error() string {
	return fmt.Sprintf("the file's md5 (%x) isn't the one the site gave", e.Got)
}

// PutChecked is Put that keeps the file only when its md5 is one of want (none given: any); otherwise it leaves
// nothing behind and says so with an *MD5MismatchError.
func (s *Store) PutChecked(r io.Reader, want ...[16]byte) (Result, error) {
	if len(want) == 0 {
		return s.PutVerified(r, nil)
	}
	return s.PutVerified(r, func(got [16]byte, _ io.ReaderAt, _ int64) bool { return slices.Contains(want, got) })
}

// PutVerified is Put that keeps the file only when ok (nil: always) accepts it - asked with its md5 and content once
// the whole file is read; otherwise it leaves nothing behind and says so with an *MD5MismatchError.
func (s *Store) PutVerified(r io.Reader, ok func(md5 [16]byte, content io.ReaderAt, size int64) bool) (Result, error) {
	var res Result

	tmp, err := os.CreateTemp(filepath.Join(s.root, tmpDir), "put-*")
	if err != nil {
		return res, fmt.Errorf("store: %w", err)
	}
	tmpName := tmp.Name()
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	head := make([]byte, media.SniffLen)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return res, fmt.Errorf("store: read: %w", err)
	}
	head = head[:n]
	if res.Type, err = media.Detect(head); err != nil {
		return res, err
	}

	sha, md := sha256.New(), md5.New()
	w := io.MultiWriter(tmp, sha, md)
	if _, err := w.Write(head); err != nil {
		return res, fmt.Errorf("store: write: %w", err)
	}
	rest, err := io.Copy(w, r)
	if err != nil {
		return res, fmt.Errorf("store: copy: %w", err)
	}
	res.Size = int64(n) + rest
	sha.Sum(res.SHA256[:0])
	md.Sum(res.MD5[:0])
	if ok != nil && !ok(res.MD5, tmp, res.Size) {
		return res, &MD5MismatchError{Got: res.MD5} // (the deferred cleanup removes the temp file)
	}

	if err := tmp.Sync(); err != nil {
		return res, fmt.Errorf("store: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return res, fmt.Errorf("store: close: %w", err)
	}

	dst := s.Path(res.SHA256, res.Type)
	if _, err := os.Stat(dst); err == nil {
		// Same hash = same content: keep the existing file untouched.
		res.Existed = true
		return res, nil // deferred cleanup removes the temp file
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, fmt.Errorf("store: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return res, fmt.Errorf("store: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return res, fmt.Errorf("store: %w", err)
	}
	done = true
	if err := syncDir(dir); err != nil {
		return res, fmt.Errorf("store: sync dir: %w", err)
	}
	return res, nil
}

// Has reports whether the file is in the store.
func (s *Store) Has(h Hash, t media.Type) bool {
	_, err := os.Stat(s.Path(h, t))
	return err == nil
}

// OpenFile opens a stored file for reading.
func (s *Store) OpenFile(h Hash, t media.Type) (*os.File, error) {
	return os.Open(s.Path(h, t))
}

// Remove deletes a stored file. Removing a missing file is not an error.
func (s *Store) Remove(h Hash, t media.Type) error {
	err := os.Remove(s.Path(h, t))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// CleanTemp deletes leftovers of interrupted writes. Call it at start-up,
// before any Put.
func (s *Store) CleanTemp() error {
	dir := filepath.Join(s.root, tmpDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// syncDir makes a rename durable. Windows can't sync directories (NTFS
// journals renames itself), so it's skipped there.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
