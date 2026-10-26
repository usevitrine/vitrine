package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/store"
)

// Wiping a site (2026-10-26, the user: removing a site can "completely wipe their server of any hint that they ever
// had it enabled. That means deleting all files/metadata/deleted files history/any entry in every menu"): the files
// only it gave go for good, files another site gave too stay without its tags, links and sources, and everything
// that names it - downloads, subscriptions, history, deletion records, notices, saved searches, settings - goes.
// Deletion records from before the sites were kept (tombstones.sites NULL) can't be told and stay.

// SiteTraces is what wiping a site would remove.
type SiteTraces struct {
	Files   int   `json:"files"`   // removed for good (only this site gave them; the trash's too)
	Bytes   int64 `json:"bytes"`   // their size
	Shared  int   `json:"shared"`  // kept (another site gave them too), this site's tags and links taken off
	Comics  int   `json:"comics"`  // its comics (the trash's too)
	Records int   `json:"records"` // deletion records naming it (of files gone before)
	Unknown int   `json:"unknown"` // deletion records that can't be told (older): kept
	Other   bool  `json:"other"`   // anything else naming it (downloads, history, notices, ...)
}

// Any reports whether there's anything to wipe.
func (t SiteTraces) Any() bool {
	return t.Files+t.Shared+t.Comics+t.Records > 0 || t.Other
}

// Wiped is what WipeSite removed.
type Wiped struct {
	Files   int          `json:"files"`
	Shared  int          `json:"shared"`
	Comics  int          `json:"comics"`
	Records int          `json:"records"`
	Hashes  []store.Hash `json:"-"` // the removed files' (their cached copies elsewhere go too)
}

// (a file has something from the site ?1: a link, a tag, a source)
const fromSite = `(f.id IN (SELECT file_id FROM file_urls WHERE site_id = ?1)
	OR f.id IN (SELECT file_id FROM file_tags WHERE origin = ?1)
	OR f.id IN (SELECT file_id FROM file_sources WHERE site_id = ?1))`

// (... and something from another site, or it's a page of a comic that stays)
const keptToo = `(EXISTS (SELECT 1 FROM file_urls WHERE file_id = f.id AND site_id NOT IN (0, ?1))
	OR EXISTS (SELECT 1 FROM file_tags WHERE file_id = f.id AND origin NOT IN (0, ?1))
	OR EXISTS (SELECT 1 FROM file_sources WHERE file_id = f.id AND site_id NOT IN (0, ?1))
	OR EXISTS (SELECT 1 FROM comic_pages p JOIN comics c ON c.id = p.comic_id WHERE p.file_id = f.id AND (c.site_id IS NOT ?1)))`

// siteID is a site's row (false: it never had one - nothing to wipe).
func (l *Library) siteID(ctx context.Context, name string) (int64, bool, error) {
	var id int64
	err := l.db.R.QueryRowContext(ctx, "SELECT id FROM sites WHERE name = ? AND id != 0", name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("library: %w", err)
	}
	return id, true, nil
}

// SiteTraces counts what WipeSite would remove.
func (l *Library) SiteTraces(ctx context.Context, name string) (SiteTraces, error) {
	var t SiteTraces
	site, ok, err := l.siteID(ctx, name)
	if err != nil || !ok {
		return t, err
	}
	q := l.db.R
	if err := q.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(size), 0) FROM files f WHERE "+fromSite+" AND NOT "+keptToo, site).
		Scan(&t.Files, &t.Bytes); err != nil {
		return t, fmt.Errorf("library: %w", err)
	}
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM files f WHERE "+fromSite+" AND "+keptToo, site).Scan(&t.Shared); err != nil {
		return t, fmt.Errorf("library: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM comics WHERE site_id = ?1),
		(SELECT count(*) FROM tombstones WHERE instr(sites, ',' || ?2 || ',') > 0 AND sha256 NOT IN (SELECT sha256 FROM files)),
		(SELECT count(*) FROM tombstones WHERE sites IS NULL),
		EXISTS (SELECT 1 FROM searches WHERE site_id = ?1) OR EXISTS (SELECT 1 FROM subscriptions WHERE site_id = ?1)
			OR EXISTS (SELECT 1 FROM seen_posts WHERE site_id = ?1) OR EXISTS (SELECT 1 FROM notices WHERE site = ?2)
			OR EXISTS (SELECT 1 FROM tag_types WHERE site_id = ?1)`, site, name).
		Scan(&t.Comics, &t.Records, &t.Unknown, &t.Other); err != nil {
		return t, fmt.Errorf("library: %w", err)
	}
	return t, nil
}

// WipeSite removes every trace of a site from the library (see above). The site's plugin should be gone first (no
// download running for it). Long: a big site's files go in batches, other changes going on in between.
func (l *Library) WipeSite(ctx context.Context, name string) (Wiped, error) {
	var w Wiped
	site, ok, err := l.siteID(ctx, name)
	if err != nil || !ok {
		return w, err
	}
	names := append(l.ix.SiteAliases(uint32(site)), name)

	// Its tags, to remove afterwards those nothing else has.
	tags, err := l.ids(ctx, "SELECT DISTINCT tag_id FROM file_tags WHERE origin = ?1 UNION SELECT DISTINCT tag_id FROM comic_tags WHERE origin = ?1", site)
	if err != nil {
		return w, err
	}

	// 1. Its comics (pages only they have go with them).
	comics, err := l.ids(ctx, "SELECT id FROM comics WHERE site_id = ?", site)
	if err != nil {
		return w, err
	}
	for c := range slices.Chunk(comics, 500) {
		ids := make([]int64, len(c))
		for i, id := range c {
			ids[i] = int64(id)
		}
		if err := l.DeleteComics(ctx, ids); err != nil {
			return w, err
		}
		n, err := l.PurgeComics(ctx, ids)
		w.Comics += n
		if err != nil {
			return w, err
		}
	}

	// 2. The files only it gave: deleted (out of searches, duplicates), removed for good, their records gone.
	type own struct {
		id    uint32
		sha   []byte
		alive bool // in the library (else in the trash, or a page)
	}
	var files []own
	rows, err := l.db.R.QueryContext(ctx, "SELECT id, sha256, deleted_at IS NULL AND flags & 64 = 0 FROM files f WHERE "+fromSite+" AND NOT "+keptToo, site)
	if err != nil {
		return w, fmt.Errorf("library: %w", err)
	}
	for rows.Next() {
		var o own
		if err := rows.Scan(&o.id, &o.sha, &o.alive); err != nil {
			rows.Close()
			return w, err
		}
		files = append(files, o)
	}
	rows.Close()
	for batch := range slices.Chunk(files, 2000) {
		var alive []uint32
		var ids, shas []any
		for _, f := range batch {
			if f.alive {
				alive = append(alive, f.id)
			}
			ids = append(ids, f.id)
			shas = append(shas, f.sha)
			var h store.Hash
			copy(h[:], f.sha)
			w.Hashes = append(w.Hashes, h)
		}
		if err := l.Delete(ctx, alive, ""); err != nil {
			return w, err
		}
		marks := "?" + strings.Repeat(",?", len(ids)-1)
		if err := l.write(ctx, func(tx *sql.Tx) (func() error, error) { // (pages no comic has any more: into the trash too)
			_, err := tx.ExecContext(ctx, "UPDATE files SET deleted_at = ? WHERE deleted_at IS NULL AND id IN ("+marks+")", append([]any{l.now().UnixMilli()}, ids...)...)
			return nil, err
		}); err != nil {
			return w, err
		}
		n, err := l.purge(ctx, "deleted_at IS NOT NULL AND id IN ("+marks+")", ids, -1)
		w.Files += n
		if err != nil {
			return w, err
		}
		if err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
			_, err := tx.ExecContext(ctx, "DELETE FROM tombstones WHERE sha256 IN ("+marks+")", shas...)
			return nil, err
		}); err != nil {
			return w, err
		}
	}

	// 3. The files another site gave too: its tags, links and sources taken off (not an edit: modified stays).
	shared, err := l.ids(ctx, "SELECT id FROM files f WHERE deleted_at IS NULL AND flags & 64 = 0 AND "+fromSite, site)
	if err != nil {
		return w, err
	}
	strip := func(tx *sql.Tx, f uint32) error {
		for _, q := range []string{
			"DELETE FROM file_tags WHERE file_id = ?1 AND origin = ?2",
			"DELETE FROM file_urls WHERE file_id = ?1 AND site_id = ?2",
			"DELETE FROM file_sources WHERE file_id = ?1 AND site_id = ?2",
		} {
			if _, err := tx.ExecContext(ctx, q, f, site); err != nil {
				return err
			}
		}
		return nil
	}
	for batch := range slices.Chunk(shared, 500) {
		if err := l.changeTags(ctx, batch, nil, false, false,
			func(*sql.Tx, uint32) (bool, error) { return false, nil },
			func(tx *sql.Tx, f uint32, _ []uint32) error { return strip(tx, f) }); err != nil {
			return w, err
		}
		w.Shared += len(batch)
	}

	// 4. Everything else naming it, in one go (the trash's files and pages too: no index to keep up).
	var subs []uint32
	err = l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM subscriptions WHERE site_id = ?", site)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uint32
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			subs = append(subs, id)
		}
		rows.Close()
		for _, q := range []string{
			"DELETE FROM file_tags WHERE origin = ?1",
			"DELETE FROM file_urls WHERE site_id = ?1",
			"DELETE FROM file_sources WHERE site_id = ?1",
			"DELETE FROM comic_tags WHERE origin = ?1",
			"DELETE FROM comics WHERE site_id = ?1",
			"DELETE FROM searches WHERE site_id = ?1",
			"DELETE FROM subscriptions WHERE site_id = ?1",
			"DELETE FROM sub_groups WHERE site_id = ?1",
			"DELETE FROM seen_posts WHERE site_id = ?1",
			"DELETE FROM tag_types WHERE site_id = ?1",
			"DELETE FROM notices WHERE site = ?2",
			"DELETE FROM site_pauses WHERE site = ?2",
			// deletion records: it's taken off the sites they name; one that named only it goes
			"UPDATE tombstones SET sites = replace(sites, ',' || ?2 || ',', ',') WHERE instr(sites, ',' || ?2 || ',') > 0",
			"DELETE FROM tombstones WHERE sites = ','",
			"DELETE FROM sites WHERE id = ?1",
		} {
			res, err := tx.ExecContext(ctx, q, site, name)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", q, err)
			}
			if strings.HasPrefix(q, "DELETE FROM tombstones") {
				k, _ := res.RowsAffected()
				w.Records += int(k)
			}
		}
		if err := dropFromSettings(ctx, tx, name, names, l.now().UnixMilli()); err != nil {
			return nil, err
		}
		return func() error {
			l.ix.DropSite(uint32(site), subs)
			return nil
		}, nil
	})
	if err != nil {
		return w, err
	}

	// 5. Its tags nothing else has any more, and links no file has.
	var gone []uint32
	for batch := range slices.Chunk(tags, 500) {
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		marks := "?" + strings.Repeat(",?", len(batch)-1)
		if err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
			rows, err := tx.QueryContext(ctx, `DELETE FROM tags WHERE id IN (`+marks+`)
				AND NOT EXISTS (SELECT 1 FROM file_tags WHERE tag_id = tags.id)
				AND NOT EXISTS (SELECT 1 FROM comic_tags WHERE tag_id = tags.id)
				AND NOT EXISTS (SELECT 1 FROM tag_parents WHERE child_id = tags.id OR parent_id = tags.id)
				AND NOT EXISTS (SELECT 1 FROM tag_siblings WHERE from_id = tags.id OR to_id = tags.id)
				RETURNING id`, args...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			for rows.Next() {
				var id uint32
				if err := rows.Scan(&id); err != nil {
					return nil, err
				}
				gone = append(gone, id)
			}
			return nil, rows.Err()
		}); err != nil {
			return w, err
		}
	}
	l.ix.ForgetTags(gone)
	// (links no file has: those of files removed before too - nothing reads them but through a file)
	err = l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		_, err := tx.ExecContext(ctx, "DELETE FROM urls WHERE NOT EXISTS (SELECT 1 FROM file_urls WHERE url_id = urls.id)")
		return nil, err
	})
	return w, err
}

// dropFromSettings takes a wiped site out of the saved searches (one that only searched it goes), the searches the
// pages open with, and the per-site settings.
func dropFromSettings(ctx context.Context, tx *sql.Tx, name string, names []string, now int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, query FROM saved_searches")
	if err != nil {
		return err
	}
	type saved struct {
		id    int64
		query string
	}
	var change []saved
	for rows.Next() {
		var s saved
		if err := rows.Scan(&s.id, &s.query); err != nil {
			rows.Close()
			return err
		}
		if q := search.DropSite(s.query, names); q != s.query {
			change = append(change, saved{s.id, q})
		}
	}
	rows.Close()
	for _, s := range change {
		q, args := "UPDATE saved_searches SET query = ? WHERE id = ?", []any{s.query, s.id}
		if strings.TrimSpace(s.query) == "" {
			q, args = "DELETE FROM saved_searches WHERE id = ?", []any{s.id}
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}

	set := func(key string, edit func(raw []byte) (any, bool)) error {
		var raw []byte
		err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		v, changed := edit(raw)
		if !changed {
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE settings SET value = ?, updated_at = ? WHERE key = ?", string(b), now, key)
		return err
	}
	for _, key := range []string{"defaultSearch", "comicsSearch", "browseSearch"} {
		if err := set(key, func(raw []byte) (any, bool) {
			var s string
			if json.Unmarshal(raw, &s) != nil {
				return nil, false
			}
			q := search.DropSite(s, names)
			return q, q != s
		}); err != nil {
			return err
		}
	}
	if err := set("siteCheckDepths", func(raw []byte) (any, bool) {
		var m map[string]float64
		if json.Unmarshal(raw, &m) != nil {
			return nil, false
		}
		_, had := m[name]
		delete(m, name)
		return m, had
	}); err != nil {
		return err
	}
	return set("sitesOff", func(raw []byte) (any, bool) {
		var ids []string
		if json.Unmarshal(raw, &ids) != nil {
			return nil, false
		}
		n := len(ids)
		ids = slices.DeleteFunc(ids, func(s string) bool { return s == name })
		return ids, len(ids) != n
	})
}

// Compact rewrites meta.db and thumbs.db without their free space: a wiped site's rows and thumbnails gone from the
// disk too (db.Compact).
func (l *Library) Compact(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.db.Compact(ctx)
	if l.thumbs != nil {
		err = errors.Join(err, l.thumbs.Compact(ctx))
	}
	return err
}
