package library

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/tag"
)

// Comics: a comic (a gallery - doujinshi, manga, an artist's CG set) is its
// ordered pages, each a file in the store (so dedupe and thumbnails work as
// for any file), with a title and tags of its own. Comics live on their own
// page: a file that is only a comic page carries FlagComicOnly and is never
// in the library's grid or searches (it isn't in the search index at all).
// The same file downloaded or imported for the library loses the flag and
// shows there as usual; deleting it from the library while a comic still has
// it only takes it out of the library.

// FlagComicOnly marks a file that is only a comic page (files.flags).
const FlagComicOnly = 1 << 6

// ErrNoComic means there is no comic with that ID.
var ErrNoComic = errors.New("no such comic")

// Comic is a comic as listed.
type Comic struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	TitleAlt  string `json:"title_alt,omitempty"`
	Site      string `json:"site,omitempty"` // plugin ID ("" = imported)
	Gallery   string `json:"gallery,omitempty"`
	URL       string `json:"url,omitempty"`
	Category  string `json:"category,omitempty"`
	Pages     int    `json:"pages"` // how many it has
	Have      int    `json:"have"`  // how many are downloaded
	Cover     uint32 `json:"cover,omitempty"`
	PostedAt  int64  `json:"posted_at,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	ReadPage  int    `json:"read_page"`
	ReadAt    int64  `json:"read_at,omitempty"`
	Favorite  bool   `json:"favorite,omitempty"`
	// SiteTitle is the site's title when the user gave it their own (Title is theirs then).
	SiteTitle string `json:"site_title,omitempty"`
	// Crop is the part of the cover shown (fractions: x, y, width, height); nil = all of it.
	Crop []float64 `json:"crop,omitempty"`
	Tags []string  `json:"tags"` // "namespace:name"
	// Collection is the collection it's in (0: none).
	Collection int64 `json:"collection,omitempty"`
}

// ComicInfo is what a site (or an import) says about a comic.
type ComicInfo struct {
	Title    string
	TitleAlt string
	Site     string // plugin ID ("" = imported)
	Gallery  string // the site's key for it (needed with Site)
	URL      string
	Category string
	Pages    int
	PostedAt int64
	Tags     []tag.Tag
}

// AddComic records a comic, or updates the one from the same site and
// gallery (title, pages, tags - the site's tags replace its earlier ones).
// It returns the comic's ID and whether it is new.
func (l *Library) AddComic(ctx context.Context, info ComicInfo) (int64, bool, error) {
	title := strings.TrimSpace(info.Title)
	if title == "" {
		return 0, false, fmt.Errorf("library: %w: a comic needs a title", ErrInvalid)
	}
	var siteID any // NULL: imported
	var origin uint32
	if info.Site != "" {
		id, err := l.Site(ctx, info.Site)
		if err != nil {
			return 0, false, err
		}
		siteID, origin = id, id
	}
	var id int64
	isNew := false
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		now := l.now().UnixMilli()
		err := sql.ErrNoRows
		if info.Site != "" {
			err = tx.QueryRowContext(ctx, "SELECT id FROM comics WHERE site_id = ? AND gallery = ?", siteID, info.Gallery).Scan(&id)
		}
		switch {
		case errors.Is(err, sql.ErrNoRows):
			isNew = true
			err = tx.QueryRowContext(ctx, `
				INSERT INTO comics (title, title_alt, site_id, gallery, url, category, pages, posted_at, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
				title, nullString(info.TitleAlt), siteID, nullString(info.Gallery), nullString(info.URL), nullString(info.Category),
				info.Pages, nullInt(info.PostedAt), now, now).Scan(&id)
		case err == nil:
			_, err = tx.ExecContext(ctx, `
				UPDATE comics SET title = ?, title_alt = ?, url = coalesce(?, url), category = coalesce(?, category),
					pages = max(pages, ?), posted_at = coalesce(?, posted_at), updated_at = ? WHERE id = ?`,
				title, nullString(info.TitleAlt), nullString(info.URL), nullString(info.Category), info.Pages,
				nullInt(info.PostedAt), now, id)
		}
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		return l.setComicTags(ctx, tx, id, origin, info.Tags)
	})
	return id, isNew, err
}

func nullInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// setComicTags replaces one origin's tags on a comic.
func (l *Library) setComicTags(ctx context.Context, tx *sql.Tx, comic int64, origin uint32, tags []tag.Tag) (func() error, error) {
	ids, register, err := l.tagIDs(ctx, tx, tags, true)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM comic_tags WHERE comic_id = ? AND origin = ?", comic, origin); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	for _, t := range ids {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO comic_tags (comic_id, tag_id, origin) VALUES (?, ?, ?)",
			comic, t, origin); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
	}
	return func() error {
		if register != nil {
			register()
		}
		return nil
	}, nil
}

// SetComicTags replaces the tags the user gave a comic.
func (l *Library) SetComicTags(ctx context.Context, comic int64, tags []tag.Tag) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if err := comicExists(ctx, tx, comic); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE comics SET updated_at = ? WHERE id = ?", l.now().UnixMilli(), comic); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		return l.setComicTags(ctx, tx, comic, 0, tags)
	})
}

func comicExists(ctx context.Context, tx *sql.Tx, comic int64) error {
	var one int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM comics WHERE id = ?", comic).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoComic
	}
	return err
}

// AddComicPage stores one page of a comic (page is 0-based). The file is
// stored like any other, but only as a comic page: a new file doesn't enter
// the library. A file the library deleted (and that's still in its undo
// window) comes back as a comic page only.
func (l *Library) AddComicPage(ctx context.Context, comic int64, page int, r io.Reader) (File, error) {
	if page < 0 {
		return File{}, fmt.Errorf("library: %w: page %d", ErrInvalid, page)
	}
	res, err := l.store.Put(r)
	if err != nil {
		return File{}, err
	}
	f := File{SHA256: res.SHA256, Type: res.Type, Size: res.Size}
	err = l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if err := comicExists(ctx, tx, comic); err != nil {
			return nil, err
		}
		now := l.now().UnixMilli()
		err := tx.QueryRowContext(ctx, `
			INSERT INTO files (sha256, md5, size, type, imported_at, modified_at, bulk, flags)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?)
			ON CONFLICT (sha256) DO NOTHING
			RETURNING id`,
			res.SHA256[:], res.MD5[:], res.Size, int(res.Type), now, now, FlagComicOnly).Scan(&f.ID)
		switch {
		case err == nil:
			f.New = true
		case errors.Is(err, sql.ErrNoRows):
			var deleted bool
			if err := tx.QueryRowContext(ctx, "SELECT id, deleted_at IS NOT NULL FROM files WHERE sha256 = ?", res.SHA256[:]).
				Scan(&f.ID, &deleted); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			if deleted { // (out of the index already: back as a page only)
				if _, err := tx.ExecContext(ctx, "UPDATE files SET deleted_at = NULL, flags = flags | ? WHERE id = ?",
					FlagComicOnly, f.ID); err != nil {
					return nil, fmt.Errorf("library: %w", err)
				}
			}
		default:
			return nil, fmt.Errorf("library: %w", err)
		}
		return nil, placePage(ctx, tx, comic, page, f.ID, "", now)
	})
	if err != nil {
		return f, err
	}
	if f.New {
		l.WakeMedia() // its thumbnail
	}
	return f, nil
}

// joinLibrary brings a file that was only a comic page into the library (it
// was downloaded or imported for the library too).
func (l *Library) joinLibrary(ctx context.Context, file uint32) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		res, err := tx.ExecContext(ctx, "UPDATE files SET flags = flags & ~? WHERE id = ? AND flags & ? != 0 AND deleted_at IS NULL",
			FlagComicOnly, file, FlagComicOnly)
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, nil
		}
		f, _, _, err := readFile(ctx, tx, file)
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		tags, err := fileTags(ctx, tx, file)
		if err != nil {
			return nil, err
		}
		return func() error { return l.ix.AddFile(f, tags) }, nil
	})
}

