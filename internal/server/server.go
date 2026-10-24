// Package server is Vitrine's HTTP side: the JSON API, file and thumbnail
// serving, and the web client.
package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/usevitrine/vitrine/internal/auth"
	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/favicon"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/logring"
	"github.com/usevitrine/vitrine/internal/push"
	"github.com/usevitrine/vitrine/internal/renditions"
)

//go:embed web
var webFiles embed.FS

const sessionCookie = "vitrine_session"

// Server handles HTTP requests.
type Server struct {
	// anonymous: the setting anonymousLogin - signed out, only a plain sign-in page (signin.go).
	anonymous atomic.Bool
	lib       *library.Library
	auth      *auth.Auth
	log       *slog.Logger
	results   *resultCache
	dl        *download.Downloader
	rend      *renditions.Cache
	push      *push.Service
	backups   *backups       // nil: no backup API
	icons     *favicon.Cache // nil: no site icons
	mediaKey  []byte         // signs file links
	info      Info           // version, where things are stored (Settings > Stats)
	logs      *logring.Ring  // recent log records (the problem report); nil: none
	notices   *notices       // the downloader's events for the bell (see notices.go)
	updates   *updates       // the update check and the Update button (updates.go); nil: off
	pictures  *pictureCache  // browsing's covers and thumbnail sheets, lately seen (browse.go)
	browsing  *browseState   // Comics > Browse: search pages lately, covers on their way
	mux       *http.ServeMux
}

