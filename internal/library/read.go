package library

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/tag"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

// FileRow is a file's stored details.
type FileRow struct {
	ID          uint32
	SHA256      store.Hash
	MD5         [16]byte
	Type        media.Type
	Size        int64
	Width       int
	Height      int
	DurationMS  int64
	HasAudio    bool
	ImportedAt  int64
	ModifiedAt  int64
	PostedAt    int64 // when posted on its site; 0 = unknown
	Deleted     bool
	Favorite    bool
	Climaxes    int // times counted with the "I came" button
	Flags       int // FlagProbed, FlagThumbnail, FlagMediaError
	Placeholder []byte
	Path        string // the original in the store
}

// Files loads details for file IDs, in the given order; unknown IDs are
// skipped.
func (l *Library) Files(ctx context.Context, ids []uint32) ([]FileRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	byID := make(map[uint32]FileRow, len(ids))
	// Batches keep the statement under SQLite's parameter limit.
	for chunk := range slices.Chunk(ids, 500) {
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := l.db.R.QueryContext(ctx, `
			SELECT id, sha256, md5, type, size, coalesce(width, 0), coalesce(height, 0), coalesce(duration_ms, 0),
			       coalesce(has_audio, 0), imported_at, modified_at, deleted_at IS NOT NULL, flags, placeholder,
			       coalesce(posted_at, 0)
			FROM files WHERE id IN (?`+strings.Repeat(",?", len(chunk)-1)+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		for rows.Next() {
			var f FileRow
			var sha, md5 []byte
			var typ uint8
			if err := rows.Scan(&f.ID, &sha, &md5, &typ, &f.Size, &f.Width, &f.Height, &f.DurationMS,
				&f.HasAudio, &f.ImportedAt, &f.ModifiedAt, &f.Deleted, &f.Flags, &f.Placeholder, &f.PostedAt); err != nil {
				rows.Close()
				return nil, fmt.Errorf("library: %w", err)
			}
			copy(f.SHA256[:], sha)
			copy(f.MD5[:], md5)
			f.Type = media.Type(typ)
			f.Path = l.store.Path(f.SHA256, f.Type)
			f.Favorite = l.ix.IsFavorite(f.ID)
			f.Climaxes = l.ix.Climaxes(f.ID)
			byID[f.ID] = f
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
	}
	out := make([]FileRow, 0, len(ids))
	for _, id := range ids {
		if f, ok := byID[id]; ok {
			out = append(out, f)
		}
	}
	return out, nil
}

// FileTag is one of a file's tags with where it came from.
type FileTag struct {
	Tag     string   // display form, "namespace:name" or "name"
	Origins []string // "user" and/or site names
}

// FileTags lists a file's tags (hidden site tags left out), sorted by name.
func (l *Library) FileTags(ctx context.Context, id uint32) ([]FileTag, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT n.name, t.name, s.name FROM file_tags ft
		JOIN tags t ON t.id = ft.tag_id
		JOIN namespaces n ON n.id = t.namespace_id
		JOIN sites s ON s.id = ft.origin
		WHERE ft.file_id = ? AND (ft.origin = 0 OR NOT EXISTS (
			SELECT 1 FROM hidden_tags h WHERE h.file_id = ft.file_id AND h.tag_id = ft.tag_id))
		ORDER BY n.name, t.name, ft.origin`, id)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	return scanFileTags(rows)
}

// scanFileTags reads rows of (namespace, tag, origin) sorted by tag into each tag with where it came from.
func scanFileTags(rows *sql.Rows) ([]FileTag, error) {
	defer rows.Close()
	var out []FileTag
	for rows.Next() {
		var ns, name, origin string
		if err := rows.Scan(&ns, &name, &origin); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		display := tag.Tag{Namespace: ns, Name: name}.String()
		if n := len(out); n > 0 && out[n-1].Tag == display {
			out[n-1].Origins = append(out[n-1].Origins, origin)
			continue
		}
		out = append(out, FileTag{Tag: display, Origins: []string{origin}})
	}
	return out, rows.Err()
}

// TagCount is a tag and how many of a set of files have it.
type TagCount struct {
	Tag   string `json:"tag"` // display form, "namespace:name" or "name"
	Count int    `json:"count"`
}

// TagsOf lists the tags of a set of files (bulk editing), each with how many
// of them have it (hidden site tags left out), by namespace and name.
func (l *Library) TagsOf(ctx context.Context, ids []uint32) ([]TagCount, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT n.name, t.name, count(DISTINCT ft.file_id) FROM file_tags ft
		JOIN tags t ON t.id = ft.tag_id
		JOIN namespaces n ON n.id = t.namespace_id
		WHERE ft.file_id IN (SELECT value FROM json_each(?)) AND (ft.origin = 0 OR NOT EXISTS (
			SELECT 1 FROM hidden_tags h WHERE h.file_id = ft.file_id AND h.tag_id = ft.tag_id))
		GROUP BY ft.tag_id
		ORDER BY n.name, t.name`, string(list))
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	return scanTagCounts(rows)
}

// scanTagCounts reads rows of (namespace, tag, count).
func scanTagCounts(rows *sql.Rows) ([]TagCount, error) {
	defer rows.Close()
	var out []TagCount
	for rows.Next() {
		var ns, name string
		var n int
		if err := rows.Scan(&ns, &name, &n); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		out = append(out, TagCount{Tag: tag.Tag{Namespace: ns, Name: name}.String(), Count: n})
	}
	return out, rows.Err()
}

// FileBySHA finds a file the store has by its SHA-256 - a deleted one too (in the trash until removed for good).
func (l *Library) FileBySHA(ctx context.Context, h store.Hash) (FileRow, error) {
	var id uint32
	err := l.db.R.QueryRowContext(ctx, "SELECT id FROM files WHERE sha256 = ? ORDER BY deleted_at IS NOT NULL LIMIT 1", h[:]).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return FileRow{}, ErrNoFile
	}
	if err != nil {
		return FileRow{}, fmt.Errorf("library: %w", err)
	}
	rows, err := l.Files(ctx, []uint32{id})
	if err != nil || len(rows) == 0 {
		return FileRow{}, cmp.Or(err, ErrNoFile)
	}
	return rows[0], nil
}

