package server

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/search"
)

// Saved searches (favorites), kept on the server so every device has them.

func savedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNoSaved):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, library.ErrInvalid):
		writeError(w, http.StatusBadRequest, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) writeSaved(w http.ResponseWriter, r *http.Request) {
	list, err := s.lib.SavedSearches(r.Context(), r.URL.Query().Get("scope"))
	if err != nil {
		savedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": orEmpty(list)})
}

// listSaved: GET /api/saved -> {saved: [{id, name, query, sort, count}]}.
func (s *Server) listSaved(w http.ResponseWriter, r *http.Request) { s.writeSaved(w, r) }

// addSaved: POST /api/saved {name, query, sort} -> {id, saved}. (Every saved call takes ?scope=comics for the Comics page's list.)
func (s *Server) addSaved(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Query string `json:"query"`
		Sort  string `json:"sort"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id, err := s.lib.SaveSearch(r.Context(), req.Name, req.Query, req.Sort, r.URL.Query().Get("scope"))
	if err != nil {
		savedError(w, err)
		return
	}
	list, err := s.lib.SavedSearches(r.Context(), r.URL.Query().Get("scope"))
	if err != nil {
		savedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "saved": orEmpty(list)})
}

func savedID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad saved search ID"))
		return 0, false
	}
	return id, true
}

// updateSaved: POST /api/saved/{id} {name?, query?, sort?} -> {saved}.
func (s *Server) updateSaved(w http.ResponseWriter, r *http.Request) {
	id, ok := savedID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name  *string `json:"name"`
		Query *string `json:"query"`
		Sort  *string `json:"sort"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.lib.UpdateSavedSearch(r.Context(), id, library.SavedChange{Name: req.Name, Query: req.Query, Sort: req.Sort}); err != nil {
		savedError(w, err)
		return
	}
	s.writeSaved(w, r)
}

// removeSaved: POST /api/saved/{id}/remove -> {saved}.
func (s *Server) removeSaved(w http.ResponseWriter, r *http.Request) {
	id, ok := savedID(w, r)
	if !ok {
		return
	}
	if err := s.lib.DeleteSavedSearch(r.Context(), id); err != nil {
		savedError(w, err)
		return
	}
	s.writeSaved(w, r)
}

// reorderSaved: POST /api/saved/reorder {ids} -> {saved}.
func (s *Server) reorderSaved(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.lib.ReorderSavedSearches(r.Context(), req.IDs); err != nil {
		savedError(w, err)
		return
	}
	s.writeSaved(w, r)
}

// ---- NEW pills in the saved searches menu: per saved search and per group
// (parent) tag, the files imported since it was last opened from the menu.

type newsItem struct {
	ID       int64  `json:"id,omitempty"`  // a saved search
	Tag      string `json:"tag,omitempty"` // a group tag
	Children int    `json:"children,omitempty"`
	New      int    `json:"new"`
	SeenAt   int64  `json:"seen_at"`
}

// news: GET /api/news (?new_bulk=0: bulk downloads don't count, the
// newFromBulk setting) -> {saved: [...], groups: [...]}.
func (s *Server) news(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.lib.SavedSearches(ctx, "")
	if err != nil {
		savedError(w, err)
		return
	}
	// Group tags: the parents, with how many tags they gather.
	parents, _ := s.lib.Index().RelationNames()
	children := map[string]int{}
	var groups []string
	for _, p := range parents {
		if children[p[1]] == 0 {
			groups = append(groups, p[1])
		}
		children[p[1]]++
	}
	sort.Strings(groups)
	keys := make([]string, 0, len(list)+len(groups))
	for _, sv := range list {
		keys = append(keys, "saved:"+strconv.FormatInt(sv.ID, 10))
	}
	for _, g := range groups {
		keys = append(keys, "tag:"+g)
	}
	marks, err := s.lib.SeenMarks(ctx, keys)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now()
	count := func(text string, since int64) int {
		q, err := search.Parse(text)
		if err != nil {
			return 0
		}
		q.Limit = 0
		return s.lib.Index().CountNew(s.lib.Index().Search(q, now).IDs, newOpts(r, since))
	}
	out := struct {
		Saved  []newsItem `json:"saved"`
		Groups []newsItem `json:"groups"`
	}{Saved: []newsItem{}, Groups: []newsItem{}}
	for _, sv := range list {
		at := marks["saved:"+strconv.FormatInt(sv.ID, 10)]
		out.Saved = append(out.Saved, newsItem{ID: sv.ID, New: count(sv.Query, at), SeenAt: at})
	}
	for _, g := range groups {
		at := marks["tag:"+g]
		out.Groups = append(out.Groups, newsItem{Tag: g, Children: children[g], New: count(g, at), SeenAt: at})
	}
	writeJSON(w, http.StatusOK, out)
}

// seen: POST /api/seen {key, at?} -> {previous}: a saved search ("saved:<id>") or
// group tag ("tag:<tag>") opened from the menu; previous is when it was last
// opened before (its files since then are shown as NEW). at (Unix ms) sets
// another time (e.g. earlier: its files since then count as new again).
func (s *Server) seen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
		At  int64  `json:"at"`
	}
	if err := readJSON(r, &req); err != nil || !(strings.HasPrefix(req.Key, "saved:") || strings.HasPrefix(req.Key, "tag:")) || len(req.Key) > 600 {
		writeError(w, http.StatusBadRequest, errors.New("bad key"))
		return
	}
	prev, err := s.lib.MarkSeen(r.Context(), req.Key, req.At)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"previous": prev})
}
