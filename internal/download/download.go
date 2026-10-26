// Package download runs searches on sites and brings their posts straight
// into the library, file by file.
//
// Each site has one worker that works through that site's queue in order;
// sites run in parallel. Posts with a blacklisted tag are skipped (see
// library.Blacklist). A search resumes exactly where it stopped (see the
// searches table), every post is recorded as soon as it's handled, and files
// the library has or deleted are skipped by md5 before downloading.
package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/store"
)

// Post outcomes (seen_posts.outcome).
const (
	Downloaded  = 1
	Already     = 2
	Deleted     = 3
	Failed      = 4
	Skipped     = 5
	Blacklisted = 6 // has a blacklisted tag: not downloaded
	TooBig      = 7 // over a download rule's size limit: not downloaded (counted as skipped)
)

// Search states.
const (
	StateQueued  = "queued"
	StateRunning = "running"
	StatePaused  = "paused"
	StateDone    = "done"
	StateFailed  = "failed"
)

// Site is a plugin ready to use.
type Site struct {
	ID     uint32 // sites.id in meta.db
	Client *plugin.Client
	Engine plugin.Engine
	// backgroundAfter: no background work before this (a back-off after an
	// error, e.g. "too many requests"). Used by the site's worker only.
	backgroundAfter time.Time
	postedFixed     int // upload times given their moment by FixPosted (for its log line)
}

// Downloader runs the queues.
type Downloader struct {
	lib   *library.Library
	db    *db.DB
	sites atomic.Pointer[map[string]*Site] // by plugin ID; replaced whole when a plugin is installed (see all)
	log   *slog.Logger
	now   func() time.Time

	// Solver passes Cloudflare challenges with FlareSolverr (off until its address is set).
	Solver *plugin.Solver

	// SecretsPath is the credentials file (secrets.toml) that SetCredentials writes.
	SecretsPath string
	credMu      sync.Mutex

	// OnPost, if set, is called after each post (for progress output).
	OnPost func(s Search, p plugin.Post, outcome int, err error)
	// OnEvent, if set, hears about things worth telling the user (notifications).
	OnEvent func(Event)

	live liveState // watchers and files in progress (live.go)

	// jitter returns 0..1: a check is scheduled up to a tenth of its interval
	// (at most 30 min) late, so subscriptions added together drift apart
	// instead of all hitting the site at once.
	jitter func() float64
	// pageRetryWait: a search page that got no answer is asked again after this, twice this, ... (tests: shorter).
	pageRetryWait time.Duration
	// pauseAfter: connection failures in a row that pause a site (Settings > Sites; 0 = never; -1: not set - the default).
	pauseAfter int
	// checksFirst: subscription checks go ahead of downloads in a site's queue, a running download stepping aside
	// for them (Settings > Downloads; on unless turned off). Read under mu.
	checksOff  bool
	asideEvery time.Duration // how often a running download looks for a check waiting (tests: 0)

	runCtx  context.Context               // Run's context (sites installed later start their worker in it)
	cancels map[string]context.CancelFunc // stops a site's worker
	workers sync.WaitGroup

	// PluginsDir is where installed plugins are saved (InstallPlugin);
	// PluginIndex the list Settings offers (a URL or a local path).
	PluginsDir  string
	PluginIndex string
	installMu   sync.Mutex
	index       pluginIndex

	mu        sync.Mutex
	retrying  map[string]*retryCall // failed posts being tried again (one at a time each: see RetryPost)
	retryJobs map[string]*RetryJob  // ... in the background, and their results for a while (see StartRetry)
	off       map[string]bool       // switched-off sites (see siteoff.go)
	wakes     map[string]chan struct{}
	stops     map[int64]bool // searches asked to stop (paused or removed) before their next post
	runs      map[int64]bool // searches being worked now (run)
	// A search's page request in flight: canceled when it's asked to stop (a
	// site that hangs would otherwise hold its queue until the request times out).
	pageCancel map[int64]context.CancelFunc
	// retryWaits: how long a failed search waits before its next automatic try
	// (one per attempt; after the last it waits for the user).
	retryWaits []time.Duration
	// fileWaits: pauses before trying a failed file download again.
	fileWaits []time.Duration
	due       chan struct{} // wakes the subscription scheduler
}

// New prepares a downloader for the given plugins and credentials.
func New(ctx context.Context, lib *library.Library, d *db.DB, plugins map[string]plugin.Manifest,
	secrets plugin.Secrets, log *slog.Logger) (*Downloader, error) {
	sites := map[string]*Site{}
	dl := &Downloader{lib: lib, db: d, log: log, jitter: rand.Float64, pageRetryWait: 3 * time.Second, pauseAfter: -1, asideEvery: 3 * time.Second, now: time.Now, wakes: map[string]chan struct{}{}, stops: map[int64]bool{}, runs: map[int64]bool{}, pageCancel: map[int64]context.CancelFunc{}, due: make(chan struct{}, 1), off: map[string]bool{},
		retryWaits: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour},
		fileWaits:  []time.Duration{2 * time.Second, 10 * time.Second}, Solver: plugin.NewSolver()}
	removed := dl.idList(ctx, "removedSites") // built-in sites the user removed
	for _, id := range dl.idList(ctx, "sitesOff") {
		dl.off[id] = true
	}
	for id, m := range plugins {
		if m.BuiltIn && slices.Contains(removed, id) {
			continue
		}
		st, err := dl.prepareSite(ctx, m, secrets[id])
		if err != nil {
			return nil, err
		}
		sites[id] = st
		dl.wakes[id] = make(chan struct{}, 1)
	}
	dl.sites.Store(&sites)
	return dl, nil
}

// all returns the sites by plugin ID. Don't change the map: installing a
// plugin replaces it whole (putSite), so readers never see it half-changed.
func (d *Downloader) all() map[string]*Site { return *d.sites.Load() }

// putSite adds or replaces a site (a copy of the map with it in).
func (d *Downloader) putSite(id string, st *Site) {
	d.mu.Lock()
	defer d.mu.Unlock()
	next := maps.Clone(*d.sites.Load())
	next[id] = st
	d.sites.Store(&next)
	if d.wakes[id] == nil {
		d.wakes[id] = make(chan struct{}, 1)
	}
}

// wakeChan is the channel that wakes a site's worker.
func (d *Downloader) wakeChan(site string) chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.wakes[site]
}

