package push

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/i18n"
)

var en = i18n.For("en-US")

func TestCompose(t *testing.T) {
	one := Compose([]Event{{Kind: "new", Site: "safebooru.org", Query: "blue_sky", Subscription: true, New: 5}}, en)
	if len(one) != 1 || one[0].Title != "5 new files" || one[0].Body != "across 1 subscription: blue_sky" || one[0].URL != "/?new=1" {
		t.Fatalf("one: %+v", one)
	}
	var many []Event
	for i, n := range []int{1, 12, 3, 8, 2, 4, 6} {
		many = append(many, Event{Kind: "new", Site: "safebooru.org", Query: string(rune('a' + i)), New: n})
	}
	many = append(many,
		Event{Kind: "failed", Site: "examplebooru.test", Query: "", Subscription: true, Error: "examplebooru.test: 503 Service Unavailable"},
		Event{Kind: "done", Site: "safebooru.org", Query: "x", New: 1})
	ms := Compose(many, en)
	if len(ms) != 3 {
		t.Fatalf("%d messages", len(ms))
	}
	if ms[0].Title != "36 new files" || ms[0].Body != "across 7 subscriptions: b (12) · d (8) · g (6) · f (4) · c (3) and 2 more" {
		t.Fatalf("many new: %+v", ms[0])
	}
	if ms[1].Title != "Subscription check failed" || !strings.HasPrefix(ms[1].Body, "everything on examplebooru.test: examplebooru.test: 503") || ms[1].URL != "/downloads" {
		t.Fatalf("failed: %+v", ms[1])
	}
	if ms[2].Title != "Download finished" || ms[2].Body != "x on safebooru.org: 1 new file" {
		t.Fatalf("done: %+v", ms[2])
	}
	if len(Compose(nil, en)) != 0 {
		t.Fatal("nothing to say")
	}
}

func TestComposeBlacklisted(t *testing.T) {
	ms := Compose([]Event{{Kind: "blacklisted", Site: "safebooru.org", Tag: "red"}}, en)
	if len(ms) != 1 || ms[0].Title != "A file got a blacklisted tag" || ms[0].URL != "/tags/rules" || !strings.HasPrefix(ms[0].Body, "red - ") {
		t.Fatalf("one: %+v", ms)
	}
	ms = Compose([]Event{{Kind: "blacklisted", Tag: "red"}, {Kind: "blacklisted", Tag: "ai_*"}, {Kind: "blacklisted", Tag: "ai_*"}}, en)
	if len(ms) != 1 || ms[0].Title != "3 files got blacklisted tags" || !strings.HasPrefix(ms[0].Body, "ai_* (2) · red - ") {
		t.Fatalf("three: %+v", ms)
	}
}

func TestComposeHourAndFinished(t *testing.T) {
	// An hour of checks: one subscription checked twice counts once.
	ms := Compose([]Event{
		{Kind: "new", Site: "safebooru.org", Query: "blue_sky", New: 3},
		{Kind: "new", Site: "safebooru.org", Query: "red", New: 1},
		{Kind: "new", Site: "safebooru.org", Query: "blue_sky", New: 4},
	}, en)
	if len(ms) != 1 || ms[0].Title != "8 new files" || ms[0].Body != "across 2 subscriptions: blue_sky (7) · red (1)" {
		t.Fatalf("hour: %+v", ms)
	}
	// Finished: a new subscription's first check, and a one-off download - each their own.
	ms = Compose([]Event{
		{Kind: "done", Site: "safebooru.org", Query: "blue_sky", Subscription: true, First: true, New: 340},
		{Kind: "done", Site: "safebooru.org", Query: "red", New: 12},
	}, en)
	if len(ms) != 2 || ms[0].Title != "Subscription ready" || ms[0].Body != "blue_sky on safebooru.org: 340 new files" ||
		ms[1].Title != "Download finished" || ms[1].Body != "red on safebooru.org: 12 new files" {
		t.Fatalf("finished: %+v", ms)
	}
}

func TestNotifierSumsNewFilesHourly(t *testing.T) {
	var mu sync.Mutex
	var kinds []string
	n := &Notifier{wait: 10 * time.Millisecond, newWait: func() time.Duration { return 80 * time.Millisecond }}
	n.enabled = func(_ context.Context, kind string) bool {
		mu.Lock()
		kinds = append(kinds, kind)
		mu.Unlock()
		return false
	} // (nothing sent)
	n.Add(Event{Kind: "new", New: 1})
	n.Add(Event{Kind: "done", New: 2})
	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	if len(kinds) != 1 || kinds[0] != "done" {
		t.Fatalf("after the short wait: %v (only the finished download)", kinds)
	}
	mu.Unlock()
	n.Add(Event{Kind: "new", New: 3}) // still the same hour: joins the first
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(kinds) != 3 || kinds[1] != "new" || kinds[2] != "new" {
		t.Fatalf("after the hour: %v (both new-file events together)", kinds)
	}
}

func TestNotifierFlushOnStop(t *testing.T) {
	var kinds []string
	n := &Notifier{wait: time.Hour, newWait: func() time.Duration { return time.Hour }}
	n.enabled = func(_ context.Context, kind string) bool { kinds = append(kinds, kind); return false }
	n.Add(Event{Kind: "new", New: 1})
	n.Add(Event{Kind: "failed"})
	n.Flush()
	if len(kinds) != 2 {
		t.Fatalf("flushed: %v", kinds)
	}
}
