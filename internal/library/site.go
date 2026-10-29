package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/tag"
)

// URL kinds in file_urls.
const (
	URLPost   = 1 // the post's page on the site
	URLSource = 2 // the post's source link (often the artist's own post)
	URLFile   = 3 // where the file was downloaded from
)

// MD5Match is what the library knows about an md5.
type MD5Match struct {
	FileID  uint32 // a live file with this md5 (0 = none)
	Deleted bool   // a deleted file had this md5 (tombstone)
}

// FindMD5 checks an md5 before downloading: sites give it up front, so files
// the library has or deleted needn't be fetched.
func (l *Library) FindMD5(ctx context.Context, md5 [16]byte) (MD5Match, error) {
	var m MD5Match
	err := l.db.R.QueryRowContext(ctx,
		"SELECT id FROM files WHERE md5 = ? AND deleted_at IS NULL LIMIT 1", md5[:]).Scan(&m.FileID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return m, fmt.Errorf("library: %w", err)
	}
	var one int
	err = l.db.R.QueryRowContext(ctx, "SELECT 1 FROM tombstones WHERE md5 = ? LIMIT 1", md5[:]).Scan(&one)
	switch {
	case err == nil:
		m.Deleted = true
	case !errors.Is(err, sql.ErrNoRows):
		return m, fmt.Errorf("library: %w", err)
	}
	return m, nil
}

// dayMS is a day in ms: an upload time that's a multiple of it is a day only (midnight UTC; index.postedKey).
const dayMS = 24 * 60 * 60 * 1000

// SitePost is what a site says about a file.
type SitePost struct {
	Tags []string // verbatim site tags
	// Namespaces gives tags their namespace (from the site's tag types). A
	// tag named here takes over the unnamespaced tag of the same name.
	Namespaces map[string]string
	PostURL    string
	Sources    []string
	FileURL    string
	Rating     media.Rating
	// PostedAt is when the post went up on the site (Unix ms; 0 = unknown).
	PostedAt int64
	// PostID: its post on the site - a day-only PostedAt's order within that day until the downloader has recorded the
	// post (read back here before that, it was 0: the file sat at the start of its day until a restart).
	PostID int64
}

// SetSitePost records a site's post for a file: its tags (replacing that
// site's earlier tags), links and rating. Safe to repeat.
func (l *Library) SetSitePost(ctx context.Context, file, site uint32, p SitePost) error {
	if l.isComicOnly(ctx, file) {
		// A comic's page that's a library post too (FindMD5 found it): into the library.
		if err := l.joinLibrary(ctx, file); err != nil {
			return err
		}
	}
	tags := make([]tag.Tag, 0, len(p.Tags))
	for _, t := range p.Tags {
		if t != "" {
			tags = append(tags, tag.Tag{Namespace: p.Namespaces[t], Name: t})
		}
	}
	if err := l.SetSiteTags(ctx, file, site, tags); err != nil {
		return err
	}
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		add := func(kind int, u string) error {
			if u == "" {
				return nil
			}
			var urlID int64
			err := tx.QueryRowContext(ctx, `
				INSERT INTO urls (url) VALUES (?) ON CONFLICT (url) DO UPDATE SET url = url RETURNING id`, u).Scan(&urlID)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO file_urls (file_id, url_id, kind, site_id) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				file, urlID, kind, site)
			return err
		}
		if err := add(URLPost, p.PostURL); err != nil {
			return nil, err
		}
		for _, s := range p.Sources {
			if !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "http://") {
				continue // only links (a site's source text can hold labels and words)
			}
			if err := add(URLSource, s); err != nil {
				return nil, err
			}
		}
		if err := add(URLFile, p.FileURL); err != nil {
			return nil, err
		}
		if p.Rating == media.RatingUnknown && p.PostedAt == 0 {
			return func() error {
				l.ix.AddFileSite(file, site)
				return nil
			}, nil
		}
		if p.Rating != media.RatingUnknown {
			if _, err := tx.ExecContext(ctx, "UPDATE files SET site_rating = ? WHERE id = ?", int(p.Rating), file); err != nil {
				return nil, err
			}
		}
		if p.PostedAt > 0 {
			// The earliest posting wins when several sites have the file - except that a day alone (midnight UTC: a
			// page that says only the day, read again by a tag refresh) leaves a moment on that same day as it is
			// (the moment came from the file when it was downloaded).
			if _, err := tx.ExecContext(ctx, `UPDATE files SET posted_at = CASE
					WHEN ?1 % 86400000 = 0 AND posted_at >= ?1 AND posted_at < ?1 + 86400000 THEN posted_at
					ELSE min(coalesce(posted_at, ?1), ?1) END WHERE id = ?2`, p.PostedAt, file); err != nil {
				return nil, err
			}
		}
		f, _, deleted, err := readFile(ctx, tx, file)
		if err != nil {
			return nil, err
		}
		if f.PostID == 0 && f.PostedAt != 0 && f.PostedAt%dayMS == 0 {
			f.PostID = p.PostID
		}
		return func() error {
			l.ix.AddFileSite(file, site)
			if !deleted {
				l.ix.UpdateFile(f)
			}
			return nil
		}, nil
	})
}

