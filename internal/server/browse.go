package server

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/plugin"
)

// Browsing a gallery site (Comics > Browse, 2026-10-22): its search page by page, a gallery's page thumbnails,
// and their pictures through here (ExHentai's need the login; the browser never talks to the site itself).

// browseSite is a site that can be browsed: a gallery engine that shows page thumbnails, switched on.
func (s *Server) browseSite(w http.ResponseWriter, id string) (*download.Site, plugin.BrowseEngine, bool) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return nil, nil, false
	}
	st := s.dl.Site(id)
	if st == nil || s.dl.IsOff(id) {
		writeError(w, http.StatusNotFound, fmt.Errorf("no such site to browse: %q", id))
		return nil, nil, false
	}
	be, ok := st.Engine.(plugin.BrowseEngine)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%s can't be browsed", st.Client.Manifest().Site))
		return nil, nil, false
	}
	return st, be, true
}

// browseTags: GET /api/browse/tags?site=&q= -> {suggestions: [{tag, count}]} (the site's tags starting with the text,
// each in the namespace a download gives it; no counts - the site doesn't say).
func (s *Server) browseTags(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	st, be, ok := s.browseSite(w, q.Get("site"))
	if !ok {
		return
	}
	list, err := be.TagSuggest(plugin.Foreground(r.Context()), q.Get("q"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	type suggestion struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := make([]suggestion, 0, min(len(list), 12))
	for _, x := range list {
		if len(out) == 12 {
			break
		}
		ns := x.NS
		if v, ok := st.Client.Manifest().Tags.Namespaces[ns]; ok {
			ns = v
		}
		tag := x.Name
		if ns != "" {
			tag = ns + ":" + x.Name
		}
		out = append(out, suggestion{Tag: tag})
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out})
}

// browseSites: GET /api/browse/sites -> {sites: [id]} (the gallery sites that can be browsed).
func (s *Server) browseSites(w http.ResponseWriter, r *http.Request) {
	out, sorts := []string{}, map[string][]string{}
	if s.dl != nil {
		for _, id := range s.dl.Sites() {
			if st := s.dl.Site(id); st != nil && !s.dl.IsOff(id) {
				if be, ok := st.Engine.(plugin.BrowseEngine); ok {
					out = append(out, id)
					sorts[id] = be.Sorts()
					// Browse opened: logged in ahead, while the page sets itself up.
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
						defer cancel()
						be.Warm(ctx)
					}()
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": out, "sorts": sorts})
}

type browseTag struct {
	NS   string `json:"ns,omitempty"`
	Name string `json:"name"`
}

type browseGallery struct {
	ID       int64       `json:"id"`
	Token    string      `json:"token"`
	URL      string      `json:"url"`
	Title    string      `json:"title"`
	TitleAlt string      `json:"title_alt,omitempty"`
	Category string      `json:"category,omitempty"`
	Pages    int         `json:"pages"`
	Posted   int64       `json:"posted,omitempty"` // Unix ms
	Cover    string      `json:"cover,omitempty"`
	Rating   float64     `json:"rating,omitempty"`
	Uploader string      `json:"uploader,omitempty"`
	Tags     []browseTag `json:"tags"`
	Comic    int64       `json:"comic,omitempty"`  // in the library already: the comic's ID
	Queued   bool        `json:"queued,omitempty"` // a download of it is waiting or running
}

var galleryLink = regexp.MustCompile(`/g/(\d+)/`)

// browse: GET /api/browse?site=&q=&before=&sort= -> {galleries, total, next} (next: pass as before for more; 0: no
// more). With sort (one of the site's other orders - its own list, no search), before is the list's page.
func (s *Server) browse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	st, be, ok := s.browseSite(w, q.Get("site"))
	if !ok {
		return
	}
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	sort := q.Get("sort")
	// (the same page lately: at once - back, the same search again)
	key := strings.Join([]string{q.Get("site"), sort, q.Get("q"), strconv.FormatInt(before, 10)}, "|")
	page, more, ok := s.browsing.cached(key)
	if !ok {
		var err error
		if sort != "" {
			page, more, err = be.Ranked(plugin.Foreground(r.Context()), sort, int(before))
		} else {
			page, err = st.Engine.Search(plugin.Foreground(r.Context()), q.Get("q"), before)
		}
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		s.browsing.keep(key, page, more)
	}
	// Its covers on their way at once (the browser asks for them a moment later and joins); a new search calls off
	// the last one's still coming, the next page adds to them.
	pctx := s.browsing.prefetch(q.Get("site"), before == 0)
	s.prefetchCovers(pctx, st, page)
	// Downloads of galleries waiting or running (by the gallery ID in their search: a gallery's link).
	queued := map[int64]bool{}
	if list, err := s.dl.List(r.Context()); err == nil {
		for _, x := range list {
			if x.Site == st.Client.Manifest().ID && (x.State == download.StateQueued || x.State == download.StateRunning || x.State == download.StatePaused) {
				if m := galleryLink.FindStringSubmatch(x.Query); m != nil {
					n, _ := strconv.ParseInt(m[1], 10, 64)
					queued[n] = true
				}
			}
		}
	}
	out := make([]browseGallery, 0, len(page.Posts))
	var next int64
	for _, p := range page.Posts {
		g := p.Gallery
		if g == nil {
			continue
		}
		bg := browseGallery{ID: p.ID, Token: g.Token, URL: p.PostURL, Title: g.Title, TitleAlt: g.TitleAlt, Category: g.Category,
			Pages: g.Pages, Cover: g.Cover, Rating: g.Rating, Uploader: g.Uploader, Tags: []browseTag{}, Queued: queued[p.ID]}
		if !p.Posted.IsZero() {
			bg.Posted = p.Posted.UnixMilli()
		}
		// Each tag in the namespace a download gives it (galleryInfo: the plugin's mapping, else the site's own -
		// female, language...).
		// (each with its own namespace: the same name can be in two - female:glasses, male:glasses)
		tags := g.SiteTags
		if tags == nil {
			for _, t := range p.Tags {
				tags = append(tags, plugin.SiteTag{NS: p.TagTypes[t], Name: t})
			}
		}
		seen := map[browseTag]bool{}
		for _, t := range tags {
			ns := t.NS
			if v, ok := st.Client.Manifest().Tags.Namespaces[ns]; ok {
				ns = v
			}
			if bt := (browseTag{NS: ns, Name: t.Name}); !seen[bt] {
				seen[bt] = true
				bg.Tags = append(bg.Tags, bt)
			}
		}
		if id, _, err := s.lib.ComicFrom(r.Context(), st.Client.Manifest().ID, g.Key); err == nil {
			bg.Comic = id
		}
		out = append(out, bg)
		next = p.ID
	}
	if len(out) == 0 || len(out) == page.Total {
		next = 0
	}
	if sort != "" {
		next = 0
		if more {
			next = before + 1
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"galleries": out, "total": page.Total, "next": next})

	// The next page on its way too, in the background (after what's on screen and the downloads' turns, called off
	// by a newer search): scrolling on finds it - and its covers - ready.
	if next > 0 {
		nkey := strings.Join([]string{q.Get("site"), sort, q.Get("q"), strconv.FormatInt(next, 10)}, "|")
		if _, _, ok := s.browsing.cached(nkey); !ok {
			go func() {
				var np plugin.Page
				var nmore bool
				var err error
				if sort != "" {
					np, nmore, err = be.Ranked(pctx, sort, int(next))
				} else {
					np, err = st.Engine.Search(pctx, q.Get("q"), next)
				}
				if err != nil || pctx.Err() != nil {
					return
				}
				s.browsing.keep(nkey, np, nmore)
				s.prefetchCovers(pctx, st, np)
			}()
		}
	}
}

// prefetchCovers gets a page's covers on their way (the browser asks for them a moment later and joins).
func (s *Server) prefetchCovers(ctx context.Context, st *download.Site, page plugin.Page) {
	for _, p := range page.Posts {
		if p.Gallery == nil || p.Gallery.Cover == "" {
			continue
		}
		u := p.Gallery.Cover
		go s.pictures.load(ctx, ctx, u, func(ctx context.Context) (picture, error) {
			b, ct, err := st.Client.Picture(ctx, u)
			return picture{b, ct}, err
		})
	}
}

// prefetchSheets gets thumbnail sheets on their way (each once: a sheet holds ~20 thumbnails).
func (s *Server) prefetchSheets(st *download.Site, thumbs []plugin.GalleryThumb) {
	seen := map[string]bool{}
	for _, th := range thumbs {
		if seen[th.URL] {
			continue
		}
		seen[th.URL] = true
		u := th.URL
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			s.pictures.load(ctx, ctx, u, func(ctx context.Context) (picture, error) {
				b, ct, err := st.Client.Picture(ctx, u)
				return picture{b, ct}, err
			})
		}()
	}
}

// browseGallery: GET /api/browse/gallery?site=&id=&token=&p= -> {thumbs: [{page, url, x, y, w, h}], pages}
// (one page of the gallery's thumbnails; pages: how many such pages).
func (s *Server) browseThumbs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	st, be, ok := s.browseSite(w, q.Get("site"))
	if !ok {
		return
	}
	gid, _ := strconv.ParseInt(q.Get("id"), 10, 64)
	token := q.Get("token")
	p, _ := strconv.Atoi(q.Get("p"))
	if gid <= 0 || !regexp.MustCompile(`^[0-9a-f]{10}$`).MatchString(token) || p < 0 {
		writeError(w, http.StatusBadRequest, errors.New("a gallery's id and token are needed"))
		return
	}
	key := fmt.Sprintf("thumbs|%s|%d|%d", q.Get("site"), gid, p)
	thumbs, pages, ok := s.browsing.thumbs(key)
	if !ok {
		var err error
		thumbs, pages, err = be.GalleryThumbs(plugin.Foreground(r.Context()), gid, token, p)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		s.browsing.keepThumbs(key, thumbs, pages)
	}
	// Their sheets on their way at once (the browser asks a moment later and joins), and the next page of them ahead:
	// the strip's scrolling finds it ready.
	s.prefetchSheets(st, thumbs)
	writeJSON(w, http.StatusOK, map[string]any{"thumbs": thumbs, "pages": pages})
	if p+1 < pages {
		nkey := fmt.Sprintf("thumbs|%s|%d|%d", q.Get("site"), gid, p+1)
		if _, _, ok := s.browsing.thumbs(nkey); !ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				if nt, np, err := be.GalleryThumbs(ctx, gid, token, p+1); err == nil {
					s.browsing.keepThumbs(nkey, nt, np)
					s.prefetchSheets(st, nt)
				}
			}()
		}
	}
}

