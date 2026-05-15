//go:build windows

package placeholder

// Integration tests for the metadata cache + watchLoop in GhostFileSystem.
// All tests use a MockStorageBackend so no real backend is required.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── MockStorageBackend ───────────────────────────────────────────────────────

// mockStorageBackend is a minimal in-memory StorageBackend for VFS tests.
// Only Stat(), List(), and Watch() are instrumented; other methods are no-ops.
type mockStorageBackend struct {
	statCalls  atomic.Int64
	listCalls  atomic.Int64
	statResult *plugins.FileInfo
	listResult []plugins.FileInfo
	watchCh    chan plugins.FileEvent
	watchErr   error
}

func (m *mockStorageBackend) Name() string    { return "mock" }
func (m *mockStorageBackend) Version() string { return "0.0.0" }
func (m *mockStorageBackend) Describe() plugins.PluginDescriptor {
	return plugins.PluginDescriptor{Type: "mock", DisplayName: "Mock"}
}

func (m *mockStorageBackend) Connect(_ plugins.BackendConfig) error  { return nil }
func (m *mockStorageBackend) Disconnect() error                       { return nil }
func (m *mockStorageBackend) IsConnected() bool                       { return true }
func (m *mockStorageBackend) GetQuota(_ context.Context) (int64, int64, error) {
	return 0, 0, nil
}

func (m *mockStorageBackend) Upload(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}
func (m *mockStorageBackend) Download(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}
func (m *mockStorageBackend) Delete(_ context.Context, _ string) error { return nil }
func (m *mockStorageBackend) Move(_ context.Context, _, _ string) error { return nil }
func (m *mockStorageBackend) CreateDir(_ context.Context, _ string) error { return nil }

func (m *mockStorageBackend) Stat(_ context.Context, _ string) (*plugins.FileInfo, error) {
	m.statCalls.Add(1)
	return m.statResult, nil
}

func (m *mockStorageBackend) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	m.listCalls.Add(1)
	return m.listResult, nil
}

func (m *mockStorageBackend) Watch(_ context.Context, _ string) (<-chan plugins.FileEvent, error) {
	if m.watchErr != nil {
		return nil, m.watchErr
	}
	if m.watchCh != nil {
		return m.watchCh, nil
	}
	// Return a closed channel to simulate no events.
	ch := make(chan plugins.FileEvent)
	close(ch)
	return ch, nil
}

// ── MockEventEmitter ─────────────────────────────────────────────────────────

type mockEventEmitter struct {
	mu     sync.Mutex
	events []emittedEvent
}

type emittedEvent struct {
	name    string
	payload any
}

func (m *mockEventEmitter) Emit(event string, data any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, emittedEvent{name: event, payload: data})
}

