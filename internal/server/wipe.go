package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/i18n"
	"github.com/usevitrine/vitrine/internal/library"
)

// Wiping a removed site (2026-10-26, the user: "if someone removes a site I want it to prompt them if they want to
// completely wipe their server of any hint that they ever had it enabled"): after its plugin goes, the library
// forgets it (library.WipeSite), and so do the log, the site icons, the resized copies of its files, the credentials
// file, the bell, and - when asked - the older backups (a fresh one made after). The databases are compacted last, so
// nothing of it is left in their free space. The notice when it's done doesn't name it.

// siteTraces: GET /api/plugins/{id}/traces -> what wiping the site would remove (library.SiteTraces) and how many
// backups there are.
func (s *Server) siteTraces(w http.ResponseWriter, r *http.Request) {
	t, err := s.lib.SiteTraces(r.Context(), r.PathValue("id"))
	if err != nil {
		libError(w, err)
		return
	}
	backups := 0
	if s.backups != nil {
		if list, err := s.backups.m.List(); err == nil {
			backups = len(list)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"traces": t, "any": t.Any(), "backups": backups})
}

// wipeSite removes every trace of a site whose plugin was just removed (in the background: a big site takes a
// while; a notice says when it's done). backups: the older backups go too. False: another wipe is still going.
func (s *Server) wipeSite(res download.Removed, backups bool) bool {
	if !s.wiping.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer s.wiping.Store(false)
		ctx := context.Background()
		start := time.Now()
		tr := i18n.For("")
		w, err := s.lib.WipeSite(ctx, res.ID)
		if err != nil {
			s.log.Error("wiping a removed site", "err", err)
			s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "app", Text: tr.T("sites.wipeFailed", "error", err.Error()), Tone: "error", Category: "deleted"})
			return
		}
		if s.rend != nil {
			keys := make([]string, len(w.Hashes))
			for i, h := range w.Hashes {
				keys[i] = h.String()
			}
			s.rend.Remove(keys...)
		}
		var errs []error
		if s.dl != nil {
			errs = append(errs, s.dl.ForgetCredentials(res.ID))
		}
		if s.icons != nil && res.Site != "" {
			s.icons.Forget(res.Site, "www."+res.Site)
		}
		s.notices.forgetSite(res.ID)
		if s.logs != nil {
			words := []string{res.ID, res.Site}
			if len(res.Name) >= 4 { // (a short name would match unrelated lines)
				words = append(words, res.Name)
			}
			s.logs.Forget(words...)
		}
		if backups && s.backups != nil {
			if list, err := s.backups.m.List(); err == nil {
				for _, b := range list {
					errs = append(errs, s.backups.m.Delete(b.Name))
				}
			}
		}
		errs = append(errs, s.lib.Compact(ctx))
		if backups && s.backups != nil {
			_, keep := s.BackupSettings(ctx)
			_, err := s.backups.m.Create(ctx, keep)
			errs = append(errs, err)
		}
		if err := errors.Join(errs...); err != nil {
			s.log.Warn("wiping a removed site: some of it is left", "err", err)
		}
		s.log.Info("removed site wiped", "files", w.Files, "shared", w.Shared, "comics", w.Comics, "records", w.Records,
			"took", time.Since(start).Round(time.Second))
		s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "app", Text: tr.T("sites.wiped", "n", w.Files), Tone: "removed", Category: "deleted"})
		if s.dl != nil {
			s.dl.Changed()
		}
	}()
	return true
}
