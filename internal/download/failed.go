package download

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// recordSeen writes a post's outcome (a failed one with why, and the search
// that met it).
func recordSeen(tx *sql.Tx, siteID uint32, postID int64, file any, outcome int, postErr error, query string, now int64) error {
	var errText any
	if outcome == Failed && postErr != nil {
		errText = postErr.Error()
	}
	var q any
	if query != "" {
		q = query
	}
	_, err := tx.Exec(`
		INSERT INTO seen_posts (site_id, post_id, file_id, outcome, seen_at, error, query) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (site_id, post_id) DO UPDATE SET file_id = coalesce(excluded.file_id, file_id),
			outcome = excluded.outcome, seen_at = excluded.seen_at, error = excluded.error,
			query = coalesce(excluded.query, query)`,
		siteID, postID, file, outcome, now, errText, q)
	return err
}

// FailedPost is a post whose file couldn't be brought in.
type FailedPost struct {
	Site  string `json:"site"`
	Post  int64  `json:"post,string"`     // as text: some sites' IDs (Mastodon's) are too big for a JavaScript number
	URL   string `json:"url,omitempty"`   // the post on its site
	Query string `json:"query,omitempty"` // the search that met it
	Error string `json:"error,omitempty"`
	At    int64  `json:"at"` // when (Unix ms)
}

