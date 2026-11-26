package index

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/RoaringBitmap/roaring/v2"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/search"
)

// The comics index: the same structure as the library's, over comics instead
// of files (a comic's ID is its "file" ID), so the Comics page searches,
// sorts and pages through results exactly like the library. Its tags are the
// comics' own (never the library's); parents and siblings are shared. A
// comic's columns: size = its pages' bytes, width/height = its cover's,
// imported = added, modified = pages or tags last changed, posted = posted on
// its site; and its own: pages, read state, last read, title order.
//
// It's small (comics, not files) and built whole from meta.db: the library
// builds it again after comics change (LoadComics), instead of updating it.

type comicColumns struct {
	pages    []uint32
	readAt   []int64
	title    []uint32          // the comic's place in title order
	titles   map[uint32]string // lower-cased title and original-language title
	started  *roaring.Bitmap
	finished *roaring.Bitmap
}

func (c *comicColumns) readState(id uint32) int64 {
	switch {
	case c.finished.Contains(id):
		return search.ReadDone
	case c.started.Contains(id):
		return search.ReadReading
	}
	return search.ReadUnread
}

func (c *comicColumns) sortKey(f search.Field) func(uint32) int64 {
	switch f {
	case search.FieldPages:
		return func(id uint32) int64 { return int64(c.pages[id]) }
	case search.FieldRead:
		return func(id uint32) int64 { return c.readAt[id] }
	}
	return func(id uint32) int64 { return int64(c.title[id]) }
}

// titleBitmap finds the comics whose title has the words of text (a tag-like
// "blue_sky" is "blue sky"; a '*' stands for anything between): a word that
// isn't a comic tag searches titles, and so does "/words" (titles only).
func (c *comicColumns) titleBitmap(text string) *roaring.Bitmap {
	out := roaring.New()
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(text, "_", " ")), "*")
	for id, t := range c.titles {
		if containsInOrder(t, parts) {
			out.Add(id)
		}
	}
	return out
}

// containsInOrder reports whether s has each of parts, one after another.
func containsInOrder(s string, parts []string) bool {
	for _, p := range parts {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return true
}

// LoadComics builds the comics index from meta.db (deleted comics left out).
func LoadComics(ctx context.Context, db *sql.DB) (*Index, error) {
	return loadComics(ctx, db, false)
}

// LoadComicsTrash builds the comics trash's index: the deleted comics (not yet removed for good), searched and sorted
// like the Comics page - "updated" is when each was deleted.
func LoadComicsTrash(ctx context.Context, db *sql.DB) (*Index, error) {
	return loadComics(ctx, db, true)
}

func loadComics(ctx context.Context, db *sql.DB, trash bool) (*Index, error) {
	ix := New()
	ix.trash = trash
	ix.mu.Lock()
	defer ix.mu.Unlock()
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
		{"tags", ix.loadComicTagNames},
		{"comics", ix.loadComics},
		{"comic tags", ix.loadComicTags},
		{"relations", ix.loadRelations},
		{"sites", ix.loadComicSites},
		{"favorites", ix.loadComicFavorites},
	}
	for _, s := range steps {
		if err := s.fn(ctx, tx); err != nil {
			return nil, fmt.Errorf("index: loading %s: %w", s.name, err)
		}
	}
	return ix, nil
}

// loadComicTagNames registers the tags comics have, and the ones in parent
// and sibling links (not the library's other tags).
func (ix *Index) loadComicTagNames(ctx context.Context, tx *sql.Tx) error {
	return ix.loadTagRows(ctx, tx, `SELECT id, namespace_id, name FROM tags WHERE id IN (
		SELECT tag_id FROM comic_tags UNION SELECT child_id FROM tag_parents UNION SELECT parent_id FROM tag_parents
		UNION SELECT from_id FROM tag_siblings UNION SELECT to_id FROM tag_siblings)`)
}

