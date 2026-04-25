package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBackend is a minimal in-memory StorageBackend for testing.
type mockBackend struct {
	files     map[string]plugins.FileInfo
	connected bool
}

func newMockBackend() *mockBackend {
	return &mockBackend{
		files:     map[string]plugins.FileInfo{},
		connected: true,
	}
}

func (m *mockBackend) Name() string { return "mock" }

func (m *mockBackend) Connect(_ plugins.BackendConfig) error {
	m.connected = true
	return nil
}

func (m *mockBackend) Disconnect() error {
	m.connected = false
	return nil
}

func (m *mockBackend) IsConnected() bool { return m.connected }

func (m *mockBackend) Upload(_ context.Context, local, remote string, _ plugins.ProgressCallback) error {
	data, err := os.ReadFile(local)
	if err != nil {
		return err
	}
	info, _ := os.Stat(local)
	m.files[remote] = plugins.FileInfo{
		Name:    filepath.Base(remote),
		Path:    remote,
		Size:    int64(len(data)),
		ModTime: info.ModTime(),
	}
	return nil
}

func (m *mockBackend) Download(_ context.Context, remote, local string, _ plugins.ProgressCallback) error {
	fi, ok := m.files[remote]
	if !ok {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return err
	}
	return os.WriteFile(local, make([]byte, fi.Size), 0644)
}

func (m *mockBackend) Delete(_ context.Context, remote string) error {
	delete(m.files, remote)
	return nil
}

func (m *mockBackend) Move(_ context.Context, old, newPath string) error {
	fi, ok := m.files[old]
	if !ok {
		return os.ErrNotExist
	}
	fi.Path = newPath
	m.files[newPath] = fi
	delete(m.files, old)
	return nil
}

func (m *mockBackend) List(_ context.Context, path string) ([]plugins.FileInfo, error) {
	var result []plugins.FileInfo
	for _, fi := range m.files {
		result = append(result, fi)
	}
	return result, nil
}

func (m *mockBackend) Stat(_ context.Context, path string) (*plugins.FileInfo, error) {
	fi, ok := m.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &fi, nil
}

func (m *mockBackend) CreateDir(_ context.Context, path string) error {
	m.files[path] = plugins.FileInfo{Path: path, IsDir: true}
	return nil
}

func (m *mockBackend) Watch(_ context.Context, _ string) (<-chan plugins.FileEvent, error) {
	ch := make(chan plugins.FileEvent)
	close(ch)
	return ch, nil
}

func (m *mockBackend) GetQuota(_ context.Context) (int64, int64, error) { return -1, -1, nil }

// addRemoteFile adds a file directly to the mock backend.
func (m *mockBackend) addRemoteFile(path string, size int64, modTime time.Time) {
	m.files[path] = plugins.FileInfo{
		Name:    filepath.Base(path),
		Path:    path,
		Size:    size,
		ModTime: modTime,
	}
}

// ─── Tests ───────────────────────────────────────────────────────────────────

func TestReconcileLocalOnlyFile(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// Create a local file that doesn't exist remotely
	localFile := filepath.Join(tmp, "newfile.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("hello"), 0644))

	r := NewReconciler(backend, tmp, "")
	actions, err := r.Reconcile(context.Background(), "/remote")
	require.NoError(t, err)

	require.Len(t, actions, 1)
	assert.Equal(t, ActionUpload, actions[0].Type)
	assert.Equal(t, localFile, actions[0].LocalPath)
}

func TestReconcileRemoteOnlyFile(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// Add a remote file that doesn't exist locally
	backend.addRemoteFile("/remote/newremote.txt", 100, time.Now())

	r := NewReconciler(backend, tmp, "")
	actions, err := r.Reconcile(context.Background(), "/remote")
	require.NoError(t, err)

	require.Len(t, actions, 1)
	assert.Equal(t, ActionDownload, actions[0].Type)
}

func TestReconcileNoChanges(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// File exists both locally and remotely with same modtime and size
	now := time.Now().Truncate(time.Second)
	content := []byte("same content")
	localFile := filepath.Join(tmp, "same.txt")
	require.NoError(t, os.WriteFile(localFile, content, 0644))

	// Force the modtime to match
	require.NoError(t, os.Chtimes(localFile, now, now))

	backend.addRemoteFile("/remote/same.txt", int64(len(content)), now)

	r := NewReconciler(backend, tmp, "")
	actions, err := r.Reconcile(context.Background(), "/remote")
	require.NoError(t, err)

	assert.Empty(t, actions, "no actions when files are identical")
}

func TestReconcileConflictLocalWins(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	logFile := filepath.Join(tmp, "sync.log")

	localContent := []byte("local version")
	localFile := filepath.Join(tmp, "conflict.txt")
	require.NoError(t, os.WriteFile(localFile, localContent, 0644))

	remoteModTime := time.Now().Add(-2 * time.Hour)
	localModTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(localFile, localModTime, localModTime))

	backend.addRemoteFile("/remote/conflict.txt", int64(len(localContent)), remoteModTime)

	r := NewReconciler(backend, tmp, logFile)
	actions, err := r.Reconcile(context.Background(), "/remote")
	require.NoError(t, err)

	require.Len(t, actions, 1)
	assert.Equal(t, ActionUpload, actions[0].Type, "local is newer → upload")

	// Verify conflict was logged
	logData, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(logData), "CONFLICT")
	assert.Contains(t, string(logData), "local-wins")
}

func TestReconcileConflictRemoteWins(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	localContent := []byte("old local version")
	localFile := filepath.Join(tmp, "conflict2.txt")
	require.NoError(t, os.WriteFile(localFile, localContent, 0644))

	// Remote is newer than local
	localModTime := time.Now().Add(-2 * time.Hour)
	remoteModTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(localFile, localModTime, localModTime))

	backend.addRemoteFile("/remote/conflict2.txt", int64(len(localContent)), remoteModTime)

	r := NewReconciler(backend, tmp, "")
	actions, err := r.Reconcile(context.Background(), "/remote")
	require.NoError(t, err)

	require.Len(t, actions, 1)
	assert.Equal(t, ActionDownload, actions[0].Type, "remote is newer → download")
}

func TestReconcileEmptyDirs(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// Empty local and remote → no actions
	r := NewReconciler(backend, tmp, "")
	actions, err := r.Reconcile(context.Background(), "/remote")
	require.NoError(t, err)
	assert.Empty(t, actions)
}
