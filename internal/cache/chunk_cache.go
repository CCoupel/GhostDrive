// Package cache implements a BoltDB-backed chunk cache for GhostDrive Files On-Demand.
// A chunk is an aligned byte slice read from a remote backend during CF API hydration.
package cache

import (
	"context"
	"sync/atomic"
	"time"
)

// ChunkKey identifies a chunk in the cache.
type ChunkKey struct {
	BackendID  string
	RemotePath string
	Offset     int64
}

// ChunkEntry is the value stored in BoltDB for a cached chunk.
type ChunkEntry struct {
	Data     []byte
	ETag     string    // FileInfo.Version for invalidation
	MTime    time.Time // for mtime-based invalidation
	StoredAt time.Time // for TTL
	Size     int64     // len(Data) — redundant but useful for stats without decoding
}

// CacheStats exposes observable cache metrics.
type CacheStats struct {
	Entries   int64
	SizeBytes int64
	Hits      int64
	Misses    int64
}

// ChunkCache is the interface for the chunk cache.
type ChunkCache interface {
	// Get returns the chunk if present, not expired, and ETag/mtime still valid.
	Get(ctx context.Context, key ChunkKey, currentETag string, currentMTime time.Time) (*ChunkEntry, bool)
	// Put stores a chunk. Triggers LRU eviction if total size exceeds maxBytes.
	Put(ctx context.Context, key ChunkKey, entry ChunkEntry) error
	// Invalidate removes all chunks for a specific file.
	Invalidate(ctx context.Context, backendID, remotePath string) error
	// InvalidateBackend removes all chunks for a backend.
	InvalidateBackend(ctx context.Context, backendID string) error
	// Stats returns current cache metrics.
	Stats() CacheStats
	// Close closes the underlying storage.
	Close() error
}

// NewNoopCache returns a disabled cache that always misses (Put is a no-op).
// Used when AppConfig.CacheEnabled == false.
func NewNoopCache() ChunkCache {
	return &noopCache{}
}

type noopCache struct {
	misses int64
}

func (n *noopCache) Get(_ context.Context, _ ChunkKey, _ string, _ time.Time) (*ChunkEntry, bool) {
	atomic.AddInt64(&n.misses, 1)
	return nil, false
}

func (n *noopCache) Put(_ context.Context, _ ChunkKey, _ ChunkEntry) error {
	return nil
}

func (n *noopCache) Invalidate(_ context.Context, _, _ string) error {
	return nil
}

func (n *noopCache) InvalidateBackend(_ context.Context, _ string) error {
	return nil
}

func (n *noopCache) Stats() CacheStats {
	return CacheStats{Misses: atomic.LoadInt64(&n.misses)}
}

func (n *noopCache) Close() error {
	return nil
}
