package library

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"strings"
)

// fixEscapedTags repairs tags saved with HTML character codes in their names
// (a site sent "kai&#039;sa" for "kai'sa"; decoded since 2026-10-19): each is
// renamed, or merged into the tag with the right name if there is one - its
// files, hidden marks, parents and siblings move over. Also the site tag types
// saved under such names. Cheap when there's nothing to do (runs at start-up,
// before the search index is built).
func fixEscapedTags(ctx context.Context, w *sql.DB) error {
	return fixTagNames(ctx, w, "name LIKE '%&%;%'", html.UnescapeString)
}

// fixCaretTags repairs tags saved with Shimmie's caret codes ("bloom_^a_rage"
// for "bloom_&_rage": its tag links write & / \ ? . as ^a ^s ^b ^q ^d; decoded
// since 2026-10-19). "^^" is left alone: real tags have carets ("^_^").
func fixCaretTags(ctx context.Context, w *sql.DB) error {
	return fixTagNames(ctx, w, "name GLOB '*^[asbqd]*'", undoCarets)
}

// undoCarets decodes Shimmie's letter codes (^a ^s ^b ^q ^d), not "^^".
func undoCarets(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '^' && i+1 < len(s) {
			if c, ok := caretCodes[s[i+1]]; ok {
				b.WriteByte(c)
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

var caretCodes = map[byte]byte{'a': '&', 's': '/', 'b': '\\', 'q': '?', 'd': '.'}

// fixTagNames renames the tags (and site tag types) a filter finds by fix, or
// merges each into the tag that already has the fixed name.
func fixTagNames(ctx context.Context, w *sql.DB, where string, fix func(string) string) error {
	rows, err := w.QueryContext(ctx, "SELECT id, namespace_id, name FROM tags WHERE "+where)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	type bad struct {
		id, ns    int64
		name, fix string
	}
	var list []bad
	for rows.Next() {
		var b bad
		if err := rows.Scan(&b.id, &b.ns, &b.name); err != nil {
			rows.Close()
			return fmt.Errorf("library: %w", err)
		}
		if b.fix = fix(b.name); b.fix != b.name && b.fix != "" {
			list = append(list, b)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("library: %w", err)
	}

	tx, err := w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	defer tx.Rollback()
	for _, b := range list {
		var good int64
		err := tx.QueryRowContext(ctx, "SELECT id FROM tags WHERE namespace_id = ? AND name = ?", b.ns, b.fix).Scan(&good)
		switch {
		case err == sql.ErrNoRows:
			if _, err := tx.ExecContext(ctx, "UPDATE tags SET name = ? WHERE id = ?", b.fix, b.id); err != nil {
				return fmt.Errorf("library: %w", err)
			}
			continue
		case err != nil:
			return fmt.Errorf("library: %w", err)
		}
		// Merge into the existing tag (rows it already has stay; the rest move).
		for _, q := range []string{
			"UPDATE OR IGNORE file_tags SET tag_id = ?1 WHERE tag_id = ?2",
			"UPDATE OR IGNORE hidden_tags SET tag_id = ?1 WHERE tag_id = ?2",
			"UPDATE OR IGNORE tag_parents SET child_id = ?1 WHERE child_id = ?2 AND parent_id <> ?1",
			"UPDATE OR IGNORE tag_parents SET parent_id = ?1 WHERE parent_id = ?2 AND child_id <> ?1",
			"UPDATE OR IGNORE tag_siblings SET from_id = ?1 WHERE from_id = ?2 AND to_id <> ?1",
			"UPDATE OR IGNORE tag_siblings SET to_id = ?1 WHERE to_id = ?2 AND from_id <> ?1",
			"DELETE FROM tags WHERE id = ?2", // what couldn't move goes with it
		} {
			if _, err := tx.ExecContext(ctx, q, good, b.id); err != nil {
				return fmt.Errorf("library: %w", err)
			}
		}
	}
	// Tag types saved under such names: under the right name (unless already there).
	trows, err := tx.QueryContext(ctx, "SELECT site_id, name FROM tag_types WHERE "+where)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	type tt struct {
		site      int64
		name, fix string
	}
	var types []tt
	for trows.Next() {
		var t tt
		if err := trows.Scan(&t.site, &t.name); err != nil {
			trows.Close()
			return fmt.Errorf("library: %w", err)
		}
		if t.fix = fix(t.name); t.fix != t.name {
			types = append(types, t)
		}
	}
	trows.Close()
	for _, t := range types {
		if _, err := tx.ExecContext(ctx, "UPDATE OR IGNORE tag_types SET name = ? WHERE site_id = ? AND name = ?", t.fix, t.site, t.name); err != nil {
			return fmt.Errorf("library: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM tag_types WHERE site_id = ? AND name = ?", t.site, t.name); err != nil {
			return fmt.Errorf("library: %w", err)
		}
	}
	return tx.Commit()
}