// isComicOnly says whether a file is only a comic page.
func (l *Library) isComicOnly(ctx context.Context, file uint32) bool {
	var flags int
	l.db.R.QueryRowContext(ctx, "SELECT flags FROM files WHERE id = ?", file).Scan(&flags)
	return flags&FlagComicOnly != 0
}

// ComicQuery picks and orders comics.
type ComicQuery struct {
	// Search: words, each a tag ("female:glasses", or "glasses" in any
	// namespace) or part of the title; "-" in front excludes; "*" matches
	// anything. Empty = all.
	Search string
	// Sort: "added" (newest first, the default), "updated", "read" (last
	// read first), "title", "pages", "posted".
	Sort   string
	Offset int
	Limit  int
}

// Comics lists comics (not deleted ones) and how many match in all.
func (l *Library) Comics(ctx context.Context, q ComicQuery) ([]Comic, int, error) {
	all, err := l.loadComics(ctx, "c.deleted_at IS NULL")
	if err != nil {
		return nil, 0, err
	}
	match := comicMatcher(q.Search)
	out := all[:0]
	for _, c := range all {
		if match(c) {
			out = append(out, c)
		}
	}
	sortComics(out, q.Sort)
	total := len(out)
	from := min(max(q.Offset, 0), total)
	to := total
	if q.Limit > 0 {
		to = min(from+q.Limit, total)
	}
	return out[from:to], total, nil
}

// Comic loads one comic (deleted ones too: undo needs them).
func (l *Library) Comic(ctx context.Context, id int64) (Comic, error) {
	list, err := l.loadComics(ctx, "c.id = ?", id)
	if err != nil {
		return Comic{}, err
	}
	if len(list) == 0 {
		return Comic{}, ErrNoComic
	}
	return list[0], nil
}

