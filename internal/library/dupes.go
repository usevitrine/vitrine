package library

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"golang.org/x/image/webp"

	"github.com/usevitrine/vitrine/internal/dupes"
	"github.com/usevitrine/vitrine/internal/media"
)

// Near-duplicates: every file's thumbnail gets a perceptual hash in the
// background (new downloads within a minute); each newly hashed file is
// compared with the whole library and alike pairs wait for review.

// PendingPHash lists live files with a thumbnail but no hash attempt yet,
// newest first (at most limit).
func (l *Library) PendingPHash(ctx context.Context, limit int) ([]uint32, error) {
	return l.ids(ctx, `SELECT id FROM files WHERE deleted_at IS NULL AND flags & 64 = 0 AND flags & ? = ? AND flags & ? = 0
		ORDER BY id DESC LIMIT ?`, FlagThumbnail, FlagThumbnail, FlagPHash, limit)
}

func (l *Library) ids(ctx context.Context, query string, args ...any) ([]uint32, error) {
	rows, err := l.db.R.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var ids []uint32
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// HashFiles makes the files' perceptual hashes and records the pairs they
// form with the rest of the library. It returns how many pairs were new.
func (l *Library) HashFiles(ctx context.Context, ids []uint32) (int, error) {
	hashed := map[uint32]uint64{}
	for _, id := range ids {
		var h uint64
		ok := false
		if t, err := l.Thumb(ctx, id); err == nil {
			if img, err := webp.Decode(bytes.NewReader(t.Data)); err == nil {
				h, ok = dupes.Hash(img)
			}
		}
		var v any // NULL: no hash (flat picture, or no readable thumbnail)
		if ok {
			v = int64(h)
			hashed[id] = h
		}
		l.mu.Lock()
		_, err := l.db.W.ExecContext(ctx, "UPDATE files SET phash = ?, flags = flags | ? WHERE id = ?", v, FlagPHash, id)
		l.mu.Unlock()
		if err != nil {
			return 0, fmt.Errorf("library: %w", err)
		}
	}
	if len(hashed) == 0 {
		return 0, nil
	}
	all, err := l.hashedItems(ctx)
	if err != nil {
		return 0, err
	}
	var fresh []dupes.Item
	for _, it := range all {
		if _, ok := hashed[it.ID]; ok {
			fresh = append(fresh, it)
		}
	}
	pairs := dupes.Find(fresh, all)
	if len(pairs) == 0 {
		return 0, nil
	}
	now := l.now().UnixMilli()
	l.mu.Lock()
	defer l.mu.Unlock()
	tx, err := l.db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	added := 0
	for _, p := range pairs {
		res, err := tx.ExecContext(ctx, `INSERT INTO dupe_pairs (a, b, distance, found_at) VALUES (?, ?, ?, ?)
			ON CONFLICT DO NOTHING`, p.A, p.B, p.Distance, now)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	return added, tx.Commit()
}

func (l *Library) hashedItems(ctx context.Context) ([]dupes.Item, error) {
	rows, err := l.db.R.QueryContext(ctx, `SELECT id, phash, type, coalesce(width, 0), coalesce(height, 0),
		coalesce(duration_ms, 0) FROM files WHERE deleted_at IS NULL AND flags & 64 = 0 AND phash IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var out []dupes.Item
	for rows.Next() {
		var it dupes.Item
		var h int64
		var typ int
		if err := rows.Scan(&it.ID, &h, &typ, &it.Width, &it.Height, &it.DurationMS); err != nil {
			return nil, err
		}
		it.Hash = uint64(h)
		it.Video = media.Type(typ).IsVideo()
		out = append(out, it)
	}
	return out, rows.Err()
}

// DupePair is a pair to review.
type DupePair struct {
	A, B     uint32
	Distance int
}

// DupeProgress is how far the search for duplicates has got.
type DupeProgress struct {
	Checked int `json:"checked"` // files hashed (or tried)
	Total   int `json:"total"`   // files that will be
}

const livePair = `FROM dupe_pairs p JOIN files fa ON fa.id = p.a AND fa.deleted_at IS NULL
	JOIN files fb ON fb.id = p.b AND fb.deleted_at IS NULL WHERE p.status = 0 AND p.distance <= ?`

// DupePairs lists the pairs to review at most maxDistance apart, newest file
// first, with how many there are in all and the search's progress.
func (l *Library) DupePairs(ctx context.Context, maxDistance, offset, limit int) (int, []DupePair, DupeProgress, error) {
	var total int
	var prog DupeProgress
	if err := l.db.R.QueryRowContext(ctx, "SELECT count(*) "+livePair, maxDistance).Scan(&total); err != nil {
		return 0, nil, prog, fmt.Errorf("library: %w", err)
	}
	if err := l.db.R.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(flags & ? != 0), 0) FROM files
		WHERE deleted_at IS NULL AND flags & 64 = 0 AND flags & ? != 0`, FlagPHash, FlagThumbnail).Scan(&prog.Total, &prog.Checked); err != nil {
		return 0, nil, prog, fmt.Errorf("library: %w", err)
	}
	rows, err := l.db.R.QueryContext(ctx, "SELECT p.a, p.b, p.distance "+livePair+
		" ORDER BY p.b DESC, p.a DESC LIMIT ? OFFSET ?", maxDistance, limit, offset)
	if err != nil {
		return 0, nil, prog, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var out []DupePair
	for rows.Next() {
		var p DupePair
		if err := rows.Scan(&p.A, &p.B, &p.Distance); err != nil {
			return 0, nil, prog, err
		}
		out = append(out, p)
	}
	return total, out, prog, rows.Err()
}

// KeepBoth marks a pair as not duplicates (it's not listed again).
func (l *Library) KeepBoth(ctx context.Context, a, b uint32) error {
	return l.setPairStatus(ctx, a, b, 1)
}

func (l *Library) setPairStatus(ctx context.Context, a, b uint32, status int) error {
	a, b = min(a, b), max(a, b)
	l.mu.Lock()
	res, err := l.db.W.ExecContext(ctx, "UPDATE dupe_pairs SET status = ? WHERE a = ? AND b = ?", status, a, b)
	l.mu.Unlock()
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("library: pair %d/%d: %w", a, b, ErrNoFile)
	}
	return nil
}

// KeepOne keeps one file of a pair: it gets the other's tags, links and
// download sources, and the other is deleted (with the usual undo window).
// What was copied is remembered with the pair, so UndoDupe can take it back.
func (l *Library) KeepOne(ctx context.Context, keep, drop uint32) error {
	if keep == drop {
		return fmt.Errorf("library: %w: the same file", ErrInvalid)
	}
	m, err := l.MergeInto(ctx, keep, drop)
	if err != nil {
		return err
	}
	if err := l.Delete(ctx, []uint32{drop}, fmt.Sprintf("duplicate of #%d", keep)); err != nil {
		return err
	}
	b, _ := json.Marshal(m)
	l.mu.Lock()
	_, err = l.db.W.ExecContext(ctx, "UPDATE dupe_pairs SET merged = ? WHERE a = ? AND b = ?", string(b), min(keep, drop), max(keep, drop))
	l.mu.Unlock()
	return err
}

// UndoDupe takes back a decision on a pair: a deleted file comes back, what
// was copied onto the kept one is removed again, and the pair is to review.
func (l *Library) UndoDupe(ctx context.Context, a, b uint32) error {
	a, b = min(a, b), max(a, b)
	var merged sql.NullString
	err := l.db.R.QueryRowContext(ctx, "SELECT merged FROM dupe_pairs WHERE a = ? AND b = ?", a, b).Scan(&merged)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("library: pair %d/%d: %w", a, b, ErrNoFile)
	}
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	var deleted []uint32
	for _, id := range []uint32{a, b} {
		var d sql.NullInt64
		if err := l.db.R.QueryRowContext(ctx, "SELECT deleted_at FROM files WHERE id = ?", id).Scan(&d); err != nil {
			return fmt.Errorf("library: %w", err)
		}
		if d.Valid {
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		if err := l.Undelete(ctx, deleted); err != nil {
			return err
		}
	}
	if merged.Valid {
		var m Merged
		if json.Unmarshal([]byte(merged.String), &m) == nil {
			if err := l.unmerge(ctx, m); err != nil {
				return err
			}
		}
	}
	l.mu.Lock()
	_, err = l.db.W.ExecContext(ctx, "UPDATE dupe_pairs SET status = 0, merged = NULL WHERE a = ? AND b = ?", a, b)
	l.mu.Unlock()
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}

// Merged is what MergeInto added to the kept file (so it can be taken back).
type Merged struct {
	Keep     uint32      `json:"keep"`
	Tags     [][2]uint32 `json:"tags,omitempty"`     // tag ID, origin
	URLs     [][3]int64  `json:"urls,omitempty"`     // url ID, kind, site
	Sources  []mergedSrc `json:"sources,omitempty"`  // download sources
	Sites    []uint32    `json:"sites,omitempty"`    // sites the file wasn't from before
	PostedAt *int64      `json:"posted_at"`          // the kept file's date before
	Favorite int64       `json:"favorite,omitempty"` // the kept file became a favorite (since then, Unix ms)
}

type mergedSrc struct {
	Site  uint32 `json:"site"`
	Query string `json:"query"`
}

// MergeInto copies one file's visible tags (keeping their origin), links and
// download sources onto another, and its earlier posting date.
func (l *Library) MergeInto(ctx context.Context, keep, from uint32) (Merged, error) {
	m := Merged{Keep: keep}
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		for _, id := range []uint32{keep, from} {
			if err := fileExists(ctx, tx, id); err != nil {
				return nil, err
			}
		}
		before, err := fileTags(ctx, tx, keep)
		if err != nil {
			return nil, err
		}
		sitesBefore, err := fileSiteIDs(ctx, tx, keep)
		if err != nil {
			return nil, err
		}
		// What the kept file doesn't have yet.
		err = scanRows(ctx, tx, func(r *sql.Rows) error {
			var t [2]uint32
			if err := r.Scan(&t[0], &t[1]); err != nil {
				return err
			}
			m.Tags = append(m.Tags, t)
			return nil
		}, `SELECT ft.tag_id, ft.origin FROM file_tags ft WHERE ft.file_id = ?2
			AND (ft.origin = 0 OR NOT EXISTS (SELECT 1 FROM hidden_tags h WHERE h.file_id = ?2 AND h.tag_id = ft.tag_id))
			AND NOT EXISTS (SELECT 1 FROM file_tags k WHERE k.file_id = ?1 AND k.tag_id = ft.tag_id AND k.origin = ft.origin)`, keep, from)
		if err != nil {
			return nil, err
		}
		err = scanRows(ctx, tx, func(r *sql.Rows) error {
			var u [3]int64
			if err := r.Scan(&u[0], &u[1], &u[2]); err != nil {
				return err
			}
			m.URLs = append(m.URLs, u)
			return nil
		}, `SELECT url_id, kind, site_id FROM file_urls f WHERE file_id = ?2 AND NOT EXISTS (
			SELECT 1 FROM file_urls k WHERE k.file_id = ?1 AND k.url_id = f.url_id AND k.kind = f.kind)`, keep, from)
		if err != nil {
			return nil, err
		}
		err = scanRows(ctx, tx, func(r *sql.Rows) error {
			var s mergedSrc
			if err := r.Scan(&s.Site, &s.Query); err != nil {
				return err
			}
			m.Sources = append(m.Sources, s)
			return nil
		}, `SELECT site_id, query FROM file_sources f WHERE file_id = ?2 AND NOT EXISTS (
			SELECT 1 FROM file_sources k WHERE k.file_id = ?1 AND k.site_id = f.site_id AND k.query = f.query)`, keep, from)
		if err != nil {
			return nil, err
		}
		var posted sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT posted_at FROM files WHERE id = ?", keep).Scan(&posted); err != nil {
			return nil, err
		}
		if posted.Valid {
			m.PostedAt = &posted.Int64
		}

		for _, t := range m.Tags {
			if _, err := tx.ExecContext(ctx, "INSERT INTO file_tags (file_id, tag_id, origin) VALUES (?, ?, ?)", keep, t[0], t[1]); err != nil {
				return nil, err
			}
		}
		for _, u := range m.URLs {
			if _, err := tx.ExecContext(ctx, "INSERT INTO file_urls (file_id, url_id, kind, site_id) VALUES (?, ?, ?, ?)",
				keep, u[0], u[1], u[2]); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO file_sources (file_id, site_id, query, subscription_id, downloaded, first_at)
			SELECT ?1, site_id, query, subscription_id, downloaded, first_at FROM file_sources
			WHERE file_id = ?2 ON CONFLICT DO NOTHING`, keep, from); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE files SET posted_at = (SELECT min(coalesce(a.posted_at, b.posted_at),
			coalesce(b.posted_at, a.posted_at)) FROM files a, files b WHERE a.id = ?1 AND b.id = ?2) WHERE id = ?1`, keep, from); err != nil {
			return nil, err
		}
		sitesAfter, err := fileSiteIDs(ctx, tx, keep)
		if err != nil {
			return nil, err
		}
		for _, s := range sitesAfter {
			if !slices.Contains(sitesBefore, s) {
				m.Sites = append(m.Sites, s)
			}
		}
		if m.Favorite, err = carryFavorite(ctx, tx, keep, from); err != nil {
			return nil, err
		}
		apply, err := l.tagsChanged(ctx, tx, keep, before, m.Sites, nil)
		if err != nil || m.Favorite == 0 {
			return apply, err
		}
		return func() error {
			l.ix.SetFavorite(keep, m.Favorite)
			if apply == nil {
				return nil
			}
			return apply()
		}, nil
	})
	return m, err
}

