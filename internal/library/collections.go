package library

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Collections: comics that belong together (a series), named by the user. A comic is in one at most; the Comics
// page shows a collection as one stacked tile (its comics fold into it) that opens to its comics.

// Collection is one, with how many comics it has and how many of them are read to the end.
type Collection struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
	Read  int    `json:"read"`
	// Cover is the comic whose cover it shows: its first by title.
	Cover int64 `json:"cover"`
	// Covers are the comics whose covers make up its tile: its first four by title.
	Covers []int64 `json:"covers,omitempty"`
}

// ErrNoCollection means there's no collection with that ID.
var ErrNoCollection = errors.New("no such collection")

// Collections lists the collections (by name) with their counts - not those whose comics are all deleted (in the
// trash: a comic restored brings its collection back; purged, the collection goes - purgeEmptyCollections).
func (l *Library) Collections(ctx context.Context) ([]Collection, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT k.id, k.name, count(c.id),
		       coalesce(sum(c.pages > 0 AND c.read_page + 1 >= c.pages AND (c.read_page > 0 OR c.pages = 1) AND c.read_at IS NOT NULL), 0)
		FROM collections k LEFT JOIN comics c ON c.collection_id = k.id AND c.deleted_at IS NULL
		GROUP BY k.id HAVING count(c.id) > 0 ORDER BY k.name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	var out []Collection
	for rows.Next() {
		var k Collection
		if err := rows.Scan(&k.ID, &k.Name, &k.Count, &k.Read); err != nil {
			rows.Close()
			return nil, fmt.Errorf("library: %w", err)
		}
		out = append(out, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	// Each one's cover: its first comic by (shown) title.
	members, err := l.collectionMembers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if m := members[out[i].ID]; len(m) > 0 {
			out[i].Cover = m[0]
			out[i].Covers = m[:min(4, len(m))]
		}
	}
	return out, nil
}

// collectionMembers lists each collection's comics in its order: those placed by the user first (OrderCollection),
// then the rest by (shown) title, naturally ("Vol 2" before "Vol 10").
func (l *Library) collectionMembers(ctx context.Context) (map[int64][]int64, error) {
	rows, err := l.db.R.QueryContext(ctx, `
		SELECT collection_id, id, coalesce(custom_title, title), coalesce(collection_pos, -1) FROM comics
		WHERE collection_id IS NOT NULL AND deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	type m struct {
		id    int64
		title string
		pos   int64 // (-1: not placed)
	}
	by := map[int64][]m{}
	for rows.Next() {
		var k int64
		var x m
		if err := rows.Scan(&k, &x.id, &x.title, &x.pos); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		by[k] = append(by[k], x)
	}
	out := map[int64][]int64{}
	for k, list := range by {
		sortNatural(list, func(x m) string { return x.title }, func(x m) int64 { return x.id })
		slices.SortStableFunc(list, func(a, b m) int { // (the placed ones first, in their places)
			switch {
			case a.pos >= 0 && b.pos >= 0:
				return cmp.Compare(a.pos, b.pos)
			case a.pos >= 0:
				return -1
			case b.pos >= 0:
				return 1
			}
			return 0
		})
		for _, x := range list {
			out[k] = append(out[k], x.id)
		}
	}
	return out, rows.Err()
}

// CollectionOf maps each comic in a collection to it (for folding a search's comics into their collections).
func (l *Library) CollectionOf(ctx context.Context) (map[int64]int64, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT id, collection_id FROM comics WHERE collection_id IS NOT NULL AND deleted_at IS NULL")
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var c, k int64
		if err := rows.Scan(&c, &k); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		out[c] = k
	}
	return out, rows.Err()
}

// CreateCollection makes a collection of comics (taken out of the one they were in, if any).
func (l *Library) CreateCollection(ctx context.Context, name string, comics []int64) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("library: %w: a collection needs a name", ErrInvalid)
	}
	res, err := l.db.W.ExecContext(ctx, "INSERT INTO collections (name, created_at) VALUES (?, ?)", name, l.now().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := l.SetCollection(ctx, comics, id); err != nil {
		return 0, err
	}
	return id, nil
}

// RenameCollection gives a collection another name.
func (l *Library) RenameCollection(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("library: %w: a collection needs a name", ErrInvalid)
	}
	res, err := l.db.W.ExecContext(ctx, "UPDATE collections SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoCollection
	}
	l.writes.Add(1)
	return nil
}

// DeleteCollection takes a collection apart: its comics stay, on their own again.
func (l *Library) DeleteCollection(ctx context.Context, id int64) error {
	if _, err := l.db.W.ExecContext(ctx, "UPDATE comics SET collection_id = NULL, collection_pos = NULL WHERE collection_id = ?", id); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	res, err := l.db.W.ExecContext(ctx, "DELETE FROM collections WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoCollection
	}
	l.writes.Add(1)
	return nil
}

// CollectionOrder lists a collection's comics in its order (see collectionMembers).
func (l *Library) CollectionOrder(ctx context.Context, id int64) ([]int64, error) {
	members, err := l.collectionMembers(ctx)
	if err != nil {
		return nil, err
	}
	return members[id], nil
}

// OrderCollection puts a collection's comics in the order given (comics not in it are skipped; those left out go
// after the placed ones, by title).
func (l *Library) OrderCollection(ctx context.Context, id int64, comics []int64) error {
	return l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		var one int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM collections WHERE id = ?", id).Scan(&one); err != nil {
			return nil, ErrNoCollection
		}
		if _, err := tx.ExecContext(ctx, "UPDATE comics SET collection_pos = NULL WHERE collection_id = ?", id); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		for i, c := range comics {
			if _, err := tx.ExecContext(ctx, "UPDATE comics SET collection_pos = ? WHERE id = ? AND collection_id = ?", i, c, id); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
		}
		l.writes.Add(1)
		return nil, nil
	})
}

// DeleteCollectionComics deletes a collection's comics (into the trash, undo within the window - DeleteComics); the
// collection goes with them (hidden while they're in the trash, gone when they're purged). It returns the comics.
func (l *Library) DeleteCollectionComics(ctx context.Context, id int64) ([]int64, error) {
	var one int
	if err := l.db.R.QueryRowContext(ctx, "SELECT 1 FROM collections WHERE id = ?", id).Scan(&one); err != nil {
		return nil, ErrNoCollection
	}
	rows, err := l.db.R.QueryContext(ctx, "SELECT id FROM comics WHERE collection_id = ? AND deleted_at IS NULL", id)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var c int64
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return nil, fmt.Errorf("library: %w", err)
		}
		ids = append(ids, c)
	}
	rows.Close()
	if err := l.DeleteComics(ctx, ids); err != nil {
		return nil, err
	}
	l.writes.Add(1)
	return ids, nil
}

// purgeEmptyCollections removes the collections with no comics left (their last ones purged).
func purgeEmptyCollections(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM collections WHERE NOT EXISTS (SELECT 1 FROM comics c WHERE c.collection_id = collections.id)")
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}

// SetCollection puts comics in a collection (0: out of theirs). A collection left empty goes.
func (l *Library) SetCollection(ctx context.Context, comics []int64, collection int64) error {
	if collection != 0 {
		var one int
		if err := l.db.R.QueryRowContext(ctx, "SELECT 1 FROM collections WHERE id = ?", collection).Scan(&one); err != nil {
			return ErrNoCollection
		}
	}
	var k any
	if collection != 0 {
		k = collection
	}
	for _, c := range comics {
		// (into another collection: not placed there yet - after its placed ones)
		if _, err := l.db.W.ExecContext(ctx, "UPDATE comics SET collection_id = ?, collection_pos = NULL WHERE id = ? AND coalesce(collection_id, 0) != coalesce(?, 0)", k, c, k); err != nil {
			return fmt.Errorf("library: %w", err)
		}
	}
	if _, err := l.db.W.ExecContext(ctx, `DELETE FROM collections WHERE NOT EXISTS
		(SELECT 1 FROM comics c WHERE c.collection_id = collections.id AND c.deleted_at IS NULL)`); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	l.writes.Add(1)
	return nil
}

// sortNatural orders by name with numbers by value ("Vol 2" before "Vol 10"), then by ID.
func sortNatural[T any](list []T, name func(T) string, id func(T) int64) {
	slices.SortFunc(list, func(a, b T) int {
		return cmp.Or(naturalCompare(strings.ToLower(name(a)), strings.ToLower(name(b))), cmp.Compare(id(a), id(b)))
	})
}