// New returns the HTTP handler for a library.
func New(lib *library.Library, a *auth.Auth, log *slog.Logger) *Server {
	s := &Server{lib: lib, auth: a, log: log, results: newResultCache(32), notices: newNotices(lib, log), pictures: newPictureCache(192 << 20),
		browsing: newBrowseState(), mux: http.NewServeMux()}
	if key, err := a.MediaKey(context.Background()); err != nil {
		log.Error("signed file links disabled (files need the session cookie)", "err", err)
	} else {
		s.mediaKey = key
	}

	// Open to everyone.
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /licenses", s.licenses) // (open: Vitrine's license, its source, the third-party notices)
	s.mux.HandleFunc("GET /api/session", s.session)
	s.mux.HandleFunc("POST /api/setup", s.setup)
	s.mux.HandleFunc("POST /api/login", s.login)
	s.mux.HandleFunc("POST /api/logout", s.logout)
	web := webRoot()
	if set, err := lib.Settings(context.Background()); err == nil {
		s.anonymous.Store(string(set["anonymousLogin"]) == "true")
	}
	s.mux.Handle("GET /startup.png", s.unlessAnonymous(http.HandlerFunc((&startupImages{web: web, cache: map[startupKey][]byte{}}).serve)))
	s.mux.Handle("GET /", s.unlessAnonymous(staticHandler(web)))
	noAPI := func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, errors.New("no such API endpoint"))
	}
	s.mux.HandleFunc("GET /api/", noAPI)
	s.mux.HandleFunc("POST /api/", noAPI)

	// Logged in only.
	s.handle("GET /api/search", s.search)
	s.handle("GET /api/results/{id}", s.resultSlice)
	s.handle("GET /api/results/{id}/dates", s.resultDates)
	s.handle("GET /api/new", s.newFiles)
	s.handle("GET /api/search/sites", s.siteCounts)
	s.handle("GET /api/files", s.files)
	s.handle("GET /api/comics", s.listComics)
	s.handle("GET /api/browse/sites", s.browseSites)
	s.handle("GET /api/browse", s.browse)
	s.handle("GET /api/browse/gallery", s.browseThumbs)
	s.handle("GET /api/browse/tags", s.browseTags)
	s.handle("GET /api/browse/image", s.browseImage)
	s.handle("GET /api/comics/tags", s.comicTags)
	s.handle("GET /api/comics/info", s.comicInfo)
	s.handle("GET /api/comics/collections", s.collections)
	s.handle("POST /api/comics/collections", s.signal(s.createCollection))
	s.handle("POST /api/comics/collections/{id}", s.signal(s.renameCollection))
	s.handle("POST /api/comics/collections/{id}/remove", s.signal(s.deleteCollection))
	s.handle("POST /api/comics/collections/{id}/delete", s.signal(s.deleteCollectionComics))
	s.handle("POST /api/comics/collections/{id}/order", s.signal(s.orderCollection))
	s.handle("GET /api/comics/{id}", s.getComic)
	s.handle("POST /api/comics/delete", func(w http.ResponseWriter, r *http.Request) { s.deleteComics(w, r, false) })
	s.handle("POST /api/comics/undelete", func(w http.ResponseWriter, r *http.Request) { s.deleteComics(w, r, true) })
	s.handle("POST /api/comics/{id}", s.updateComic)
	s.handle("POST /api/comics/do/{action}", s.signal(s.comicsAction))
	s.handle("GET /api/files/{id}/tags", s.fileTags)
	s.handle("POST /api/tags", s.editTags)
	s.handle("POST /api/tags/of", s.tagsOf)
	s.handle("GET /api/tags/sites", s.tagSites)
	s.handle("GET /api/tags", s.listTags)
	s.handle("POST /api/tags/rename", s.renameTag)
	s.handle("POST /api/tags/namespace", s.setTagNamespace)
	s.handle("POST /api/delete", s.deleteFiles)
	s.handle("POST /api/favorite", s.favorite)
	s.handle("POST /api/climax", s.climax)
	s.handle("POST /api/climax/undo", s.undoClimax)
	s.handle("POST /api/climax/remove", s.removeClimaxes)
	s.handle("POST /api/climax/clear", s.clearClimaxes)
	s.handle("GET /api/rankings", s.rankings)
	s.handle("POST /api/undelete", s.undeleteFiles)
	s.handle("GET /api/sections", s.sections)
	s.handle("POST /api/sections/{section}/off", s.signal(s.sectionOff))
	s.handle("GET /api/trash", s.trashInfo)
	s.handle("POST /api/trash/purge", s.purgeTrash)
	s.handle("POST /api/trash/empty", s.emptyTrash)
	s.handle("POST /api/trash/purge-comics", s.purgeComicsTrash)
	s.handle("POST /api/trash/empty-comics", s.emptyComicsTrash)
	s.handle("GET /api/autocomplete", s.autocomplete)
	s.handle("GET /api/relations", s.relations)
	s.handle("POST /api/relations", s.editRelation)
	s.handle("GET /api/stats", s.stats)
	s.handle("GET /api/namespaces", s.namespaces)
	s.handle("GET /api/downloads", s.listDownloads)
	s.handle("GET /api/events", s.events)
	s.handle("GET /api/notices", s.listNotices)
	s.handle("POST /api/notices", s.addNotice)
	s.handle("POST /api/notices/read", s.markNotices(false))
	s.handle("POST /api/notices/clear", s.markNotices(true))
	s.handle("POST /api/downloads", s.signal(s.addDownload))
	s.handle("POST /api/downloads/clear", s.signal(s.clearDownloads))
	s.handle("POST /api/downloads/reorder", s.signal(s.reorderDownloads))
	s.handle("POST /api/downloads/{id}/{action}", s.signal(s.changeDownload))
	s.handle("GET /api/settings", s.getSettings)
	s.handle("POST /api/settings", s.setSettings)
	s.handle("GET /api/sites", s.listSites)
	s.handle("POST /api/sites/{id}/credentials", s.setCredentials)
	s.handle("POST /api/sites/{id}/test", s.testSite)
	s.handle("POST /api/sites/{id}/off", s.setSiteOff)
	s.handle("POST /api/sites/{id}/try", s.trySite)
	s.handle("GET /api/plugins", s.listPlugins)
	s.handle("POST /api/plugins/install", s.signal(s.installPlugin))
	s.handle("POST /api/plugins/upload", s.signal(s.uploadPlugin))
	s.handle("POST /api/plugins/{id}/remove", s.signal(s.removePlugin))
	s.handle("GET /api/failed-posts", s.failedPosts)
	s.handle("POST /api/failed-posts/retry", s.retryPost)
	s.handle("POST /api/failed-posts/dismiss", s.dismissPosts)
	s.handle("GET /api/saved", s.listSaved)
	s.handle("GET /api/news", s.news)
	s.handle("POST /api/seen", s.seen)
	s.handle("POST /api/saved", s.addSaved)
	s.handle("POST /api/saved/reorder", s.reorderSaved)
	s.handle("POST /api/saved/{id}", s.updateSaved)
	s.handle("POST /api/saved/{id}/remove", s.removeSaved)
	s.handle("POST /api/refresh", s.scheduleRefresh)
	s.handle("POST /api/files/{id}/refresh", s.refreshFile)
	s.handle("GET /api/files/{id}/sprite", s.spriteInfo)
	s.handle("POST /api/subscriptions", s.signal(s.addSubscription))
	s.handle("POST /api/subscriptions/{id}", s.signal(s.updateSubscription))
	s.handle("POST /api/subscriptions/{id}/{action}", s.signal(s.subscriptionAction))
	s.handle("GET /api/subscriptions/{id}/fav-gone", s.favGone)
	s.handle("GET /api/subscriptions/looks", s.subscriptionLooks)
	s.handle("POST /api/sub-groups", s.signal(s.addGroup))
	s.handle("POST /api/sub-groups/{id}", s.signal(s.updateGroup))
	s.handle("POST /api/sub-groups/{id}/{action}", s.signal(s.groupAction))
	s.handle("GET /api/push", s.pushInfo)
	s.handle("POST /api/push/subscribe", s.pushSubscribe)
	s.handle("POST /api/push/unsubscribe", s.pushUnsubscribe)
	s.handle("POST /api/push/test", s.pushTest)
	s.handle("GET /api/blacklist", s.getBlacklist)
	s.handle("POST /api/blacklist", s.setBlacklist)
	s.handle("GET /api/blacklist/matches", s.blacklistMatches)
	s.handle("POST /api/blacklist/delete", s.deleteBlacklisted)
	s.handle("GET /api/dupes", s.listDupes)
	s.handle("GET /api/flaresolverr", s.getFlareSolverr)
	s.handle("POST /api/flaresolverr", s.setFlareSolverr)
	s.handle("POST /api/dupes/decide", s.decideDupe)
	s.handle("POST /api/dupes/undo", s.undoDupe)
	s.handle("GET /api/favicon", s.favicon)
	s.handle("GET /api/report", s.report)
	s.handle("GET /api/logs", s.listLogs)
	s.handle("POST /api/log", s.clientLog)
	// Files and thumbnails check the login themselves: a session cookie or a
	// signed link (see media.go).
	s.mux.HandleFunc("GET /files/{name}", s.serveFile)
	s.mux.HandleFunc("GET /thumbs/{name}", s.serveThumb)
	s.mux.HandleFunc("GET /sprites/{name}", s.serveSprite)
	return s
}

