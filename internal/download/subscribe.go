package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
)

// Subscriptions: a search checked again on a schedule for new posts.
//
// A check is an ordinary search in the site's queue (so it shows progress
// and can be paused like any other) that stops at the first post at or below
// the subscription's high_id - the newest post the previous check reached.
// The first check goes through the whole search (or initial_max posts).
// When a check is done its counts move to the subscription and the search row
// is removed; a failed check stays in the list (Retry continues it) and is
// retried automatically after at most an hour.

// ErrExists means a subscription for that site and search exists already.
var ErrExists = errors.New("already subscribed to that search")

// ErrEmptyQuery means the search is empty.
var ErrEmptyQuery = errors.New("the search is empty")

// ErrInterval means a check interval is out of range.
var ErrInterval = errors.New("the check interval must be between 1 hour and 1 year")

// Check intervals: at least an hour (a site's rate limit is shared by every
// subscription), at most a year.
const (
	MinInterval = time.Hour
	maxInterval = 365 * 24 * time.Hour
	retryAfter  = time.Hour // a failed check is tried again after this (or the interval, if shorter)
)

// Subscription is a search checked on a schedule.
type Subscription struct {
	ID          int64   `json:"id"`
	Site        string  `json:"site"`
	Query       string  `json:"query"`
	Label       string  `json:"label,omitempty"` // its name where the query says little (a gallery: its title); "" = the query
	Interval    int64   `json:"interval"`        // seconds between checks
	Paused      bool    `json:"paused"`
	InitialMax  int     `json:"initial_max"`
	Checks      int     `json:"checks"`          // finished checks
	Downloaded  int     `json:"downloaded"`      // files downloaded, all checks
	LastNew     int     `json:"last_new"`        // files the last finished check downloaded
	Pages       int     `json:"pages,omitempty"` // pages its checks brought in for galleries (a gallery site)
	Error       string  `json:"error,omitempty"`
	CreatedAt   int64   `json:"created_at"`
	LastCheckAt int64   `json:"last_check_at,omitempty"`
	NextCheckAt int64   `json:"next_check_at"`
	Check       *Search `json:"check,omitempty"`    // the check in progress (or paused or failed)
	GroupID     int64   `json:"group_id,omitempty"` // its group (0 = none)
	// CheckDepth: how many posts its checks go on past the newest one the last check saw,
	// if set on it (nil = from its group, site or the setting for all); Depth: the one used.
	CheckDepth *int `json:"check_depth,omitempty"`
	Depth      int  `json:"depth"`
	// FavGone: files of its posts no longer in the site's favorites (a favorites
	// subscription), waiting for the user to delete or keep them.
	FavGone int `json:"fav_gone,omitempty"`
	// LastFoundAt: when it last brought in a new file (Unix ms; 0 = never).
	LastFoundAt int64 `json:"last_found_at,omitempty"`
	highID      int64
	siteID      uint32
}

func checkInterval(d time.Duration) (int64, error) {
	if d < MinInterval || d > maxInterval {
		return 0, ErrInterval
	}
	return int64(d / time.Second), nil
}

// Subscribe adds a subscription; its first check starts as soon as the site's
// worker is free. initialMax > 0 limits the first check to the newest posts.
func (d *Downloader) Subscribe(ctx context.Context, site, query string, every time.Duration, initialMax int) (int64, error) {
	return d.SubscribeTo(ctx, site, query, every, initialMax, false)
}

// SubscribeTo is Subscribe; favorite: the files its checks bring in become favorites too.
func (d *Downloader) SubscribeTo(ctx context.Context, site, query string, every time.Duration, initialMax int, favorite bool) (int64, error) {
	st, ok := d.all()[site]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, ErrEmptyQuery
	}
	iv, err := checkInterval(every)
	if err != nil {
		return 0, err
	}
	id, err := d.insertSubscription(ctx, d.db.W, st.ID, site, query, iv, max(0, initialMax), 0, favorite)
	if err != nil {
		return 0, err
	}
	return id, d.favoritesGroup(ctx, site, query, id)
}

