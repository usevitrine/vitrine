package server

import (
	"errors"
	"net/http"

	"github.com/usevitrine/vitrine/internal/i18n"
	"github.com/usevitrine/vitrine/internal/push"
)

// Push notifications:
//   GET  /api/push              -> {key, devices}  (key: the VAPID public key for PushManager.subscribe)
//   POST /api/push/subscribe    {subscription, label, language}
//   POST /api/push/unsubscribe  {endpoint}
//   POST /api/push/test         {endpoint}  sends a test notification to that device

// SetPush turns on the push notification endpoints.
func (s *Server) SetPush(p *push.Service) { s.push = p }

func (s *Server) pushReady(w http.ResponseWriter) bool {
	if s.push == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("push notifications aren't available"))
		return false
	}
	return true
}

func (s *Server) pushInfo(w http.ResponseWriter, r *http.Request) {
	if !s.pushReady(w) {
		return
	}
	devs, err := s.push.Devices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": s.push.PublicKey(), "devices": devs})
}

func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.pushReady(w) {
		return
	}
	var req struct {
		Subscription push.Subscription `json:"subscription"`
		Label        string            `json:"label"`
		Language     string            `json:"language"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.push.Subscribe(r.Context(), req.Subscription, req.Label, req.Language); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.pushInfo(w, r)
}

func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.pushReady(w) {
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.push.Unsubscribe(r.Context(), req.Endpoint); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.pushInfo(w, r)
}

func (s *Server) pushTest(w http.ResponseWriter, r *http.Request) {
	if !s.pushReady(w) {
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	l := i18n.For(s.push.Language(r.Context(), req.Endpoint))
	err := s.push.SendTo(r.Context(), req.Endpoint, push.Message{
		Title: l.T("push.testTitle"), Body: l.T("push.testBody"), URL: "/settings"})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}