// browseImage: GET /api/browse/image?site=&u= -> a cover or thumbnail sheet from the site (only from its own
// addresses, with its login), kept: in memory here (the last 64 MB of them) and by the browser a month (a picture's
// address never changes on these sites).
func (s *Server) browseImage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	st, _, ok := s.browseSite(w, q.Get("site"))
	if !ok {
		return
	}
	u := q.Get("u")
	if !strings.HasPrefix(u, "https://") {
		writeError(w, http.StatusBadRequest, errors.New("a picture's https address is needed"))
		return
	}
	pic, err := s.pictures.load(r.Context(), context.Background(), u, func(ctx context.Context) (picture, error) {
		b, ct, err := st.Client.Picture(ctx, u)
		return picture{b, ct}, err
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", pic.ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(pic.b)))
	w.Header().Set("Cache-Control", "private, max-age=2592000, immutable")
	w.Write(pic.b)
}

type picture struct {
	b  []byte
	ct string
}

// pictureCache keeps the pictures browsed lately (the least recently used go past max bytes), and fetches each
// once however many ask for it at the same time (a search's prefetch and the browser's request).
type pictureCache struct {
	mu       sync.Mutex
	max      int
	size     int
	order    *list.List // front: the latest
	byURL    map[string]*list.Element
	inflight map[string]*pictureCall
}

type pictureCall struct {
	done chan struct{}
	p    picture
	err  error
	ctx  context.Context // the fetch's (a prefetch's ends when a newer search starts)
}

// load returns a picture: from the cache, joining a fetch of it on its way, or fetching it (with fetchCtx: a
// request's own fetch outlives it - another may be waiting for it). ctx: how long this caller waits.
func (c *pictureCache) load(ctx, fetchCtx context.Context, u string, fetch func(context.Context) (picture, error)) (picture, error) {
	for {
		if p, ok := c.get(u); ok {
			return p, nil
		}
		c.mu.Lock()
		call, ok := c.inflight[u]
		if !ok {
			call = &pictureCall{done: make(chan struct{}), ctx: fetchCtx}
			c.inflight[u] = call
			go func() {
				fctx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
				defer cancel()
				call.p, call.err = fetch(fctx)
				if call.err == nil {
					c.put(u, call.p)
				}
				c.mu.Lock()
				delete(c.inflight, u)
				c.mu.Unlock()
				close(call.done)
			}()
		}
		c.mu.Unlock()
		select {
		case <-call.done:
		case <-ctx.Done():
			return picture{}, ctx.Err()
		}
		if call.err != nil && call.ctx.Err() != nil && ctx.Err() == nil {
			fetchCtx = context.Background() // (a prefetch called off by a newer search: this one still wants it)
			continue
		}
		return call.p, call.err
	}
}

type cached struct {
	u string
	p picture
}

func newPictureCache(max int) *pictureCache {
	return &pictureCache{max: max, order: list.New(), byURL: map[string]*list.Element{}, inflight: map[string]*pictureCall{}}
}

func (c *pictureCache) get(u string) (picture, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byURL[u]
	if !ok {
		return picture{}, false
	}
	c.order.MoveToFront(e)
	return e.Value.(cached).p, true
}

func (c *pictureCache) put(u string, p picture) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.byURL[u]; ok || len(p.b) > c.max/4 {
		return
	}
	c.byURL[u] = c.order.PushFront(cached{u, p})
	c.size += len(p.b)
	for c.size > c.max {
		last := c.order.Back()
		c.order.Remove(last)
		v := last.Value.(cached)
		delete(c.byURL, v.u)
		c.size -= len(v.p.b)
	}
}

