package index

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/usevitrine/vitrine/internal/media"
)

// Load builds the index from meta.db (deleted files left out).
func Load(ctx context.Context, db *sql.DB) (*Index, error) {
	return load(ctx, db, false)
}

// LoadTrash builds the trash's index: the deleted files (not yet removed for good) with their tags, searched and
// sorted like the library - its "modified" time is when each was deleted (the Trash page's order: the last deleted
// first).
func LoadTrash(ctx context.Context, db *sql.DB) (*Index, error) {
	return load(ctx, db, true)
}

func load(ctx context.Context, db *sql.DB, trash bool) (*Index, error) {
	ix := New()
	ix.trash = trash
	ix.mu.Lock()
	defer ix.mu.Unlock()

	// One read transaction, so every query sees the same state.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	defer tx.Rollback()

	steps := []struct {
		name string
		fn   func(context.Context, *sql.Tx) error
	}{
		{"namespaces", ix.loadNamespaces},
		{"tags", ix.loadTags},
		{"files", ix.loadFiles},
		{"file tags", ix.loadFileTags},
		{"relations", ix.loadRelations},
		{"sites", ix.loadSites},
		{"found by", ix.loadFoundBy},
		{"favorites", ix.loadFavorites},
		{"climaxes", ix.loadClimaxes},
	}
	for _, s := range steps {
		if err := s.fn(ctx, tx); err != nil {
			return nil, fmt.Errorf("index: loading %s: %w", s.name, err)
		}
	}
	return ix, nil
}

func (ix *Index) loadNamespaces(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, name FROM namespaces")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint32
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		ix.addNamespace(id, name)
	}
	return rows.Err()
}

func (ix *Index) loadTags(ctx context.Context, tx *sql.Tx) error {
	return ix.loadTagRows(ctx, tx, "SELECT id, namespace_id, name FROM tags")
}

// loadTagRows registers the tags a query lists (id, namespace_id, name), sorted.
func (ix *Index) loadTagRows(ctx context.Context, tx *sql.Tx, query string) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.NamespaceID, &t.Name); err != nil {
			return err
		}
		ix.addTag(t, false)
		ix.sorted = append(ix.sorted, t.ID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ix.sortTags()
	return nil
}