func (l *Library) loadComics(ctx context.Context, where string, args ...any) ([]Comic, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT c.id, c.title, coalesce(c.custom_title, ''), coalesce(c.cover_crop, ''), coalesce(c.title_alt, ''), coalesce(s.name, ''), coalesce(c.gallery, ''), coalesce(c.url, ''),
		       coalesce(c.category, ''), c.pages, (SELECT count(*) FROM comic_pages p WHERE p.comic_id = c.id),
		       coalesce(c.cover, 0), coalesce(c.posted_at, 0), c.created_at, c.updated_at, c.read_page, coalesce(c.read_at, 0),
		       EXISTS (SELECT 1 FROM comic_favorites cf WHERE cf.comic_id = c.id), coalesce(c.collection_id, 0)
		FROM comics c LEFT JOIN sites s ON s.id = c.site_id WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	var out []Comic
	byID := map[int64]int{}
	for rows.Next() {
		var c Comic
		var custom, crop string
		if err := rows.Scan(&c.ID, &c.Title, &custom, &crop, &c.TitleAlt, &c.Site, &c.Gallery, &c.URL, &c.Category, &c.Pages, &c.Have,
			&c.Cover, &c.PostedAt, &c.CreatedAt, &c.UpdatedAt, &c.ReadPage, &c.ReadAt, &c.Favorite, &c.Collection); err != nil {
			rows.Close()
			return nil, fmt.Errorf("library: %w", err)
		}
		if custom != "" {
			c.SiteTitle, c.Title = c.Title, custom
		}
		c.Crop = ParseCrop(crop)
		c.Tags = []string{}
		byID[c.ID] = len(out)
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	if len(out) == 0 {
		return out, nil
	}
	trows, err := l.db.R.QueryContext(ctx, `
		SELECT DISTINCT ct.comic_id, n.name, t.name FROM comic_tags ct
		JOIN tags t ON t.id = ct.tag_id JOIN namespaces n ON n.id = t.namespace_id
		JOIN comics c ON c.id = ct.comic_id WHERE `+where+` ORDER BY n.name, t.name`, args...)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer trows.Close()
	for trows.Next() {
		var id int64
		var t tag.Tag
		if err := trows.Scan(&id, &t.Namespace, &t.Name); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if i, ok := byID[id]; ok {
			out[i].Tags = append(out[i].Tags, t.String())
		}
	}
	return out, trows.Err()
}

// comicMatcher reads a comic search (see ComicQuery.Search).
func comicMatcher(search string) func(Comic) bool {
	type term struct {
		neg  bool
		text string
	}
	var terms []term
	for _, w := range strings.Fields(strings.ToLower(search)) {
		t := term{text: w}
		if strings.HasPrefix(w, "-") && len(w) > 1 {
			t = term{neg: true, text: w[1:]}
		}
		terms = append(terms, t)
	}
	matches := func(c Comic, w string) bool {
		if len(w) > 1 && w[0] == '/' {
			// "/words": the title only (a '*' for anything between)
			title := strings.ToLower(c.Title + "\n" + c.SiteTitle + "\n" + c.TitleAlt)
			for _, p := range strings.Split(strings.ReplaceAll(w[1:], "_", " "), "*") {
				i := strings.Index(title, p)
				if i < 0 {
					return false
				}
				title = title[i+len(p):]
			}
			return true
		}
		glob := strings.Contains(w, "*")
		for _, tg := range c.Tags {
			tg = strings.ToLower(tg)
			_, name, _ := strings.Cut(tg, ":")
			if !strings.Contains(tg, ":") {
				name = tg
			}
			if glob {
				if ok, _ := path.Match(w, tg); ok {
					return true
				}
				if ok, _ := path.Match(w, name); ok {
					return true
				}
			} else if tg == w || name == w {
				return true
			}
		}
		plain := strings.ReplaceAll(strings.ReplaceAll(w, "*", ""), "_", " ")
		title := strings.ToLower(c.Title + " " + c.TitleAlt)
		return plain != "" && !strings.Contains(w, ":") && strings.Contains(title, plain)
	}
	return func(c Comic) bool {
		for _, t := range terms {
			if matches(c, t.text) == t.neg {
				return false
			}
		}
		return true
	}
}

func sortComics(list []Comic, by string) {
	slices.SortStableFunc(list, func(a, b Comic) int {
		switch by {
		case "updated":
			return cmp.Or(cmp.Compare(b.UpdatedAt, a.UpdatedAt), cmp.Compare(b.ID, a.ID))
		case "read":
			return cmp.Or(cmp.Compare(b.ReadAt, a.ReadAt), cmp.Compare(b.ID, a.ID))
		case "title":
			return cmp.Or(cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)), cmp.Compare(a.ID, b.ID))
		case "pages":
			return cmp.Or(cmp.Compare(b.Pages, a.Pages), cmp.Compare(b.ID, a.ID))
		case "posted":
			return cmp.Or(cmp.Compare(b.PostedAt, a.PostedAt), cmp.Compare(b.ID, a.ID))
		default: // added
			return cmp.Compare(b.ID, a.ID)
		}
	})
}

