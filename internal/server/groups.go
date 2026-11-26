package server

import (
	"errors"
	"net/http"
	"time"
)

// Subscription groups (listed with the downloads: GET /api/downloads -> groups):
//   POST /api/sub-groups {site, name}               -> group (an existing one of that name is reused)
//   POST /api/sub-groups/{id} {name?, interval?, check_depth?}
//                                                    rename; set every member's interval (seconds);
//                                                    its check depth (-1: none)
//   POST /api/sub-groups/{id}/{check|pause|resume|remove}
//                                                    remove deletes the group; its subscriptions stay

func (s *Server) addGroup(w http.ResponseWriter, r *http.Request) {
	if !s.needDL(w) {
		return
	}
	var req struct {
		Site string `json:"site"`
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	id, err := s.dl.CreateGroup(r.Context(), req.Site, req.Name)
	if err != nil {
		downloadError(w, err)
		return
	}
	g, err := s.dl.GetGroup(r.Context(), id)
	if err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request) {
	if !s.needDL(w) {
		return
	}
	id, ok := pathID(w, r, "group")
	if !ok {
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Interval *int64  `json:"interval"`
		Depth    *int    `json:"check_depth"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil {
		if err := s.dl.RenameGroup(r.Context(), id, *req.Name); err != nil {
			downloadError(w, err)
			return
		}
	}
	if req.Interval != nil {
		if err := s.dl.SetGroupInterval(r.Context(), id, time.Duration(*req.Interval)*time.Second); err != nil {
			downloadError(w, err)
			return
		}
	}
	if req.Depth != nil {
		if err := s.dl.SetGroupCheckDepth(r.Context(), id, *req.Depth); err != nil {
			downloadError(w, err)
			return
		}
	}
	g, err := s.dl.GetGroup(r.Context(), id)
	if err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) groupAction(w http.ResponseWriter, r *http.Request) {
	if !s.needDL(w) {
		return
	}
	id, ok := pathID(w, r, "group")
	if !ok {
		return
	}
	var err error
	switch a := r.PathValue("action"); a {
	case "remove":
		err = s.dl.DeleteGroup(r.Context(), id)
	case "check", "pause", "resume":
		err = s.dl.GroupAction(r.Context(), id, a)
	default:
		writeError(w, http.StatusNotFound, errors.New("unknown action"))
		return
	}
	if err != nil {
		downloadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