// LabelSubscription names a subscription (a gallery's title for its link).
func (d *Downloader) LabelSubscription(ctx context.Context, id int64, label string) error {
	if _, err := d.db.W.ExecContext(ctx, "UPDATE subscriptions SET label = nullif(?, '') WHERE id = ?", strings.TrimSpace(label), id); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	d.Changed()
	return nil
}

// FavoritesGroup is the group a subscription to a user's favorites goes in (one per site).
const FavoritesGroup = "Favorites"

// favoritesGroup puts a subscription to a user's favorites in its site's
// Favorites group (made the first time).
func (d *Downloader) favoritesGroup(ctx context.Context, site, query string, subID int64) error {
	if !plugin.IsFavorites(query) {
		return nil
	}
	g, err := d.CreateGroup(ctx, site, FavoritesGroup)
	if err != nil {
		return err
	}
	return d.SetGroup(ctx, subID, g)
}

type execer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (d *Downloader) insertSubscription(ctx context.Context, q execer, siteID uint32, site, query string, iv int64,
	initialMax int, highID int64, favorite bool) (int64, error) {
	now := d.now().UnixMilli()
	var id int64
	err := q.QueryRowContext(ctx, `
		INSERT INTO subscriptions (site_id, query, interval_s, initial_max, high_id, created_at, next_check_at, favorite)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (site_id, query) DO NOTHING RETURNING id`,
		siteID, query, iv, initialMax, highID, now, now, favorite).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s on %s", ErrExists, query, site)
	}
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	d.kick()
	return id, nil
}

// SubscribeSearch turns a search into a subscription without overlap: the
// subscription only looks at posts newer than the newest one the search
// reached, and the search itself carries on (or stays finished). A search that
// hasn't started yet is replaced by the subscription's first check.
func (d *Downloader) SubscribeSearch(ctx context.Context, searchID int64, every time.Duration) (int64, error) {
	iv, err := checkInterval(every)
	if err != nil {
		return 0, err
	}
	s, err := d.Get(ctx, searchID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoSearch
	}
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	if s.SubscriptionID != 0 {
		return 0, fmt.Errorf("%w: that is a subscription's check", ErrState)
	}
	tx, err := d.db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	defer tx.Rollback()
	initialMax := 0
	if s.HighID == 0 && s.State == StateQueued {
		// Not started: the first check does its job.
		initialMax = s.MaxPosts
		if _, err := tx.ExecContext(ctx, "DELETE FROM searches WHERE id = ?", s.ID); err != nil {
			return 0, fmt.Errorf("download: %w", err)
		}
	}
	var favorite bool // (the search's setting goes with it)
	tx.QueryRowContext(ctx, "SELECT favorite FROM searches WHERE id = ?", s.ID).Scan(&favorite)
	id, err := d.insertSubscription(ctx, tx, s.siteID, s.Site, strings.TrimSpace(s.Query), iv, initialMax, s.HighID, favorite)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	d.kick()
	return id, d.favoritesGroup(ctx, s.Site, s.Query, id)
}

const subColumns = `sub.id, si.name, sub.site_id, sub.query, sub.interval_s, sub.paused, sub.initial_max, sub.checks,
	sub.downloaded, sub.last_new, coalesce(sub.error, ''), sub.created_at, coalesce(sub.last_check_at, 0),
	sub.next_check_at, sub.high_id, coalesce(sub.group_id, 0), sub.check_depth,
	sub.pages, coalesce(sub.label, (SELECT title FROM comics c WHERE rtrim(c.url, '/') = rtrim(sub.query, '/') AND c.deleted_at IS NULL LIMIT 1), '')`

func scanSub(row interface{ Scan(...any) error }) (Subscription, error) {
	var s Subscription
	err := row.Scan(&s.ID, &s.Site, &s.siteID, &s.Query, &s.Interval, &s.Paused, &s.InitialMax, &s.Checks,
		&s.Downloaded, &s.LastNew, &s.Error, &s.CreatedAt, &s.LastCheckAt, &s.NextCheckAt, &s.highID, &s.GroupID, &s.CheckDepth, &s.Pages, &s.Label)
	return s, err
}

