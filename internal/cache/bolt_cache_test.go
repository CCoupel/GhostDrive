package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempDB(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "chunks.db")
	return dbPath, func() { os.RemoveAll(dir) }
}

func TestBoltCacheGetPut(t *testing.T) {
	dbPath, cleanup := tempDB(t)
	defer cleanup()

	c, err := NewBoltCache(dbPath, 0, 0)
	if err != nil {
		t.Fatalf("NewBoltCache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	key := ChunkKey{BackendID: "b1", RemotePath: "/doc.txt", Offset: 0}
	data := []byte("hello world")
	mtime := time.Now().Truncate(time.Second)

	entry := ChunkEntry{Data: data, ETag: "etag1", MTime: mtime}
	if err := c.Put(ctx, key, entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok := c.Get(ctx, key, "etag1", mtime)
	if !ok {
		t.Fatal("Get: expected cache hit")
	}
	if string(got.Data) != string(data) {
		t.Errorf("Get: data mismatch: got %q, want %q", got.Data, data)
	}
	if got.ETag != "etag1" {
		t.Errorf("Get: ETag mismatch: got %q, want %q", got.ETag, "etag1")
	}

	// Stats: 1 hit.
	s := c.Stats()
	if s.Hits < 1 {
		t.Errorf("Stats.Hits: got %d, want >= 1", s.Hits)
	}
}

func TestBoltCacheTTL(t *testing.T) {
	dbPath, cleanup := tempDB(t)
	defer cleanup()

	// TTL of 1 nanosecond → expires immediately.
	c, err := NewBoltCache(dbPath, time.Nanosecond, 0)
	if err != nil {
		t.Fatalf("NewBoltCache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	key := ChunkKey{BackendID: "b1", RemotePath: "/file.txt", Offset: 0}
	entry := ChunkEntry{Data: []byte("data"), ETag: "e1", MTime: time.Now()}

	if err := c.Put(ctx, key, entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Sleep a tiny bit to guarantee TTL expiry.
	time.Sleep(5 * time.Millisecond)

	got, ok := c.Get(ctx, key, "e1", entry.MTime)
	if ok || got != nil {
		t.Error("Get: expected TTL-expired miss, got hit")
	}
}

func TestBoltCacheETagInvalidation(t *testing.T) {
	dbPath, cleanup := tempDB(t)
	defer cleanup()

	c, err := NewBoltCache(dbPath, 0, 0)
	if err != nil {
		t.Fatalf("NewBoltCache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	mtime := time.Now().Truncate(time.Second)
	key := ChunkKey{BackendID: "b1", RemotePath: "/file.txt", Offset: 0}
	entry := ChunkEntry{Data: []byte("v1"), ETag: "etag-v1", MTime: mtime}

	if err := c.Put(ctx, key, entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Get with same ETag → hit.
	got, ok := c.Get(ctx, key, "etag-v1", mtime)
	if !ok || got == nil {
		t.Fatal("Get with same ETag: expected hit")
	}

	// Get with different ETag → miss (file was updated remotely).
	got, ok = c.Get(ctx, key, "etag-v2", mtime)
	if ok || got != nil {
		t.Error("Get with different ETag: expected miss")
	}
}

func TestBoltCacheLRUEviction(t *testing.T) {
	dbPath, cleanup := tempDB(t)
	defer cleanup()

	// Max 100 bytes.
	c, err := NewBoltCache(dbPath, 0, 100)
	if err != nil {
		t.Fatalf("NewBoltCache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	mtime := time.Now()

	// Put 3 × 40-byte entries — total 120 bytes > 100 → should evict oldest.
	for i := 0; i < 3; i++ {
		key := ChunkKey{BackendID: "b1", RemotePath: "/file.txt", Offset: int64(i * 4096)}
		entry := ChunkEntry{
			Data:  make([]byte, 40),
			ETag:  "e1",
			MTime: mtime,
		}
		if err := c.Put(ctx, key, entry); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
		// Small sleep so timestamps differ for LRU ordering.
		time.Sleep(time.Millisecond)
	}

	s := c.Stats()
	// After eviction, total size must not exceed maxBytes.
	if s.SizeBytes > 100 {
		t.Errorf("LRU eviction: SizeBytes=%d > maxBytes=100", s.SizeBytes)
	}
}

func TestBoltCacheClose_AfterClose(t *testing.T) {
	dbPath, cleanup := tempDB(t)
	defer cleanup()

	c, err := NewBoltCache(dbPath, 0, 0)
	if err != nil {
		t.Fatalf("NewBoltCache: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second Close must not panic (may return an error from bbolt).
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("second Close panicked: %v", r)
		}
	}()
	_ = c.Close()
}

func TestBoltCachePut_ZeroBytes(t *testing.T) {
	dbPath, cleanup := tempDB(t)
	defer cleanup()

	c, err := NewBoltCache(dbPath, 0, 0)
	if err != nil {
		t.Fatalf("NewBoltCache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	mtime := time.Now().Truncate(time.Second)
	key := ChunkKey{BackendID: "b1", RemotePath: "/empty.bin", Offset: 0}
	entry := ChunkEntry{Data: []byte{}, ETag: "etag-zero", MTime: mtime}

	if err := c.Put(ctx, key, entry); err != nil {
		t.Fatalf("Put zero-byte chunk: %v", err)
	}

	got, ok := c.Get(ctx, key, "etag-zero", mtime)
	if !ok || got == nil {
		t.Fatal("Get: expected cache hit for zero-byte chunk")
	}
	if len(got.Data) != 0 {
		t.Errorf("Get: expected empty Data, got %d bytes", len(got.Data))
	}
}

func TestBoltCacheStats(t *testing.T) {
	dbPath, cleanup := tempDB(t)
	defer cleanup()

	c, err := NewBoltCache(dbPath, 0, 0)
	if err != nil {
		t.Fatalf("NewBoltCache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	mtime := time.Now().Truncate(time.Second)
	key1 := ChunkKey{BackendID: "b1", RemotePath: "/a.txt", Offset: 0}
	key2 := ChunkKey{BackendID: "b1", RemotePath: "/b.txt", Offset: 0}

	_ = c.Put(ctx, key1, ChunkEntry{Data: []byte("data1"), ETag: "e1", MTime: mtime})
	_ = c.Put(ctx, key2, ChunkEntry{Data: []byte("data2"), ETag: "e2", MTime: mtime})

	// 2 hits: key1 accessed twice.
	c.Get(ctx, key1, "e1", mtime)
	c.Get(ctx, key1, "e1", mtime)
	// 1 miss: key that was never inserted.
	c.Get(ctx, ChunkKey{BackendID: "b1", RemotePath: "/missing.txt", Offset: 0}, "e3", mtime)

	s := c.Stats()
	if s.Hits < 2 {
		t.Errorf("Stats.Hits = %d, want >= 2", s.Hits)
	}
	if s.Misses < 1 {
		t.Errorf("Stats.Misses = %d, want >= 1", s.Misses)
	}
	if s.Entries < 2 {
		t.Errorf("Stats.Entries = %d, want >= 2", s.Entries)
	}
}

func TestBoltCacheInvalidate_SpecificFile(t *testing.T) {
	dbPath, cleanup := tempDB(t)
	defer cleanup()

	c, err := NewBoltCache(dbPath, 0, 0)
	if err != nil {
		t.Fatalf("NewBoltCache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	mtime := time.Now().Truncate(time.Second)

	// Insert 2 chunks for fileA and 2 for fileB in the same backend.
	for i := 0; i < 2; i++ {
		key := ChunkKey{BackendID: "b1", RemotePath: "/fileA.txt", Offset: int64(i * 4096)}
		_ = c.Put(ctx, key, ChunkEntry{Data: []byte("dataA"), ETag: "eA", MTime: mtime})
	}
	for i := 0; i < 2; i++ {
		key := ChunkKey{BackendID: "b1", RemotePath: "/fileB.txt", Offset: int64(i * 4096)}
		_ = c.Put(ctx, key, ChunkEntry{Data: []byte("dataB"), ETag: "eB", MTime: mtime})
	}

	// Invalidate only fileA — fileB must survive.
	if err := c.Invalidate(ctx, "b1", "/fileA.txt"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	// fileA chunks must be misses.
	for i := 0; i < 2; i++ {
		key := ChunkKey{BackendID: "b1", RemotePath: "/fileA.txt", Offset: int64(i * 4096)}
		if _, ok := c.Get(ctx, key, "eA", mtime); ok {
			t.Errorf("fileA chunk %d: expected miss after Invalidate, got hit", i)
		}
	}

	// fileB chunks must still be hits.
	for i := 0; i < 2; i++ {
		key := ChunkKey{BackendID: "b1", RemotePath: "/fileB.txt", Offset: int64(i * 4096)}
		if _, ok := c.Get(ctx, key, "eB", mtime); !ok {
			t.Errorf("fileB chunk %d: expected hit after fileA Invalidate, got miss", i)
		}
	}
}

func TestBoltCacheInvalidateBackend(t *testing.T) {
	dbPath, cleanup := tempDB(t)
	defer cleanup()

	c, err := NewBoltCache(dbPath, 0, 0)
	if err != nil {
		t.Fatalf("NewBoltCache: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	mtime := time.Now()

	// Insert 5 chunks for backend "b1" and 2 for "b2".
	for i := 0; i < 5; i++ {
		key := ChunkKey{BackendID: "b1", RemotePath: "/file.txt", Offset: int64(i * 1024)}
		_ = c.Put(ctx, key, ChunkEntry{Data: []byte("data"), ETag: "e1", MTime: mtime})
	}
	for i := 0; i < 2; i++ {
		key := ChunkKey{BackendID: "b2", RemotePath: "/other.txt", Offset: int64(i * 1024)}
		_ = c.Put(ctx, key, ChunkEntry{Data: []byte("data2"), ETag: "e2", MTime: mtime})
	}

	// Invalidate backend "b1" — only b2 chunks should remain.
	if err := c.InvalidateBackend(ctx, "b1"); err != nil {
		t.Fatalf("InvalidateBackend: %v", err)
	}

	// All b1 keys should be misses now.
	for i := 0; i < 5; i++ {
		key := ChunkKey{BackendID: "b1", RemotePath: "/file.txt", Offset: int64(i * 1024)}
		if _, ok := c.Get(ctx, key, "e1", mtime); ok {
			t.Errorf("b1 key %d: expected miss after InvalidateBackend", i)
		}
	}

	// b2 keys should still be hits.
	for i := 0; i < 2; i++ {
		key := ChunkKey{BackendID: "b2", RemotePath: "/other.txt", Offset: int64(i * 1024)}
		if _, ok := c.Get(ctx, key, "e2", mtime); !ok {
			t.Errorf("b2 key %d: expected hit, got miss", i)
		}
	}
}
