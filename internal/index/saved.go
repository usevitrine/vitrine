package index

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/RoaringBitmap/roaring/v2"
)

// A saved copy of the tags' files (2026-11-14, after measuring a big library's start: 25 s of which ~20 s read its 54
// million file-tag links, row by row; "files", "tags" and the rest took ~4.5 s). Saved when the server stops
// (library.SaveIndex), read at the next start instead of file_tags - only when nothing that shapes them changed since
// (library.Open: the save's token, kept in meta and cleared by triggers on any such write). Everything else is still
// read from the database. The format: a header (magic, version, the token), then each tag with files (its ID, its
// bitmap in roaring's portable format), an end marker, and a CRC-32 of all of it.

const (
	savedMagic   = "VITRINE-TAGS\n"
	savedVersion = 1
	savedEnd     = ^uint32(0)
)

// ErrSavedTags: the saved tags can't be used (another format, damaged, or not this database's); load them from it.
var ErrSavedTags = errors.New("index: the saved tags can't be used")

// SaveTags writes the tags' files - as a full load would read them (alive files only, hidden site tags left out) - with
// token in its header. The caller holds whatever keeps the library still meanwhile.
func (ix *Index) SaveTags(w io.Writer, token string) error {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	sum := crc32.NewIEEE()
	bw := bufio.NewWriterSize(io.MultiWriter(w, sum), 1<<20)
	if err := writeSavedHeader(bw, token); err != nil {
		return err
	}
	var id [4]byte
	for t, bm := range ix.raw {
		if bm == nil || t == 0 {
			continue
		}
		files := roaring.And(bm, ix.alive)
		if files.IsEmpty() {
			continue
		}
		files.RunOptimize()
		binary.LittleEndian.PutUint32(id[:], uint32(t))
		if _, err := bw.Write(id[:]); err != nil {
			return err
		}
		if _, err := files.WriteTo(bw); err != nil {
			return err
		}
	}
	binary.LittleEndian.PutUint32(id[:], savedEnd)
	if _, err := bw.Write(id[:]); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	var crc [4]byte
	binary.LittleEndian.PutUint32(crc[:], sum.Sum32())
	_, err := w.Write(crc[:])
	return err
}

func writeSavedHeader(w io.Writer, token string) error {
	b := []byte(savedMagic)
	b = binary.LittleEndian.AppendUint32(b, savedVersion)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(token)))
	b = append(b, token...)
	_, err := w.Write(b)
	return err
}

// SavedToken reads a saved file's token (to compare with the one kept in meta before reading the rest).
func SavedToken(r io.Reader) (string, error) {
	head := make([]byte, len(savedMagic)+8)
	if _, err := io.ReadFull(r, head); err != nil || string(head[:len(savedMagic)]) != savedMagic {
		return "", ErrSavedTags
	}
	if binary.LittleEndian.Uint32(head[len(savedMagic):]) != savedVersion {
		return "", ErrSavedTags
	}
	n := binary.LittleEndian.Uint32(head[len(savedMagic)+4:])
	if n > 256 {
		return "", ErrSavedTags
	}
	tok := make([]byte, n)
	if _, err := io.ReadFull(r, tok); err != nil {
		return "", ErrSavedTags
	}
	return string(tok), nil
}

// LoadSaved builds the index as Load does, its tags' files from data (a whole saved file, its token already checked)
// instead of file_tags. A damaged file: ErrSavedTags (load it from the database then).
func LoadSaved(ctx context.Context, db *sql.DB, data []byte) (*Index, error) {
	if len(data) < 4 || crc32.ChecksumIEEE(data[:len(data)-4]) != binary.LittleEndian.Uint32(data[len(data)-4:]) {
		return nil, ErrSavedTags
	}
	body := data[:len(data)-4]
	return loadWith(ctx, db, false, func(ix *Index, _ context.Context, _ *sql.Tx) error {
		return ix.readSavedTags(body)
	})
}

