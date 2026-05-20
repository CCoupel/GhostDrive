// Package webdav contains integration tests for the WebDAV storage backend.
//
// Tests run against an in-memory WebDAV server (golang.org/x/net/webdav) and
// therefore do not require an external WebDAV server.  Each test case creates
// its own server instance (fresh MemFS) to guarantee full isolation.
//
// Build tag: none — compiled and run with the rest of the package.
package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gowebdav "golang.org/x/net/webdav"
)

// ─── Test constants ───────────────────────────────────────────────────────────

const (
	testUser  = "testuser"
	testPass  = "testpass"
	testToken = "test-bearer-token-xyz"

	// testPollInterval is the Watch poll interval used in test backends.
	// Kept short so Watch tests complete quickly.
	testPollInterval = "20" // milliseconds
)

// ─── Server helpers ───────────────────────────────────────────────────────────

// newTestServer launches an in-memory WebDAV HTTP server protected by Basic
// Auth (testUser / testPass).  Each call creates a fresh MemFS so tests are
// fully isolated.  The cleanup function closes the server; callers must invoke
// it (via defer or t.Cleanup).
func newTestServer(t *testing.T) (serverURL string, cleanup func()) {
	t.Helper()

	wdHandler := &gowebdav.Handler{
		FileSystem: gowebdav.NewMemFS(),
		LockSystem: gowebdav.NewMemLS(),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != testUser || pass != testPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="webdav"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		wdHandler.ServeHTTP(w, r)
	}))

	return srv.URL, srv.Close
}

// newBearerTestServer launches an in-memory WebDAV server that authenticates
// requests via Bearer token.  The provided token is the only accepted value.
func newBearerTestServer(t *testing.T, token string) (serverURL string, cleanup func()) {
	t.Helper()

	wdHandler := &gowebdav.Handler{
		FileSystem: gowebdav.NewMemFS(),
		LockSystem: gowebdav.NewMemLS(),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		wdHandler.ServeHTTP(w, r)
	}))

	return srv.URL, srv.Close
}

// newTestBackend returns a Backend already connected to serverURL using Basic
// Auth.  A t.Cleanup is registered to disconnect the backend when the test ends.
func newTestBackend(t *testing.T, serverURL string) *Backend {
	t.Helper()

	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":          serverURL,
			"username":     testUser,
			"password":     testPass,
			"authType":     "basic",
			"pollInterval": testPollInterval,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Disconnect() })
	return b
}

// writeTempFile creates a temporary file (inside t.TempDir()) with the
// provided content and returns its path.
func writeTempFile(t *testing.T, content []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "ghostdrive-test-*")
	require.NoError(t, err)
	_, err = f.Write(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// ─── Name ────────────────────────────────────────────────────────────────────

func TestName(t *testing.T) {
	b := New()
	assert.Equal(t, "webdav", b.Name())
}

// ─── Connect ─────────────────────────────────────────────────────────────────

func TestConnect_BasicAuth_OK(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":      serverURL,
			"username": testUser,
			"password": testPass,
			"authType": "basic",
		},
	})
	require.NoError(t, err)
	assert.True(t, b.IsConnected())
	_ = b.Disconnect()
}

func TestConnect_BearerToken_OK(t *testing.T) {
	serverURL, cleanup := newBearerTestServer(t, testToken)
	defer cleanup()

	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":      serverURL,
			"token":    testToken,
			"authType": "bearer",
		},
	})
	require.NoError(t, err)
	assert.True(t, b.IsConnected())
	_ = b.Disconnect()
}

func TestConnect_MissingURL(t *testing.T) {
	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"username": testUser,
			"password": testPass,
			"authType": "basic",
		},
	})
	require.Error(t, err)
	assert.False(t, b.IsConnected())
}

func TestConnect_InvalidURL(t *testing.T) {
	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":      "://this-is-not-a-valid-url",
			"username": testUser,
			"password": testPass,
			"authType": "basic",
		},
	})
	require.Error(t, err)
	assert.False(t, b.IsConnected())
}

func TestConnect_ServerUnreachable(t *testing.T) {
	// Start a server, immediately close it, and attempt to connect to the
	// now-dead URL — the backend must return an error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := srv.URL
	srv.Close()

	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":      unreachableURL,
			"username": testUser,
			"password": testPass,
			"authType": "basic",
		},
	})
	require.Error(t, err)
	assert.False(t, b.IsConnected())
}

func TestConnect_Idempotent(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	cfg := plugins.BackendConfig{
		Params: map[string]string{
			"url":      serverURL,
			"username": testUser,
			"password": testPass,
			"authType": "basic",
		},
	}

	b := New()
	require.NoError(t, b.Connect(cfg))
	assert.True(t, b.IsConnected())

	// Second Connect must succeed and keep the backend connected (reconnect).
	require.NoError(t, b.Connect(cfg))
	assert.True(t, b.IsConnected())
	_ = b.Disconnect()
}

// ─── Disconnect ───────────────────────────────────────────────────────────────

func TestDisconnect_AfterConnect(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)
	require.True(t, b.IsConnected())

	require.NoError(t, b.Disconnect())
	assert.False(t, b.IsConnected())
}

func TestDisconnect_DoubleDisconnect(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)

	require.NoError(t, b.Disconnect())
	// Second call must be a no-op: no panic, no error.
	require.NoError(t, b.Disconnect())
	assert.False(t, b.IsConnected())
}

// ─── Not-connected guards ─────────────────────────────────────────────────────

