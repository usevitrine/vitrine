package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/tag"
)

// OriginUser is the origin of tags the user added by hand. Sites have their
// own IDs (see Site).
const OriginUser = 0

// ParseTag reads a tag typed by the user ("name" or "namespace:name"). The
// prefix is a namespace only if the library has one by that name.
func (l *Library) ParseTag(text string) tag.Tag {
	return tag.Split(tag.Normalize(text), l.ix.IsNamespace)
}

// Site returns the ID of a site (a plugin ID like "safebooru"), creating it.
func (l *Library) Site(ctx context.Context, name string) (uint32, error) {
	if name == "" || name == "user" {
		return 0, fmt.Errorf("library: %w: bad site name %q", ErrInvalid, name)
	}
	var id uint32
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		_, err := tx.ExecContext(ctx, "INSERT INTO sites (name) VALUES (?) ON CONFLICT (name) DO NOTHING", name)
		if err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, "SELECT id FROM sites WHERE name = ?", name).Scan(&id); err != nil {
			return nil, err
		}
		return func() error {
			l.ix.AddSiteAlias(name, id) // system:site=<name>
			return nil
		}, nil
	})
	return id, err
}

// tagIDs finds tags, creating missing ones if create is set (otherwise they
// are skipped). New tags and namespaces are registered with
// the index by the returned function (call it after commit, before using the
// IDs in the index).
func (l *Library) tagIDs(ctx context.Context, tx *sql.Tx, tags []tag.Tag, create bool) ([]uint32, func(), error) {
	return l.lookupTags(ctx, tx, tags, create, false)
}

// notComicOnly leaves out tags only comics have (a WHERE clause on tags).
const notComicOnly = " AND (EXISTS (SELECT 1 FROM file_tags ft WHERE ft.tag_id = tags.id) OR NOT EXISTS (SELECT 1 FROM comic_tags ct WHERE ct.tag_id = tags.id))"

