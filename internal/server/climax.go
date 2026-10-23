package server

import (
	"net/http"
	"strconv"
	"time"
)

// The viewer's "I came" button and the Rankings page (opt-in: Settings > Viewing).

// climax: POST /api/climax {file} | {comic, page} -> {id, count}: a press on a file (the viewer; its count), or on a
// comic's page (the reader; page 0-based - that page's count).
func (s *Server) climax(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File  uint32 `json:"file"`
		Comic int64  `json:"comic"`
		Page  int    `json:"page"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var id int64
	var n int
	var err error
	if req.Comic != 0 {
		id, n, err = s.lib.AddComicClimax(r.Context(), req.Comic, req.Page)
	} else {
		id, n, err = s.lib.AddClimax(r.Context(), req.File)
	}
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id, "count": int64(n)})
}

// undoClimax: POST /api/climax/undo {id} -> {file, count}.
func (s *Server) undoClimax(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, n, err := s.lib.UndoClimax(r.Context(), req.ID)
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"file": int64(file), "count": int64(n)})
}

// rankings: GET /api/rankings?period=week|month|year|all&tz=<minutes east of UTC>.
func (s *Server) rankings(w http.ResponseWriter, r *http.Request) {
	tz, _ := strconv.ParseInt(r.URL.Query().Get("tz"), 10, 64)
	tz = min(max(tz, -14*60), 14*60)
	days := map[string]int{"week": 7, "month": 30, "year": 365, "all": 0}
	period := r.URL.Query().Get("period")
	n, ok := days[period]
	if !ok {
		n, period = 7, "week"
	}
	var from int64
	chart := n
	if n > 0 {
		// From the start of the first day of the period (the caller's days).
		now := time.Now().Unix() + tz*60
		from = (now - now%86400 - int64(n-1)*86400 - tz*60) * 1000
	} else {
		chart = 365
	}
	res, err := s.lib.Rankings(r.Context(), from, chart, tz*60)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// removeClimaxes: POST /api/climax/remove {file, all} -> {count}: the file's
// latest press taken off, or all of them.
func (s *Server) removeClimaxes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File uint32 `json:"file"`
		All  bool   `json:"all"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.lib.RemoveClimaxes(r.Context(), req.File, req.All)
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

// clearClimaxes: POST /api/climax/clear -> {removed}: every press removed.
func (s *Server) clearClimaxes(w http.ResponseWriter, r *http.Request) {
	n, err := s.lib.ClearClimaxes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}