// unmerge takes back what MergeInto added.
func (l *Library) unmerge(ctx context.Context, m Merged) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		before, err := fileTags(ctx, tx, m.Keep)
		if err != nil {
			return nil, err
		}
		for _, t := range m.Tags {
			if _, err := tx.ExecContext(ctx, "DELETE FROM file_tags WHERE file_id = ? AND tag_id = ? AND origin = ?", m.Keep, t[0], t[1]); err != nil {
				return nil, err
			}
		}
		for _, u := range m.URLs {
			if _, err := tx.ExecContext(ctx, "DELETE FROM file_urls WHERE file_id = ? AND url_id = ? AND kind = ?", m.Keep, u[0], u[1]); err != nil {
				return nil, err
			}
		}
		for _, s := range m.Sources {
			if _, err := tx.ExecContext(ctx, "DELETE FROM file_sources WHERE file_id = ? AND site_id = ? AND query = ?", m.Keep, s.Site, s.Query); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE files SET posted_at = ? WHERE id = ?", m.PostedAt, m.Keep); err != nil {
			return nil, err
		}
		if m.Favorite != 0 {
			if _, err := tx.ExecContext(ctx, "DELETE FROM favorites WHERE file_id = ?", m.Keep); err != nil {
				return nil, err
			}
		}
		apply, err := l.tagsChanged(ctx, tx, m.Keep, before, nil, m.Sites)
		if err != nil || m.Favorite == 0 {
			return apply, err
		}
		return func() error {
			l.ix.SetFavorite(m.Keep, 0)
			if apply == nil {
				return nil
			}
			return apply()
		}, nil
	})
}

