package server

// The account (Settings > Account, 2026-10-31): changing the name and the password, logging out other devices, and
// passkeys - adding them, signing in with one, signing in with passkeys only (internal/auth/passkeys.go).

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/auth"
)

// ceremonyCookie holds a passkey step's ID between its two requests (5 minutes, this site only, never to scripts).
const ceremonyCookie = "vitrine_passkey"

func userOf(r *http.Request) auth.User {
	u, _ := r.Context().Value(userKey).(auth.User)
	return u
}

// originOf is the address the browser is on (for passkeys: what a new one is bound to).
func originOf(r *http.Request) string {
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// clientOf says where a request came from, for the log (the proxy's word for it when there is one: information only,
// never trusted for anything).
func clientOf(r *http.Request) string {
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		return strings.TrimSpace(strings.Split(f, ",")[0]) + " (via " + r.RemoteAddr + ")"
	}
	return r.RemoteAddr
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		MaxAge:   int(auth.SessionIdle / time.Second),
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

func setCeremony(w http.ResponseWriter, r *http.Request, id string) {
	http.SetCookie(w, &http.Cookie{Name: ceremonyCookie, Value: id, Path: "/api/", MaxAge: 300,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteStrictMode})
}

// takeCeremony reads (and clears) the passkey step's cookie.
func takeCeremony(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(ceremonyCookie)
	http.SetCookie(w, &http.Cookie{Name: ceremonyCookie, Value: "", Path: "/api/", MaxAge: -1,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteStrictMode})
	if err != nil {
		return ""
	}
	return c.Value
}

// ---- signing in with a passkey (open to everyone)

// passkeyLoginBegin: POST /api/passkeys/login/begin {} -> the browser's options (publicKey).
func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	assertion, id, err := s.auth.BeginPasskeyLogin(originOf(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	setCeremony(w, r, id)
	writeJSON(w, http.StatusOK, assertion)
}

// passkeyLoginFinish: POST /api/passkeys/login/finish <the browser's answer> -> {logged_in, user} and the session.
func (s *Server) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token, u, err := s.auth.FinishPasskeyLogin(r.Context(), takeCeremony(w, r), body, r.UserAgent())
	if err != nil {
		s.log.Warn("passkey sign-in failed", "from", clientOf(r), "err", err)
		status := http.StatusUnauthorized
		if errors.Is(err, auth.ErrCeremony) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	s.log.Info("signed in", "user", u.Name, "with", "a passkey", "from", clientOf(r))
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"logged_in": true, "user": u.Name})
}

// ---- the account's passkeys (signed in)

// listPasskeys: GET /api/passkeys -> {passkeys, passkey_only, here}.
func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request) {
	list, err := s.auth.Passkeys(r.Context(), userOf(r).ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	only, err := s.auth.PasskeyOnly(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": list, "passkey_only": only, "here": originOf(r)})
}

// passkeyBegin: POST /api/passkeys/begin {} -> the browser's options for a new passkey (publicKey).
func (s *Server) passkeyBegin(w http.ResponseWriter, r *http.Request) {
	creation, id, err := s.auth.BeginPasskey(r.Context(), userOf(r).ID, originOf(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	setCeremony(w, r, id)
	writeJSON(w, http.StatusOK, creation)
}

// passkeyFinish: POST /api/passkeys/finish {name, credential} -> the new passkey.
func (s *Server) passkeyFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.auth.FinishPasskey(r.Context(), userOf(r).ID, takeCeremony(w, r), req.Name, req.Credential)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Info("passkey added", "user", userOf(r).Name, "name", p.Name, "from", clientOf(r))
	writeJSON(w, http.StatusOK, p)
}

// changePasskey: POST /api/passkeys/{id}/remove {} | /rename {name}.
func (s *Server) changePasskey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	u := userOf(r)
	switch r.PathValue("action") {
	case "remove":
		err = s.auth.RemovePasskey(r.Context(), u.ID, id)
		if err == nil {
			s.log.Info("passkey removed", "user", u.Name, "from", clientOf(r))
		}
	case "rename":
		var req struct {
			Name string `json:"name"`
		}
		if err = readJSON(r, &req); err == nil {
			err = s.auth.RenamePasskey(r.Context(), u.ID, id, req.Name)
		}
	default:
		writeError(w, http.StatusNotFound, errors.New("no such action"))
		return
	}
	if errors.Is(err, auth.ErrNoPasskey) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.listPasskeys(w, r)
}

// passkeyOnly: POST /api/passkeys/only {on} -> the list again.
func (s *Server) passkeyOnly(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	u := userOf(r)
	if err := s.auth.SetPasskeyOnly(r.Context(), u.ID, req.On); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Info("passkey-only sign-in", "user", u.Name, "on", req.On, "from", clientOf(r))
	s.listPasskeys(w, r)
}

// ---- the name, the password, other devices (signed in; the current password first)

// accountChange: POST /api/account/password {current, password} | /api/account/name {current, name}.
func (s *Server) accountChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current  string `json:"current"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	u := userOf(r)
	var err error
	what := r.PathValue("what")
	switch what {
	case "password":
		keep := ""
		if c, e := r.Cookie(sessionCookie); e == nil {
			keep = c.Value
		}
		err = s.auth.ChangePassword(r.Context(), u.ID, req.Current, req.Password, keep)
	case "name":
		err = s.auth.Rename(r.Context(), u.ID, req.Current, req.Name)
	default:
		writeError(w, http.StatusNotFound, errors.New("no such change"))
		return
	}
	switch {
	case errors.Is(err, auth.ErrBadPassword):
		s.log.Warn("account change refused: wrong password", "user", u.Name, "change", what, "from", clientOf(r))
		writeError(w, http.StatusForbidden, err)
		return
	case errors.Is(err, auth.ErrTooMany):
		writeError(w, http.StatusTooManyRequests, err)
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Info("account changed", "user", u.Name, "change", what, "from", clientOf(r))
	name := u.Name
	if what == "name" {
		name = req.Name
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": name})
}

// logoutOthers: POST /api/account/logout-others {} -> {ended}: every other device signed out.
func (s *Server) logoutOthers(w http.ResponseWriter, r *http.Request) {
	keep := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		keep = c.Value
	}
	n, err := s.auth.EndOtherSessions(r.Context(), userOf(r).ID, keep)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.log.Info("other devices signed out", "user", userOf(r).Name, "sessions", n, "from", clientOf(r))
	writeJSON(w, http.StatusOK, map[string]int{"ended": n})
}