// lookupTags is tagIDs; with move set (a site said what type a tag is), a
// namespaced tag that doesn't exist yet takes over the unnamespaced tag of
// the same name instead of being created: the tag keeps its ID, files,
// parents and siblings, and from then on shows as namespace:name everywhere.
func (l *Library) lookupTags(ctx context.Context, tx *sql.Tx, tags []tag.Tag, create, move bool) ([]uint32, func(), error) {
	ids := make([]uint32, 0, len(tags))
	var newTags []index.Tag
	type ns struct {
		id   uint32
		name string
	}
	var newNS []ns
	nsIDs := map[string]uint32{"": 0}
	st := newTxStmts(ctx, tx) // (the same few statements for every tag)

	for _, t := range tags {
		if t.Name == "" {
			return nil, nil, fmt.Errorf("library: %w: empty tag", ErrInvalid)
		}
		nsID, ok := nsIDs[t.Namespace]
		if !ok {
			err := st.QueryRow("SELECT id FROM namespaces WHERE name = ?", t.Namespace).Scan(&nsID)
			switch {
			case errors.Is(err, sql.ErrNoRows) && !create:
				continue
			case errors.Is(err, sql.ErrNoRows):
				err = st.QueryRow("INSERT INTO namespaces (name) VALUES (?) RETURNING id", t.Namespace).Scan(&nsID)
				newNS = append(newNS, ns{nsID, t.Namespace})
			}
			if err != nil {
				return nil, nil, fmt.Errorf("library: namespace %q: %w", t.Namespace, err)
			}
			nsIDs[t.Namespace] = nsID
		}

		// A tag the user moved to another namespace: there (what a site still calls it lands with it).
		nsID, err := nsOverride(st, nsID, t.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("library: tag %q: %w", t, err)
		}
		var id uint32
		err = st.QueryRow("SELECT id FROM tags WHERE namespace_id = ? AND name = ?", nsID, t.Name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) && nsID == 0 {
			// No plain tag by that name, but one namespaced tag (some_artist ->
			// creator:some_artist, after a site said it's an artist): that's the one.
			// Not counting tags only comics have (a gallery site's other:3d made "3d"
			// look ambiguous: a plain 3d was made, and files moved over to it).
			var n int
			if e := st.QueryRow("SELECT count(*) FROM tags WHERE name = ? AND namespace_id != 0"+notComicOnly,
				t.Name).Scan(&n); e != nil {
				return nil, nil, fmt.Errorf("library: tag %q: %w", t, e)
			}
			if n == 1 {
				err = st.QueryRow("SELECT id FROM tags WHERE name = ? AND namespace_id != 0"+notComicOnly, t.Name).Scan(&id)
			}
		}
		switch {
		case errors.Is(err, sql.ErrNoRows) && !create:
			continue
		case errors.Is(err, sql.ErrNoRows) && move && nsID != 0:
			err = st.QueryRow("UPDATE tags SET namespace_id = ? WHERE namespace_id = 0 AND name = ? RETURNING id",
				nsID, t.Name).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				err = st.QueryRow("INSERT INTO tags (namespace_id, name) VALUES (?, ?) RETURNING id", nsID, t.Name).Scan(&id)
			}
			// Registering an existing ID with another namespace renames it in
			// the index (its files stay).
			newTags = append(newTags, index.Tag{ID: id, NamespaceID: nsID, Name: t.Name})
		case errors.Is(err, sql.ErrNoRows):
			err = st.QueryRow("INSERT INTO tags (namespace_id, name) VALUES (?, ?) RETURNING id", nsID, t.Name).Scan(&id)
			newTags = append(newTags, index.Tag{ID: id, NamespaceID: nsID, Name: t.Name})
		}
		if err != nil {
			return nil, nil, fmt.Errorf("library: tag %q: %w", t, err)
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	register := func() {
		for _, n := range newNS {
			l.ix.AddNamespace(n.id, n.name)
		}
		l.ix.AddTags(newTags...)
	}
	return ids, register, nil
}

// fileTags returns the tags a file has: any origin counts, except site tags
// the user removed (hidden) - unless the user also added them.
func fileTags(ctx context.Context, tx *sql.Tx, file uint32) ([]uint32, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT tag_id FROM file_tags ft
		WHERE file_id = ? AND (origin = 0 OR NOT EXISTS (
			SELECT 1 FROM hidden_tags h WHERE h.file_id = ft.file_id AND h.tag_id = ft.tag_id))`, file)
	if err != nil {
		return nil, err
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

// diff lists what is in b but not a (added) and in a but not b (removed).
func diff(a, b []uint32) (added, removed []uint32) {
	for _, x := range b {
		if !slices.Contains(a, x) {
			added = append(added, x)
		}
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			removed = append(removed, x)
		}
	}
	return added, removed
}

type tagChange struct {
	file           uint32
	added, removed []uint32
	modified       int64
}

// changeTags runs edit for each file inside one transaction, then records
// which files' tags actually changed: their modified time is updated (unless
// touch, asked before the edit, says the change doesn't count - nil: it
// always does) and the index is told.
func (l *Library) changeTags(ctx context.Context, files []uint32, tags []tag.Tag, create, move bool,
	touch func(tx *sql.Tx, file uint32) (bool, error),
	edit func(tx *sql.Tx, file uint32, tagIDs []uint32) error) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		ids, register, err := l.lookupTags(ctx, tx, tags, create, move)
		if err != nil {
			return nil, err
		}
		now := l.now().UnixMilli()
		var changes []tagChange
		for _, f := range files {
			if err := fileExists(ctx, tx, f); err != nil {
				return nil, err
			}
			before, err := fileTags(ctx, tx, f)
			if err != nil {
				return nil, err
			}
			counts := true
			if touch != nil {
				if counts, err = touch(tx, f); err != nil {
					return nil, err
				}
			}
			if err := edit(tx, f, ids); err != nil {
				return nil, err
			}
			after, err := fileTags(ctx, tx, f)
			if err != nil {
				return nil, err
			}
			added, removed := diff(before, after)
			if len(added)+len(removed) == 0 {
				continue
			}
			modified := now
			if counts {
				if _, err := tx.ExecContext(ctx, "UPDATE files SET modified_at = ? WHERE id = ?", now, f); err != nil {
					return nil, err
				}
			} else if err := tx.QueryRowContext(ctx, "SELECT modified_at FROM files WHERE id = ?", f).Scan(&modified); err != nil {
				return nil, err
			}
			changes = append(changes, tagChange{f, added, removed, modified})
		}
		return func() error {
			register()
			for _, c := range changes {
				if err := l.ix.UpdateTags(c.file, c.added, c.removed, c.modified); err != nil {
					return err
				}
			}
			return nil
		}, nil
	})
}

// ErrNoFile means a file ID isn't in the library.
var ErrNoFile = errors.New("no such file")

func fileExists(ctx context.Context, tx *sql.Tx, id uint32) error {
	var one int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM files WHERE id = ?", id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("library: file %d: %w", id, ErrNoFile)
	}
	return err
}

// AddTags gives files tags as the user's own (they survive site re-syncs).
func (l *Library) AddTags(ctx context.Context, files []uint32, tags []tag.Tag) error {
	return l.changeTags(ctx, files, tags, true, false, nil, func(tx *sql.Tx, f uint32, ids []uint32) error {
		st := newTxStmts(ctx, tx)
		for _, t := range ids {
			if _, err := st.Exec(
				"INSERT INTO file_tags (file_id, tag_id, origin) VALUES (?, ?, 0) ON CONFLICT DO NOTHING", f, t); err != nil {
				return err
			}
			if _, err := st.Exec("DELETE FROM hidden_tags WHERE file_id = ? AND tag_id = ?", f, t); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveTags takes tags off files. A tag that came from a site is hidden
// rather than deleted, so re-syncing the site doesn't bring it back.
func (l *Library) RemoveTags(ctx context.Context, files []uint32, tags []tag.Tag) error {
	return l.changeTags(ctx, files, tags, false, false, nil, func(tx *sql.Tx, f uint32, ids []uint32) error {
		st := newTxStmts(ctx, tx)
		for _, t := range ids {
			if _, err := st.Exec(
				"DELETE FROM file_tags WHERE file_id = ? AND tag_id = ? AND origin = 0", f, t); err != nil {
				return err
			}
			if _, err := st.Exec(`
				INSERT INTO hidden_tags (file_id, tag_id)
				SELECT ?1, ?2 WHERE EXISTS (SELECT 1 FROM file_tags WHERE file_id = ?1 AND tag_id = ?2)
				ON CONFLICT DO NOTHING`, f, t); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetSiteTags replaces the tags one site gives a file (a download or a tag
// refresh). User tags and the user's removals are kept. Only a site new to the
// file counts as a change to its modified time: a refresh of a site it already
// has doesn't ("modified recently" filled up with files whose only change was a
// site's retagging).
func (l *Library) SetSiteTags(ctx context.Context, file, site uint32, tags []tag.Tag) error {
	if site == OriginUser {
		return fmt.Errorf("library: SetSiteTags needs a site, not the user")
	}
	isNew := func(tx *sql.Tx, f uint32) (bool, error) {
		var had bool
		err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM file_tags WHERE file_id = ?1 AND origin = ?2)
			    OR EXISTS (SELECT 1 FROM file_urls WHERE file_id = ?1 AND site_id = ?2)`, f, site).Scan(&had)
		return !had, err
	}
	return l.changeTags(ctx, []uint32{file}, tags, true, true, isNew, func(tx *sql.Tx, f uint32, ids []uint32) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM file_tags WHERE file_id = ? AND origin = ?", f, site); err != nil {
			return err
		}
		st := newTxStmts(ctx, tx)
		for _, t := range ids {
			if _, err := st.Exec(
				"INSERT INTO file_tags (file_id, tag_id, origin) VALUES (?, ?, ?)", f, t, site); err != nil {
				return err
			}
		}
		return nil
	})
}
