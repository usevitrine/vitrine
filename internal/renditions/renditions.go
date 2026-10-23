// Package renditions keeps resized copies of huge images for viewing on
// phones (decoding a 100+ megapixel original can crash Safari). Copies are
// made on first request, stored as WebP and evicted oldest-first when the
// cache grows past its size limit. Everything here can be regenerated.
package renditions

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Sizes are the allowed long-side sizes (a few fixed sizes keep the cache
// small and well shared).
var Sizes = []int{1600, 2048, 2560, 3072, 4096, 8192} // 2560: a phone's screen (e.g. 2556 px)

// Snap returns the smallest allowed size >= want (or the largest).
func Snap(want int) int {
	for _, s := range Sizes {
		if want <= s {
			return s
		}
	}
	return Sizes[len(Sizes)-1]
}

// MakeFunc produces a rendition's bytes.
type MakeFunc func(ctx context.Context) ([]byte, error)

// Cache is a directory of renditions with a size limit.
type Cache struct {
	dir      string
	maxBytes int64

	mu       sync.Mutex
	total    int64
	inflight map[string]*call
	used     map[int]int64 // screen sizes asked for -> the day last asked (sizes.go)
}

type call struct {
	done chan struct{}
	err  error
}

// Open uses dir for the cache (created if needed) with a size limit.
func Open(dir string, maxBytes int64) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("renditions: %w", err)
	}
	c := &Cache{dir: dir, maxBytes: maxBytes, inflight: map[string]*call{}}
	c.loadSizes()
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && d.Name() != sizesFile {
			if info, err := d.Info(); err == nil {
				c.total += info.Size()
			}
		}
		return nil
	})
	return c, nil
}

func (c *Cache) path(key string, size int) string {
	return filepath.Join(c.dir, key[:2], fmt.Sprintf("%s-%d.webp", key, size))
}

// Get returns the rendition file for key (e.g. the original's SHA-256) at a
// size, making it with produce if needed. Concurrent requests for the same
// rendition share one produce call.
func (c *Cache) Get(ctx context.Context, key string, size int, produce MakeFunc) (string, error) {
	if len(key) < 2 {
		return "", fmt.Errorf("renditions: bad key")
	}
	p := c.path(key, size)
	if _, err := os.Stat(p); err == nil {
		now := time.Now()
		os.Chtimes(p, now, now) // recently used: evicted last
		return p, nil
	}

	id := filepath.Base(p)
	c.mu.Lock()
	if cl, ok := c.inflight[id]; ok {
		c.mu.Unlock()
		select {
		case <-cl.done:
			return p, cl.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	cl := &call{done: make(chan struct{})}
	c.inflight[id] = cl
	c.mu.Unlock()

	cl.err = c.create(ctx, p, produce)
	c.mu.Lock()
	delete(c.inflight, id)
	c.mu.Unlock()
	close(cl.done)
	return p, cl.err
}

func (c *Cache) create(ctx context.Context, p string, produce MakeFunc) error {
	data, err := produce(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("renditions: writing: %v %v", werr, cerr)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	c.mu.Lock()
	c.total += int64(len(data))
	over := c.total > c.maxBytes
	c.mu.Unlock()
	if over {
		c.evict(p)
	}
	return nil
}

// evict deletes the least recently used renditions until the cache is
// within 90% of its limit (keeping the one just made).
func (c *Cache) evict(keep string) {
	type entry struct {
		path string
		size int64
		used time.Time
	}
	var all []entry
	filepath.WalkDir(c.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || path == keep || d.Name() == sizesFile {
			return nil
		}
		if info, err := d.Info(); err == nil {
			all = append(all, entry{path, info.Size(), info.ModTime()})
		}
		return nil
	})
	slices.SortFunc(all, func(a, b entry) int { return a.used.Compare(b.used) })
	c.mu.Lock()
	defer c.mu.Unlock()
	target := c.maxBytes * 9 / 10
	for _, e := range all {
		if c.total <= target {
			break
		}
		if os.Remove(e.path) == nil {
			c.total -= e.size
		}
	}
}

// Size is the cache's current size in bytes.
func (c *Cache) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}
