package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/usevitrine/vitrine/internal/library"
)

// The tag browser: GET /api/tags?q=&ns=&sort=count|name&offset=&limit= ->
// {total, tags: [{tag, namespace, count, ideal?, from_sites}]}.
func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	total, list, err := s.lib.TagList(r.Context(), q.Get("q"), q.Get("ns"), q.Get("sort") == "name",
		intParam(r, "offset", 0, 0, 1<<30), intParam(r, "limit", 100, 1, 1000))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "tags": orEmpty(list)})
}

// renameTag: POST /api/tags/rename {from, to} -> {files}.
func (s *Server) renameTag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.To) == "" || strings.ContainsAny(strings.TrimSpace(req.To), " \t") {
		writeError(w, http.StatusBadRequest, errors.New("the new name must be one tag (use _ instead of spaces)"))
		return
	}
	n, err := s.lib.RenameTag(r.Context(), s.lib.ParseTag(req.From), s.lib.ParseTag(req.To))
	switch {
	case errors.Is(err, library.ErrNoTag):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, library.ErrSiteTag), errors.Is(err, library.ErrHasRelations), errors.Is(err, library.ErrInvalid):
		writeError(w, http.StatusConflict, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, map[string]int{"files": n})
	}
}

// setTagNamespace: POST /api/tags/namespace {tag, namespace} ("" = general) ->
// {tag, files}: the tag moved there on every file (merged into a tag of that
// name there, if any).
func (s *Server) setTagNamespace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tag       string `json:"tag"`
		Namespace string `json:"namespace"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	t, n, err := s.lib.SetNamespace(r.Context(), s.lib.ParseTag(req.Tag), strings.TrimSpace(req.Namespace))
	switch {
	case errors.Is(err, library.ErrNoTag):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, library.ErrBadNamespace):
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, library.ErrHasRelations):
		writeError(w, http.StatusConflict, errors.New("this tag has parents or siblings and a tag of that name is there already: remove them first"))
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"tag": t.String(), "files": n})
	}
}
