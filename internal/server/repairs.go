package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
)

// Fixes an update offers (library/repairs.go), for the top bar's button.

// listRepairs: GET /api/repairs -> {repairs: [...]}.
func (s *Server) listRepairs(w http.ResponseWriter, r *http.Request) {
	list, _ := s.lib.Repairs()
	writeJSON(w, http.StatusOK, map[string]any{"repairs": list})
}

// repairAction: POST /api/repairs/{id}/run, skip or ack -> {repairs: [...]}.
func (s *Server) repairAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "run":
		err = s.lib.RunRepair(r.Context(), id)
	case "skip":
		err = s.lib.SkipRepair(r.Context(), id)
	case "ack":
		err = s.lib.AckRepair(r.Context(), id)
	default:
		writeError(w, http.StatusNotFound, errors.New("no such action"))
		return
	}
	if errors.Is(err, library.ErrNoRepair) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.listRepairs(w, r)
}

// RepairNotice puts a fix being offered or ending in the bell (worded by the page: its query names the fix).
func (s *Server) RepairNotice(r library.Repair) {
	n := library.Notice{At: time.Now().UnixMilli(), Kind: "repair", Query: r.ID, New: int(r.Changed), Error: r.Error, First: r.State == library.RepairOffered}
	s.notices.put(n)
}
