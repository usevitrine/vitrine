package server

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

// Text answers (the web app's code, its styles, JSON) are gzipped when the
// browser accepts it: a third of the size on a slow connection. Files,
// thumbnails, previews and the live event stream never are (already
// compressed, or streamed).

var gzipPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
	return w
}}

// mayGzip is whether a request's answer can be gzipped at all.
func mayGzip(r *http.Request) bool {
	if r.Method != http.MethodGet || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Header.Get("Range") != "" {
		return false
	}
	p := r.URL.Path
	return !strings.HasPrefix(p, "/files/") && !strings.HasPrefix(p, "/thumbs/") && !strings.HasPrefix(p, "/sprites/") &&
		p != "/api/events" && !strings.HasPrefix(p, "/api/backups/")
}

func compressible(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "text/") || strings.HasPrefix(ct, "application/json") ||
		strings.Contains(ct, "javascript") || strings.HasPrefix(ct, "image/svg") || strings.Contains(ct, "manifest+json")
}

// gzipWriter decides at the first header or byte whether to compress.
type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if !g.decided {
		g.decided = true
		h := g.Header()
		if code == http.StatusOK && h.Get("Content-Encoding") == "" && compressible(h.Get("Content-Type")) {
			h.Del("Content-Length")
			h.Set("Content-Encoding", "gzip")
			h.Add("Vary", "Accept-Encoding")
			g.gz = gzipPool.Get().(*gzip.Writer)
			g.gz.Reset(g.ResponseWriter)
		}
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) Flush() {
	if g.gz != nil {
		g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// close finishes the gzip stream (and returns the writer to the pool).
func (g *gzipWriter) close() {
	if g.gz != nil {
		g.gz.Close()
		gzipPool.Put(g.gz)
		g.gz = nil
	}
}

func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }
