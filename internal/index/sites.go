package index

import (
	"context"
	"database/sql"
	"strings"

	"github.com/RoaringBitmap/roaring/v2"
)

// Sites: which files came from which site (a file the same on two sites is
// in both), for system:site=... A site is named by its plugin ID or, once the
// downloader has said so, its address ("safebooru" or "safebooru.org").

// loadSiteAliases registers each site's name (its plugin ID) for system:site=.
func (ix *Index) loadSiteAliases(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, name FROM sites")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint32
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		ix.siteAlias[strings.ToLower(name)] = id
	}
	return rows.Err()
}

func (ix *Index) loadSites(ctx context.Context, tx *sql.Tx) error {
	if err := ix.loadSiteAliases(ctx, tx); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT file_id, site_id FROM file_urls WHERE 1"+ix.only("file_id"))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var file, site uint32
		if err := rows.Scan(&file, &site); err != nil {
			return err
		}
		ix.addFileSite(file, site)
	}
	return rows.Err()
}

// AddSiteAlias lets system:site= name a site this way too (its address).
func (ix *Index) AddSiteAlias(alias string, site uint32) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.siteAlias[strings.ToLower(alias)] = site
}

// AddFileSite records that a file came from a site.
func (ix *Index) AddFileSite(file, site uint32) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.addFileSite(file, site)
}

// RemoveFileSite takes back AddFileSite (e.g. undoing a merge of duplicates).
func (ix *Index) RemoveFileSite(file, site uint32) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if bm := ix.siteFiles[site]; bm != nil {
		bm.Remove(file)
	}
}

func (ix *Index) addFileSite(file, site uint32) {
	bm := ix.siteFiles[site]
	if bm == nil {
		bm = roaring.New()
		ix.siteFiles[site] = bm
	}
	bm.Add(file)
}

// siteBitmap is the files from the named site (nil if no such site).
func (ix *Index) siteBitmap(name string) *roaring.Bitmap {
	id, ok := ix.siteAlias[strings.ToLower(name)]
	if !ok {
		return nil
	}
	return ix.siteFiles[id]
}

// sitesBitmap returns the files from any of a comma-separated list of sites
// (system:site=a,b), nil when none of them has files. One site: the index's
// own bitmap (don't modify it).
func (ix *Index) sitesBitmap(list string) *roaring.Bitmap {
	var out *roaring.Bitmap
	shared := false
	for _, name := range strings.Split(list, ",") {
		bm := ix.siteBitmap(strings.TrimSpace(name))
		switch {
		case bm == nil:
		case out == nil:
			out, shared = bm, true
		default:
			if shared {
				out, shared = out.Clone(), false
			}
			out.Or(bm)
		}
	}
	return out
}

// SiteCounts counts, per site, the files of ids that came from it.
func (ix *Index) SiteCounts(ids []uint32) map[uint32]int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	in := roaring.BitmapOf(ids...)
	out := map[uint32]int{}
	for site, bm := range ix.siteFiles {
		if n := int(in.AndCardinality(bm)); n > 0 {
			out[site] = n
		}
	}
	return out
}

// Found by: the files each subscription downloaded, for system:found_by=<its ID> (a subscription's "new" files on
// the Downloads page open as a search).

func (ix *Index) loadFoundBy(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT file_id, subscription_id FROM file_sources WHERE subscription_id IS NOT NULL AND downloaded = 1"+ix.only("file_id"))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var file, sub uint32
		if err := rows.Scan(&file, &sub); err != nil {
			return err
		}
		ix.addFoundBy(file, sub)
	}
	return rows.Err()
}

// AddFoundBy records that a subscription downloaded a file.
func (ix *Index) AddFoundBy(file, sub uint32) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.addFoundBy(file, sub)
}

func (ix *Index) addFoundBy(file, sub uint32) {
	bm := ix.subFiles[sub]
	if bm == nil {
		bm = roaring.New()
		ix.subFiles[sub] = bm
	}
	bm.Add(file)
}

// SiteAliases is every name system:site= knows a site by (its plugin ID, its address).
func (ix *Index) SiteAliases(site uint32) []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []string
	for name, id := range ix.siteAlias {
		if id == site {
			out = append(out, name)
		}
	}
	return out
}

// DropSite forgets a site: its names, which files came from it, and what its subscriptions found (a site wiped:
// Library.WipeSite).
func (ix *Index) DropSite(site uint32, subs []uint32) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for name, id := range ix.siteAlias {
		if id == site {
			delete(ix.siteAlias, name)
		}
	}
	delete(ix.siteFiles, site)
	for _, s := range subs {
		delete(ix.subFiles, s)
	}
}