// TestNotConnected_AllMethods verifies that every I/O method wraps
// plugins.ErrNotConnected when the backend has not been connected.
func TestNotConnected_AllMethods(t *testing.T) {
	ctx := context.Background()

	t.Run("Upload", func(t *testing.T) {
		b := New()
		err := b.Upload(ctx, "/local/src", "/remote/dst", nil)
		assert.ErrorIs(t, err, plugins.ErrNotConnected)
	})

	t.Run("Download", func(t *testing.T) {
		b := New()
		err := b.Download(ctx, "/remote/src", "/local/dst", nil)
		assert.ErrorIs(t, err, plugins.ErrNotConnected)
	})

	t.Run("Delete", func(t *testing.T) {
		b := New()
		err := b.Delete(ctx, "/remote/file.txt")
		assert.ErrorIs(t, err, plugins.ErrNotConnected)
	})

	t.Run("Move", func(t *testing.T) {
		b := New()
		err := b.Move(ctx, "/remote/src.txt", "/remote/dst.txt")
		assert.ErrorIs(t, err, plugins.ErrNotConnected)
	})

	t.Run("List", func(t *testing.T) {
		b := New()
		_, err := b.List(ctx, "/")
		assert.ErrorIs(t, err, plugins.ErrNotConnected)
	})

	t.Run("Stat", func(t *testing.T) {
		b := New()
		_, err := b.Stat(ctx, "/file.txt")
		assert.ErrorIs(t, err, plugins.ErrNotConnected)
	})

	t.Run("CreateDir", func(t *testing.T) {
		b := New()
		err := b.CreateDir(ctx, "/newdir")
		assert.ErrorIs(t, err, plugins.ErrNotConnected)
	})

	t.Run("Watch", func(t *testing.T) {
		b := New()
		_, err := b.Watch(ctx, "/")
		assert.ErrorIs(t, err, plugins.ErrNotConnected)
	})

	t.Run("GetQuota", func(t *testing.T) {
		b := New()
		_, _, err := b.GetQuota(ctx)
		assert.ErrorIs(t, err, plugins.ErrNotConnected)
	})
}

// ─── CreateDir ────────────────────────────────────────────────────────────────

func TestCreateDir_OK(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	require.NoError(t, b.CreateDir(ctx, "/newdir"))

	fi, err := b.Stat(ctx, "/newdir")
	require.NoError(t, err)
	assert.True(t, fi.IsDir)
}

func TestCreateDir_Idempotent(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	require.NoError(t, b.CreateDir(ctx, "/idemdir"))
	// Second call on an existing directory must not return an error.
	// (WebDAV MKCOL may return 405; the backend should treat that as success.)
	require.NoError(t, b.CreateDir(ctx, "/idemdir"))
}

// ─── Upload ───────────────────────────────────────────────────────────────────

