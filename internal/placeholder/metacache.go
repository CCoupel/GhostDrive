//go:build windows

package placeholder

// metaCache is an in-memory LRU cache for VFS metadata (Stat / List results).
//
// Design goals:
//   - Stdlib only: uses container/list for O(1) LRU eviction.
//   - Thread-safe: all operations are protected by a single Mutex.
//   - Bounded: at most maxMetaCacheEntries entries; oldest entries are evicted
//     when the limit is reached.
//   - TTL fallback: entries expire after their TTL regardless of Watch() events.
//     The primary invalidation path is push-based (handleWatchEvent), making the
//     TTL a long fallback for changes that occur outside GhostDrive (e.g. direct
//     server edits not reflected by a Watch() event within the TTL window).
//
// Key format: "<backendID>:<remotePath>" to isolate backends sharing the same
// cache instance.

import (
	"container/list"
	"sync"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
)

const (
	// defaultMetaCacheTTL is the TTL fallback when no Watch() event invalidates
	// an entry.  5 minutes is intentionally long: the primary invalidation path
	// (Write/Delete/Rename + Watch() push) keeps entries fresh for local ops.
	defaultMetaCacheTTL = 5 * time.Minute

	// maxMetaCacheEntries caps memory usage.  At ~500 bytes/entry (conservative
	// estimate for a FileInfo + list overhead), 1 000 entries ≈ 500 KB.
	maxMetaCacheEntries = 1000
)

// metaEntry is a single cache record.  Either stat or list is non-nil,
// depending on whether the entry was populated by Stat or List.
type metaEntry struct {
	key       string
	stat      *plugins.FileInfo   // non-nil for Stat cache entries
	list      []plugins.FileInfo  // non-nil for List cache entries
	expiresAt time.Time
}

// metaCache is the LRU metadata cache.
type metaCache struct {
	mu      sync.Mutex
	lru     *list.List               // front = most-recently-used
	index   map[string]*list.Element // key → element
	maxSize int
	ttl     time.Duration
}

// newMetaCache creates an empty metaCache with the given TTL.
// Pass ttl = 0 to use defaultMetaCacheTTL.
func newMetaCache(ttl time.Duration) *metaCache {
	if ttl <= 0 {
		ttl = defaultMetaCacheTTL
	}
	return &metaCache{
		lru:     list.New(),
		index:   make(map[string]*list.Element),
		maxSize: maxMetaCacheEntries,
		ttl:     ttl,
	}
}

// getStat returns the cached FileInfo for key, or (nil, false) on miss/expiry.
func (c *metaCache) getStat(key string) (*plugins.FileInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.index[key]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*metaEntry)
	if time.Now().After(entry.expiresAt) {
		c.evict(el)
		return nil, false
	}
	c.lru.MoveToFront(el)
	return entry.stat, entry.stat != nil
}

// getList returns the cached directory listing for key, or (nil, false) on miss/expiry.
func (c *metaCache) getList(key string) ([]plugins.FileInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.index[key]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*metaEntry)
	if time.Now().After(entry.expiresAt) {
		c.evict(el)
		return nil, false
	}
	c.lru.MoveToFront(el)
	return entry.list, entry.list != nil
}

// putStat stores a Stat result in the cache, evicting the LRU entry if full.
func (c *metaCache) putStat(key string, fi *plugins.FileInfo) {
	if fi == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.put(key, &metaEntry{key: key, stat: fi, expiresAt: time.Now().Add(c.ttl)})
}

// putList stores a List result in the cache, evicting the LRU entry if full.
func (c *metaCache) putList(key string, entries []plugins.FileInfo) {
	if entries == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.put(key, &metaEntry{key: key, list: entries, expiresAt: time.Now().Add(c.ttl)})
}

// put inserts or replaces an entry under the lock.  Caller must hold c.mu.
func (c *metaCache) put(key string, entry *metaEntry) {
	if el, ok := c.index[key]; ok {
		// Replace existing entry in-place, move to front.
		el.Value = entry
		c.lru.MoveToFront(el)
		return
	}
	// Evict LRU entry when at capacity.
	for c.lru.Len() >= c.maxSize {
		if back := c.lru.Back(); back != nil {
			c.evict(back)
		}
	}
	el := c.lru.PushFront(entry)
	c.index[key] = el
}

// invalidate removes the entry for key from the cache (no-op if absent).
func (c *metaCache) invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.index[key]; ok {
		c.evict(el)
	}
}

// invalidatePrefix removes all entries whose key starts with prefix.
// Useful for invalidating an entire directory subtree in one call.
func (c *metaCache) invalidatePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, el := range c.index {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			c.evict(el)
		}
	}
}

// clear removes all entries from the cache.
func (c *metaCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Init()
	c.index = make(map[string]*list.Element)
}

// evict removes el from the LRU list and index.  Caller must hold c.mu.
func (c *metaCache) evict(el *list.Element) {
	entry := el.Value.(*metaEntry)
	delete(c.index, entry.key)
	c.lru.Remove(el)
}
