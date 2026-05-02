package grpc_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	grpcbridge "github.com/CCoupel/GhostDrive/plugins/grpc"
	storagepb "github.com/CCoupel/GhostDrive/plugins/proto"
	"github.com/CCoupel/GhostDrive/plugins"
)

const bufSize = 1 << 20 // 1 MB in-process buffer

// newTestPair starts an in-process gRPC server backed by impl and returns a
// GRPCBackend client connected to it. The returned cleanup function stops the
// server and closes the connection.
func newTestPair(t *testing.T, impl plugins.StorageBackend) (*grpcbridge.GRPCBackend, func()) {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	storagepb.RegisterStorageServiceServer(srv, &grpcbridge.GRPCBackendServer{Impl: impl})

	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	backend := grpcbridge.NewGRPCBackend(conn)
	cleanup := func() {
		conn.Close()
		srv.GracefulStop()
		lis.Close()
	}
	return backend, cleanup
}

// ── Mock backend ──────────────────────────────────────────────────────────────

type mockBackend struct {
	connected bool
	files     map[string][]byte // remote path → content
}

func newMockBackend() *mockBackend {
	return &mockBackend{files: make(map[string][]byte)}
}

func (m *mockBackend) Name() string { return "mock" }

func (m *mockBackend) Connect(cfg plugins.BackendConfig) error {
	m.connected = true
	return nil
}
func (m *mockBackend) Disconnect() error {
	m.connected = false
	return nil
}
func (m *mockBackend) IsConnected() bool { return m.connected }

func (m *mockBackend) Upload(_ context.Context, local, remote string, progress plugins.ProgressCallback) error {
	if !m.connected {
		return fmt.Errorf("mock: %w", plugins.ErrNotConnected)
	}
	data, err := os.ReadFile(local)
	if err != nil {
		return err
	}
	m.files[remote] = data
	if progress != nil {
		progress(int64(len(data)), int64(len(data)))
	}
	return nil
}

func (m *mockBackend) Download(_ context.Context, remote, local string, progress plugins.ProgressCallback) error {
	if !m.connected {
		return fmt.Errorf("mock: %w", plugins.ErrNotConnected)
	}
	data, ok := m.files[remote]
	if !ok {
		return fmt.Errorf("mock: %w", plugins.ErrFileNotFound)
	}
	if err := os.WriteFile(local, data, 0644); err != nil {
		return err
	}
	if progress != nil {
		progress(int64(len(data)), int64(len(data)))
	}
	return nil
}

func (m *mockBackend) Delete(_ context.Context, remote string) error {
	if !m.connected {
		return fmt.Errorf("mock: %w", plugins.ErrNotConnected)
	}
	if _, ok := m.files[remote]; !ok {
		return fmt.Errorf("mock: %w", plugins.ErrFileNotFound)
	}
	delete(m.files, remote)
	return nil
}

func (m *mockBackend) Move(_ context.Context, oldPath, newPath string) error {
	if !m.connected {
		return fmt.Errorf("mock: %w", plugins.ErrNotConnected)
	}
	data, ok := m.files[oldPath]
	if !ok {
		return fmt.Errorf("mock: %w", plugins.ErrFileNotFound)
	}
	m.files[newPath] = data
	delete(m.files, oldPath)
	return nil
}

func (m *mockBackend) List(_ context.Context, path string) ([]plugins.FileInfo, error) {
	if !m.connected {
		return nil, fmt.Errorf("mock: %w", plugins.ErrNotConnected)
	}
	return []plugins.FileInfo{
		{Name: "file.txt", Path: path + "/file.txt", Size: 10, IsDir: false, ModTime: time.Now()},
	}, nil
}

func (m *mockBackend) Stat(_ context.Context, path string) (*plugins.FileInfo, error) {
	if !m.connected {
		return nil, fmt.Errorf("mock: %w", plugins.ErrNotConnected)
	}
	if _, ok := m.files[path]; !ok && path != "/" {
		return nil, fmt.Errorf("mock: stat %s: %w", path, plugins.ErrFileNotFound)
	}
	fi := &plugins.FileInfo{Name: filepath.Base(path), Path: path, Size: 0, ModTime: time.Now()}
	return fi, nil
}

func (m *mockBackend) CreateDir(_ context.Context, path string) error {
	if !m.connected {
		return fmt.Errorf("mock: %w", plugins.ErrNotConnected)
	}
	return nil
}