// FailedPosts lists the failed posts, newest first (at most 500).
func (d *Downloader) FailedPosts(ctx context.Context) ([]FailedPost, error) {
	rows, err := d.db.R.QueryContext(ctx, `SELECT s.name, p.post_id, coalesce(p.query, ''), coalesce(p.error, ''), p.seen_at
		FROM seen_posts p JOIN sites s ON s.id = p.site_id WHERE p.outcome = ? ORDER BY p.seen_at DESC LIMIT 500`, Failed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := d.all()
	var out []FailedPost
	for rows.Next() {
		var f FailedPost
		if err := rows.Scan(&f.Site, &f.Post, &f.Query, &f.Error, &f.At); err != nil {
			return nil, err
		}
		if st := all[f.Site]; st != nil {
			f.URL = st.Client.Manifest().Post(f.Post)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// PostRef names a post on a site.
type PostRef struct {
	Site string `json:"site"`
	Post int64  `json:"post,string"`
}

// DismissFailed takes failed posts off the list: forgotten, as if never met (a later search that meets one tries it
// again). It returns how many went.
func (d *Downloader) DismissFailed(ctx context.Context, posts []PostRef) (int, error) {
	n := 0
	for _, p := range posts {
		res, err := d.db.W.ExecContext(ctx, `DELETE FROM seen_posts WHERE outcome = ? AND post_id = ?
			AND site_id = (SELECT id FROM sites WHERE name = ?)`, Failed, p.Post, p.Site)
		if err != nil {
			return n, fmt.Errorf("download: %w", err)
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	if n > 0 {
		d.Changed()
	}
	return n, nil
}

// ErrNotFailed means the post isn't a failed one (any more).
var ErrNotFailed = errors.New("that post didn't fail")

// RetryPost tries a failed post again now (looked up on its site, then like
// any post of a search) and returns how it went: "downloaded", "already",
// "failed" (with the error), "skipped" (gone from the site), ...
func (d *Downloader) RetryPost(ctx context.Context, site string, post int64) (string, error) {
	// One at a time per post: pressed again while it runs, it waits for that one (four presses fetched the
	// file four times at once).
	key := site + "/" + strconv.FormatInt(post, 10)
	d.mu.Lock()
	if d.retrying == nil {
		d.retrying = map[string]*retryCall{}
	}
	if c, ok := d.retrying[key]; ok {
		d.mu.Unlock()
		select {
		case <-c.done:
			return c.result, c.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	c := &retryCall{done: make(chan struct{})}
	d.retrying[key] = c
	d.mu.Unlock()
	c.result, c.err = d.retryPost(ctx, site, post)
	d.mu.Lock()
	delete(d.retrying, key)
	d.mu.Unlock()
	close(c.done)
	return c.result, c.err
}

type retryCall struct {
	done   chan struct{}
	result string
	err    error
}

func (d *Downloader) retryPost(ctx context.Context, site string, post int64) (result string, err error) {
	began := d.now()
	d.log.Debug("retry post", "site", site, "post", post)
	defer func() {
		d.log.Info("retried post", "site", site, "post", post, "outcome", result, "took", d.now().Sub(began).Round(time.Millisecond), "err", err)
	}()
	st, ok := d.all()[site]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	var prev int
	var query sql.NullString // (the search that met it: the file is "found by" it, as from the search)
	if err := d.db.R.QueryRowContext(ctx, "SELECT outcome, query FROM seen_posts WHERE site_id = ? AND post_id = ?", st.ID, post).Scan(&prev, &query); err != nil || prev != Failed {
		return "", ErrNotFailed
	}
	var outcome int
	bl, err := d.lib.Blacklist(ctx)
	if err != nil {
		return "", err
	}
	rules, err := d.lib.DownloadRules(ctx)
	if err != nil {
		return "", err
	}
	p, err := st.Engine.Lookup(ctx, post)
	if err == nil {
		d.dated(ctx, st, p)
	}
	var fileID uint32
	switch {
	case err != nil:
		outcome = Failed
	case p == nil:
		outcome = Skipped // gone from the site
	default:
		// (someone waits for it: not behind a running search's files)
		// (its bytes show like a search's file, under its site; nothing waits behind a running search)
		f := &fileProgress{post: post, site: site}
		f.total.Store(-1)
		tctx := context.WithValue(plugin.Urgent(ctx), progressKey{}, f)
		outcome, fileID, err = d.handle(tctx, st, *p, bl, rules, true)
		d.live.mu.Lock()
		delete(d.live.files, f)
		d.live.mu.Unlock()
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	tx, terr := d.db.W.Begin()
	if terr != nil {
		return "", terr
	}
	defer tx.Rollback()
	var file any
	if fileID != 0 {
		file = fileID
	}
	if terr := recordSeen(tx, st.ID, post, file, outcome, err, "", d.now().UnixMilli()); terr != nil {
		return "", terr
	}
	var foundBy int64 // (the subscription that downloaded it: system:found_by=, once committed)
	if q := strings.TrimSpace(query.String); fileID != 0 && q != "" && (outcome == Downloaded || outcome == Already) {
		var sub any
		var id int64
		if tx.QueryRow("SELECT id FROM subscriptions WHERE site_id = ? AND query = ?", st.ID, q).Scan(&id) == nil {
			sub = id
			if outcome == Downloaded {
				foundBy = id
			}
		}
		if _, terr := tx.Exec(`INSERT INTO file_sources (file_id, site_id, query, subscription_id, downloaded, first_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, fileID, st.ID, q, sub, outcome == Downloaded, d.now().UnixMilli()); terr != nil {
			return "", terr
		}
	}
	if terr := tx.Commit(); terr != nil {
		return "", terr
	}
	if foundBy != 0 {
		d.lib.Index().AddFoundBy(fileID, uint32(foundBy))
	}
	d.Changed()
	name := outcomeNames[outcome]
	if outcome == Failed && err != nil {
		return name, err
	}
	return name, nil
}

var outcomeNames = map[int]string{
	Downloaded: "downloaded", Already: "already", Deleted: "deleted", Failed: "failed", Skipped: "skipped",
	Blacklisted: "blacklisted", TooBig: "too big",
}

// dated fills in a looked-up post's upload time when the lookup didn't bring one (rule34's answer for one post has
// none; its search's does): the site's search for that post. Only gelbooru-style sites (their search takes id:N);
// on an error it stays unknown.
func (d *Downloader) dated(ctx context.Context, st *Site, p *plugin.Post) {
	if p == nil || !p.Posted.IsZero() || st.Client.Manifest().Engine != "gelbooru-v02" {
		return
	}
	page, err := st.Engine.Search(ctx, "id:"+strconv.FormatInt(p.ID, 10), 0)
	if err != nil {
		return
	}
	for _, q := range page.Posts {
		if q.ID == p.ID && !q.Posted.IsZero() {
			p.Posted = q.Posted
			return
		}
	}
}

// RetryJob is a failed post being tried again in the background - or just tried (kept a few minutes, for its
// result).
type RetryJob struct {
	Site     string `json:"site"`
	Post     int64  `json:"post,string"`
	Query    string `json:"query,omitempty"`
	Started  int64  `json:"started"`
	Finished int64  `json:"finished,omitempty"`
	Outcome  string `json:"outcome,omitempty"` // once finished: as RetryPost's
	Error    string `json:"error,omitempty"`
}

// retryKept is how long a finished retry stays listed.
const retryKept = 5 * time.Minute

// StartRetry tries a failed post again in the background (once: already being tried, nothing more starts).
// The queue's watchers hear when it starts and ends; Retries lists it.
func (d *Downloader) StartRetry(ctx context.Context, site string, post int64) error {
	st, ok := d.all()[site]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	var prev int
	var query sql.NullString
	if err := d.db.R.QueryRowContext(ctx, "SELECT outcome, query FROM seen_posts WHERE site_id = ? AND post_id = ?", st.ID, post).Scan(&prev, &query); err != nil || prev != Failed {
		return ErrNotFailed
	}
	key := site + "/" + strconv.FormatInt(post, 10)
	d.mu.Lock()
	if d.retryJobs == nil {
		d.retryJobs = map[string]*RetryJob{}
	}
	if j := d.retryJobs[key]; j != nil && j.Finished == 0 {
		d.mu.Unlock()
		return nil
	}
	job := &RetryJob{Site: site, Post: post, Query: query.String, Started: d.now().UnixMilli()}
	d.retryJobs[key] = job
	run := d.runCtx
	d.mu.Unlock()
	if run == nil {
		run = context.Background()
	}
	d.Changed()
	go func() {
		outcome, err := d.RetryPost(run, site, post)
		d.mu.Lock()
		job.Finished, job.Outcome = d.now().UnixMilli(), outcome
		if err != nil {
			job.Error = err.Error()
			if job.Outcome == "" {
				job.Outcome = "failed"
			}
		}
		d.mu.Unlock()
		d.Changed()
	}()
	return nil
}

// Retries lists the failed posts being tried again, and those tried in the last few minutes (oldest first).
func (d *Downloader) Retries() []RetryJob {
	d.mu.Lock()
	defer d.mu.Unlock()
	cut := d.now().Add(-retryKept).UnixMilli()
	out := []RetryJob{}
	for k, j := range d.retryJobs {
		if j.Finished != 0 && j.Finished < cut {
			delete(d.retryJobs, k)
			continue
		}
		out = append(out, *j)
	}
	slices.SortFunc(out, func(a, b RetryJob) int { return cmp.Compare(a.Started, b.Started) })
	return out
}