func (ix *Index) loadFiles(ctx context.Context, tx *sql.Tx) error {
	var maxID sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT max(id) FROM files").Scan(&maxID); err != nil {
		return err
	}
	if !maxID.Valid {
		return nil
	}
	ix.growFiles(uint32(maxID.Int64))

	// (flag 64: only a comic page, not in the library; the trash: the deleted files, "modified" = when deleted)
	which, modified := "deleted_at IS NULL", "modified_at"
	if ix.trash {
		which, modified = "deleted_at IS NOT NULL", "deleted_at"
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, size, type, coalesce(width, 0), coalesce(height, 0), coalesce(duration_ms, 0),
		       coalesce(has_audio, 0), imported_at, `+modified+`, coalesce(site_rating, 0), coalesce(posted_at, 0), bulk
		FROM files WHERE `+which+` AND flags & 64 = 0 ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	posts, err := dayOnlyPosts(ctx, tx)
	if err != nil {
		return err
	}
	ids := make([]uint32, 0, maxID.Int64)
	for rows.Next() {
		var f File
		var typ, rating uint8
		if err := rows.Scan(&f.ID, &f.Size, &typ, &f.Width, &f.Height, &f.DurationMS,
			&f.HasAudio, &f.ImportedAt, &f.ModifiedAt, &rating, &f.PostedAt, &f.Bulk); err != nil {
			return err
		}
		f.Type, f.Rating = media.Type(typ), media.Rating(rating)
		f.PostID = posts[f.ID]
		ix.setColumns(f)
		if f.Bulk {
			ix.bulk.Add(f.ID)
		}
		ids = append(ids, f.ID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ix.alive.AddMany(ids)
	return nil
}

// dayOnlyPosts returns the post ID of each file whose upload time is a day only (see postedKey).
func dayOnlyPosts(ctx context.Context, tx *sql.Tx) (map[uint32]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT sp.file_id, max(sp.post_id) FROM seen_posts sp JOIN files f ON f.id = sp.file_id
		WHERE f.posted_at IS NOT NULL AND f.posted_at % ? = 0 GROUP BY sp.file_id`, dayMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint32]int64{}
	for rows.Next() {
		var id uint32
		var post int64
		if err := rows.Scan(&id, &post); err != nil {
			return nil, err
		}
		out[id] = post
	}
	return out, rows.Err()
}

func (ix *Index) loadFileTags(ctx context.Context, tx *sql.Tx) error {
	// Walk links in tag order (the file_tags_tag index), adding each tag's
	// files in one batch. Deleted files and duplicate origins are skipped.
	q := "SELECT tag_id, file_id FROM file_tags ORDER BY tag_id, file_id"
	if ix.trash { // (only the deleted files' links: a small index, quick to build again)
		q = "SELECT tag_id, file_id FROM file_tags WHERE file_id IN (SELECT id FROM files WHERE deleted_at IS NOT NULL) ORDER BY tag_id, file_id"
	}
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	var cur uint32
	var batch []uint32
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := ix.checkTags([]uint32{cur}); err != nil {
			return err
		}
		ix.raw[cur].AddMany(batch)
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		var t, f uint32
		if err := rows.Scan(&t, &f); err != nil {
			return err
		}
		if t != cur {
			if err := flush(); err != nil {
				return err
			}
			cur = t
		}
		if ix.alive.Contains(f) && (len(batch) == 0 || batch[len(batch)-1] != f) {
			batch = append(batch, f)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	rows.Close()

	// Site tags the user removed don't count, unless the user also added the
	// tag themselves.
	hidden, err := tx.QueryContext(ctx, `
		SELECT h.file_id, h.tag_id FROM hidden_tags h
		WHERE NOT EXISTS (SELECT 1 FROM file_tags ft
		                  WHERE ft.file_id = h.file_id AND ft.tag_id = h.tag_id AND ft.origin = 0)`)
	if err != nil {
		return err
	}
	defer hidden.Close()
	for hidden.Next() {
		var f, t uint32
		if err := hidden.Scan(&f, &t); err != nil {
			return err
		}
		if int(t) < len(ix.raw) && ix.raw[t] != nil {
			ix.raw[t].Remove(f)
		}
	}
	if err := hidden.Err(); err != nil {
		return err
	}

	// Per-file tag counts.
	for _, bm := range ix.raw {
		if bm == nil {
			continue
		}
		bm.RunOptimize()
		bm.Iterate(func(f uint32) bool {
			if ix.tagCount[f] < 65535 {
				ix.tagCount[f]++
			}
			return true
		})
	}
	return nil
}

func (ix *Index) loadRelations(ctx context.Context, tx *sql.Tx) error {
	read := func(q string) ([]Relation, error) {
		rows, err := tx.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []Relation
		for rows.Next() {
			var r Relation
			if err := rows.Scan(&r.From, &r.To); err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
	parents, err := read("SELECT child_id, parent_id FROM tag_parents")
	if err != nil {
		return err
	}
	siblings, err := read("SELECT from_id, to_id FROM tag_siblings")
	if err != nil {
		return err
	}
	return ix.setRelations(parents, siblings)
}

func (ix *Index) loadFavorites(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT file_id, added_at FROM favorites")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint32
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			return err
		}
		ix.growFiles(id)
		ix.favAt[id] = at
		ix.favorite.Add(id)
	}
	return rows.Err()
}

func (ix *Index) loadClimaxes(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT file_id, count(*) FROM climaxes GROUP BY file_id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint32
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		ix.growFiles(id)
		ix.climaxes[id] = uint16(min(n, 65535))
	}
	return rows.Err()
}
