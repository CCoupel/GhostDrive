//go:build windows

package placeholder

// Integration-level tests for the #160/#162 cache-integrity fix (tasks 13-18,
// _work/reports/plan-20260812-102022.md révision 2): a truncated download
// must never be servable as a complete file, and concurrent Open()s on the
// same path must not each trigger their own download.
//
// These complement (not duplicate) the pure-unit isCacheFresh tests already
// in filesystem_whitebox_test.go — TestIsCacheFresh_SizeMismatch_ReturnsFalse
// covers the size-comparison logic in isolation; the tests here exercise the
// full ensureDownloaded/downloadToCache wiring end-to-end through a mock
// StorageBackend, which is what actually proves the fix closes D7 (silent
// data loss: a truncated file served as complete for up to cacheTTL).
//
// Each test defines its own minimal plugins.StorageBackend mock rather than
// extending the shared mockStorageBackend (metacache_vfs_test.go), which is
// reused across many other test files and always returns a nil-op Download —
// these tests need Download to fail partway, succeed with specific content,
// or block, none of which the shared mock supports.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── partialFailBackend (task 29 — CA11) ──────────────────────────────────────

// partialFailBackend simulates a backend whose Download writes some bytes to
// the destination path and then fails — the realistic shape of the
// interruption that produced the #160/#162 truncated-cache data loss (D7):
// the local file exists and has a non-zero size when the error is returned.
type partialFailBackend struct {
	calls    atomic.Int32
	statSize int64
}

func (b *partialFailBackend) Name() string    { return "partial-fail" }
func (b *partialFailBackend) Version() string { return "0.0.0" }
func (b *partialFailBackend) Describe() plugins.PluginDescriptor {
	return plugins.PluginDescriptor{Type: "mock", DisplayName: "PartialFail"}
}
func (b *partialFailBackend) Connect(_ plugins.BackendConfig) error { return nil }
func (b *partialFailBackend) Disconnect() error                     { return nil }
func (b *partialFailBackend) IsConnected() bool                     { return true }
func (b *partialFailBackend) GetQuota(_ context.Context) (int64, int64, error) {
	return 0, 0, nil
}
func (b *partialFailBackend) Upload(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}
func (b *partialFailBackend) Delete(_ context.Context, _ string) error    { return nil }
func (b *partialFailBackend) Move(_ context.Context, _, _ string) error   { return nil }
func (b *partialFailBackend) Rename(_ context.Context, _, _ string) error { return nil }
func (b *partialFailBackend) Copy(_ context.Context, _, _ string) error   { return nil }
func (b *partialFailBackend) CreateDir(_ context.Context, _ string) error { return nil }
func (b *partialFailBackend) Stat(_ context.Context, _ string) (*plugins.FileInfo, error) {
	return &plugins.FileInfo{Size: b.statSize}, nil
}
func (b *partialFailBackend) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	return nil, nil
}
func (b *partialFailBackend) ReadAt(_ context.Context, _ string, _, _ int64) ([]byte, error) {
	return nil, nil
}
func (b *partialFailBackend) ChunkSize() int64 { return 0 }
func (b *partialFailBackend) Watch(_ context.Context, _ string) (<-chan plugins.FileEvent, error) {
	ch := make(chan plugins.FileEvent)
	close(ch)
	return ch, nil
}

// Download always writes a partial payload to localPath and then fails —
// every attempt of downloadToCache's internal retry loop behaves this way, so
// the test can assert on the state left behind after all attempts are spent.
func (b *partialFailBackend) Download(_ context.Context, _, localPath string, _ plugins.ProgressCallback) error {
	b.calls.Add(1)
	_ = os.WriteFile(localPath, []byte("partial-bytes-before-failure"), 0644)
	return errors.New("simulated transfer interruption")
}

// TestEnsureDownloaded_PartialRemovedOnError verifies that a failed download
// leaves NOTHING exploitable at the final cache path — no partial file that a
// later isCacheFresh() call could mistake for a complete one — and that the
// private temp file is cleaned up after every failed attempt, not only the
// last (CA11, task 29).
func TestEnsureDownloaded_PartialRemovedOnError(t *testing.T) {
	mock := &partialFailBackend{}
	fs := newGhostFileSystem([]MountedBackend{{
		ID: "test-backend", Name: "TestBackend", Backend: mock,
		Config: plugins.BackendConfig{ID: "test-backend"},
	}}, nil)

	r := fs.route("/TestBackend/big.mp4")
	require.NotNil(t, r, "route must resolve the configured TestBackend")

	_, err := fs.ensureDownloaded(r)
	require.Error(t, err, "ensureDownloaded must surface the download failure after exhausting retries")

	local := cachePath(r.config.ID, r.relPath)
	_, statErr := os.Stat(local)
	assert.True(t, os.IsNotExist(statErr),
		"a failed download must leave NO file at the final cache path — a partial file served as complete is exactly the #160/#162 data-loss bug (D7)")

	tmp := local + ".ghostdrive.tmp"
	_, tmpErr := os.Stat(tmp)
	assert.True(t, os.IsNotExist(tmpErr),
		"the private temp file must be removed after every failed attempt, not left dangling")

	assert.Equal(t, int32(downloadMaxRetries), mock.calls.Load(),
		"ensureDownloaded must exhaust all configured retry attempts before giving up")
}

