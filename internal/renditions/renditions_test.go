package renditions

import (
	"bytes"
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetMakesOnceAndCaches(t *testing.T) {
	c, err := Open(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	mk := func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		return []byte("webp-data"), nil
	}
	var wg sync.WaitGroup
	paths := make([]string, 5)
	for i := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := c.Get(context.Background(), "abcdef", 2048, mk)
			if err != nil {
				t.Error(err)
			}
			paths[i] = p
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("made %d times, want 1", calls.Load())
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || !bytes.Equal(data, []byte("webp-data")) {
		t.Fatalf("content %q, %v", data, err)
	}
	c.Get(context.Background(), "abcdef", 2048, mk)
	if calls.Load() != 1 {
		t.Fatal("cached rendition made again")
	}
	if c.Size() != 9 {
		t.Fatalf("size %d", c.Size())
	}
}

func TestEviction(t *testing.T) {
	dir := t.TempDir()
	c, _ := Open(dir, 2500)
	blob := func(ctx context.Context) ([]byte, error) { return make([]byte, 1000), nil }
	ctx := context.Background()
	first, _ := c.Get(ctx, "aa1111", 2048, blob)
	time.Sleep(20 * time.Millisecond)
	second, _ := c.Get(ctx, "bb2222", 2048, blob)
	time.Sleep(20 * time.Millisecond)
	c.Get(ctx, "aa1111", 2048, blob) // use the first again: it's now the newest
	time.Sleep(20 * time.Millisecond)
	third, _ := c.Get(ctx, "cc3333", 2048, blob) // 3000 > 2500: evict the oldest (second)
	if _, err := os.Stat(second); err == nil {
		t.Error("least recently used rendition not evicted")
	}
	for _, p := range []string{first, third} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s evicted", p)
		}
	}
	if c.Size() > 2500 {
		t.Errorf("size %d over the limit", c.Size())
	}
	// A reopened cache counts what's on disk.
	c2, _ := Open(dir, 2500)
	if c2.Size() != c.Size() {
		t.Errorf("reopened size %d, want %d", c2.Size(), c.Size())
	}
}

func TestSnap(t *testing.T) {
	for want, got := range map[int]int{100: 1600, 2048: 2048, 2556: 2560, 3000: 3072, 3100: 4096, 5000: 8192, 20000: 8192} {
		if Snap(want) != got {
			t.Errorf("Snap(%d) = %d, want %d", want, Snap(want), got)
		}
	}
}
