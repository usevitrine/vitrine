package library

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// waitRepair waits until a fix is in a state (or gone from the list: "").
func waitRepair(t *testing.T, l *Library, id, state string) Repair {
	t.Helper()
	for range 500 {
		list, _ := l.Repairs()
		found := Repair{}
		for _, r := range list {
			if r.ID == id {
				found = r
			}
		}
		if found.State == state {
			return found
		}
		time.Sleep(5 * time.Millisecond)
	}
	list, _ := l.Repairs()
	t.Fatalf("%s never became %q: %+v", id, state, list)
	return Repair{}
}

// A fix is offered, not run; run, it counts and waits to be acknowledged, then it's gone - for good. One skipped isn't
// offered again, one with nothing to do never shows, and one that failed can be tried again.
func TestRepairs(t *testing.T) {
	l := newTest(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	l.rep.wait = time.Millisecond
	var mu sync.Mutex
	told := map[string]int{}
	l.OnRepair = func(r Repair) {
		mu.Lock()
		told[r.ID+"/"+r.State]++
		mu.Unlock()
	}
	ran, fail := make(chan struct{}, 9), error(errors.New("disk full"))
	fix := func(id string, total int64) {
		l.AddRepair(id, func(context.Context) (int64, int64, error) { return 0, total, nil }, func(ctx context.Context) error {
			ran <- struct{}{}
			l.stepRepair(ctx, id, total, 2)
			return nil
		})
	}
	fix("good", 5)
	fix("skipped", 3)
	fix("nothing", 0)
	l.AddRepair("flaky", func(context.Context) (int64, int64, error) { return 0, 4, nil }, func(ctx context.Context) error { return fail })
	if err := l.StartRepairs(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := l.Repairs()
	if len(list) != 3 {
		t.Fatalf("offered: %+v", list)
	}
	select {
	case <-ran:
		t.Fatal("ran without being asked")
	case <-time.After(20 * time.Millisecond):
	}
	if err := l.AckRepair(ctx, "good"); !errors.Is(err, ErrNoRepair) {
		t.Fatalf("acknowledged before it finished: %v", err)
	}

	if err := l.RunRepair(ctx, "good"); err != nil {
		t.Fatal(err)
	}
	if r := waitRepair(t, l, "good", RepairDone); r.Done != 5 || r.Total != 5 || r.Changed != 2 || r.FinishedAt == 0 {
		t.Fatalf("finished: %+v", r)
	}
	if err := l.RunRepair(ctx, "good"); !errors.Is(err, ErrNoRepair) {
		t.Fatalf("ran again: %v", err)
	}
	if err := l.AckRepair(ctx, "good"); err != nil {
		t.Fatal(err)
	}
	waitRepair(t, l, "good", "")

	if err := l.SkipRepair(ctx, "skipped"); err != nil {
		t.Fatal(err)
	}
	waitRepair(t, l, "skipped", "")

	if err := l.RunRepair(ctx, "flaky"); err != nil {
		t.Fatal(err)
	}
	if r := waitRepair(t, l, "flaky", RepairFailed); r.Error != "disk full" {
		t.Fatalf("failed: %+v", r)
	}
	mu.Lock()
	fail = nil
	mu.Unlock()
	if err := l.RunRepair(ctx, "flaky"); err != nil {
		t.Fatal(err)
	}
	waitRepair(t, l, "flaky", RepairDone)

	mu.Lock()
	if told["good/offered"] != 1 || told["good/done"] != 1 || told["flaky/failed"] != 1 || told["flaky/done"] != 1 || told["nothing/offered"] != 0 || told["skipped/done"] != 0 {
		t.Fatalf("told: %v", told)
	}
	mu.Unlock()

	// After a restart: what was answered stays answered; the unacknowledged one still waits.
	l.rep.list = nil
	if err := l.StartRepairs(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ = l.Repairs()
	if len(list) != 1 || list[0].ID != "flaky" || list[0].State != RepairDone {
		t.Fatalf("after a restart: %+v", list)
	}
}

// One that was running when the server stopped goes on by itself - and one that had begun before fixes were offered
// isn't offered: it carries on from what it had done.
func TestRepairsGoOnAfterARestart(t *testing.T) {
	l := newTest(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	l.rep.wait = time.Millisecond
	block := make(chan struct{})
	run := func(id string) func(context.Context) error {
		return func(ctx context.Context) error {
			select {
			case <-block:
			case <-ctx.Done():
				return ctx.Err()
			}
			l.stepRepair(ctx, id, 1, 0)
			return nil
		}
	}
	l.AddRepair("begun", func(context.Context) (int64, int64, error) { return 40, 100, nil }, run("begun"))
	l.AddRepair("asked", func(context.Context) (int64, int64, error) { return 0, 10, nil }, run("asked"))
	if err := l.StartRepairs(ctx); err != nil {
		t.Fatal(err)
	}
	if r := waitRepair(t, l, "begun", RepairRunning); r.Done != 40 || r.Total != 100 {
		t.Fatalf("begun: %+v", r)
	}
	if err := l.RunRepair(ctx, "asked"); err != nil {
		t.Fatal(err)
	}
	waitRepair(t, l, "asked", RepairRunning)
	stop()
	time.Sleep(20 * time.Millisecond)

	// (the same library, started again: it says it's holding off, then it isn't)
	ctx2, stop2 := context.WithCancel(context.Background())
	defer stop2()
	// (under the lock: a repair of the first start can still be ending - the race detector caught it on CI, 2026-11-09)
	l.rep.mu.Lock()
	l.rep.list = nil
	l.rep.wait = 150 * time.Millisecond
	l.rep.mu.Unlock()
	if err := l.StartRepairs(ctx2); err != nil {
		t.Fatal(err)
	}
	if r := waitRepair(t, l, "asked", RepairRunning); !r.Waiting {
		t.Fatalf("not waiting: %+v", r)
	}
	for r := waitRepair(t, l, "asked", RepairRunning); r.Waiting; r = waitRepair(t, l, "asked", RepairRunning) {
		time.Sleep(5 * time.Millisecond)
	}
	close(block)
	waitRepair(t, l, "asked", RepairDone)
	if r := waitRepair(t, l, "begun", RepairDone); r.Done != 41 {
		t.Fatalf("begun after: %+v", r)
	}
}