// ComicPages lists a comic's page files in order (missing pages - not yet
// downloaded - are left out).
func (l *Library) ComicPages(ctx context.Context, comic int64) ([]uint32, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT p.file_id FROM comic_pages p JOIN files f ON f.id = p.file_id AND f.deleted_at IS NULL
		WHERE p.comic_id = ? ORDER BY p.page`, comic)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var out []uint32
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetComicRead remembers where reading stopped.
func (l *Library) SetComicRead(ctx context.Context, comic int64, page int) error {
	res, err := l.db.W.ExecContext(ctx, "UPDATE comics SET read_page = ?, read_at = ? WHERE id = ?",
		max(page, 0), l.now().UnixMilli(), comic)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoComic
	}
	l.writes.Add(1)
	return nil
}

// ComicIndex is the comics' search index (see index.LoadComics), up to date
// with every write.
func (l *Library) ComicIndex(ctx context.Context) (*index.Index, error) {
	l.comicMu.Lock()
	defer l.comicMu.Unlock()
	at := l.writes.Load()
	if l.comicIx != nil && l.comicAt == at {
		return l.comicIx, nil
	}
	ix, err := index.LoadComics(ctx, l.db.R)
	if err != nil {
		return nil, err
	}
	l.comicIx, l.comicAt = ix, at
	return ix, nil
}

// DeleteComics deletes comics (undo within the window, like files): their
// pages go with them, except files the library or another comic still has.
func (l *Library) DeleteComics(ctx context.Context, ids []int64) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		now := l.now().UnixMilli()
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, "UPDATE comics SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL", now, id); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE files SET deleted_at = ? WHERE deleted_at IS NULL AND flags & ? != 0
					AND id IN (SELECT file_id FROM comic_pages WHERE comic_id = ?)
					AND NOT EXISTS (SELECT 1 FROM comic_pages p JOIN comics c ON c.id = p.comic_id
						WHERE p.file_id = files.id AND c.id != ? AND c.deleted_at IS NULL)`,
				now, FlagComicOnly, id, id); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
		}
		return nil, nil
	})
}

// UndeleteComics brings deleted comics back with their pages.
func (l *Library) UndeleteComics(ctx context.Context, ids []int64) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		for _, id := range ids {
			var deletedAt sql.NullInt64
			err := tx.QueryRowContext(ctx, "SELECT deleted_at FROM comics WHERE id = ?", id).Scan(&deletedAt)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("library: comic %d: %w", id, ErrPurged)
			}
			if err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			if !deletedAt.Valid {
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE comics SET deleted_at = NULL WHERE id = ?", id); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE files SET deleted_at = NULL WHERE deleted_at = ? AND flags & ? != 0
					AND id IN (SELECT file_id FROM comic_pages WHERE comic_id = ?)`,
				deletedAt.Int64, FlagComicOnly, id); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
		}
		return nil, nil
	})
}

// purgeComics removes comics deleted before cutoff (their page files are
// purged with the other deleted files).
func purgeComics(ctx context.Context, tx *sql.Tx, cutoff int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM comics WHERE deleted_at IS NOT NULL AND deleted_at < ?", cutoff); err != nil {
		return err
	}
	return purgeEmptyCollections(ctx, tx)
}

// AddComicTags gives a comic more tags of the user's own.
func (l *Library) AddComicTags(ctx context.Context, comic int64, tags []tag.Tag) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if err := comicExists(ctx, tx, comic); err != nil {
			return nil, err
		}
		ids, register, err := l.tagIDs(ctx, tx, tags, true)
		if err != nil {
			return nil, err
		}
		for _, t := range ids {
			if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO comic_tags (comic_id, tag_id, origin) VALUES (?, ?, 0)", comic, t); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE comics SET updated_at = ? WHERE id = ?", l.now().UnixMilli(), comic); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		return func() error {
			if register != nil {
				register()
			}
			return nil
		}, nil
	})
}

// ComicHas lists which pages of a comic are stored (0-based).
func (l *Library) ComicHas(ctx context.Context, comic int64) (map[int]bool, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT p.page FROM comic_pages p JOIN files f ON f.id = p.file_id AND f.deleted_at IS NULL WHERE p.comic_id = ?`, comic)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		out[p] = true
	}
	return out, rows.Err()
}

// ComicFrom finds the comic from a site's gallery: its ID and link (0, "" = none).
func (l *Library) ComicFrom(ctx context.Context, site, gallery string) (int64, string, error) {
	var id int64
	var u sql.NullString
	err := l.db.R.QueryRowContext(ctx, `SELECT c.id, c.url FROM comics c JOIN sites s ON s.id = c.site_id
		WHERE s.name = ? AND c.gallery = ?`, site, gallery).Scan(&id, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("library: %w", err)
	}
	return id, u.String, nil
}

