package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/usevitrine/vitrine/internal/download"
)

// scheduleRefresh makes files' posts due for a tag refresh now: {ids}. They
// are read again when their site is next idle.
func (s *Server) scheduleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		IDs []uint32 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.dl.ScheduleRefresh(r.Context(), req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"posts": n})
}

// refreshFile reads one file's posts from their sites now; returns how many
// the sites still have, and how many were queued instead (their site is paused).
func (s *Server) refreshFile(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad file ID"))
		return
	}
	n, queued, err := s.dl.RefreshFile(r.Context(), uint32(id))
	switch {
	case errors.Is(err, download.ErrNoPosts):
		writeError(w, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err) // the site failed
	default:
		writeJSON(w, http.StatusOK, map[string]int{"found": n, "queued": queued})
	}
}
