package server

import (
	"errors"
	"net/http"

	"github.com/usevitrine/vitrine/internal/download"
)

// Plugins (Settings > Sites):
//   GET  /api/plugins[?refresh=1]  -> {plugins: [{id, name, site, version, description, nsfw, installed, installed_version, update}], error}
//                                     (the plugin list; error says why it couldn't be read - not a failure)
//   POST /api/plugins/install {id}   installs or updates one from the list -> {installed, sites}
//   POST /api/plugins/upload {yaml}  installs or updates from a plugin file  -> {installed, sites}
//   POST /api/plugins/{id}/remove    removes an installed plugin {wipe, backups} (files, searches, credentials stay unless wiped) -> {removed, sites, wiping}
//   GET  /api/plugins/{id}/traces    what wiping the site would remove -> {traces, any, backups}

func (s *Server) listPlugins(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	list, err := s.dl.Plugins(r.Context(), r.URL.Query().Get("refresh") != "")
	out := map[string]any{"plugins": orEmpty(list)}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) installPlugin(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.dl.InstallFromIndex(r.Context(), req.ID)
	s.installed(w, res, err)
}

func (s *Server) uploadPlugin(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		YAML string `json:"yaml"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.dl.InstallPlugin(r.Context(), []byte(req.YAML))
	s.installed(w, res, err)
}

func (s *Server) installed(w http.ResponseWriter, res download.Installed, err error) {
	switch {
	case errors.Is(err, download.ErrBadPlugin), errors.Is(err, download.ErrUnknownSite):
		writeError(w, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"installed": res, "sites": orEmpty(s.dl.SiteInfos())})
	}
}

// removePlugin: POST /api/plugins/{id}/remove {wipe, backups} -> {removed, sites, wiping}. Files, tags, history,
// searches, subscriptions and credentials stay - or, with wipe, every trace of the site goes (wipe.go; not when the
// site stays, back to its built-in plugin).
func (s *Server) removePlugin(w http.ResponseWriter, r *http.Request) {
	if s.dl == nil {
		writeError(w, http.StatusServiceUnavailable, errNoDownloader)
		return
	}
	var req struct {
		Wipe    bool `json:"wipe"`
		Backups bool `json:"backups"` // the older backups too
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.dl.RemovePlugin(r.Context(), r.PathValue("id"))
	wipe := err == nil && req.Wipe && !res.Restored && s.wipeSite(res, req.Backups)
	switch {
	case errors.Is(err, download.ErrUnknownSite):
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"removed": res, "sites": orEmpty(s.dl.SiteInfos()), "wiping": wipe})
	}
}
