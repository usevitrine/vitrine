package library

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/tag"
)

// changeRelations edits the parent/sibling lists. edit gets the current
// lists (by tag ID) and returns the new ones plus the SQL to save the change;
// loops are rejected before anything is written.
func (l *Library) changeRelations(ctx context.Context, tags []tag.Tag, create bool,
	edit func(ids []uint32, parents, siblings []index.Relation) (p, s []index.Relation, save func(*sql.Tx) error, err error)) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		ids, register, err := l.tagIDs(ctx, tx, tags, create)
		if err != nil {
			return nil, err
		}
		switch {
		case len(ids) != len(tags) && !create:
			return nil, nil // a tag doesn't exist: nothing to remove
		case len(ids) != len(tags):
			return nil, fmt.Errorf("library: %w: a tag can't be related to itself", ErrInvalid)
		}
		register() // the index must know new tags to check the relations
		parents, siblings := l.ix.Relations()
		p, s, save, err := edit(ids, parents, siblings)
		if err != nil {
			return nil, err
		}
		if err := l.ix.CheckRelations(p, s); err != nil {
			return nil, fmt.Errorf("library: %w: %w", ErrInvalid, err)
		}
		if err := save(tx); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		return func() error { return l.ix.SetRelations(p, s) }, nil
	})
}

// AddParent makes parent implied by child: searching the parent finds every
// file with the child. Loops are refused.
func (l *Library) AddParent(ctx context.Context, child, parent tag.Tag) error {
	return l.changeRelations(ctx, []tag.Tag{child, parent}, true,
		func(ids []uint32, parents, siblings []index.Relation) ([]index.Relation, []index.Relation, func(*sql.Tx) error, error) {
			r := index.Relation{From: ids[0], To: ids[1]}
			if ids[0] == ids[1] {
				return nil, nil, nil, fmt.Errorf("library: %w: a tag can't be its own parent", ErrInvalid)
			}
			if !slices.Contains(parents, r) {
				parents = append(parents, r)
			}
			return parents, siblings, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx,
					"INSERT INTO tag_parents (child_id, parent_id) VALUES (?, ?) ON CONFLICT DO NOTHING", r.From, r.To)
				return err
			}, nil
		})
}

// RemoveParent undoes AddParent.
func (l *Library) RemoveParent(ctx context.Context, child, parent tag.Tag) error {
	return l.changeRelations(ctx, []tag.Tag{child, parent}, false,
		func(ids []uint32, parents, siblings []index.Relation) ([]index.Relation, []index.Relation, func(*sql.Tx) error, error) {
			r := index.Relation{From: ids[0], To: ids[1]}
			parents = slices.DeleteFunc(parents, func(x index.Relation) bool { return x == r })
			return parents, siblings, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "DELETE FROM tag_parents WHERE child_id = ? AND parent_id = ?", r.From, r.To)
				return err
			}, nil
		})
}

// SetSibling makes from a sibling of the ideal tag to: from is shown and
// searched as to. A tag has at most one ideal; setting another replaces it.
func (l *Library) SetSibling(ctx context.Context, from, to tag.Tag) error {
	return l.changeRelations(ctx, []tag.Tag{from, to}, true,
		func(ids []uint32, parents, siblings []index.Relation) ([]index.Relation, []index.Relation, func(*sql.Tx) error, error) {
			if ids[0] == ids[1] {
				return nil, nil, nil, fmt.Errorf("library: %w: a tag can't be its own sibling", ErrInvalid)
			}
			r := index.Relation{From: ids[0], To: ids[1]}
			siblings = slices.DeleteFunc(siblings, func(x index.Relation) bool { return x.From == r.From })
			siblings = append(siblings, r)
			return parents, siblings, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `
					INSERT INTO tag_siblings (from_id, to_id) VALUES (?, ?)
					ON CONFLICT (from_id) DO UPDATE SET to_id = excluded.to_id`, r.From, r.To)
				return err
			}, nil
		})
}

// RemoveSibling makes from a tag of its own again.
func (l *Library) RemoveSibling(ctx context.Context, from tag.Tag) error {
	return l.changeRelations(ctx, []tag.Tag{from}, false,
		func(ids []uint32, parents, siblings []index.Relation) ([]index.Relation, []index.Relation, func(*sql.Tx) error, error) {
			siblings = slices.DeleteFunc(siblings, func(x index.Relation) bool { return x.From == ids[0] })
			return parents, siblings, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "DELETE FROM tag_siblings WHERE from_id = ?", ids[0])
				return err
			}, nil
		})
}
