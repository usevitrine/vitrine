package library

import (
	"context"
	"database/sql"
	"fmt"
)

// SeenMarks returns when each of keys was last opened (Unix ms). A key never
// opened is marked now (so it starts with nothing new) and returned as such.
func (l *Library) SeenMarks(ctx context.Context, keys []string) (map[string]int64, error) {
	out := make(map[string]int64, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := l.db.R.QueryContext(ctx, "SELECT key, seen_at FROM seen_marks")
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	all := map[string]int64{}
	for rows.Next() {
		var k string
		var at int64
		if err := rows.Scan(&k, &at); err != nil {
			rows.Close()
			return nil, fmt.Errorf("library: %w", err)
		}
		all[k] = at
	}
	rows.Close()
	var missing []string
	for _, k := range keys {
		if at, ok := all[k]; ok {
			out[k] = at
		} else {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return out, nil // (the usual case: a read only, no write every check)
	}
	now := l.now().UnixMilli()
	err = l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		for _, k := range missing {
			if _, err := tx.ExecContext(ctx, "INSERT INTO seen_marks (key, seen_at) VALUES (?, ?) ON CONFLICT DO NOTHING", k, now); err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			out[k] = now
		}
		return nil, nil
	})
	return out, err
}

// MarkSeen notes that key was opened at (Unix ms; 0 = now) and returns when it
// was last opened before (0 = never).
func (l *Library) MarkSeen(ctx context.Context, key string, at int64) (int64, error) {
	if at <= 0 {
		at = l.now().UnixMilli()
	}
	var prev int64
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		err := tx.QueryRowContext(ctx, "SELECT seen_at FROM seen_marks WHERE key = ?", key).Scan(&prev)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("library: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO seen_marks (key, seen_at) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET seen_at = excluded.seen_at`, key, at)
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		return nil, nil
	})
	return prev, err
}
