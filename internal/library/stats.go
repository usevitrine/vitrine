package library

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// Stats is the library at a glance (Settings > Stats).
type Stats struct {
	Files   int64      `json:"files"`
	Bytes   int64      `json:"bytes"`
	Kinds   []KindStat `json:"kinds"` // images, GIFs, videos
	Trash   Count      `json:"trash"` // deleted, still in the undo window
	Removed int64      `json:"removed"`
	// Tags: in use (with a live file), links between files and tags (all
	// origins), links added by the user, parents, siblings.
	Tags      int64 `json:"tags"`
	TagLinks  int64 `json:"tag_links"`
	UserLinks int64 `json:"user_links"`
	Parents   int64 `json:"parents"`
	Siblings  int64 `json:"siblings"`
	// Imports per day, oldest first (Days days ending today, in the caller's time zone).
	Imports   []int64    `json:"imports"`
	Since     int64      `json:"since"` // the first import (unix seconds; 0 = none)
	Sites     []SiteStat `json:"sites"`
	Subs      SubStat    `json:"subscriptions"`
	TookMs    float64    `json:"took_ms"`
	Untagged  int64      `json:"untagged"`
	Favorites int64      `json:"favorites"`
}

// Count is a number of files and their size.
type Count struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// KindStat is one kind of file (image, gif, video).
type KindStat struct {
	Kind string `json:"kind"`
	Count
}

// SiteStat is what came from one site: files in the library and posts looked at.
type SiteStat struct {
	Site  string `json:"site"`
	Files int64  `json:"files"`
	Posts int64  `json:"posts"`
}

// SubStat counts subscriptions.
type SubStat struct {
	Total   int64 `json:"total"`
	Paused  int64 `json:"paused"`
	Failing int64 `json:"failing"`
}

// kindOf groups the file types the way people talk about them.
func kindOf(t media.Type) string {
	switch t {
	case media.GIF:
		return "gif"
	case media.MP4, media.QuickTime, media.WebM, media.Matroska:
		return "video"
	default:
		return "image"
	}
}

// Stats counts the library. days: how many days of imports (ending today);
// tzOffset: the caller's offset from UTC in seconds (so days end at their midnight).
// The live files' numbers come from the index in one pass (the database's were seconds of scanning on a big
// library); the rest are small tables or partial indexes (migration 0043).
func (l *Library) Stats(ctx context.Context, days int, tzOffset int64) (Stats, error) {
	start := time.Now()
	st := Stats{Kinds: []KindStat{}, Sites: []SiteStat{}, Imports: []int64{}}
	q := l.db.R

	now := time.Now().Unix() + tzOffset
	today := now - now%86400 // the caller's midnight, in shifted seconds
	c := l.ix.Census(days, today-int64(max(days, 1)-1)*86400, tzOffset)
	kinds := map[string]*KindStat{}
	for t, n := range c.Types {
		k := kindOf(t)
		if kinds[k] == nil {
			kinds[k] = &KindStat{Kind: k}
		}
		kinds[k].Files += n[0]
		kinds[k].Bytes += n[1]
		st.Files += n[0]
		st.Bytes += n[1]
	}
	for _, k := range []string{"image", "gif", "video"} {
		if kinds[k] != nil {
			st.Kinds = append(st.Kinds, *kinds[k])
		}
	}
	st.Since = c.FirstImport / 1000
	st.Untagged = c.Untagged
	st.TagLinks = c.TagLinks
	if days > 0 {
		st.Imports = c.Imports
	}
	st.Tags = int64(l.ix.Stats().Tags)
	st.Favorites = int64(l.ix.Favorites())

	var err error
	one := func(dst ...any) func(string) {
		return func(query string) {
			if err == nil {
				err = q.QueryRowContext(ctx, query).Scan(dst...)
			}
		}
	}
	one(&st.Trash.Files, &st.Trash.Bytes)(`SELECT count(*), coalesce(sum(size), 0) FROM files WHERE deleted_at IS NOT NULL`)
	one(&st.Removed)(`SELECT count(*) FROM tombstones`)
	one(&st.UserLinks)(`SELECT count(*) FROM file_tags WHERE origin = 0`)
	one(&st.Parents)(`SELECT count(*) FROM tag_parents`)
	one(&st.Siblings)(`SELECT count(*) FROM tag_siblings`)
	one(&st.Subs.Total, &st.Subs.Paused, &st.Subs.Failing)(`SELECT count(*), coalesce(sum(paused != 0), 0),
		coalesce(sum(paused = 0 AND error IS NOT NULL AND error != ''), 0) FROM subscriptions`)
	if err != nil {
		return st, err
	}

	// Per site: files in the library from it (the index), posts looked at (seen_posts, along its key).
	files := l.ix.SiteFiles()
	rows, err := q.QueryContext(ctx, `SELECT s.id, s.name, coalesce(p.n, 0) FROM sites s
		LEFT JOIN (SELECT site_id, count(*) AS n FROM seen_posts GROUP BY site_id) p ON p.site_id = s.id`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var id uint32
		var s SiteStat
		if err := rows.Scan(&id, &s.Site, &s.Posts); err != nil {
			rows.Close()
			return st, err
		}
		if s.Files = files[id]; s.Files > 0 || s.Posts > 0 {
			st.Sites = append(st.Sites, s)
		}
	}
	rows.Close()
	slices.SortStableFunc(st.Sites, func(a, b SiteStat) int { return cmp.Compare(b.Files, a.Files) })
	st.TookMs = float64(time.Since(start).Microseconds()) / 1000
	return st, rows.Err()
}
