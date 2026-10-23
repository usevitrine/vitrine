package library

import (
	"context"
	"database/sql"
	"fmt"
)

// SetFavorite makes files favorites (on) or not; files already so are left as
// they are (a favorite keeps the time it became one). It returns how many changed.
func (l *Library) SetFavorite(ctx context.Context, ids []uint32, on bool) (int, error) {
	var changed int
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		now := l.now().UnixMilli()
		type change struct {
			id uint32
			at int64
		}
		var done []change
		for _, id := range ids {
			if err := fileExists(ctx, tx, id); err != nil {
				return nil, err
			}
			var res sql.Result
			var err error
			if on {
				res, err = tx.ExecContext(ctx, "INSERT INTO favorites (file_id, added_at) VALUES (?, ?) ON CONFLICT DO NOTHING", id, now)
			} else {
				res, err = tx.ExecContext(ctx, "DELETE FROM favorites WHERE file_id = ?", id)
			}
			if err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				at := int64(0)
				if on {
					at = now
				}
				done = append(done, change{id, at})
			}
		}
		changed = len(done)
		return func() error {
			for _, c := range done {
				l.ix.SetFavorite(c.id, c.at)
			}
			return nil
		}, nil
	})
	return changed, err
}

// carryFavorite makes keep a favorite if from is one and keep isn't (merging
// duplicates); it returns since when, or 0 when nothing changed.
func carryFavorite(ctx context.Context, tx *sql.Tx, keep, from uint32) (int64, error) {
	var at int64
	err := tx.QueryRowContext(ctx, `INSERT INTO favorites (file_id, added_at)
		SELECT ?1, added_at FROM favorites WHERE file_id = ?2 ON CONFLICT DO NOTHING RETURNING added_at`, keep, from).Scan(&at)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return at, err
}