func TestUpload_OK(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	content := []byte("hello webdav upload — テスト")
	src := writeTempFile(t, content)

	require.NoError(t, b.Upload(ctx, src, "/upload-ok.txt", nil))

	fi, err := b.Stat(ctx, "/upload-ok.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), fi.Size)
	assert.False(t, fi.IsDir)
}

func TestUpload_WithProgress(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	payload := make([]byte, 8192)
	src := writeTempFile(t, payload)

	var calls int
	var lastDone int64
	progress := func(done, total int64) {
		calls++
		assert.GreaterOrEqual(t, done, lastDone, "done must be monotonically increasing")
		assert.Greater(t, done, int64(0))
		lastDone = done
	}

	require.NoError(t, b.Upload(ctx, src, "/progress-upload.bin", progress))
	assert.Greater(t, calls, 0, "progress callback must be called at least once")
}

func TestUpload_ContextCancelled(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Upload is called

	src := writeTempFile(t, []byte("this upload should be rejected"))
	err := b.Upload(ctx, src, "/should-fail.txt", nil)
	assert.Error(t, err)
}

// ─── Download ─────────────────────────────────────────────────────────────────

func TestDownload_OK(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	content := []byte("download roundtrip content — こんにちは")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/download-ok.txt", nil))

	dst := filepath.Join(t.TempDir(), "downloaded.txt")
	require.NoError(t, b.Download(ctx, "/download-ok.txt", dst, nil))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestDownload_WithProgress(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	payload := make([]byte, 4096)
	src := writeTempFile(t, payload)
	require.NoError(t, b.Upload(ctx, src, "/download-progress.bin", nil))

	var calls int
	dst := filepath.Join(t.TempDir(), "out.bin")
	progress := func(done, total int64) {
		calls++
		assert.Greater(t, done, int64(0))
	}

	require.NoError(t, b.Download(ctx, "/download-progress.bin", dst, progress))
	assert.Greater(t, calls, 0, "progress callback must be called at least once")
}

func TestDownload_NotFound(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	dst := filepath.Join(t.TempDir(), "nonexistent.txt")
	err := b.Download(context.Background(), "/does-not-exist.txt", dst, nil)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

func TestDownload_CreatesParentDir(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	content := []byte("parent dir creation test content")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/parent-test.txt", nil))

	// The local destination is in a directory tree that does not yet exist.
	dst := filepath.Join(t.TempDir(), "deep", "nested", "dir", "downloaded.txt")
	require.NoError(t, b.Download(ctx, "/parent-test.txt", dst, nil))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

// ─── List ─────────────────────────────────────────────────────────────────────

func TestList_EmptyDir(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	entries, err := b.List(context.Background(), "/")
	require.NoError(t, err)
	assert.NotNil(t, entries)
	assert.Empty(t, entries)
}

func TestList_WithFiles(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	names := []string{"alpha.txt", "beta.txt", "gamma.txt"}
	for _, name := range names {
		src := writeTempFile(t, []byte("content-"+name))
		require.NoError(t, b.Upload(ctx, src, "/"+name, nil))
	}

	entries, err := b.List(ctx, "/")
	require.NoError(t, err)
	assert.Len(t, entries, len(names))

	for _, e := range entries {
		assert.NotEmpty(t, e.Name)
		assert.False(t, e.IsDir)
	}
}

func TestList_NotFound(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	_, err := b.List(context.Background(), "/nonexistent-directory")
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── Stat ─────────────────────────────────────────────────────────────────────

func TestStat_File(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	content := []byte("stat test content")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/stat-file.txt", nil))

	fi, err := b.Stat(ctx, "/stat-file.txt")
	require.NoError(t, err)
	require.NotNil(t, fi)
	assert.Equal(t, "stat-file.txt", fi.Name)
	assert.Equal(t, int64(len(content)), fi.Size)
	assert.False(t, fi.IsDir)
	assert.False(t, fi.ModTime.IsZero())
}

func TestStat_Directory(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	require.NoError(t, b.CreateDir(ctx, "/stat-dir"))

	fi, err := b.Stat(ctx, "/stat-dir")
	require.NoError(t, err)
	require.NotNil(t, fi)
	assert.True(t, fi.IsDir)
}

func TestStat_NotFound(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	_, err := b.Stat(context.Background(), "/does-not-exist.txt")
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestDelete_OK(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	src := writeTempFile(t, []byte("delete me"))
	require.NoError(t, b.Upload(ctx, src, "/delete-ok.txt", nil))

	require.NoError(t, b.Delete(ctx, "/delete-ok.txt"))

	_, err := b.Stat(ctx, "/delete-ok.txt")
	assert.ErrorIs(t, err, plugins.ErrFileNotFound, "file must no longer exist after delete")
}

func TestDelete_NotFound(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	err := b.Delete(context.Background(), "/ghost-file.txt")
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── Move ─────────────────────────────────────────────────────────────────────

func TestMove_OK(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	content := []byte("move test content")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/move-src.txt", nil))

	require.NoError(t, b.Move(ctx, "/move-src.txt", "/move-dst.txt"))

	// Source must be gone.
	_, err := b.Stat(ctx, "/move-src.txt")
	assert.ErrorIs(t, err, plugins.ErrFileNotFound, "source must not exist after move")

	// Destination must be present with the correct size.
	fi, err := b.Stat(ctx, "/move-dst.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), fi.Size)
}

func TestMove_SourceNotFound(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	err := b.Move(context.Background(), "/ghost.txt", "/nowhere.txt")
	// The golang.org/x/net/webdav MemFS returns 403 (Forbidden) — not 404 — when
	// the MOVE source does not exist, which deviates from RFC 4918 §9.9.5.
	// Real WebDAV servers (Nextcloud, Apache) correctly return 404 → ErrFileNotFound.
	// We assert only that an error is returned; integration tests against a real
	// server should assert errors.Is(err, plugins.ErrFileNotFound).
	assert.Error(t, err)
}

// ─── GetQuota ─────────────────────────────────────────────────────────────────

// TestGetQuota_NoSupport verifies the graceful degradation contract: when the
// WebDAV server does not implement RFC 4331 quota reporting (which the
// golang.org/x/net/webdav MemFS does not), the backend must return
// (-1, -1, nil) instead of surfacing an error.
func TestGetQuota_NoSupport(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	free, total, err := b.GetQuota(context.Background())
	require.NoError(t, err, "unsupported quota must not produce an error")
	assert.Equal(t, int64(-1), free, "free must be -1 when quota is unsupported")
	assert.Equal(t, int64(-1), total, "total must be -1 when quota is unsupported")
}

// ─── Watch ────────────────────────────────────────────────────────────────────

// TestWatch_DetectsUpload verifies that Watch emits a FileEvent after a new
// file is uploaded to the watched path.
//
// Implementation note: because WebDAV has no push notification mechanism, the
// backend polls.  The testPollInterval constant (20 ms) makes this fast enough
// for a unit test while staying well within the 5-second timeout.
func TestWatch_DetectsUpload(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Pre-create the file outside the goroutine to avoid calling t.* concurrently.
	srcFile := writeTempFile(t, []byte("watch trigger content"))

	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	// Give the watcher time to establish the initial directory baseline before
	// we create the new file.
	time.Sleep(60 * time.Millisecond)

	go func() {
		_ = b.Upload(context.Background(), srcFile, "/watched-file.txt", nil)
	}()

	select {
	case event, ok := <-ch:
		require.True(t, ok, "channel must not be closed prematurely")
		assert.NotEmpty(t, event.Type, "event type must be set")
		assert.False(t, event.Timestamp.IsZero(), "event timestamp must be set")
	case <-ctx.Done():
		t.Fatal("timed out waiting for a FileEvent from Watch")
	}
}

// TestWatch_StopsOnContextCancel verifies that the Watch channel is closed
// when the context is cancelled.
func TestWatch_StopsOnContextCancel(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	// Cancel the context; the background goroutine must close the channel.
	cancel()

	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // success: channel closed as expected
			}
			// Drain any buffered events that arrived before the cancel.
		case <-deadline:
			t.Fatal("Watch channel not closed within 500 ms after context cancellation")
		}
	}
}

// TestWatch_ClosesOnPersistentErrors verifies that Watch closes its channel
// after watchMaxConsecutiveErrors consecutive poll failures, and that the
// engine's remoteEvents handler will therefore be able to record a SyncError
// (issue #115: tray stays green when backend is down).
//
// Strategy: connect the backend to a working server, start Watch, then shut
// the server down so every subsequent List() call returns a connection error.
// With the short test backoff the channel must close well within 2 seconds.
func TestWatch_ClosesOnPersistentErrors(t *testing.T) {
	// Override backoff timings so the test completes in milliseconds.
	origInitial := watchBackoffInitial
	origMax := watchBackoffMax
	watchBackoffInitial = 5 * time.Millisecond
	watchBackoffMax = 20 * time.Millisecond
	t.Cleanup(func() {
		watchBackoffInitial = origInitial
		watchBackoffMax = origMax
	})

	serverURL, srvClose := newTestServer(t)
	b := newTestBackend(t, serverURL)

	ctx := context.Background()
	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	// Give the watcher one successful tick to establish its baseline before
	// we pull the server out from under it.
	time.Sleep(3 * time.Duration(20) * time.Millisecond) // ~3 poll ticks

	// Shut down the server — all subsequent List() calls will fail with
	// "connection refused", triggering the consecutive-error counter.
	srvClose()

	// The channel must close within 2 seconds: 5 errors × max 20ms backoff
	// = at most 100ms, well under the 2s budget even on a slow CI runner.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // success: Watch closed the channel after persistent errors
			}
			// Drain any events that arrived before the server went down.
		case <-deadline:
			t.Fatal("Watch channel not closed within 2s after persistent backend failure (#115)")
		}
	}
}

// TestWatch_SendsOfflineSentinelOnFirstError verifies that Watch sends a
// FileEventBackendOffline sentinel on the first consecutive poll failure so
// the engine can transition to SyncOffline (orange tray) before the full
// error state (issue #115b).
func TestWatch_SendsOfflineSentinelOnFirstError(t *testing.T) {
	// Override backoff timings for a fast test.
	origInitial := watchBackoffInitial
	origMax := watchBackoffMax
	watchBackoffInitial = 5 * time.Millisecond
	watchBackoffMax = 20 * time.Millisecond
	t.Cleanup(func() {
		watchBackoffInitial = origInitial
		watchBackoffMax = origMax
	})

	serverURL, srvClose := newTestServer(t)
	b := newTestBackend(t, serverURL)

	// Use a cancellable context so we can stop the Watch goroutine before
	// t.Cleanup resets the backoff vars — prevents the data race seen with
	// context.Background().
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	// Wait for one healthy tick (baseline established).
	time.Sleep(3 * time.Duration(20) * time.Millisecond)

	// Kill the server — next poll will fail.
	srvClose()

	// The first event after the server goes down must be the offline sentinel.
	// Cancel the context once we've verified it so the Watch goroutine exits
	// cleanly before t.Cleanup restores the package-level backoff vars.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("channel closed before offline sentinel was received")
			}
			if ev.Type == plugins.FileEventBackendOffline {
				cancel()
				// Drain until the goroutine sees the cancellation and closes the channel.
				for range ch {
				}
				return // success
			}
			// Skip any events emitted before the server went down.
		case <-deadline:
			cancel()
			t.Fatal("offline sentinel not received within 2s after server shutdown (#115b)")
		}
	}
}

