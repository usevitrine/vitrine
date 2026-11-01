package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/usevitrine/vitrine/internal/library"
)

// Settings shared by every device: GET /api/settings -> {key: value};
// POST /api/settings {key: value, ...} stores those keys and returns all.

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.lib.Settings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

func (s *Server) setSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]json.RawMessage
	if !decode(w, r, &req) {
		return
	}
	if err := s.lib.SetSettings(r.Context(), req); err != nil {
		if errors.Is(err, library.ErrInvalid) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.applySettings(req)
	s.getSettings(w, r)
}

// applySettings hands the settings the running program keeps in memory to it: the stored ones at the start (New,
// SetDownloader) and each change (POST /api/settings, /api/flaresolverr) - one place, so a setting can't apply on
// change but not after a restart, or the other way round. Only the keys in set; the other settings are read where
// they're used.
func (s *Server) applySettings(set map[string]json.RawMessage) {
	if v, ok := set["anonymousLogin"]; ok {
		s.anonymous.Store(string(v) == "true")
	}
	if s.dl == nil {
		return
	}
	// (when a site that keeps failing to answer pauses)
	if v, ok := set["pauseAfter"]; ok {
		var n int
		if json.Unmarshal(v, &n) == nil {
			s.dl.SetPauseAfter(n)
		}
	}
	if v, ok := set["checksFirst"]; ok {
		var on bool
		if json.Unmarshal(v, &on) == nil {
			s.dl.SetChecksFirst(on)
		}
	}
	if v, ok := set["flaresolverrURL"]; ok {
		var u string
		if json.Unmarshal(v, &u) == nil {
			s.dl.Solver.SetEndpoint(u)
		}
	}
}