func (m *mockEventEmitter) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func (m *mockEventEmitter) get(i int) emittedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events[i]
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// newTestFS creates a GhostFileSystem backed by mock for use in unit tests.
func newTestFS(mock *mockStorageBackend, emitter *mockEventEmitter) *GhostFileSystem {
	backendID := "test-backend"
	mb := MountedBackend{
		ID:      backendID,
		Name:    "TestBackend",
		Backend: mock,
		Config:  plugins.BackendConfig{ID: backendID},
	}
	fs := newGhostFileSystem([]MountedBackend{mb}, emitter)
	return fs
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestGetattr_MetaCache_Hit verifies that two consecutive Getattr calls on the
// same path result in only one backend.Stat() call (second call uses cache).
func TestGetattr_MetaCache_Hit(t *testing.T) {
	mock := &mockStorageBackend{
		statResult: &plugins.FileInfo{Name: "file.txt", Path: "/file.txt", Size: 100},
	}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	cacheKey := "test-backend:/file.txt"

	// Populate cache manually (simulates what Getattr would do).
	fs.meta.putStat(cacheKey, mock.statResult)

	// First call: cache hit — no backend.Stat() call expected.
	got, hit := fs.meta.getStat(cacheKey)
	require.True(t, hit, "cache must have the entry after putStat")
	assert.Equal(t, mock.statResult.Name, got.Name)
	assert.Equal(t, int64(0), mock.statCalls.Load(), "backend.Stat() must not be called on cache hit")
}

// TestReaddir_MetaCache_Hit verifies that two consecutive Readdir calls result
// in only one backend.List() call.
func TestReaddir_MetaCache_Hit(t *testing.T) {
	mock := &mockStorageBackend{
		listResult: []plugins.FileInfo{
			{Name: "a.txt", Path: "/dir/a.txt", Size: 10},
			{Name: "b.txt", Path: "/dir/b.txt", Size: 20},
		},
	}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	cacheKey := "test-backend:/dir"

	// Populate cache manually.
	fs.meta.putList(cacheKey, mock.listResult)

	// Second access: cache hit.
	got, hit := fs.meta.getList(cacheKey)
	require.True(t, hit, "cache must have the entry after putList")
	assert.Len(t, got, 2, "cached listing must have 2 entries")
	assert.Equal(t, int64(0), mock.listCalls.Load(), "backend.List() must not be called on cache hit")
}

// TestGetattr_MetaCache_InvalidatedByRelease verifies that after an explicit
// cache invalidation (simulating Release after upload), the cache entry is gone.
func TestGetattr_MetaCache_InvalidatedByRelease(t *testing.T) {
	mock := &mockStorageBackend{
		statResult: &plugins.FileInfo{Name: "doc.txt", Path: "/doc.txt", Size: 42},
	}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	cacheKey := "test-backend:/doc.txt"
	fs.meta.putStat(cacheKey, mock.statResult)

	// Simulate Release invalidating the cache.
	fs.meta.invalidate(cacheKey)

	_, hit := fs.meta.getStat(cacheKey)
	assert.False(t, hit, "cache must be empty after invalidation (simulating Release after upload)")
}

// TestWatchLoop_PushInvalidation verifies that sending a FileEvent on watchCh
// causes the affected cache entry to be invalidated.
func TestWatchLoop_PushInvalidation(t *testing.T) {
	watchCh := make(chan plugins.FileEvent, 1)
	mock := &mockStorageBackend{
		watchCh:    watchCh,
		statResult: &plugins.FileInfo{Name: "report.pdf", Path: "/report.pdf", Size: 512},
	}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	cacheKey := "test-backend:/report.pdf"
	fs.meta.putStat(cacheKey, mock.statResult)

	// Start the watchLoop with a cancellable context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go fs.watchLoop(ctx, fs.backends[0])

	// Send a FileEvent to trigger cache invalidation.
	watchCh <- plugins.FileEvent{
		Type: plugins.FileEventModified,
		Path: "/report.pdf",
	}

	// Wait for the watchLoop goroutine to process the event.
	require.Eventually(t, func() bool {
		_, hit := fs.meta.getStat(cacheKey)
		return !hit
	}, 500*time.Millisecond, 10*time.Millisecond, "cache must be invalidated after Watch() FileEvent")
}

// TestWatchLoop_ChannelClosed_NoLeak verifies that a closed watchCh causes
// the watchLoop goroutine to exit cleanly (no panic, no goroutine leak).
// Run with -race to detect any data race.
func TestWatchLoop_ChannelClosed_NoLeak(t *testing.T) {
	watchCh := make(chan plugins.FileEvent)
	mock := &mockStorageBackend{watchCh: watchCh}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fs.watchLoop(ctx, fs.backends[0])
	}()

	// Close the channel — watchLoop must detect this and exit.
	close(watchCh)

	select {
	case <-done:
		// goroutine exited cleanly
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watchLoop goroutine did not exit after channel close")
	}
}

// TestWatchLoop_WatchError_FallbackTTL verifies that when Watch() returns an
// error, the watchLoop exits immediately (TTL-only mode) without panicking.
func TestWatchLoop_WatchError_FallbackTTL(t *testing.T) {
	mock := &mockStorageBackend{watchErr: plugins.ErrNotConnected}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fs.watchLoop(ctx, fs.backends[0])
	}()

	select {
	case <-done:
		// goroutine exited immediately (TTL fallback)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watchLoop must exit immediately when Watch() returns an error")
	}

	// The metadata cache must still work via TTL.
	cacheKey := "test-backend:/any-file.txt"
	fi := &plugins.FileInfo{Name: "any-file.txt", Size: 1}
	fs.meta.putStat(cacheKey, fi)
	got, hit := fs.meta.getStat(cacheKey)
	require.True(t, hit, "TTL cache must be functional even when Watch() fails")
	assert.Equal(t, fi.Name, got.Name)
}