// TrimComicPages drops a comic's pages from n on (a newer version of the gallery has fewer).
func (l *Library) TrimComicPages(ctx context.Context, comic int64, n int) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM comic_pages WHERE comic_id = ? AND page >= ?", comic, n); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		_, err := tx.ExecContext(ctx, "UPDATE comics SET pages = ? WHERE id = ?", n, comic)
		return nil, err
	})
}

// ComicSiteTag is a comic tag that starts with what's typed, and how many comics have it.
type ComicSiteTag struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ComicSiteTags completes a tag from the comics' tags (never the
// library's): names (or namespace:name) with the text in them, as the
// library's suggestions - starting with it first, then a word starting with it,
// then anywhere (from two characters on), each the most used first. site: only
// tags from that site's galleries (a plugin ID; "" = all).
func (l *Library) ComicSiteTags(ctx context.Context, prefix, site string, limit int) ([]ComicSiteTag, error) {
	prefix = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(prefix)), "-")
	if prefix == "" {
		return []ComicSiteTag{}, nil
	}
	esc := strings.NewReplacer(`!`, `!!`, `%`, `!%`, `_`, `!_`).Replace
	text, nsWhere, nsArgs := prefix, "", []any{}
	if ns, name, ok := strings.Cut(prefix, ":"); ok {
		text, nsWhere, nsArgs = name, "lower(n.name) = ? AND ", []any{ns}
	}
	match := esc(text) + "%"
	if len([]rune(text)) >= 2 {
		match = "%" + match
	}
	// (how well it matched: 0 the start, 1 a word's start, 2 inside one)
	tier := "CASE WHEN lower(t.name) LIKE ? ESCAPE '!' THEN 0 WHEN lower(t.name) LIKE ? ESCAPE '!' OR lower(t.name) LIKE ? ESCAPE '!' THEN 1 ELSE 2 END"
	tierArgs := []any{esc(text) + "%", "%!_" + esc(text) + "%", "%-" + esc(text) + "%"}
	where := "(" + nsWhere + "lower(t.name) LIKE ? ESCAPE '!')"
	args := append(nsArgs, match)
	q := `SELECT n.name, t.name, count(DISTINCT ct.comic_id) AS c, ` + tier + ` AS tier FROM comic_tags ct
		JOIN tags t ON t.id = ct.tag_id JOIN namespaces n ON n.id = t.namespace_id
		JOIN comics cm ON cm.id = ct.comic_id AND cm.deleted_at IS NULL`
	if site != "" {
		q += ` JOIN sites s ON s.id = cm.site_id AND s.name = ?`
		args = append([]any{site}, args...)
	}
	args = append(tierArgs, args...) // (the tier's, first in the query)
	q += ` WHERE ` + where + ` GROUP BY t.id ORDER BY tier, c DESC, t.name LIMIT ?`
	args = append(args, max(1, min(limit, 100)))
	rows, err := l.db.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := []ComicSiteTag{}
	for rows.Next() {
		var t tag.Tag
		var s ComicSiteTag
		var tier int
		if err := rows.Scan(&t.Namespace, &t.Name, &s.Count, &tier); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		s.Tag = t.String()
		out = append(out, s)
	}
	return out, rows.Err()
}

// ComicsByID loads comics (deleted ones too), in the order of ids; unknown IDs are left out.
func (l *Library) ComicsByID(ctx context.Context, ids []int64) ([]Comic, error) {
	if len(ids) == 0 {
		return []Comic{}, nil
	}
	args := make([]any, len(ids))
	marks := make([]string, len(ids))
	for i, id := range ids {
		args[i], marks[i] = id, "?"
	}
	list, err := l.loadComics(ctx, "c.id IN ("+strings.Join(marks, ",")+")", args...)
	if err != nil {
		return nil, err
	}
	at := make(map[int64]int, len(ids))
	for i, id := range ids {
		if _, ok := at[id]; !ok {
			at[id] = i
		}
	}
	slices.SortFunc(list, func(a, b Comic) int { return cmp.Compare(at[a.ID], at[b.ID]) })
	return list, nil
}

