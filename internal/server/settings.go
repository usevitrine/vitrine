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
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
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
	// (when a site that keeps failing to answer pauses: applied at once)
	if v, ok := req["pauseAfter"]; ok && s.dl != nil {
		var n int
		if json.Unmarshal(v, &n) == nil {
			s.dl.SetPauseAfter(n)
		}
	}
	if v, ok := req["anonymousLogin"]; ok {
		s.anonymous.Store(string(v) == "true")
	}
	if v, ok := req["checksFirst"]; ok && s.dl != nil {
		var on bool
		if json.Unmarshal(v, &on) == nil {
			s.dl.SetChecksFirst(on)
		}
	}
	s.getSettings(w, r)
}