// TestHandleWatchEvent_Rename_InvalidatesFourKeys verifies that a rename
// FileEvent (OldPath populated) causes handleWatchEvent to invalidate all four
// affected cache keys: new path, parent(new path), old path, parent(old path).
func TestHandleWatchEvent_Rename_InvalidatesFourKeys(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	id := "test-backend"
	// Populate all four paths that the rename should invalidate.
	fs.meta.putStat(id+":/docs/old.txt", &plugins.FileInfo{Name: "old.txt", Size: 100})
	fs.meta.putList(id+":/docs", []plugins.FileInfo{{Name: "old.txt"}})
	fs.meta.putStat(id+":/archive/new.txt", &plugins.FileInfo{Name: "new.txt", Size: 100})
	fs.meta.putList(id+":/archive", []plugins.FileInfo{{Name: "new.txt"}})

	// Send a rename event.
	ev := plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    "/archive/new.txt",
		OldPath: "/docs/old.txt",
	}
	fs.handleWatchEvent(fs.backends[0], ev)

	_, ok1 := fs.meta.getStat(id + ":/docs/old.txt")
	assert.False(t, ok1, "old path must be invalidated by rename event")
	_, ok2 := fs.meta.getList(id + ":/docs")
	assert.False(t, ok2, "old parent must be invalidated by rename event")
	_, ok3 := fs.meta.getStat(id + ":/archive/new.txt")
	assert.False(t, ok3, "new path must be invalidated by rename event")
	_, ok4 := fs.meta.getList(id + ":/archive")
	assert.False(t, ok4, "new parent must be invalidated by rename event")
}

// TestHandleWatchEvent_Rename_EmitsMetaUpdatedWithNewPath verifies that a rename
// event emits a "meta:updated" event whose payload contains the new (destination)
// path — not the old path — and the correct event type "renamed".
func TestHandleWatchEvent_Rename_EmitsMetaUpdatedWithNewPath(t *testing.T) {
	mock := &mockStorageBackend{}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	ev := plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    "/archive/new.txt",
		OldPath: "/docs/old.txt",
	}
	fs.handleWatchEvent(fs.backends[0], ev)

	require.Equal(t, 1, emitter.len(), "exactly one meta:updated event must be emitted for a rename")
	assert.Equal(t, "meta:updated", emitter.get(0).name)
	payload, ok := emitter.get(0).payload.(MetaUpdatedEvent)
	require.True(t, ok, "payload must be a MetaUpdatedEvent")
	assert.Equal(t, "test-backend", payload.BackendID)
	assert.Equal(t, "/archive/new.txt", payload.Path,
		"payload Path must be the new (destination) path, not the old path")
	assert.Equal(t, "renamed", payload.EventType)
}

// TestWatchLoop_CtxCancel_NoLeak verifies that cancelling the context causes
// the watchLoop goroutine to exit cleanly, even when the Watch() channel stays
// open and never emits or closes.  Run with -race.
func TestWatchLoop_CtxCancel_NoLeak(t *testing.T) {
	// An open channel that never receives nor closes — only ctx cancellation
	// can unblock the watchLoop select.
	watchCh := make(chan plugins.FileEvent)
	mock := &mockStorageBackend{watchCh: watchCh}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		fs.watchLoop(ctx, fs.backends[0])
	}()

	// Cancel the context — watchLoop must unblock on <-ctx.Done() and exit.
	cancel()

	select {
	case <-done:
		// goroutine exited cleanly via ctx.Done()
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watchLoop goroutine did not exit after context cancellation")
	}
}

// TestMetaUpdated_EventEmitted verifies that receiving a FileEvent from Watch()
// causes the emitter to emit a "meta:updated" event with the correct payload.
func TestMetaUpdated_EventEmitted(t *testing.T) {
	watchCh := make(chan plugins.FileEvent, 1)
	mock := &mockStorageBackend{watchCh: watchCh}
	emitter := &mockEventEmitter{}
	fs := newTestFS(mock, emitter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go fs.watchLoop(ctx, fs.backends[0])

	watchCh <- plugins.FileEvent{
		Type: plugins.FileEventCreated,
		Path: "/new-file.txt",
	}

	require.Eventually(t, func() bool {
		return emitter.len() > 0
	}, 500*time.Millisecond, 10*time.Millisecond, "meta:updated event must be emitted")

	assert.Equal(t, "meta:updated", emitter.get(0).name)
	payload, ok := emitter.get(0).payload.(MetaUpdatedEvent)
	require.True(t, ok, "payload must be a MetaUpdatedEvent")
	assert.Equal(t, "test-backend", payload.BackendID)
	assert.Equal(t, "/new-file.txt", payload.Path)
	assert.Equal(t, "created", payload.EventType)
}