// tagsChanged finishes a change of a file's tags and sites: modified_at (if
// its tags changed) and the in-memory index once the change is saved.
func (l *Library) tagsChanged(ctx context.Context, tx *sql.Tx, file uint32, before []uint32, addSites, removeSites []uint32) (func() error, error) {
	after, err := fileTags(ctx, tx, file)
	if err != nil {
		return nil, err
	}
	added, removed := diff(before, after)
	now := l.now().UnixMilli()
	if len(added)+len(removed) > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE files SET modified_at = ? WHERE id = ?", now, file); err != nil {
			return nil, err
		}
	}
	f, _, deleted, err := readFile(ctx, tx, file)
	if err != nil {
		return nil, err
	}
	return func() error {
		for _, s := range addSites {
			l.ix.AddFileSite(file, s)
		}
		for _, s := range removeSites {
			l.ix.RemoveFileSite(file, s)
		}
		if deleted {
			return nil
		}
		if len(added)+len(removed) > 0 {
			if err := l.ix.UpdateTags(file, added, removed, now); err != nil {
				return err
			}
		}
		l.ix.UpdateFile(f)
		return nil
	}, nil
}

// fileSiteIDs are the sites a file has tags or links from.
func fileSiteIDs(ctx context.Context, tx *sql.Tx, file uint32) ([]uint32, error) {
	var ids []uint32
	err := scanRows(ctx, tx, func(r *sql.Rows) error {
		var id uint32
		if err := r.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
		return nil
	}, `SELECT origin FROM file_tags WHERE file_id = ?1 AND origin != 0
		UNION SELECT site_id FROM file_urls WHERE file_id = ?1 AND site_id != 0`, file)
	return ids, err
}

