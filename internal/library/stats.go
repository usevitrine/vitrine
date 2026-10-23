package library

import (
	"context"
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
func (l *Library) Stats(ctx context.Context, days int, tzOffset int64) (Stats, error) {
	start := time.Now()
	st := Stats{Kinds: []KindStat{}, Sites: []SiteStat{}, Imports: []int64{}}
	q := l.db.R

	rows, err := q.QueryContext(ctx, `SELECT type, count(*), coalesce(sum(size), 0) FROM files WHERE deleted_at IS NULL AND flags & 64 = 0 GROUP BY type`)
	if err != nil {
		return st, err
	}
	kinds := map[string]*KindStat{}
	for rows.Next() {
		var t media.Type
		var c Count
		if err := rows.Scan(&t, &c.Files, &c.Bytes); err != nil {
			rows.Close()
			return st, err
		}
		k := kindOf(t)
		if kinds[k] == nil {
			kinds[k] = &KindStat{Kind: k}
		}
		kinds[k].Files += c.Files
		kinds[k].Bytes += c.Bytes
		st.Files += c.Files
		st.Bytes += c.Bytes
	}
	rows.Close()
	for _, k := range []string{"image", "gif", "video"} {
		if kinds[k] != nil {
			st.Kinds = append(st.Kinds, *kinds[k])
		}
	}

	one := func(dst any, query string, args ...any) {
		if err == nil {
			err = q.QueryRowContext(ctx, query, args...).Scan(dst)
		}
	}
	one(&st.Trash.Files, `SELECT count(*) FROM files WHERE deleted_at IS NOT NULL`)
	one(&st.Trash.Bytes, `SELECT coalesce(sum(size), 0) FROM files WHERE deleted_at IS NOT NULL`)
	one(&st.Removed, `SELECT count(*) FROM tombstones`)
	one(&st.TagLinks, `SELECT count(*) FROM file_tags`)
	one(&st.UserLinks, `SELECT count(*) FROM file_tags WHERE origin = 0`)
	one(&st.Parents, `SELECT count(*) FROM tag_parents`)
	one(&st.Siblings, `SELECT count(*) FROM tag_siblings`)
	one(&st.Since, `SELECT coalesce(min(imported_at), 0) / 1000 FROM files WHERE deleted_at IS NULL AND flags & 64 = 0`)
	one(&st.Untagged, `SELECT count(*) FROM files f WHERE deleted_at IS NULL AND flags & 64 = 0 AND NOT EXISTS (SELECT 1 FROM file_tags t WHERE t.file_id = f.id)`)
	one(&st.Subs.Total, `SELECT count(*) FROM subscriptions`)
	one(&st.Subs.Paused, `SELECT count(*) FROM subscriptions WHERE paused != 0`)
	one(&st.Subs.Failing, `SELECT count(*) FROM subscriptions WHERE paused = 0 AND error IS NOT NULL AND error != ''`)
	if err != nil {
		return st, err
	}
	st.Tags = int64(l.ix.Stats().Tags)
	st.Favorites = int64(l.ix.Favorites())

	// Imports per day.
	if days > 0 {
		st.Imports = make([]int64, days)
		now := time.Now().Unix() + tzOffset
		today := now - now%86400 // the caller's midnight, in shifted seconds
		first := today - int64(days-1)*86400
		rows, err := q.QueryContext(ctx, `SELECT (imported_at / 1000 + ?1 - ?2) / 86400, count(*) FROM files
			WHERE deleted_at IS NULL AND flags & 64 = 0 AND imported_at / 1000 + ?1 >= ?2 GROUP BY 1`, tzOffset, first)
		if err != nil {
			return st, err
		}
		for rows.Next() {
			var d, n int64
			if err := rows.Scan(&d, &n); err != nil {
				rows.Close()
				return st, err
			}
			if d >= 0 && d < int64(days) {
				st.Imports[d] = n
			}
		}
		rows.Close()
	}

	// Per site: files in the library it gave (downloaded or already had), posts looked at.
	rows, err = q.QueryContext(ctx, `SELECT s.name,
			count(DISTINCT CASE WHEN f.deleted_at IS NULL THEN p.file_id END),
			count(*)
		FROM seen_posts p JOIN sites s ON s.id = p.site_id LEFT JOIN files f ON f.id = p.file_id
		GROUP BY p.site_id ORDER BY 2 DESC`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var s SiteStat
		if err := rows.Scan(&s.Site, &s.Files, &s.Posts); err != nil {
			rows.Close()
			return st, err
		}
		st.Sites = append(st.Sites, s)
	}
	rows.Close()
	st.TookMs = float64(time.Since(start).Microseconds()) / 1000
	return st, rows.Err()
}
