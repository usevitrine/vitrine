package renditions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// The sizes viewers ask for to fill their screen are remembered (with the
// day each was last asked for), so copies at those sizes can be made ahead
// of time; a size nobody has used for a while is forgotten.

const sizesFile = "screen-sizes.json"

func (c *Cache) loadSizes() {
	c.used = map[int]int64{}
	if b, err := os.ReadFile(filepath.Join(c.dir, sizesFile)); err == nil {
		json.Unmarshal(b, &c.used)
	}
}

// UsedForScreen records that a viewer asked for size to fill its screen.
func (c *Cache) UsedForScreen(size int) {
	day := time.Now().Unix() / 86400
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used[size] == day {
		return
	}
	c.used[size] = day
	if b, err := json.Marshal(c.used); err == nil {
		tmp := filepath.Join(c.dir, sizesFile+".tmp")
		if os.WriteFile(tmp, b, 0o644) == nil {
			os.Rename(tmp, filepath.Join(c.dir, sizesFile))
		}
	}
}

// ScreenSizes are the sizes asked for to fill a screen within the last maxAge.
func (c *Cache) ScreenSizes(maxAge time.Duration) []int {
	cutoff := time.Now().Add(-maxAge).Unix() / 86400
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []int
	for s, day := range c.used {
		if day >= cutoff {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// Has reports whether the copy of key at size exists.
func (c *Cache) Has(key string, size int) bool {
	if len(key) < 2 {
		return false
	}
	_, err := os.Stat(c.path(key, size))
	return err == nil
}

// RoomAhead reports whether copies made ahead of time still fit: they stop at
// 80% of the limit, so they never push out copies that were really viewed.
func (c *Cache) RoomAhead() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total < c.maxBytes*8/10
}
