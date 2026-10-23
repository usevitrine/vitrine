package download

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/plugin"
	"github.com/usevitrine/vitrine/internal/tag"
)

// Galleries: a post that is a whole gallery (plugin.Post.Gallery) comes in as
// a comic on the Comics page - never as library files. Its pages are fetched
// skipping the ones the comic has (so a stopped download carries on from
// where it was); a newer version of the gallery (the same first version)
// updates the same comic, its new pages fetched (the ones it has stay). A page
// whose key (plugin.GalleryEngine) the library has - in this comic or another -
// is used as it is: neither its page nor its picture is asked for. The site's
// image limit stops the search without counting the gallery as done
// (errStopSearch): it carries on later.
//
// Each page is two requests: the picture's page on the site (paced by the
// site's rate limit, one after another) and the picture itself, from one of
// many image servers (the site's connections at once). The picture pages are
// read ahead while pictures download, so the rate limit is the only wait.

// errStopSearch wraps an error that stops the search with the post not done
// (it's handled again when the search goes on).
type errStopSearch struct{ err error }

func (e errStopSearch) Error() string { return e.err.Error() }
func (e errStopSearch) Unwrap() error { return e.err }

func (d *Downloader) handleGallery(ctx context.Context, st *Site, p plugin.Post, bl *library.Blacklist) (int, uint32, error) {
	ge, ok := st.Engine.(plugin.GalleryEngine)
	if !ok {
		return Failed, 0, errors.New("the site's engine can't download galleries")
	}
	if bl.Match(p.Tags) != "" {
		return Blacklisted, 0, nil
	}
	m := st.Client.Manifest()
	g := p.Gallery
	// A newer version of a gallery the library has (an ongoing one: pages added): the
	// pages the comic has stay, the new ones are fetched - from where it got to.
	_, before, err := d.lib.ComicFrom(ctx, m.ID, g.Key)
	if err != nil {
		return Failed, 0, err
	}
	newVersion := before != "" && before != p.PostURL
	info := galleryInfo(st, p)
	id, _, err := d.lib.AddComic(ctx, info)
	if err != nil {
		return Failed, 0, err
	}
	have, err := d.lib.ComicPageKeys(ctx, id)
	if err != nil {
		return Failed, 0, err
	}
	if !newVersion && g.Pages > 0 && len(have) >= g.Pages {
		return Already, 0, nil
	}
	prog, _ := ctx.Value(progressKey{}).(*fileProgress)
	if prog != nil {
		prog.gallery = true
		prog.pages.Store(int64(g.Pages))
		prog.done.Store(int64(len(have)))
	}
	// (a newer version: every page checked by its key - pages can be added in the middle, or changed)
	added, _, err := d.galleryPages(ctx, st, ge, id, p.ID, g, have, newVersion, prog)
	if added > 0 && prog != nil && prog.search != 0 {
		// (its pages counted on the download: one gallery's progress is by page)
		d.db.W.ExecContext(ctx, "UPDATE searches SET pages = pages + ? WHERE id = ?", added, prog.search)
	}
	if err != nil {
		return Failed, 0, galleryErr(err)
	}
	if newVersion && g.Pages > 0 && len(have) > g.Pages { // (a version with fewer pages: the rest go)
		if err := d.lib.TrimComicPages(ctx, id, g.Pages); err != nil {
			return Failed, 0, err
		}
	}
	if added > 0 {
		return Downloaded, 0, nil
	}
	return Already, 0, nil
}

