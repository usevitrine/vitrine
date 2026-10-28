package download

import (
	"context"
	"io"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Live updates: watchers hear when the queue changes (a post handled, a
// search started or finished, ...) and can read the files downloading now.

type liveState struct {
	mu       sync.Mutex
	watchers map[chan struct{}]struct{}
	files    map[*fileProgress]struct{}
	// Bytes read per search, and a few recent readings of them (Speeds).
	read     map[int64]*atomic.Int64
	readings map[int64][]reading
}

// reading is a search's bytes read so far at a moment.
type reading struct {
	at time.Time
	n  int64
}

// speedWindow: the download speed is the average over this long (2026-10-28, the user: rule34's speed "keeps jumping
// around from 40 MB/s down to single digit MB/s" - read file by file in the page, a big video showed 40, a run of small
// pictures a few, and a small file begun and done between two looks not at all, while the minute average stayed
// 10-20 MB/s).
const speedWindow = 10 * time.Second

// fileProgress is one file being downloaded.
type fileProgress struct {
	search int64
	post   int64
	sum    *atomic.Int64 // its search's bytes read
	site   string        // (a retried failed post: no search - see RetryJob)
	read   atomic.Int64
	total  atomic.Int64 // -1 = unknown
	// A gallery (a comic): its pages, how many are stored; read counts all its pictures.
	gallery     bool
	pages, done atomic.Int64
}

// FileProgress is a snapshot of a file being downloaded.
type FileProgress struct {
	SearchID int64  `json:"search"`
	PostID   int64  `json:"post"`
	Site     string `json:"site,omitempty"` // a retried failed post (search 0): its site
	Key      string `json:"key,omitempty"`  // ... and "site/post" (a Mastodon ID is too big for a JavaScript number)
	Read     int64  `json:"read"`
	Total    int64  `json:"total"` // -1 if the site didn't say
	// A gallery: how many of its pages are stored.
	Pages int64 `json:"pages,omitempty"`
	Done  int64 `json:"done,omitempty"`
}

// Watch returns a channel that gets a signal (coalesced: at most one
// waiting) whenever the queue changes; call stop when done.
func (d *Downloader) Watch() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	d.live.mu.Lock()
	if d.live.watchers == nil {
		d.live.watchers = map[chan struct{}]struct{}{}
	}
	d.live.watchers[ch] = struct{}{}
	d.live.mu.Unlock()
	return ch, func() {
		d.live.mu.Lock()
		delete(d.live.watchers, ch)
		d.live.mu.Unlock()
	}
}

// Changed tells the watchers that the queue changed.
func (d *Downloader) Changed() {
	d.live.mu.Lock()
	defer d.live.mu.Unlock()
	for ch := range d.live.watchers {
		select {
		case ch <- struct{}{}:
		default: // one is already waiting
		}
	}
}

// Progress lists the files downloading now, by search then post (newest first).
func (d *Downloader) Progress() []FileProgress {
	d.live.mu.Lock()
	out := make([]FileProgress, 0, len(d.live.files))
	for f := range d.live.files {
		out = append(out, FileProgress{SearchID: f.search, PostID: f.post, Site: f.site, Key: retryKey(f), Read: f.read.Load(), Total: f.total.Load(),
			Pages: f.pages.Load(), Done: f.done.Load()})
	}
	d.live.mu.Unlock()
	slices.SortFunc(out, func(a, b FileProgress) int {
		if a.SearchID != b.SearchID {
			return int(a.SearchID - b.SearchID)
		}
		return int(b.PostID - a.PostID)
	})
	return out
}

type progressKey struct{}

// trackPost marks a post as in progress until done is called; fetchFile
// counts its bytes through the context.
func (d *Downloader) trackPost(ctx context.Context, search, post int64) (context.Context, func()) {
	f := &fileProgress{search: search, post: post}
	f.total.Store(-1)
	return context.WithValue(ctx, progressKey{}, f), func() {
		d.live.mu.Lock()
		delete(d.live.files, f)
		d.live.mu.Unlock()
	}
}

// countBody starts counting a file's bytes (if the post is tracked).
func (d *Downloader) countBody(ctx context.Context, body io.ReadCloser, size int64) io.ReadCloser {
	f, ok := ctx.Value(progressKey{}).(*fileProgress)
	if !ok {
		return body
	}
	if !f.gallery { // (a gallery's pictures add up)
		f.read.Store(0)
		if size > 0 {
			f.total.Store(size)
		}
	}
	d.live.mu.Lock()
	if d.live.files == nil {
		d.live.files = map[*fileProgress]struct{}{}
	}
	d.live.files[f] = struct{}{}
	if f.sum == nil {
		if d.live.read == nil {
			d.live.read = map[int64]*atomic.Int64{}
		}
		if d.live.read[f.search] == nil {
			d.live.read[f.search] = new(atomic.Int64)
		}
		f.sum = d.live.read[f.search]
	}
	d.live.mu.Unlock()
	return &countingReader{body, f}
}

type countingReader struct {
	io.ReadCloser
	f *fileProgress
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.f.read.Add(int64(n))
	c.f.sum.Add(int64(n))
	return n, err
}

// Speeds returns each search's download speed (bytes a second) over the last speedWindow. Every byte read counts,
// also a small file begun and done between two looks, which per-file progress misses. A search with nothing read in
// that time isn't listed. Called twice a second by each open page; the readings are shared.
func (d *Downloader) Speeds() map[int64]float64 {
	now := time.Now()
	d.live.mu.Lock()
	defer d.live.mu.Unlock()
	if d.live.readings == nil {
		d.live.readings = map[int64][]reading{}
	}
	out := map[int64]float64{}
	for id, sum := range d.live.read {
		r := d.live.readings[id]
		if len(r) == 0 || now.Sub(r[len(r)-1].at) >= time.Second/4 {
			r = append(r, reading{now, sum.Load()})
		}
		for len(r) > 1 && now.Sub(r[0].at) > speedWindow {
			r = r[1:]
		}
		d.live.readings[id] = r
		first, last := r[0], r[len(r)-1]
		if last.n == first.n {
			if now.Sub(first.at) >= speedWindow/2 && !d.live.busy(id) { // (quiet a while: forgotten)
				delete(d.live.read, id)
				delete(d.live.readings, id)
			}
			continue
		}
		out[id] = float64(last.n-first.n) / max(last.at.Sub(first.at).Seconds(), 1)
	}
	return out
}

// busy reports whether a file of the search is downloading (mu held).
func (l *liveState) busy(search int64) bool {
	for f := range l.files {
		if f.search == search {
			return true
		}
	}
	return false
}

func retryKey(f *fileProgress) string {
	if f.site == "" {
		return ""
	}
	return f.site + "/" + strconv.FormatInt(f.post, 10)
}