// Sites lists the plugin IDs, sorted.
func (d *Downloader) Sites() []string {
	out := make([]string, 0, len(d.all()))
	for id := range d.all() {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Search is a queued or finished search.
type Search struct {
	ID    int64  `json:"id"`
	Site  string `json:"site"`
	Query string `json:"query"`
	// Label: its name where the query says little (a gallery's link: its title - given, its subscription's, or the
	// downloaded comic's); "" = the query.
	Label string `json:"label,omitempty"`
	// Pages: pages it brought in for galleries (a gallery site: one gallery's counts are by page).
	Pages       int    `json:"pages,omitempty"`
	State       string `json:"state"`
	MaxPosts    int    `json:"max_posts"`
	LowID       int64  `json:"-"`
	HighID      int64  `json:"-"`
	StopID      int64  `json:"-"` // done on reaching a post at or below this ID (subscription checks)
	StopDepth   int    `json:"-"` // ... and this many posts past it (the check depth)
	pastStop    int    // posts at or below StopID gone through so far (this run)
	runFrom     int64  // when this run of it began (Unix ms; a resumed search's earlier posts are from before)
	SiteTotal   int    `json:"site_total"`
	Handled     int    `json:"handled"`
	Downloaded  int    `json:"downloaded"`
	Already     int    `json:"already"`
	Deleted     int    `json:"deleted"`
	Skipped     int    `json:"skipped"`
	Blacklisted int    `json:"blacklisted"`
	Failed      int    `json:"failed"`
	Error       string `json:"error,omitempty"`
	Position    int    `json:"position"`           // order in its site's queue
	Attempts    int    `json:"attempts,omitempty"` // failures in a row
	RetryAt     int64  `json:"retry_at,omitempty"` // when it is tried again automatically (0 = not)
	CreatedAt   int64  `json:"created_at"`
	FinishedAt  int64  `json:"finished_at,omitempty"`
	// SubscriptionID is set on a subscription's check.
	SubscriptionID int64 `json:"subscription_id,omitempty"`
	FirstCheck     bool  `json:"first_check,omitempty"` // a subscription's first check: a bulk download, not one of its checks
	siteID         uint32
}

// ErrUnknownSite means no plugin has that ID.
var ErrUnknownSite = plugin.ErrUnknownSite

// Add queues a search at the end of its site's queue. maxPosts > 0 stops it
// after that many posts.
func (d *Downloader) Add(ctx context.Context, site, query string, maxPosts int) (int64, error) {
	return d.AddTo(ctx, site, query, maxPosts, false)
}

// AddTo is Add; favorite: the files it brings in become favorites too (e.g.
// your own favorites on the site).
func (d *Downloader) AddTo(ctx context.Context, site, query string, maxPosts int, favorite bool) (int64, error) {
	s, ok := d.all()[site]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	if d.IsOff(site) {
		return 0, fmt.Errorf("%w: %s", ErrSiteOff, site)
	}
	var id int64
	err := d.db.W.QueryRowContext(ctx, `
		INSERT INTO searches (site_id, query, max_posts, position, created_at, favorite)
		VALUES (?, ?, ?, (SELECT coalesce(max(position), 0) + 1 FROM searches WHERE site_id = ?), ?, ?)
		RETURNING id`, s.ID, query, max(0, maxPosts), s.ID, d.now().UnixMilli(), favorite).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	d.wake(site)
	return id, nil
}

// LabelSearch names a search (a gallery's title for its link).
func (d *Downloader) LabelSearch(ctx context.Context, id int64, label string) error {
	if _, err := d.db.W.ExecContext(ctx, "UPDATE searches SET label = nullif(?, '') WHERE id = ?", strings.TrimSpace(label), id); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	d.Changed()
	return nil
}

func (d *Downloader) wake(site string) {
	d.Changed() // something was queued
	select {
	case d.wakeChan(site) <- struct{}{}:
	default:
	}
}

const searchColumns = `s.id, si.name, s.site_id, s.query, s.state, s.max_posts, s.low_id, s.site_total, s.handled,
	s.downloaded, s.already, s.deleted, s.skipped, s.blacklisted, s.failed, coalesce(s.error, ''), s.position, s.created_at,
	coalesce(s.finished_at, 0), s.high_id, s.stop_id, coalesce(s.subscription_id, 0), s.attempts, coalesce(s.retry_at, 0),
	s.stop_depth, s.pages, coalesce(s.label, (SELECT label FROM subscriptions WHERE id = s.subscription_id), (SELECT title FROM comics c WHERE rtrim(c.url, '/') = rtrim(s.query, '/') AND c.deleted_at IS NULL LIMIT 1), '')`

func scanSearch(row interface{ Scan(...any) error }) (Search, error) {
	var s Search
	err := row.Scan(&s.ID, &s.Site, &s.siteID, &s.Query, &s.State, &s.MaxPosts, &s.LowID, &s.SiteTotal, &s.Handled,
		&s.Downloaded, &s.Already, &s.Deleted, &s.Skipped, &s.Blacklisted, &s.Failed, &s.Error, &s.Position, &s.CreatedAt, &s.FinishedAt,
		&s.HighID, &s.StopID, &s.SubscriptionID, &s.Attempts, &s.RetryAt, &s.StopDepth, &s.Pages, &s.Label)
	s.FirstCheck = s.SubscriptionID != 0 && s.StopID == 0
	return s, err
}

// List returns all searches, each site's queue in order.
func (d *Downloader) List(ctx context.Context) ([]Search, error) {
	rows, err := d.db.R.QueryContext(ctx, "SELECT "+searchColumns+
		" FROM searches s JOIN sites si ON si.id = s.site_id ORDER BY si.name, s.position")
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer rows.Close()
	var out []Search
	for rows.Next() {
		s, err := scanSearch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Get returns one search.
func (d *Downloader) Get(ctx context.Context, id int64) (Search, error) {
	return scanSearch(d.db.R.QueryRowContext(ctx, "SELECT "+searchColumns+
		" FROM searches s JOIN sites si ON si.id = s.site_id WHERE s.id = ?", id))
}

// checkSQL: a subscription's check for what's new (it stops at the newest post the last one saw) - not its first
// check, which takes everything at once like a download (2026-10-23, the user: "initial downloads that i put in a
// sub group" are bulk downloads: they don't go first, and they step aside for checks).
const checkSQL = "(s.subscription_id IS NOT NULL AND s.stop_id > 0)"

// isCheck: a subscription's check for what's new (see checkSQL).
func (s Search) isCheck() bool { return s.SubscriptionID != 0 && s.StopID > 0 }

// next returns the site's first unfinished search: subscription checks first (unless turned off), then an
// interrupted running one, then queued ones in order.
func (d *Downloader) next(ctx context.Context, siteID uint32) (Search, bool, error) {
	s, err := scanSearch(d.db.R.QueryRowContext(ctx, "SELECT "+searchColumns+`
		FROM searches s JOIN sites si ON si.id = s.site_id
		WHERE s.site_id = ?1 AND s.state IN ('running', 'queued')
		ORDER BY (?2 AND `+checkSQL+`) DESC, s.state = 'running' DESC, s.position LIMIT 1`, siteID, d.checksFirst()))
	if errors.Is(err, sql.ErrNoRows) {
		return s, false, nil
	}
	return s, err == nil, err
}

// Run works every site's queue until ctx ends, waiting for new searches when
// a queue is empty, and starts subscription checks when they are due.
func (d *Downloader) Run(ctx context.Context) {
	d.mu.Lock()
	d.runCtx = ctx
	d.mu.Unlock()
	if n, err := d.scheduleUndated(ctx); err != nil {
		d.log.Warn("files without an upload time", "err", err)
	} else if n > 0 {
		d.log.Info("files without an upload time: first in the tag refresh", "posts", n)
	}
	d.workers.Add(1)
	go func() {
		defer d.workers.Done()
		d.schedule(ctx)
	}()
	for id := range d.all() {
		d.startWorker(ctx, id)
	}
	<-ctx.Done()
	d.workers.Wait()
}

// startWorker runs a site's queue and background work until ctx ends.
func (d *Downloader) startWorker(parent context.Context, id string) {
	ctx, cancel := context.WithCancel(parent)
	d.mu.Lock()
	if d.cancels == nil {
		d.cancels = map[string]context.CancelFunc{}
	}
	d.cancels[id] = cancel // removing the site stops its worker (a running search resumes when it's back)
	d.mu.Unlock()
	d.workers.Add(1)
	go func() {
		defer d.workers.Done()
		defer cancel()
		for ctx.Err() == nil {
			if _, ok := d.all()[id]; !ok || d.IsOff(id) {
				return // the site was removed or switched off
			}
			if err := d.RunQueue(ctx, id); err != nil && ctx.Err() == nil {
				d.log.Error("download queue", "site", id, "err", err)
			}
			d.background(ctx, id) // tag types of older files, tag refresh: while nothing else runs
			wait := time.Minute
			if st, ok := d.all()[id]; ok {
				if p := st.Client.Paused(); !p.Until.IsZero() {
					wait = min(wait, max(time.Second, time.Until(p.Until))) // (paused: wakes for its next try)
				}
			}
			select {
			case <-d.wakeChan(id):
			case <-time.After(wait):
			case <-ctx.Done():
			}
		}
	}()
}

// RunQueue works through one site's queue until it is empty or ctx ends.
func (d *Downloader) RunQueue(ctx context.Context, site string) error {
	for ctx.Err() == nil {
		st, ok := d.all()[site] // read for each search: an updated plugin applies from the next one
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownSite, site)
		}
		// Paused (it kept failing to answer): the queue waits until its next try (the worker wakes for it).
		if st.Client.Waiting() {
			return nil
		}
		s, ok, err := d.next(ctx, st.ID)
		if err != nil || !ok {
			return err
		}
		d.run(ctx, st, s)
		if !st.Client.Paused().Until.IsZero() {
			return nil // (it paused on the way: the search waits where it was)
		}
	}
	return nil
}

// run works one search until it is done, fails, or ctx ends (then it stays
// "running" and resumes on the next start).
func (d *Downloader) run(ctx context.Context, st *Site, s Search) {
	d.mu.Lock()
	d.runs[s.ID] = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.runs, s.ID)
		delete(d.stops, s.ID)
		d.mu.Unlock()
	}()
	// Removed since the queue picked it (Remove stops only a search it sees running): nothing to do.
	if err := d.db.R.QueryRowContext(ctx, "SELECT 1 FROM searches WHERE id = ?", s.ID).Scan(new(int)); errors.Is(err, sql.ErrNoRows) {
		return
	}
	if s.State == StateQueued {
		s.State = StateRunning
		d.db.W.ExecContext(ctx, "UPDATE searches SET state = 'running', started_at = ? WHERE id = ? AND state = 'queued'",
			d.now().UnixMilli(), s.ID)
		d.Changed()
	}
	// Read once per run: changes apply from the next search (or resume).
	bl, err := d.lib.Blacklist(ctx)
	if err != nil {
		d.finish(s, StateFailed, err.Error())
		return
	}
	rules, err := d.lib.DownloadRules(ctx)
	if err != nil {
		d.finish(s, StateFailed, err.Error())
		return
	}
	var favorite bool // its files become favorites
	d.db.R.QueryRowContext(ctx, "SELECT favorite FROM searches WHERE id = ?", s.ID).Scan(&favorite)
	// Paused or removed: stop before the next post (the state is already set).
	defer d.stopped(s.ID, true)
	// A download (or a subscription's first check: not a check for what's new) steps aside for a subscription check that's waiting (Settings > Downloads): it goes
	// back in the queue, where it was, and carries on from its last post once the checks are done. (Looked at every
	// few seconds, not for every post.)
	yielded := false
	var lookedAt time.Time
	stepAside := func() bool {
		if yielded {
			return true
		}
		if s.isCheck() || !d.checksFirst() || time.Since(lookedAt) < d.asideEvery {
			return false
		}
		lookedAt = time.Now()
		yielded = d.checkWaiting(ctx, st.ID)
		return yielded
	}
	defer func() {
		if yielded {
			d.db.W.ExecContext(context.WithoutCancel(ctx), "UPDATE searches SET state = 'queued' WHERE id = ? AND state = 'running'", s.ID)
			d.log.Info("download steps aside for subscription checks", "site", s.Site, "search", s.ID)
			d.Changed()
		}
	}()
	stop := func() bool { return d.stopped(s.ID, false) || stepAside() }
	first := s.LowID == 0
	// A favorites subscription's complete check (from the newest post, not resumed):
	// the posts it saw, to find favorites removed on the site (favgone.go).
	var favSeen map[int64]bool
	if s.SubscriptionID != 0 && s.LowID == 0 && plugin.IsFavorites(s.Query) {
		favSeen = map[int64]bool{}
	}
	began := d.now()
	s.runFrom = began.UnixMilli()
	d.log.Debug("search started", "site", s.Site, "search", s.ID, "query", s.Query, "resumed", s.LowID != 0, "below", s.LowID, "subscription", s.SubscriptionID)
	defer func() {
		d.log.Debug("search ended", "site", s.Site, "search", s.ID, "handled", s.Handled, "downloaded", s.Downloaded, "already", s.Already,
			"took", d.now().Sub(began).Round(time.Second), "interrupted", ctx.Err() != nil)
	}()
	fetch := func(below int64) (plugin.Page, error) {
		pctx, cancel := context.WithCancel(ctx)
		d.trackPage(s.ID, cancel)
		pageStart := d.now()
		page, err := st.Engine.Search(pctx, s.Query, below)
		d.trackPage(s.ID, nil)
		cancel()
		d.log.Debug("page", "site", s.Site, "search", s.ID, "below", below, "posts", len(page.Posts), "total", page.Total,
			"took", d.now().Sub(pageStart).Round(time.Millisecond), "err", err)
		return page, err
	}
	seen := func(posts []plugin.Post) {
		for _, p := range posts {
			if favSeen != nil {
				favSeen[p.ID] = true
			}
		}
		if err := d.recordTypes(ctx, st.ID, posts[0].TagTypes); err != nil {
			d.log.Warn("saving tag types", "site", s.Site, "err", err)
		}
	}
	// The next page while a page's last files are still coming (below its oldest post): one slow file at the
	// end of a page held every connection up until it came or was given up. Nothing (an error, the end):
	// the next round of the loop below asks again from the resume point, as always.
	more := func(below int64) []plugin.Post {
		if stop() {
			return nil
		}
		page, err := fetch(below)
		if err != nil || len(page.Posts) == 0 {
			return nil
		}
		seen(page.Posts)
		return page.Posts
	}
	for {
		if stop() {
			return
		}
		page, err := fetch(s.LowID)
		// No answer (or a 5xx, a Cloudflare block): asked again a few times, a little later each time - a site
		// that's only briefly unwell carries on; one that's down gets paused on the way (and the search waits).
		for try := 1; err != nil && try < st.Client.PauseAfter() && ctx.Err() == nil && !stop() && !pausedBy(st, err) && st.Client.Failing() > 0; try++ {
			select {
			case <-time.After(time.Duration(try) * d.pageRetryWait):
			case <-ctx.Done():
			}
			if ctx.Err() == nil {
				page, err = fetch(s.LowID)
			}
		}
		if ctx.Err() != nil {
			return
		}
		if stop() { // (a canceled request lands here: stopped, not failed)
			return
		}
		if err != nil {
			if pausedBy(st, err) {
				return // (it stays running: it carries on from here once the site answers again)
			}
			d.finish(s, StateFailed, err.Error())
			return
		}
		if first {
			first = false
			d.db.W.ExecContext(ctx, "UPDATE searches SET site_total = ? WHERE id = ?", page.Total, s.ID)
		}
		if len(page.Posts) == 0 {
			if favSeen != nil {
				if n, err := d.markFavGone(ctx, s.SubscriptionID, favSeen); err != nil {
					d.log.Warn("favorites removed on the site", "subscription", s.SubscriptionID, "err", err)
				} else if n > 0 {
					d.log.Info("favorites removed on the site", "site", s.Site, "subscription", s.SubscriptionID, "files", n)
				}
			}
			d.finish(s, StateDone, "")
			return
		}
		seen(page.Posts)
		if !d.runPage(ctx, st, &s, page.Posts, bl, rules, favorite, stop, more) {
			return
		}
	}
}