// SetComicFavorite makes comics favorites (on) or not; it returns how many changed.
func (l *Library) SetComicFavorite(ctx context.Context, ids []int64, on bool) (int, error) {
	changed := 0
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		now := l.now().UnixMilli()
		for _, id := range ids {
			if err := comicExists(ctx, tx, id); err != nil {
				return nil, err
			}
			var res sql.Result
			var err error
			if on {
				res, err = tx.ExecContext(ctx, "INSERT INTO comic_favorites (comic_id, added_at) VALUES (?, ?) ON CONFLICT DO NOTHING", id, now)
			} else {
				res, err = tx.ExecContext(ctx, "DELETE FROM comic_favorites WHERE comic_id = ?", id)
			}
			if err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			n, _ := res.RowsAffected()
			changed += int(n)
		}
		return nil, nil
	})
	return changed, err
}

// SetComicsRead marks comics read (to the last page) or unread (never opened).
func (l *Library) SetComicsRead(ctx context.Context, ids []int64, read bool) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		for _, id := range ids {
			q := "UPDATE comics SET read_page = 0, read_at = NULL WHERE id = ?"
			args := []any{id}
			if read {
				q = "UPDATE comics SET read_page = max(pages - 1, 0), read_at = ? WHERE id = ?"
				args = []any{l.now().UnixMilli(), id}
			}
			if _, err := tx.ExecContext(ctx, q, args...); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
		}
		return nil, nil
	})
}

// EditComicTags gives comics tags of the user's own (add) and takes tags off
// them (remove: whatever gave them - a site's come back when its tags are read again).
func (l *Library) EditComicTags(ctx context.Context, comics []int64, add, remove []tag.Tag) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		addIDs, register, err := l.tagIDs(ctx, tx, add, true)
		if err != nil {
			return nil, err
		}
		rmIDs, _, err := l.tagIDs(ctx, tx, remove, false)
		if err != nil {
			return nil, err
		}
		now := l.now().UnixMilli()
		for _, c := range comics {
			if err := comicExists(ctx, tx, c); err != nil {
				return nil, err
			}
			for _, t := range addIDs {
				if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO comic_tags (comic_id, tag_id, origin) VALUES (?, ?, 0)", c, t); err != nil {
					return nil, fmt.Errorf("library: %w", err)
				}
			}
			for _, t := range rmIDs {
				if _, err := tx.ExecContext(ctx, "DELETE FROM comic_tags WHERE comic_id = ? AND tag_id = ?", c, t); err != nil {
					return nil, fmt.Errorf("library: %w", err)
				}
			}
			if _, err := tx.ExecContext(ctx, "UPDATE comics SET updated_at = ? WHERE id = ?", now, c); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
		}
		return func() error {
			if register != nil {
				register()
			}
			return nil
		}, nil
	})
}

