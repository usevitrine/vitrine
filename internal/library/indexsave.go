package library

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/index"
)

// The saved tags (index/saved.go): a big library's start read 54 million file-tag links for ~20 s of its ~25 s
// (2026-11-14, measured on a copy of a big real library). The server saves the tags' files as it stops; the next start
// reads them instead - only when the token saved with them is still in meta, which triggers on file_tags, hidden_tags
// and the files that decide what's in the library take away on any change (migration 0056), whatever program makes it.
// After a start from the save, StartIndexCheck reads file_tags all the same, in the background, and compares; a
// difference (a bug: the index and the database apart) throws the save away, so the next start reads them from the
// database.

const indexSaveKey = "index_save"

// loadSaved builds the index from the saved tags at path, if they're this database's as it is now; nil if not.
func loadSaved(ctx context.Context, d *db.DB, path string) *index.Index {
	var want string
	if err := d.R.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", indexSaveKey).Scan(&want); err != nil || want == "" {
		return nil
	}
	if !tokenFits(want) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if got, err := index.SavedToken(bytes.NewReader(data)); err != nil || got != want {
		return nil
	}
	ix, err := index.LoadSaved(ctx, d.R, data)
	if err != nil {
		return nil
	}
	return ix
}

// A token: random, then the schema it was saved under (a database changed by migrations since isn't read from it).
func newToken() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b) + ":" + strconv.Itoa(db.SchemaVersion())
}

func tokenFits(tok string) bool {
	i := bytes.LastIndexByte([]byte(tok), ':')
	return i >= 0 && tok[i+1:] == strconv.Itoa(db.SchemaVersion())
}

// FromSave says whether the index came from the saved tags.
func (l *Library) FromSave() bool { return l.fromSave }

// SaveIndex saves the tags' files at path for the next start (the server, as it stops). The library is held still
// meanwhile: the index and the database agree. The token goes into meta first, so any write while the file is being
// made takes it away again - the file is kept only if it's still there. Returns the file's size.
func (l *Library) SaveIndex(ctx context.Context, path string) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	tok := newToken()
	if _, err := l.db.W.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, indexSaveKey, tok); err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	err = l.ix.SaveTags(f, tok)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		l.forgetSave(ctx, path)
		return 0, err
	}
	var still string
	if l.db.W.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", indexSaveKey).Scan(&still); still != tok {
		os.Remove(path)
		return 0, errors.New("library: the library changed while its index was being saved")
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// forgetSave throws the saved tags away: the next start reads them from the database.
func (l *Library) forgetSave(ctx context.Context, path string) {
	l.db.W.ExecContext(ctx, "DELETE FROM meta WHERE key = ?", indexSaveKey)
	os.Remove(path)
}

// StartIndexCheck, after a start from the saved tags, reads them from the database too (in the background, as the
// database was when the index was loaded) and compares; done gets how many tags differ - any, and the save is thrown
// away. Call it right after Open, before anything writes. Nothing to do after a start from the database.
func (l *Library) StartIndexCheck(ctx context.Context, path string, done func(bad int, took time.Duration, err error)) {
	if !l.fromSave || l.savedSums == nil {
		return
	}
	want := l.savedSums
	l.savedSums = nil
	alive := l.ix.Alive()
	start := time.Now()
	// (the read transaction's snapshot taken now: what the index was loaded from, whatever is written after)
	tx, err := l.db.R.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err == nil {
		var one int
		err = tx.QueryRowContext(ctx, "SELECT 1 FROM meta LIMIT 1").Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
	}
	if err != nil {
		if tx != nil {
			tx.Rollback()
		}
		done(0, 0, err)
		return
	}
	go func() {
		defer tx.Rollback()
		bad, err := index.CheckTags(ctx, tx, alive, want)
		if err == nil && bad > 0 {
			l.forgetSave(context.WithoutCancel(ctx), path)
		}
		done(bad, time.Since(start), err)
	}()
}