// runPage works through one page's posts, up to the plugin's connections at
// once (a site's files download in parallel, in list order) - and the pages
// after it that more brings while its last posts are in flight (so the
// connections stay busy across pages). Each post is
// recorded when it's done; the resume point (low_id) only moves past a post
// once every newer post on the page is done - a crash may fetch a file
// again, never skip one. Pausing stops new posts and lets the ones in flight
// finish. It reports whether the search goes on to the next page.
func (d *Downloader) runPage(ctx context.Context, st *Site, s *Search, posts []plugin.Post, bl *library.Blacklist,
	rules library.DownloadRules, favorite bool, stop func() bool, more func(below int64) []plugin.Post) bool {
	// Files from a subscription's regular check (it stops at the newest post the last
	// check saw) are "new" in the strict sense; anything else came in bulk.
	bulk := !s.isCheck()
	conns := max(1, st.Client.Manifest().Limits.Connections)
	if _, ok := st.Engine.(plugin.GalleryEngine); ok {
		conns = 1 // one gallery at a time: its pages share the connections (see handleGallery)
	}
	type result struct {
		i       int
		outcome int
		fileID  uint32
		err     error
	}
	results := make(chan result)
	done := make([]bool, len(posts))
	next, inflight := 0, 0 // next: the first post not yet behind the resume point
	ending := ""           // why no more posts start: "done", "stop" or "failed: ..."
	// max_posts counts the posts gone through - new, had, blacklisted, skipped, deleted before, failed: what the
	// Downloads page counts against it (the user, 2026-10-24; before, only the files that ended up in the library).
	// While the posts in flight would reach it, no more start.
	reached := func() bool { return s.MaxPosts > 0 && s.Handled >= s.MaxPosts }
	// A user's favorites: a check goes through all of them (a post favorited
	// since can be older than the newest one the last check saw).
	favorites := plugin.IsFavorites(s.Query)
	exhausted := more == nil // (no more pages to bring meanwhile)
	pf := newPrefetcher(ctx, st)
	for i := 0; i < len(posts) || inflight > 0; {
		if ending == "" && reached() {
			ending = "done"
		}
		enoughInFlight := s.MaxPosts > 0 && s.Handled+inflight >= s.MaxPosts
		if i == len(posts) && inflight > 0 && inflight < conns && ending == "" && !enoughInFlight && !exhausted {
			// A connection is free and the page is used up: the next page now, not once its last files are in.
			if np := more(posts[len(posts)-1].ID); len(np) > 0 {
				posts = append(posts, np...)
				done = append(done, make([]bool, len(np))...)
			} else {
				exhausted = true
			}
			continue
		}
		if i < len(posts) && inflight < conns && ending == "" && !enoughInFlight {
			p := posts[i]
			switch {
			case ctx.Err() != nil || stop():
				ending = "stop"
				continue
			case s.StopID > 0 && p.ID <= s.StopID && !favorites && s.pastStop >= s.StopDepth:
				ending = "done" // reached the posts the last check had (and went as deep past them as asked)
				continue
			case p.ID <= 0 || (s.LowID > 0 && p.ID >= s.LowID):
				done[i] = true // the site ignored id:<N; don't loop
				i++
				continue
			}
			if s.StopID > 0 && p.ID <= s.StopID {
				s.pastStop++ // (a resumed check starts its depth again: a few posts more, never fewer)
			}
			inflight++
			next := slices.Clone(posts[i+1 : min(i+4, len(posts))]) // (the posts after it: one may have its page opened ahead)
			go func(i int, p plugin.Post) {
				pctx, untrack := d.trackPost(ctx, s.ID, p.ID)
				pctx = withAhead(pctx, pf, next)
				postStart := d.now()
				outcome, fileID, err := d.handle(pctx, st, p, bl, rules, bulk)
				untrack()
				d.log.Debug("post", "site", s.Site, "search", s.ID, "post", p.ID, "outcome", outcomeNames[outcome], "file", fileID,
					"took", d.now().Sub(postStart).Round(time.Millisecond), "err", err)
				results <- result{i, outcome, fileID, err}
			}(i, p)
			i++
			continue
		}
		if inflight == 0 {
			break // nothing in flight and nothing more to start
		}
		r := <-results
		inflight--
		rctx := ctx // (recording it)
		if ctx.Err() != nil {
			if r.err != nil || (r.outcome != Downloaded && r.outcome != Already) {
				continue // interrupted mid-file: fetched again on resume
			}
			// In the library before the stop came: recorded as it is (left out, the resume found its file and counted
			// it "already there" instead of downloaded - CI caught it). The loop still stops: nothing new starts.
			rctx = context.WithoutCancel(ctx)
		}
		p := posts[r.i]
		var stop errStopSearch
		if errors.As(r.err, &stop) {
			// (not recorded: the search goes on from this post later - a gallery from the page it stopped at)
			d.log.Warn("search stopped", "site", s.Site, "post", p.ID, "err", r.err)
			if ending == "" || ending == "done" || ending == "stop" {
				ending = "failed: " + stop.Error()
			}
			continue
		}
		if r.err != nil && pausedBy(st, r.err) {
			// The site stopped answering: not recorded as failed - the search carries on from this post later.
			if ending == "" || ending == "done" || ending == "stop" {
				ending = "paused"
			}
			continue
		}
		if r.err != nil {
			d.log.Warn("post failed", "site", s.Site, "post", p.ID, "err", r.err)
		}
		if favorites && s.SubscriptionID != 0 && r.fileID != 0 && (r.outcome == Downloaded || r.outcome == Already) {
			d.noteFavMember(rctx, s.SubscriptionID, p.ID, r.fileID)
		}
		if favorite && r.fileID != 0 && (r.outcome == Downloaded || r.outcome == Already) {
			// (a favorite already keeps the time it became one)
			if _, err := d.lib.SetFavorite(rctx, []uint32{r.fileID}, true); err != nil {
				d.log.Warn("adding to favorites", "site", s.Site, "file", r.fileID, "err", err)
			}
		}
		done[r.i] = true
		low := s.LowID
		for next < len(posts) && done[next] {
			low = posts[next].ID
			next++
		}
		again, err := d.record(*s, st.ID, p.ID, r.fileID, r.outcome, low, r.err)
		if err != nil {
			if ending == "" || ending == "stop" || ending == "done" {
				ending = "failed: " + err.Error()
			}
			continue
		}
		s.LowID = low
		if !again { // (counted already, before the search was resumed)
			s.Handled++
			switch r.outcome {
			case Downloaded:
				s.Downloaded++
			case Already:
				s.Already++
			}
		}
		if d.OnPost != nil {
			d.OnPost(*s, p, r.outcome, r.err)
		}
	}
	// Every post on the page handled: the resume point is the page's last post.
	for next < len(posts) && done[next] {
		s.LowID = posts[next].ID
		next++
	}
	switch {
	case ctx.Err() != nil, ending == "stop", ending == "paused":
		return false
	case strings.HasPrefix(ending, "failed: "):
		d.finish(*s, StateFailed, strings.TrimPrefix(ending, "failed: "))
		return false
	case ending == "done" || reached():
		d.finish(*s, StateDone, "")
		return false
	}
	return true
}

