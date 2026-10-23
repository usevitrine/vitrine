package server

import (
	"errors"
	"net/http"

	"github.com/usevitrine/vitrine/internal/download"
)

// Site plugins and their credentials (Settings > Sites). Credentials go in,
// never out: responses only say which fields are set.

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": orEmpty(s.dl.SiteInfos())})
}

// setCredentials: POST /api/sites/{id}/credentials {values: {field: value}};
// an empty value removes that field. -> {sites}.
func (s *Server) setCredentials(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		Values map[string]string `json:"values"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad request body")) // never echo what was sent
		return
	}
	err := s.dl.SetCredentials(r.PathValue("id"), req.Values)
	switch {
	case errors.Is(err, download.ErrUnknownSite):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, download.ErrBadField):
		writeError(w, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"sites": orEmpty(s.dl.SiteInfos())})
	}
}

// setSiteOff: POST /api/sites/{id}/off {off: bool} -> {sites}. Off: nothing
// runs for the site (its queue and subscriptions wait); on: it carries on.
// trySite: POST /api/sites/{id}/try - a paused site (it kept failing to answer) tries again now -> {sites}.
func (s *Server) trySite(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	err := s.dl.TryNow(r.PathValue("id"))
	switch {
	case errors.Is(err, download.ErrUnknownSite):
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"sites": orEmpty(s.dl.SiteInfos())})
	}
}

func (s *Server) setSiteOff(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		Off bool `json:"off"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	err := s.dl.SetOff(r.Context(), r.PathValue("id"), req.Off)
	switch {
	case errors.Is(err, download.ErrUnknownSite):
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"sites": orEmpty(s.dl.SiteInfos())})
	}
}

// testSite: POST /api/sites/{id}/test -> {ok, posts, message, detail}.
func (s *Server) testSite(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	res, err := s.dl.TestSite(r.Context(), r.PathValue("id"))
	if errors.Is(err, download.ErrUnknownSite) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
