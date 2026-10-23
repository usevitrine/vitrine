package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/usevitrine/vitrine/internal/i18n"
	"github.com/usevitrine/vitrine/internal/library"
)

// The sections, Media and Comics (library/sections.go): what each holds, and switching one off - its subscriptions
// and downloads removed (the user's choice), everything in it deleted for good if asked (in the background: a big
// library takes a while; a notice says when it's done).

type sectionInfo struct {
	library.SectionSize
	Subscriptions int `json:"subscriptions"`
	Downloads     int `json:"downloads"`
}

// sections: GET /api/sections -> {media, comics}: each one's files (or comics), size, subscriptions and downloads.
func (s *Server) sections(w http.ResponseWriter, r *http.Request) {
	out := map[string]sectionInfo{}
	for _, name := range []string{"media", "comics"} {
		var info sectionInfo
		var err error
		if name == "media" {
			info.SectionSize, err = s.lib.MediaSize(r.Context())
		} else {
			info.SectionSize, err = s.lib.ComicsSize(r.Context())
		}
		if err == nil && s.dl != nil {
			info.Subscriptions, info.Downloads, err = s.dl.SectionUse(r.Context(), name == "comics")
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		out[name] = info
	}
	writeJSON(w, http.StatusOK, out)
}

// sectionOff: POST /api/sections/{section}/off {delete} -> {subscriptions, downloads, deleting}: Media or Comics
// switched off (refused when the other is off), its subscriptions and downloads removed; delete: everything in it
// deleted for good, in the background.
func (s *Server) sectionOff(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("section")
	if name != "media" && name != "comics" {
		writeError(w, http.StatusNotFound, errors.New("no such section (media or comics)"))
		return
	}
	var req struct {
		Delete bool `json:"delete"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.lib.SetSettings(r.Context(), map[string]json.RawMessage{name + "Off": json.RawMessage("true")}); err != nil {
		libError(w, err)
		return
	}
	comics := name == "comics"
	var subs, downloads int
	if s.dl != nil {
		var err error
		if subs, downloads, err = s.dl.RemoveSection(r.Context(), comics); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.log.Info("section switched off", "section", name, "subscriptions", subs, "downloads", downloads, "delete", req.Delete)
	if req.Delete {
		go func() {
			ctx := context.WithoutCancel(r.Context())
			start := time.Now()
			var n int
			var err error
			if comics {
				n, err = s.lib.DeleteAllComics(ctx)
			} else {
				n, err = s.lib.DeleteAllMedia(ctx)
			}
			tr := i18n.For("")
			if err != nil {
				s.log.Error("deleting a section", "section", name, "err", err)
				s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "app", Text: tr.T("appSections.deleteFailed", "error", err.Error()), Tone: "error", Category: "deleted"})
				return
			}
			s.log.Info("section deleted", "section", name, "items", n, "took", time.Since(start).Round(time.Second))
			key := "appSections.deletedMedia"
			if comics {
				key = "appSections.deletedComics"
			}
			s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "app", Text: tr.T(key, "n", n), Tone: "removed", Category: "deleted"})
		}()
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subs, "downloads": downloads, "deleting": req.Delete})
}
