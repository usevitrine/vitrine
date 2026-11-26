package download

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// Post pages opened ahead (2026-10-21). A site whose list pages give only IDs and md5s (the HTML engines: realbooru,
// Shimmie2, favorites pages) needs each post's page before its file; one after the other, every post waited for its
// page (and the requests' pace) after the file before it. Now, while a file downloads, the next post that will need
// its page has it opened (at the same pace: measured on realbooru 2026-10-21, a page alongside a download didn't slow
// it, while extra file connections are throttled to almost nothing). The post then takes it, or asks again itself
// if that failed.
type prefetcher struct {
	ctx context.Context // the search's: a lookup outlives the post that started it
	st  *Site
	mu  sync.Mutex
	m   map[int64]*lookup
}

type lookup struct {
	done chan struct{}
	post *plugin.Post
	err  error
}

func newPrefetcher(ctx context.Context, st *Site) *prefetcher {
	return &prefetcher{ctx: ctx, st: st, m: map[int64]*lookup{}}
}

// start opens a post's page in the background (once).
func (pf *prefetcher) start(id int64) {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	if _, ok := pf.m[id]; ok {
		return
	}
	l := &lookup{done: make(chan struct{})}
	pf.m[id] = l
	go func() {
		defer close(l.done)
		l.post, l.err = pf.st.Engine.Lookup(pf.ctx, id)
	}()
}

// take returns a page opened ahead for the post, if one was (waiting for it if it's still on its way).
func (pf *prefetcher) take(ctx context.Context, id int64) (*plugin.Post, error, bool) {
	pf.mu.Lock()
	l, ok := pf.m[id]
	delete(pf.m, id)
	pf.mu.Unlock()
	if !ok {
		return nil, nil, false
	}
	select {
	case <-l.done:
		return l.post, l.err, true
	case <-ctx.Done():
		return nil, ctx.Err(), true
	}
}

// ahead is what a post being handled may open ahead: the posts after it on the list page.
type ahead struct {
	pf    *prefetcher
	posts []plugin.Post
}

type aheadKey struct{}

func withAhead(ctx context.Context, pf *prefetcher, posts []plugin.Post) context.Context {
	return context.WithValue(ctx, aheadKey{}, &ahead{pf: pf, posts: posts})
}

func aheadOf(ctx context.Context) *ahead {
	a, _ := ctx.Value(aheadKey{}).(*ahead)
	return a
}

// openNext opens the page of the first of the posts ahead that will need it (see needsPage).
func (d *Downloader) openNext(ctx context.Context, a *ahead) {
	if a == nil {
		return
	}
	for _, p := range a.posts {
		if d.needsPage(ctx, a.pf.st, p) {
			a.pf.start(p.ID)
			return
		}
	}
}

// needsPage reports whether handle will open the post's page: a post from a list page with only its ID and md5,
// not seen before (or seen and failed, blacklisted or too big: tried again), and not a file deleted before.
func (d *Downloader) needsPage(ctx context.Context, st *Site, p plugin.Post) bool {
	if !p.Partial || p.Gallery != nil || p.ID <= 0 {
		return false
	}
	var prev int
	err := d.db.R.QueryRowContext(ctx, "SELECT outcome FROM seen_posts WHERE site_id = ? AND post_id = ?", st.ID, p.ID).Scan(&prev)
	if err == nil && prev != Failed && prev != Blacklisted && prev != TooBig {
		return false
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if p.MD5 != [16]byte{} {
		if m, err := d.lib.FindMD5(ctx, p.MD5); err != nil || (m.Deleted && m.FileID == 0) {
			return false
		}
	}
	return true
}
