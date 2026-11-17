package download

import (
	"context"
	"fmt"
	"sync"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// Counting the queue's searches on their sites (2026-11-14, the user: the Queue tab sorted by file count, most or
// fewest first - "if they pick sort by file count, it gets the counts first automatically"). A waiting search's count is
// its site's total for it: one request for its first page, in the site's turn (a request someone is waiting for: the
// foreground's), with the account's login - as the search itself asks when it starts. Kept in searches.site_total, which
// the search's own first page sets again when it runs.

// siteNoCount: the site said nothing about how many (a Mastodon site, say) - not asked again.
const siteNoCount = -2

// CountSearches asks the sites how many posts each of the given waiting searches has (none given: every waiting one
// not counted yet), one site at a time in its own turn, all sites at once; the page hears of each count (Changed).
// Favorites lists are left out (their total is the whole list, read whole), as are searches counted already.
// It returns how many it set out to count, and works in the background.
func (d *Downloader) CountSearches(ctx context.Context, ids []int64) (int, error) {
	return d.countWaiting(ctx, ids, "")
}

// countSite counts a site's waiting searches not counted yet - not its checks (the queue doesn't sort them by count) -
// when the queue runs by count (2026-11-17, the user: "when i add new downloads it doesnt seem to count the files on
// the site in the background, i have to navigate to the page"): wake calls it whenever something is queued there.
func (d *Downloader) countSite(site string) {
	if _, err := d.countWaiting(context.Background(), nil, site); err != nil {
		d.log.Debug("counting a site's queue", "site", site, "err", err)
	}
}

// countWaiting is CountSearches; site: only that site's, and no checks.
func (d *Downloader) countWaiting(ctx context.Context, ids []int64, onlySite string) (int, error) {
	rows, err := d.db.R.QueryContext(ctx, `SELECT s.id, si.name, s.query FROM searches s JOIN sites si ON si.id = s.site_id
		WHERE s.state IN ('queued', 'paused') AND s.site_total = -1 AND (?1 = '' OR (si.name = ?1 AND NOT `+checkSQL+`))
		ORDER BY s.position`, onlySite)
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	type job struct {
		id    int64
		query string
	}
	bySite := map[string][]job{}
	n := 0
	for rows.Next() {
		var j job
		var site string
		if err := rows.Scan(&j.id, &site, &j.query); err != nil {
			rows.Close()
			return 0, fmt.Errorf("download: %w", err)
		}
		if (len(want) > 0 && !want[j.id]) || plugin.IsFavorites(j.query) {
			continue
		}
		bySite[site] = append(bySite[site], j)
		n++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	ctx = context.WithoutCancel(ctx) // (it outlives the request that asked)
	sites := d.all()
	for site, jobs := range bySite {
		st := sites[site]
		if st == nil {
			continue
		}
		go func() {
			if !d.countBegin(site) {
				d.countAgain.Store(d.countKey(site), true) // (that site is counting already: it looks again when done)
				return
			}
			for _, j := range jobs {
				total := d.countOne(ctx, st, j.query)
				d.db.W.ExecContext(ctx, "UPDATE searches SET site_total = ? WHERE id = ? AND site_total = -1", total, j.id)
				d.Changed()
			}
			d.countEnd(site)
			// Asked again while it counted (a search added meanwhile): the site's waiting ones not counted yet, once more.
			if _, again := d.countAgain.LoadAndDelete(d.countKey(site)); again {
				d.countSite(site)
			}
		}()
	}
	return n, nil
}

// countOne is a search's total on its site (siteNoCount if the site doesn't say; -1 when the site couldn't be asked).
func (d *Downloader) countOne(ctx context.Context, st *Site, query string) int {
	m := st.Client.Manifest()
	if _, single, _ := plugin.SinglePost(m, query); single {
		return 1
	}
	page, err := st.Engine.Search(plugin.Foreground(ctx), query, 0)
	switch {
	case err != nil:
		d.log.Debug("counting a search on its site", "site", m.ID, "err", err)
		return -1 // (asked again next time)
	case page.Total < 0:
		return siteNoCount
	}
	return page.Total
}

// One counting run per site at a time; a second ask while one runs makes it look again when it's done (countAgain).
var counting sync.Map

func (d *Downloader) countBegin(site string) bool {
	_, busy := counting.LoadOrStore(d.countKey(site), true)
	return !busy
}

func (d *Downloader) countEnd(site string) { counting.Delete(d.countKey(site)) }

func (d *Downloader) countKey(site string) string { return fmt.Sprintf("%p %s", d, site) }

// Counting says which sites are counting their searches now (the page shows them as counting).
func (d *Downloader) Counting() []string {
	var out []string
	prefix := fmt.Sprintf("%p ", d)
	counting.Range(func(k, _ any) bool {
		if s := k.(string); len(s) > len(prefix) && s[:len(prefix)] == prefix {
			out = append(out, s[len(prefix):])
		}
		return true
	})
	return out
}
