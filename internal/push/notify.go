package push

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/i18n"
)

// Event is something to tell the user about (the downloader's events).
type Event struct {
	Kind         string // "new" (a subscription's check found files), "failed", "done" (a search or a first check finished), "blacklisted", "paused" or "back" (a site stopped / started answering)
	Site         string // e.g. "safebooru.org"
	Query        string
	Subscription bool
	New          int
	Error        string
	Tag          string // "blacklisted": the blacklist entry a library file now matches
	First        bool   // "done": a subscription's first check (its initial download)
	Pages        int    // a gallery site's: the pages brought in (New counts its galleries)
}

// Notifier gathers events and sends them as a few notifications (one per
// kind). Subscriptions' new files are summed up once an hour ("38 new files
// across 5 subscriptions"), however many checks found them; the rest (a
// download or a first check finished, a failure, a blacklisted tag) goes out
// within seconds, together with whatever else came meanwhile.
type Notifier struct {
	svc     *Service
	enabled func(ctx context.Context, kind string) bool
	wait    time.Duration        // the rest: gathered this long
	newWait func() time.Duration // subscriptions' new files: until the next full hour

	mu       sync.Mutex
	pending  []Event
	timer    *time.Timer
	newFiles []Event
	newTimer *time.Timer
}

// NewNotifier sends through svc; enabled says whether a kind is turned on.
func NewNotifier(svc *Service, enabled func(ctx context.Context, kind string) bool) *Notifier {
	return &Notifier{svc: svc, enabled: enabled, wait: 20 * time.Second, newWait: untilNextHour}
}

func untilNextHour() time.Duration {
	now := time.Now()
	return now.Truncate(time.Hour).Add(time.Hour).Sub(now)
}

// Add queues an event: subscriptions' new files until the next full hour, the
// rest with the others that arrive within the wait.
func (n *Notifier) Add(e Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if e.Kind == "new" {
		n.newFiles = append(n.newFiles, e)
		if n.newTimer == nil {
			n.newTimer = time.AfterFunc(n.newWait(), n.flushNew)
		}
		return
	}
	n.pending = append(n.pending, e)
	if n.timer == nil {
		n.timer = time.AfterFunc(n.wait, n.flush)
	}
}

// Flush sends whatever is waiting now (Vitrine is stopping: the hour's new
// files aren't lost with it).
func (n *Notifier) Flush() {
	n.mu.Lock()
	events := append(n.pending, n.newFiles...)
	if n.timer != nil {
		n.timer.Stop()
	}
	if n.newTimer != nil {
		n.newTimer.Stop()
	}
	n.pending, n.timer, n.newFiles, n.newTimer = nil, nil, nil, nil
	n.mu.Unlock()
	n.send(events)
}

func (n *Notifier) flushNew() {
	n.mu.Lock()
	events := n.newFiles
	n.newFiles, n.newTimer = nil, nil
	n.mu.Unlock()
	n.send(events)
}

func (n *Notifier) flush() {
	n.mu.Lock()
	events := n.pending
	n.pending, n.timer = nil, nil
	n.mu.Unlock()
	n.send(events)
}

func (n *Notifier) send(events []Event) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var on []Event
	for _, e := range events {
		if n.enabled(ctx, e.Kind) {
			on = append(on, e)
		}
	}
	if len(on) == 0 {
		return
	}
	// Each device gets them in its own language.
	langs, err := n.svc.languages(ctx)
	if err != nil {
		n.svc.log.Warn("push notifications not sent", "err", err)
		return
	}
	for _, lang := range langs {
		for _, m := range Compose(on, i18n.For(lang)) {
			if _, err := n.svc.sendLang(ctx, lang, m); err != nil {
				n.svc.log.Warn("push notification not delivered everywhere", "err", err)
			}
		}
	}
}

// Compose turns events into notifications in a language: one per kind. They say what happened and how many - never
// a site, a tag, a search or an error's text (the user: nothing "that could be explicit"; a push passes through the
// browser maker's push service and shows on a locked screen). Tapping one opens the page that has the names.
func Compose(events []Event, l i18n.Lang) []Message {
	by := map[string][]Event{}
	for _, e := range events {
		by[e.Kind] = append(by[e.Kind], e)
	}
	var out []Message
	if es := by["new"]; len(es) > 0 {
		// An hour's checks: how many new files, from how many subscriptions (one may have been checked more than once).
		subs := map[string]bool{}
		for _, e := range es {
			subs[e.Site+"\x00"+e.Query] = true
		}
		out = append(out, Message{Title: newWhat(es, l), Body: l.T("push.fromSubs", "n", len(subs)), URL: "/?new=1"}) // the library, new files first
	}
	if es := by["failed"]; len(es) > 0 {
		m := Message{URL: "/downloads", Body: l.T("push.seeWhy")}
		switch {
		case len(es) > 1:
			m.Title = l.T("push.downloadsFailed", "n", len(es))
		case es[0].Subscription:
			m.Title = l.T("push.checkFailed")
		default:
			m.Title = l.T("push.downloadStopped")
		}
		out = append(out, m)
	}
	// A site that kept failing to answer (its downloads wait), or answers again: one notification per site.
	for range by["paused"] {
		out = append(out, Message{URL: "/downloads", Title: l.T("push.sitePaused"), Body: l.T("push.sitePausedBody")})
	}
	for range by["back"] {
		out = append(out, Message{URL: "/downloads", Title: l.T("push.siteBack"), Body: l.T("push.siteBackBody")})
	}
	if es := by["blacklisted"]; len(es) > 0 {
		// Files already in the library that got a blacklisted tag: the blacklist (Tags > Tag rules) shows them.
		out = append(out, Message{URL: "/tags/rules", Title: l.T("push.blacklisted", "n", len(es)), Body: l.T("push.blacklistedBody")})
	}
	// Finished: one-off downloads, and new subscriptions' first checks (their initial downloads), each their own
	// notification - how many, and how many new files in all.
	var oneOff, first []Event
	for _, e := range by["done"] {
		if e.First {
			first = append(first, e)
		} else {
			oneOff = append(oneOff, e)
		}
	}
	appendDone := func(es []Event, title func(n int) string) {
		if len(es) == 0 {
			return
		}
		out = append(out, Message{URL: "/downloads", Title: title(len(es)), Body: newWhat(es, l)})
	}
	appendDone(first, func(n int) string { return l.T("push.subsReady", "n", n) })
	appendDone(oneOff, func(n int) string { return l.T("push.downloadsDone", "n", n) })
	return out
}

// newWhat says what events brought: new files, and a gallery site's new pages (its galleries are comics, counted by
// page - "1 new file" for a gallery of 40 pages said nothing).
func newWhat(es []Event, l i18n.Lang) string {
	files, pages := 0, 0
	for _, e := range es {
		if e.Pages > 0 {
			pages += e.Pages
		} else {
			files += e.New
		}
	}
	switch {
	case pages > 0 && files > 0:
		return l.T("push.newFiles", "n", files) + ", " + l.T("push.newPages", "n", pages)
	case pages > 0:
		return l.T("push.newPages", "n", pages)
	}
	return l.T("push.newFiles", "n", files)
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
