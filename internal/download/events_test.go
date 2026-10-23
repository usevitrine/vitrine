package download

import (
	"context"
	"testing"
	"time"
)

func TestEvents(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	advance := e.clock()
	e.dl.retryWaits = []time.Duration{time.Minute}
	e.dl.Site("fake").Client.SetTooManyWait(time.Millisecond)
	var got []Event
	e.dl.OnEvent = func(ev Event) { got = append(got, ev) }
	take := func() []Event { g := got; got = nil; return g }
	setDown := func(v bool) { e.fb.mu.Lock(); e.fb.broken = v; e.fb.mu.Unlock() } // (answers nonsense: a real failure)

	// A subscription's first check: its initial download finished (done, First).
	id, _ := e.dl.Subscribe(ctx, "fake", "common", time.Hour, 0)
	e.runDue(t)
	if ev := take(); len(ev) != 1 || ev[0].Kind != EventDone || !ev[0].First || ev[0].New != 5 || !ev[0].Subscription ||
		ev[0].Query != "common" || ev[0].Site != e.dl.Site("fake").Client.Manifest().Site {
		t.Fatalf("first check: %+v", ev)
	}
	// A later check with new files: new files; then one without: nothing.
	e.fb.upload(2)
	advance(time.Hour)
	e.runDue(t)
	if ev := take(); len(ev) != 1 || ev[0].Kind != EventNewFiles || ev[0].New != 2 || ev[0].First {
		t.Fatalf("a check with new files: %+v", ev)
	}
	advance(time.Hour)
	e.runDue(t)
	if ev := take(); len(ev) != 0 {
		t.Fatalf("a check without new files: %+v", ev)
	}
	// Failing checks: told once, not every hour.
	setDown(true)
	for range 2 {
		advance(time.Hour)
		e.runDue(t)
	}
	if ev := take(); len(ev) != 1 || ev[0].Kind != EventFailed || ev[0].Error == "" {
		t.Fatalf("failing checks: %+v", ev)
	}
	e.dl.Unsubscribe(ctx, id)

	// A search that fails: told only when it stops trying by itself.
	sid, _ := e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if ev := take(); len(ev) != 0 {
		t.Fatalf("first failure (will retry): %+v", ev)
	}
	advance(time.Minute)
	e.dl.retryFailed(ctx)
	e.dl.RunQueue(ctx, "fake")
	if ev := take(); len(ev) != 1 || ev[0].Kind != EventFailed || ev[0].Subscription {
		t.Fatalf("gave up: %+v", ev)
	}
	// Retried by hand, the site back: finished.
	setDown(false)
	e.dl.Resume(ctx, sid)
	e.dl.RunQueue(ctx, "fake")
	if ev := take(); len(ev) != 1 || ev[0].Kind != EventDone {
		t.Fatalf("done: %+v", ev)
	}
}

// A file already in the library that a site retags with a blacklisted tag is
// told about once (not on every later rerun); new downloads never are (they're skipped).
func TestBlacklistedEvent(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	var got []Event
	e.dl.OnEvent = func(ev Event) {
		if ev.Kind == EventBlacklisted {
			got = append(got, ev)
		}
	}
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if _, err := e.lib.SetBlacklist(ctx, []string{"retag*"}); err != nil {
		t.Fatal(err)
	}
	e.dl.Add(ctx, "fake", "common", 0) // the same posts, tags unchanged
	e.dl.RunQueue(ctx, "fake")
	if len(got) != 0 {
		t.Fatalf("no blacklisted tags yet: %+v", got)
	}
	e.fb.mu.Lock()
	e.fb.extra = " retagged"
	e.fb.mu.Unlock()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if len(got) != 3 || got[0].Tag != "retag*" || got[0].Site != e.dl.Site("fake").Client.Manifest().Site {
		t.Fatalf("retagged: %+v", got)
	}
	got = nil
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if len(got) != 0 {
		t.Fatalf("told again: %+v", got)
	}
}

// A site that stops answering is paused once (told once - not on every try), its check waits instead of failing;
// it answering again is told once too, and the check carries on.
func TestSitePausedEvents(t *testing.T) {
	e := newEnv(t, 5)
	ctx := context.Background()
	advance := e.clock()
	var got []Event
	e.dl.OnEvent = func(ev Event) { got = append(got, ev) }
	take := func() []Event { g := got; got = nil; return g }
	setDown := func(v bool) { e.fb.mu.Lock(); e.fb.down = v; e.fb.mu.Unlock() }
	id, _ := e.dl.Subscribe(ctx, "fake", "common", time.Hour, 0)
	e.runDue(t)
	take()

	e.fb.upload(2)
	setDown(true)
	advance(time.Hour)
	e.runDue(t)
	if ev := take(); len(ev) != 1 || ev[0].Kind != EventPaused || ev[0].Error == "" || ev[0].Site != "fake.example.test" {
		t.Fatalf("paused: %+v", ev)
	}
	if s := e.sub(t, id); s.Check == nil || s.Check.State != StateRunning || s.Error != "" {
		t.Fatalf("the check should wait: %+v", s)
	}
	// Its tries fail: told nothing more.
	for range 3 {
		advance(time.Hour)
		e.runDue(t)
	}
	if ev := take(); len(ev) != 0 {
		t.Fatalf("tries told: %+v", ev)
	}
	// Back: told once, the check finishes with the new files.
	setDown(false)
	advance(time.Hour)
	e.runDue(t)
	ev := take()
	if len(ev) != 2 || ev[0].Kind != EventBack || ev[1].Kind != EventNewFiles || ev[1].New != 2 {
		t.Fatalf("back: %+v", ev)
	}
}
