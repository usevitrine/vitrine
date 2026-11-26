package download

import (
	"context"
	"fmt"
)

// Check depth: how many posts a subscription's check goes on past the newest
// post the last check saw - posts tagged after they were checked turn up
// further down a search, and a check that stops at once misses them. It's set
// on the subscription, else its group, else its site, else for all (the
// default 0 stops at once).

// depths are the check depths set on groups, sites and for all.
type depths struct {
	groups map[int64]int  // group ID -> its depth (set ones only)
	sites  map[string]int // plugin ID -> its depth
	all    int
}

// depthSource reads every level a subscription's check depth can come from.
func (d *Downloader) depthSource(ctx context.Context) (depths, error) {
	out := depths{groups: map[int64]int{}}
	all, sites, err := d.lib.CheckDepths(ctx)
	if err != nil {
		return out, fmt.Errorf("download: %w", err)
	}
	if all != nil {
		out.all = *all
	}
	out.sites = sites
	rows, err := d.db.R.QueryContext(ctx, "SELECT id, check_depth FROM sub_groups WHERE check_depth IS NOT NULL")
	if err != nil {
		return out, fmt.Errorf("download: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return out, fmt.Errorf("download: %w", err)
		}
		out.groups[id] = n
	}
	return out, rows.Err()
}

// of is a subscription's check depth.
func (ds depths) of(s Subscription) int {
	if s.CheckDepth != nil {
		return *s.CheckDepth
	}
	return ds.ofGroup(s.GroupID, s.Site)
}

// ofGroup is the depth a group's subscriptions get when they set none (group 0 = none).
func (ds depths) ofGroup(group int64, site string) int {
	if n, ok := ds.groups[group]; ok && group != 0 {
		return n
	}
	if n, ok := ds.sites[site]; ok {
		return n
	}
	return ds.all
}
