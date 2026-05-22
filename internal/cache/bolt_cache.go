package cache

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketChunks = []byte("chunks")
	bucketLRU    = []byte("lru")
	bucketLRUIdx = []byte("lru_idx") // inverse index: chunk_key → lru_composite_key
	bucketMeta   = []byte("meta")

	keyTotalSize = []byte("total_size_bytes")
	keyHitCount  = []byte("hit_count")
	keyMissCount = []byte("miss_count")
)

type boltCache struct {
	db       *bolt.DB
	ttl      time.Duration
	maxBytes int64
}

// NewBoltCache creates a ChunkCache backed by BoltDB at dbPath.
// ttl is the duration before a chunk expires (0 = no TTL).
// maxBytes is the maximum total data size before LRU eviction (0 = no limit).
func NewBoltCache(dbPath string, ttl time.Duration, maxBytes int64) (ChunkCache, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("cache: mkdir %s: %w", filepath.Dir(dbPath), err)
	}
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("cache: open %s: %w", dbPath, err)
	}
	// Ensure all buckets exist.
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketChunks, bucketLRU, bucketLRUIdx, bucketMeta} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %s: %w", name, err)
			}
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("cache: init buckets: %w", err)
	}
	return &boltCache{db: db, ttl: ttl, maxBytes: maxBytes}, nil
}

// chunkKeyBytes builds the canonical BoltDB key for a chunk.
// Format: "<backendID>\x00<remotePath>\x00<offset16hexchars>"
// The zero-padded hex offset ensures lexicographic sort order.
func chunkKeyBytes(backendID, remotePath string, offset int64) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%016x", backendID, remotePath, uint64(offset)))
}

// encodeInt64 returns v as 8 big-endian bytes.
func encodeInt64(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

// decodeInt64 reads an int64 from 8 big-endian bytes.
func decodeInt64(b []byte) int64 {
	if len(b) < 8 {
		return 0
	}
	return int64(binary.BigEndian.Uint64(b))
}

func readMeta(bkt *bolt.Bucket, key []byte) int64 {
	return decodeInt64(bkt.Get(key))
}

func writeMeta(bkt *bolt.Bucket, key []byte, val int64) error {
	return bkt.Put(key, encodeInt64(val))
}

func serializeEntry(e ChunkEntry) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(e); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func deserializeEntry(data []byte) (ChunkEntry, error) {
	var e ChunkEntry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&e); err != nil {
		return e, err
	}
	return e, nil
}

// Get looks up a chunk. Returns (entry, true) on cache hit.
// Returns (nil, false) on: miss, TTL expiry, or ETag/mtime mismatch.
// Expired entries are lazily deleted.
func (c *boltCache) Get(ctx context.Context, key ChunkKey, currentETag string, currentMTime time.Time) (*ChunkEntry, bool) {
	var entry ChunkEntry
	var found bool

	_ = c.db.Update(func(tx *bolt.Tx) error {
		chunks := tx.Bucket(bucketChunks)
		meta := tx.Bucket(bucketMeta)
		k := chunkKeyBytes(key.BackendID, key.RemotePath, key.Offset)
		v := chunks.Get(k)
		if v == nil {
			_ = writeMeta(meta, keyMissCount, readMeta(meta, keyMissCount)+1)
			return nil
		}

		e, err := deserializeEntry(v)
		if err != nil {
			// Corrupt entry — remove it.
			_ = chunks.Delete(k)
			_ = writeMeta(meta, keyMissCount, readMeta(meta, keyMissCount)+1)
			return nil
		}

		// TTL check.
		if c.ttl > 0 && time.Since(e.StoredAt) > c.ttl {
			_ = chunks.Delete(k)
			_ = writeMeta(meta, keyTotalSize, readMeta(meta, keyTotalSize)-e.Size)
			_ = writeMeta(meta, keyMissCount, readMeta(meta, keyMissCount)+1)
			return nil
		}

		// ETag invalidation: if both the stored and current ETags are non-empty
		// and differ, the chunk is stale.
		if currentETag != "" && e.ETag != "" && e.ETag != currentETag {
			_ = chunks.Delete(k)
			_ = writeMeta(meta, keyTotalSize, readMeta(meta, keyTotalSize)-e.Size)
			_ = writeMeta(meta, keyMissCount, readMeta(meta, keyMissCount)+1)
			return nil
		}

		// MTime invalidation: if both are non-zero and differ, chunk is stale.
		if !currentMTime.IsZero() && !e.MTime.IsZero() && !e.MTime.Equal(currentMTime) {
			_ = chunks.Delete(k)
			_ = writeMeta(meta, keyTotalSize, readMeta(meta, keyTotalSize)-e.Size)
			_ = writeMeta(meta, keyMissCount, readMeta(meta, keyMissCount)+1)
			return nil
		}

		_ = writeMeta(meta, keyHitCount, readMeta(meta, keyHitCount)+1)
		entry = e
		found = true
		return nil
	})

	if !found {
		return nil, false
	}
	return &entry, true
}