func (ix *Index) readSavedTags(data []byte) error {
	r := &sliceReader{b: data}
	if _, err := SavedToken(r); err != nil {
		return err
	}
	for {
		var id [4]byte
		if _, err := io.ReadFull(r, id[:]); err != nil {
			return ErrSavedTags
		}
		t := binary.LittleEndian.Uint32(id[:])
		if t == savedEnd {
			break
		}
		bm := roaring.New()
		if _, err := bm.ReadFrom(r); err != nil {
			return fmt.Errorf("%w: tag %d: %v", ErrSavedTags, t, err)
		}
		// (a tag the database no longer has, or files it doesn't: not this database's save)
		if err := ix.checkTags([]uint32{t}); err != nil {
			return ErrSavedTags
		}
		if !bm.IsEmpty() && bm.Maximum() >= uint32(len(ix.tagCount)) {
			return ErrSavedTags
		}
		ix.raw[t].Or(bm) // (into the tag's own bitmap: its effective files are often the same one - eff)
	}
	if r.i != len(r.b) {
		return ErrSavedTags
	}
	ix.countTags() // (run-optimized there)
	ix.savedSums = make(map[uint32]uint64, len(ix.raw))
	for t, bm := range ix.raw {
		if bm != nil && t != 0 && !bm.IsEmpty() {
			ix.savedSums[uint32(t)] = bm.Checksum()
		}
	}
	return nil
}

type sliceReader struct {
	b []byte
	i int
}

func (s *sliceReader) Read(p []byte) (int, error) {
	if s.i >= len(s.b) {
		return 0, io.EOF
	}
	n := copy(p, s.b[s.i:])
	s.i += n
	return n, nil
}

// SavedSums hands over the checksums of the tags' files as read from a save (nil after a load from the database, or
// once taken) - for CheckTags to compare with the database.
func (ix *Index) SavedSums() map[uint32]uint64 {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	out := ix.savedSums
	ix.savedSums = nil
	return out
}

// checksum is a bitmap's, the same whatever its containers (it's run-optimized first: one shape per set of files).
func checksum(bm *roaring.Bitmap) uint64 {
	bm.RunOptimize()
	return bm.Checksum()
}

// CheckTags reads file_tags (in tx: the database as the index was loaded) the way a full load does, one tag at a time,
// and returns how many tags' files differ from want (TagChecksums of the loaded index). alive: the files alive then.
func CheckTags(ctx context.Context, tx *sql.Tx, alive *roaring.Bitmap, want map[uint32]uint64) (int, error) {
	hidden := map[uint32][]uint32{}
	rows, err := tx.QueryContext(ctx, `
		SELECT h.file_id, h.tag_id FROM hidden_tags h
		WHERE NOT EXISTS (SELECT 1 FROM file_tags ft
		                  WHERE ft.file_id = h.file_id AND ft.tag_id = h.tag_id AND ft.origin = 0)`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var f, t uint32
		if err := rows.Scan(&f, &t); err != nil {
			rows.Close()
			return 0, err
		}
		hidden[t] = append(hidden[t], f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT tag_id, file_id FROM file_tags ORDER BY tag_id, file_id")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	bad, present := 0, 0
	var cur uint32
	var batch []uint32
	flush := func() {
		if len(batch) == 0 {
			return
		}
		bm := roaring.BitmapOf(batch...)
		for _, f := range hidden[cur] {
			bm.Remove(f)
		}
		batch = batch[:0]
		if bm.IsEmpty() {
			return
		}
		sum, ok := want[cur]
		if ok {
			present++
		}
		if !ok || sum != checksum(bm) {
			bad++
		}
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		var t, f uint32
		if err := rows.Scan(&t, &f); err != nil {
			return 0, err
		}
		if t != cur {
			flush()
			cur = t
		}
		if alive.Contains(f) && (len(batch) == 0 || batch[len(batch)-1] != f) {
			batch = append(batch, f)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	flush()
	bad += len(want) - present // (tags the save had and the database doesn't)
	return bad, nil
}

// Alive is a copy of the files alive now (for CheckTags).
func (ix *Index) Alive() *roaring.Bitmap {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.alive.Clone()
}
