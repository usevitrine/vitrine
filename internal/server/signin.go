package server

import (
	"embed"
	"net/http"
)

// The anonymous sign-in page (setting anonymousLogin; 2026-10-23, the user: "no info anywhere on the login page that
// indicates what the login page is for so if someone finds it on someone's network, they can't look up what the
// software is"). Signed out, every page path gets a plain form - no name, logo, icon, theme color, manifest or the
// app's scripts - and every other file of the app answers 404; the API and media check the login as always.
// /signin is its own address (the service worker leaves it alone; the app sends a signed-out visitor there).

//go:embed signin
var signinFS embed.FS

// unlessAnonymous serves next, or - anonymous and signed out - the plain sign-in page.
func (s *Server) unlessAnonymous(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.anonymous.Load() || s.signedIn(r) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; "+
			"img-src data:; form-action 'none'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("Cache-Control", "no-store") // (never kept: not by the browser, not by the service worker)
		h.Set("X-Robots-Tag", "noindex, nofollow")
		switch path := r.URL.Path; {
		case path == "/_s/signin.css":
			serveEmbedded(w, "signin/signin.css", "text/css; charset=utf-8")
		case path == "/_s/signin.js":
			serveEmbedded(w, "signin/signin.js", "text/javascript; charset=utf-8")
		case isPagePath(path):
			serveEmbedded(w, "signin/index.html", "text/html; charset=utf-8")
		default:
			http.NotFound(w, r) // (the app's scripts, icons, manifest, ...: nothing to look up)
		}
	})
}

// isPagePath: a path the app answers with its page (no file extension): "/", "/signin", "/downloads", ...
func isPagePath(p string) bool {
	for i := len(p) - 1; i >= 0 && p[i] != '/'; i-- {
		if p[i] == '.' {
			return false
		}
	}
	return true
}

func serveEmbedded(w http.ResponseWriter, name, contentType string) {
	b, err := signinFS.ReadFile(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(b)
}

// signedIn tells whether the request carries a valid session.
func (s *Server) signedIn(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	_, err = s.auth.Session(r.Context(), c.Value)
	return err == nil
}
