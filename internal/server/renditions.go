package server

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/renditions"
)

// SetRenditions enables resized copies of huge images (?size= on /files).
func (s *Server) SetRenditions(c *renditions.Cache) { s.rend = c }

// rendition returns a resized copy of a still image whose long side is
// larger than the requested size (snapped to an allowed size). ok is false
// when the original should be sent instead.
func (s *Server) rendition(r *http.Request, f library.FileRow, want int) (string, bool) {
	if s.rend == nil || !f.Type.IsImage() || f.Type == media.GIF || f.Width == 0 {
		return "", false // GIFs may be animated; unprobed files have no size yet
	}
	size := renditions.Snap(want)
	if max(f.Width, f.Height) <= size {
		return "", false
	}
	if r.URL.Query().Get("for") == "screen" {
		s.rend.UsedForScreen(size) // made ahead for new files from now on (renditions_ahead.go)
	}
	// The copy is made even if this request goes away: others may be waiting
	// for it, and the next view needs it anyway.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()
	p, err := s.rend.Get(ctx, f.SHA256.String(), size, func(ctx context.Context) ([]byte, error) {
		return s.lib.Tools().Rendition(ctx, f.Path, f.Width, f.Height, size)
	})
	if err != nil {
		s.log.Warn("rendition failed, sending the original", "file", f.ID, "size", size, "err", err)
		return "", false
	}
	return p, true
}

// sendFile serves a file from disk with immutable caching (its URL names
// the content).
func (s *Server) sendFile(w http.ResponseWriter, r *http.Request, path, mime string, modified int64) {
	fh, err := os.Open(path)
	if err != nil {
		http.Error(w, "file missing", http.StatusInternalServerError)
		return
	}
	defer fh.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", time.UnixMilli(modified), fh)
}
