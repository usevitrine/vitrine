package server

import (
	"encoding/json"
	"net/http"
)

// The trash (2026-10-23, the user: "a way to restore individual deleted media and clear the bin"): deleted files stay
// restorable until removed for good - chosen ones, everything, or (setting trashDays) those deleted longer ago.
// Searching it: scope=trash (the trash's own index); restoring: POST /api/undelete.

// trashInfo: GET /api/trash -> {files, bytes, comics, comic_bytes, days}: what the trash holds (files; comics and
// their own pages), and after how many days it empties itself (0 = never).
func (s *Server) trashInfo(w http.ResponseWriter, r *http.Request) {
	st, err := s.lib.Stats(r.Context(), 0, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var days float64
	if set, err := s.lib.Settings(r.Context()); err == nil && set["trashDays"] != nil {
		json.Unmarshal(set["trashDays"], &days)
	}
	comics, comicBytes, err := s.lib.ComicsTrash(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": st.Trash.Files, "bytes": st.Trash.Bytes, "comics": comics, "comic_bytes": comicBytes, "days": days})
}

// purgeTrash: POST /api/trash/purge {ids} -> {purged}: those deleted files removed for good (their originals too).
func (s *Server) purgeTrash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []uint32 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.lib.PurgeFiles(r.Context(), req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("removed from the trash for good", "files", n)
	writeJSON(w, http.StatusOK, map[string]int{"purged": n})
}

// emptyTrash: POST /api/trash/empty -> {purged}: every deleted file removed for good.
func (s *Server) emptyTrash(w http.ResponseWriter, r *http.Request) {
	n, err := s.lib.EmptyTrash(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("emptied the trash", "files", n)
	writeJSON(w, http.StatusOK, map[string]int{"purged": n})
}

// purgeComicsTrash: POST /api/trash/purge-comics {ids} -> {purged}: those deleted comics removed for good.
func (s *Server) purgeComicsTrash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.lib.PurgeComics(r.Context(), req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("removed comics from the trash for good", "comics", n)
	writeJSON(w, http.StatusOK, map[string]int{"purged": n})
}

// emptyComicsTrash: POST /api/trash/empty-comics -> {purged}: every deleted comic removed for good.
func (s *Server) emptyComicsTrash(w http.ResponseWriter, r *http.Request) {
	n, err := s.lib.EmptyComicsTrash(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("emptied the comics trash", "comics", n)
	writeJSON(w, http.StatusOK, map[string]int{"purged": n})
}