// Subscriptions lists every subscription by site and search, each with its
// check in progress, if any.
func (d *Downloader) Subscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := d.db.R.QueryContext(ctx, "SELECT "+subColumns+
		" FROM subscriptions sub JOIN sites si ON si.id = sub.site_id ORDER BY si.name, sub.query")
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	var out []Subscription
	for rows.Next() {
		s, err := scanSub(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	checks, err := d.checks(ctx)
	if err != nil {
		return nil, err
	}
	gone, err := d.favGoneCounts(ctx)
	if err != nil {
		return nil, err
	}
	depths, err := d.depthSource(ctx)
	if err != nil {
		return nil, err
	}
	found, err := d.lastFound(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Depth = depths.of(out[i])
		out[i].LastFoundAt = found[out[i].ID]
		if c, ok := checks[out[i].ID]; ok {
			out[i].Check = &c
		}
		out[i].FavGone = gone[out[i].ID]
	}
	return out, nil
}

// GetSubscription returns one subscription.
func (d *Downloader) GetSubscription(ctx context.Context, id int64) (Subscription, error) {
	s, err := scanSub(d.db.R.QueryRowContext(ctx, "SELECT "+subColumns+
		" FROM subscriptions sub JOIN sites si ON si.id = sub.site_id WHERE sub.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNoSubscription
	}
	if err != nil {
		return s, fmt.Errorf("download: %w", err)
	}
	if c, ok, err := d.check(ctx, id); err != nil {
		return s, err
	} else if ok {
		s.Check = &c
	}
	if gone, err := d.favGoneCounts(ctx); err == nil {
		s.FavGone = gone[id]
	}
	if depths, err := d.depthSource(ctx); err == nil {
		s.Depth = depths.of(s)
	}
	return s, nil
}

// ErrNoSubscription means there is no subscription with that ID.
var ErrNoSubscription = errors.New("no such subscription")

// checks returns every subscription's check in progress, by subscription.
func (d *Downloader) checks(ctx context.Context) (map[int64]Search, error) {
	rows, err := d.db.R.QueryContext(ctx, "SELECT "+searchColumns+
		" FROM searches s JOIN sites si ON si.id = s.site_id WHERE s.subscription_id IS NOT NULL")
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer rows.Close()
	out := map[int64]Search{}
	for rows.Next() {
		s, err := scanSearch(rows)
		if err != nil {
			return nil, err
		}
		out[s.SubscriptionID] = s
	}
	return out, rows.Err()
}

func (d *Downloader) check(ctx context.Context, subID int64) (Search, bool, error) {
	s, err := scanSearch(d.db.R.QueryRowContext(ctx, "SELECT "+searchColumns+
		" FROM searches s JOIN sites si ON si.id = s.site_id WHERE s.subscription_id = ?", subID))
	if errors.Is(err, sql.ErrNoRows) {
		return s, false, nil
	}
	return s, err == nil, err
}

// SubscriptionChange is an edit of a subscription; nil fields stay as they are.
type SubscriptionChange struct {
	Interval *time.Duration
	Paused   *bool
	// CheckDepth: set its own (>= 0), or -1 to go back to its group's, site's or the setting for all.
	CheckDepth *int
}

// UpdateSubscription changes the check interval or pauses/resumes a
// subscription. Pausing also pauses its check in progress (the file in flight
// finishes); resuming resumes it.
func (d *Downloader) UpdateSubscription(ctx context.Context, id int64, c SubscriptionChange) error {
	sub, err := d.GetSubscription(ctx, id)
	if err != nil {
		return err
	}
	if c.Interval != nil {
		iv, err := checkInterval(*c.Interval)
		if err != nil {
			return err
		}
		// The next check moves with the interval (counted from the last one).
		from := sub.LastCheckAt
		if from == 0 {
			from = sub.CreatedAt
		}
		if _, err := d.db.W.ExecContext(ctx, "UPDATE subscriptions SET interval_s = ?, next_check_at = ? WHERE id = ?",
			iv, from+iv*1000, id); err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}
	if c.CheckDepth != nil {
		var v any // (NULL: from its group, site or the setting for all)
		if *c.CheckDepth >= 0 {
			v = min(*c.CheckDepth, library.MaxCheckDepth)
		}
		if _, err := d.db.W.ExecContext(ctx, "UPDATE subscriptions SET check_depth = ? WHERE id = ?", v, id); err != nil {
			return fmt.Errorf("download: %w", err)
		}
	}
	if c.Paused != nil && *c.Paused != sub.Paused {
		if _, err := d.db.W.ExecContext(ctx, "UPDATE subscriptions SET paused = ? WHERE id = ?", *c.Paused, id); err != nil {
			return fmt.Errorf("download: %w", err)
		}
		if ch := sub.Check; ch != nil {
			switch {
			case *c.Paused && (ch.State == StateQueued || ch.State == StateRunning):
				err = d.Pause(ctx, ch.ID)
			case !*c.Paused && ch.State == StatePaused:
				err = d.Resume(ctx, ch.ID)
			}
			if err != nil && !errors.Is(err, ErrState) {
				return err
			}
		}
	}
	d.kick()
	return nil
}

// CheckNow starts a check at once (or resumes a paused or failed one), even
// for a paused subscription.
func (d *Downloader) CheckNow(ctx context.Context, id int64) error {
	sub, err := d.GetSubscription(ctx, id)
	if err != nil {
		return err
	}
	if ch := sub.Check; ch != nil {
		if ch.State == StatePaused || ch.State == StateFailed {
			return d.Resume(ctx, ch.ID)
		}
		return nil // already waiting or running
	}
	return d.startCheck(ctx, sub, true) // asked for now: next in line
}

// Unsubscribe removes a subscription and stops its check in progress. The
// files it downloaded stay in the library.
func (d *Downloader) Unsubscribe(ctx context.Context, id int64) error {
	c, ok, err := d.check(ctx, id)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if ok {
		d.stop(c.ID)
	}
	res, err := d.db.W.ExecContext(ctx, "DELETE FROM subscriptions WHERE id = ?", id) // the check goes with it
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoSubscription
	}
	return nil
}

// startCheck queues a check. A due check goes to the end of the site's queue
// (downloads you added go first); one asked for with Check now goes to the
// front (after the search running now).
func (d *Downloader) startCheck(ctx context.Context, sub Subscription, now bool) error {
	if _, ok := d.all()[sub.Site]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSite, sub.Site)
	}
	pos := "(SELECT coalesce(max(position), 0) + 1 FROM searches WHERE site_id = ?)"
	if now && sub.highID != 0 { // (a first check may be the whole search: always last)
		pos = "(SELECT coalesce(min(position), 1) - 1 FROM searches WHERE site_id = ?)"
	}
	maxPosts := 0
	if sub.highID == 0 {
		maxPosts = sub.InitialMax // the first check: up to the subscription's first-check limit
	}
	depth := 0
	if depths, err := d.depthSource(ctx); err == nil {
		depth = depths.of(sub)
	}
	_, err := d.db.W.ExecContext(ctx, `
		INSERT INTO searches (site_id, query, max_posts, stop_id, subscription_id, position, created_at, favorite, stop_depth)
		SELECT ?, ?, ?, ?, ?, `+pos+`, ?, (SELECT favorite FROM subscriptions WHERE id = ?), ?
		WHERE NOT EXISTS (SELECT 1 FROM searches WHERE subscription_id = ?)`,
		sub.siteID, sub.Query, maxPosts, sub.highID, sub.ID, sub.siteID, d.now().UnixMilli(), sub.ID, depth, sub.ID)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	d.wake(sub.Site)
	return nil
}