// pausedBy says whether err came from the site being paused (or pausing with it): the work waits instead of failing.
func pausedBy(st *Site, err error) bool {
	return errors.Is(err, plugin.ErrPaused) || !st.Client.Paused().Until.IsZero()
}

// sitePaused hears a site pause (it kept failing to answer; again: a try failed) or answer again: the user hears
// of it once each way (not of every try), the Downloads page shows it, and the site's queue carries on when it's back.
func (d *Downloader) sitePaused(m plugin.Manifest, paused bool, p plugin.Pause) {
	if paused {
		d.log.Warn("site paused: it isn't answering", "site", m.ID, "until", p.Until.Format("15:04:05"), "tries", p.Tries, "err", p.Reason)
	} else {
		d.log.Info("site answers again", "site", m.ID, "paused_since", p.Since.Format("15:04:05"))
	}
	// Kept over restarts (a restart doesn't start the waits over).
	var keep *library.SitePause
	if paused {
		keep = &library.SitePause{Since: p.Since.UnixMilli(), Until: p.Until.UnixMilli(), Reason: p.Reason, Tries: p.Tries}
	}
	if err := d.lib.SetSitePause(context.Background(), m.ID, keep); err != nil {
		d.log.Warn("keeping a site's pause", "site", m.ID, "err", err)
	}
	if d.OnEvent != nil && (!paused || p.Tries == 0) {
		e := Event{Kind: EventBack, Site: m.Site}
		if paused {
			e = Event{Kind: EventPaused, Site: m.Site, Error: p.Reason}
		}
		d.OnEvent(e)
	}
	if !paused {
		d.wake(m.ID)
		d.kick()
	}
	d.Changed()
}

