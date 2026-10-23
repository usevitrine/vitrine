package push

import (
	"cmp"
	"context"
	"fmt"
	"slices"
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

// Compose turns events into notifications in a language: one per kind.
func Compose(events []Event, l i18n.Lang) []Message {
	by := map[string][]Event{}
	for _, e := range events {
		by[e.Kind] = append(by[e.Kind], e)
	}
	query := func(e Event) string {
		if e.Query == "" {
			return l.T("push.everything")
		}
		return clip(e.Query, 60)
	}
	what := func(e Event) string { return l.T("push.on", "query", query(e), "site", e.Site) }
	// list joins up to five items: "a (3) · b (2) · c (1) and 2 more".
	list := func(es []Event, f func(Event) string) string {
		var parts []string
		for i, e := range es {
			if i == 5 {
				return strings.Join(parts, " · ") + " " + l.T("push.andMore", "n", len(es)-5)
			}
			parts = append(parts, f(e))
		}
		return strings.Join(parts, " · ")
	}
	var out []Message
	if es := by["new"]; len(es) > 0 {
		// An hour's checks: one line per subscription (it may have been checked
		// more than once), the most files first.
		var subs []Event
		at := map[string]int{}
		total := 0
		for _, e := range es {
			total += e.New
			k := e.Site + "\x00" + e.Query
			if i, ok := at[k]; ok {
				subs[i].New += e.New
				continue
			}
			at[k] = len(subs)
			subs = append(subs, e)
		}
		// Always "across N subscriptions: ..." (the user: "1 new file - across 1 subscription"), one or many; a single
		// subscription without its count (the title says it).
		m := Message{Title: l.T("push.newFiles", "n", total), URL: "/?new=1"} // the library, new files first
		slices.SortStableFunc(subs, func(a, b Event) int { return cmp.Compare(b.New, a.New) })
		m.Body = l.T("push.across", "n", len(subs), "list", list(subs, func(e Event) string {
			if len(subs) == 1 {
				return query(e)
			}
			return l.T("push.count", "query", query(e), "count", e.New)
		}))
		out = append(out, m)
	}
	if es := by["failed"]; len(es) > 0 {
		m := Message{URL: "/downloads"}
		if len(es) == 1 {
			e := es[0]
			m.Title = l.T("push.downloadStopped")
			if e.Subscription {
				m.Title = l.T("push.checkFailed")
			}
			m.Body = what(e) + ": " + clip(e.Error, 140)
		} else {
			m.Title = l.T("push.downloadsFailed", "n", len(es))
			m.Body = list(es, query)
		}
		out = append(out, m)
	}
	// A site that kept failing to answer (its downloads wait), or answers again: one notification per site.
	for _, e := range by["paused"] {
		out = append(out, Message{URL: "/downloads", Title: l.T("push.sitePaused", "site", e.Site), Body: l.T("push.sitePausedBody", "error", clip(e.Error, 140))})
	}
	for _, e := range by["back"] {
		out = append(out, Message{URL: "/downloads", Title: l.T("push.siteBack", "site", e.Site), Body: l.T("push.siteBackBody")})
	}
	if es := by["blacklisted"]; len(es) > 0 {
		// Files already in the library that got a blacklisted tag: the blacklist
		// (Tags > Tag rules) shows them, with Show them / Delete them.
		m := Message{URL: "/tags/rules", Title: l.T("push.blacklisted", "n", len(es))}
		count := map[string]int{}
		var tags []string
		for _, e := range es {
			if count[e.Tag] == 0 {
				tags = append(tags, e.Tag)
			}
			count[e.Tag]++
		}
		slices.SortStableFunc(tags, func(a, b string) int { return cmp.Compare(count[b], count[a]) })
		var parts []string
		for i, t := range tags {
			if i == 5 {
				parts = append(parts, l.T("push.andMore", "n", len(tags)-5))
				break
			}
			if count[t] > 1 {
				t = fmt.Sprintf("%s (%d)", t, count[t])
			}
			parts = append(parts, t)
		}
		m.Body = l.T("push.blacklistedBody", "tags", strings.Join(parts, " · "))
		out = append(out, m)
	}
	// Finished: one-off downloads, and new subscriptions' first checks (their
	// initial downloads), each their own notification.
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
		m := Message{URL: "/downloads", Title: title(len(es))}
		if len(es) == 1 {
			m.Body = what(es[0]) + ": " + l.T("push.newFiles", "n", es[0].New)
		} else {
			m.Body = list(es, func(e Event) string { return l.T("push.countNew", "query", query(e), "count", e.New) })
		}
		out = append(out, m)
	}
	appendDone(first, func(n int) string { return l.T("push.subsReady", "n", n) })
	appendDone(oneOff, func(n int) string { return l.T("push.downloadsDone", "n", n) })
	return out
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
