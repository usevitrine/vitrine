package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/usevitrine/vitrine/internal/db"
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
func (l *Library) TagList(ctx context.Context, filter, ns, site string, byName bool, offset, limit int) (int, []BrowsedTag, error) {
	total, list := l.ix.TagList(filter, ns, site, byName, offset, limit)
	out := make([]BrowsedTag, len(list))
	if len(list) == 0 {
		return total, out, nil
	}
	tags := make([]uint32, len(list))
	for i, t := range list {
		out[i].TagCount = t
		tags[i] = t.ID
	}
	marks, args := db.InList(tags)
	ids, err := db.Column[uint32](ctx, l.db.R, "SELECT DISTINCT tag_id FROM file_tags WHERE origin <> 0 AND tag_id IN ("+marks+")", args...)
	if err != nil {
		return 0, nil, fmt.Errorf("library: %w", err)
	}
	given := map[uint32]bool{}
	for _, id := range ids {
		given[id] = true
	}
	for i := range out {
		out[i].FromSites = given[out[i].ID]
	}
	return total, out, nil
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
	files, err := db.Column[uint32](ctx, l.db.R, "SELECT file_id FROM file_tags WHERE tag_id = ? AND origin = 0", id)
	if err != nil {
		return 0, fmt.Errorf("library: %w", err)
	}
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
