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

// ── Tests ─────────────────────────────────────────────────────────────────────

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
