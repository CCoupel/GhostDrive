package webdav

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/webdav"
)

// newTestServer creates an in-memory WebDAV server for testing.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := &webdav.Handler{
		FileSystem: webdav.NewMemFS(),
		LockSystem: webdav.NewMemLS(),
	}
	return httptest.NewServer(handler)
}

// newConnectedBackend creates a Backend already connected to the test server.
func newConnectedBackend(t *testing.T, srv *httptest.Server) *Backend {
	t.Helper()
	b := New()
	cfg := plugins.BackendConfig{
		Params: map[string]string{"url": srv.URL},
	}
	require.NoError(t, b.Connect(cfg))
	return b
}

func TestConnect(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	b := New()
	cfg := plugins.BackendConfig{
		Params: map[string]string{"url": srv.URL},
	}
	require.NoError(t, b.Connect(cfg))
	assert.True(t, b.IsConnected())
}

func TestConnectInvalidURL(t *testing.T) {
	b := New()
	tests := []struct {
		name string
		url  string
	}{
		{"empty url", ""},
		{"invalid scheme", "ftp://example.com"},
		{"malformed url", "not-a-url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := plugins.BackendConfig{
				Params: map[string]string{"url": tt.url},
			}
			err := b.Connect(cfg)
			assert.Error(t, err)
			assert.False(t, b.IsConnected())
		})
	}
}

func TestConnectUnreachableServer(t *testing.T) {
	b := New()
	cfg := plugins.BackendConfig{
		Params: map[string]string{"url": "http://127.0.0.1:19999"},
	}
	err := b.Connect(cfg)
	assert.Error(t, err)
	assert.False(t, b.IsConnected())
}

func TestDisconnect(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	b := newConnectedBackend(t, srv)
	assert.True(t, b.IsConnected())

	require.NoError(t, b.Disconnect())
	assert.False(t, b.IsConnected())
}

func TestUploadDownloadRoundTrip(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	b := newConnectedBackend(t, srv)

	tmp := t.TempDir()

	// Create local source file
	localSrc := filepath.Join(tmp, "source.txt")
	require.NoError(t, os.WriteFile(localSrc, []byte("hello ghostdrive"), 0644))

	// Upload
	err := b.Upload(context.Background(), localSrc, "/test.txt", nil)
	require.NoError(t, err)

	// Download to a different local path
	localDst := filepath.Join(tmp, "downloaded.txt")
	err = b.Download(context.Background(), "/test.txt", localDst, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(localDst)
	require.NoError(t, err)
	assert.Equal(t, "hello ghostdrive", string(data))
}

func TestUploadWithProgress(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	b := newConnectedBackend(t, srv)

	tmp := t.TempDir()
	localSrc := filepath.Join(tmp, "bigfile.bin")
	content := make([]byte, 8192)
	for i := range content {
		content[i] = byte(i % 256)
	}
	require.NoError(t, os.WriteFile(localSrc, content, 0644))

	var lastDone, lastTotal int64
	progress := func(done, total int64) {
		lastDone = done
		lastTotal = total
	}

	require.NoError(t, b.Upload(context.Background(), localSrc, "/bigfile.bin", progress))
	assert.Equal(t, int64(len(content)), lastDone)
	assert.Equal(t, int64(len(content)), lastTotal)
}

func TestUploadMissingLocalFile(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	b := newConnectedBackend(t, srv)

	err := b.Upload(context.Background(), "/nonexistent/path.txt", "/remote.txt", nil)
	assert.Error(t, err)
}

func TestList(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	b := newConnectedBackend(t, srv)

	tmp := t.TempDir()

	// Upload two files
	for _, name := range []string{"a.txt", "b.txt"} {
		local := filepath.Join(tmp, name)
		require.NoError(t, os.WriteFile(local, []byte(name), 0644))
		require.NoError(t, b.Upload(context.Background(), local, "/"+name, nil))
	}

	files, err := b.List(context.Background(), "/")
	require.NoError(t, err)
	assert.Len(t, files, 2)

	names := map[string]bool{}
	for _, f := range files {
		names[f.Name] = true
	}
	assert.True(t, names["a.txt"])
	assert.True(t, names["b.txt"])
}

func TestDelete(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	b := newConnectedBackend(t, srv)

	tmp := t.TempDir()
	local := filepath.Join(tmp, "todelete.txt")
	require.NoError(t, os.WriteFile(local, []byte("bye"), 0644))
	require.NoError(t, b.Upload(context.Background(), local, "/todelete.txt", nil))

	// Delete
	require.NoError(t, b.Delete(context.Background(), "/todelete.txt"))

	// Verify it's gone
	_, err := b.Stat(context.Background(), "/todelete.txt")
	assert.Error(t, err)
}

func TestDeleteNonExistent(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	b := newConnectedBackend(t, srv)

	err := b.Delete(context.Background(), "/does-not-exist.txt")
	assert.Error(t, err)
}

func TestStat(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	b := newConnectedBackend(t, srv)

	tmp := t.TempDir()
	local := filepath.Join(tmp, "stat.txt")
	content := []byte("stat me")
	require.NoError(t, os.WriteFile(local, content, 0644))
	require.NoError(t, b.Upload(context.Background(), local, "/stat.txt", nil))

	fi, err := b.Stat(context.Background(), "/stat.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), fi.Size)
	assert.False(t, fi.IsDir)
}

func TestCreateDir(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	b := newConnectedBackend(t, srv)

	require.NoError(t, b.CreateDir(context.Background(), "/newdir"))

	fi, err := b.Stat(context.Background(), "/newdir")
	require.NoError(t, err)
	assert.True(t, fi.IsDir)
}

func TestNotConnectedErrors(t *testing.T) {
	b := New()
	ctx := context.Background()

	assert.ErrorIs(t, b.Upload(ctx, "/local", "/remote", nil), ErrNotConnected)
	assert.ErrorIs(t, b.Download(ctx, "/remote", "/local", nil), ErrNotConnected)
	assert.ErrorIs(t, b.Delete(ctx, "/remote"), ErrNotConnected)
	assert.ErrorIs(t, b.Move(ctx, "/a", "/b"), ErrNotConnected)
	assert.ErrorIs(t, b.CreateDir(ctx, "/dir"), ErrNotConnected)

	_, err := b.List(ctx, "/")
	assert.ErrorIs(t, err, ErrNotConnected)

	_, err = b.Stat(ctx, "/file")
	assert.ErrorIs(t, err, ErrNotConnected)

	_, err = b.Watch(ctx, "/")
	assert.ErrorIs(t, err, ErrNotConnected)
}

func TestMove(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	b := newConnectedBackend(t, srv)

	tmp := t.TempDir()
	local := filepath.Join(tmp, "move.txt")
	require.NoError(t, os.WriteFile(local, []byte("moving"), 0644))
	require.NoError(t, b.Upload(context.Background(), local, "/move.txt", nil))

	require.NoError(t, b.Move(context.Background(), "/move.txt", "/moved.txt"))

	_, err := b.Stat(context.Background(), "/move.txt")
	assert.Error(t, err, "original should be gone after move")

	fi, err := b.Stat(context.Background(), "/moved.txt")
	require.NoError(t, err)
	assert.Equal(t, "moved.txt", fi.Name)
}
