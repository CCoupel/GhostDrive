package plugins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Stub backend for registry tests ─────────────────────────────────────────

// stubBackend is a minimal StorageBackend whose Name() is configurable.
type stubBackend struct{ name string }

func (s *stubBackend) Name() string                       { return s.name }
func (s *stubBackend) Connect(_ BackendConfig) error      { return nil }
func (s *stubBackend) Disconnect() error                  { return nil }
func (s *stubBackend) IsConnected() bool                  { return true }
func (s *stubBackend) Upload(_ context.Context, _, _ string, _ ProgressCallback) error {
	return nil
}
func (s *stubBackend) Download(_ context.Context, _, _ string, _ ProgressCallback) error {
	return nil
}
func (s *stubBackend) Delete(_ context.Context, _ string) error { return nil }
func (s *stubBackend) Move(_ context.Context, _, _ string) error { return nil }
func (s *stubBackend) List(_ context.Context, _ string) ([]FileInfo, error) {
	return nil, nil
}
func (s *stubBackend) Stat(_ context.Context, _ string) (*FileInfo, error) { return nil, nil }
func (s *stubBackend) CreateDir(_ context.Context, _ string) error          { return nil }
func (s *stubBackend) Watch(_ context.Context, _ string) (<-chan FileEvent, error) {
	return nil, nil
}
func (s *stubBackend) GetQuota(_ context.Context) (int64, int64, error) { return -1, -1, nil }

// ─── Test helper ─────────────────────────────────────────────────────────────

// isolatedRegistry swaps out the global registry for the duration of a test
// and restores it on cleanup.  Tests using this helper must NOT be run in
// parallel, since they share the package-level registry variable.
func isolatedRegistry(t *testing.T) {
	t.Helper()
	registryMu.Lock()
	saved := registry
	registry = map[string]func() StorageBackend{}
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	})
}

// ─── Tests ───────────────────────────────────────────────────────────────────

func TestRegister_And_Get_OK(t *testing.T) {
	isolatedRegistry(t)

	Register("alpha", func() StorageBackend { return &stubBackend{name: "alpha"} })

	b, err := Get("alpha")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "alpha", b.Name())
}

func TestGet_Unknown_ReturnsError(t *testing.T) {
	isolatedRegistry(t)

	_, err := Get("does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown backend type")
	assert.Contains(t, err.Error(), "does-not-exist")
}

func TestRegister_Duplicate_Overwrites(t *testing.T) {
	isolatedRegistry(t)

	// Register factory A, then overwrite with factory B.
	Register("dup", func() StorageBackend { return &stubBackend{name: "first"} })
	Register("dup", func() StorageBackend { return &stubBackend{name: "second"} })

	b, err := Get("dup")
	require.NoError(t, err)
	// The second registration should win.
	assert.Equal(t, "second", b.Name())
}

func TestGet_Returns_New_Instance_Each_Call(t *testing.T) {
	isolatedRegistry(t)

	Register("singleton-test", func() StorageBackend { return &stubBackend{name: "singleton-test"} })

	b1, err1 := Get("singleton-test")
	b2, err2 := Get("singleton-test")
	require.NoError(t, err1)
	require.NoError(t, err2)
	// Factory must produce distinct instances.
	assert.NotSame(t, b1, b2)
}

func TestListBackends_Sorted(t *testing.T) {
	isolatedRegistry(t)

	Register("zzz", func() StorageBackend { return &stubBackend{name: "zzz"} })
	Register("aaa", func() StorageBackend { return &stubBackend{name: "aaa"} })
	Register("mmm", func() StorageBackend { return &stubBackend{name: "mmm"} })

	names := ListBackends()
	assert.Equal(t, []string{"aaa", "mmm", "zzz"}, names)
}

func TestListBackends_Empty(t *testing.T) {
	isolatedRegistry(t)

	names := ListBackends()
	assert.NotNil(t, names)
	assert.Empty(t, names)
}

func TestListBackends_IsFreshSlice(t *testing.T) {
	isolatedRegistry(t)

	Register("x", func() StorageBackend { return &stubBackend{name: "x"} })

	a := ListBackends()
	b := ListBackends()
	// Mutating one slice must not affect the other.
	a[0] = "mutated"
	assert.NotEqual(t, "mutated", b[0])
}
