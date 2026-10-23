package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/plugin"
)

// Tag types: sites that say what a tag is (artist, character, ...) give
// their tags namespaces (the plugin maps types to namespaces). Types arrive
// with each page of a search and are saved in tag_types; files downloaded
// before their tags' types were known are filled in by LearnTypes, which
// looks up their posts one at a time while the site's queue is idle.

// namespaces maps a post's tags to namespaces from the site's tag types.
func namespaces(m plugin.Manifest, p plugin.Post) map[string]string {
	if p.TagTypes == nil {
		return nil
	}
	out := map[string]string{}
	for _, t := range p.Tags {
		if ns := m.Namespace(p.TagTypes[t]); ns != "" {
			out[t] = ns
		}
	}
	return out
}

// recordTypes saves what the site said about its tags.
func (d *Downloader) recordTypes(ctx context.Context, siteID uint32, types map[string]string) error {
	if len(types) == 0 {
		return nil
	}
	tx, err := d.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO tag_types (site_id, name, type, checked_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (site_id, name) DO UPDATE SET type = excluded.type, checked_at = excluded.checked_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := d.now().UnixMilli()
	for name, typ := range types {
		if _, err := stmt.ExecContext(ctx, siteID, name, typ, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// learnsTypes reports whether a site's plugin can tell tag types.
func (st *Site) learnsTypes() bool { return st.Client.Manifest().Tags.Types != "" }

// LearnTypes looks up one post whose file has site tags with no known type
// (the post with the most of them, so few lookups cover the library) and
// writes the post again with namespaces. It reports whether there was such a
// post. A post the site no longer has marks its tags as looked up (”).
func (d *Downloader) LearnTypes(ctx context.Context, site string) (bool, error) {
	st, ok := d.all()[site]
	if !ok {
		return false, fmt.Errorf("%w: %q", ErrUnknownSite, site)
	}
	if !st.learnsTypes() {
		return false, nil
	}
	var postID int64
	var fileID uint32
	err := d.db.R.QueryRowContext(ctx, `
		SELECT sp.post_id, sp.file_id FROM seen_posts sp
		JOIN files f ON f.id = sp.file_id AND f.deleted_at IS NULL
		JOIN file_tags ft ON ft.file_id = sp.file_id AND ft.origin = sp.site_id
		JOIN tags t ON t.id = ft.tag_id
		WHERE sp.site_id = ?1
			AND NOT EXISTS (SELECT 1 FROM tag_types tt WHERE tt.site_id = ?1 AND tt.name = t.name)
		GROUP BY sp.post_id ORDER BY count(*) DESC, sp.post_id DESC LIMIT 1`, st.ID).Scan(&postID, &fileID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("download: %w", err)
	}

	if _, err := d.refreshPost(ctx, st, postID, fileID); err != nil {
		return true, err
	}
	// Whatever is still unknown on this file (the site dropped the post or
	// those tags) counts as looked up, so the same post isn't picked again.
	_, err = d.db.W.ExecContext(ctx, `
		INSERT INTO tag_types (site_id, name, type, checked_at)
		SELECT ?1, t.name, '', ?2 FROM file_tags ft JOIN tags t ON t.id = ft.tag_id
		WHERE ft.file_id = ?3 AND ft.origin = ?1
		ON CONFLICT (site_id, name) DO NOTHING`, st.ID, d.now().UnixMilli(), fileID)
	if err != nil {
		return true, fmt.Errorf("download: %w", err)
	}
	return true, nil
}

// refreshPost reads a post from the site again and writes what it says now
// (tags with namespaces, links, rating) to its file; the post's seen_at
// becomes now (also when the site no longer has it, so it isn't asked again
// before its next turn). Reports whether the site still has the post.
func (d *Downloader) refreshPost(ctx context.Context, st *Site, postID int64, fileID uint32) (bool, error) {
	post, err := st.Engine.Lookup(ctx, postID)
	if err != nil {
		return false, err
	}
	var undated bool // (its file has no upload time yet: asked for it - a refresh otherwise needn't)
	if d.db.R.QueryRowContext(ctx, "SELECT posted_at IS NULL FROM files WHERE id = ?", fileID).Scan(&undated); undated {
		d.dated(ctx, st, post)
	}
	if post != nil {
		if st.learnsTypes() && post.TagTypes == nil {
			return true, errors.New("the site sent no tag types")
		}
		if err := d.recordTypes(ctx, st.ID, post.TagTypes); err != nil {
			return true, err
		}
		sp := library.SitePost{Tags: post.Tags, PostURL: post.PostURL, Sources: post.Sources, FileURL: post.FileURL,
			Rating: post.Rating, Namespaces: namespaces(st.Client.Manifest(), *post)}
		if !post.Posted.IsZero() {
			sp.PostedAt = post.Posted.UnixMilli()
		}
		bl, err := d.lib.Blacklist(ctx)
		if err != nil {
			return true, err
		}
		if err := d.setSitePost(ctx, st, fileID, sp, bl); err != nil {
			return true, err
		}
	}
	if _, err := d.db.W.ExecContext(ctx, "UPDATE seen_posts SET seen_at = ? WHERE site_id = ? AND post_id = ?",
		d.now().UnixMilli(), st.ID, postID); err != nil {
		return post != nil, fmt.Errorf("download: %w", err)
	}
	return post != nil, nil
}

// Background work (tag-type lookups, then tag refreshes) leaves the site's
// rate limit mostly to searches: a pause between posts, and a back-off after
// an error. A site answered 429 Too Many Requests after two minutes of
// back-to-back lookups.
const (
	backgroundPause   = 6 * time.Second
	backgroundBackoff = 15 * time.Minute
)

// background works on a site while its queue is empty: first files whose
// tags' types aren't known yet, then posts due for a tag refresh, one post at
// a time, until there's nothing left, the site gets a search or a
// subscription check, or ctx ends.
func (d *Downloader) background(ctx context.Context, site string) {
	st := d.all()[site]
	// (a paused site: nothing until its next try is due - then the next lookup or refresh is that try)
	if d.now().Before(st.backgroundAfter) || st.Client.Waiting() {
		return
	}
	learned, refreshed := 0, 0
	defer func() {
		if learned+refreshed > 0 {
			d.log.Info("background work done", "site", site, "types_looked_up", learned, "refreshed", refreshed)
		}
	}()
	for ctx.Err() == nil && len(d.wakeChan(site)) == 0 {
		more, err := d.LearnTypes(ctx, site)
		if err == nil && more {
			learned++
		} else if err == nil {
			if more, err = d.FixPosted(ctx, site); err == nil && !more {
				more, err = d.RefreshDue(ctx, site)
			}
			if more {
				refreshed++
			}
		}
		if err != nil {
			// (the site paused, or is: its pause decides when this goes on - it carries on as soon as the site answers)
			if ctx.Err() == nil && !pausedBy(st, err) {
				st.backgroundAfter = d.now().Add(backgroundBackoff)
				d.log.Warn("tag lookups paused", "site", site, "for", backgroundBackoff, "err", err)
			}
			return
		}
		if !more {
			return
		}
		if n := learned + refreshed; n%100 == 0 {
			d.log.Info("background work", "site", site, "types_looked_up", learned, "refreshed", refreshed)
		}
		select {
		case <-time.After(backgroundPause):
		case <-ctx.Done():
		}
	}
}

// FixPosted is a one-off pass (2026-10-21) over a site's files whose upload time is a day only - pages that say
// "Posted at Oct, 14 2026" (the gelbooru-v02-html engine), downloaded before their file's Last-Modified was used for
// the moment: one file per call, its Last-Modified asked for (the file itself not downloaded), the moment taken when
// it falls on that day (refinePosted). How far it got is kept (meta "posted_fix:<site>": the last file ID, then
// "done"), so a restart carries on. more: there may be another.
func (d *Downloader) FixPosted(ctx context.Context, site string) (more bool, err error) {
	st := d.all()[site]
	if st == nil || st.Client.Manifest().Engine != "gelbooru-v02-html" {
		return false, nil
	}
	key := "posted_fix:" + site
	var mark string
	if err := d.db.R.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&mark); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if mark == "done" {
		return false, nil
	}
	after, _ := strconv.ParseInt(mark, 10, 64)
	save := func(v string) error {
		_, err := d.db.W.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", key, v)
		return err
	}
	var id, posted int64
	var fileURL string
	err = d.db.R.QueryRowContext(ctx, `SELECT f.id, f.posted_at, u.url FROM files f
		JOIN file_urls fu ON fu.file_id = f.id AND fu.kind = 3 AND fu.site_id = ?
		JOIN urls u ON u.id = fu.url_id
		WHERE f.id > ? AND f.posted_at IS NOT NULL AND f.posted_at % 86400000 = 0 AND f.deleted_at IS NULL
		ORDER BY f.id LIMIT 1`, st.ID, after).Scan(&id, &posted, &fileURL)
	if errors.Is(err, sql.ErrNoRows) {
		d.log.Info("upload times fixed", "site", site, "files", st.postedFixed)
		return false, save("done")
	}
	if err != nil {
		return false, err
	}
	modified, err := st.Client.Modified(ctx, fileURL)
	var se *plugin.StatusError
	switch {
	case errors.As(err, &se) && se.Code < 500 && se.Code != http.StatusTooManyRequests, errors.Is(err, plugin.ErrNotAFile):
		// gone from the site (or a page instead): left as it is
	case err != nil:
		return false, err // (the site paused or not answering: the background work's own wait, then this file again)
	default:
		if at := refinePosted(time.UnixMilli(posted), modified).UnixMilli(); at != posted {
			if err := d.lib.SetPostedAt(ctx, uint32(id), at); err != nil {
				return false, err
			}
			st.postedFixed++
			if st.postedFixed%200 == 0 {
				d.log.Info("fixing upload times", "site", site, "files", st.postedFixed, "at_file", id)
			}
		}
	}
	return true, save(strconv.FormatInt(id, 10))
}