// TestWatch_SendsOnlineSentinelOnRecovery verifies that Watch sends a
// FileEventBackendOnline sentinel when a poll succeeds after one or more
// consecutive failures (backend came back online, issue #115b).
func TestWatch_SendsOnlineSentinelOnRecovery(t *testing.T) {
	// Override backoff timings for a fast test.
	origInitial := watchBackoffInitial
	origMax := watchBackoffMax
	watchBackoffInitial = 5 * time.Millisecond
	watchBackoffMax = 20 * time.Millisecond
	t.Cleanup(func() {
		watchBackoffInitial = origInitial
		watchBackoffMax = origMax
	})

	// Start a mutable server: can be paused and resumed.
	var serverDown atomic.Bool
	wdHandler := &gowebdav.Handler{
		FileSystem: gowebdav.NewMemFS(),
		LockSystem: gowebdav.NewMemLS(),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverDown.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != testUser || pass != testPass {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		wdHandler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	// Let Watch establish its baseline on the healthy server.
	time.Sleep(3 * time.Duration(20) * time.Millisecond)

	// Take the server offline — triggers offline sentinel.
	serverDown.Store(true)

	deadline := time.After(2 * time.Second)
	gotOffline := false
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("channel closed before online sentinel was received")
			}
			switch ev.Type {
			case plugins.FileEventBackendOffline:
				if !gotOffline {
					gotOffline = true
					// Bring server back online.
					serverDown.Store(false)
				}
			case plugins.FileEventBackendOnline:
				if gotOffline {
					cancel()
					for range ch {
					}
					return // success: offline → online cycle complete
				}
			}
		case <-deadline:
			cancel()
			t.Fatal("online sentinel not received within 2s after server recovery (#115b)")
		}
	}
}

// ─── Watch adaptive polling ───────────────────────────────────────────────────

