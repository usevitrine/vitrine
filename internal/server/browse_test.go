package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The pictures browsed lately are kept up to a size; the least recently used go first.
func TestPictureCache(t *testing.T) {
	c := newPictureCache(40)
	pic := func(n int) picture { return picture{make([]byte, n), "image/webp"} }
	c.put("a", pic(10))
	c.put("b", pic(10))
	c.put("c", pic(10))
	if _, ok := c.get("a"); !ok { // (a: used again - the latest now)
		t.Fatal("a not kept")
	}
	c.put("d", pic(10))
	c.put("e", pic(10)) // over 40: the least recently used (b) goes
	if _, ok := c.get("b"); ok {
		t.Fatal("b kept past the size")
	}
	for _, u := range []string{"a", "c", "d", "e"} {
		if _, ok := c.get(u); !ok {
			t.Fatalf("%s not kept", u)
		}
	}
	c.put("big", pic(30)) // (more than a quarter of it all: not kept)
	if _, ok := c.get("big"); ok {
		t.Fatal("a picture too big for the cache was kept")
	}
}

// One fetch per picture however many ask at once; a fetch called off (a newer search) is done again for one still
// waiting.
func TestPictureCacheLoad(t *testing.T) {
	c := newPictureCache(1 << 20)
	var fetches atomic.Int32
	slow := func(ctx context.Context) (picture, error) {
		fetches.Add(1)
		select {
		case <-time.After(50 * time.Millisecond):
			return picture{[]byte("x"), "image/webp"}, nil
		case <-ctx.Done():
			return picture{}, ctx.Err()
		}
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.load(context.Background(), context.Background(), "a", slow); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := fetches.Load(); n != 1 {
		t.Fatalf("%d fetches for one picture", n)
	}
	// A prefetch called off while someone waits for it: fetched again for them.
	pre, stop := context.WithCancel(context.Background())
	go c.load(pre, pre, "b", slow)
	time.Sleep(10 * time.Millisecond)
	done := make(chan error)
	go func() {
		_, err := c.load(context.Background(), context.Background(), "b", slow)
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	stop()
	if err := <-done; err != nil {
		t.Fatalf("the one waiting got: %v", err)
	}
}

// Browse hides a gallery with a blacklisted tag: one of the site's own, or one as the library names it.
func TestBrowseBlacklisted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.lib.SetBlacklist(ctx, []string{"creator:some_artist", "red*"}); err != nil {
		t.Fatal(err)
	}
	bl, err := e.lib.Blacklist(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		site []string
		tags []browseTag
		want bool
	}{
		{[]string{"blue_sky"}, []browseTag{{Name: "blue_sky"}}, false},
		{[]string{"artist:some_artist"}, []browseTag{{NS: "creator", Name: "some_artist"}}, true}, // (as the library names it)
		{[]string{"female:red_hair"}, []browseTag{{NS: "female", Name: "red_hair"}}, true},        // (a wildcard, on the name)
		{nil, []browseTag{{NS: "female", Name: "blue_sky"}}, false},
	} {
		if got := blacklisted(bl, c.site, c.tags); got != c.want {
			t.Errorf("%v %v: %v, want %v", c.site, c.tags, got, c.want)
		}
	}
	if blacklisted(nil, []string{"red"}, nil) {
		t.Error("no blacklist hid a gallery")
	}
}
