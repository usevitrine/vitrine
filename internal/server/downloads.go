package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/plugin"
)

// SetDownloader enables the download API.
func (s *Server) SetDownloader(dl *download.Downloader) { s.dl = dl }

var errNoDownloader = errors.New("the downloader isn't running")

func (s *Server) listDownloads(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	list, err := s.dl.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// ?active=1: only what's going on now (running, waiting, paused) and the paused sites - the top bar's downloads
	// menu reads it on every change; the whole page (history, subscriptions, groups) was a lot to send and parse that
	// often while downloads ran, and scrolling the library stalled (the user saw the background at the bottom).
	if r.URL.Query().Get("active") == "1" {
		active := []download.Search{}
		for _, x := range list {
			if x.State == download.StateRunning || x.State == download.StateQueued || x.State == download.StatePaused {
				active = append(active, x)
			}
		}
		paused := map[string]plugin.Pause{}
		for _, id := range s.dl.Sites() {
			if st := s.dl.Site(id); st != nil {
				if p := st.Client.Paused(); !p.Until.IsZero() {
					paused[id] = p
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"searches": active, "sites_paused": paused})
		return
	}
	groups, err := s.dl.Groups(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// ?sites=1: only the sites to download from and the groups - the New download dialog's (the whole page's numbers
	// took 1-3 s on a big library, and the dialog's site page waited for them).
	if r.URL.Query().Get("sites") == "1" {
		on := []string{}
		for _, id := range s.dl.Sites() {
			if !s.dl.IsOff(id) {
				on = append(on, id)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"sites": on, "groups": groups})
		return
	}
	subs, err := s.dl.Subscriptions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	refresh, err := s.dl.RefreshStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Sites to download from (switched-off ones apart: their queues and subscriptions wait).
	// Paused ones (they kept failing to answer) wait until they answer again.
	on, off := []string{}, []string{}
	paused := map[string]plugin.Pause{}
	for _, id := range s.dl.Sites() {
		if s.dl.IsOff(id) {
			off = append(off, id)
		} else {
			on = append(on, id)
		}
		if st := s.dl.Site(id); st != nil {
			if p := st.Client.Paused(); !p.Until.IsZero() {
				paused[id] = p
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": on, "sites_off": off, "sites_paused": paused, "searches": orEmpty(list), "subscriptions": orEmpty(subs),
		"groups": groups, "refresh": orEmpty(refresh), "retries": s.dl.Retries()})
}

func (s *Server) addDownload(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		Site     string `json:"site"`
		Query    string `json:"query"`
		MaxPosts int    `json:"max_posts"`
		Favorite bool   `json:"favorite"` // its files become favorites too
		Label    string `json:"label"`    // its name where the query says little (a gallery's title)
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, errors.New("the search is empty"))
		return
	}
	id, err := s.dl.AddTo(r.Context(), req.Site, req.Query, req.MaxPosts, req.Favorite)
	if errors.Is(err, download.ErrUnknownSite) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err == nil && req.Label != "" {
		err = s.dl.LabelSearch(r.Context(), id, req.Label)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	sr, err := s.dl.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sr)
}

func downloadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, download.ErrNoSearch), errors.Is(err, download.ErrNoSubscription), errors.Is(err, download.ErrNoGroup):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, download.ErrExists), errors.Is(err, download.ErrGroupExists):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, download.ErrState), errors.Is(err, download.ErrUnknownSite), errors.Is(err, download.ErrInterval),
		errors.Is(err, download.ErrEmptyQuery), errors.Is(err, download.ErrGroupName), errors.Is(err, download.ErrGroupSite):
		writeError(w, http.StatusBadRequest, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// changeDownload pauses, resumes or removes one search:
// POST /api/downloads/{id}/{pause|resume|remove}. Removing keeps the files.
func (s *Server) changeDownload(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad search ID"))
		return
	}
	switch r.PathValue("action") {
	case "pause":
		err = s.dl.Pause(r.Context(), id)
	case "resume":
		err = s.dl.Resume(r.Context(), id)
	case "remove":
		err = s.dl.Remove(r.Context(), id)
	default:
		writeError(w, http.StatusNotFound, errors.New("unknown action"))
		return
	}
	if err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// clearDownloads removes every finished search (files stay).
func (s *Server) clearDownloads(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	n, err := s.dl.ClearFinished(r.Context())
	if err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}

// reorderDownloads sets a site's queue order: {site, ids}.
func (s *Server) reorderDownloads(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		Site string  `json:"site"`
		IDs  []int64 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.dl.Reorder(r.Context(), req.Site, req.IDs); err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// failedPosts: GET /api/failed-posts -> {posts: [{site, post, url, query, error, at}]}.
func (s *Server) failedPosts(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	list, err := s.dl.FailedPosts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": orEmpty(list)})
}

// dismissPosts: POST /api/failed-posts/dismiss {posts: [{site, post}]} -> {dismissed}: off the list (a later search
// that meets one tries it again).
func (s *Server) dismissPosts(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		Posts []download.PostRef `json:"posts"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.dl.DismissFailed(r.Context(), req.Posts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"dismissed": n})
}

// retryPost: POST /api/failed-posts/retry {site, post} -> {outcome, error?}:
// one failed post tried again now.
func (s *Server) retryPost(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		Site string `json:"site"`
		Post int64  `json:"post,string"` // as text (see download.FailedPost)
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// In the background (the page shows it in its site's queue, with its progress, and its result): at once.
	if r.URL.Query().Get("wait") != "1" {
		if err := s.dl.StartRetry(r.Context(), req.Site, req.Post); err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, download.ErrNotFailed) || errors.Is(err, download.ErrUnknownSite) {
				code = http.StatusNotFound
			}
			writeError(w, code, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
		return
	}
	// wait=1: until it's done - on to the end if the request goes (its result is recorded either way).
	outcome, err := s.dl.RetryPost(context.WithoutCancel(r.Context()), req.Site, req.Post)
	switch {
	case errors.Is(err, download.ErrNotFailed), errors.Is(err, download.ErrUnknownSite):
		writeError(w, http.StatusNotFound, err)
	case outcome == "":
		writeError(w, http.StatusInternalServerError, err)
	case err != nil:
		writeJSON(w, http.StatusOK, map[string]string{"outcome": outcome, "error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"outcome": outcome})
	}
}
