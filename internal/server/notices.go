package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/download"
	"github.com/usevitrine/vitrine/internal/library"
)

// Notices for the web client's bell: what the downloader wanted to tell the user (new files from a subscription,
// a download finished or failed, a file that got a blacklisted tag, a site that stopped or started answering), the
// last ones kept in meta.db (over restarts) and here. Open pages get each as it comes (the event stream: "notice");
// a page opened later catches up (GET /api/notices). The page words them in its own language and keeps what was
// read. Boot names the run of IDs (it stays the same over restarts: a page tells notices it has seen by it and ID).
// Read and cleared are the same on every device (kept in meta.db: how far, by ID); open pages hear when they change
// (the event stream: "notices-state").

type notices struct {
	lib  *library.Library
	log  *slog.Logger
	mu   sync.Mutex
	boot string
	list []library.Notice
	next int64 // (only when meta.db can't be written: numbered on from the last)
	// read and cleared: how far (notice IDs); marks goes up with each change (the event stream sends it then).
	read, cleared, marks int64
}

func newNotices(lib *library.Library, log *slog.Logger) *notices {
	n := &notices{lib: lib, log: log, boot: "local", next: 1}
	if lib == nil {
		return n
	}
	ctx := context.Background()
	if boot, err := lib.NoticesBoot(ctx); err == nil {
		n.boot = boot
	}
	if read, cleared, err := lib.NoticeMarks(ctx); err == nil {
		n.read, n.cleared = read, cleared
	}
	if list, err := lib.Notices(ctx); err == nil {
		n.list = list
		if len(list) > 0 {
			n.next = list[len(list)-1].ID + 1
		}
	} else if log != nil {
		log.Warn("reading the notices", "err", err)
	}
	return n
}

func (n *notices) add(e download.Event) {
	n.put(library.Notice{At: time.Now().UnixMilli(), Kind: e.Kind, Site: e.Site, Query: e.Query,
		Subscription: e.Subscription, New: e.New, Error: e.Error, Tag: e.Tag, First: e.First, Pages: e.Pages})
}

func (n *notices) put(x library.Notice) {
	n.mu.Lock()
	defer n.mu.Unlock()
	x.ID = n.next
	if n.lib != nil {
		id, err := n.lib.AddNotice(context.Background(), x)
		if err == nil && id > 0 {
			x.ID = id
		} else if err != nil && n.log != nil {
			n.log.Warn("keeping a notice", "err", err)
		}
	}
	n.next = x.ID + 1
	n.list = append(n.list, x)
	if len(n.list) > library.NoticesKept {
		n.list = n.list[len(n.list)-library.NoticesKept:]
	}
}

// has tells whether a notice of a kind about a query is kept already (cleared or not) - an update's, once per version.
func (n *notices) has(kind, query string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, x := range n.list {
		if x.Kind == kind && x.Query == query {
			return true
		}
	}
	return false
}

// after returns the notices after an ID (0: all of them) - not the cleared ones.
func (n *notices) after(id int64) []library.Notice {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := []library.Notice{}
	for _, x := range n.list {
		if x.ID > max(id, n.cleared) {
			out = append(out, x)
		}
	}
	return out
}

// state is how far they're read and cleared, and the change count.
func (n *notices) state() (read, cleared, marks int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.read, n.cleared, n.marks
}

// mark reads them (and clears them, with clear) up to an ID - never back.
func (n *notices) mark(upTo int64, clear bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	upTo = min(upTo, n.next-1)
	read, cleared := max(n.read, upTo), n.cleared
	if clear {
		cleared = max(cleared, upTo)
	}
	if read == n.read && cleared == n.cleared {
		return
	}
	n.read, n.cleared = read, cleared
	n.marks++
	if n.lib != nil {
		if err := n.lib.SetNoticeMarks(context.Background(), read, cleared); err != nil && n.log != nil {
			n.log.Warn("keeping the notices' read and cleared", "err", err)
		}
	}
}

func (n *notices) last() int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.next - 1
}

// Notice records one of the downloader's events for the bell.
func (s *Server) Notice(e download.Event) { s.notices.add(e) }

// addNotice: POST /api/notices {text, tone, category} - something done in the app that's set to go to the bell
// (Settings > Notifications): in every device's bell -> {id}.
func (s *Server) addNotice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text     string `json:"text"`
		Tone     string `json:"tone"`
		Category string `json:"category"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || len(req.Text) > 500 || len(req.Category) > 40 || !slices.Contains([]string{"done", "info", "removed", "error"}, req.Tone) {
		writeError(w, http.StatusBadRequest, errors.New("a notice needs its words (at most 500 characters), a tone and a kind"))
		return
	}
	s.notices.put(library.Notice{At: time.Now().UnixMilli(), Kind: "app", Text: req.Text, Tone: req.Tone, Category: req.Category})
	writeJSON(w, http.StatusOK, map[string]int64{"id": s.notices.last()})
}

// listNotices: GET /api/notices -> {boot, notices, read} (not the cleared ones; read: how far, by ID).
func (s *Server) listNotices(w http.ResponseWriter, r *http.Request) {
	read, _, _ := s.notices.state()
	writeJSON(w, http.StatusOK, map[string]any{"boot": s.notices.boot, "notices": s.notices.after(0), "read": read})
}

// markNotices: POST /api/notices/read and /api/notices/clear {up_to} - for every device -> {read, cleared}.
func (s *Server) markNotices(clear bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UpTo int64 `json:"up_to"`
		}
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.notices.mark(req.UpTo, clear)
		read, cleared, _ := s.notices.state()
		writeJSON(w, http.StatusOK, map[string]int64{"read": read, "cleared": cleared})
	}
}