func scanRows(ctx context.Context, tx *sql.Tx, each func(*sql.Rows) error, query string, args ...any) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := each(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// FileFacts are what the duplicates screen compares besides the file itself.
type FileFacts struct {
	Tags  int      `json:"tags"`
	Sites []string `json:"sites"`
}

// Facts returns each file's tag count and the sites it's from.
func (l *Library) Facts(ctx context.Context, ids []uint32) (map[uint32]FileFacts, error) {
	out := make(map[uint32]FileFacts, len(ids))
	for _, id := range ids {
		var f FileFacts
		err := l.db.R.QueryRowContext(ctx, `SELECT count(DISTINCT tag_id) FROM file_tags ft WHERE file_id = ?1
			AND (origin = 0 OR NOT EXISTS (SELECT 1 FROM hidden_tags h WHERE h.file_id = ?1 AND h.tag_id = ft.tag_id))`, id).Scan(&f.Tags)
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		rows, err := l.db.R.QueryContext(ctx, `SELECT DISTINCT s.name FROM sites s WHERE s.id IN (
			SELECT origin FROM file_tags WHERE file_id = ?1 AND origin != 0
			UNION SELECT site_id FROM file_urls WHERE file_id = ?1 AND site_id != 0) ORDER BY s.name`, id)
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				f.Sites = append(f.Sites, n)
			}
		}
		rows.Close()
		out[id] = f
	}
	return out, nil
}