// browseState: the search pages loaded lately (for a few minutes), and per site the covers of its latest search on
// their way (a newer search calls them off).
type browseState struct {
	mu    sync.Mutex
	pages map[string]browsePage
	// (a gallery's thumbnail pages, by site, gallery and page)
	thumbPages map[string]browseThumbs
	gen        map[string]context.Context
	stop       map[string]context.CancelFunc
}

type browseThumbs struct {
	thumbs []plugin.GalleryThumb
	pages  int
	at     time.Time
}

// thumbs: a gallery's thumbnail page read lately (the next one fetched ahead, the same gallery opened again).
func (b *browseState) thumbs(key string) ([]plugin.GalleryThumb, int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.thumbPages[key]
	if !ok || time.Since(t.at) > browsePageAge {
		return nil, 0, false
	}
	return t.thumbs, t.pages, true
}

func (b *browseState) keepThumbs(key string, thumbs []plugin.GalleryThumb, pages int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.thumbPages) >= 200 {
		for k, t := range b.thumbPages {
			if len(b.thumbPages) < 100 || time.Since(t.at) > browsePageAge {
				delete(b.thumbPages, k)
			}
		}
	}
	b.thumbPages[key] = browseThumbs{thumbs, pages, time.Now()}
}

type browsePage struct {
	page plugin.Page
	more bool
	at   time.Time
}

