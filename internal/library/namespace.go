package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/tag"
)

// ErrBadNamespace: a namespace name must be lower-case letters, digits and _.
var ErrBadNamespace = errors.New("a namespace is lower-case letters, digits and _")

var namespaceName = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)

// SetNamespace moves a tag to another namespace ("" = general): on every file
// that has it, from a site or the user. No tag of that name there yet: it
// changes in place (it keeps its files, parents and siblings). One there
// already: they become one (not when the moved tag has parents or siblings:
// those are removed or made siblings first). Sites calling it by the old
// namespace go on giving the new one (ns_overrides). Returns the tag as it is
// now and how many files have it.
func (l *Library) SetNamespace(ctx context.Context, from tag.Tag, ns string) (tag.Tag, int, error) {
	if ns != "" && !namespaceName.MatchString(ns) {
		return tag.Tag{}, 0, fmt.Errorf("library: %w", ErrBadNamespace)
	}
	to := tag.Tag{Namespace: ns, Name: from.Name}
	if from == to {
		return to, 0, nil
	}
	var files int
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		var id, oldNS uint32
		err := tx.QueryRowContext(ctx, `SELECT t.id, t.namespace_id FROM tags t JOIN namespaces n ON n.id = t.namespace_id
			WHERE n.name = ? AND t.name = ?`, from.Namespace, from.Name).Scan(&id, &oldNS)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoTag
		}
		if err != nil {
			return nil, err
		}
		// The namespace (made if new).
		var nsID uint32
		var newNS bool
		err = tx.QueryRowContext(ctx, "SELECT id FROM namespaces WHERE name = ?", ns).Scan(&nsID)
		if errors.Is(err, sql.ErrNoRows) {
			err = tx.QueryRowContext(ctx, "INSERT INTO namespaces (name) VALUES (?) RETURNING id", ns).Scan(&nsID)
			newNS = true
		}
		if err != nil {
			return nil, err
		}
		// From now on, what sites call by the old namespace (or called so before
		// an earlier move) lands here.
		for _, q := range []string{
			"UPDATE ns_overrides SET to_ns = ?3 WHERE name = ?1 AND to_ns = ?2",
			"INSERT INTO ns_overrides (from_ns, name, to_ns) VALUES (?2, ?1, ?3) ON CONFLICT DO UPDATE SET to_ns = excluded.to_ns",
			"DELETE FROM ns_overrides WHERE name = ?1 AND from_ns = to_ns",
		} {
			if _, err := tx.ExecContext(ctx, q, from.Name, oldNS, nsID); err != nil {
				return nil, err
			}
		}

		var target uint32
		err = tx.QueryRowContext(ctx, "SELECT id FROM tags WHERE namespace_id = ? AND name = ?", nsID, from.Name).Scan(&target)
		if errors.Is(err, sql.ErrNoRows) {
			// In place: the same tag, another namespace.
			if _, err := tx.ExecContext(ctx, "UPDATE tags SET namespace_id = ? WHERE id = ?", nsID, id); err != nil {
				return nil, err
			}
			if err := tx.QueryRowContext(ctx, "SELECT count(DISTINCT file_id) FROM file_tags WHERE tag_id = ?", id).Scan(&files); err != nil {
				return nil, err
			}
			return func() error {
				if newNS {
					l.ix.AddNamespace(nsID, ns)
				}
				l.ix.AddTags(index.Tag{ID: id, NamespaceID: nsID, Name: from.Name}) // renames it (its files stay)
				return nil
			}, nil
		}
		if err != nil {
			return nil, err
		}

		// Into the tag that's there: its files and hidden marks move over.
		var rel int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM tag_parents WHERE child_id = ?1 OR parent_id = ?1) +
			(SELECT count(*) FROM tag_siblings WHERE from_id = ?1 OR to_id = ?1)`, id).Scan(&rel); err != nil {
			return nil, err
		}
		if rel > 0 {
			return nil, ErrHasRelations
		}
		rows, err := tx.QueryContext(ctx, "SELECT DISTINCT file_id FROM file_tags WHERE tag_id = ?", id)
		if err != nil {
			return nil, err
		}
		var affected []uint32
		for rows.Next() {
			var f uint32
			if err := rows.Scan(&f); err != nil {
				rows.Close()
				return nil, err
			}
			affected = append(affected, f)
		}
		rows.Close()
		befores := make([][]uint32, len(affected))
		for i, f := range affected {
			if befores[i], err = fileTags(ctx, tx, f); err != nil {
				return nil, err
			}
		}
		for _, q := range []string{
			"UPDATE OR IGNORE file_tags SET tag_id = ?1 WHERE tag_id = ?2",
			"UPDATE OR IGNORE hidden_tags SET tag_id = ?1 WHERE tag_id = ?2",
			"DELETE FROM tags WHERE id = ?2", // what couldn't move (already there) goes with it
		} {
			if _, err := tx.ExecContext(ctx, q, target, id); err != nil {
				return nil, err
			}
		}
		var changes []tagChange // (modified unchanged: a namespace move isn't an edit of the file's tags)
		for i, f := range affected {
			after, err := fileTags(ctx, tx, f)
			if err != nil {
				return nil, err
			}
			if added, removed := diff(befores[i], after); len(added)+len(removed) > 0 {
				c := tagChange{file: f, added: added, removed: removed}
				if err := tx.QueryRowContext(ctx, "SELECT modified_at FROM files WHERE id = ?", f).Scan(&c.modified); err != nil {
					return nil, err
				}
				changes = append(changes, c)
			}
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(DISTINCT file_id) FROM file_tags WHERE tag_id = ?", target).Scan(&files); err != nil {
			return nil, err
		}
		return func() error {
			if newNS {
				l.ix.AddNamespace(nsID, ns)
			}
			for _, c := range changes {
				if err := l.ix.UpdateTags(c.file, c.added, c.removed, c.modified); err != nil {
					return err
				}
			}
			return nil
		}, nil
	})
	if err != nil {
		return tag.Tag{}, 0, err
	}
	return to, files, nil
}

// nsOverride is where a tag a site calls namespace:name goes, if the user moved
// it (else the same namespace).
func nsOverride(ctx context.Context, tx *sql.Tx, nsID uint32, name string) (uint32, error) {
	var to uint32
	err := tx.QueryRowContext(ctx, "SELECT to_ns FROM ns_overrides WHERE from_ns = ? AND name = ?", nsID, name).Scan(&to)
	if errors.Is(err, sql.ErrNoRows) {
		return nsID, nil
	}
	return to, err
}
