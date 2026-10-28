// Package backup makes copies of meta.db - the library's tags, subscriptions,
// settings and deletion records, everything that can't be downloaded again -
// and restores them.
//
// A backup is a consistent snapshot (SQLite's VACUUM INTO, which reads one
// moment of the database while Vitrine keeps running), gzipped, in the backup
// folder: vitrine-YYYYMMDD-HHMMSS.db.gz. thumbs.db, previews and resized
// copies are left out (they're made again from the originals), and so are
// site credentials (secrets.toml).
//
// Restoring can't swap the database under a running library: a backup is
// checked and staged as meta.db.restore, and the next start puts it in place
// (the database it replaces is kept as meta.db.before-restore).
package backup

import (
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	_ "modernc.org/sqlite" // the "sqlite" driver
)

// Info describes one backup.
type Info struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"created_at"` // Unix ms
}

var nameRe = regexp.MustCompile(`^vitrine-(\d{8}-\d{6})\.db\.gz$`)

// ErrBadName means a backup name that isn't one of ours (no paths).
var ErrBadName = errors.New("backup: no such backup")

// Manager makes, lists and restores backups of one meta.db.
type Manager struct {
	dbPath string // the live meta.db
	dir    string // where backups go
	now    func() time.Time
	mu     sync.Mutex // one backup at a time
}

// New returns a manager for the meta.db at dbPath, keeping backups in dir.
func New(dbPath, dir string) *Manager {
	return &Manager{dbPath: dbPath, dir: dir, now: time.Now}
}

// Dir is the backup folder.
func (m *Manager) Dir() string { return m.dir }

// Create makes a backup now and then keeps only the newest keep backups
// (keep <= 0: all).
func (m *Manager) Create(ctx context.Context, keep int) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return Info{}, fmt.Errorf("backup: %w", err)
	}
	t := m.now()
	name := "vitrine-" + t.Format("20060102-150405") + ".db.gz"
	for i := 1; fileExists(filepath.Join(m.dir, name)); i++ { // two in one second
		t = t.Add(time.Second)
		name = "vitrine-" + t.Format("20060102-150405") + ".db.gz"
	}
	snap := filepath.Join(m.dir, ".snapshot-"+name+".db")
	defer os.Remove(snap)

	// A connection of its own: reading a snapshot doesn't hold up writers (WAL).
	conn, err := sql.Open("sqlite", m.dbPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return Info{}, fmt.Errorf("backup: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", snap); err != nil {
		return Info{}, fmt.Errorf("backup: snapshot: %w", err)
	}

	tmp := filepath.Join(m.dir, ".writing-"+name)
	if err := gzipFile(snap, tmp); err != nil {
		os.Remove(tmp)
		return Info{}, fmt.Errorf("backup: %w", err)
	}
	final := filepath.Join(m.dir, name)
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return Info{}, fmt.Errorf("backup: %w", err)
	}
	st, err := os.Stat(final)
	if err != nil {
		return Info{}, fmt.Errorf("backup: %w", err)
	}
	// Read back as a restore would read it (unpacked from the file on disk, checked): a backup that can't be
	// restored is found the day it's made, not the day it's needed - and never pushes good ones out (no pruning).
	if err := verify(final); err != nil {
		return Info{Name: name, Size: st.Size(), CreatedAt: t.UnixMilli()}, fmt.Errorf("backup %s made, but it can't be restored: %w", name, err)
	}
	if keep > 0 {
		if err := m.prune(keep); err != nil {
			return Info{}, err
		}
	}
	return Info{Name: name, Size: st.Size(), CreatedAt: t.UnixMilli()}, nil
}

// List returns the backups, newest first.
func (m *Manager) List() ([]Info, error) {
	entries, err := os.ReadDir(m.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	var out []Info
	for _, e := range entries {
		mt := nameRe.FindStringSubmatch(e.Name())
		if mt == nil || e.IsDir() {
			continue
		}
		t, err := time.ParseInLocation("20060102-150405", mt[1], time.Local)
		if err != nil {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Name: e.Name(), Size: fi.Size(), CreatedAt: t.UnixMilli()})
	}
	slices.SortFunc(out, func(a, b Info) int { return strings.Compare(b.Name, a.Name) })
	return out, nil
}

// Path is where a backup is on disk (only names List returns are accepted).
func (m *Manager) Path(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", ErrBadName
	}
	p := filepath.Join(m.dir, name)
	if !fileExists(p) {
		return "", ErrBadName
	}
	return p, nil
}

// Delete removes a backup.
func (m *Manager) Delete(name string) error {
	p, err := m.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// prune deletes all but the newest keep backups.
func (m *Manager) prune(keep int) error {
	list, err := m.List()
	if err != nil {
		return err
	}
	for _, b := range list[min(keep, len(list)):] {
		if err := os.Remove(filepath.Join(m.dir, b.Name)); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	return nil
}

// ---- restoring

// pendingPath is where a checked backup waits for the next start.
func pendingPath(dbPath string) string { return dbPath + ".restore" }

// Pending reports whether a restore waits for the next start.
func (m *Manager) Pending() bool { return fileExists(pendingPath(m.dbPath)) }

// CancelRestore drops a waiting restore.
func (m *Manager) CancelRestore() error {
	err := os.Remove(pendingPath(m.dbPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// StageBackup checks one of the backups and stages it for the next start.
func (m *Manager) StageBackup(name string) error {
	p, err := m.Path(name)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer f.Close()
	return m.Stage(f)
}

// Stage checks a backup (gzipped, or a plain meta.db) read from r and stages
// it: it must be a Vitrine library database no newer than this version knows.
func (m *Manager) Stage(r io.Reader) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tmp := pendingPath(m.dbPath) + ".tmp"
	defer os.Remove(tmp)
	if err := unpack(r, tmp); err != nil {
		return err
	}
	if err := Check(tmp); err != nil {
		return err
	}
	return os.Rename(tmp, pendingPath(m.dbPath))
}

// Check opens a database file read-only and checks that it is a sound
// Vitrine library this version can open.
func Check(path string) error {
	conn, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer conn.Close()
	var version int
	if err := conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("backup: not a database: %w", err)
	}
	var files int
	if err := conn.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('files', 'tags', 'settings')").Scan(&files); err != nil || files < 3 {
		return errors.New("backup: not a Vitrine library")
	}
	if version > db.SchemaVersion() {
		return fmt.Errorf("backup: made by a newer Vitrine (schema %d, this one knows %d) - update Vitrine first", version, db.SchemaVersion())
	}
	var ok string
	if err := conn.QueryRow("PRAGMA quick_check").Scan(&ok); err != nil || ok != "ok" {
		return fmt.Errorf("backup: the database is damaged (%s)", ok)
	}
	return nil
}

// verify unpacks a backup file beside it and checks it (see Check); the unpacked copy goes again.
func verify(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	tmp := filepath.Join(filepath.Dir(path), ".verify-"+filepath.Base(path)+".db")
	defer os.Remove(tmp)
	if err := unpack(f, tmp); err != nil {
		return err
	}
	return Check(tmp)
}

// ApplyPending puts a staged backup in place of the meta.db at dbPath, before
// anything opens it. The database it replaces is kept as
// meta.db.before-restore. It reports whether it did.
func ApplyPending(dbPath string) (bool, error) {
	pending := pendingPath(dbPath)
	if !fileExists(pending) {
		return false, nil
	}
	before := dbPath + ".before-restore"
	for _, suf := range []string{"", "-wal", "-shm"} {
		os.Remove(before + suf)
	}
	if fileExists(dbPath) {
		// Its WAL goes with it (the newest changes may still be there).
		for _, suf := range []string{"", "-wal", "-shm"} {
			if err := os.Rename(dbPath+suf, before+suf); err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, fmt.Errorf("backup: keeping the current database: %w", err)
			}
		}
	}
	if err := os.Rename(pending, dbPath); err != nil {
		return false, fmt.Errorf("backup: restoring: %w", err)
	}
	return true, nil
}

// ---- files

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw, err := gzip.NewWriterLevel(out, gzip.BestSpeed)
	if err != nil {
		out.Close()
		return err
	}
	if _, err := io.Copy(zw, in); err != nil {
		out.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// unpack writes r to dst, gunzipping it when it is gzipped.
func unpack(r io.Reader, dst string) error {
	head := make([]byte, 2)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("backup: empty file")
	}
	r = io.MultiReader(strings.NewReader(string(head[:n])), r)
	if n == 2 && head[0] == 0x1f && head[1] == 0x8b {
		zr, err := gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		defer zr.Close()
		r = zr
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return fmt.Errorf("backup: reading the file: %w", err)
	}
	return out.Close()
}