// ComicTagsOf lists the tags of the selected comics with how many have each.
func (l *Library) ComicTagsOf(ctx context.Context, comics []int64) ([]TagCount, error) {
	if len(comics) == 0 {
		return nil, nil
	}
	list, err := json.Marshal(comics)
	if err != nil {
		return nil, err
	}
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT n.name, t.name, count(DISTINCT ct.comic_id) FROM comic_tags ct
		JOIN tags t ON t.id = ct.tag_id JOIN namespaces n ON n.id = t.namespace_id
		WHERE ct.comic_id IN (SELECT value FROM json_each(?))
		GROUP BY ct.tag_id ORDER BY n.name, t.name`, string(list))
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	return scanTagCounts(rows)
}

// ClearComicPages takes comics' pages off them, to be fetched again (a
// download of the gallery). Page files no comic and not the library still have
// are deleted (a new download of the same picture brings it back).
func (l *Library) ClearComicPages(ctx context.Context, ids []int64) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		now := l.now().UnixMilli()
		for _, id := range ids {
			if err := comicExists(ctx, tx, id); err != nil {
				return nil, err
			}
			rows, err := tx.QueryContext(ctx, "SELECT file_id FROM comic_pages WHERE comic_id = ?", id)
			if err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			var files []uint32
			for rows.Next() {
				var f uint32
				if err := rows.Scan(&f); err != nil {
					rows.Close()
					return nil, fmt.Errorf("library: %w", err)
				}
				files = append(files, f)
			}
			rows.Close()
			if _, err := tx.ExecContext(ctx, "DELETE FROM comic_pages WHERE comic_id = ?", id); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			for _, f := range files {
				if _, err := tx.ExecContext(ctx, `
					UPDATE files SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL AND flags & ? != 0
						AND NOT EXISTS (SELECT 1 FROM comic_pages WHERE file_id = files.id)`, now, f, FlagComicOnly); err != nil {
					return nil, fmt.Errorf("library: %w", err)
				}
			}
			if _, err := tx.ExecContext(ctx, "UPDATE comics SET cover = NULL, read_page = 0, updated_at = ? WHERE id = ?", now, id); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
		}
		return nil, nil
	})
}

// ComicTags lists a comic's tags with where each came from ("user" and/or
// site names), sorted by name - like FileTags.
func (l *Library) ComicTags(ctx context.Context, comic int64) ([]FileTag, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT n.name, t.name, s.name FROM comic_tags ct
		JOIN tags t ON t.id = ct.tag_id
		JOIN namespaces n ON n.id = t.namespace_id
		JOIN sites s ON s.id = ct.origin
		WHERE ct.comic_id = ?
		ORDER BY n.name, t.name, ct.origin`, comic)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	out, err := scanFileTags(rows)
	if out == nil && err == nil {
		out = []FileTag{} // (none: an empty list, not null)
	}
	return out, err
}

// SetComicTitle gives a comic the user's own title ("" = the site's again). The
// site's title stays as it was (a refresh updates it, never this one).
func (l *Library) SetComicTitle(ctx context.Context, comic int64, title string) error {
	title = strings.Join(strings.Fields(title), " ")
	if r := []rune(title); len(r) > 500 {
		title = string(r[:500])
	}
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if err := comicExists(ctx, tx, comic); err != nil {
			return nil, err
		}
		_, err := tx.ExecContext(ctx, "UPDATE comics SET custom_title = ? WHERE id = ?", nullString(title), comic)
		return nil, err
	})
}

// SetComicCrop shows only part of a comic's cover: x, y, width, height as
// fractions of it (nil = all of it).
func (l *Library) SetComicCrop(ctx context.Context, comic int64, crop []float64) error {
	text := ""
	if crop != nil {
		if len(crop) != 4 {
			return fmt.Errorf("library: %w: a crop is x, y, width, height", ErrInvalid)
		}
		x, y, w, h := crop[0], crop[1], crop[2], crop[3]
		if x < 0 || y < 0 || w <= 0.02 || h <= 0.02 || x+w > 1.0001 || y+h > 1.0001 {
			return fmt.Errorf("library: %w: the crop must be inside the cover", ErrInvalid)
		}
		text = fmt.Sprintf("%.4f,%.4f,%.4f,%.4f", x, y, w, h)
	}
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if err := comicExists(ctx, tx, comic); err != nil {
			return nil, err
		}
		_, err := tx.ExecContext(ctx, "UPDATE comics SET cover_crop = ? WHERE id = ?", nullString(text), comic)
		return nil, err
	})
}

// ParseCrop reads a cover crop ("x,y,w,h"; nil if there's none or it's broken).
func ParseCrop(text string) []float64 {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, ",")
	if len(parts) != 4 {
		return nil
	}
	out := make([]float64, 4)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil
		}
		out[i] = v
	}
	return out
}
