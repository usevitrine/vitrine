package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/search"
)

// SavedSearch is a search the user keeps for one-tap use (a favorite).
type SavedSearch struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Query string `json:"query"`
	Sort  string `json:"sort"`  // '' = the default order
	Count int    `json:"count"` // files it finds now
}

// ErrNoSaved means there is no saved search with that ID.
var ErrNoSaved = errors.New("no such saved search")

const maxSavedName = 80

func cleanSaved(name, query string) (string, string, error) {
	name = strings.Join(strings.Fields(name), " ")
	query = strings.Join(strings.Fields(query), " ")
	if name == "" {
		name = query
	}
	if name == "" {
		return "", "", fmt.Errorf("library: %w: a saved search needs a name or a search", ErrInvalid)
	}
	if r := []rune(name); len(r) > maxSavedName {
		name = string(r[:maxSavedName])
	}
	if _, err := search.Parse(query); err != nil {
		return "", "", fmt.Errorf("library: %w: %v", ErrInvalid, err)
	}
	return name, query, nil
}

// SavedSearches lists the saved searches in the user's order, each with how
// many files it finds now. scope: "" the library's, "comics" the Comics page's.
func (l *Library) SavedSearches(ctx context.Context, scope string) ([]SavedSearch, error) {
	ix := l.ix
	if scope == "comics" {
		var err error
		if ix, err = l.ComicIndex(ctx); err != nil {
			return nil, err
		}
	}
	rows, err := l.db.R.QueryContext(ctx, "SELECT id, name, query, sort FROM saved_searches WHERE scope = ? ORDER BY position, id", scope)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	var out []SavedSearch
	now := time.Now()
	for rows.Next() {
		var s SavedSearch
		if err := rows.Scan(&s.ID, &s.Name, &s.Query, &s.Sort); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if q, err := search.Parse(s.Query); err == nil {
			s.Count = ix.Count(q, now)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveSearch adds a saved search at the end of its scope's list (see SavedSearches).
func (l *Library) SaveSearch(ctx context.Context, name, query, sort, scope string) (int64, error) {
	if scope != "" && scope != "comics" {
		return 0, fmt.Errorf("library: %w: scope %q", ErrInvalid, scope)
	}
	name, query, err := cleanSaved(name, query)
	if err != nil {
		return 0, err
	}
	var id int64
	err = l.db.W.QueryRowContext(ctx, `INSERT INTO saved_searches (name, query, sort, position, created_at, scope)
		VALUES (?, ?, ?, (SELECT coalesce(max(position), 0) + 1 FROM saved_searches), ?, ?) RETURNING id`,
		name, query, strings.TrimSpace(sort), l.now().UnixMilli(), scope).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	return id, nil
}

// SavedChange edits a saved search; nil fields stay as they are.
type SavedChange struct {
	Name, Query, Sort *string
}

// UpdateSavedSearch renames a saved search or changes its search or order.
func (l *Library) UpdateSavedSearch(ctx context.Context, id int64, c SavedChange) error {
	var name, query, sort string
	err := l.db.R.QueryRowContext(ctx, "SELECT name, query, sort FROM saved_searches WHERE id = ?", id).Scan(&name, &query, &sort)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoSaved
	}
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if c.Name != nil {
		name = *c.Name
	}
	if c.Query != nil {
		query = *c.Query
	}
	if c.Sort != nil {
		sort = strings.TrimSpace(*c.Sort)
	}
	if name, query, err = cleanSaved(name, query); err != nil {
		return err
	}
	_, err = l.db.W.ExecContext(ctx, "UPDATE saved_searches SET name = ?, query = ?, sort = ? WHERE id = ?", name, query, sort, id)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}

// DeleteSavedSearch removes a saved search.
func (l *Library) DeleteSavedSearch(ctx context.Context, id int64) error {
	res, err := l.db.W.ExecContext(ctx, "DELETE FROM saved_searches WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoSaved
	}
	return nil
}

// ReorderSavedSearches puts the saved searches in this order (others after,
// as they were).
func (l *Library) ReorderSavedSearches(ctx context.Context, ids []int64) error {
	tx, err := l.db.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM saved_searches ORDER BY position, id")
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var rest []int64
	known := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		known[id] = true
		if !want[id] {
			rest = append(rest, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if !known[id] {
			return ErrNoSaved
		}
	}
	for i, id := range append(append([]int64{}, ids...), rest...) {
		if _, err := tx.ExecContext(ctx, "UPDATE saved_searches SET position = ? WHERE id = ?", i+1, id); err != nil {
			return fmt.Errorf("library: %w", err)
		}
	}
	return tx.Commit()
}
