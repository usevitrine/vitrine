package download

import (
	"bytes"
	"context"
	"io"
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
		// (until all of the half that came is read: it can arrive in parts - CI saw 64 of its 71 bytes)
		if p = e.dl.Progress(); len(p) == 1 && p[0].Read >= int64(size/2) {
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

// The speed counts every byte a search reads - a small file begun and done between two looks too - over the last
// few seconds, not file by file.
func TestLiveSpeeds(t *testing.T) {
	e := newEnv(t, 1)
	ctx := context.Background()
	if sp := e.dl.Speeds(); len(sp) != 0 {
		t.Fatalf("speeds before anything: %v", sp)
	}
	read := func(search, post int64, n int) {
		pctx, done := e.dl.trackPost(ctx, search, post)
		body := e.dl.countBody(pctx, io.NopCloser(bytes.NewReader(make([]byte, n))), int64(n))
		io.Copy(io.Discard, body)
		body.Close()
		done()
	}
	read(7, 1, 1000) // (before the first look: not in the speed)
	if sp := e.dl.Speeds(); len(sp) != 0 {
		t.Fatalf("a speed from one look: %v", sp)
	}
	read(7, 2, 300_000) // two small files, both done before the next look
	read(7, 3, 200_000)
	time.Sleep(300 * time.Millisecond)
	// (at least a second counted: a fraction of one would make a burst look fast)
	if sp := e.dl.Speeds(); len(sp) != 1 || sp[7] != 500_000 {
		t.Fatalf("speeds: %v, want 500000 for search 7", sp)
	}
}
