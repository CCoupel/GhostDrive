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
	expectedKeys := []string{"url", "username", "password", "token", "authType", "pollInterval", "tlsSkipVerify"}
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

	// "pollInterval" must be ParamTypeNumber
	if poll, ok := paramsByKey["pollInterval"]; ok {
		assert.Equal(t, plugins.ParamTypeNumber, poll.Type,
			"Describe() param \"pollInterval\" must be ParamTypeNumber")
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
