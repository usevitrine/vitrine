package library

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// Files come in without waiting for their thumbnails; the media worker makes
// them in the background (woken by each new file).
func TestRunMedia(t *testing.T) {
	l := newTest(t)
	if l.tools.Check() != nil {
		t.Skip("media tools needed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 10)
	go l.RunMedia(ctx, 2, func(id uint32, err error) { t.Errorf("file %d: %v", id, err) }, func(files, failed int) { done <- files })
	var ids []uint32
	for i := range 3 {
		f, err := l.Import(ctx, bytes.NewReader(scenePNG(t, i+1, 320, 240)))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, f.ID)
		l.WakeMedia()
	}
	deadline := time.After(20 * time.Second)
	for {
		pending, err := l.PendingMedia(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-done:
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("still waiting for thumbnails: %v", pending)
		}
	}
	for _, id := range ids {
		if _, err := l.Thumb(ctx, id); err != nil {
			t.Fatalf("file %d has no thumbnail: %v", id, err)
		}
	}
}
