package download

import (
	"context"
	"testing"
	"time"
)

func TestLiveProgress(t *testing.T) {
	e := newEnv(t, 3)
	ctx := context.Background()
	changes, stop := e.dl.Watch()
	defer stop()
	release := make(chan struct{})
	e.fb.mu.Lock()
	e.fb.partial[3] = release
	size := len(e.fb.files[3])
	e.fb.mu.Unlock()

	e.dl.Add(ctx, "fake", "common", 0)
	select {
	case <-changes: // queued
	case <-time.After(time.Second):
		t.Fatal("no signal for the new search")
	}
	done := make(chan error)
	go func() { done <- e.dl.RunQueue(ctx, "fake") }()

	// Post 3 is half downloaded: it shows in the progress list.
	var p []FileProgress
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if p = e.dl.Progress(); len(p) == 1 && p[0].Read > 0 {
			break
		}
	}
	if len(p) != 1 || p[0].PostID != 3 || p[0].Read != int64(size/2) || p[0].Total != int64(size) {
		t.Fatalf("progress: %+v (file %d bytes)", p, size)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p := e.dl.Progress(); len(p) != 0 {
		t.Fatalf("still in progress after the run: %+v", p)
	}
	select {
	case <-changes:
	default:
		t.Fatal("no signal for the handled posts")
	}
}
