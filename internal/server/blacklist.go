package server

import (
	"net/http"
	"strings"
)

// The download blacklist (see library.Blacklist).

func (s *Server) getBlacklist(w http.ResponseWriter, r *http.Request) {
	b, err := s.lib.Blacklist(r.Context())
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": b.Patterns()})
}

func (s *Server) setBlacklist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tags []string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	tags, err := s.lib.SetBlacklist(r.Context(), req.Tags)
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

// patternsParam reads ?tags=a,b (nil = the whole blacklist).
func patternsParam(r *http.Request) []string {
	v := r.URL.Query().Get("tags")
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

// blacklistMatches counts the library's files with blacklisted tags.
func (s *Server) blacklistMatches(w http.ResponseWriter, r *http.Request) {
	m, err := s.lib.BlacklistedFiles(r.Context(), patternsParam(r))
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// deleteBlacklisted deletes them (undoable with /api/undelete and the IDs).
func (s *Server) deleteBlacklisted(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tags []string `json:"tags"` // empty = the whole blacklist
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Tags) == 0 {
		req.Tags = nil
	}
	ids, err := s.lib.DeleteBlacklisted(r.Context(), req.Tags)
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": len(ids), "ids": orEmpty(ids)})
}
