package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// newConnected returns a connected Backend whose rootPath is a fresh temp dir.
// The backend is automatically disconnected (and temp dir removed) when the
// test ends.
func newConnected(t *testing.T) (*Backend, string) {
	t.Helper()
	dir := t.TempDir()
	b := New()
	require.NoError(t, b.Connect(plugins.BackendConfig{
		Params: map[string]string{"rootPath": dir},
	}))
	t.Cleanup(func() { _ = b.Disconnect() })
	return b, dir
}

// writeFile creates a file with the given content in rootDir.
func writeFile(t *testing.T, rootDir, rel string, content []byte) string {
	t.Helper()
	p := filepath.Join(rootDir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0755))
	require.NoError(t, os.WriteFile(p, content, 0644))
	return p
}

// ─── Connect / Disconnect ─────────────────────────────────────────────────────

func TestConnect_OK(t *testing.T) {
	dir := t.TempDir()
	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{"rootPath": dir},
	})
	require.NoError(t, err)
	assert.True(t, b.IsConnected())
}

func TestConnect_InvalidPath(t *testing.T) {
	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{"rootPath": "/this/path/does/not/exist/at/all"},
	})
	assert.Error(t, err)
	assert.False(t, b.IsConnected())
}

func TestConnect_MissingParam(t *testing.T) {
	b := New()
	err := b.Connect(plugins.BackendConfig{Params: map[string]string{}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rootPath")
}

func TestDisconnect(t *testing.T) {
	b, _ := newConnected(t)

	require.NoError(t, b.Disconnect())
	assert.False(t, b.IsConnected())
	// Second disconnect is a no-op
	require.NoError(t, b.Disconnect())
}

// ─── Not-connected guards ─────────────────────────────────────────────────────

func TestUpload_NotConnected(t *testing.T) {
	b := New()
	err := b.Upload(context.Background(), "/src", "/dst", nil)
	assert.ErrorIs(t, err, ErrNotConnected)
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
}

func TestDownload_NotConnected(t *testing.T) {
	b := New()
	err := b.Download(context.Background(), "/remote", "/local", nil)
	assert.ErrorIs(t, err, ErrNotConnected)
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
}

func TestList_NotConnected(t *testing.T) {
	b := New()
	_, err := b.List(context.Background(), "")
	assert.ErrorIs(t, err, ErrNotConnected)
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
}

func TestStat_NotConnected(t *testing.T) {
	b := New()
	_, err := b.Stat(context.Background(), "file.txt")
	assert.ErrorIs(t, err, ErrNotConnected)
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
}

func TestDelete_NotConnected(t *testing.T) {
	b := New()
	err := b.Delete(context.Background(), "file.txt")
	assert.ErrorIs(t, err, ErrNotConnected)
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
}

func TestCreateDir_NotConnected(t *testing.T) {
	b := New()
	err := b.CreateDir(context.Background(), "subdir")
	assert.ErrorIs(t, err, ErrNotConnected)
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
}

func TestMove_NotConnected(t *testing.T) {
	b := New()
	err := b.Move(context.Background(), "a.txt", "b.txt")
	assert.ErrorIs(t, err, ErrNotConnected)
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
}

func TestWatch_NotConnected(t *testing.T) {
	b := New()
	_, err := b.Watch(context.Background(), "")
	assert.ErrorIs(t, err, ErrNotConnected)
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
}

// ─── Upload / Download ────────────────────────────────────────────────────────

func TestUploadDownload_Roundtrip(t *testing.T) {
	b, dir := newConnected(t)

	content := []byte("roundtrip test content — こんにちは")

	// Create source file outside rootPath
	srcFile := filepath.Join(t.TempDir(), "source.txt")
	require.NoError(t, os.WriteFile(srcFile, content, 0644))

	// Upload into rootPath
	require.NoError(t, b.Upload(context.Background(), srcFile, "uploaded.txt", nil))

	// Confirm file is present on "remote" (inside rootPath)
	_, err := os.Stat(filepath.Join(dir, "uploaded.txt"))
	require.NoError(t, err)

	// Download to a third location
	dstFile := filepath.Join(t.TempDir(), "downloaded.txt")
	require.NoError(t, b.Download(context.Background(), "uploaded.txt", dstFile, nil))

	got, err := os.ReadFile(dstFile)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestUpload_CreatesParentDirs(t *testing.T) {
	b, dir := newConnected(t)

	srcFile := filepath.Join(t.TempDir(), "src.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("hello"), 0644))

	// Upload to a nested path; "subdir" does not exist in rootPath yet.
	err := b.Upload(context.Background(), srcFile, "subdir/nested/file.txt", nil)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "subdir", "nested", "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), content)
}

func TestUpload_ProgressCallback(t *testing.T) {
	b, _ := newConnected(t)

	srcFile := filepath.Join(t.TempDir(), "src.txt")
	payload := make([]byte, 4096)
	require.NoError(t, os.WriteFile(srcFile, payload, 0644))

	var calls int
	progress := func(done, total int64) {
		calls++
		assert.GreaterOrEqual(t, done, int64(0))
		assert.Equal(t, int64(4096), total)
	}
	require.NoError(t, b.Upload(context.Background(), srcFile, "progress.bin", progress))
	assert.Greater(t, calls, 0)
}

func TestDownload_NotFound(t *testing.T) {
	b, _ := newConnected(t)
	err := b.Download(context.Background(), "nonexistent.txt", filepath.Join(t.TempDir(), "out.txt"), nil)
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── List ─────────────────────────────────────────────────────────────────────

func TestList_Empty(t *testing.T) {
	b, _ := newConnected(t)

	entries, err := b.List(context.Background(), "")
	require.NoError(t, err)
	assert.NotNil(t, entries)
	assert.Empty(t, entries)
}

func TestList_WithFiles(t *testing.T) {
	b, dir := newConnected(t)

	for _, name := range []string{"alpha.txt", "beta.txt", "gamma.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644))
	}

	entries, err := b.List(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	// All entries should have Name and Path populated
	for _, e := range entries {
		assert.NotEmpty(t, e.Name)
		assert.NotEmpty(t, e.Path)
		assert.False(t, e.IsDir)
	}
}

func TestList_WithSubdir(t *testing.T) {
	b, dir := newConnected(t)

	require.NoError(t, os.Mkdir(filepath.Join(dir, "mydir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0644))

	entries, err := b.List(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	var dirCount, fileCount int
	for _, e := range entries {
		if e.IsDir {
			dirCount++
		} else {
			fileCount++
		}
	}
	assert.Equal(t, 1, dirCount)
	assert.Equal(t, 1, fileCount)
}

func TestList_NotFound(t *testing.T) {
	b, _ := newConnected(t)

	_, err := b.List(context.Background(), "nonexistent-dir")
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── Stat ─────────────────────────────────────────────────────────────────────

func TestStat_File(t *testing.T) {
	b, dir := newConnected(t)

	content := []byte("stat test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stat.txt"), content, 0644))

	fi, err := b.Stat(context.Background(), "stat.txt")
	require.NoError(t, err)
	require.NotNil(t, fi)
	assert.Equal(t, "stat.txt", fi.Name)
	assert.Equal(t, int64(len(content)), fi.Size)
	assert.False(t, fi.IsDir)
	assert.False(t, fi.ModTime.IsZero())
}

func TestStat_Dir(t *testing.T) {
	b, dir := newConnected(t)

	require.NoError(t, os.Mkdir(filepath.Join(dir, "mydir"), 0755))

	fi, err := b.Stat(context.Background(), "mydir")
	require.NoError(t, err)
	require.NotNil(t, fi)
	assert.Equal(t, "mydir", fi.Name)
	assert.True(t, fi.IsDir)
}

func TestStat_NotFound(t *testing.T) {
	b, _ := newConnected(t)

	_, err := b.Stat(context.Background(), "no-such-file.txt")
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestDelete_OK(t *testing.T) {
	b, dir := newConnected(t)

	path := filepath.Join(dir, "delete-me.txt")
	require.NoError(t, os.WriteFile(path, []byte("bye"), 0644))

	require.NoError(t, b.Delete(context.Background(), "delete-me.txt"))

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "file should no longer exist")
}

func TestDelete_NotFound(t *testing.T) {
	b, _ := newConnected(t)

	err := b.Delete(context.Background(), "no-such-file.txt")
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── CreateDir ────────────────────────────────────────────────────────────────

func TestCreateDir_OK(t *testing.T) {
	b, dir := newConnected(t)

	require.NoError(t, b.CreateDir(context.Background(), "newdir"))

	info, err := os.Stat(filepath.Join(dir, "newdir"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestCreateDir_Idempotent(t *testing.T) {
	b, _ := newConnected(t)

	require.NoError(t, b.CreateDir(context.Background(), "idem"))
	// Second call must not return an error
	require.NoError(t, b.CreateDir(context.Background(), "idem"))
}

// ─── Move ─────────────────────────────────────────────────────────────────────

func TestMove_OK(t *testing.T) {
	b, dir := newConnected(t)

	src := filepath.Join(dir, "before.txt")
	require.NoError(t, os.WriteFile(src, []byte("move me"), 0644))

	require.NoError(t, b.Move(context.Background(), "before.txt", "after.txt"))

	_, err := os.Stat(src)
	assert.True(t, os.IsNotExist(err), "source should be gone after move")

	content, err := os.ReadFile(filepath.Join(dir, "after.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("move me"), content)
}

func TestMove_NotFound(t *testing.T) {
	b, _ := newConnected(t)

	err := b.Move(context.Background(), "ghost.txt", "nowhere.txt")
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── Path traversal protection ───────────────────────────────────────────────

func TestAbsPath_TraversalBlocked(t *testing.T) {
	b, _ := newConnected(t)

	// Classic traversal sequences that should be rejected.
	traversalPaths := []string{
		"../../etc/passwd",
		"../secret.txt",
		"subdir/../../../etc/passwd",
	}
	for _, p := range traversalPaths {
		t.Run(p, func(t *testing.T) {
			err := b.Delete(context.Background(), p)
			require.Error(t, err)
			// The traversal check must fire before any file-not-found check.
			assert.False(t, errors.Is(err, plugins.ErrFileNotFound),
				"expected traversal error, not ErrFileNotFound for %q", p)
			assert.Contains(t, err.Error(), "s'échappe de rootPath")
		})
	}
}

func TestAbsPath_TraversalWithEncoding(t *testing.T) {
	b, _ := newConnected(t)

	// "..%2F..%2Fetc%2Fpasswd" — Go's filepath does NOT decode percent-encoding,
	// so this string is treated as a single path component (literal '%' chars)
	// and remains safely within rootPath.  This test documents that behaviour:
	// URL-decoding must happen at a higher layer (HTTP/Wails) before reaching
	// the backend; the backend is not an HTTP server and must not decode URLs.
	encoded := "..%2F..%2Fetc%2Fpasswd"
	// Because filepath keeps the literal %, the resolved path stays inside
	// rootPath → no traversal error, only "file not found" at most.
	err := b.Delete(context.Background(), encoded)
	// We expect ErrFileNotFound (file does not exist), NOT a traversal error.
	assert.ErrorIs(t, err, plugins.ErrFileNotFound,
		"percent-encoded path should be treated as a literal filename within rootPath")
}

func TestAbsPath_ValidSubpath(t *testing.T) {
	b, dir := newConnected(t)

	// A well-formed relative path must pass through without error.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "allowed.txt"), []byte("ok"), 0644))

	fi, err := b.Stat(context.Background(), "allowed.txt")
	require.NoError(t, err)
	assert.Equal(t, "allowed.txt", fi.Name)
}

// ─── Watch ────────────────────────────────────────────────────────────────────

func TestWatch_ReceivesCreate(t *testing.T) {
	b, dir := newConnected(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := b.Watch(ctx, "")
	require.NoError(t, err)

	// Give the watcher a moment to initialise before creating the file.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "watch-test.txt"), []byte("event"), 0644)
	}()

	select {
	case event, ok := <-ch:
		require.True(t, ok, "channel should not be closed yet")
		assert.Equal(t, plugins.FileEventCreated, event.Type)
		assert.Equal(t, "local", event.Source)
		assert.False(t, event.Timestamp.IsZero())
	case <-ctx.Done():
		t.Fatal("timed out waiting for FileEventCreated")
	}
}

func TestWatch_ClosesOnCancel(t *testing.T) {
	b, _ := newConnected(t)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := b.Watch(ctx, "")
	require.NoError(t, err)

	// Cancel the context; the goroutine should close the channel.
	cancel()

	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // success: channel closed as expected
			}
			// Drain any buffered events before the close.
		case <-deadline:
			t.Fatal("channel not closed within 500 ms after context cancellation")
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
	assert.Equal(t, "local", d.Type,
		"Describe().Type must equal \"local\"")

	// At least the rootPath param must be declared
	assert.GreaterOrEqual(t, len(d.Params), 1,
		"Describe().Params must contain at least one ParamSpec")

	// First param must be rootPath, of type path, and required
	if len(d.Params) >= 1 {
		p := d.Params[0]
		assert.Equal(t, "rootPath", p.Key,
			"Describe().Params[0].Key must be \"rootPath\"")
		assert.Equal(t, plugins.ParamTypePath, p.Type,
			"Describe().Params[0].Type must be ParamTypePath")
		assert.True(t, p.Required,
			"Describe().Params[0].Required must be true")
	}

	// DisplayName and Description must be non-empty
	assert.NotEmpty(t, d.DisplayName,
		"Describe().DisplayName must not be empty")
	assert.NotEmpty(t, d.Description,
		"Describe().Description must not be empty")
}

// ─── GetQuota ────────────────────────────────────────────────────────────────

func TestGetQuota_Connected(t *testing.T) {
	b, _ := newConnected(t)
	free, total, err := b.GetQuota(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, free, int64(0), "free space must be non-negative")
	assert.Greater(t, total, int64(0), "total space must be positive")
	assert.LessOrEqual(t, free, total, "free must not exceed total")
}

func TestGetQuota_NotConnected(t *testing.T) {
	b := New()
	free, total, err := b.GetQuota(context.Background())
	assert.ErrorIs(t, err, plugins.ErrNotConnected)
	assert.Equal(t, int64(0), free)
	assert.Equal(t, int64(0), total)
}