// SetChecksFirst sets whether subscription checks go ahead of downloads (Settings > Downloads).
func (d *Downloader) SetChecksFirst(on bool) {
	d.mu.Lock()
	d.checksOff = !on
	d.mu.Unlock()
	if on {
		for id := range d.all() {
			d.wake(id) // (a download running now steps aside at its next post)
		}
	}
}

func (d *Downloader) checksFirst() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.checksOff
}

// checkWaiting reports whether a subscription check waits in the site's queue (a download steps aside for it).
func (d *Downloader) checkWaiting(ctx context.Context, siteID uint32) bool {
	var n int
	d.db.R.QueryRowContext(ctx, `SELECT count(*) FROM searches s WHERE s.site_id = ? AND s.state = 'queued' AND `+checkSQL, siteID).Scan(&n)
	return n > 0
}

// SetPauseAfter sets how many connection failures in a row pause a site (0: never; below 0: the default).
func (d *Downloader) SetPauseAfter(n int) {
	if n < 0 {
		n = plugin.PauseAfter
	}
	d.mu.Lock()
	d.pauseAfter = n
	d.mu.Unlock()
	for _, st := range d.all() {
		st.Client.SetPauseAfter(n)
	}
}

// TryNow makes a paused site try again now.
func (d *Downloader) TryNow(site string) error {
	st, ok := d.all()[site]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	st.Client.TryNow()
	d.wake(site)
	return nil
}

