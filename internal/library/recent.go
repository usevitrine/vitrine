package library

import (
	"context"
	"fmt"
	"strings"
)

// RecentSearch is a search run on one of the search pages lately (the search bar's drop-down; 2026-11-11).
type RecentSearch struct {
	Query  string `json:"query"`
	Sort   string `json:"sort"`    // '' = the page's default order
	UsedAt int64  `json:"used_at"` // unix ms
}

// MaxRecent is how many recent searches each page keeps.
const MaxRecent = 20

const maxRecentQuery = 2000

func recentScope(scope string) error {
	if scope != "" && scope != "comics" && scope != "browse" {
		return fmt.Errorf("library: %w: scope %q", ErrInvalid, scope)
	}
	return nil
}

// cleanRecent writes a search the same way however it was typed: spaces collapsed, lower case for comparing (the
// stored text keeps its case - the search bar's own writing).
func cleanRecent(query, sort string) (string, string) {
	return strings.Join(strings.Fields(query), " "), strings.TrimSpace(sort)
}

// RecentSearches lists a page's recent searches, newest first.
func (l *Library) RecentSearches(ctx context.Context, scope string) ([]RecentSearch, error) {
	if err := recentScope(scope); err != nil {
		return nil, err
	}
	rows, err := l.db.R.QueryContext(ctx, "SELECT query, sort, used_at FROM recent_searches WHERE scope = ? ORDER BY used_at DESC LIMIT ?", scope, MaxRecent)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := []RecentSearch{}
	for rows.Next() {
		var r RecentSearch
		if err := rows.Scan(&r.Query, &r.Sort, &r.UsedAt); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddRecentSearch puts a search at the top of its page's recent searches (one already there - the same text, case
// aside, and order - moves up instead of showing twice); only the newest MaxRecent are kept.
func (l *Library) AddRecentSearch(ctx context.Context, query, sort, scope string) error {
	if err := recentScope(scope); err != nil {
		return err
	}
	query, sort = cleanRecent(query, sort)
	if query == "" {
		return fmt.Errorf("library: %w: an empty search", ErrInvalid)
	}
	if len(query) > maxRecentQuery {
		return fmt.Errorf("library: %w: a search that long", ErrInvalid)
	}
	tx, err := l.db.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM recent_searches WHERE scope = ? AND lower(query) = lower(?) AND sort = ?", scope, query, sort); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO recent_searches (scope, query, sort, used_at) VALUES (?, ?, ?, ?)", scope, query, sort, l.now().UnixMilli()); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM recent_searches WHERE scope = ? AND rowid NOT IN
		(SELECT rowid FROM recent_searches WHERE scope = ? ORDER BY used_at DESC, rowid DESC LIMIT ?)`, scope, scope, MaxRecent); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return tx.Commit()
}

// RemoveRecentSearch takes one search out of a page's recent searches (none there: nothing to do).
func (l *Library) RemoveRecentSearch(ctx context.Context, query, sort, scope string) error {
	if err := recentScope(scope); err != nil {
		return err
	}
	query, sort = cleanRecent(query, sort)
	if _, err := l.db.W.ExecContext(ctx, "DELETE FROM recent_searches WHERE scope = ? AND lower(query) = lower(?) AND sort = ?", scope, query, sort); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}

// ClearRecentSearches empties a page's recent searches.
func (l *Library) ClearRecentSearches(ctx context.Context, scope string) error {
	if err := recentScope(scope); err != nil {
		return err
	}
	if _, err := l.db.W.ExecContext(ctx, "DELETE FROM recent_searches WHERE scope = ?", scope); err != nil {
		return fmt.Errorf("library: %w", err)
	}
	return nil
}