// galleryInfo is what a gallery post says about its comic.
func galleryInfo(st *Site, p plugin.Post) library.ComicInfo {
	m := st.Client.Manifest()
	g := p.Gallery
	tags := make([]tag.Tag, 0, len(p.Tags)+1)
	// Each tag with its own namespace (the engine's SiteTags: a name can be in two - female:glasses, male:glasses -
	// which TagTypes, by name, made one tag twice).
	site := g.SiteTags
	if site == nil {
		for _, t := range p.Tags {
			site = append(site, plugin.SiteTag{NS: p.TagTypes[t], Name: t})
		}
	}
	seen := map[tag.Tag]bool{}
	for _, t := range site {
		ns := t.NS
		if v, ok := m.Tags.Namespaces[ns]; ok {
			ns = v
		}
		if tg := (tag.Tag{Namespace: ns, Name: t.Name}); !seen[tg] {
			seen[tg] = true
			tags = append(tags, tg)
		}
	}
	if c := strings.TrimSpace(g.Category); c != "" {
		// The category as a tag too (category:manga): searched and filtered like the others.
		tags = append(tags, tag.Tag{Namespace: "category", Name: strings.ReplaceAll(strings.ToLower(c), " ", "_")})
	}
	info := library.ComicInfo{Title: g.Title, TitleAlt: g.TitleAlt, Site: m.ID, Gallery: g.Key, URL: p.PostURL, Category: g.Category,
		Pages: g.Pages, Tags: tags}
	if !p.Posted.IsZero() {
		info.PostedAt = p.Posted.UnixMilli()
	}
	return info
}

// RefreshComics reads comics' galleries from their sites again: title, page
// count and tags (a site's tags replace its earlier ones). Pages aren't
// fetched (a download of the gallery does that, a newer version included).
// It returns how many comics were read.
func (d *Downloader) RefreshComics(ctx context.Context, ids []int64) (int, error) {
	list, err := d.lib.ComicsByID(ctx, ids)
	if err != nil {
		return 0, err
	}
	n, asked := 0, 0
	var paused error // (a comic from a paused site is left out: the others still refresh)
	for _, c := range list {
		st, ok := d.all()[c.Site]
		if !ok || c.URL == "" {
			continue
		}
		if err := waitingErr(st); err != nil {
			paused = err
			continue
		}
		asked++
		page, err := st.Engine.Search(ctx, c.URL, 0)
		if err != nil {
			return n, fmt.Errorf("%s: %w", c.Site, err)
		}
		for _, p := range page.Posts {
			if p.Gallery == nil || p.Gallery.Key != c.Gallery {
				continue
			}
			info := galleryInfo(st, p)
			info.URL = "" // (kept: a download then sees a newer version by its link)
			if _, _, err := d.lib.AddComic(ctx, info); err != nil {
				return n, err
			}
			n++
			break
		}
	}
	if asked == 0 && paused != nil {
		return 0, paused
	}
	return n, nil
}