// TestComputeNextInterval verifies the pure backoff algorithm used by Watch.
func TestComputeNextInterval(t *testing.T) {
	min := 10 * time.Millisecond
	max := 80 * time.Millisecond

	tests := []struct {
		name    string
		current time.Duration
		factor  float64
		changed bool
		want    time.Duration
	}{
		// Change detected → always reset to min.
		{"changed resets to min from min", min, 2.0, true, min},
		{"changed resets to min from mid", 40 * time.Millisecond, 2.0, true, min},
		{"changed resets to min from max", max, 2.0, true, min},

		// No change → backoff by factor.
		{"idle min→20ms", min, 2.0, false, 20 * time.Millisecond},
		{"idle 20ms→40ms", 20 * time.Millisecond, 2.0, false, 40 * time.Millisecond},
		{"idle 40ms→80ms", 40 * time.Millisecond, 2.0, false, max},
		{"idle clamped at max", max, 2.0, false, max},
		{"idle factor=1.5", min, 1.5, false, 15 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeNextInterval(tt.current, min, max, tt.factor, tt.changed)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestWatch_adaptiveBackoff_sequence verifies that the interval sequence
// produced by successive empty polls follows the expected backoff pattern.
func TestWatch_adaptiveBackoff_sequence(t *testing.T) {
	min := 10 * time.Millisecond
	max := 80 * time.Millisecond

	sequence := []time.Duration{min}
	cur := min
	for i := 0; i < 4; i++ {
		cur = computeNextInterval(cur, min, max, 2.0, false)
		sequence = append(sequence, cur)
	}

	want := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
		80 * time.Millisecond,
		80 * time.Millisecond, // clamped at max
	}
	assert.Equal(t, want, sequence, "backoff sequence mismatch")
}

// TestWatch_adaptiveReset verifies that Watch resets to fast polling
// after a change is detected, allowing a subsequent change to be picked
// up quickly even if the interval had backed off.
func TestWatch_adaptiveReset(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Override adaptive interval to short values for a fast test.
	b.mu.Lock()
	b.pollIntervalMin = 20 * time.Millisecond
	b.pollIntervalMax = 200 * time.Millisecond
	b.pollBackoffFactor = 2.0
	b.mu.Unlock()

	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	// Let the snapshot settle.
	time.Sleep(60 * time.Millisecond)

	// Upload first file.
	f1 := writeTempFile(t, []byte("adaptive-reset-1"))
	go func() { _ = b.Upload(context.Background(), f1, "/adaptive1.txt", nil) }()

	select {
	case ev, ok := <-ch:
		require.True(t, ok, "channel closed unexpectedly")
		assert.Equal(t, plugins.FileEventCreated, ev.Type)
	case <-ctx.Done():
		t.Fatal("timed out waiting for first FileEventCreated")
	}

	// Upload second file. Interval should have reset to min (20ms).
	f2 := writeTempFile(t, []byte("adaptive-reset-2"))
	go func() { _ = b.Upload(context.Background(), f2, "/adaptive2.txt", nil) }()

	select {
	case ev, ok := <-ch:
		require.True(t, ok, "channel closed unexpectedly")
		assert.Equal(t, plugins.FileEventCreated, ev.Type)
	case <-ctx.Done():
		t.Fatal("timed out waiting for second FileEventCreated (adaptive reset failed)")
	}
}

// ─── Describe ────────────────────────────────────────────────────────────────

// TestBackend_Describe verifies the static descriptor returned by Describe().
// Describe must be callable before Connect and must never perform I/O.
func TestBackend_Describe(t *testing.T) {
	b := New() // intentionally NOT connected — Describe must not require Connect

	d := b.Describe()

	// Type must match Name()
	assert.Equal(t, "webdav", d.Type,
		"Describe().Type must equal \"webdav\"")

	// DisplayName and Description must be non-empty
	assert.NotEmpty(t, d.DisplayName,
		"Describe().DisplayName must not be empty")
	assert.NotEmpty(t, d.Description,
		"Describe().Description must not be empty")

	// Build a map of params by key for easy lookup
	paramsByKey := make(map[string]plugins.ParamSpec, len(d.Params))
	for _, p := range d.Params {
		paramsByKey[p.Key] = p
	}

	// Required keys from the plugin contract
	expectedKeys := []string{"url", "username", "password", "token", "authType",
		"pollIntervalMin", "pollIntervalMax", "pollBackoffFactor", "tlsSkipVerify"}
	for _, key := range expectedKeys {
		assert.Contains(t, paramsByKey, key,
			"Describe().Params must contain a ParamSpec with Key=%q", key)
	}

	// "url" must be Required
	if url, ok := paramsByKey["url"]; ok {
		assert.True(t, url.Required,
			"Describe() param \"url\" must be Required")
		assert.Equal(t, plugins.ParamTypeString, url.Type,
			"Describe() param \"url\" must be ParamTypeString")
	}

	// "password" must be ParamTypePassword
	if password, ok := paramsByKey["password"]; ok {
		assert.Equal(t, plugins.ParamTypePassword, password.Type,
			"Describe() param \"password\" must be ParamTypePassword")
	}

	// "token" must be ParamTypePassword
	if token, ok := paramsByKey["token"]; ok {
		assert.Equal(t, plugins.ParamTypePassword, token.Type,
			"Describe() param \"token\" must be ParamTypePassword")
	}

	// "authType" must be ParamTypeSelect with options ["basic", "bearer"]
	if authType, ok := paramsByKey["authType"]; ok {
		assert.Equal(t, plugins.ParamTypeSelect, authType.Type,
			"Describe() param \"authType\" must be ParamTypeSelect")
		assert.Contains(t, authType.Options, "basic",
			"Describe() param \"authType\" options must contain \"basic\"")
		assert.Contains(t, authType.Options, "bearer",
			"Describe() param \"authType\" options must contain \"bearer\"")
		assert.Len(t, authType.Options, 2,
			"Describe() param \"authType\" must have exactly 2 options")
	}

	// "tlsSkipVerify" must be ParamTypeBool
	if tls, ok := paramsByKey["tlsSkipVerify"]; ok {
		assert.Equal(t, plugins.ParamTypeBool, tls.Type,
			"Describe() param \"tlsSkipVerify\" must be ParamTypeBool")
	}

	// "pollIntervalMin" and "pollIntervalMax" must be ParamTypeNumber
	if poll, ok := paramsByKey["pollIntervalMin"]; ok {
		assert.Equal(t, plugins.ParamTypeNumber, poll.Type,
			"Describe() param \"pollIntervalMin\" must be ParamTypeNumber")
	}
	if poll, ok := paramsByKey["pollIntervalMax"]; ok {
		assert.Equal(t, plugins.ParamTypeNumber, poll.Type,
			"Describe() param \"pollIntervalMax\" must be ParamTypeNumber")
	}
}

// ─── #82 — basePath ──────────────────────────────────────────────────────────

// TestConnect_BasePath_FromParams verifies that Params["basePath"] is applied:
// the backend must connect to the sub-path and List("/") must return files
// stored under that sub-path on the server.
// Non-regression test for issue #82.
func TestConnect_BasePath_FromParams(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Setup: connect to root, create /subdir, upload a file inside.
	root := newTestBackend(t, serverURL)
	require.NoError(t, root.CreateDir(ctx, "/subdir"))
	src := writeTempFile(t, []byte("basepath param file"))
	require.NoError(t, root.Upload(ctx, src, "/subdir/bp-file.txt", nil))

	// Now connect with Params["basePath"] = "/subdir".
	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":          serverURL,
			"username":     testUser,
			"password":     testPass,
			"authType":     "basic",
			"basePath":     "/subdir",
			"pollInterval": testPollInterval,
		},
	})
	require.NoError(t, err, "Connect with Params[\"basePath\"] must succeed")
	t.Cleanup(func() { _ = b.Disconnect() })

	// List("/") is relative to basePath; must return the file inside /subdir.
	entries, err := b.List(ctx, "/")
	require.NoError(t, err)
	require.Len(t, entries, 1, "List('/') must return exactly the file under basePath")
	assert.Equal(t, "bp-file.txt", entries[0].Name)
}

