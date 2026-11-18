package download

import (
	"context"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/plugin"
)

// withConnections gives the fake site n connections.
func withConnections(e *env, n int) {
	m := e.dl.Site("fake").Client.Manifest()
	m.Limits.Connections = n
	c := plugin.NewClient(m, nil)
	c.SetHTTPClient(e.fb.srv.Client())
	eng, _ := plugin.NewEngine(c)
	e.dl.putSite("fake", &Site{ID: e.dl.all()["fake"].ID, Client: c, Engine: eng})
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second) // (CI's machines are slow)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Two downloads of a site at once: the second doesn't wait for the first, whose files are slow.
func TestSiteDownloadsAtOnce(t *testing.T) {
	e := newEnvWith(t, 25, "")
	withConnections(e, 3)
	e.dl.SetSiteDownloads(2)
	ctx := context.Background()
	holds := []chan struct{}{make(chan struct{}), make(chan struct{})}
	e.fb.mu.Lock()
	e.fb.hold[25], e.fb.hold[24] = holds[0], holds[1]
	e.fb.mu.Unlock()
	a, _ := e.dl.Add(ctx, "fake", "common", 2)        // 25 and 24: both held
	b, _ := e.dl.Add(ctx, "fake", "common id:<10", 3) // 9, 8, 7 on the third connection
	finished := make(chan error)
	go func() { finished <- e.dl.RunQueue(ctx, "fake") }()
	waitFor(t, "the second download", func() bool { s, _ := e.dl.Get(ctx, b); return s.State == StateDone })
	if s, _ := e.dl.Get(ctx, a); s.State != StateRunning || s.Handled != 0 {
		t.Fatalf("the first: %+v", s)
	}
	if s, _ := e.dl.Get(ctx, b); s.Downloaded != 3 {
		t.Fatalf("the second: %+v", s)
	}
	for _, h := range holds {
		close(h)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if s, _ := e.dl.Get(ctx, a); s.State != StateDone || s.Downloaded != 2 {
		t.Fatalf("the first: %+v", s)
	}
	e.fb.mu.Lock()
	defer e.fb.mu.Unlock()
	if e.fb.peak > 3 {
		t.Fatalf("%d files at once, the site has 3 connections", e.fb.peak)
	}
}

// Downloads running together share the site's connections: never more files at once than it has. (Every file waits,
// so which download's page comes first - either may - doesn't matter: CI, 2026-11-18.)
func TestSiteDownloadsShareConnections(t *testing.T) {
	e := newEnvWith(t, 25, "")
	withConnections(e, 2)
	e.dl.SetSiteDownloads(3)
	ctx := context.Background()
	block := make(chan struct{})
	e.fb.mu.Lock()
	e.fb.block = block
	e.fb.mu.Unlock()
	a, _ := e.dl.Add(ctx, "fake", "common", 0)
	b, _ := e.dl.Add(ctx, "fake", "common id:<10", 3)
	finished := make(chan error)
	go func() { finished <- e.dl.RunQueue(ctx, "fake") }()
	waitFor(t, "both running, two files on their way", func() bool {
		sa, _ := e.dl.Get(ctx, a)
		sb, _ := e.dl.Get(ctx, b)
		e.fb.mu.Lock()
		defer e.fb.mu.Unlock()
		return sa.State == StateRunning && sb.State == StateRunning && e.fb.active == 2
	})
	time.Sleep(200 * time.Millisecond) // (a third would have started by now)
	e.fb.mu.Lock()
	peak := e.fb.peak
	e.fb.block = nil
	e.fb.mu.Unlock()
	if peak != 2 {
		t.Fatalf("%d files at once, the site has 2 connections", peak)
	}
	close(block)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	sa, _ := e.dl.Get(ctx, a)
	sb, _ := e.dl.Get(ctx, b)
	if sa.State != StateDone || sb.State != StateDone || sb.Handled != 3 {
		t.Fatalf("a %+v, b %+v", sa, sb)
	}
	e.fb.mu.Lock()
	defer e.fb.mu.Unlock()
	if e.fb.peak != 2 {
		t.Fatalf("%d files at once, the site has 2 connections", e.fb.peak)
	}
}

// Fewer at once than run now: the ones past the first step aside (and wait in the queue where they were).
func TestSiteDownloadsFewer(t *testing.T) {
	e := newEnvWith(t, 25, "")
	withConnections(e, 3)
	e.dl.SetSiteDownloads(2)
	e.dl.asideEvery = 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	block := make(chan struct{})
	e.fb.mu.Lock()
	e.fb.block = block
	e.fb.mu.Unlock()
	a, _ := e.dl.Add(ctx, "fake", "common", 0)
	b, _ := e.dl.Add(ctx, "fake", "common id:<10", 0)
	finished := make(chan error)
	go func() { finished <- e.dl.RunQueue(ctx, "fake") }()
	waitFor(t, "both running", func() bool {
		sa, _ := e.dl.Get(ctx, a)
		sb, _ := e.dl.Get(ctx, b)
		return sa.State == StateRunning && sb.State == StateRunning
	})
	e.dl.SetSiteDownloads(1)
	e.fb.mu.Lock()
	e.fb.block = nil
	e.fb.mu.Unlock()
	close(block)
	waitFor(t, "the second to step aside", func() bool { s, _ := e.dl.Get(ctx, b); return s.State == StateQueued })
	if s, _ := e.dl.Get(ctx, a); s.State != StateRunning && s.State != StateDone {
		t.Fatalf("the first: %+v", s)
	}
	cancel()
	<-finished
}

func TestFileGateTurns(t *testing.T) {
	g := &fileGate{free: make(chan struct{})}
	if ok, _ := g.take(1, 2); !ok {
		t.Fatal("first")
	}
	if ok, _ := g.take(1, 2); !ok {
		t.Fatal("second")
	}
	ok, wait := g.take(2, 2) // full: 2 waits in line
	if ok || wait == nil {
		t.Fatal("over the limit")
	}
	g.release()
	select {
	case <-wait:
	default:
		t.Fatal("not told")
	}
	if ok, _ := g.take(1, 2); ok {
		t.Fatal("1 took the connection 2 waits for")
	}
	if ok, _ := g.take(2, 2); !ok {
		t.Fatal("2's turn")
	}
	if ok, _ := g.take(1, 2); ok {
		t.Fatal("over the limit")
	}
	g.leave(1)
	if len(g.waiting) != 0 {
		t.Fatalf("line %v", g.waiting)
	}
}
