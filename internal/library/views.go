package library

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/usevitrine/vitrine/internal/index"
)

// View is a file shown in the viewer: when it began (Unix ms) and how long it lasted (on screen; a video: played).
type View struct {
	File uint32 `json:"file"`
	At   int64  `json:"at"`
	MS   int64  `json:"ms"`
}

// viewGap: a view beginning within this long of the file's last one ending adds to it (swiping back and forth, the
// app put away for a moment, two devices) instead of counting again.
const viewGap = 30 * 60 * 1000

// AddViews records views (a device sends what it gathered). Files gone for good are skipped. It returns each file's
// views now.
func (l *Library) AddViews(ctx context.Context, views []View) (map[uint32]index.ViewStats, error) {
	out := map[uint32]index.ViewStats{}
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		for _, v := range views {
			if v.MS < 0 || v.At <= 0 {
				return nil, fmt.Errorf("library: bad view %+v: %w", v, ErrInvalid)
			}
			if err := fileExists(ctx, tx, v.File); err != nil {
				continue
			}
			var id, at, ms int64
			err := tx.QueryRowContext(ctx, "SELECT id, at, ms FROM views WHERE file_id = ? ORDER BY at DESC LIMIT 1", v.File).Scan(&id, &at, &ms)
			switch {
			case err == nil && v.At >= at && v.At-(at+ms) < viewGap:
				_, err = tx.ExecContext(ctx, "UPDATE views SET ms = ms + ? WHERE id = ?", v.MS, id)
			case err == nil || err == sql.ErrNoRows:
				_, err = tx.ExecContext(ctx, "INSERT INTO views (file_id, at, ms) VALUES (?, ?, ?)", v.File, v.At, v.MS)
			}
			if err != nil {
				return nil, fmt.Errorf("library: %w", err)
			}
			s, err := viewStats(ctx, tx, v.File)
			if err != nil {
				return nil, err
			}
			out[v.File] = s
		}
		return func() error {
			for id, s := range out {
				l.ix.SetViews(id, s)
			}
			return nil
		}, nil
	})
	return out, err
}

func viewStats(ctx context.Context, tx *sql.Tx, file uint32) (index.ViewStats, error) {
	var s index.ViewStats
	err := tx.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(ms), 0), coalesce(max(at), 0) FROM views WHERE file_id = ?", file).Scan(&s.Count, &s.MS, &s.Last)
	if err != nil {
		return s, fmt.Errorf("library: %w", err)
	}
	return s, nil
}

// ClearViews removes every view; it returns how many there were.
func (l *Library) ClearViews(ctx context.Context) (int, error) {
	var removed int
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		var files []uint32
		rows, err := tx.QueryContext(ctx, "SELECT DISTINCT file_id FROM views")
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		for rows.Next() {
			var id uint32
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, fmt.Errorf("library: %w", err)
			}
			files = append(files, id)
		}
		rows.Close()
		res, err := tx.ExecContext(ctx, "DELETE FROM views")
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		n, _ := res.RowsAffected()
		removed = int(n)
		return func() error {
			for _, id := range files {
				l.ix.SetViews(id, index.ViewStats{})
			}
			return nil
		}, nil
	})
	return removed, err
}
