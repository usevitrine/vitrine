package library

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
)

// AddClimax counts a press of the viewer's "I came" button on a file. It
// returns the press (for Undo) and the file's count now.
func (l *Library) AddClimax(ctx context.Context, file uint32) (int64, int, error) {
	var id int64
	var n int
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if err := fileExists(ctx, tx, file); err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, "INSERT INTO climaxes (file_id, at) VALUES (?, ?) RETURNING id", file, l.now().UnixMilli()).Scan(&id); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM climaxes WHERE file_id = ?", file).Scan(&n); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		return func() error { l.ix.SetClimaxes(file, n); return nil }, nil
	})
	return id, n, err
}

// AddComicClimax counts a press of the comic reader's "I came" button on a page (0-based). It returns the press (for
// Undo) and the page's count now.
func (l *Library) AddComicClimax(ctx context.Context, comic int64, page int) (int64, int, error) {
	var id int64
	var file uint32
	var n, all int
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		err := tx.QueryRowContext(ctx, `SELECT p.file_id FROM comic_pages p JOIN comics c ON c.id = p.comic_id
			WHERE p.comic_id = ? AND p.page = ? AND c.deleted_at IS NULL`, comic, page).Scan(&file)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("library: comic %d has no page %d: %w", comic, page+1, ErrInvalid)
		}
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if err := tx.QueryRowContext(ctx, "INSERT INTO climaxes (file_id, at, comic_id, page) VALUES (?, ?, ?, ?) RETURNING id",
			file, l.now().UnixMilli(), comic, page).Scan(&id); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM climaxes WHERE comic_id = ? AND page = ?", comic, page).Scan(&n); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM climaxes WHERE file_id = ?", file).Scan(&all); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		return func() error { l.ix.SetClimaxes(file, all); return nil }, nil
	})
	return id, n, err
}

// ComicClimaxes returns a comic's counts by page (0-based page -> presses).
func (l *Library) ComicClimaxes(ctx context.Context, comic int64) (map[int]int, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT page, count(*) FROM climaxes WHERE comic_id = ? GROUP BY page", comic)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	out := map[int]int{}
	for rows.Next() {
		var p, n int
		if err := rows.Scan(&p, &n); err != nil {
			return nil, err
		}
		out[p] = n
	}
	return out, rows.Err()
}

// UndoClimax takes a press back; it returns the file and its count now.
func (l *Library) UndoClimax(ctx context.Context, id int64) (uint32, int, error) {
	var file uint32
	var n int
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		err := tx.QueryRowContext(ctx, "DELETE FROM climaxes WHERE id = ? RETURNING file_id", id).Scan(&file)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("library: no such press: %w", ErrInvalid)
		}
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM climaxes WHERE file_id = ?", file).Scan(&n); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		return func() error { l.ix.SetClimaxes(file, n); return nil }, nil
	})
	return file, n, err
}

// Ranking is the Rankings page for a period.
type Ranking struct {
	Total      int        `json:"total"`
	Days       []int      `json:"days"`       // per day, oldest first, ending today (the caller's time zone)
	Files      []RankFile `json:"files"`      // the most counted files (live ones)
	Pages      []RankPage `json:"pages"`      // the most counted comic pages (the comic reader's presses)
	Creators   []RankTag  `json:"creators"`   // tags in the creator namespace
	Characters []RankTag  `json:"characters"` // in the character namespace
	Series     []RankTag  `json:"series"`     // in the series namespace
	Tags       []RankTag  `json:"tags"`       // every other tag
	Streak     int        `json:"streak"`     // days in a row up to today (or yesterday) with at least one
	First      int64      `json:"first"`      // the first press ever (Unix ms; 0 = none)
}

// RankFile is a file and how many times it was counted in the period.
type RankFile struct {
	ID    uint32 `json:"id"`
	Count int    `json:"count"`
}

