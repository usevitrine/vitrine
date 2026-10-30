package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// Tag refresh: sites keep editing posts' tags after they're downloaded, so
// every post the library has from a site is read again now and then and its
// file gets the site's current tags (with namespaces), links and rating.
//
// A post's turn comes a quarter of its age after it was last read (seen_at:
// set by downloads, searches that meet the post again, and refreshes),
// between a day and half a year: a 4-day-old post daily, a 2-month-old one
// every two weeks, anything over two years old twice a year. Refreshes run
// one post at a time while the site has no searches to work on (see
// background), at a gentle pace.

const (
	refreshMin = 24 * time.Hour
	refreshMax = 182 * 24 * time.Hour
)

// dueSQL selects a site's posts with a live file and when each is due: a quarter of the post's age after it was
// last read, at least refreshMin, at most refreshMax
// (?1 site, ?2 now, ?3 min and ?4 max interval in ms). The age counts from the
// upload, or the import when the upload time is unknown.
const dueSQL = `
	SELECT sp.post_id, sp.file_id,
		sp.seen_at + min(max((?2 - coalesce(f.posted_at, f.imported_at)) / 4, ?3), ?4) AS due
	FROM seen_posts sp JOIN files f ON f.id = sp.file_id AND f.deleted_at IS NULL
	WHERE sp.site_id = ?1`

func (d *Downloader) dueArgs(st *Site) []any {
	return []any{st.ID, d.now().UnixMilli(), refreshMin.Milliseconds(), refreshMax.Milliseconds()}
}

// RefreshDue refreshes the site's most overdue post, if one is due. It
// reports whether there was one.
func (d *Downloader) RefreshDue(ctx context.Context, site string) (bool, error) {
	st, ok := d.all()[site]
	if !ok {
		return false, fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	var postID int64
	var fileID uint32
	var due int64
	err := d.db.R.QueryRowContext(ctx, "SELECT * FROM ("+dueSQL+") WHERE due <= ?2 ORDER BY due LIMIT 1",
		d.dueArgs(st)...).Scan(&postID, &fileID, &due)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("download: %w", err)
	}
	_, err = d.refreshPost(ctx, st, postID, fileID)
	return true, err
}

// RefreshStatus is how far a site's tag refresh is.
type RefreshStatus struct {
	Site  string `json:"site"`
	Posts int    `json:"posts"` // posts with a file in the library
	Due   int    `json:"due"`   // due now
	// Paused: the site kept failing to answer - its refresh waits until it does.
	Paused bool `json:"paused,omitempty"`
}

// statusEvery is how often the Downloads page's slow numbers are worked out again (2026-10-30: a CPU profile of a
// 583k-file library while downloading - the page asks for them on every change, up to twice a second, and counting
// a site's due posts reads all of them: two thirds of Vitrine's own processor time).
const statusEvery = 30 * time.Second

// RefreshStatus counts due posts per site: worked out at most every statusEvery (Refresh now - ScheduleRefresh -
// starts again); a site's pause is always current.
func (d *Downloader) RefreshStatus(ctx context.Context) ([]RefreshStatus, error) {
	d.statusMu.Lock()
	defer d.statusMu.Unlock() // (one at a time: requests at once share the answer)
	now := d.now()
	if d.refreshOut == nil || now.Sub(d.refreshAt) >= statusEvery || now.Before(d.refreshAt) {
		out, err := d.refreshStatus(ctx)
		if err != nil {
			return nil, err
		}
		d.refreshOut, d.refreshAt = out, now
	}
	out := slices.Clone(d.refreshOut)
	all := d.all()
	for i := range out {
		if st := all[out[i].Site]; st != nil {
			out[i].Paused = !st.Client.Paused().Until.IsZero()
		}
	}
	return out, nil
}

func (d *Downloader) refreshStatus(ctx context.Context) ([]RefreshStatus, error) {
	out := []RefreshStatus{}
	for _, site := range d.Sites() {
		st := d.all()[site]
		s := RefreshStatus{Site: site}
		err := d.db.R.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(due <= ?2), 0) FROM ("+dueSQL+")",
			d.dueArgs(st)...).Scan(&s.Posts, &s.Due)
		if err != nil {
			return nil, fmt.Errorf("download: %w", err)
		}
		s.Paused = !st.Client.Paused().Until.IsZero()
		if s.Posts > 0 {
			out = append(out, s)
		}
	}
	return out, nil
}