// Event is something the user may want to hear about.
type Event struct {
	Kind         string // EventNewFiles, EventFailed, EventDone or EventBlacklisted
	Site         string // the site's address, e.g. "safebooru.org"
	Query        string
	Subscription bool
	New          int    // files downloaded
	Error        string // EventFailed: why
	Tag          string // EventBlacklisted: the blacklist entry the file now matches
	First        bool   // EventDone: a subscription's first check (its initial download)
	Pages        int    // a gallery site's: the pages brought in (New counts its galleries)
}

const (
	EventNewFiles = "new"    // a subscription's regular check found new files
	EventFailed   = "failed" // a subscription check failed (the first time in a row) or a search stopped retrying
	EventPaused   = "paused" // a site kept failing to answer: its downloads wait (Error: the last failure)
	EventBack     = "back"   // a paused site answers again: its downloads carry on
	EventDone     = "done"   // a search finished, or a subscription's first check (First)
	// A file in the library got a blacklisted tag (a tag refresh, or the same
	// file found on another site). New downloads never have one: they're skipped.
	EventBlacklisted = "blacklisted"
)

// setSitePost writes what a site says about a file the library has, and tells
// when that gives the file a blacklisted tag it didn't have.
func (d *Downloader) setSitePost(ctx context.Context, st *Site, file uint32, sp library.SitePost, bl *library.Blacklist) error {
	if bl == nil || len(bl.Patterns()) == 0 || bl.Match(sp.Tags) == "" {
		return d.lib.SetSitePost(ctx, file, st.ID, sp) // nothing blacklisted can come of it
	}
	before := d.blacklistedTag(ctx, file, bl)
	if err := d.lib.SetSitePost(ctx, file, st.ID, sp); err != nil {
		return err
	}
	if before != "" || d.OnEvent == nil {
		return nil
	}
	if now := d.blacklistedTag(ctx, file, bl); now != "" {
		d.OnEvent(Event{Kind: EventBlacklisted, Site: st.Client.Manifest().Site, Tag: now})
	}
	return nil
}

// blacklistedTag is the blacklist entry one of the file's tags matches ("" if none).
func (d *Downloader) blacklistedTag(ctx context.Context, file uint32, bl *library.Blacklist) string {
	tags, err := d.lib.FileTags(ctx, file)
	if err != nil {
		return ""
	}
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = t.Tag
		if _, name, ok := strings.Cut(t.Tag, ":"); ok {
			names = append(names, name) // "creator:x" also as "x" (the blacklist matches either)
		}
	}
	return bl.Match(names)
}

func (d *Downloader) emit(s Search, e Event) {
	if d.OnEvent == nil {
		return
	}
	e.Site = s.Site
	if st := d.all()[s.Site]; st != nil {
		e.Site = st.Client.Manifest().Site
	}
	e.Query = strings.TrimSpace(s.Query)
	e.Subscription = s.SubscriptionID != 0
	d.OnEvent(e)
}

// finish marks a search done or failed. It uses a fresh context: it must be
// written even if the run's context was just canceled.
func (d *Downloader) finish(s Search, state, msg string) {
	defer d.Changed()
	if s.SubscriptionID != 0 {
		d.finishCheck(s, state, msg)
		return
	}
	now := d.now().UnixMilli()
	// A failure is retried automatically a few times, waiting longer each time
	// (attempts counts failures in a row; any handled post resets it).
	_, err := d.db.W.Exec(`UPDATE searches SET state = ?1, error = nullif(?2, ''), finished_at = ?3,
		attempts = CASE WHEN ?1 = 'failed' THEN attempts + 1 ELSE 0 END,
		retry_at = CASE WHEN ?1 = 'failed' AND attempts < ?4
			THEN ?3 + (SELECT value FROM json_each(?5) WHERE key = attempts) END
		WHERE id = ?6`, state, msg, now, len(d.retryWaits), retryWaitsJSON(d.retryWaits), s.ID)
	if err != nil {
		d.log.Error("finishing search", "search", s.ID, "err", err)
	}
	if state == StateFailed {
		d.log.Warn("search failed", "site", s.Site, "query", s.Query, "err", msg)
	}
	if err == nil && d.OnEvent != nil {
		var downloaded, pages int
		var retrying bool
		d.db.R.QueryRow("SELECT downloaded, pages, retry_at IS NOT NULL FROM searches WHERE id = ?", s.ID).Scan(&downloaded, &pages, &retrying)
		switch {
		case state == StateDone:
			d.emit(s, Event{Kind: EventDone, New: downloaded, Pages: pages})
		case !retrying: // gave up trying by itself
			d.emit(s, Event{Kind: EventFailed, Error: msg})
		}
	}
}

var counterColumn = map[int]string{
	Downloaded: "downloaded", Already: "already", Deleted: "deleted", Failed: "failed", Skipped: "skipped",
	Blacklisted: "blacklisted", TooBig: "skipped",
}

