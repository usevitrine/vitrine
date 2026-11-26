package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/i18n"
	"github.com/usevitrine/vitrine/internal/library"
)

// Old files (library/oldfiles.go): how many a year would take, and the work on them - in the background, one at a time:
// a real library's can take a long while (a million files), and the page may be closed meanwhile. A notice says how it
// ended.

// cleanupJob is the work under way.
type cleanupJob struct {
	Kind  string `json:"kind"` // delete, restore or purge
	Year  int    `json:"year"` // (purge: 0 = the whole trash)
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

type cleanupState struct {
	mu     sync.Mutex
	job    *cleanupJob
	cancel context.CancelFunc
}

func (c *cleanupState) now() *cleanupJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job == nil {
		return nil
	}
	j := *c.job
	return &j
}

// phase returns what the library's work calls as it goes (a kind's own count).
func (c *cleanupState) phase(kind string) func(done, total int) {
	return func(done, total int) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.job.Kind, c.job.Done, c.job.Total = kind, done, total
	}
}

// startCleanup runs the work in the background (false: other work is under way; a 409 written). run returns how many
// files went and the notice's text key for it.
func (s *Server) startCleanup(w http.ResponseWriter, kind string, year int, run func(ctx context.Context) (int, string, error)) bool {
	c := &s.cleanup
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job != nil {
		writeError(w, http.StatusConflict, errors.New("other files are being deleted or restored: wait for that to finish"))
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.job, c.cancel = &cleanupJob{Kind: kind, Year: year}, cancel
	go func() {
		defer cancel()
		start := time.Now()
		n, key, err := run(ctx)
		c.mu.Lock()
		c.job, c.cancel = nil, nil
		c.mu.Unlock()
		tr := i18n.For("")
		switch {
		case errors.Is(err, context.Canceled):
			s.log.Info("old files: stopped", "kind", kind, "year", year, "files", n)
			s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "app", Text: tr.T("oldFiles.stopped", "n", n), Tone: "removed", Category: "deleted"})
		case err != nil:
			s.log.Error("old files", "kind", kind, "year", year, "files", n, "err", err)
			s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "app", Text: tr.T("appSections.deleteFailed", "error", err.Error()), Tone: "error", Category: "deleted"})
		default:
			s.log.Info("old files", "kind", kind, "year", year, "files", n, "took", time.Since(start).Round(time.Second))
			s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "app", Text: tr.T(key, "n", n), Tone: "removed", Category: "deleted"})
		}
	}()
	return true
}

// oldFiles: GET /api/old-files -> {years, undated, in_trash, job}: the library's files per upload year, those with no
// upload time and what such deletes left in the trash. (While work goes on: only the job. It would change as it's read.)
func (s *Server) oldFiles(w http.ResponseWriter, r *http.Request) {
	if j := s.cleanup.now(); j != nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": j})
		return
	}
	years, undated, err := s.lib.UploadYears(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	inTrash, err := s.lib.OldInTrash(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"years": years, "undated": undated, "in_trash": inTrash})
}

// oldFilesJob: GET /api/old-files/job -> {job}: the work under way (null: none).
func (s *Server) oldFilesJob(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"job": s.cleanup.now()})
}

// oldYear reads {year} and checks it (false: a 400 written).
func oldYear(w http.ResponseWriter, r *http.Request, req any, year *int) bool {
	if !decode(w, r, req) {
		return false
	}
	if *year < 1970 || *year > time.Now().UTC().Year() {
		writeError(w, http.StatusBadRequest, errors.New("not a year"))
		return false
	}
	return true
}

// deleteOldFiles: POST /api/old-files/delete {year, forever}: the files uploaded in that year or before, into the
// trash - forever: straight on out of it, which is what frees the disk.
func (s *Server) deleteOldFiles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Year    int  `json:"year"`
		Forever bool `json:"forever"`
	}
	if !oldYear(w, r, &req, &req.Year) {
		return
	}
	ok := s.startCleanup(w, "delete", req.Year, func(ctx context.Context) (int, string, error) {
		n, err := s.lib.DeleteOld(ctx, req.Year, s.cleanup.phase("delete"))
		if err != nil || !req.Forever {
			return n, "oldFiles.trashed", err
		}
		n, err = s.lib.PurgeOld(ctx, req.Year, s.cleanup.phase("purge"))
		return n, "trash.purged", err
	})
	if ok {
		writeJSON(w, http.StatusOK, map[string]bool{"started": true})
	}
}

// restoreOldFiles: POST /api/old-files/restore {year}: what a delete of that year left in the trash comes back.
func (s *Server) restoreOldFiles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Year int `json:"year"`
	}
	if !oldYear(w, r, &req, &req.Year) {
		return
	}
	ok := s.startCleanup(w, "restore", req.Year, func(ctx context.Context) (int, string, error) {
		n, err := s.lib.RestoreOld(ctx, req.Year, s.cleanup.phase("restore"))
		return n, "trash.restored", err
	})
	if ok {
		writeJSON(w, http.StatusOK, map[string]bool{"started": true})
	}
}

// purgeOldFiles: POST /api/old-files/purge {year}: what a delete of that year left in the trash goes for good; no year:
// the whole trash (a big trash: the page's own turns would take hours).
func (s *Server) purgeOldFiles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Year int `json:"year"`
	}
	if !decode(w, r, &req) {
		return
	}
	ok := s.startCleanup(w, "purge", req.Year, func(ctx context.Context) (int, string, error) {
		n, err := s.lib.PurgeOld(ctx, req.Year, s.cleanup.phase("purge"))
		return n, "trash.purged", err
	})
	if ok {
		writeJSON(w, http.StatusOK, map[string]bool{"started": true})
	}
}

// stopOldFiles: POST /api/old-files/stop: the work stops after its current batch; what went stays gone.
func (s *Server) stopOldFiles(w http.ResponseWriter, r *http.Request) {
	s.cleanup.mu.Lock()
	if s.cleanup.cancel != nil {
		s.cleanup.cancel()
	}
	s.cleanup.mu.Unlock()
	writeJSON(w, http.StatusOK, json.RawMessage("{}"))
}