// TestConnect_BasePath_Empty verifies that an empty Params["basePath"] is
// equivalent to connecting to the server root (backward-compatible behaviour).
// Non-regression test for issue #82.
func TestConnect_BasePath_Empty(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Upload a file at the root of the test server.
	root := newTestBackend(t, serverURL)
	src := writeTempFile(t, []byte("root level file"))
	require.NoError(t, root.Upload(ctx, src, "/root-bp-empty.txt", nil))

	// Connect with an explicitly empty basePath.
	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":          serverURL,
			"username":     testUser,
			"password":     testPass,
			"authType":     "basic",
			"basePath":     "", // explicitly empty → must behave like root
			"pollInterval": testPollInterval,
		},
	})
	require.NoError(t, err, "empty basePath must succeed (root connection)")
	t.Cleanup(func() { _ = b.Disconnect() })

	entries, err := b.List(ctx, "/")
	require.NoError(t, err)

	found := false
	for _, e := range entries {
		if e.Name == "root-bp-empty.txt" {
			found = true
			break
		}
	}
	assert.True(t, found, "root-level file must be visible when basePath is empty")
}

// TestConnect_BasePath_FromRemotePath_Fallback verifies that when
// Params["basePath"] is absent, the backend falls back to cfg.RemotePath.
// Non-regression test for issue #82.
func TestConnect_BasePath_FromRemotePath_Fallback(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Setup: create /subdir and upload a file.
	root := newTestBackend(t, serverURL)
	require.NoError(t, root.CreateDir(ctx, "/subdir"))
	src := writeTempFile(t, []byte("remotepath fallback file"))
	require.NoError(t, root.Upload(ctx, src, "/subdir/rp-file.txt", nil))

	// Connect without Params["basePath"]; use cfg.RemotePath as the sub-path.
	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":          serverURL,
			"username":     testUser,
			"password":     testPass,
			"authType":     "basic",
			"pollInterval": testPollInterval,
			// "basePath" intentionally absent
		},
		RemotePath: "/subdir",
	})
	require.NoError(t, err, "cfg.RemotePath must be used when Params[\"basePath\"] is absent")
	t.Cleanup(func() { _ = b.Disconnect() })

	entries, err := b.List(ctx, "/")
	require.NoError(t, err)
	require.Len(t, entries, 1, "List('/') must return the file under RemotePath")
	assert.Equal(t, "rp-file.txt", entries[0].Name)
}

// ─── #83 — GetQuota ───────────────────────────────────────────────────────────