// RankPage is a comic's page (0-based) and how many times it was counted in the period.
type RankPage struct {
	Comic int64  `json:"comic"`
	Page  int    `json:"page"`
	File  uint32 `json:"file"` // the page's picture
	Title string `json:"title"`
	Count int    `json:"count"`
}

// RankTag is a tag and how many presses were on files with it in the period.
type RankTag struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// Rankings counts the presses since from (Unix ms; 0 = all time): the total,
// per day for the last days days, and the top files, creators, characters, series and tags.
// tzOffset (seconds east of UTC) decides where days end.
func (l *Library) Rankings(ctx context.Context, from int64, days int, tzOffset int64) (Ranking, error) {
	out := Ranking{Days: []int{}, Files: []RankFile{}, Pages: []RankPage{}, Creators: []RankTag{}, Characters: []RankTag{}, Series: []RankTag{}, Tags: []RankTag{}}
	q := l.db.R
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM climaxes WHERE at >= ?", from).Scan(&out.Total); err != nil {
		return out, err
	}
	if err := q.QueryRowContext(ctx, "SELECT coalesce(min(at), 0) FROM climaxes").Scan(&out.First); err != nil {
		return out, err
	}
	// Per day (and the streak, over all time).
	dayOf := func(ms int64) int64 { return (ms/1000 + tzOffset) / 86400 }
	today := dayOf(l.now().UnixMilli())
	rows, err := q.QueryContext(ctx, "SELECT DISTINCT (at / 1000 + ?) / 86400 FROM climaxes ORDER BY 1 DESC", tzOffset)
	if err != nil {
		return out, err
	}
	var seen []int64 // days with a press, newest first
	for rows.Next() {
		var d int64
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return out, err
		}
		seen = append(seen, d)
	}
	rows.Close()
	// The streak: days in a row ending today - or yesterday, when today has none yet.
	expect := today
	if len(seen) > 0 && seen[0] == today-1 {
		expect = today - 1
	}
	for _, d := range seen {
		if d != expect {
			break
		}
		out.Streak++
		expect--
	}
	if days > 0 {
		out.Days = make([]int, days)
		first := today - int64(days-1)
		rows, err := q.QueryContext(ctx, "SELECT (at / 1000 + ?1) / 86400, count(*) FROM climaxes WHERE (at / 1000 + ?1) / 86400 >= ?2 GROUP BY 1", tzOffset, first)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var d int64
			var n int
			if err := rows.Scan(&d, &n); err != nil {
				rows.Close()
				return out, err
			}
			if i := d - first; i >= 0 && i < int64(days) {
				out.Days[i] = n
			}
		}
		rows.Close()
	}
	// Top comic pages (the comic reader's presses).
	rows, err = q.QueryContext(ctx, `SELECT c.comic_id, c.page, max(c.file_id), coalesce(nullif(k.custom_title, ''), k.title), count(*)
		FROM climaxes c JOIN comics k ON k.id = c.comic_id
		WHERE c.at >= ? AND k.deleted_at IS NULL GROUP BY c.comic_id, c.page ORDER BY 5 DESC, max(c.at) DESC LIMIT 30`, from)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p RankPage
		if err := rows.Scan(&p.Comic, &p.Page, &p.File, &p.Title, &p.Count); err != nil {
			rows.Close()
			return out, err
		}
		out.Pages = append(out.Pages, p)
	}
	rows.Close()
	// Top files (the viewer's presses).
	rows, err = q.QueryContext(ctx, `SELECT c.file_id, count(*) FROM climaxes c JOIN files f ON f.id = c.file_id
		WHERE c.at >= ? AND c.comic_id IS NULL AND f.deleted_at IS NULL GROUP BY c.file_id ORDER BY 2 DESC, max(c.at) DESC LIMIT 30`, from)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var f RankFile
		if err := rows.Scan(&f.ID, &f.Count); err != nil {
			rows.Close()
			return out, err
		}
		out.Files = append(out.Files, f)
	}
	rows.Close()
	// Top tags: presses on files with the tag (whatever its origin, once), and on pages of comics with it.
	type tagCount struct {
		ns, name string
		n        int
	}
	counts := map[[2]string]int{}
	for _, tq := range []string{
		`SELECT n.name, t.name, count(DISTINCT c.id) FROM climaxes c
		JOIN file_tags ft ON ft.file_id = c.file_id JOIN tags t ON t.id = ft.tag_id JOIN namespaces n ON n.id = t.namespace_id
		WHERE c.at >= ? AND n.name != 'meta' GROUP BY t.id ORDER BY 3 DESC LIMIT 2000`,
		`SELECT n.name, t.name, count(DISTINCT c.id) FROM climaxes c
		JOIN comic_tags ct ON ct.comic_id = c.comic_id JOIN tags t ON t.id = ct.tag_id JOIN namespaces n ON n.id = t.namespace_id
		WHERE c.at >= ? AND c.comic_id IS NOT NULL AND n.name != 'meta' GROUP BY t.id ORDER BY 3 DESC LIMIT 2000`,
	} {
		rows, err = q.QueryContext(ctx, tq, from)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var ns, name string
			var n int
			if err := rows.Scan(&ns, &name, &n); err != nil {
				rows.Close()
				return out, err
			}
			counts[[2]string{ns, name}] += n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
	}
	ranked := make([]tagCount, 0, len(counts))
	for k, n := range counts {
		ranked = append(ranked, tagCount{k[0], k[1], n})
	}
	slices.SortFunc(ranked, func(a, b tagCount) int {
		return cmp.Or(cmp.Compare(b.n, a.n), cmp.Compare(a.ns, b.ns), cmp.Compare(a.name, b.name))
	})
	for _, tc := range ranked {
		ns, name, n := tc.ns, tc.name, tc.n
		switch {
		case ns == "creator" && len(out.Creators) < 20:
			out.Creators = append(out.Creators, RankTag{Tag: ns + ":" + name, Count: n})
		case ns == "character" && len(out.Characters) < 20:
			out.Characters = append(out.Characters, RankTag{Tag: ns + ":" + name, Count: n})
		case ns == "series" && len(out.Series) < 20:
			out.Series = append(out.Series, RankTag{Tag: ns + ":" + name, Count: n})
		case ns != "creator" && ns != "character" && ns != "series" && len(out.Tags) < 20:
			tag := name
			if ns != "" {
				tag = ns + ":" + name
			}
			out.Tags = append(out.Tags, RankTag{Tag: tag, Count: n})
		}
	}
	return out, nil
}