// ─── sizedBackend (task 30 — CA12) ─────────────────────────────────────────────

// sizedBackend serves fixed content and reports its real length via Stat —
// the minimum needed to exercise the isCacheFresh(size) comparison through
// the full ensureDownloaded path rather than calling isCacheFresh directly.
type sizedBackend struct {
	content       []byte
	downloadCalls atomic.Int32
}

func (b *sizedBackend) Name() string    { return "sized" }
func (b *sizedBackend) Version() string { return "0.0.0" }
func (b *sizedBackend) Describe() plugins.PluginDescriptor {
	return plugins.PluginDescriptor{Type: "mock", DisplayName: "Sized"}
}
func (b *sizedBackend) Connect(_ plugins.BackendConfig) error { return nil }
func (b *sizedBackend) Disconnect() error                     { return nil }
func (b *sizedBackend) IsConnected() bool                     { return true }
func (b *sizedBackend) GetQuota(_ context.Context) (int64, int64, error) {
	return 0, 0, nil
}
func (b *sizedBackend) Upload(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}
func (b *sizedBackend) Delete(_ context.Context, _ string) error    { return nil }
func (b *sizedBackend) Move(_ context.Context, _, _ string) error   { return nil }
func (b *sizedBackend) Rename(_ context.Context, _, _ string) error { return nil }
func (b *sizedBackend) Copy(_ context.Context, _, _ string) error   { return nil }
func (b *sizedBackend) CreateDir(_ context.Context, _ string) error { return nil }
func (b *sizedBackend) Stat(_ context.Context, _ string) (*plugins.FileInfo, error) {
	return &plugins.FileInfo{Size: int64(len(b.content))}, nil
}
func (b *sizedBackend) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	return nil, nil
}
func (b *sizedBackend) ReadAt(_ context.Context, _ string, _, _ int64) ([]byte, error) {
	return nil, nil
}
func (b *sizedBackend) ChunkSize() int64 { return 0 }
func (b *sizedBackend) Watch(_ context.Context, _ string) (<-chan plugins.FileEvent, error) {
	ch := make(chan plugins.FileEvent)
	close(ch)
	return ch, nil
}
func (b *sizedBackend) Download(_ context.Context, _, localPath string, _ plugins.ProgressCallback) error {
	b.downloadCalls.Add(1)
	return os.WriteFile(localPath, b.content, 0644)
}

// TestEnsureDownloaded_RedownloadsTruncatedCache is the end-to-end
// non-regression test for the #160/#162 data-loss defect (D7): a cache file
// left behind by a previous interrupted download (non-zero size, recent
// mtime, but SHORTER than the remote) must never be served as complete — it
// must trigger a real redownload, and the file ultimately served must be the
// full, correct content (CA12, task 30 — "le plus important de cette phase").
//
// This is deliberately an integration test through ensureDownloaded rather
// than a second unit test of isCacheFresh (already covered by
// TestIsCacheFresh_SizeMismatch_ReturnsFalse in filesystem_whitebox_test.go):
// it is the only test that proves isCacheFresh, remoteSize and
// downloadToCache are actually wired together correctly.
func TestEnsureDownloaded_RedownloadsTruncatedCache(t *testing.T) {
	remoteContent := []byte("this is the complete, full-size remote file content")
	mock := &sizedBackend{content: remoteContent}
	fs := newGhostFileSystem([]MountedBackend{{
		ID: "test-backend", Name: "TestBackend", Backend: mock,
		Config: plugins.BackendConfig{ID: "test-backend"},
	}}, nil)

	r := fs.route("/TestBackend/movie.mp4")
	require.NotNil(t, r, "route must resolve the configured TestBackend")

	// Pre-seed the cache path with a TRUNCATED file — the shape a previous
	// failed download (pre-fix) would have left behind: non-zero size,
	// recent mtime, but shorter than the real remote content.
	local := cachePath(r.config.ID, r.relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(local), 0755))
	require.NoError(t, os.WriteFile(local, remoteContent[:10], 0644))

	got, err := fs.ensureDownloaded(r)
	require.NoError(t, err, "ensureDownloaded must transparently redownload a truncated cache entry")
	assert.Equal(t, local, got)

	assert.GreaterOrEqual(t, mock.downloadCalls.Load(), int32(1),
		"a size-mismatched cache entry must trigger a real redownload, not be served as-is")

	served, readErr := os.ReadFile(local)
	require.NoError(t, readErr)
	assert.Equal(t, remoteContent, served,
		"after redownload, the served file must be the COMPLETE remote content — a truncated file must never be served as complete (D7)")
}