// galleryPages fetches the pages a comic doesn't have yet: the picture pages
// in order as the gallery lists them (rate limited; a stopped download lists
// from the first page it lacks - recheck: from the first, each page's key
// compared), the pictures up to the site's connections at once. A page whose
// key the library has is used without asking the site. It also returns how many pages the gallery listed (from the first).
// It returns how many pages the comic gained; the first error stops reading more picture
// pages (pictures already on their way finish; pages stored stay).
func (d *Downloader) galleryPages(ctx context.Context, st *Site, ge plugin.GalleryEngine, comic, gid int64, g *plugin.Gallery,
	have map[int]library.PageKey, recheck bool, prog *fileProgress) (int, int, error) {
	m := st.Client.Manifest()
	feed, stop := context.WithCancel(ctx) // (the picture pages: stopped by an error)
	defer stop()
	type job struct {
		i                    int
		page, img, next, key string
	}
	total := 0 // (pages listed: the last index + 1; under mu)
	jobs := make(chan job)
	var (
		mu       sync.Mutex
		firstErr error
		added    int
	)
	of := func() int {
		mu.Lock()
		defer mu.Unlock()
		return max(g.Pages, total)
	}
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
		stop()
	}
	var wg sync.WaitGroup
	for range max(1, m.Limits.Connections) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				_, err := d.galleryPage(ctx, st, ge, comic, j.i, j.page, j.img, j.next)
				switch {
				case errors.Is(err, media.ErrUnsupported):
					d.log.Info("a gallery page isn't a picture Vitrine keeps", "site", m.ID, "comic", comic, "page", j.i+1)
				case err != nil:
					fail(fmt.Errorf("page %d of %d: %w", j.i+1, of(), err))
					continue
				default: // (a page the comic gained, even a file the store had)
					if j.key != "" {
						if err := d.lib.SetComicPageKey(ctx, comic, j.i, j.key); err != nil {
							d.log.Warn("keeping a comic page's key", "comic", comic, "page", j.i+1, "err", err)
						}
					}
					mu.Lock()
					added++
					mu.Unlock()
				}
				if prog != nil {
					prog.done.Add(1)
				}
			}
		}()
	}
	// The picture pages, in order, as the gallery lists them.
	from := 0
	for _, ok := have[from]; ok && !recheck; _, ok = have[from] {
		from++
	}
	byKey := map[string]uint32{} // (this comic's pages as they were: a newer version can move them)
	for _, k := range have {
		if k.Key != "" {
			byKey[k.Key] = k.File
		}
	}
	errStop := errors.New("stopped")
	err := ge.GalleryPages(feed, gid, g, from, func(i int, pageURL, key string) error {
		mu.Lock()
		total = max(total, i+1)
		mu.Unlock()
		if prog != nil && int64(total) > prog.pages.Load() {
			prog.pages.Store(int64(total))
		}
		if k, ok := have[i]; ok && (key == "" || k.Key == "" || k.Key == key) {
			if k.Key == "" && key != "" { // (a page from before keys were kept: its key from now on)
				if err := d.lib.SetComicPageKey(ctx, comic, i, key); err != nil {
					return err
				}
			}
			return nil
		}
		// The picture in the library already (this comic's, moved in a newer version, or another comic's): used.
		if key != "" {
			file := byKey[key]
			if file == 0 {
				var err error
				if file, err = d.lib.PageByKey(feed, key); err != nil {
					return err
				}
			}
			if file != 0 {
				if err := d.lib.LinkComicPage(ctx, comic, i, file, key); err != nil {
					return err
				}
				mu.Lock()
				added++
				mu.Unlock()
				if prog != nil {
					prog.done.Add(1)
				}
				return nil
			}
		}
		img, next, err := ge.PageImage(feed, pageURL, "")
		if feed.Err() != nil {
			return errStop
		}
		if err != nil {
			return fmt.Errorf("page %d of %d: %w", i+1, of(), err)
		}
		select {
		case jobs <- job{i, pageURL, img, next, key}:
		case <-feed.Done():
			return errStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) && feed.Err() == nil {
		fail(err)
	}
	close(jobs)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if firstErr == nil && ctx.Err() != nil {
		firstErr = ctx.Err()
	}
	return added, total, firstErr
}

// galleryPage fetches one page's picture into a comic: from the server the
// picture's page named (img), or (if that fails) another one it offers.
func (d *Downloader) galleryPage(ctx context.Context, st *Site, ge plugin.GalleryEngine, comic int64, page int, pageURL, img, next string) (library.File, error) {
	reload := ""
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			var err error
			if img, next, err = ge.PageImage(ctx, pageURL, reload); err != nil {
				return library.File{}, err
			}
		}
		body, size, err := st.Client.File(ctx, img)
		if err == nil {
			body = d.countBody(ctx, body, size)
			f, err := d.lib.AddComicPage(ctx, comic, page, body)
			body.Close()
			if err == nil || errors.Is(err, media.ErrUnsupported) || ctx.Err() != nil {
				return f, err
			}
			lastErr = err
		} else {
			lastErr = err
		}
		if ctx.Err() != nil {
			return library.File{}, ctx.Err()
		}
		if next == "" || next == reload {
			break
		}
		reload = next
		d.log.Info("a gallery picture failed; asking for another server", "site", st.Client.Manifest().ID, "err", lastErr)
	}
	return library.File{}, lastErr
}

// galleryErr marks the errors that stop the whole search (the image limit, a
// login the site refuses, a blocked country): later pages would fail the same way.
func galleryErr(err error) error {
	if errors.Is(err, plugin.ErrImageLimit) || errors.Is(err, plugin.ErrSadPanda) || errors.Is(err, plugin.ErrBanned) || errors.Is(err, plugin.ErrRegionBlocked) ||
		errors.Is(err, plugin.ErrLoginRefused) || errors.Is(err, plugin.ErrNoCredentials) {
		return errStopSearch{err}
	}
	return err
}
