package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/RoaringBitmap/roaring/v2"

	"github.com/usevitrine/vitrine/internal/index"
	"github.com/usevitrine/vitrine/internal/search"
	"github.com/usevitrine/vitrine/internal/tag"
)

// The download blacklist: tags whose posts are never downloaded. An entry is
// a tag ("furry") or a pattern with '*' ("ai_*"), matched against a post's
// whole site tag, ignoring case. Files already in the library are found with
// BlacklistedFiles and removed with DeleteBlacklisted (normal deletes, so
// they can be undone and are never downloaded again).

// ErrBadPattern means a blacklist entry isn't a tag or a tag pattern.
var ErrBadPattern = errors.New("not a tag or tag pattern")

// NormalizePattern cleans up a typed entry: trimmed, lower case, spaces as
// underscores (how sites write tags).
func NormalizePattern(s string) string {
	return strings.Join(strings.Fields(tag.Normalize(s)), "_")
}

func validPattern(p string) bool {
	if p == "" || strings.Trim(p, "*") == "" || strings.ContainsAny(p, "()~") || strings.HasPrefix(p, "-") {
		return false
	}
	for _, prefix := range []string{"system:", "sort:", "order:", "limit:"} {
		if strings.HasPrefix(p, prefix) {
			return false
		}
	}
	return true
}

// Blacklist is a loaded blacklist, ready to check posts against.
type Blacklist struct {
	patterns []string
}

// Patterns lists the entries, sorted.
func (b *Blacklist) Patterns() []string { return b.patterns }

// Match returns the first entry one of the tags matches ("" if none). An entry with a namespace ("creator:x", e.g.
// added from a file's tags, where the library files the site's tags under namespaces) also matches the bare tag
// ("x"): image boards send bare tags, so "creator:x" never stopped a download of a post tagged x (the user saw one
// come in).
func (b *Blacklist) Match(tags []string) string {
	if b == nil || len(b.patterns) == 0 {
		return ""
	}
	for _, t := range tags {
		t = strings.ToLower(t)
		for _, p := range b.patterns {
			if index.GlobMatch(p, t) {
				return p
			}
			if _, bare, ok := strings.Cut(p, ":"); ok && bare != "" && !strings.Contains(t, ":") && index.GlobMatch(bare, t) {
				return p
			}
		}
	}
	return ""
}

// Blacklist reads the blacklist.
func (l *Library) Blacklist(ctx context.Context) (*Blacklist, error) {
	rows, err := l.db.R.QueryContext(ctx, "SELECT pattern FROM blacklist ORDER BY pattern")
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer rows.Close()
	b := &Blacklist{patterns: []string{}}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		b.patterns = append(b.patterns, p)
	}
	return b, rows.Err()
}

// SetBlacklist replaces the blacklist (entries are normalized; duplicates
// dropped) and returns it as stored.
func (l *Library) SetBlacklist(ctx context.Context, entries []string) ([]string, error) {
	var clean []string
	for _, e := range entries {
		p := NormalizePattern(e)
		if p == "" {
			continue
		}
		if !validPattern(p) {
			return nil, fmt.Errorf("library: %q: %w", e, ErrBadPattern)
		}
		clean = append(clean, p)
	}
	slices.Sort(clean)
	clean = slices.Compact(clean)

	err := l.write(ctx, func(tx *sql.Tx) (func() error, error) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM blacklist WHERE pattern NOT IN (SELECT value FROM json_each(?))",
			jsonList(clean)); err != nil {
			return nil, err
		}
		now := l.now().UnixMilli()
		for _, p := range clean {
			if _, err := tx.ExecContext(ctx, "INSERT INTO blacklist (pattern, added_at) VALUES (?, ?) ON CONFLICT DO NOTHING",
				p, now); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	l.listsVer.Add(1)
	if clean == nil {
		clean = []string{}
	}
	return clean, nil
}

// DownloadListsVersion changes whenever the blacklist or the download rules do: running downloads compare it before
// each post and read them again, so a tag blacklisted mid-download applies from the next post (2026-10-28, the user:
// "when you add new blacklisted tags, the changes [don't] apply to running downloads").
func (l *Library) DownloadListsVersion() int64 { return l.listsVer.Load() }

func jsonList(s []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range s {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q", v)
	}
	b.WriteByte(']')
	return b.String()
}

// PatternCount is how many library files one blacklist entry matches.
type PatternCount struct {
	Pattern string `json:"tag"`
	Files   int    `json:"files"`
}

// BlacklistMatches are the library files with blacklisted tags.
type BlacklistMatches struct {
	IDs    []uint32       `json:"-"`
	Total  int            `json:"total"`
	Size   int64          `json:"size"`
	Counts []PatternCount `json:"tags"`
}

// BlacklistedFiles finds the library's files matching the given entries
// (all blacklist entries if patterns is nil). Like a search, a tag also
// matches files that only have it through a tag parent or sibling.
func (l *Library) BlacklistedFiles(ctx context.Context, patterns []string) (BlacklistMatches, error) {
	var m BlacklistMatches
	if patterns == nil {
		b, err := l.Blacklist(ctx)
		if err != nil {
			return m, err
		}
		patterns = b.patterns
	}
	all := roaring.New()
	m.Counts = []PatternCount{}
	for _, p := range patterns {
		p = NormalizePattern(p)
		if !validPattern(p) {
			return m, fmt.Errorf("library: %q: %w", p, ErrBadPattern)
		}
		q, err := search.Parse(p)
		if err != nil {
			return m, fmt.Errorf("library: %q: %w", p, err)
		}
		res := l.ix.Search(q, l.now())
		m.Counts = append(m.Counts, PatternCount{Pattern: p, Files: len(res.IDs)})
		all.AddMany(res.IDs)
	}
	m.IDs = all.ToArray()
	m.Total = len(m.IDs)
	m.Size = l.ix.SizeOf(m.IDs)
	return m, nil
}

// DeleteBlacklisted deletes the library's files matching the given entries
// (all if nil), like Delete: undoable for a while, never downloaded again.
func (l *Library) DeleteBlacklisted(ctx context.Context, patterns []string) ([]uint32, error) {
	m, err := l.BlacklistedFiles(ctx, patterns)
	if err != nil {
		return nil, err
	}
	if err := l.Delete(ctx, m.IDs, "blacklisted tag"); err != nil {
		return nil, err
	}
	return m.IDs, nil
}