// ─── slowBackend (task 31 — CA13) ──────────────────────────────────────────────

// slowBackend blocks inside Download until release is closed, so a test can
// launch several concurrent callers, let them all reach the download path,
// then release them together and observe how many real downloads happened.
type slowBackend struct {
	content       []byte
	downloadCalls atomic.Int32
	release       chan struct{}
}

func (b *slowBackend) Name() string    { return "slow" }
func (b *slowBackend) Version() string { return "0.0.0" }
func (b *slowBackend) Describe() plugins.PluginDescriptor {
	return plugins.PluginDescriptor{Type: "mock", DisplayName: "Slow"}
}
func (b *slowBackend) Connect(_ plugins.BackendConfig) error { return nil }
func (b *slowBackend) Disconnect() error                     { return nil }
func (b *slowBackend) IsConnected() bool                     { return true }
func (b *slowBackend) GetQuota(_ context.Context) (int64, int64, error) {
	return 0, 0, nil
}
func (b *slowBackend) Upload(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return nil
}
func (b *slowBackend) Delete(_ context.Context, _ string) error    { return nil }
func (b *slowBackend) Move(_ context.Context, _, _ string) error   { return nil }
func (b *slowBackend) Rename(_ context.Context, _, _ string) error { return nil }
func (b *slowBackend) Copy(_ context.Context, _, _ string) error   { return nil }
func (b *slowBackend) CreateDir(_ context.Context, _ string) error { return nil }
func (b *slowBackend) Stat(_ context.Context, _ string) (*plugins.FileInfo, error) {
	return &plugins.FileInfo{Size: int64(len(b.content))}, nil
}
func (b *slowBackend) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	return nil, nil
}
func (b *slowBackend) ReadAt(_ context.Context, _ string, _, _ int64) ([]byte, error) {
	return nil, nil
}
func (b *slowBackend) ChunkSize() int64 { return 0 }
func (b *slowBackend) Watch(_ context.Context, _ string) (<-chan plugins.FileEvent, error) {
	ch := make(chan plugins.FileEvent)
	close(ch)
	return ch, nil
}
func (b *slowBackend) Download(_ context.Context, _, localPath string, _ plugins.ProgressCallback) error {
	b.downloadCalls.Add(1)
	<-b.release // held open until the test releases every concurrent caller at once
	return os.WriteFile(localPath, b.content, 0644)
}

// TestEnsureDownloaded_ConcurrentOpensSingleDownload verifies that N
// concurrent Open()s (→ ensureDownloaded calls) on the same cache path
// trigger exactly ONE backend.Download call — every other caller attaches to
// the in-flight download instead of starting a redundant transfer (CA13,
// task 31, downloadCoordinator in filesystem_windows.go).
func TestEnsureDownloaded_ConcurrentOpensSingleDownload(t *testing.T) {
	content := []byte("deduplicated-download-content")
	mock := &slowBackend{content: content, release: make(chan struct{})}
	fs := newGhostFileSystem([]MountedBackend{{
		ID: "test-backend", Name: "TestBackend", Backend: mock,
		Config: plugins.BackendConfig{ID: "test-backend"},
	}}, nil)

	r := fs.route("/TestBackend/shared.bin")
	require.NotNil(t, r, "route must resolve the configured TestBackend")

	const concurrency = 8
	var wg sync.WaitGroup
	results := make([]error, concurrency)
	paths := make([]string, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := fs.ensureDownloaded(r)
			results[i] = err
			paths[i] = p
		}(i)
	}

	// Give every goroutine a chance to reach the coordinator/backend before
	// releasing the download — otherwise a slow scheduler could let the
	// first call finish before the others even start, which wouldn't
	// actually exercise deduplication.
	time.Sleep(150 * time.Millisecond)
	close(mock.release)

	wg.Wait()

	for i, err := range results {
		require.NoErrorf(t, err, "concurrent ensureDownloaded call %d must succeed", i)
		assert.Equal(t, paths[0], paths[i], "all concurrent callers must receive the same cache path")
	}
	assert.Equal(t, int32(1), mock.downloadCalls.Load(),
		"N concurrent Open()s on the same path must trigger exactly ONE backend.Download call")
}
