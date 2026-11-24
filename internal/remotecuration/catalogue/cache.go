package catalogue

import (
	"container/list"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
)

// pageCache keeps parsed pages per replica (R-CUR-008): the names and facts of an answer, never a document's bytes, at most the
// settings' bytes for the settings' period, the least recently used first out. A page's size is its JSON encoding's length, a bound
// the heap it holds stays near.
type pageCache struct {
	now   func() time.Time
	mu    sync.Mutex
	lru   *list.List
	items map[string]*list.Element
	bytes int64
}

type cached struct {
	key   string
	value any
	size  int64
	until time.Time
}

func newPageCache(now func() time.Time) *pageCache {
	return &pageCache{now: now, lru: list.New(), items: map[string]*list.Element{}}
}

// get returns the page of key while it is within its period.
func (c *pageCache) get(key string, ttl time.Duration) (any, bool) {
	if ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*cached) //nolint:forcetypeassert // the list holds *cached only
	if c.now().After(e.until) {
		c.remove(el)
		return nil, false
	}
	c.lru.MoveToFront(el)
	return e.value, true
}

// put keeps a page for the period within the bytes; a page larger than a quarter of the bytes is not kept.
func (c *pageCache) put(key string, v any, maxBytes int64, ttl time.Duration) {
	if ttl <= 0 || maxBytes <= 0 {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	size := int64(len(b) + len(key) + 64)
	if size > maxBytes/4 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}
	c.items[key] = c.lru.PushFront(&cached{key: key, value: v, size: size, until: c.now().Add(ttl)})
	c.bytes += size
	for c.bytes > maxBytes {
		back := c.lru.Back()
		if back == nil {
			break
		}
		c.remove(back)
	}
}

// dropCaps forgets the capabilities of a repository's upstream: a capability turned off reads anew.
func (c *pageCache) dropCaps(t Target) {
	prefix := strconv.FormatInt(t.RepoID, 10) + "\x00" + t.Upstream + "\x00" + CallCaps + "\x00"
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, el := range c.items {
		if strings.HasPrefix(k, prefix) {
			c.remove(el)
		}
	}
}

func (c *pageCache) remove(el *list.Element) {
	e := el.Value.(*cached) //nolint:forcetypeassert // the list holds *cached only
	c.lru.Remove(el)
	delete(c.items, e.key)
	c.bytes -= e.size
}
