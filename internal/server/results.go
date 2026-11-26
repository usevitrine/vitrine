package server

import (
	"container/list"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// resultCache keeps recent search results server-side, so clients can fetch
// any slice of a huge result by position (1M IDs = 4 MB) without re-running
// the search or receiving all of it. Least recently used results are
// dropped; a client whose result is gone simply searches again.
type resultCache struct {
	mu    sync.Mutex
	max   int
	byID  map[string]*list.Element
	order *list.List // front = most recently used
}

type cachedResult struct {
	id  string
	ids []uint32
}

func newResultCache(max int) *resultCache {
	return &resultCache{max: max, byID: map[string]*list.Element{}, order: list.New()}
}

func (c *resultCache) add(ids []uint32) string {
	b := make([]byte, 8)
	rand.Read(b)
	id := hex.EncodeToString(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byID[id] = c.order.PushFront(&cachedResult{id: id, ids: ids})
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.byID, oldest.Value.(*cachedResult).id)
	}
	return id
}

func (c *resultCache) get(id string) ([]uint32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byID[id]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*cachedResult).ids, true
}