// finishCheck ends a subscription's check: done moves its counts to the
// subscription and removes it; failed keeps it (Retry continues it) and
// schedules a retry.
func (d *Downloader) finishCheck(s Search, state, msg string) {
	defer d.Changed()
	now := d.now().UnixMilli()
	// For notifications: what the check found, and whether the last one failed too.
	var downloaded, checks int
	var failedBefore bool
	var pages int
	d.db.R.QueryRow("SELECT downloaded, pages FROM searches WHERE id = ?", s.ID).Scan(&downloaded, &pages)
	d.db.R.QueryRow("SELECT error IS NOT NULL, checks FROM subscriptions WHERE id = ?", s.SubscriptionID).Scan(&failedBefore, &checks)
	err := func() error {
		tx, err := d.db.W.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if state == StateDone {
			if _, err := tx.Exec(`
				UPDATE subscriptions SET high_id = max(subscriptions.high_id, c.high_id), checks = checks + 1,
					downloaded = subscriptions.downloaded + c.downloaded, last_new = c.downloaded, error = NULL,
					pages = subscriptions.pages + c.pages,
					last_check_at = ?, next_check_at = ? + interval_s * 1000 + CAST(min(interval_s * 100, 1800000) * ? AS INTEGER)
				FROM (SELECT high_id, downloaded, pages FROM searches WHERE id = ?) AS c
				WHERE subscriptions.id = ?`, now, now, d.jitter(), s.ID, s.SubscriptionID); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM searches WHERE id = ?", s.ID); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec("UPDATE searches SET state = ?, error = nullif(?, ''), finished_at = ? WHERE id = ?",
				state, msg, now, s.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE subscriptions SET error = nullif(?, ''), last_check_at = ?,
				next_check_at = ? + min(interval_s, ?) * 1000 + CAST(min(interval_s, ?) * 100 * ? AS INTEGER) WHERE id = ?`,
				msg, now, now, int64(retryAfter/time.Second), int64(retryAfter/time.Second), d.jitter(), s.SubscriptionID); err != nil {
				return err
			}
		}
		return tx.Commit()
	}()
	if err != nil {
		d.log.Error("finishing subscription check", "search", s.ID, "err", err)
	}
	if state == StateFailed {
		d.log.Warn("subscription check failed", "site", s.Site, "query", s.Query, "err", msg)
	}
	if err != nil {
		return
	}
	switch {
	case state == StateDone && checks == 0: // the first check: its initial download finished
		d.emit(s, Event{Kind: EventDone, New: downloaded, First: true, Pages: pages})
	case state == StateDone && (downloaded > 0 || pages > 0):
		d.emit(s, Event{Kind: EventNewFiles, New: downloaded, Pages: pages})
	case state == StateFailed && !failedBefore: // not again every hour while it keeps failing
		d.emit(s, Event{Kind: EventFailed, Error: msg})
	}
}

// StartDue starts the checks that are due: a new check for a subscription
// without one, and a retry for a failed one. Paused subscriptions and checks
// someone paused are left alone.
func (d *Downloader) StartDue(ctx context.Context) error {
	rows, err := d.db.R.QueryContext(ctx, "SELECT "+subColumns+
		" FROM subscriptions sub JOIN sites si ON si.id = sub.site_id WHERE NOT sub.paused AND sub.next_check_at <= ?",
		d.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	var due []Subscription
	for rows.Next() {
		s, err := scanSub(rows)
		if err != nil {
			rows.Close()
			return err
		}
		due = append(due, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, sub := range due {
		if _, ok := d.all()[sub.Site]; !ok || d.IsOff(sub.Site) {
			continue // its plugin isn't installed (any more), or the site is switched off
		}
		c, ok, err := d.check(ctx, sub.ID)
		switch {
		case err != nil:
			return err
		case !ok:
			err = d.startCheck(ctx, sub, false)
		case c.State == StateFailed:
			err = d.Resume(ctx, c.ID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// kick makes the scheduler look for due checks now.
func (d *Downloader) kick() {
	select {
	case d.due <- struct{}{}:
	default:
	}
}

// schedule starts due checks every half minute (and when kicked) until ctx
// ends.
func (d *Downloader) schedule(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		if err := d.retryFailed(ctx); err != nil && ctx.Err() == nil {
			d.log.Error("retrying failed searches", "err", err)
		}
		if err := d.StartDue(ctx); err != nil && ctx.Err() == nil {
			d.log.Error("subscriptions", "err", err)
		}
		select {
		case <-t.C:
		case <-d.due:
		case <-ctx.Done():
			return
		}
	}
}

// lastFound is when each subscription last brought in a new file (Unix ms).
func (d *Downloader) lastFound(ctx context.Context) (map[int64]int64, error) {
	rows, err := d.db.R.QueryContext(ctx, `SELECT subscription_id, max(first_at) FROM file_sources
		WHERE subscription_id IS NOT NULL AND downloaded = 1 GROUP BY subscription_id`)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("download: %w", err)
		}
		out[id] = at
	}
	return out, rows.Err()
}

// SubLook is what the Downloads page shows of a subscription besides its numbers: the newest files it downloaded
// (still in the library), or for a gallery the library has, its comic and cover; and how many files it downloaded
// since a time (the page's "new since you last looked").
type SubLook struct {
	Recent []uint32 // its newest posts in the library (downloaded by it or not), newest first, at most 4
	Comic  int64    // a gallery subscription's comic (0: none)
	Cover  uint32   // ... its cover file
	Pages  int      // ... its pages
	Fresh  int
}

// SubscriptionLooks gathers every subscription's SubLook; since: files after it count as fresh (0: none do).
func (d *Downloader) SubscriptionLooks(ctx context.Context, since int64) (map[int64]*SubLook, error) {
	rows, err := d.db.R.QueryContext(ctx, `SELECT sub.id,
		coalesce((SELECT c.id FROM comics c WHERE rtrim(c.url, '/') = rtrim(sub.query, '/') AND c.deleted_at IS NULL LIMIT 1), 0),
		coalesce((SELECT c.cover FROM comics c WHERE rtrim(c.url, '/') = rtrim(sub.query, '/') AND c.deleted_at IS NULL LIMIT 1), 0),
		coalesce((SELECT c.pages FROM comics c WHERE rtrim(c.url, '/') = rtrim(sub.query, '/') AND c.deleted_at IS NULL LIMIT 1), 0)
		FROM subscriptions sub`)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	out := map[int64]*SubLook{}
	for rows.Next() {
		var id int64
		l := &SubLook{}
		if err := rows.Scan(&id, &l.Comic, &l.Cover, &l.Pages); err != nil {
			rows.Close()
			return nil, fmt.Errorf("download: %w", err)
		}
		out[id] = l
	}
	rows.Close()
	for id, l := range out {
		if l.Comic != 0 {
			continue // (a gallery: its cover - its pages aren't library files)
		}
		// Its newest posts in the library - the ones it met that were there already too (2026-10-23: a subscription
		// whose posts another download had got first showed none).
		r, err := d.db.R.QueryContext(ctx, `SELECT fs.file_id FROM file_sources fs JOIN files f ON f.id = fs.file_id AND f.deleted_at IS NULL
			WHERE fs.subscription_id = ? ORDER BY coalesce(f.posted_at, fs.first_at) DESC, fs.file_id DESC LIMIT 4`, id)
		if err != nil {
			return nil, fmt.Errorf("download: %w", err)
		}
		for r.Next() {
			var f uint32
			if err := r.Scan(&f); err == nil {
				l.Recent = append(l.Recent, f)
			}
		}
		r.Close()
	}
	if since > 0 {
		r, err := d.db.R.QueryContext(ctx, `SELECT fs.subscription_id, count(*) FROM file_sources fs JOIN files f ON f.id = fs.file_id
			AND f.deleted_at IS NULL WHERE fs.subscription_id IS NOT NULL AND fs.downloaded = 1 AND fs.first_at > ? GROUP BY fs.subscription_id`, since)
		if err != nil {
			return nil, fmt.Errorf("download: %w", err)
		}
		for r.Next() {
			var id int64
			var n int
			if err := r.Scan(&id, &n); err == nil && out[id] != nil {
				out[id].Fresh = n
			}
		}
		r.Close()
	}
	return out, nil
}