// RemoveClimaxes takes presses off a file: its latest one, or all of them
// (all). It returns the file's count now.
func (l *Library) RemoveClimaxes(ctx context.Context, file uint32, all bool) (int, error) {
	var n int
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		q := "DELETE FROM climaxes WHERE id = (SELECT id FROM climaxes WHERE file_id = ? ORDER BY at DESC, id DESC LIMIT 1)"
		if all {
			q = "DELETE FROM climaxes WHERE file_id = ?"
		}
		if _, err := tx.ExecContext(ctx, q, file); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM climaxes WHERE file_id = ?", file).Scan(&n); err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		return func() error { l.ix.SetClimaxes(file, n); return nil }, nil
	})
	return n, err
}

// ClearClimaxes removes every press; it returns how many there were.
func (l *Library) ClearClimaxes(ctx context.Context) (int, error) {
	var removed int
	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		var files []uint32
		rows, err := tx.QueryContext(ctx, "SELECT DISTINCT file_id FROM climaxes")
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
		res, err := tx.ExecContext(ctx, "DELETE FROM climaxes")
		if err != nil {
			return nil, fmt.Errorf("library: %w", err)
		}
		n, _ := res.RowsAffected()
		removed = int(n)
		return func() error {
			for _, id := range files {
				l.ix.SetClimaxes(id, 0)
			}
			return nil
		}, nil
	})
	return removed, err
}
