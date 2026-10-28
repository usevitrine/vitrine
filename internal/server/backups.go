package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/backup"
	"github.com/usevitrine/vitrine/internal/i18n"
	"github.com/usevitrine/vitrine/internal/library"
)

// Backups: copies of meta.db (see internal/backup), made daily and on demand,
// downloadable, restorable on the next start.

// backups is the server's backup state.
type backups struct {
	m       *backup.Manager
	restart func() // stops Vitrine so a supervisor (Docker, systemd) starts it again
	mu      sync.Mutex
	lastErr string
}

// SetBackups enables the backup API. restart stops the server (a staged
// restore is applied when it starts again).
func (s *Server) SetBackups(m *backup.Manager, restart func()) {
	s.backups = &backups{m: m, restart: restart}
	s.handle("GET /api/backups", s.listBackups)
	s.handle("POST /api/backups", s.createBackup)
	s.handle("POST /api/backups/upload", s.uploadBackup)
	s.handle("POST /api/backups/cancel-restore", s.cancelRestore)
	s.handle("GET /api/backups/{name}/download", s.downloadBackup)
	s.handle("POST /api/backups/{name}/{action}", s.backupAction)
	s.handle("POST /api/restart", s.restartServer)
}

// BackupSettings are the daily backup's settings: on unless turned off,
// keeping 7 unless set.
func (s *Server) BackupSettings(ctx context.Context) (auto bool, keep int) {
	auto, keep = true, 7
	set, err := s.lib.Settings(ctx)
	if err != nil {
		return
	}
	if string(set["backupAuto"]) == "false" {
		auto = false
	}
	var n float64
	if json.Unmarshal(set["backupKeep"], &n) == nil && n >= 1 {
		keep = int(n)
	}
	return
}

// RunDailyBackups makes a backup when the newest one is a day old (checked
// hourly; the first check a few minutes after the start).
func (s *Server) RunDailyBackups(ctx context.Context) {
	if s.backups == nil {
		return
	}
	wait := 5 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = time.Hour
		auto, keep := s.BackupSettings(ctx)
		if !auto {
			continue
		}
		list, err := s.backups.m.List()
		if err == nil && len(list) > 0 && time.Since(time.UnixMilli(list[0].CreatedAt)) < 23*time.Hour+30*time.Minute {
			continue
		}
		b, err := s.backups.m.Create(ctx, keep)
		s.backups.mu.Lock()
		if err != nil {
			s.backups.lastErr = err.Error()
			s.log.Error("daily backup failed", "err", err)
			// (a notice too: Settings > Backups shows it only to whoever looks there)
			s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "app", Text: i18n.For("").T("backups.dailyFailed", "error", err.Error()), Tone: "error", Category: "errors"})
		} else {
			s.backups.lastErr = ""
			s.log.Info("daily backup made", "name", b.Name, "size", b.Size)
		}
		s.backups.mu.Unlock()
	}
}

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	list, err := s.backups.m.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	auto, keep := s.BackupSettings(r.Context())
	s.backups.mu.Lock()
	lastErr := s.backups.lastErr
	s.backups.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"backups":    orEmpty(list),
		"dir":        s.backups.m.Dir(),
		"auto":       auto,
		"keep":       keep,
		"pending":    s.backups.m.Pending(), // a restore waits for the next start
		"last_error": lastErr,
	})
}

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	_, keep := s.BackupSettings(r.Context())
	b, err := s.backups.m.Create(r.Context(), keep)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backup": b})
}

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := s.backups.m.Path(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, p)
}

func (s *Server) backupAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var err error
	switch r.PathValue("action") {
	case "delete":
		err = s.backups.m.Delete(name)
	case "restore":
		err = s.backups.m.StageBackup(name)
	default:
		writeError(w, http.StatusNotFound, errors.New("no such action"))
		return
	}
	switch {
	case errors.Is(err, backup.ErrBadName):
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pending": s.backups.m.Pending()})
	}
}

// uploadBackup stages a backup sent as the request body (a .db.gz from
// Download, or a plain meta.db).
func (s *Server) uploadBackup(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 16<<30)
	if err := s.backups.m.Stage(body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pending": true})
}

func (s *Server) cancelRestore(w http.ResponseWriter, r *http.Request) {
	if err := s.backups.m.CancelRestore(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pending": false})
}

// restartServer answers, then stops Vitrine; Docker or a service manager
// starts it again (and a staged restore is put in place then).
func (s *Server) restartServer(w http.ResponseWriter, r *http.Request) {
	if s.backups == nil || s.backups.restart == nil {
		writeError(w, http.StatusNotImplemented, errors.New("restarting isn't available"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	s.log.Info("restart asked for in the web app")
	go func() {
		time.Sleep(300 * time.Millisecond)
		s.backups.restart()
	}()
}