// handle registers a handler that needs a logged-in user.
func (s *Server) handle(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.requireLogin(h))
}

type ctxKey int

const (
	userKey  ctxKey = 0
	localKey ctxKey = 1 // (a request through the local socket)
)

// Local serves requests that came through the server's local socket (the vitrine command): they act as the account,
// without a session - only this machine reaches the socket, and only users who can open its file.
func (s *Server) Local() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), localKey, true)))
	})
}

func (s *Server) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if local, _ := r.Context().Value(localKey).(bool); local {
			u, err := s.auth.Owner(r.Context())
			if err != nil {
				if errors.Is(err, auth.ErrNoSession) {
					err = errors.New("no account yet: create one in the web app or with vitrine passwd")
				}
				writeError(w, http.StatusUnauthorized, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, auth.ErrNoSession)
			return
		}
		u, err := s.auth.Session(r.Context(), c.Value)
		if err != nil {
			// (a request called off meanwhile - the page went on - is no server error)
			if !errors.Is(err, auth.ErrNoSession) && r.Context().Err() == nil {
				s.log.Error("session check", "err", err)
			}
			writeError(w, http.StatusUnauthorized, auth.ErrNoSession)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

// ServeHTTP adds security headers and cross-site request protection, then
// routes the request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	// Reached over HTTPS: browsers keep to it for this address (never plain HTTP, not even a first request).
	if isHTTPS(r) {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		// Changes must come from our own pages: JSON bodies (a plain HTML
		// form can't send one cross-site) and, if the browser says where the
		// request came from, the same host.
		// The one upload of a raw file (a backup to restore) carries a header
		// of ours instead: a cross-site page can't add one without asking (CORS).
		rawUpload := r.URL.Path == "/api/backups/upload" && r.Header.Get("X-Vitrine-Upload") == "1"
		if !rawUpload && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, errors.New("requests must send JSON"))
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				writeError(w, http.StatusForbidden, errors.New("cross-site request refused"))
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	}

	start := time.Now()
	if mayGzip(r) {
		g := &gzipWriter{ResponseWriter: w}
		defer g.close()
		w = g
	}
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	s.mux.ServeHTTP(sw, r)
	if logRequests {
		s.log.Debug("request", "method", r.Method, "path", r.URL.Path, "range", r.Header.Get("Range"),
			"status", sw.status, "took", time.Since(start).Round(time.Microsecond))
	} else if sw.status >= 500 || (time.Since(start) > time.Second && r.URL.Path != "/api/events") {
		args := []any{"method", r.Method, "path", r.URL.Path, "status", sw.status, "took", time.Since(start).Round(time.Millisecond)}
		if sw.err != nil {
			args = append(args, "err", sw.err)
		}
		s.log.Warn("request", args...)
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	err    error // a server error's cause (writeError), for the request's log line
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController and io.Copy's sendfile path reach the
// real writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	if sw, ok := w.(*statusWriter); ok && status >= 500 {
		sw.err = err // (logged with the request: a "500" without its cause told nothing)
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("bad request body: " + err.Error())
	}
	return nil
}