// ScheduleRefresh makes the posts behind these files due now (first in line
// when their site is next idle). Returns how many posts that is.
func (d *Downloader) ScheduleRefresh(ctx context.Context, files []uint32) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}
	n := 0
	for chunk := range chunks(files, 500) {
		args := make([]any, len(chunk))
		for i, f := range chunk {
			args[i] = f
		}
		res, err := d.db.W.ExecContext(ctx, "UPDATE seen_posts SET seen_at = 0 WHERE file_id IN (?"+
			strings.Repeat(",?", len(chunk)-1)+")", args...)
		if err != nil {
			return n, fmt.Errorf("download: %w", err)
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	d.statusMu.Lock()
	d.refreshOut = nil // (counted again: these are due now)
	d.statusMu.Unlock()
	for site := range d.all() {
		d.wake(site)
	}
	return n, nil
}

func chunks[T any](s []T, n int) func(func([]T) bool) {
	return func(yield func([]T) bool) {
		for len(s) > 0 {
			k := min(n, len(s))
			if !yield(s[:k]) {
				return
			}
			s = s[k:]
		}
	}
}

// RefreshFile reads the file's posts from their sites again now (waiting for
// each site's rate limit) and returns how many the sites still have. A post on a
// paused site (it kept failing to answer) isn't asked: it's queued - first in its
// site's tag refresh once the site answers again (queued counts those).
func (d *Downloader) RefreshFile(ctx context.Context, file uint32) (found, queued int, err error) {
	rows, err := d.db.R.QueryContext(ctx, `SELECT si.name, sp.post_id FROM seen_posts sp
		JOIN sites si ON si.id = sp.site_id WHERE sp.file_id = ?`, file)
	if err != nil {
		return 0, 0, fmt.Errorf("download: %w", err)
	}
	type post struct {
		site string
		id   int64
	}
	var posts []post
	for rows.Next() {
		var p post
		if err := rows.Scan(&p.site, &p.id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		posts = append(posts, p)
	}
	rows.Close()
	if len(posts) == 0 {
		return 0, 0, ErrNoPosts
	}
	for _, p := range posts {
		st, ok := d.all()[p.site]
		if !ok {
			continue // its plugin is gone
		}
		if st.Client.Waiting() {
			// Paused: due now - its site's refresh reads it first once the site answers again.
			if _, err := d.db.W.ExecContext(ctx, "UPDATE seen_posts SET seen_at = 0 WHERE site_id = ? AND post_id = ?", st.ID, p.id); err != nil {
				return found, queued, fmt.Errorf("download: %w", err)
			}
			queued++
			continue
		}
		ok, err := d.refreshPost(ctx, st, p.id, file)
		if err != nil {
			return found, queued, err
		}
		if ok {
			found++
		}
	}
	return found, queued, nil
}

// waitingErr is the error for a site that's paused and not due to try again yet (nil: it may be asked).
func waitingErr(st *Site) error {
	if !st.Client.Waiting() {
		return nil
	}
	p := st.Client.Paused()
	return &plugin.PausedError{Site: st.Client.Manifest().Site, Until: p.Until, Reason: p.Reason}
}

// ErrNoPosts means a file didn't come from any site (nothing to refresh).
var ErrNoPosts = errors.New("this file didn't come from a site")

// scheduleUndated puts the posts of files without an upload time first in their site's tag refresh (once, at
// start): a refresh asks the site for it (see dated). Only sites that can tell (gelbooru-style); one that
// still can't goes back to its usual turn.
func (d *Downloader) scheduleUndated(ctx context.Context) (int, error) {
	n := 0
	for _, st := range d.all() {
		if st.Client.Manifest().Engine != "gelbooru-v02" {
			continue
		}
		res, err := d.db.W.ExecContext(ctx, `UPDATE seen_posts SET seen_at = 0
			WHERE site_id = ? AND seen_at > 0 AND outcome IN (?, ?)
			  AND file_id IN (SELECT id FROM files WHERE posted_at IS NULL AND deleted_at IS NULL)`, st.ID, Downloaded, Already)
		if err != nil {
			return n, fmt.Errorf("download: %w", err)
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, nil
}
