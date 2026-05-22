package cache

import (
	"context"
	"testing"
	"time"
)

func TestNoopCache(t *testing.T) {
	c := NewNoopCache()

	ctx := context.Background()
	key := ChunkKey{BackendID: "b1", RemotePath: "/foo.txt", Offset: 0}
	entry := ChunkEntry{Data: []byte("hello"), ETag: "abc", MTime: time.Now()}

	// Put should never fail.
	if err := c.Put(ctx, key, entry); err != nil {
		t.Errorf("Put: unexpected error: %v", err)
	}

	// Get should always return a miss (noop cache never stores anything).
	got, ok := c.Get(ctx, key, entry.ETag, entry.MTime)
	if ok || got != nil {
		t.Errorf("Get: expected cache miss, got hit")
	}

	// Invalidate should never fail.
	if err := c.Invalidate(ctx, "b1", "/foo.txt"); err != nil {
		t.Errorf("Invalidate: unexpected error: %v", err)
	}
	if err := c.InvalidateBackend(ctx, "b1"); err != nil {
		t.Errorf("InvalidateBackend: unexpected error: %v", err)
	}

	// Stats: misses should be non-zero after Get.
	s := c.Stats()
	if s.Misses == 0 {
		t.Errorf("Stats: expected Misses > 0, got %d", s.Misses)
	}
	if s.Hits != 0 {
		t.Errorf("Stats: expected Hits == 0, got %d", s.Hits)
	}

	// Close should never fail.
	if err := c.Close(); err != nil {
		t.Errorf("Close: unexpected error: %v", err)
	}
}