// record writes the post's outcome and the search's resume point (low: the
// oldest post every newer one of which is done), in one transaction.
func (d *Downloader) record(s Search, siteID uint32, postID int64, fileID uint32, outcome int, low int64, postErr error) (again bool, err error) {
	tx, err := d.db.W.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var file any
	if fileID != 0 {
		file = fileID
	}
	// Recorded by this same search already (it was resumed: the posts finished past its resume point come again)?
	// Then it isn't counted twice - only moved to its new outcome's count.
	// (recorded since the search started, before this run of it began: by its earlier run)
	before := -1
	tx.QueryRow(`SELECT sp.outcome FROM seen_posts sp JOIN searches s ON s.id = ?
		WHERE sp.site_id = ? AND sp.post_id = ? AND sp.query = s.query AND s.started_at IS NOT NULL
		AND sp.seen_at > s.started_at AND sp.seen_at < ?`,
		s.ID, siteID, postID, s.runFrom).Scan(&before)
	if err := recordSeen(tx, siteID, postID, file, outcome, postErr, strings.TrimSpace(s.Query), d.now().UnixMilli()); err != nil {
		return false, err
	}
	if fileID != 0 && (outcome == Downloaded || outcome == Already) {
		// "Found by": the first time this search met the file.
		var sub any
		if s.SubscriptionID != 0 {
			sub = s.SubscriptionID
		}
		if _, err := tx.Exec(`INSERT INTO file_sources (file_id, site_id, query, subscription_id, downloaded, first_at)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			fileID, siteID, strings.TrimSpace(s.Query), sub, outcome == Downloaded, d.now().UnixMilli()); err != nil {
			return false, err
		}
	}
	col := counterColumn[outcome]
	counts := `handled = handled + 1, ` + col + ` = ` + col + ` + 1`
	if before >= 0 {
		// Counted already: unchanged - unless it had failed and now hasn't (then it moves).
		counts = `handled = handled`
		if before == Failed && outcome != Failed {
			counts = col + ` = ` + col + ` + 1, failed = max(0, failed - 1)`
		}
	}
	if _, err := tx.Exec(`UPDATE searches SET low_id = ?, high_id = max(high_id, ?), attempts = 0, `+counts+` WHERE id = ?`,
		low, postID, s.ID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if fileID != 0 && outcome == Downloaded && s.SubscriptionID != 0 {
		d.lib.Index().AddFoundBy(fileID, uint32(s.SubscriptionID)) // (system:found_by= finds it)
	}
	d.Changed()
	return before >= 0, nil
}

// handle brings one post into the library.
func (d *Downloader) handle(ctx context.Context, st *Site, p plugin.Post, bl *library.Blacklist, rules library.DownloadRules, bulk bool) (int, uint32, error) {
	// A partial post (an HTML list page: ID and md5 only) is opened only when
	// it's needed - not for posts seen before or files deleted before.
	var full *plugin.Post
	if !p.Partial {
		full = &p
	}
	details := func() (*plugin.Post, error) {
		if full != nil {
			return full, nil
		}
		var fp *plugin.Post
		var err error
		opened := false
		if a := aheadOf(ctx); a != nil {
			fp, err, opened = a.pf.take(ctx, p.ID) // (opened while the post before downloaded its file)
			if opened && err != nil && ctx.Err() == nil {
				opened = false // (that try failed: once more, now)
			}
		}
		if !opened {
			fp, err = st.Engine.Lookup(ctx, p.ID)
		}
		if err != nil {
			return nil, err
		}
		if fp != nil {
			if fp.MD5 == [16]byte{} {
				fp.MD5 = p.MD5
			}
			if err := d.recordTypes(ctx, st.ID, fp.TagTypes); err != nil {
				d.log.Warn("saving tag types", "site", st.Client.Manifest().ID, "err", err)
			}
		}
		full = fp
		return fp, nil
	}
	sitePost := func(fp *plugin.Post) library.SitePost {
		sp := library.SitePost{Tags: fp.Tags, PostURL: fp.PostURL, Sources: fp.Sources, FileURL: fp.FileURL, Rating: fp.Rating,
			Namespaces: namespaces(st.Client.Manifest(), *fp)}
		if !fp.Posted.IsZero() {
			sp.PostedAt = fp.Posted.UnixMilli()
		}
		return sp
	}

	// A whole gallery: a comic (its pages, never library files). Never skipped as
	// seen: it fetches only the pages its comic lacks (none: had).
	if p.Gallery != nil {
		return d.handleGallery(ctx, st, p, bl)
	}

	// Seen before (e.g. by an overlapping search)?
	var prev int
	var prevFile sql.NullInt64
	err := d.db.R.QueryRowContext(ctx, "SELECT outcome, file_id FROM seen_posts WHERE site_id = ? AND post_id = ?",
		st.ID, p.ID).Scan(&prev, &prevFile)
	if err == nil && prev != Failed && prev != Blacklisted && prev != TooBig {
		// Refresh what the site says now (tags, links, upload time): running a
		// search again updates posts the library already has. (A partial post
		// isn't opened for that: the tag refresh does it at its own pace.)
		if prevFile.Valid && prevFile.Int64 > 0 && !p.Partial {
			return Already, uint32(prevFile.Int64), d.setSitePost(ctx, st, uint32(prevFile.Int64), sitePost(&p), bl)
		}
		return Already, uint32(max(0, prevFile.Int64)), nil
	}

	// Ask the library first: the site gives the md5 up front.
	if p.MD5 != [16]byte{} {
		m, err := d.lib.FindMD5(ctx, p.MD5)
		if err != nil {
			return Failed, 0, err
		}
		if m.FileID != 0 {
			// Same file, maybe from another site: add this site's post to it.
			fp, err := details()
			if err != nil {
				return Failed, 0, err
			}
			if fp == nil {
				return Already, m.FileID, nil
			}
			return Already, m.FileID, d.setSitePost(ctx, st, m.FileID, sitePost(fp), bl)
		}
		if m.Deleted {
			return Deleted, 0, nil
		}
	}

	fp, err := details()
	if err != nil {
		return Failed, 0, err
	}
	if fp == nil {
		return Skipped, 0, nil // gone from the site since the list page
	}

	// Blacklisted: not downloaded, but recorded as seen so a rerun doesn't page
	// through it again. (A post skipped earlier is checked again: the
	// blacklist may have changed.)
	if bl.Match(fp.Tags) != "" {
		return Blacklisted, 0, nil
	}
	if fp.FileURL == "" {
		return Failed, 0, errors.New("the post has no file URL")
	}
	// Download rules: images by their size in pixels (when the site says).
	if rules.ImagePixels > 0 && !isVideoURL(fp.FileURL) && int64(fp.Width)*int64(fp.Height) > rules.ImagePixels {
		return TooBig, 0, nil
	}

	// The next post that will need its page has it opened while this file downloads (prefetch.go).
	d.openNext(ctx, aheadOf(ctx))

	// The file, or its alternatives in turn (a video's webm when its mp4 is
	// missing or empty).
	var f library.File
	var modified time.Time
	urls := append([]string{fp.FileURL}, fp.AltFileURLs...)
	for i, u := range urls {
		var limit int64
		if isVideoURL(u) {
			limit = rules.VideoBytes
		}
		var md5 [16]byte
		if i == 0 {
			md5 = fp.MD5 // (the site's md5 is the post's file's, not an alternative's)
		}
		f, modified, err = d.fetchFile(ctx, st, st.Client.Manifest().FileFetchURL(u, fp.ID), md5, limit, bulk) // (as the site links it: see FileQuery)
		if err != nil && ctx.Err() != nil {
			return Failed, 0, ctx.Err() // (stopped mid-file: fetched again on resume)
		}
		if err == nil || errors.Is(err, library.ErrDeleted) || i == len(urls)-1 {
			break
		}
		d.log.Info("trying the next file for the post", "site", st.Client.Manifest().ID, "post", p.ID, "err", err)
	}
	switch {
	case errors.Is(err, errTooBig):
		return TooBig, 0, nil
	case errors.Is(err, media.ErrUnsupported):
		return Skipped, 0, nil
	case errors.Is(err, library.ErrDeleted):
		return Deleted, 0, nil
	case err != nil:
		return Failed, 0, err
	}
	fp.Posted = refinePosted(fp.Posted, modified)
	// In the library now: finished whatever comes (a stop just after the import left it unrecorded - the resume found
	// its file and counted it "already there" instead of downloaded; CI, on its slower machines).
	if err := d.lib.SetSitePost(context.WithoutCancel(ctx), f.ID, st.ID, sitePost(fp)); err != nil {
		return Failed, f.ID, err
	}
	if f.New {
		// Its thumbnail is made by the media worker (library.RunMedia): the
		// download goes straight on to the next file.
		d.lib.WakeMedia()
		return Downloaded, f.ID, nil
	}
	return Already, f.ID, nil
}

// fetchFile downloads one file into the library, trying again after short
// waits when the failure may pass (network trouble, the server busy) - not for
// a missing file, a web page instead of the file, or a type Vitrine can't keep.
// It also returns the file's Last-Modified time (zero if the site sent none).
//
// With the md5 the site gave for it (zero: none), the file is kept only if it matches: one that doesn't came out
// wrong and is downloaded again. If the site then sends the very same file again, its md5 is just wrong for it (a
// site that lists another version's md5): it's kept - an error would fail the post for good.
func (d *Downloader) fetchFile(ctx context.Context, st *Site, u string, md5 [16]byte, maxBytes int64, bulk bool) (library.File, time.Time, error) {
	var want [][16]byte // the md5s the file may have
	if md5 != ([16]byte{}) {
		want = [][16]byte{md5}
	}
	for attempt := 0; ; attempt++ {
		var f library.File
		var modified time.Time
		fileStart := d.now()
		body, size, err := st.Client.File(ctx, u)
		if err == nil && maxBytes > 0 && size > maxBytes {
			body.Close()
			return f, modified, errTooBig // a download rule: known from the size before reading it
		}
		if err == nil {
			if m, ok := body.(interface{ Modified() time.Time }); ok {
				modified = m.Modified()
			}
			body = d.countBody(ctx, body, size)
			f, err = d.lib.ImportChecked(ctx, body, bulk, want...)
			body.Close()
		}
		var wrong *store.MD5MismatchError
		if errors.As(err, &wrong) {
			want = [][16]byte{md5, wrong.Got} // (the same again: the site's md5 is wrong, not the download)
		} else if err == nil && md5 != ([16]byte{}) && f.MD5 != md5 {
			d.log.Warn("the site's md5 doesn't match its file (the same file came twice): kept", "site", st.Client.Manifest().ID, "url", u)
		}
		d.log.Debug("file", "site", st.Client.Manifest().ID, "url", u, "attempt", attempt+1, "bytes", size, "new", f.New, "file_id", f.ID,
			"took", d.now().Sub(fileStart).Round(time.Millisecond), "err", err)
		if err == nil || ctx.Err() != nil || !mayPass(err) || attempt >= len(d.fileWaits) {
			return f, modified, err
		}
		d.log.Info("file download failed; trying again", "site", st.Client.Manifest().ID, "err", err)
		select {
		case <-time.After(d.fileWaits[attempt]):
		case <-ctx.Done():
			return f, modified, ctx.Err()
		}
	}
}

var errTooBig = errors.New("larger than the download rules allow")

// refinePosted gives an upload time that is only a day (a site's page says "Oct, 14 2026": midnight UTC) its moment
// from the file's Last-Modified - when that falls on the same day (realbooru keeps it as the upload time; checked
// on posts from 2019 to 2026, images and videos). Anything else stays as the page says.
func refinePosted(posted, modified time.Time) time.Time {
	if posted.IsZero() || modified.IsZero() {
		return posted
	}
	p := posted.UTC()
	if p.Hour() != 0 || p.Minute() != 0 || p.Second() != 0 || p.Nanosecond() != 0 {
		return posted // (a moment already)
	}
	m := modified.UTC()
	if m.Year() == p.Year() && m.YearDay() == p.YearDay() {
		return m
	}
	return posted
}

// isVideoURL guesses from a file URL's extension whether it's a video.
func isVideoURL(u string) bool {
	u = strings.ToLower(u)
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	for _, ext := range []string{".mp4", ".webm", ".mov", ".mkv", ".m4v"} {
		if strings.HasSuffix(u, ext) {
			return true
		}
	}
	return false
}

// mayPass reports whether a download error is worth another try.
func mayPass(err error) bool {
	var se *plugin.StatusError
	switch {
	case errors.As(err, &se):
		return se.Code == http.StatusTooManyRequests || se.Code >= 500
	case errors.Is(err, plugin.ErrNotAFile), errors.Is(err, media.ErrUnsupported), errors.Is(err, library.ErrDeleted),
		errors.Is(err, plugin.ErrCutShort): // (the same again from the start: no)
		return false
	}
	return true // the connection dropped, a timeout, ...
}

// OtherHosts lists the other addresses the plugins know for the site a host
// belongs to (its API, its post pages) - e.g. for a site icon when the main
// address is behind a bot check.
func (d *Downloader) OtherHosts(host string) []string {
	var out []string
	for _, s := range d.all() {
		m := s.Client.Manifest()
		if !slices.ContainsFunc(m.Domains, func(dom string) bool { return host == dom || strings.HasSuffix(host, "."+dom) }) {
			continue
		}
		for _, raw := range []string{m.Base, m.PostURL} {
			if u, err := url.Parse(strings.ReplaceAll(raw, "{id}", "1")); err == nil && u.Hostname() != "" && u.Hostname() != host && !slices.Contains(out, u.Hostname()) {
				out = append(out, u.Hostname())
			}
		}
	}
	return out
}

// Site returns a site by plugin ID (nil if unknown).
func (d *Downloader) Site(id string) *Site { return d.all()[id] }