func (m *mockBackend) Watch(ctx context.Context, path string) (<-chan plugins.FileEvent, error) {
	if !m.connected {
		return nil, fmt.Errorf("mock: %w", plugins.ErrNotConnected)
	}
	ch := make(chan plugins.FileEvent, 64)
	go func() {
		defer close(ch)
		// Send one event then wait for context cancellation.
		select {
		case ch <- plugins.FileEvent{Type: plugins.FileEventCreated, Path: path + "/new.txt", Source: "remote"}:
		case <-ctx.Done():
			return
		}
		<-ctx.Done()
	}()
	return ch, nil
}

func (m *mockBackend) GetQuota(_ context.Context) (free, total int64, err error) {
	return 1000, 2000, nil
}

func (m *mockBackend) Describe() plugins.PluginDescriptor {
	return plugins.PluginDescriptor{Type: "mock", DisplayName: "Mock Backend (test)"}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestGRPCBackend_Version(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()

	assert.Equal(t, "unknown", backend.Version(),
		"Version() must return 'unknown' (no version RPC in proto)")
}

func TestGRPCBackend_Name(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()

	assert.Equal(t, "mock", backend.Name())
}

func TestGRPCBackend_ConnectDisconnect(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()

	require.NoError(t, backend.Connect(plugins.BackendConfig{Params: map[string]string{}}))
	assert.True(t, backend.IsConnected())

	require.NoError(t, backend.Disconnect())
	assert.False(t, backend.IsConnected())
}

func TestGRPCBackend_List(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()
	_ = backend.Connect(plugins.BackendConfig{})

	files, err := backend.List(context.Background(), "/")
	require.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, "file.txt", files[0].Name)
}

func TestGRPCBackend_Stat(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()
	_ = backend.Connect(plugins.BackendConfig{})

	fi, err := backend.Stat(context.Background(), "/")
	require.NoError(t, err)
	assert.NotNil(t, fi)
}

func TestGRPCBackend_UploadDownload(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()
	_ = backend.Connect(plugins.BackendConfig{})

	// Create a temp file to upload.
	tmpDir := t.TempDir()
	localSrc := filepath.Join(tmpDir, "upload.txt")
	require.NoError(t, os.WriteFile(localSrc, []byte("hello ghostdrive"), 0644))

	var progressCalled bool
	err := backend.Upload(context.Background(), localSrc, "/remote/upload.txt", func(done, total int64) {
		progressCalled = true
	})
	require.NoError(t, err)
	assert.True(t, progressCalled, "progress callback must be called")
	assert.Contains(t, mock.files, "/remote/upload.txt")

	// Download back.
	localDst := filepath.Join(tmpDir, "download.txt")
	err = backend.Download(context.Background(), "/remote/upload.txt", localDst, nil)
	require.NoError(t, err)

	got, err := os.ReadFile(localDst)
	require.NoError(t, err)
	assert.Equal(t, "hello ghostdrive", string(got))
}

func TestGRPCBackend_Watch_ReceivesEvent(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()
	_ = backend.Connect(plugins.BackendConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := backend.Watch(ctx, "/")
	require.NoError(t, err)

	select {
	case ev, ok := <-ch:
		assert.True(t, ok)
		assert.Equal(t, plugins.FileEventCreated, ev.Type)
	case <-ctx.Done():
		t.Fatal("timeout waiting for Watch event")
	}
}

func TestGRPCBackend_Watch_CancelStopsChannel(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()
	_ = backend.Connect(plugins.BackendConfig{})

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := backend.Watch(ctx, "/")
	require.NoError(t, err)

	// Drain the initial event.
	select {
	case <-ch:
	case <-time.After(time.Second):
	}

	cancel()

	select {
	case _, open := <-ch:
		assert.False(t, open, "channel must be closed after context cancellation")
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after context cancellation")
	}
}

func TestGRPCBackend_GetQuota(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()
	_ = backend.Connect(plugins.BackendConfig{})

	free, total, err := backend.GetQuota(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1000), free)
	assert.Equal(t, int64(2000), total)
}

// ── Error mapping tests ───────────────────────────────────────────────────────

// errorBackend returns wrapped sentinel errors to test the server→client error mapping.
// Real backends return sentinel errors (not gRPC status codes directly).
type errorBackend struct {
	mockBackend
}

func (e *errorBackend) Stat(_ context.Context, path string) (*plugins.FileInfo, error) {
	return nil, fmt.Errorf("mock: stat %s: %w", path, plugins.ErrFileNotFound)
}

func (e *errorBackend) List(_ context.Context, path string) ([]plugins.FileInfo, error) {
	return nil, fmt.Errorf("mock: list: %w", plugins.ErrNotConnected)
}

