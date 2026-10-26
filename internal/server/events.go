package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GET /api/events: a Server-Sent Events stream for open pages.
//   event: downloads  - the download queue or subscriptions changed (re-read /api/downloads)
//   event: progress   - the files downloading now: [{search, post, read, total}], twice a
//                       second while any are (and one empty list when they stop)
//   event: notice     - the downloader's new events for the bell: [{id, at, kind, ...}] (see notices.go)
//   event: notices-state - how far the bell's notices are read and cleared changed (on another device), or some
//                          were dropped (forgets went up: a wiped site's): {read, cleared, forgets}
// Changes are sent at most twice a second; a comment every 20 s keeps proxies
// from closing the connection. The browser's EventSource reconnects by itself.

const eventTick = 500 * time.Millisecond

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // proxies: don't buffer
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	if err := rc.Flush(); err != nil {
		return
	}
	var changes <-chan struct{}
	if s.dl != nil {
		ch, stop := s.dl.Watch()
		defer stop()
		changes = ch
	}
	tick := time.NewTicker(eventTick)
	defer tick.Stop()
	pending, wasActive, quiet := false, false, 0
	lastNotice := s.notices.last() // (the page read the earlier ones itself)
	_, _, lastMarks := s.notices.state()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			pending = true
			continue
		case <-tick.C:
		}
		wrote := false
		if pending {
			fmt.Fprint(w, "event: downloads\ndata: {}\n\n")
			pending, wrote = false, true
		}
		if fresh := s.notices.after(lastNotice); len(fresh) > 0 {
			b, _ := json.Marshal(map[string]any{"boot": s.notices.boot, "notices": fresh})
			fmt.Fprintf(w, "event: notice\ndata: %s\n\n", b)
			lastNotice = fresh[len(fresh)-1].ID
			wrote = true
		}
		if read, cleared, marks := s.notices.state(); marks != lastMarks {
			lastMarks = marks
			b, _ := json.Marshal(map[string]int64{"read": read, "cleared": cleared, "forgets": s.notices.forgot()})
			fmt.Fprintf(w, "event: notices-state\ndata: %s\n\n", b)
			wrote = true
		}
		if s.dl != nil {
			p := s.dl.Progress()
			if len(p) > 0 || wasActive {
				b, _ := json.Marshal(p)
				fmt.Fprintf(w, "event: progress\ndata: %s\n\n", b)
				wrote = true
			}
			wasActive = len(p) > 0
		}
		if quiet++; !wrote && quiet >= int(20*time.Second/eventTick) {
			fmt.Fprint(w, ": ping\n\n")
			wrote = true
		}
		if wrote {
			quiet = 0
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// signal wraps a handler that changes the queue: open pages hear about it.
func (s *Server) signal(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h(w, r)
		if s.dl != nil {
			s.dl.Changed()
		}
	}
}
