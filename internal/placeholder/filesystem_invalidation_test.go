//go:build windows

package placeholder

// Tests for cache invalidation triggered by VFS write operations
// (Rename, Create, Unlink, Mkdir) and for the configurable TTL.
//
// All tests are whitebox (package placeholder) so they can access the
// unexported metaCache fields and reuse mockStorageBackend / newTestFS
// defined in metacache_vfs_test.go.

import (
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── TTL from BackendConfig.Params ────────────────────────────────────────────

// TestGhostFileSystem_MetaCacheTTL_FromParams verifies that the metaCacheTTL
// parameter in BackendConfig.Params is read by newGhostFileSystem and used as
// the metadata cache TTL (plan criterion: TTL fallback configurable via Params).
func TestGhostFileSystem_MetaCacheTTL_FromParams(t *testing.T) {
	const wantSecs = 7 // arbitrary non-default value
	backendID := "ttl-backend"
	mb := MountedBackend{
		ID:      backendID,
		Name:    "TTLBackend",
		Backend: &mockStorageBackend{},
		Config: plugins.BackendConfig{
			ID:     backendID,
			Name:   "TTLBackend",
			Params: map[string]string{"metaCacheTTL": "7"},
		},
	}
	fs := newGhostFileSystem([]MountedBackend{mb}, nil)

	assert.Equal(t, time.Duration(wantSecs)*time.Second, fs.meta.ttl,
		"BackendConfig.Params[\"metaCacheTTL\"] must configure the cache TTL (seconds)")
}

// TestGhostFileSystem_MetaCacheTTL_Default verifies that when no metaCacheTTL
// param is set, the cache uses the defaultMetaCacheTTL (5 minutes).
func TestGhostFileSystem_MetaCacheTTL_Default(t *testing.T) {
	fs := newTestFS(&mockStorageBackend{}, &mockEventEmitter{})
	assert.Equal(t, defaultMetaCacheTTL, fs.meta.ttl,
		"when BackendConfig.Params[\"metaCacheTTL\"] is absent, defaultMetaCacheTTL must be used")
}

// ── VFS write-operation invalidation ─────────────────────────────────────────

// TestRename_InvalidatesCacheForOldAndNewPaths verifies that a successful
// Rename() call invalidates all four cache keys:
//   - old path, parent(old path), new path, parent(new path)
//
// Plan criterion: "Cache invalidé immédiatement après Move".
func TestRename_InvalidatesCacheForOldAndNewPaths(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	id := "test-backend"
	// Populate all four cache entries that Rename must invalidate.
	fs.meta.putStat(id+":/docs/old.txt", &plugins.FileInfo{Name: "old.txt", Size: 100})
	fs.meta.putList(id+":/docs", []plugins.FileInfo{{Name: "old.txt"}})
	fs.meta.putStat(id+":/archive/new.txt", &plugins.FileInfo{Name: "new.txt", Size: 100})
	fs.meta.putList(id+":/archive", []plugins.FileInfo{{Name: "new.txt"}})

	ret := fs.Rename("/docs/old.txt", "/archive/new.txt")
	require.Equal(t, 0, ret, "Rename must succeed with a no-op mock backend")

	_, ok1 := fs.meta.getStat(id + ":/docs/old.txt")
	assert.False(t, ok1, "Rename must invalidate the old file path")
	_, ok2 := fs.meta.getList(id + ":/docs")
	assert.False(t, ok2, "Rename must invalidate the old parent directory")
	_, ok3 := fs.meta.getStat(id + ":/archive/new.txt")
	assert.False(t, ok3, "Rename must invalidate the new file path")
	_, ok4 := fs.meta.getList(id + ":/archive")
	assert.False(t, ok4, "Rename must invalidate the new parent directory")
}

// TestCreate_InvalidatesParentDirCache verifies that Create() invalidates the
// parent directory's List cache entry, so that Readdir reflects the new file.
//
// Plan criterion: "Cache invalidé immédiatement après CreateDir (écriture VFS)".
func TestCreate_InvalidatesParentDirCache(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	// Populate the root directory listing cache (parent of "/newfile.txt").
	parentKey := "test-backend:/"
	fs.meta.putList(parentKey, []plugins.FileInfo{{Name: "existing.txt", Size: 10}})

	// Create a new file — must invalidate the parent directory cache.
	ret, fh := fs.Create("/newfile.txt", 0, 0644)
	require.Equal(t, 0, ret, "Create must succeed with a no-op mock backend")
	require.NotEqual(t, ^uint64(0), fh, "Create must return a valid file handle")
	defer fs.Release("/newfile.txt", fh) // clean up handle

	_, hit := fs.meta.getList(parentKey)
	assert.False(t, hit, "Create must invalidate the parent directory cache entry")
}

// TestUnlink_InvalidatesFileAndParentCache verifies that Unlink() invalidates
// both the deleted file's Stat cache entry and its parent directory's List entry.
//
// Plan criterion: "Cache invalidé immédiatement après Delete".
func TestUnlink_InvalidatesFileAndParentCache(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	fileKey := "test-backend:/docs/report.pdf"
	parentKey := "test-backend:/docs"
	fs.meta.putStat(fileKey, &plugins.FileInfo{Name: "report.pdf", Path: "/docs/report.pdf", Size: 512})
	fs.meta.putList(parentKey, []plugins.FileInfo{{Name: "report.pdf"}})

	ret := fs.Unlink("/docs/report.pdf")
	require.Equal(t, 0, ret, "Unlink must succeed with a no-op mock backend")

	_, fileHit := fs.meta.getStat(fileKey)
	assert.False(t, fileHit, "Unlink must invalidate the file's Stat cache entry")
	_, parentHit := fs.meta.getList(parentKey)
	assert.False(t, parentHit, "Unlink must invalidate the parent directory's List cache entry")
}

// TestMkdir_InvalidatesParentDirCache verifies that Mkdir() invalidates the
// parent directory's List cache entry, so that Readdir shows the new folder.
//
// Plan criterion: "Cache invalidé immédiatement après CreateDir".
func TestMkdir_InvalidatesParentDirCache(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	// Populate the parent directory listing cache.
	parentKey := "test-backend:/projects"
	fs.meta.putList(parentKey, []plugins.FileInfo{{Name: "existing-dir", IsDir: true}})

	ret := fs.Mkdir("/projects/new-subdir", 0755)
	require.Equal(t, 0, ret, "Mkdir must succeed with a no-op mock backend")

	_, hit := fs.meta.getList(parentKey)
	assert.False(t, hit, "Mkdir must invalidate the parent directory's List cache entry")
}

// ── meta:updated emission from VFS callbacks (#116) ──────────────────────────

// TestCreate_EmitsMetaUpdated verifies that Create() emits a "meta:updated"
// event with eventType="created" after creating a new file (#116).
func TestCreate_EmitsMetaUpdated(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	ret, fh := fs.Create("/newfile.txt", 0, 0644)
	require.Equal(t, 0, ret, "Create must succeed")
	defer fs.Release("/newfile.txt", fh)

	require.Equal(t, 1, emitter.len(), "Create must emit exactly one meta:updated event")
	ev := emitter.get(0)
	assert.Equal(t, "meta:updated", ev.name)
	payload, ok := ev.payload.(MetaUpdatedEvent)
	require.True(t, ok, "payload must be MetaUpdatedEvent")
	assert.Equal(t, "test-backend", payload.BackendID)
	assert.Equal(t, "/newfile.txt", payload.Path)
	assert.Equal(t, "created", payload.EventType)
}

// TestUnlink_EmitsMetaUpdated verifies that Unlink() emits a "meta:updated"
// event with eventType="deleted" (#116).
func TestUnlink_EmitsMetaUpdated(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	ret := fs.Unlink("/docs/report.pdf")
	require.Equal(t, 0, ret, "Unlink must succeed")

	require.Equal(t, 1, emitter.len(), "Unlink must emit exactly one meta:updated event")
	ev := emitter.get(0)
	assert.Equal(t, "meta:updated", ev.name)
	payload, ok := ev.payload.(MetaUpdatedEvent)
	require.True(t, ok, "payload must be MetaUpdatedEvent")
	assert.Equal(t, "test-backend", payload.BackendID)
	assert.Equal(t, "/docs/report.pdf", payload.Path)
	assert.Equal(t, "deleted", payload.EventType)
}

// TestRename_EmitsMetaUpdated verifies that Rename() emits a "meta:updated"
// event with eventType="renamed" and the new path (#116).
func TestRename_EmitsMetaUpdated(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	ret := fs.Rename("/docs/old.txt", "/archive/new.txt")
	require.Equal(t, 0, ret, "Rename must succeed")

	require.Equal(t, 1, emitter.len(), "Rename must emit exactly one meta:updated event")
	ev := emitter.get(0)
	assert.Equal(t, "meta:updated", ev.name)
	payload, ok := ev.payload.(MetaUpdatedEvent)
	require.True(t, ok, "payload must be MetaUpdatedEvent")
	assert.Equal(t, "test-backend", payload.BackendID)
	assert.Equal(t, "/archive/new.txt", payload.Path, "Rename must emit the new (destination) path")
	assert.Equal(t, "renamed", payload.EventType)
}

// TestMkdir_EmitsMetaUpdated verifies that Mkdir() emits a "meta:updated"
// event with eventType="created" after creating a directory (#116).
func TestMkdir_EmitsMetaUpdated(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	ret := fs.Mkdir("/projects/new-subdir", 0755)
	require.Equal(t, 0, ret, "Mkdir must succeed")

	require.Equal(t, 1, emitter.len(), "Mkdir must emit exactly one meta:updated event")
	ev := emitter.get(0)
	assert.Equal(t, "meta:updated", ev.name)
	payload, ok := ev.payload.(MetaUpdatedEvent)
	require.True(t, ok, "payload must be MetaUpdatedEvent")
	assert.Equal(t, "test-backend", payload.BackendID)
	assert.Equal(t, "/projects/new-subdir", payload.Path)
	assert.Equal(t, "created", payload.EventType)
}