// internalErrorBackend returns a generic (non-sentinel) error to exercise the
// default / codes.Internal path in both mapBackendError and mapGRPCError.
type internalErrorBackend struct {
	mockBackend
}

func (e *internalErrorBackend) Stat(_ context.Context, path string) (*plugins.FileInfo, error) {
	return nil, fmt.Errorf("some unexpected internal failure")
}

func (e *internalErrorBackend) Move(_ context.Context, _, _ string) error {
	return fmt.Errorf("some unexpected internal failure")
}

// TestGRPCBackend_ErrorMapping_Internal verifies that a generic (non-sentinel)
// backend error is forwarded as codes.Internal on the server side and wrapped
// as a plain error on the client side (default case in mapGRPCError).
func TestGRPCBackend_ErrorMapping_Internal(t *testing.T) {
	eb := &internalErrorBackend{
		mockBackend: mockBackend{connected: true, files: make(map[string][]byte)},
	}
	backend, cleanup := newTestPair(t, eb)
	defer cleanup()

	_, err := backend.Stat(context.Background(), "/some-path")
	require.Error(t, err)
	assert.False(t, errors.Is(err, plugins.ErrFileNotFound),
		"internal error must NOT be wrapped as ErrFileNotFound")
	assert.False(t, errors.Is(err, plugins.ErrNotConnected),
		"internal error must NOT be wrapped as ErrNotConnected")
}

// TestGRPCBackend_ErrorMapping_Move_Internal verifies the Move error path.
func TestGRPCBackend_ErrorMapping_Move_Internal(t *testing.T) {
	eb := &internalErrorBackend{
		mockBackend: mockBackend{connected: true, files: make(map[string][]byte)},
	}
	backend, cleanup := newTestPair(t, eb)
	defer cleanup()

	err := backend.Move(context.Background(), "/src", "/dst")
	require.Error(t, err, "Move must propagate a non-nil internal error")
}

func TestGRPCBackend_ErrorMapping_NotFound(t *testing.T) {
	eb := &errorBackend{mockBackend: mockBackend{connected: true, files: make(map[string][]byte)}}
	backend, cleanup := newTestPair(t, eb)
	defer cleanup()

	_, err := backend.Stat(context.Background(), "/missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, plugins.ErrFileNotFound), "expected ErrFileNotFound, got: %v", err)
}

func TestGRPCBackend_ErrorMapping_NotConnected(t *testing.T) {
	eb := &errorBackend{mockBackend: mockBackend{connected: true, files: make(map[string][]byte)}}
	backend, cleanup := newTestPair(t, eb)
	defer cleanup()

	_, err := backend.List(context.Background(), "/")
	require.Error(t, err)
	assert.True(t, errors.Is(err, plugins.ErrNotConnected), "expected ErrNotConnected, got: %v", err)
}

// ── Delete / Move tests ───────────────────────────────────────────────────────

func TestGRPCBackend_Delete(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()
	_ = backend.Connect(plugins.BackendConfig{})

	// Pre-populate a remote file.
	mock.files["/to-delete.txt"] = []byte("data")

	err := backend.Delete(context.Background(), "/to-delete.txt")
	require.NoError(t, err)
	assert.NotContains(t, mock.files, "/to-delete.txt", "file must be removed after Delete")
}

func TestGRPCBackend_Move(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()
	_ = backend.Connect(plugins.BackendConfig{})

	mock.files["/src.txt"] = []byte("content")

	err := backend.Move(context.Background(), "/src.txt", "/dst.txt")
	require.NoError(t, err)
	assert.NotContains(t, mock.files, "/src.txt", "source must not exist after Move")
	assert.Contains(t, mock.files, "/dst.txt", "destination must exist after Move")
}

func TestGRPCBackend_CreateDir(t *testing.T) {
	mock := newMockBackend()
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()
	_ = backend.Connect(plugins.BackendConfig{})

	err := backend.CreateDir(context.Background(), "/new-dir")
	require.NoError(t, err, "CreateDir must succeed on connected backend")
}

func TestGRPCBackend_CreateDir_NotConnected(t *testing.T) {
	mock := newMockBackend() // connected = false
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()

	// mockBackend.CreateDir returns ErrNotConnected when not connected.
	err := backend.CreateDir(context.Background(), "/new-dir")
	require.Error(t, err)
	assert.True(t, errors.Is(err, plugins.ErrNotConnected),
		"CreateDir on disconnected backend must wrap ErrNotConnected, got: %v", err)
}

func TestGRPCBackend_ErrorMapping_DeleteNotFound(t *testing.T) {
	mock := newMockBackend()
	mock.connected = true
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()

	// File does not exist → mockBackend.Delete wraps ErrFileNotFound.
	err := backend.Delete(context.Background(), "/nonexistent.txt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, plugins.ErrFileNotFound),
		"expected ErrFileNotFound on Delete of nonexistent file, got: %v", err)
}

// ── GetQuota error mapping ────────────────────────────────────────────────────
//
// After the fix in server.go, GetQuota uses mapBackendError (gRPC status
// errors) instead of the proto Error field.  The round-trip preserves Go
// sentinel errors: ErrNotConnected → codes.FailedPrecondition → ErrNotConnected.

// quotaErrorBackend is a mock that returns a controlled error from GetQuota.
type quotaErrorBackend struct {
	mockBackend
	quotaErr error
}

func (q *quotaErrorBackend) GetQuota(_ context.Context) (int64, int64, error) {
	return 0, 0, q.quotaErr
}

// notSupportedQuotaBackend returns the canonical "quota not supported" response.
type notSupportedQuotaBackend struct {
	mockBackend
}

func (n *notSupportedQuotaBackend) GetQuota(_ context.Context) (int64, int64, error) {
	return -1, -1, nil
}

// TestGRPCBackend_GetQuota_ErrorMapping verifies that ErrNotConnected returned
// by the plugin is propagated through the gRPC bridge as a proper sentinel.
// This tests the server.go fix: GetQuota now uses mapBackendError instead of
// {Error: err.Error()}, so the sentinel survives the gRPC round-trip.
func TestGRPCBackend_GetQuota_ErrorMapping(t *testing.T) {
	eb := &quotaErrorBackend{
		mockBackend: mockBackend{connected: true, files: make(map[string][]byte)},
		quotaErr:    fmt.Errorf("quota: %w", plugins.ErrNotConnected),
	}
	backend, cleanup := newTestPair(t, eb)
	defer cleanup()

	_, _, err := backend.GetQuota(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, plugins.ErrNotConnected),
		"ErrNotConnected must round-trip through GetQuota gRPC bridge, got: %v", err)
}

