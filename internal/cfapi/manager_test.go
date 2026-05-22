package cfapi

import (
	"context"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/internal/cache"
	"github.com/CCoupel/GhostDrive/internal/config"
	"github.com/CCoupel/GhostDrive/plugins"
)

// mockBackend is a minimal StorageBackend for testing.
type mockBackend struct {
	name      string
	connected bool
}

func (m *mockBackend) Name() string                                                    { return m.name }
func (m *mockBackend) Version() string                                                 { return "1.0" }
func (m *mockBackend) Connect(_ plugins.BackendConfig) error                           { m.connected = true; return nil }
func (m *mockBackend) Disconnect() error                                               { m.connected = false; return nil }
func (m *mockBackend) IsConnected() bool                                               { return m.connected }
func (m *mockBackend) Upload(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}
func (m *mockBackend) Download(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}
func (m *mockBackend) Delete(_ context.Context, _ string) error              { return nil }
func (m *mockBackend) Move(_ context.Context, _, _ string) error             { return nil }
func (m *mockBackend) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	return nil, nil
}
func (m *mockBackend) Stat(_ context.Context, _ string) (*plugins.FileInfo, error) {
	return nil, nil
}
func (m *mockBackend) CreateDir(_ context.Context, _ string) error { return nil }
func (m *mockBackend) Watch(_ context.Context, _ string) (<-chan plugins.FileEvent, error) {
	ch := make(chan plugins.FileEvent)
	close(ch)
	return ch, nil
}
func (m *mockBackend) GetQuota(_ context.Context) (int64, int64, error)         { return -1, -1, nil }
func (m *mockBackend) ReadAt(_ context.Context, _ string, _, _ int64) ([]byte, error) {
	return nil, nil
}
func (m *mockBackend) ChunkSize() int64 { return 0 }
func (m *mockBackend) Describe() plugins.PluginDescriptor {
	return plugins.PluginDescriptor{Type: m.name}
}

// noopEmitter implements cfapi.EventEmitter.
type noopEmitter struct{}

func (n *noopEmitter) Emit(_ string, _ any) {}

func newTestManager(t *testing.T) *CFManager {
	t.Helper()
	cfg := &config.AppConfig{
		CloudProviderID:    "{test-provider-id}",
		ChunkCacheTTLHours: 24,
	}
	return NewCFManager(cfg, &noopEmitter{})
}

