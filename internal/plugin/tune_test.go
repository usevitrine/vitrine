package plugin

import (
	"context"
	"testing"
	"time"
)

// Self-tuning connections: more files at once are kept while they bring a tenth more in total, a try that doesn't is
// undone (and not tried again for a while), a 429 goes back to the plugin's number - and the slots that downloads
// wait for follow.
func TestTuner(t *testing.T) {
	var m Manifest
	m.Site = "example.test"
	m.Limits.Connections, m.Limits.MaxConnections = 3, 6
	c := NewClient(m, nil)
	tu := c.tune
	clock := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	tu.now = func() time.Time { return clock }
	free := func() int { return cap(c.files) - len(c.files) } // slots a download can take
	if c.Connections() != 3 || free() != 3 {
		t.Fatalf("to begin with: %d files at once, %d slots free", c.Connections(), free())
	}

	// The total speed for each number of connections: rises up to 5 (a slow route), then flat.
	speed := map[int]float64{3: 30, 4: 40, 5: 48, 6: 50}
	minute := func() { tu.judge(speed[tu.Target()]); clock = clock.Add(time.Minute) }
	for range 12 {
		minute()
	}
	// 4 and 5 paid (+33 %, +20 %); 6 didn't (+4 %): back to 5.
	if c.Connections() != 5 {
		t.Fatalf("settled on %d, want 5", c.Connections())
	}
	time.Sleep(10 * time.Millisecond) // (a slot taken back once free: no download holds one here)
	if free() != 5 {
		t.Fatalf("%d slots free at 5 files at once", free())
	}
	// Not tried again for half an hour.
	for range 10 {
		minute()
	}
	if c.Connections() != 5 {
		t.Fatalf("tried more again within the half hour: %d", c.Connections())
	}

	// A 429: the plugin's number at once.
	c.backOff("")
	time.Sleep(10 * time.Millisecond)
	if c.Connections() != 3 || free() != 3 {
		t.Fatalf("after a 429: %d files at once, %d slots free", c.Connections(), free())
	}

	// A plugin without max_connections: fixed, no tuner.
	m.Limits.MaxConnections = 0
	if f := NewClient(m, nil); f.tune != nil || f.Connections() != 3 {
		t.Fatalf("no max_connections: tuner %v, %d", f.tune, f.Connections())
	}
}

// A plugin version that needs a newer Vitrine (min_vitrine) isn't offered to older ones; a build of the code reads
// anything.
func TestMinVitrine(t *testing.T) {
	defer func(v string) { AppVersion = v }(AppVersion)
	e := IndexEntry{ID: "x", Version: "2.0.0", MinVitrine: "1.0.6"}
	for v, want := range map[string]bool{"1.0.5": false, "1.0.6": true, "1.1.0": true, "v1.0.4": false, "dev": true, "test-abc123": true} {
		AppVersion = v
		if e.Supported() != want {
			t.Errorf("Vitrine %s: supported %v, want %v", v, !want, want)
		}
	}
	AppVersion = "1.0.0"
	if !(IndexEntry{Version: "1.0.0"}).Supported() {
		t.Error("no min_vitrine: not supported")
	}
}

// Idle requests (tag lookups) take a turn only when no other request waits: a download's request asked after an idle
// one still goes first; with nothing else waiting the idle one goes at the usual pace.
func TestIdleGoesLast(t *testing.T) {
	var m Manifest
	m.Limits.RequestsPerSec = 10
	c := NewClient(m, nil)
	ctx := context.Background()
	if err := c.waitTurn(ctx); err != nil { // (the next turn is 100 ms away now)
		t.Fatal(err)
	}
	order := make(chan string, 2)
	go func() { c.waitTurn(Idle(ctx)); order <- "idle" }()
	time.Sleep(20 * time.Millisecond)
	go func() { c.waitTurn(ctx); order <- "download" }()
	if first, second := <-order, <-order; first != "download" || second != "idle" {
		t.Fatalf("order: %s, %s", first, second)
	}
}
