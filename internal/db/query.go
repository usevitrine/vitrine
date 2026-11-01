package db

import (
	"context"
	"database/sql"
	"strings"
)

// Querier runs queries: a *sql.DB (W or R) or a *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Column reads a one-column query's values in order (nil when none).
func Column[T any](ctx context.Context, q Querier, query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var v T
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// InList is an IN (...) list for vals (at least one): its placeholders and arguments.
func InList[T any](vals []T) (marks string, args []any) {
	args = make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return "?" + strings.Repeat(",?", len(vals)-1), args
}