const browsePageAge = 3 * time.Minute

func newBrowseState() *browseState {
	return &browseState{pages: map[string]browsePage{}, thumbPages: map[string]browseThumbs{}, gen: map[string]context.Context{}, stop: map[string]context.CancelFunc{}}
}

func (b *browseState) cached(key string) (plugin.Page, bool, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.pages[key]
	if !ok || time.Since(p.at) > browsePageAge {
		return plugin.Page{}, false, false
	}
	return p.page, p.more, true
}

func (b *browseState) keep(key string, page plugin.Page, more bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pages) >= 100 {
		for k, p := range b.pages {
			if time.Since(p.at) > browsePageAge {
				delete(b.pages, k)
			}
		}
		for k := range b.pages { // (still full: any one)
			if len(b.pages) < 100 {
				break
			}
			delete(b.pages, k)
		}
	}
	b.pages[key] = browsePage{page, more, time.Now()}
}

// prefetch is the context a site's cover prefetch runs in: a new one for a new search (the last one's called off),
// the current one for its next page.
func (b *browseState) prefetch(site string, fresh bool) context.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx, ok := b.gen[site]; ok && !fresh && ctx.Err() == nil {
		return ctx
	}
	if stop := b.stop[site]; stop != nil {
		stop()
	}
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Minute)
	b.gen[site], b.stop[site] = ctx, stop
	return ctx
}
