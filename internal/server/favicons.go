package server

import (
	"errors"
	"net/http"

	"github.com/usevitrine/vitrine/internal/favicon"
)

// SetFavicons enables site icons beside the links to where a file came from.
func (s *Server) SetFavicons(c *favicon.Cache) { s.icons = c }

// favicon serves a site's icon: GET /api/favicon?host=example.com. 404: the
// site has none (the page shows its own link icon); an icon is cached for a
// week, "none" for an hour (it can turn up: e.g. a plugin added for the site).
func (s *Server) favicon(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if s.icons == nil || !favicon.ValidHost(host) {
		w.Header().Set("Cache-Control", "private, max-age=3600")
		writeError(w, http.StatusNotFound, errors.New("no icon"))
		return
	}
	var also []string // the site's other addresses (from its plugin)
	if s.dl != nil {
		also = s.dl.OtherHosts(host)
	}
	data, err := s.icons.Get(r.Context(), host, also...)
	if err != nil {
		if !errors.Is(err, favicon.ErrNone) {
			s.log.Warn("site icon", "host", host, "err", err)
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		writeError(w, http.StatusNotFound, errors.New("no icon"))
		return
	}
	w.Header().Set("Content-Type", favicon.ContentType(data))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Write(data)
}
