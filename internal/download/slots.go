package download

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// Several downloads of a site at once (2026-11-18; as Hydrus runs several queues of a domain side by side): a site's
// queue runs up to siteDownloads of its searches together (Settings > Downloads; the server's default
// DefaultSiteDownloads, 1 without a setting - the CLI, tests). They share the site, not multiply it: its API requests
// already share one rate (plugin.Client), and its files share its connections (fileGate) - a site that caps each
// address at a few connections gets no more than before. Comic sites (a gallery engine: one gallery already uses every
// connection) run one at a time.

// DefaultSiteDownloads is how many downloads of a site run at once unless the settings say otherwise.
const DefaultSiteDownloads = 3

// MaxSiteDownloads is the most the setting allows.
const MaxSiteDownloads = 10

// SetSiteDownloads sets how many downloads of a site run at once (1 to MaxSiteDownloads); the queues start more at once.
func (d *Downloader) SetSiteDownloads(n int) {
	n = min(max(n, 1), MaxSiteDownloads)
	d.mu.Lock()
	d.siteDownloads = n
	d.mu.Unlock()
	for id := range d.all() {
		d.wake(id)
	}
}

// slots is how many of the site's downloads run at once.
func (d *Downloader) slots(st *Site) int {
	if _, ok := st.Engine.(plugin.GalleryEngine); ok {
		return 1
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return max(1, d.siteDownloads)
}

// fileGate shares a site's file connections between its downloads running at once: at most the site's connections in
// flight in all, given in turn - a download waiting for one gets the next one free before one that asks after it (the
// download that just finished a file doesn't take it straight back, so none starves).
type fileGate struct {
	mu      sync.Mutex
	n       int
	waiting []int64       // searches waiting for a connection, in the order they asked
	free    chan struct{} // closed when a connection comes free or the line changes (then replaced)
}

// gate is the site's fileGate (kept by site, not on Site: an updated plugin's new Site shares it with the downloads
// still running on the old one).
func (d *Downloader) gate(site uint32) *fileGate {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gates == nil {
		d.gates = map[uint32]*fileGate{}
	}
	g := d.gates[site]
	if g == nil {
		g = &fileGate{free: make(chan struct{})}
		d.gates[site] = g
	}
	return g
}

// take gives the search a connection if one is free that no search ahead of it in line waits for; otherwise the search
// waits in line and the channel tells when to ask again.
func (g *fileGate) take(search int64, limit int) (bool, <-chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := slices.Index(g.waiting, search)
	ahead := i
	if i < 0 {
		ahead = len(g.waiting)
	}
	if g.n+ahead < limit {
		g.n++
		if i >= 0 {
			g.waiting = slices.Delete(g.waiting, i, i+1)
			g.signal() // (the ones behind it are nearer the front)
		}
		return true, nil
	}
	if i < 0 {
		g.waiting = append(g.waiting, search)
	}
	return false, g.free
}

// release gives a connection back.
func (g *fileGate) release() {
	g.mu.Lock()
	g.n--
	g.signal()
	g.mu.Unlock()
}

// leave takes the search out of the line (it starts no more files for now).
func (g *fileGate) leave(search int64) {
	g.mu.Lock()
	if i := slices.Index(g.waiting, search); i >= 0 {
		g.waiting = slices.Delete(g.waiting, i, i+1)
		g.signal()
	}
	g.mu.Unlock()
}

func (g *fileGate) signal() {
	close(g.free)
	g.free = make(chan struct{})
}

// nextFree returns the site's first unfinished search that isn't running already, in next's order.
func (d *Downloader) nextFree(ctx context.Context, siteID uint32) (Search, bool, error) {
	rows, err := d.db.R.QueryContext(ctx, "SELECT "+searchColumns+`
		FROM searches s JOIN sites si ON si.id = s.site_id
		WHERE s.site_id = ?1 AND s.state IN ('running', 'queued')
		ORDER BY (?2 AND `+checkSQL+`) DESC, s.state = 'running' DESC, `+d.queueOrder()+` LIMIT ?3`,
		siteID, d.checksFirst(), MaxSiteDownloads+1)
	if err != nil {
		return Search{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		s, err := scanSearch(rows)
		if err != nil {
			return s, false, err
		}
		d.mu.Lock()
		busy := d.runs[s.ID]
		d.mu.Unlock()
		if !busy {
			return s, true, nil
		}
	}
	return Search{}, false, rows.Err()
}

// RunQueue works through one site's queue until it is empty or ctx ends: up to slots of its searches at once, each
// started in the queue's order as one ends (or steps aside).
func (d *Downloader) RunQueue(ctx context.Context, site string) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	ended := make(chan struct{}, MaxSiteDownloads)
	running := 0
	stoppedPaused := false
	defer func() {
		for ; running > 0; running-- {
			<-ended
		}
	}()
	for ctx.Err() == nil {
		st, ok := d.all()[site] // read for each search: an updated plugin applies from the next one
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownSite, site)
		}
		// Paused (it kept failing to answer): nothing starts until its next try (the worker wakes for it); the try is one
		// search on its own, and a search that paused it on the way (or the try failing again) ends the queue's turn.
		paused := !st.Client.Paused().Until.IsZero()
		if paused && (stoppedPaused || st.Client.Waiting()) {
			if running == 0 {
				return nil
			}
		} else if running < d.slots(st) && (!paused || running == 0) {
			s, ok, err := d.nextFree(ctx, st.ID)
			if err != nil {
				return err
			}
			if ok {
				d.mu.Lock()
				d.runs[s.ID] = true // (before it starts: the next pick doesn't take it again)
				d.mu.Unlock()
				running++
				wg.Add(1)
				go func() {
					defer wg.Done()
					d.run(ctx, st, s)
					ended <- struct{}{}
				}()
				continue
			}
		}
		if running == 0 {
			return nil
		}
		select {
		case <-ended:
			running--
			if !st.Client.Paused().Until.IsZero() {
				stoppedPaused = true // (it paused on the way: the searches wait where they were)
			}
		case <-d.wakeChan(site):
		case <-ctx.Done():
		}
	}
	return nil
}
