package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/tag"
)

// The tag browser: every tag with its count, and renaming the user's own.

// BrowsedTag is a tag in the tag browser.
type BrowsedTag struct {
	index.TagCount
	FromSites bool `json:"from_sites"` // some file has it from a site (renaming would be undone by the next refresh)
}

// TagList lists tags (see index.TagList) and says which come from sites.
func (l *Library) TagList(ctx context.Context, filter, ns string, byName bool, offset, limit int) (int, []BrowsedTag, error) {
	total, list := l.ix.TagList(filter, ns, byName, offset, limit)
	out := make([]BrowsedTag, len(list))
	if len(list) == 0 {
		return total, out, nil
	}
	args := make([]any, len(list))
	for i, t := range list {
		out[i].TagCount = t
		args[i] = t.ID
	}
	rows, err := l.db.R.QueryContext(ctx, "SELECT DISTINCT tag_id FROM file_tags WHERE origin <> 0 AND tag_id IN (?"+
		strings.Repeat(",?", len(args)-1)+")", args...)
	if err != nil {
		return 0, nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	site := map[uint32]bool{}
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return 0, nil, err
		}
		site[id] = true
	}
	for i := range out {
		out[i].FromSites = site[out[i].ID]
	}
	return total, out, rows.Err()
}

// Rename errors.
var (
	ErrNoTag        = errors.New("no such tag")
	ErrSiteTag      = errors.New("this tag comes from a site: the next tag refresh would bring the old name back - make it a sibling instead")
	ErrHasRelations = errors.New("this tag has parents or siblings: remove them first, or make it a sibling")
)

// RenameTag moves the user's tag to a new name on every file that has it
// (into an existing tag of that name, if there is one). Returns how many files
// changed. Tags from sites and tags with parents or siblings can't be renamed.
func (l *Library) RenameTag(ctx context.Context, from, to tag.Tag) (int, error) {
	if to.Name == "" {
		return 0, fmt.Errorf("library: %w: empty tag", ErrInvalid)
	}
	var id uint32
	err := l.db.R.QueryRowContext(ctx, `SELECT t.id FROM tags t JOIN namespaces n ON n.id = t.namespace_id
		WHERE n.name = ? AND t.name = ?`, from.Namespace, from.Name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoTag
	}
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	if from == to {
		return 0, nil
	}
	var n int
	if err := l.db.R.QueryRowContext(ctx, "SELECT count(*) FROM file_tags WHERE tag_id = ? AND origin <> 0", id).Scan(&n); err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	if n > 0 {
		return 0, ErrSiteTag
	}
	if err := l.db.R.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM tag_parents WHERE child_id = ?1 OR parent_id = ?1) +
		(SELECT count(*) FROM tag_siblings WHERE from_id = ?1 OR to_id = ?1)`, id).Scan(&n); err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	if n > 0 {
		return 0, ErrHasRelations
	}
	rows, err := l.db.R.QueryContext(ctx, "SELECT file_id FROM file_tags WHERE tag_id = ? AND origin = 0", id)
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
	var files []uint32
	for rows.Next() {
		var f uint32
		if err := rows.Scan(&f); err != nil {
			rows.Close()
			return 0, err
		}
		files = append(files, f)
	}
	rows.Close()
	if len(files) == 0 {
		return 0, nil
	}
	if err := l.AddTags(ctx, files, []tag.Tag{to}); err != nil {
		return 0, err
	}
	if err := l.RemoveTags(ctx, files, []tag.Tag{from}); err != nil {
		return 0, err
	}
	return len(files), nil
}
