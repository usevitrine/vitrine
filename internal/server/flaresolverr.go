package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// FlareSolverr (passes Cloudflare challenges for the downloader): its address
// is kept with the settings and takes effect at once.

// GET /api/flaresolverr -> {url}
func (s *Server) getFlareSolverr(w http.ResponseWriter, r *http.Request) {
	url := ""
	if s.dl != nil {
		url = s.dl.Solver.Endpoint()
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// POST /api/flaresolverr {url} -> {url, version} or {url, error}: saved even
// when it doesn't answer (it may be starting), with what went wrong.
func (s *Server) setFlareSolverr(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("the downloader isn't running"))
		return
	}
	url := strings.TrimRight(strings.TrimSpace(req.URL), "/")
	if err := plugin.ValidEndpoint(url); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	v, _ := json.Marshal(url)
	if err := s.lib.SetSettings(r.Context(), map[string]json.RawMessage{"flaresolverrURL": v}); err != nil {
		libError(w, err)
		return
	}
	s.dl.Solver.SetEndpoint(url)
	out := map[string]string{"url": url}
	if url != "" {
		if version, err := s.dl.Solver.Ping(r.Context(), url); err != nil {
			out["error"] = err.Error()
		} else {
			out["version"] = version
		}
	}
	writeJSON(w, http.StatusOK, out)
}
