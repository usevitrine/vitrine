package server

import (
	"net/http"

	"github.com/usevitrine/vitrine/internal/library"
)

// View counts (Settings > Viewing, "Count views"): the viewer gathers them and sends them here.

type viewJSON struct {
	ID       uint32 `json:"id"`
	Views    int    `json:"views"`
	ViewMS   int64  `json:"view_ms"`
	ViewedAt int64  `json:"viewed_at"`
}

// addViews: POST /api/views {views: [{file, at, ms}]} -> {files: [{id, views, view_ms, viewed_at}]}: the views
// recorded (one within half an hour of the file's last adds to it), each file's counts now.
func (s *Server) addViews(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Views []library.View `json:"views"`
	}
	if !decode(w, r, &req) {
		return
	}
	got, err := s.lib.AddViews(r.Context(), req.Views)
	if err != nil {
		libError(w, err)
		return
	}
	out := make([]viewJSON, 0, len(got))
	for id, v := range got {
		out = append(out, viewJSON{ID: id, Views: v.Count, ViewMS: v.MS, ViewedAt: v.Last})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

// clearViews: POST /api/views/clear -> {removed}: every view removed.
func (s *Server) clearViews(w http.ResponseWriter, r *http.Request) {
	n, err := s.lib.ClearViews(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}
