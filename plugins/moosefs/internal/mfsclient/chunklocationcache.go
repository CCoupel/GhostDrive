// Package mfsclient — chunk location cache (issue #163, B1).
//
// Client.Read previously called locateChunk (a master CLTOMA_FUSE_READ_CHUNK
// roundtrip) before *every* chunk-server read, regardless of read size. With
// Download's original 64 KiB block size and a 64 MiB MooseFS chunk, the same
// location was therefore requested from the master up to 1024 times instead
// of once — and locateChunk held Client.mu, the single master connection's
// mutex, so this serialized every other master operation (GetAttr, Lookup,
// ReadDir…) across the whole plugin, not just within one file's download.
//
// This file adds the cache the official MooseFS client keeps for exactly
// this purpose — mfsclient/chunksdatacache.{c,h} (find/insert/invalidate),
// indexed by (inode, chindx). Issue #160 already ported the *invalidation*
// half of that model (a retry re-queries the master instead of reusing a
// cached location, ecclient.go / client.go) without the cache it was meant
// to invalidate; chunkLocationCache is the other half.
//
// Design notes:
//   - Bounded LRU + TTL, not an unbounded map: a mass copy of thousands of
//     files must not let this grow forever (#163 CA6).
//   - Guarded by its own mutex, distinct from Client.mu: a cache hit never
//     touches the master connection at all, so concurrent reads across
//     different files no longer serialize behind it (the dominant bottleneck
//     identified for #163).
//   - No separate "chunk version" validation before use: the wire protocol
//     already carries the cached Version in every CLTOCS_READ request
//     (csclient.go ReadChunk), so a stale entry naturally surfaces as a
//     non-OK server status from the chunk server — which is already a
//     read error, already routed through doCSRead's re-locate-on-retry path
//     (locateChunk with forceRefresh=true). Re-validating the version against
//     the master before every use would require a master roundtrip on every
//     read, which defeats the point of caching at all.
package mfsclient

import (
	"container/list"
	"sync"
	"time"
)

// chunkLocationCacheMaxEntries bounds the number of (nodeID, chunkIndex)
// entries kept at once. Each entry is a handful of ChunkServer structs
// (a few hundred bytes at most), so this is a generous ceiling chosen to
// comfortably cover a mass copy of thousands of files without unbounded
// growth (#163 CA6) — not a value requiring precise tuning.
const chunkLocationCacheMaxEntries = 8192

// chunkLocationCacheTTL bounds how long a cached location is trusted without
// being refreshed. As with maxIdleConnAge (csclient.go), the real MooseFS
// master has no client-observable "location changed" push notification
// available here, so this is a conservative default rather than a measured
// server-side value — it exists as defense in depth on top of the
// error-driven invalidation path (a stale location fails at the chunk server
// and forces a fresh locateChunk long before 30s in the common case).
const chunkLocationCacheTTL = 30 * time.Second

// chunkLocationKey identifies one chunk within one file.
type chunkLocationKey struct {
	nodeID     uint32
	chunkIndex uint32
}

type chunkLocationEntry struct {
	key       chunkLocationKey
	info      *ChunkInfo
	expiresAt time.Time
}

// chunkLocationCache is a bounded, TTL-aware LRU cache of ChunkInfo (chunk
// placement, version, EC geometry) indexed by (nodeID, chunkIndex).
// Safe for concurrent use.
type chunkLocationCache struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	ll         *list.List // front = most recently used
	items      map[chunkLocationKey]*list.Element
}

func newChunkLocationCache(maxEntries int, ttl time.Duration) *chunkLocationCache {
	return &chunkLocationCache{
		maxEntries: maxEntries,
		ttl:        ttl,
		ll:         list.New(),
		items:      make(map[chunkLocationKey]*list.Element),
	}
}

// find returns the cached ChunkInfo for key, or (nil, false) on a miss or an
// expired entry (which is evicted as a side effect of the lookup).
func (c *chunkLocationCache) find(key chunkLocationKey) (*ChunkInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*chunkLocationEntry)
	if c.ttl > 0 && time.Now().After(entry.expiresAt) {
		c.ll.Remove(el)
		delete(c.items, key)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return entry.info, true
}

// insert adds or refreshes the cached entry for key, evicting the least
// recently used entry if the cache is at capacity.
func (c *chunkLocationCache) insert(key chunkLocationKey, info *ChunkInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiresAt := time.Now().Add(c.ttl)
	if el, ok := c.items[key]; ok {
		entry := el.Value.(*chunkLocationEntry)
		entry.info = info
		entry.expiresAt = expiresAt
		c.ll.MoveToFront(el)
		return
	}

	el := c.ll.PushFront(&chunkLocationEntry{key: key, info: info, expiresAt: expiresAt})
	c.items[key] = el

	if c.maxEntries > 0 && c.ll.Len() > c.maxEntries {
		oldest := c.ll.Back()
		if oldest != nil {
			c.ll.Remove(oldest)
			delete(c.items, oldest.Value.(*chunkLocationEntry).key)
		}
	}
}

// invalidate removes any cached entry for key. Safe to call on a key that
// is not cached (no-op).
func (c *chunkLocationCache) invalidate(key chunkLocationKey) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		c.ll.Remove(el)
		delete(c.items, key)
	}
}

// len returns the number of entries currently cached (test/diagnostic use).
func (c *chunkLocationCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