// Thumb returns a file's thumbnail.
func (l *Library) Thumb(ctx context.Context, id uint32) (thumbs.Thumb, error) {
	if l.thumbs == nil {
		return thumbs.Thumb{}, thumbs.ErrNotFound
	}
	return l.thumbs.Get(ctx, id)
}

// Relation is a parent (child -> parent) or sibling (tag -> ideal) pair by
// display name.
type Relation struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Relations lists all parent and sibling relations by name.
func (l *Library) Relations() (parents, siblings []Relation) {
	p, s := l.ix.RelationNames()
	for _, r := range p {
		parents = append(parents, Relation{r[0], r[1]})
	}
	for _, r := range s {
		siblings = append(siblings, Relation{r[0], r[1]})
	}
	return parents, siblings
}

// OpenOriginal opens a file's original for reading.
func (l *Library) OpenOriginal(f FileRow) (*os.File, error) {
	return l.store.OpenFile(f.SHA256, f.Type)
}

// Tools are the external programs the library uses (ffmpeg, libvips).
func (l *Library) Tools() media.Tools { return l.tools }

// FileSource is a search or subscription that met a file ("Found by").
type FileSource struct {
	Site  string `json:"site"`
	Query string `json:"query"`
	// SubscriptionID is the subscription for this site and search, if there is
	// one now (whether or not a check of it met the file).
	SubscriptionID int64 `json:"subscription_id,omitempty"`
	Downloaded     bool  `json:"downloaded"` // this search downloaded it (else it was there already)
	FirstAt        int64 `json:"first_at"`
}

// FileSources lists what met a file, the first first.
func (l *Library) FileSources(ctx context.Context, id uint32) ([]FileSource, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT si.name, fs.query,
			coalesce(fs.subscription_id, (SELECT s.id FROM subscriptions s WHERE s.site_id = fs.site_id AND s.query = fs.query), 0),
			fs.downloaded, fs.first_at
		FROM file_sources fs JOIN sites si ON si.id = fs.site_id
		WHERE fs.file_id = ? ORDER BY fs.first_at, fs.query`, id)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var out []FileSource
	for rows.Next() {
		var s FileSource
		if err := rows.Scan(&s.Site, &s.Query, &s.SubscriptionID, &s.Downloaded, &s.FirstAt); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SiteCount is how many files got a tag from a site.
type SiteCount struct {
	Site  string `json:"site"`
	Count int    `json:"count"`
}

// TagSites says which sites gave each tag (live files only), most files
// first - a new download from tags picks its site from them. A tag not in the
// library, or only added by the user, has none.
func (l *Library) TagSites(ctx context.Context, texts []string) (map[string][]SiteCount, error) {
	out := make(map[string][]SiteCount, len(texts))
	for _, text := range texts {
		t := l.ParseTag(text)
		out[text] = []SiteCount{}
		if t.Name == "" {
			continue
		}
		var id uint32
		err := l.db.R.QueryRowContext(ctx, `
			SELECT t.id FROM tags t JOIN namespaces n ON n.id = t.namespace_id
			WHERE n.name = ? AND t.name = ?`, t.Namespace, t.Name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) && t.Namespace == "" {
			// No plain tag by that name, but one namespaced one (some_artist -> creator:some_artist).
			err = l.db.R.QueryRowContext(ctx, `
				SELECT min(id) FROM tags WHERE name = ? AND namespace_id != 0 HAVING count(*) = 1`, t.Name).Scan(&id)
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		rows, err := l.db.R.QueryContext(ctx, `
			SELECT s.name, count(DISTINCT ft.file_id) FROM file_tags ft
			JOIN sites s ON s.id = ft.origin
			JOIN files f ON f.id = ft.file_id AND f.deleted_at IS NULL
			WHERE ft.tag_id = ? AND ft.origin != 0
			GROUP BY ft.origin
			ORDER BY 2 DESC, 1`, id)
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		for rows.Next() {
			var c SiteCount
			if err := rows.Scan(&c.Site, &c.Count); err != nil {
				rows.Close()
				return nil, fmt.Errorf("library: %w", err)
			}
			out[text] = append(out[text], c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
	}
	return out, nil
}