// EnsureNamespaces creates namespaces that don't exist yet (the ones site
// plugins map tag types to), so searches and the web client know them before
// the first tag uses them.
func (l *Library) EnsureNamespaces(ctx context.Context, names []string) error {
	type ns struct {
		id   uint32
		name string
	}
	var added []ns
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		for _, n := range names {
			var id uint32
			err := tx.QueryRowContext(ctx, "INSERT INTO namespaces (name) VALUES (?) ON CONFLICT DO NOTHING RETURNING id", n).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				continue // exists
			}
			if err != nil {
				return nil, err
			}
			added = append(added, ns{id, n})
		}
		return func() error {
			for _, a := range added {
				l.ix.AddNamespace(a.id, a.name)
			}
			return nil
		}, nil
	})
}

// Namespaces lists the library's namespaces by name.
func (l *Library) Namespaces() []string {
	// Not the namespaces only comics use (female:, language: ... from a gallery site):
	// those aren't the library's. (A plugin's own namespaces count before any tag is in them.)
	rows, err := l.db.R.Query(`SELECT n.name FROM namespaces n WHERE n.id != 0 AND NOT (
		EXISTS (SELECT 1 FROM tags t JOIN comic_tags ct ON ct.tag_id = t.id WHERE t.namespace_id = n.id)
		AND NOT EXISTS (SELECT 1 FROM tags t JOIN file_tags ft ON ft.tag_id = t.id WHERE t.namespace_id = n.id)) ORDER BY n.name`)
	if err != nil {
		return l.ix.Namespaces()
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

// SiteNames maps site IDs to their names (plugin IDs).
func (l *Library) SiteNames(ctx context.Context) (map[uint32]string, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT id, name FROM sites")
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := map[uint32]string{}
	for rows.Next() {
		var id uint32
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// FileURL is one of a file's links.
type FileURL struct {
	Kind int
	Site string
	URL  string
}

// FileURLs lists a file's links (post pages, sources, file URLs).
func (l *Library) FileURLs(ctx context.Context, file uint32) ([]FileURL, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT fu.kind, s.name, u.url FROM file_urls fu
		JOIN urls u ON u.id = fu.url_id JOIN sites s ON s.id = fu.site_id
		WHERE fu.file_id = ? ORDER BY fu.kind, s.name, u.url`, file)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var out []FileURL
	for rows.Next() {
		var u FileURL
		if err := rows.Scan(&u.Kind, &u.Site, &u.URL); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetPostedAt sets a file's upload time - a fix (a day-only one given its moment), not a site's word.
func (l *Library) SetPostedAt(ctx context.Context, file uint32, at int64) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if _, err := tx.ExecContext(ctx, "UPDATE files SET posted_at = ? WHERE id = ?", at, file); err != nil {
			return nil, err
		}
		f, _, deleted, err := readFile(ctx, tx, file)
		if err != nil {
			return nil, err
		}
		return func() error {
			if !deleted {
				l.ix.UpdateFile(f)
			}
			return nil
		}, nil
	})
}
