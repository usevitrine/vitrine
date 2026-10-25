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

// Push notifications say what happened and how many - never a site, a tag, a search or an error (the user).
func TestCompose(t *testing.T) {
	one := Compose([]Event{{Kind: "new", Site: "safebooru.org", Query: "blue_sky", Subscription: true, New: 5}}, en)
	if len(one) != 1 || one[0].Title != "5 new files" || one[0].Body != "from 1 subscription" || one[0].URL != "/?new=1" {
		t.Fatalf("one: %+v", one)
	}
	var many []Event
	for i, n := range []int{1, 12, 3, 8, 2, 4, 6} {
		many = append(many, Event{Kind: "new", Site: "safebooru.org", Query: string(rune('a' + i)), New: n})
	}
	many = append(many,
		Event{Kind: "failed", Site: "examplebooru.test", Query: "", Subscription: true, Error: "examplebooru.test: 503 Service Unavailable"},
		Event{Kind: "done", Site: "safebooru.org", Query: "x", New: 1},
		Event{Kind: "paused", Site: "examplebooru.test", Error: "examplebooru.test: timeout"},
		Event{Kind: "back", Site: "examplebooru.test"})
	ms := Compose(many, en)
	if len(ms) != 5 {
		t.Fatalf("%d messages", len(ms))
	}
	if ms[0].Title != "36 new files" || ms[0].Body != "from 7 subscriptions" {
		t.Fatalf("many new: %+v", ms[0])
	}
	if ms[1].Title != "A subscription check failed" || ms[1].Body != "Open Vitrine to see why" || ms[1].URL != "/downloads" {
		t.Fatalf("failed: %+v", ms[1])
	}
	if ms[2].Title != "A site isn’t answering" || ms[3].Title != "A site answers again" {
		t.Fatalf("paused, back: %+v %+v", ms[2], ms[3])
	}
	if ms[4].Title != "Download finished" || ms[4].Body != "1 new file" {
		t.Fatalf("done: %+v", ms[4])
	}
	for _, m := range ms {
		for _, secret := range []string{"safebooru", "examplebooru", "503", "timeout"} {
			if strings.Contains(m.Title+m.Body, secret) {
				t.Fatalf("%q says %q: %+v", m.Title, secret, m)
			}
		}
	}
	if len(Compose(nil, en)) != 0 {
		t.Fatal("nothing to say")
	}
}

// A gallery site's downloads count pages, not "1 new file" for a gallery of many pages.
func TestComposeGalleryPages(t *testing.T) {
	ms := Compose([]Event{{Kind: "done", Site: "galleries.example.test", Query: "x", New: 1, Pages: 40}}, en)
	if len(ms) != 1 || ms[0].Body != "40 new pages" {
		t.Fatalf("one gallery: %+v", ms)
	}
	ms = Compose([]Event{
		{Kind: "new", Site: "galleries.example.test", Query: "a", Subscription: true, New: 2, Pages: 75},
		{Kind: "new", Site: "safebooru.org", Query: "b", Subscription: true, New: 5},
	}, en)
	if len(ms) != 1 || ms[0].Title != "5 new files, 75 new pages" {
		t.Fatalf("files and pages: %+v", ms)
	}
}

func TestComposeBlacklisted(t *testing.T) {
	ms := Compose([]Event{{Kind: "blacklisted", Site: "safebooru.org", Tag: "red"}}, en)
	if len(ms) != 1 || ms[0].Title != "A file got a blacklisted tag" || ms[0].URL != "/tags/rules" || strings.Contains(ms[0].Body, "red") {
		t.Fatalf("one: %+v", ms)
	}
	ms = Compose([]Event{{Kind: "blacklisted", Tag: "red"}, {Kind: "blacklisted", Tag: "ai_*"}, {Kind: "blacklisted", Tag: "ai_*"}}, en)
	if len(ms) != 1 || ms[0].Title != "3 files got blacklisted tags" || ms[0].Body != "They were in the library already - see Tag rules" {
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
	if len(ms) != 1 || ms[0].Title != "8 new files" || ms[0].Body != "from 2 subscriptions" {
		t.Fatalf("hour: %+v", ms)
	}
	// Finished: a new subscription's first check, and one-off downloads - each kind its own, the new files summed.
	ms = Compose([]Event{
		{Kind: "done", Site: "safebooru.org", Query: "blue_sky", Subscription: true, First: true, New: 340},
		{Kind: "done", Site: "safebooru.org", Query: "red", New: 12},
		{Kind: "done", Site: "safebooru.org", Query: "green", New: 40},
	}, en)
	if len(ms) != 2 || ms[0].Title != "Subscription ready" || ms[0].Body != "340 new files" ||
		ms[1].Title != "2 downloads finished" || ms[1].Body != "52 new files" {
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
