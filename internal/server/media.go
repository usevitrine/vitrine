package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"

	"github.com/usevitrine/vitrine/internal/store"
)

// Signed file links. The API hands out file and thumbnail URLs with a
// signature of the file's hash (?k=), so media loads without the session
// cookie - some browsers' video loaders don't send it, and iOS is a
// first-class target. A link stays valid (it is cached for good) and only
// opens that one file; the links only come from the logged-in API.

// sign returns the signature for a file's hash ("" if signing is off).
func (s *Server) sign(h store.Hash) string {
	if s.mediaKey == nil {
		return ""
	}
	mac := hmac.New(sha256.New, s.mediaKey)
	mac.Write([]byte("file:"))
	mac.Write(h[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18])
}

// mayRead reports whether the request may read the file with hash h: a
// valid signature, or a logged-in session.
func (s *Server) mayRead(r *http.Request, h store.Hash) bool {
	if k := r.URL.Query().Get("k"); k != "" && s.mediaKey != nil {
		if hmac.Equal([]byte(k), []byte(s.sign(h))) {
			return true
		}
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	_, err = s.auth.Session(r.Context(), c.Value)
	return err == nil
}

// linkSuffix is the query part that signs a file link ("" or "k=...").
func (s *Server) linkSuffix(h store.Hash) string {
	if k := s.sign(h); k != "" {
		return "k=" + k
	}
	return ""
}
