package main

import (
	"context"
	"sync"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
)

// MockPlugin is a no-op implementation of plugins.StorageBackend for use in
// integration tests. All write operations succeed without performing any I/O.
// Name() returns "mock" -- this value must match the expected plugin name in
// loader integration tests.
type MockPlugin struct {
	mu        sync.Mutex
	connected bool
}

// ── Identification ────────────────────────────────────────────────────────────

// Name returns "mock". Immutable; safe to call before Connect.
func (m *MockPlugin) Name() string { return "mock" }

// ── Connection ────────────────────────────────────────────────────────────────

// Connect marks the plugin as connected. Accepts any config without validation.
func (m *MockPlugin) Connect(_ plugins.BackendConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = true
	return nil
}

// Disconnect marks the plugin as disconnected.
func (m *MockPlugin) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	return nil
}

// IsConnected returns the current connection state. Thread-safe.
func (m *MockPlugin) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

// ── File operations ───────────────────────────────────────────────────────────

// Upload is a no-op that always returns nil (success).
func (m *MockPlugin) Upload(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}

// Download is a no-op that always returns nil (success).
func (m *MockPlugin) Download(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}

// Delete is a no-op that always returns nil (success).
func (m *MockPlugin) Delete(_ context.Context, _ string) error { return nil }

// Move is a no-op that always returns nil (success).
func (m *MockPlugin) Move(_ context.Context, _, _ string) error { return nil }

// ── Navigation ────────────────────────────────────────────────────────────────

// List returns an empty slice (no files). Never returns ErrFileNotFound.
func (m *MockPlugin) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	return []plugins.FileInfo{}, nil
}

// Stat returns a minimal FileInfo for path. Never returns ErrFileNotFound.
func (m *MockPlugin) Stat(_ context.Context, path string) (*plugins.FileInfo, error) {
	return &plugins.FileInfo{
		Name:    "mock",
		Path:    path,
		ModTime: time.Now(),
	}, nil
}

// CreateDir is a no-op that always returns nil (success).
func (m *MockPlugin) CreateDir(_ context.Context, _ string) error { return nil }

// ── Watch ─────────────────────────────────────────────────────────────────────

// Watch returns an open channel that emits no events and closes when ctx is
// cancelled. Buffer size 64 absorbs burst events from the loader.
func (m *MockPlugin) Watch(ctx context.Context, _ string) (<-chan plugins.FileEvent, error) {
	ch := make(chan plugins.FileEvent, 64)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// ── Quota ─────────────────────────────────────────────────────────────────────

// GetQuota reports that quota is not supported by returning (-1, -1, nil).
func (m *MockPlugin) GetQuota(_ context.Context) (free, total int64, err error) {
	return -1, -1, nil
}
