package download

import (
	"context"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/plugin"
)

func TestParallelDownloads(t *testing.T) {
	e := newEnvWith(t, 25, "") // per_page 10
	ctx := context.Background()
	// Three files at a time for this site.
	m := e.dl.Site("fake").Client.Manifest()
	m.Limits.Connections = 3
	c := plugin.NewClient(m, nil)
	c.SetHTTPClient(e.fb.srv.Client())
	eng, _ := plugin.NewEngine(c)
	e.dl.putSite("fake", &Site{ID: e.dl.all()["fake"].ID, Client: c, Engine: eng})

	// The first four files wait: three start, the fourth must not.
	holds := map[int64]chan struct{}{}
	e.fb.mu.Lock()
	for _, id := range []int64{25, 24, 23, 22} {
		holds[id] = make(chan struct{})
		e.fb.hold[id] = holds[id]
	}
	e.fb.mu.Unlock()
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	finished := make(chan error)
	go func() { finished <- e.dl.RunQueue(ctx, "fake") }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		e.fb.mu.Lock()
		active := e.fb.active
		e.fb.mu.Unlock()
		if active == 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // a fourth would have started by now
	e.fb.mu.Lock()
	peak := e.fb.peak
	e.fb.mu.Unlock()
	if peak != 3 {
		t.Fatalf("%d files at once, want 3", peak)
	}
	for _, h := range holds {
		close(h)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	s, _ := e.dl.Get(ctx, id)
	if s.State != StateDone || s.Downloaded != 24 || s.Failed != 1 || s.LowID != 1 {
		t.Fatalf("search: %+v", s)
	}
	if n := e.count(t, ""); n != 24 {
		t.Fatalf("%d files", n)
	}
}

func TestParallelWatermark(t *testing.T) {
	e := newEnvWith(t, 6, "")
	m := e.dl.Site("fake").Client.Manifest()
	m.Limits.Connections = 3
	c := plugin.NewClient(m, nil)
	c.SetHTTPClient(e.fb.srv.Client())
	eng, _ := plugin.NewEngine(c)
	e.dl.putSite("fake", &Site{ID: e.dl.all()["fake"].ID, Client: c, Engine: eng})

	// The newest post (6) is slow; 5 and 4 finish first. Stop while 6 is in flight.
	hold := make(chan struct{})
	e.fb.mu.Lock()
	e.fb.hold[6] = hold
	e.fb.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	done := 0
	e.dl.OnPost = func(s Search, p plugin.Post, outcome int, err error) {
		done++
		if done == 2 { // 5 and 4 done, 6 still downloading
			go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		}
	}
	e.dl.RunQueue(ctx, "fake")
	s, _ := e.dl.Get(context.Background(), id)
	if s.LowID != 0 {
		t.Fatalf("resume point moved past an unfinished post: low_id %d", s.LowID)
	}
	if e.count(t, "tag_6") != 0 || e.count(t, "tag_5") != 1 {
		t.Fatalf("files: 6=%d 5=%d", e.count(t, "tag_6"), e.count(t, "tag_5"))
	}

	// Resume: 6 is fetched, 5 and 4 aren't fetched again.
	close(hold)
	e.dl.OnPost = nil
	hits5 := e.fb.fileHits[5]
	e.dl.RunQueue(context.Background(), "fake")
	s, _ = e.dl.Get(context.Background(), id)
	if s.State != StateDone || e.count(t, "") != 6 || e.fb.fileHits[5] != hits5 {
		t.Fatalf("after resume: %+v, %d files, post 5 fetched %d -> %d", s, e.count(t, ""), hits5, e.fb.fileHits[5])
	}
	// Each post counted once: 5 and 4 came again after the resume (past its resume point), not counted again.
	if s.Handled != 6 || s.Downloaded != 6 || s.Already != 0 {
		t.Fatalf("counts after resume: handled %d, downloaded %d, already %d", s.Handled, s.Downloaded, s.Already)
	}
}

// A slow file at the end of a page doesn't hold the others up: the next page comes while it's in flight, and its
// files download meanwhile - but the resume point never moves past the slow one.
func TestNextPageWhileTheLastFilesCome(t *testing.T) {
	e := newEnvWith(t, 25, "") // per_page 10: 25..16, 15..6, 5..1
	ctx := context.Background()
	m := e.dl.Site("fake").Client.Manifest()
	m.Limits.Connections = 3
	c := plugin.NewClient(m, nil)
	c.SetHTTPClient(e.fb.srv.Client())
	eng, _ := plugin.NewEngine(c)
	e.dl.putSite("fake", &Site{ID: e.dl.all()["fake"].ID, Client: c, Engine: eng})

	hold := make(chan struct{})
	e.fb.mu.Lock()
	e.fb.hold[16] = hold // the first page's last post
	e.fb.mu.Unlock()
	id, _ := e.dl.Add(ctx, "fake", "common", 0)
	finished := make(chan error)
	go func() { finished <- e.dl.RunQueue(ctx, "fake") }()
	deadline := time.Now().Add(5 * time.Second)
	for e.count(t, "tag_8") == 0 && time.Now().Before(deadline) { // (well into the second page)
		time.Sleep(10 * time.Millisecond)
	}
	if e.count(t, "tag_8") == 0 {
		t.Fatal("the second page waited for the first page's slow file")
	}
	if s, _ := e.dl.Get(ctx, id); s.LowID != 17 {
		t.Fatalf("resume point %d while post 16 is in flight, want 17", s.LowID)
	}
	close(hold)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	s, _ := e.dl.Get(ctx, id)
	if s.State != StateDone || s.Downloaded != 24 || s.Failed != 1 || s.LowID != 1 {
		t.Fatalf("search: %+v", s)
	}
}
