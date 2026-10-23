package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/usevitrine/vitrine/internal/dupes"
	"github.com/usevitrine/vitrine/internal/library"
)

// Duplicates to review (see library/dupes.go).

var errBadKeep = errors.New("keep must be a, b or both")

type dupeFileJSON struct {
	fileJSON
	library.FileFacts
}

type dupePairJSON struct {
	A        dupeFileJSON `json:"a"`
	B        dupeFileJSON `json:"b"`
	Distance int          `json:"distance"`
}

// GET /api/dupes?max=6&offset=0&limit=20
func (s *Server) listDupes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	maxDist, err := strconv.Atoi(q.Get("max"))
	if err != nil || maxDist < 0 || maxDist > dupes.MaxStored {
		maxDist = 6
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}
	total, pairs, prog, err := s.lib.DupePairs(r.Context(), maxDist, max(0, offset), limit)
	if err != nil {
		libError(w, err)
		return
	}
	var ids []uint32
	for _, p := range pairs {
		ids = append(ids, p.A, p.B)
	}
	rows, err := s.lib.Files(r.Context(), ids)
	if err != nil {
		libError(w, err)
		return
	}
	facts, err := s.lib.Facts(r.Context(), ids)
	if err != nil {
		libError(w, err)
		return
	}
	byID := map[uint32]dupeFileJSON{}
	for _, f := range rows {
		byID[f.ID] = dupeFileJSON{s.toJSON(f), facts[f.ID]}
	}
	out := make([]dupePairJSON, 0, len(pairs))
	for _, p := range pairs {
		a, okA := byID[p.A]
		b, okB := byID[p.B]
		if okA && okB {
			out = append(out, dupePairJSON{a, b, p.Distance})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "pairs": out, "progress": prog})
}

// POST /api/dupes/decide {a, b, keep: "a" | "b" | "both"}
func (s *Server) decideDupe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		A    uint32 `json:"a"`
		B    uint32 `json:"b"`
		Keep string `json:"keep"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var err error
	switch req.Keep {
	case "a":
		err = s.lib.KeepOne(r.Context(), req.A, req.B)
	case "b":
		err = s.lib.KeepOne(r.Context(), req.B, req.A)
	case "both":
		err = s.lib.KeepBoth(r.Context(), req.A, req.B)
	default:
		writeError(w, http.StatusBadRequest, errBadKeep)
		return
	}
	if err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/dupes/undo {a, b}
func (s *Server) undoDupe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		A uint32 `json:"a"`
		B uint32 `json:"b"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.lib.UndoDupe(r.Context(), req.A, req.B); err != nil {
		libError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
