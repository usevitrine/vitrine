package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A site that keeps answering 503 is paused after PauseAfter failures in a row: nothing is sent meanwhile; once the
// wait is over one request tries (the others still wait); a failed try pauses it longer, an answer ends the pause.
// A 404 is the site answering (it doesn't count against it).
func TestSitePausesWhenItKeepsFailing(t *testing.T) {
	var down atomic.Bool
	var hits atomic.Int32
	down.Store(true)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case r.URL.Query().Get("q") == "missing":
			w.WriteHeader(http.StatusNotFound)
		case down.Load():
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.Write([]byte("ok"))
		}
	}))
	t.Cleanup(srv.Close)
	m, err := Parse([]byte("id: test\nengine: gelbooru-v02-html\nbase: " + srv.URL + "\npost_url: " + srv.URL + "/post/{id}\nlimits: {requests_per_sec: 1000}\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(m, srv, nil)
	now := time.Now()
	c.brk.now = func() time.Time { return now }
	var events []bool
	c.OnPause(func(paused bool, p Pause) { events = append(events, paused) })
	ctx := context.Background()
	get := func(q string) error {
		_, err := c.API(ctx, pageURL(srv, "&q="+q))
		return err
	}

	// A 404 between the failures: it answered - the count starts again.
	for range PauseAfter - 1 {
		get("")
	}
	get("missing")
	for range PauseAfter - 1 {
		get("")
	}
	if !c.Paused().Until.IsZero() {
		t.Fatal("paused although the site answered in between")
	}
	if err := get(""); errors.Is(err, ErrPaused) || c.Paused().Until.IsZero() {
		t.Fatalf("not paused after %d failures in a row: %v", PauseAfter, err)
	}
	if p := c.Paused(); p.Until != now.Add(pauseSteps[0]) || p.Reason == "" || len(events) != 1 || !events[0] {
		t.Fatalf("pause = %+v, events %v", p, events)
	}
	before := hits.Load()
	if err := get(""); !errors.Is(err, ErrPaused) || hits.Load() != before {
		t.Fatalf("a request went out while paused: %v", err)
	}

	// The wait is over: one try, which fails - longer.
	now = now.Add(pauseSteps[0])
	if err := get(""); errors.Is(err, ErrPaused) {
		t.Fatalf("the try didn't go out: %v", err)
	}
	if p := c.Paused(); p.Tries != 1 || p.Until != now.Add(pauseSteps[1]) || len(events) != 2 {
		t.Fatalf("after a failed try: %+v, events %v", p, events)
	}

	// Try now; the site is back: the pause ends.
	down.Store(false)
	c.TryNow()
	if err := get(""); err != nil {
		t.Fatal(err)
	}
	if !c.Paused().Until.IsZero() || len(events) != 3 || events[2] {
		t.Fatalf("still paused: %+v, events %v", c.Paused(), events)
	}
}