// Put stores a chunk and triggers LRU eviction if total size exceeds maxBytes.
func (c *boltCache) Put(ctx context.Context, key ChunkKey, entry ChunkEntry) error {
	entry.StoredAt = time.Now()
	entry.Size = int64(len(entry.Data))

	data, err := serializeEntry(entry)
	if err != nil {
		return fmt.Errorf("cache: serialize: %w", err)
	}

	return c.db.Update(func(tx *bolt.Tx) error {
		chunks := tx.Bucket(bucketChunks)
		lru := tx.Bucket(bucketLRU)
		lruIdx := tx.Bucket(bucketLRUIdx)
		meta := tx.Bucket(bucketMeta)

		k := chunkKeyBytes(key.BackendID, key.RemotePath, key.Offset)

		// If key already exists, remove the old LRU entry and subtract old size.
		if old := chunks.Get(k); old != nil {
			if oldEntry, err := deserializeEntry(old); err == nil {
				_ = writeMeta(meta, keyTotalSize, readMeta(meta, keyTotalSize)-oldEntry.Size)
			}
			// Remove the old LRU entry using the inverse index.
			if oldLRUKey := lruIdx.Get(k); oldLRUKey != nil {
				_ = lru.Delete(oldLRUKey)
				_ = lruIdx.Delete(k)
			}
		}

		// Write LRU entry: composite key (timestamp_ns + chunk_key_bytes) → chunk key.
		// The composite key guarantees uniqueness even when two Put calls share the same nanosecond.
		tsKey := append(encodeInt64(entry.StoredAt.UnixNano()), k...)
		if putErr := lru.Put(tsKey, k); putErr != nil {
			return putErr
		}
		// Maintain inverse index: chunk_key → lru_composite_key.
		if putErr := lruIdx.Put(k, tsKey); putErr != nil {
			return putErr
		}

		if putErr := chunks.Put(k, data); putErr != nil {
			return putErr
		}

		newTotal := readMeta(meta, keyTotalSize) + entry.Size
		_ = writeMeta(meta, keyTotalSize, newTotal)

		// LRU eviction if over capacity.
		if c.maxBytes > 0 && newTotal > c.maxBytes {
			return c.evictTx(tx, newTotal-c.maxBytes)
		}
		return nil
	})
}

// evictTx removes the oldest entries from the LRU bucket until bytesToFree are freed.
// Must be called within an active bolt Update transaction.
func (c *boltCache) evictTx(tx *bolt.Tx, bytesToFree int64) error {
	chunks := tx.Bucket(bucketChunks)
	lru := tx.Bucket(bucketLRU)
	lruIdx := tx.Bucket(bucketLRUIdx)
	meta := tx.Bucket(bucketMeta)

	freed := int64(0)
	cur := lru.Cursor()
	var lruKeysToDelete [][]byte

	for tsKey, chunkKey := cur.First(); tsKey != nil && freed < bytesToFree; tsKey, chunkKey = cur.Next() {
		if raw := chunks.Get(chunkKey); raw != nil {
			if e, err := deserializeEntry(raw); err == nil {
				_ = chunks.Delete(chunkKey)
				freed += e.Size
				_ = writeMeta(meta, keyTotalSize, readMeta(meta, keyTotalSize)-e.Size)
			}
		}
		// Remove from inverse index.
		_ = lruIdx.Delete(chunkKey)
		lruKeysToDelete = append(lruKeysToDelete, tsKey)
	}

	for _, k := range lruKeysToDelete {
		_ = lru.Delete(k)
	}
	return nil
}

// Invalidate removes all cached chunks for a specific remote file.
// It also removes the corresponding LRU and lru_idx entries to prevent stale accumulation.
func (c *boltCache) Invalidate(ctx context.Context, backendID, remotePath string) error {
	prefix := []byte(fmt.Sprintf("%s\x00%s\x00", backendID, remotePath))
	return c.db.Update(func(tx *bolt.Tx) error {
		return c.invalidateByPrefix(tx, prefix)
	})
}

// InvalidateBackend removes all cached chunks for a backend.
// It also removes the corresponding LRU and lru_idx entries to prevent stale accumulation.
func (c *boltCache) InvalidateBackend(ctx context.Context, backendID string) error {
	prefix := []byte(backendID + "\x00")
	return c.db.Update(func(tx *bolt.Tx) error {
		return c.invalidateByPrefix(tx, prefix)
	})
}

// invalidateByPrefix deletes all chunks whose key starts with prefix, along with
// their corresponding LRU and lru_idx entries.
func (c *boltCache) invalidateByPrefix(tx *bolt.Tx, prefix []byte) error {
	chunks := tx.Bucket(bucketChunks)
	lru := tx.Bucket(bucketLRU)
	lruIdx := tx.Bucket(bucketLRUIdx)
	meta := tx.Bucket(bucketMeta)

	cur := chunks.Cursor()
	freed := int64(0)
	var toDelete [][]byte

	for k, v := cur.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = cur.Next() {
		if e, err := deserializeEntry(v); err == nil {
			freed += e.Size
		}
		// Collect a copy of the key (cursor value may change across deletes).
		kCopy := make([]byte, len(k))
		copy(kCopy, k)
		toDelete = append(toDelete, kCopy)
	}

	for _, k := range toDelete {
		_ = chunks.Delete(k)
		// Remove the corresponding LRU entry via the inverse index.
		if lruKey := lruIdx.Get(k); lruKey != nil {
			_ = lru.Delete(lruKey)
			_ = lruIdx.Delete(k)
		}
	}

	_ = writeMeta(meta, keyTotalSize, readMeta(meta, keyTotalSize)-freed)
	return nil
}

// Stats returns current cache metrics from the meta bucket.
func (c *boltCache) Stats() CacheStats {
	var s CacheStats
	_ = c.db.View(func(tx *bolt.Tx) error {
		chunks := tx.Bucket(bucketChunks)
		meta := tx.Bucket(bucketMeta)
		s.Entries = int64(chunks.Stats().KeyN)
		s.SizeBytes = readMeta(meta, keyTotalSize)
		s.Hits = readMeta(meta, keyHitCount)
		s.Misses = readMeta(meta, keyMissCount)
		return nil
	})
	return s
}

// Close closes the underlying BoltDB database.
func (c *boltCache) Close() error {
	return c.db.Close()
}