func TestCFManagerStartStop(t *testing.T) {
	m := newTestManager(t)

	b := &mockBackend{name: "test", connected: true}
	entry := BackendEntry{
		ID:        "backend-1",
		Name:      "TestBackend",
		Backend:   b,
		LocalPath: t.TempDir(),
	}

	// Start should succeed (no-op on Linux).
	if err := m.Start(entry, b, cache.NewNoopCache()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Starting again should be idempotent.
	if err := m.Start(entry, b, cache.NewNoopCache()); err != nil {
		t.Fatalf("Start (second): %v", err)
	}

	// Stop should succeed.
	if err := m.Stop(entry.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Stopping non-existent backend should be a no-op.
	if err := m.Stop("nonexistent"); err != nil {
		t.Fatalf("Stop nonexistent: %v", err)
	}
}

func TestCFManagerStartAll(t *testing.T) {
	m := newTestManager(t)

	entries := []BackendEntry{
		{ID: "b1", Name: "Backend1", Backend: &mockBackend{name: "b1", connected: true}, LocalPath: t.TempDir()},
		{ID: "b2", Name: "Backend2", Backend: &mockBackend{name: "b2", connected: true}, LocalPath: t.TempDir()},
	}
	caches := map[string]cache.ChunkCache{
		"b1": cache.NewNoopCache(),
		"b2": cache.NewNoopCache(),
	}

	if err := m.StartAll(entries, caches); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if err := m.StopAll(); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
}

func TestCFManagerSetSyncState_NoOp(t *testing.T) {
	m := newTestManager(t)

	// SetSyncState on unknown backend → silent no-op.
	if err := m.SetSyncState("unknown", "/path/file.txt", int(SyncStateSynced)); err != nil {
		t.Errorf("SetSyncState unknown: expected nil, got %v", err)
	}

	// Register a backend, then SetSyncState should succeed.
	b := &mockBackend{name: "x", connected: true}
	entry := BackendEntry{ID: "x", Name: "X", Backend: b, LocalPath: t.TempDir()}
	_ = m.Start(entry, b, nil)

	for _, state := range []SyncState{SyncStateCloudOnly, SyncStateSyncing, SyncStateSynced, SyncStatePinned, SyncStateUnpinned} {
		if err := m.SetSyncState("x", entry.LocalPath, int(state)); err != nil {
			t.Errorf("SetSyncState state=%d: %v", state, err)
		}
	}
}

func TestCFManagerStopAll(t *testing.T) {
	m := newTestManager(t)

	for i := 0; i < 3; i++ {
		b := &mockBackend{name: "b", connected: true}
		entry := BackendEntry{
			ID:        string(rune('a' + i)),
			Name:      "B",
			Backend:   b,
			LocalPath: t.TempDir(),
		}
		_ = m.Start(entry, b, nil)
	}

	if err := m.StopAll(); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	// Second StopAll should be a no-op.
	if err := m.StopAll(); err != nil {
		t.Fatalf("StopAll (second): %v", err)
	}
}

// TestSyncStateString verifies the string representation of SyncState values.
func TestSyncStateString(t *testing.T) {
	tests := []struct {
		state SyncState
		want  string
	}{
		{SyncStateCloudOnly, "cloud_only"},
		{SyncStateSyncing, "syncing"},
		{SyncStateSynced, "synced"},
		{SyncStatePinned, "pinned"},
		{SyncStateUnpinned, "unpinned"},
	}
	for _, tt := range tests {
		got := syncStateString(tt.state)
		if got != tt.want {
			t.Errorf("syncStateString(%d) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// TestNewHydratorChunkSize verifies defaultChunkSize fallback.
func TestNewHydratorChunkSize(t *testing.T) {
	b := &mockBackend{name: "t", connected: true}
	h := NewHydrator(b, nil, nil, nil, "backend-1")
	if h.chunkSize != defaultChunkSize {
		t.Errorf("chunkSize: got %d, want %d", h.chunkSize, defaultChunkSize)
	}
	_ = time.Now() // avoid unused import
}

func TestCFManagerWithNilCache(t *testing.T) {
	m := newTestManager(t)
	b := &mockBackend{name: "t", connected: true}
	entry := BackendEntry{ID: "nil-cache", Name: "NC", Backend: b, LocalPath: t.TempDir()}

	// nil cache should fall back to noop — no panic.
	if err := m.Start(entry, b, nil); err != nil {
		t.Fatalf("Start with nil cache: %v", err)
	}
	_ = m.Stop("nil-cache")
}

// TestCFManager_SetSyncState_UnknownBackend verifies that SetSyncState on a
// backend that was registered then stopped is a clean no-op (nil, no panic).
func TestCFManager_SetSyncState_UnknownBackend(t *testing.T) {
	m := newTestManager(t)

	b := &mockBackend{name: "x", connected: true}
	entry := BackendEntry{ID: "x-stopped", Name: "X", Backend: b, LocalPath: t.TempDir()}

	_ = m.Start(entry, b, nil)
	_ = m.Stop("x-stopped") // backend is now unregistered

	// After Stop, SetSyncState must return nil without panicking.
	if err := m.SetSyncState("x-stopped", entry.LocalPath, int(SyncStateSynced)); err != nil {
		t.Errorf("SetSyncState on stopped backend: expected nil, got %v", err)
	}
}

// TestCFManager_StartAll_PartialFailure verifies that when one backend out of
// three fails Start (missing LocalPath), the other two are still registered and
// operational.  StartAll itself must return nil.
func TestCFManager_StartAll_PartialFailure(t *testing.T) {
	emitter := &countingEmitter{} // defined in hydrator_test.go (same package)
	cfg := &config.AppConfig{CloudProviderID: "{test}", ChunkCacheTTLHours: 24}
	m := NewCFManager(cfg, emitter)

	entries := []BackendEntry{
		{ID: "b1", Name: "B1", Backend: &mockBackend{name: "b1", connected: true}, LocalPath: t.TempDir()},
		{ID: "b2-fail", Name: "B2 (no LocalPath)", Backend: &mockBackend{name: "b2", connected: true}, LocalPath: ""},
		{ID: "b3", Name: "B3", Backend: &mockBackend{name: "b3", connected: true}, LocalPath: t.TempDir()},
	}

	// StartAll must return nil even when one backend fails to register.
	if err := m.StartAll(entries, nil); err != nil {
		t.Fatalf("StartAll: expected nil, got %v", err)
	}

	// SetSyncState on b1 and b3 (registered) → emitter called twice.
	// SetSyncState on b2-fail (not registered) → silent no-op, no emit.
	_ = m.SetSyncState("b1", entries[0].LocalPath, int(SyncStateSynced))
	_ = m.SetSyncState("b2-fail", "/some/path", int(SyncStateSynced))
	_ = m.SetSyncState("b3", entries[2].LocalPath, int(SyncStateSynced))

	emitter.mu.Lock()
	count := emitter.count
	emitter.mu.Unlock()

	if count != 2 {
		t.Errorf("StartAll partial failure: expected 2 cf:sync_state events (b1+b3), got %d", count)
	}

	_ = m.StopAll()
}

// TestProviderStub verifies all stub methods return nil on non-Windows.
func TestProviderStub(t *testing.T) {
	p := NewSyncProvider("/tmp/test", "{00000000-0000-0000-0000-000000000000}", "Test")
	if err := p.Register(); err != nil {
		t.Errorf("Register: %v", err)
	}
	if err := p.Connect(CFCallbacks{}); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect: %v", err)
	}
	if err := p.SetSyncState("/tmp/test/file.txt", SyncStateSynced); err != nil {
		t.Errorf("SetSyncState: %v", err)
	}
	if err := p.Deregister(); err != nil {
		t.Errorf("Deregister: %v", err)
	}
	n, err := p.CreatePlaceholders("/tmp/test", []PlaceholderInfo{{RelativePath: "foo.txt", FileSize: 100}})
	if err != nil || n != 0 {
		t.Errorf("CreatePlaceholders: n=%d err=%v", n, err)
	}
	req := FetchRequest{LocalPath: "/tmp/test/file.txt"}
	if err := p.ExecuteTransfer(req, []byte("data"), true); err != nil {
		t.Errorf("ExecuteTransfer: %v", err)
	}
	if err := p.ReportError(req, nil); err != nil {
		t.Errorf("ReportError: %v", err)
	}
	if err := p.ReportProgress(req, 1024, 512); err != nil {
		t.Errorf("ReportProgress: %v", err)
	}
}