// webRoot is the built web client (web/app, made by `npm run build` in
// web/), or a page saying how to build it.
func webRoot() fs.FS {
	if app, err := fs.Sub(webFiles, "web/app"); err == nil {
		if _, err := fs.Stat(app, "index.html"); err == nil {
			return app
		}
	}
	fallback, _ := fs.Sub(webFiles, "web/fallback")
	return fallback
}

// staticHandler serves the embedded web client. Unknown paths get
// index.html (the client routes itself). Built assets have content hashes
// in their names, so they are cached for good; index.html is revalidated.
func staticHandler(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'self'; "+
				"style-src-attr 'unsafe-inline'; script-src 'self'; connect-src 'self'; "+
				"frame-ancestors 'none'; base-uri 'none'; object-src 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(web, name); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		} else if strings.HasPrefix(name, "assets/") && mayGzip(r) {
			// The app's code and styles never change under a name: compressed once (best compression), kept.
			if gz, ok := gzipped.load(web, name); ok {
				h := w.Header()
				h.Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
				h.Set("Content-Encoding", "gzip")
				h.Add("Vary", "Accept-Encoding")
				h.Set("Content-Length", strconv.Itoa(len(gz)))
				w.Write(gz)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}

// gzipped keeps the web app's compressed assets (a few hundred KB): compressing the 680 KB script on every first
// load cost time and CPU each time.
var gzipped assetCache

type assetCache struct{ m sync.Map } // name -> []byte (nil: not worth compressing)

func (c *assetCache) load(web fs.FS, name string) ([]byte, bool) {
	if v, ok := c.m.Load(name); ok {
		b := v.([]byte)
		return b, b != nil
	}
	var out []byte
	if compressible(mime.TypeByExtension(path.Ext(name))) {
		if raw, err := fs.ReadFile(web, name); err == nil {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(raw)
			zw.Close()
			out = buf.Bytes()
		}
	}
	c.m.Store(name, out)
	return out, out != nil
}

// logRequests: every request in the log (VITRINE_LOG_LEVEL=debug) - otherwise
// only failed and slow ones (it would be every thumbnail).
var logRequests = strings.EqualFold(os.Getenv("VITRINE_LOG_LEVEL"), "debug")
