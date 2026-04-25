package backends

import (
	"context"
	"testing"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBackend is a minimal StorageBackend stub for testing.
type mockBackend struct {
	connected bool
}

func (m *mockBackend) Name() string                       { return "mock" }
func (m *mockBackend) Connect(_ plugins.BackendConfig) error { m.connected = true; return nil }
func (m *mockBackend) Disconnect() error                  { m.connected = false; return nil }
func (m *mockBackend) IsConnected() bool                  { return m.connected }
func (m *mockBackend) Upload(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}
func (m *mockBackend) Download(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}
func (m *mockBackend) Delete(_ context.Context, _ string) error { return nil }
func (m *mockBackend) Move(_ context.Context, _, _ string) error { return nil }
func (m *mockBackend) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	return nil, nil
}
func (m *mockBackend) Stat(_ context.Context, _ string) (*plugins.FileInfo, error) { return nil, nil }
func (m *mockBackend) CreateDir(_ context.Context, _ string) error                  { return nil }
func (m *mockBackend) Watch(_ context.Context, _ string) (<-chan plugins.FileEvent, error) {
	return make(chan plugins.FileEvent), nil
}
func (m *mockBackend) GetQuota(_ context.Context) (int64, int64, error) { return -1, -1, nil }

// mockBackendFactory temporarily overrides InstantiateBackend for tests.
func withMockFactory(t *testing.T) func() {
	t.Helper()
	// We test via Add() directly by patching the factory at instantiation level.
	// Since InstantiateBackend is a package-level func we test it separately.
	return func() {}
}

func newTestManager() *BackendManager {
	return NewBackendManager(nil)
}

func mockConfig(id string) plugins.BackendConfig {
	return plugins.BackendConfig{
		ID:         id,
		Name:       "Test Backend",
		Type:       "webdav",
		Enabled:    true,
		SyncDir:    "/tmp/sync",
		RemotePath: "/GhostDrive",
		Params: map[string]string{
			"url":      "https://nas.local/dav",
			"username": "user",
			"password": "pass",
		},
	}
}

func TestBackendManagerAddAndGet(t *testing.T) {
	m := newTestManager()

	// Manually inject a mock backend (bypassing InstantiateBackend which needs real plugins)
	bc := mockConfig("id-1")
	mock := &mockBackend{}
	require.NoError(t, mock.Connect(bc))

	m.mu.Lock()
	m.backends["id-1"] = mock
	m.configs["id-1"] = bc
	m.mu.Unlock()

	b, ok := m.Get("id-1")
	require.True(t, ok)
	assert.True(t, b.IsConnected())

	cfg, ok := m.GetConfig("id-1")
	require.True(t, ok)
	assert.Equal(t, "id-1", cfg.ID)
	assert.Equal(t, "/GhostDrive", cfg.RemotePath)
}

func TestBackendManagerRemove(t *testing.T) {
	m := newTestManager()

	bc := mockConfig("id-2")
	mock := &mockBackend{connected: true}

	m.mu.Lock()
	m.backends["id-2"] = mock
	m.configs["id-2"] = bc
	m.mu.Unlock()

	require.NoError(t, m.Remove("id-2"))

	_, ok := m.Get("id-2")
	assert.False(t, ok, "backend should not be found after removal")
	assert.False(t, mock.IsConnected(), "backend should be disconnected after removal")
}

func TestBackendManagerRemoveNotFound(t *testing.T) {
	m := newTestManager()
	err := m.Remove("nonexistent-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestBackendManagerList(t *testing.T) {
	m := newTestManager()

	for _, id := range []string{"a", "b", "c"} {
		m.mu.Lock()
		m.backends[id] = &mockBackend{connected: true}
		m.configs[id] = mockConfig(id)
		m.mu.Unlock()
	}

	ids := m.List()
	assert.Len(t, ids, 3)
}

func TestBackendManagerListStatuses(t *testing.T) {
	m := newTestManager()

	m.mu.Lock()
	m.backends["s1"] = &mockBackend{connected: true}
	m.configs["s1"] = mockConfig("s1")
	m.backends["s2"] = &mockBackend{connected: false}
	m.configs["s2"] = mockConfig("s2")
	m.mu.Unlock()

	statuses := m.ListStatuses()
	require.Len(t, statuses, 2)

	connected := 0
	for _, s := range statuses {
		if s.Connected {
			connected++
		}
	}
	assert.Equal(t, 1, connected)
}

func TestInstantiateBackendUnknownType(t *testing.T) {
	bc := plugins.BackendConfig{Type: "unknown-type"}
	_, err := InstantiateBackend(bc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type")
}

// TestInstantiateBackendLocal verifies that the "local" plugin is available via
// the registry (plugins/local registers itself in its init()).
func TestInstantiateBackendLocal(t *testing.T) {
	bc := plugins.BackendConfig{Type: "local"}
	b, err := InstantiateBackend(bc)
	require.NoError(t, err)
	assert.NotNil(t, b)
	assert.Equal(t, "local", b.Name())
}

// TestAvailableTypes verifies that AvailableTypes returns at least the "local"
// backend (the only one registered at this milestone).
func TestAvailableTypes(t *testing.T) {
	types := AvailableTypes()
	assert.Contains(t, types, "local")
}

// TestAvailableTypes_NameConsistency verifies that, for every type returned by
// AvailableTypes(), the registry key matches the value returned by Name() on a
// freshly instantiated backend.  This catches mismatches between the key passed
// to plugins.Register() and the Name() implementation.
func TestAvailableTypes_NameConsistency(t *testing.T) {
	for _, typeName := range AvailableTypes() {
		t.Run(typeName, func(t *testing.T) {
			b, err := InstantiateBackend(plugins.BackendConfig{Type: typeName})
			require.NoError(t, err, "type %q must be instantiable", typeName)
			assert.Equal(t, typeName, b.Name(),
				"registry key %q must match backend.Name()", typeName)
		})
	}
}

// TestGetAvailableBackendTypes_NoDelegationBreak verifies the full delegation
// chain: GetAvailableBackendTypes (Wails binding) → AvailableTypes (manager)
// → ListBackends (registry).  The list must not be empty and must not be
// produced by a hardcoded fallback.
func TestGetAvailableBackendTypes_NoDelegationBreak(t *testing.T) {
	// AvailableTypes() is the Go equivalent of the Wails GetAvailableBackendTypes().
	types := AvailableTypes()
	assert.NotEmpty(t, types, "at least one plugin must be registered")
	// Verify the list comes from the live registry, not a hardcoded fallback.
	assert.Equal(t, types, plugins.ListBackends(),
		"AvailableTypes() must equal plugins.ListBackends() exactly")
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()
	assert.NotEmpty(t, id1)
	assert.NotEqual(t, id1, id2, "generated IDs must be unique")
	assert.Len(t, id1, 32, "hex-encoded 16 bytes = 32 chars")
}