// TestGetQuota_ServerSupportsRFC4331 verifies that GetQuota correctly parses
// RFC 4331 quota properties when the server supports them.
// The mock server returns quota-available-bytes=1 MiB and quota-used-bytes=512 KiB,
// so GetQuota must return free=1048576 and total=1572864.
// Non-regression test for issue #83.
func TestGetQuota_ServerSupportsRFC4331(t *testing.T) {
	const quotaXML = `<?xml version="1.0" encoding="utf-8"?>` +
		`<D:multistatus xmlns:D="DAV:">` +
		`<D:response>` +
		`<D:href>/</D:href>` +
		`<D:propstat>` +
		`<D:prop>` +
		`<D:resourcetype><D:collection/></D:resourcetype>` +
		`<D:quota-available-bytes>1048576</D:quota-available-bytes>` +
		`<D:quota-used-bytes>524288</D:quota-used-bytes>` +
		`</D:prop>` +
		`<D:status>HTTP/1.1 200 OK</D:status>` +
		`</D:propstat>` +
		`</D:response>` +
		`</D:multistatus>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != testUser || pass != testPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="webdav"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == "PROPFIND" {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(quotaXML))
			return
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":          srv.URL,
			"username":     testUser,
			"password":     testPass,
			"authType":     "basic",
			"pollInterval": testPollInterval,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Disconnect() })

	free, total, err := b.GetQuota(context.Background())
	require.NoError(t, err, "GetQuota must not error on a server that supports RFC 4331")
	assert.Equal(t, int64(1048576), free, "free must equal quota-available-bytes (1 MiB)")
	assert.Equal(t, int64(1572864), total, "total must equal quota-available + quota-used (1.5 MiB)")
}

// TestGetQuota_ServerNoQuota verifies that GetQuota returns (-1, -1, nil) when
// the server does not include quota properties in its PROPFIND response.
// Non-regression test for issue #83.
func TestGetQuota_ServerNoQuota(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)

	free, total, err := b.GetQuota(context.Background())
	require.NoError(t, err, "absent quota properties must not produce an error")
	assert.Equal(t, int64(-1), free, "free must be -1 when quota is not reported by server")
	assert.Equal(t, int64(-1), total, "total must be -1 when quota is not reported by server")
}

// ─── Reconnect ────────────────────────────────────────────────────────────────

// ─── ReadAt (#121) ────────────────────────────────────────────────────────────

// TestReadAt_Success uploads a file and reads a slice of it using ReadAt.
// The in-memory WebDAV handler may not support Range (returns 200), but the
// ReadAt implementation falls back to a full download + slice in that case,
// so the returned bytes must always match the expected sub-slice.
func TestReadAt_Success(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	content := []byte("Hello, WebDAV ReadAt World!")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/readat.txt", nil))

	// Read bytes [7:13) → "WebDAV"
	data, err := b.ReadAt(ctx, "/readat.txt", 7, 6)
	require.NoError(t, err)
	assert.Equal(t, []byte("WebDAV"), data)
}

// TestReadAt_FullFile verifies that ReadAt with offset=0 and length=fileSize
// returns the full file content.
func TestReadAt_FullFile(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	content := []byte("full WebDAV file content")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/full.txt", nil))

	data, err := b.ReadAt(ctx, "/full.txt", 0, int64(len(content)))
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

// TestReadAt_NotConnected verifies that ReadAt returns ErrNotConnected when the
// backend is not connected.
func TestReadAt_NotConnected(t *testing.T) {
	b := New()
	_, err := b.ReadAt(context.Background(), "/file.txt", 0, 10)
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
}

// TestChunkSize_WebDAV verifies that WebDAV ChunkSize() returns 0 (no chunking
// hint; the FUSE layer should use unbounded sequential reads).
func TestChunkSize_WebDAV(t *testing.T) {
	b := New()
	assert.Equal(t, int64(0), b.ChunkSize(), "WebDAV ChunkSize must be 0")
}

// TestReconnect verifies the Connect → Disconnect → Connect lifecycle.
func TestReconnect(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	cfg := plugins.BackendConfig{
		Params: map[string]string{
			"url":          serverURL,
			"username":     testUser,
			"password":     testPass,
			"authType":     "basic",
			"pollInterval": testPollInterval,
		},
	}

	b := New()

	// First connect.
	require.NoError(t, b.Connect(cfg))
	assert.True(t, b.IsConnected())

	// Disconnect.
	require.NoError(t, b.Disconnect())
	assert.False(t, b.IsConnected())

	// Reconnect must work transparently.
	require.NoError(t, b.Connect(cfg))
	assert.True(t, b.IsConnected())
	_ = b.Disconnect()
}

// ─── ReadAt complementary tests (#121) ───────────────────────────────────────

// propfindResp207 is a minimal 207 Multistatus XML body used by mock HTTP
// servers to satisfy the PROPFIND probe issued during Connect().
const propfindResp207 = `<?xml version="1.0" encoding="utf-8"?>` +
	`<D:multistatus xmlns:D="DAV:">` +
	`<D:response><D:href>/</D:href>` +
	`<D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop>` +
	`<D:status>HTTP/1.1 200 OK</D:status></D:propstat>` +
	`</D:response></D:multistatus>`

// TestReadAt_RangeHeader_Sent verifies that ReadAt sends a well-formed
// "Range: bytes=<offset>-<end>" header to the server and correctly processes
// a 206 Partial Content response.
// This is the primary test for the HTTP Range read path (#121).
func TestReadAt_RangeHeader_Sent(t *testing.T) {
	content := []byte("Hello, WebDAV Range Header Test!")

	var capturedRange atomic.Value // written by handler goroutine, read by test

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != testUser || pass != testPass {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(propfindResp207))
		case "GET":
			capturedRange.Store(r.Header.Get("Range"))
			// Respond 206 Partial Content with bytes [7:13)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Range", "bytes 7-12/32")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(content[7:13])
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	b := New()
	require.NoError(t, b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url": srv.URL, "username": testUser, "password": testPass, "authType": "basic",
		},
	}))
	t.Cleanup(func() { _ = b.Disconnect() })

	// ReadAt offset=7, length=6 → Range header must be "bytes=7-12"
	data, err := b.ReadAt(context.Background(), "/range-test.txt", 7, 6)
	require.NoError(t, err)
	assert.Equal(t, content[7:13], data,
		"ReadAt must return bytes from the 206 Partial Content response")

	rng, _ := capturedRange.Load().(string)
	assert.Equal(t, "bytes=7-12", rng,
		"ReadAt must send Range header with correct byte range (bytes=offset-offset+length-1)")
}

// TestReadAt_NoRangeSupport_200_Fallback verifies that ReadAt correctly handles
// a server that ignores Range headers and returns 200 with the full body.
// The implementation must fall back to downloading the full body and extracting
// the requested slice.
func TestReadAt_NoRangeSupport_200_Fallback(t *testing.T) {
	content := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ") // 26 bytes

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != testUser || pass != testPass {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(propfindResp207))
		case "GET":
			// Explicitly ignore Range header — return 200 with full body.
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	b := New()
	require.NoError(t, b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url": srv.URL, "username": testUser, "password": testPass, "authType": "basic",
		},
	}))
	t.Cleanup(func() { _ = b.Disconnect() })

	// Read bytes [5:10) → "FGHIJ"
	data, err := b.ReadAt(context.Background(), "/alphabet.txt", 5, 5)
	require.NoError(t, err,
		"ReadAt must succeed even when server returns 200 (no Range support)")
	assert.Equal(t, []byte("FGHIJ"), data,
		"ReadAt must correctly slice the full-body 200 response")
}

// TestReadAt_FileNotFound verifies that ReadAt returns a wrapped ErrFileNotFound
// when the remote file does not exist on the server (HTTP 404).
func TestReadAt_FileNotFound(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)

	_, err := b.ReadAt(context.Background(), "/does-not-exist.txt", 0, 10)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound,
		"ReadAt on a non-existent remote file must return ErrFileNotFound (wrapped)")
}

// TestReadAt_ContextCancelled verifies that ReadAt returns an error when the
// context is already cancelled before the call.
func TestReadAt_ContextCancelled(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel before calling ReadAt

	_, err := b.ReadAt(ctx, "/file.txt", 0, 10)
	assert.Error(t, err,
		"ReadAt with a pre-cancelled context must return an error")
}

// ─── Version and Watch enrichment tests (#130 #131) ──────────────────────────

// TestFileInfoVersionFromETag verifies that Stat() returns FileInfo.Version
// equal to the ETag (stripped of surrounding quotes).  Both must be non-empty
// on a standard WebDAV server that provides ETags for files (#131).
func TestFileInfoVersionFromETag(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	content := []byte("version-from-etag-test-content")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/version-etag.txt", nil))

	fi, err := b.Stat(ctx, "/version-etag.txt")
	require.NoError(t, err)
	require.NotNil(t, fi)

	assert.NotEmpty(t, fi.ETag, "ETag must not be empty after upload (#131)")
	assert.Equal(t, fi.ETag, fi.Version,
		"FileInfo.Version must equal ETag (stripped of quotes) for the WebDAV backend (#131)")
}

// TestWatchEmitsModTime verifies that Watch populates FileEvent.ModTime
// (non-zero, reflecting the new mtime) and FileEvent.PreviousModTime
// (non-zero, reflecting the snapshot mtime) on a FileEventModified event (#130).
func TestWatchEmitsModTime(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Upload the file BEFORE Watch so it appears in the initial baseline snapshot.
	content1 := []byte("modtime-v1-initial-content") // 26 bytes
	src1 := writeTempFile(t, content1)
	require.NoError(t, b.Upload(ctx, src1, "/modtime-watch.txt", nil))

	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	// Allow the watcher goroutine to build its baseline snapshot.
	time.Sleep(100 * time.Millisecond)

	// Re-upload with different content size so FileEventModified is emitted.
	content2 := []byte("modtime-v2-updated-content-with-more-bytes") // 42 bytes — larger
	src2 := writeTempFile(t, content2)
	go func() {
		_ = b.Upload(context.Background(), src2, "/modtime-watch.txt", nil)
	}()

	deadline := time.After(8 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			require.True(t, ok, "Watch channel must not close prematurely")
			if ev.Type == plugins.FileEventModified {
				assert.False(t, ev.ModTime.IsZero(),
					"FileEvent.ModTime must not be zero for FileEventModified (#130)")
				assert.False(t, ev.PreviousModTime.IsZero(),
					"FileEvent.PreviousModTime must not be zero when file was in the baseline snapshot (#130)")
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for FileEventModified with non-zero ModTime and PreviousModTime (#130)")
		}
	}
}

// TestWatchMetadataChanged verifies that Watch emits FileEventMetadataChanged
// with MetadataOnly=true when a file's ETag changes while its size remains
// stable.  Uses a custom mock HTTP server with controlled PROPFIND responses so
// that the test is deterministic and does not depend on filesystem timing.
// This exercises the ETag-only change detection path in the WebDAV Watch
// goroutine (#130).
func TestWatchMetadataChanged(t *testing.T) {
	const (
		fileName = "meta-change.txt"
		etag1    = "abc123def456"
		etag2    = "xyz789uvw012" // different ETag, same size
	)

	modTime1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	modTime2 := time.Date(2025, 1, 1, 0, 1, 0, 0, time.UTC) // one minute later

	// buildListResp builds a Depth:1 PROPFIND response with the root collection
	// and one file entry with the given ETag and modification time.
	buildListResp := func(etag string, mt time.Time) string {
		return `<?xml version="1.0" encoding="utf-8"?>` +
			`<D:multistatus xmlns:D="DAV:">` +
			// Root self-entry (always present for Depth:1 responses).
			`<D:response><D:href>/</D:href>` +
			`<D:propstat><D:prop>` +
			`<D:resourcetype><D:collection/></D:resourcetype>` +
			`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>` +
			// File entry — ETag changes between polls, size stays constant.
			`<D:response><D:href>/` + fileName + `</D:href>` +
			`<D:propstat><D:prop>` +
			`<D:resourcetype/>` +
			`<D:getcontentlength>10</D:getcontentlength>` +
			`<D:getlastmodified>` + mt.UTC().Format(http.TimeFormat) + `</D:getlastmodified>` +
			`<D:getetag>"` + etag + `"</D:getetag>` +
			`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>` +
			`</D:multistatus>`
	}

	// depth1CallCount counts Depth:1 PROPFIND requests (List calls).
	// Call 1 (buildSnapshot) sets the snapshot; call 2+ trigger MetadataChanged.
	var depth1CallCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != testUser || pass != testPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="webdav"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != "PROPFIND" {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		switch r.Header.Get("Depth") {
		case "0":
			// Connect probe — return root collection only.
			_, _ = w.Write([]byte(propfindResp207))
		default:
			// Depth:1 — Watch polling.
			n := depth1CallCount.Add(1)
			if n <= 1 {
				// First call (buildSnapshot): establish snapshot with etag1.
				_, _ = w.Write([]byte(buildListResp(etag1, modTime1)))
			} else {
				// Subsequent calls: etag2 with same size → MetadataChanged.
				_, _ = w.Write([]byte(buildListResp(etag2, modTime2)))
			}
		}
	}))
	defer srv.Close()

	b := New()
	require.NoError(t, b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"url":          srv.URL,
			"username":     testUser,
			"password":     testPass,
			"authType":     "basic",
			"pollInterval": testPollInterval,
		},
	}))
	t.Cleanup(func() { _ = b.Disconnect() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	// The first List (inside buildSnapshot) captures etag1.  Every subsequent
	// poll returns etag2 with the same size → Watch must emit MetadataChanged.
	deadline := time.After(4 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			require.True(t, ok, "Watch channel must not close prematurely")
			if ev.Type == plugins.FileEventMetadataChanged {
				assert.True(t, ev.MetadataOnly,
					"MetadataOnly must be true for FileEventMetadataChanged (#130)")
				assert.False(t, ev.ModTime.IsZero(),
					"FileEvent.ModTime must not be zero for MetadataChanged (#130)")
				assert.False(t, ev.PreviousModTime.IsZero(),
					"FileEvent.PreviousModTime must not be zero when file was in snapshot (#130)")
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for FileEventMetadataChanged with ETag-only change (#130)")
		}
	}
}

// TestWatchModified verifies that Watch emits FileEventModified (not
// FileEventMetadataChanged) with MetadataOnly=false when a file's content
// size changes.  A size change is the definitive signal that file content was
// modified, not just metadata (#130).
func TestWatchModified(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()
	b := newTestBackend(t, serverURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Upload the initial file BEFORE Watch so it is in the baseline snapshot.
	smallContent := []byte("small-initial") // 13 bytes
	src1 := writeTempFile(t, smallContent)
	require.NoError(t, b.Upload(ctx, src1, "/size-change.txt", nil))

	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	// Allow the watcher to build its baseline snapshot.
	time.Sleep(100 * time.Millisecond)

	// Re-upload with larger content so the size changes → FileEventModified.
	largeContent := []byte("much-larger-content-triggers-file-modified-event") // 48 bytes
	src2 := writeTempFile(t, largeContent)
	go func() {
		_ = b.Upload(context.Background(), src2, "/size-change.txt", nil)
	}()

	deadline := time.After(8 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			require.True(t, ok, "Watch channel must not close prematurely")
			if ev.Type == plugins.FileEventModified {
				assert.False(t, ev.MetadataOnly,
					"MetadataOnly must be false for FileEventModified when size changed (#130)")
				assert.False(t, ev.ModTime.IsZero(),
					"FileEvent.ModTime must not be zero for FileEventModified (#130)")
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for FileEventModified with MetadataOnly=false (#130)")
		}
	}
}