// TestGRPCBackend_GetQuota_NotSupportedReturnsMinusOne verifies that when the
// plugin returns (-1, -1, nil) the client passes the values through unchanged.
func TestGRPCBackend_GetQuota_NotSupportedReturnsMinusOne(t *testing.T) {
	nb := &notSupportedQuotaBackend{
		mockBackend: mockBackend{connected: true, files: make(map[string][]byte)},
	}
	backend, cleanup := newTestPair(t, nb)
	defer cleanup()

	free, total, err := backend.GetQuota(context.Background())
	require.NoError(t, err, "quota-not-supported must not return an error")
	assert.Equal(t, int64(-1), free, "free must be -1 for unsupported quota")
	assert.Equal(t, int64(-1), total, "total must be -1 for unsupported quota")
}

// TestGRPCBackend_Upload_Disconnected verifies that Upload propagates an error
// when the backend reports it is not connected.
// Note: Upload uses the client-streaming proto pattern, so the error is encoded
// in UploadResult.Error (a string field) rather than as a gRPC status. The
// sentinel is therefore NOT preserved across the bridge; we verify the error
// message instead.
func TestGRPCBackend_Upload_Disconnected(t *testing.T) {
	mock := newMockBackend() // connected = false
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()

	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "upload.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("data"), 0644))

	err := backend.Upload(context.Background(), localFile, "/remote/upload.txt", nil)
	require.Error(t, err, "Upload on disconnected backend must return an error")
	assert.Contains(t, err.Error(), "not connected",
		"error message must mention 'not connected'")
}

// TestGRPCBackend_Download_FileNotFound verifies that Download propagates an
// error when the backend cannot find the requested remote file.
// Note: Download uses the server-streaming proto pattern; the error is encoded
// in DownloadChunk.Error (a string field). The ErrFileNotFound sentinel is NOT
// preserved; we verify the error message instead.
func TestGRPCBackend_Download_FileNotFound(t *testing.T) {
	mock := newMockBackend()
	mock.connected = true
	// No files added → Download will hit ErrFileNotFound in mockBackend.
	backend, cleanup := newTestPair(t, mock)
	defer cleanup()

	tmpDir := t.TempDir()
	localDst := filepath.Join(tmpDir, "downloaded.txt")

	err := backend.Download(context.Background(), "/nonexistent.txt", localDst, nil)
	require.Error(t, err, "Download of non-existent remote file must return an error")
	assert.Contains(t, err.Error(), "not found",
		"error message must mention 'not found'")
}
