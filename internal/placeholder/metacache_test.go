//go:build windows

package placeholder

import (
	"fmt"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeFileInfo(name string, size int64) *plugins.FileInfo {
	return &plugins.FileInfo{Name: name, Path: "/" + name, Size: size, ModTime: time.Now()}
}

// TestMetaCache_Hit verifies that a Stat entry put before TTL can be retrieved.
func TestMetaCache_Hit(t *testing.T) {
	c := newMetaCache(5 * time.Minute)
	fi := makeFileInfo("file.txt", 1024)
	c.putStat("backend1:/file.txt", fi)

	got, ok := c.getStat("backend1:/file.txt")
	require.True(t, ok, "getStat must return true for a fresh entry")
	assert.Equal(t, fi.Name, got.Name)
	assert.Equal(t, fi.Size, got.Size)
}

// TestMetaCache_Miss_Expired verifies that an expired entry returns a miss.
func TestMetaCache_Miss_Expired(t *testing.T) {
	c := newMetaCache(1 * time.Millisecond) // very short TTL
	fi := makeFileInfo("doc.pdf", 2048)
	c.putStat("backend1:/doc.pdf", fi)

	time.Sleep(5 * time.Millisecond) // wait for expiry

	_, ok := c.getStat("backend1:/doc.pdf")
	assert.False(t, ok, "getStat must return false after TTL expiry")
}

// TestMetaCache_LRU_Eviction verifies that the oldest entry is evicted when
// the cache exceeds maxSize.
func TestMetaCache_LRU_Eviction(t *testing.T) {
	const capacity = 5
	c := newMetaCache(5 * time.Minute)
	c.maxSize = capacity // override for test

	// Fill cache to capacity.
	for i := 0; i < capacity; i++ {
		key := fmt.Sprintf("backend1:/file%d.txt", i)
		c.putStat(key, makeFileInfo(fmt.Sprintf("file%d.txt", i), int64(i)))
	}

	// Access key 0 to make it recently-used (it should survive eviction).
	_, ok := c.getStat("backend1:/file0.txt")
	require.True(t, ok)

	// Adding one more entry must evict the LRU entry (which is now file1.txt,
	// since file0.txt was just accessed).
	c.putStat("backend1:/fileNew.txt", makeFileInfo("fileNew.txt", 999))

	// file1.txt (LRU at insertion of fileNew) must be gone.
	_, evicted := c.getStat("backend1:/file1.txt")
	assert.False(t, evicted, "LRU entry must be evicted when capacity is exceeded")

	// file0.txt (recently accessed) must still be present.
	_, survives := c.getStat("backend1:/file0.txt")
	assert.True(t, survives, "recently accessed entry must survive LRU eviction")

	// fileNew.txt must be present.
	_, newPresent := c.getStat("backend1:/fileNew.txt")
	assert.True(t, newPresent, "newly inserted entry must be present after eviction")
}

// TestMetaCache_Invalidate verifies that invalidate() removes the entry.
func TestMetaCache_Invalidate(t *testing.T) {
	c := newMetaCache(5 * time.Minute)
	fi := makeFileInfo("notes.txt", 512)
	c.putStat("backend1:/notes.txt", fi)

	c.invalidate("backend1:/notes.txt")

	_, ok := c.getStat("backend1:/notes.txt")
	assert.False(t, ok, "getStat must return false after explicit invalidation")
}

// TestMetaCache_InvalidatePrefix verifies that invalidatePrefix() removes all
// matching keys (e.g. invalidating a directory and its children).
func TestMetaCache_InvalidatePrefix(t *testing.T) {
	c := newMetaCache(5 * time.Minute)

	// Insert entries for two different directories.
	c.putStat("backend1:/docs/report.pdf", makeFileInfo("report.pdf", 1024))
	c.putStat("backend1:/docs/slides.pptx", makeFileInfo("slides.pptx", 2048))
	c.putStat("backend1:/images/photo.jpg", makeFileInfo("photo.jpg", 4096))

	// Invalidate the /docs subtree.
	c.invalidatePrefix("backend1:/docs/")

	_, ok1 := c.getStat("backend1:/docs/report.pdf")
	_, ok2 := c.getStat("backend1:/docs/slides.pptx")
	_, ok3 := c.getStat("backend1:/images/photo.jpg")

	assert.False(t, ok1, "report.pdf must be invalidated")
	assert.False(t, ok2, "slides.pptx must be invalidated")
	assert.True(t, ok3, "photo.jpg must NOT be invalidated (different prefix)")
}