func (ix *Index) loadComics(ctx context.Context, tx *sql.Tx) error {
	var maxID sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT max(id) FROM comics").Scan(&maxID); err != nil {
		return err
	}
	c := &comicColumns{started: roaring.New(), finished: roaring.New(), titles: map[uint32]string{}}
	ix.comic = c
	if !maxID.Valid {
		return nil
	}
	n := int(maxID.Int64) + 1
	ix.growFiles(uint32(maxID.Int64))
	c.pages, c.readAt, c.title = make([]uint32, n), make([]int64, n), make([]uint32, n)
	rows, err := tx.QueryContext(ctx, `
		SELECT c.id, c.title, coalesce(c.custom_title, ''), coalesce(c.cover_crop, ''), coalesce(c.title_alt, ''), c.pages,
		       coalesce(f.width, 0), coalesce(f.height, 0), c.created_at, `+ix.comicsWhen()+`,
		       coalesce(c.posted_at, 0), c.read_page, coalesce(c.read_at, 0),
		       (SELECT coalesce(sum(pf.size), 0) FROM comic_pages p JOIN files pf ON pf.id = p.file_id WHERE p.comic_id = c.id)
		FROM comics c LEFT JOIN files f ON f.id = c.cover WHERE c.`+ix.comicsWhich()+` ORDER BY c.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type titled struct {
		id    uint32
		title string
	}
	var ids []uint32
	var titles []titled
	for rows.Next() {
		var f File
		var title, custom, crop, alt string
		var pages, readPage uint32
		var readAt int64
		if err := rows.Scan(&f.ID, &title, &custom, &crop, &alt, &pages, &f.Width, &f.Height, &f.ImportedAt, &f.ModifiedAt, &f.PostedAt,
			&readPage, &readAt, &f.Size); err != nil {
			return err
		}
		f.Type = media.JPEG
		if cr := cropOf(crop); cr != nil && f.Width > 0 && f.Height > 0 { // (the tile shows the crop: its shape)
			f.Width, f.Height = uint32(float64(f.Width)*cr[2]+0.5), uint32(float64(f.Height)*cr[3]+0.5)
		}
		ix.setColumns(f)
		c.pages[f.ID], c.readAt[f.ID] = pages, readAt
		// Left on the cover: as if never opened (but a comic of one page is read there).
		done := readAt > 0 && pages > 0 && readPage+1 >= pages && (readPage > 0 || pages == 1)
		if done || (readAt > 0 && readPage > 0) {
			c.started.Add(f.ID)
			if done {
				c.finished.Add(f.ID)
			}
		}
		c.titles[f.ID] = strings.ToLower(custom + "\n" + title + "\n" + alt)
		ids = append(ids, f.ID)
		shown := title
		if custom != "" {
			shown = custom
		}
		titles = append(titles, titled{f.ID, strings.ToLower(shown)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ix.alive.AddMany(ids)
	slices.SortFunc(titles, func(a, b titled) int { return cmp.Or(strings.Compare(a.title, b.title), cmp.Compare(a.id, b.id)) })
	for i, t := range titles {
		c.title[t.id] = uint32(i)
	}
	return nil
}

func (ix *Index) loadComicTags(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT tag_id, comic_id FROM comic_tags ORDER BY tag_id, comic_id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var t, id uint32
		if err := rows.Scan(&t, &id); err != nil {
			return err
		}
		if !ix.alive.Contains(id) {
			continue
		}
		if err := ix.checkTags([]uint32{t}); err != nil {
			return err
		}
		ix.raw[t].Add(id)
		if ix.tagCount[id] < 65535 {
			ix.tagCount[id]++
		}
	}
	return rows.Err()
}

func (ix *Index) loadComicSites(ctx context.Context, tx *sql.Tx) error {
	if err := ix.loadSiteAliases(ctx, tx); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id, site_id FROM comics WHERE site_id IS NOT NULL AND "+ix.comicsWhich())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, site uint32
		if err := rows.Scan(&id, &site); err != nil {
			return err
		}
		ix.addFileSite(id, site)
	}
	return rows.Err()
}

func (ix *Index) loadComicFavorites(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT comic_id, added_at FROM comic_favorites")
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

// UsedNamespaces lists the namespaces the index's tags are in (the comics'
// own: female, language, ...), sorted.
func (ix *Index) UsedNamespaces() []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	seen := map[uint32]bool{}
	for id, e := range ix.tags {
		if e.known && e.ns != 0 && ix.raw[id] != nil && !ix.raw[id].IsEmpty() {
			seen[e.ns] = true
		}
	}
	out := []string{}
	for ns := range seen {
		out = append(out, ix.nsNames[ns])
	}
	slices.Sort(out)
	return out
}

// cropOf reads a cover crop ("x,y,w,h" fractions; nil if none).
func cropOf(text string) []float64 {
	parts := strings.Split(text, ",")
	if len(parts) != 4 {
		return nil
	}
	out := make([]float64, 4)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || v < 0 || v > 1 {
			return nil
		}
		out[i] = v
	}
	if out[2] <= 0 || out[3] <= 0 {
		return nil
	}
	return out
}

// PinReading puts the comics being read (opened, not finished) first, the
// last read first; the rest keep their order. ("Continue reading" is the
// start of the list.)
func (ix *Index) PinReading(ids []uint32) []uint32 {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	c := ix.comic
	if c == nil {
		return ids
	}
	var reading, rest []uint32
	for _, id := range ids {
		if c.readState(id) == search.ReadReading {
			reading = append(reading, id)
		} else {
			rest = append(rest, id)
		}
	}
	if len(reading) == 0 {
		return ids
	}
	slices.SortStableFunc(reading, func(a, b uint32) int { return cmp.Compare(c.readAt[b], c.readAt[a]) })
	return append(reading, rest...)
}

// comicsWhich picks the comics this index holds: the live ones, or (the trash) the deleted ones.
func (ix *Index) comicsWhich() string {
	if ix.trash {
		return "deleted_at IS NOT NULL"
	}
	return "deleted_at IS NULL"
}

// comicsWhen is the "updated" time: when last changed, or (the trash) when deleted.
func (ix *Index) comicsWhen() string {
	if ix.trash {
		return "c.deleted_at"
	}
	return "c.updated_at"
}
