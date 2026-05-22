package cfapi

import (
	"context"
	"errors"
	gosync "sync"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/internal/cache"
	"github.com/CCoupel/GhostDrive/plugins"
)

// readAtBackend extends mockBackend with a configurable ReadAt.
type readAtBackend struct {
	mockBackend
	readAtData []byte
	readAtErr  error
	files      map[string]*plugins.FileInfo
	listItems  []plugins.FileInfo
}

func (r *readAtBackend) ReadAt(_ context.Context, _ string, _, _ int64) ([]byte, error) {
	return r.readAtData, r.readAtErr
}

func (r *readAtBackend) Stat(_ context.Context, path string) (*plugins.FileInfo, error) {
	if r.files != nil {
		if fi, ok := r.files[path]; ok {
			return fi, nil
		}
	}
	return &plugins.FileInfo{Name: "file.txt", Version: "etag1", ModTime: time.Now()}, nil
}

func (r *readAtBackend) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	return r.listItems, nil
}

// transferRecorder wraps SyncProvider to capture ExecuteTransfer calls.
type transferRecorder struct {
	SyncProvider
	transferred [][]byte
	syncStates  []SyncState
}

func (t *transferRecorder) ExecuteTransfer(_ FetchRequest, data []byte, _ bool) error {
	t.transferred = append(t.transferred, data)
	return nil
}

func (t *transferRecorder) SetSyncState(_ string, state SyncState) error {
	t.syncStates = append(t.syncStates, state)
	return nil
}

func (t *transferRecorder) CreatePlaceholders(_ string, items []PlaceholderInfo) (int, error) {
	return len(items), nil
}

func (t *transferRecorder) ReportError(_ FetchRequest, _ error) error                    { return nil }
func (t *transferRecorder) ReportProgress(_ FetchRequest, _, _ int64) error              { return nil }

// ─── Test helpers (shared within package cfapi test files) ────────────────────

// hitCache is a ChunkCache that always returns a cache hit with pre-configured data.
type hitCache struct {
	data  []byte
	etag  string
	mtime time.Time
}

func (h *hitCache) Get(_ context.Context, _ cache.ChunkKey, _ string, _ time.Time) (*cache.ChunkEntry, bool) {
	return &cache.ChunkEntry{Data: h.data, ETag: h.etag, MTime: h.mtime}, true
}
func (h *hitCache) Put(_ context.Context, _ cache.ChunkKey, _ cache.ChunkEntry) error { return nil }
func (h *hitCache) Invalidate(_ context.Context, _, _ string) error                   { return nil }
func (h *hitCache) InvalidateBackend(_ context.Context, _ string) error               { return nil }
func (h *hitCache) Stats() cache.CacheStats                                           { return cache.CacheStats{Hits: 1} }
func (h *hitCache) Close() error                                                      { return nil }

// trackingBackend counts ReadAt invocations to verify cache-hit shortcuts.
type trackingBackend struct {
	readAtBackend
	readAtCalls int
}

func (t *trackingBackend) ReadAt(ctx context.Context, path string, offset, length int64) ([]byte, error) {
	t.readAtCalls++
	return t.readAtBackend.ReadAt(ctx, path, offset, length)
}

// chunkedBackend exposes a configurable ChunkSize and counts ReadAt calls.
type chunkedBackend struct {
	readAtBackend
	chunkSz int64
	readAtN int
}

func (c *chunkedBackend) ChunkSize() int64 { return c.chunkSz }
func (c *chunkedBackend) ReadAt(_ context.Context, _ string, _ int64, length int64) ([]byte, error) {
	c.readAtN++
	return make([]byte, length), nil
}

// countingEmitter records how many times Emit is called (thread-safe).
type countingEmitter struct {
	mu    gosync.Mutex
	count int
}

func (e *countingEmitter) Emit(_ string, _ any) {
	e.mu.Lock()
	e.count++
	e.mu.Unlock()
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestHydratorFetchData_CacheMiss_ThenHit(t *testing.T) {
	ctx := context.Background()
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}

	b := &readAtBackend{
		mockBackend: mockBackend{name: "t", connected: true},
		readAtData:  data,
	}
	ch := cache.NewNoopCache() // always miss

	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(b, ch, provider, &noopEmitter{}, "backend-1")

	req := FetchRequest{
		LocalPath: provider.localPath + "/file.txt",
		Offset:    0,
		Length:    int64(len(data)),
	}

	// First fetch: cache miss → ReadAt.
	if err := h.OnFetchData(ctx, req); err != nil {
		t.Fatalf("OnFetchData (miss): %v", err)
	}
}

func TestHydratorFetchData_BackendError(t *testing.T) {
	ctx := context.Background()
	b := &readAtBackend{
		mockBackend: mockBackend{name: "t", connected: true},
		readAtErr:   errors.New("network error"),
	}
	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(b, nil, provider, &noopEmitter{}, "backend-1")

	req := FetchRequest{
		LocalPath: provider.localPath + "/file.txt",
		Offset:    0,
		Length:    4096,
	}

	err := h.OnFetchData(ctx, req)
	if err == nil {
		t.Fatal("OnFetchData: expected error on backend failure, got nil")
	}
}

func TestHydratorCancelFetch(t *testing.T) {
	// OnCancelFetch on an unknown path should not panic.
	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(&mockBackend{name: "t", connected: true}, nil, provider, nil, "b1")

	req := FetchRequest{LocalPath: "/unknown/path/file.txt"}
	h.OnCancelFetch(req) // must not panic
}

func TestHydratorFetchPlaceholders(t *testing.T) {
	ctx := context.Background()
	items := []plugins.FileInfo{
		{Name: "doc.txt", Size: 100, ModTime: time.Now()},
		{Name: "img.png", Size: 200, ModTime: time.Now(), IsDir: false},
	}
	b := &readAtBackend{
		mockBackend: mockBackend{name: "t", connected: true},
		listItems:   items,
	}
	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(b, nil, provider, &noopEmitter{}, "b1")

	// Should not error even though provider is a stub (no-op on Linux).
	if err := h.OnFetchPlaceholders(ctx, provider.localPath); err != nil {
		t.Fatalf("OnFetchPlaceholders: %v", err)
	}
}

// TestHydratorFetchData_CacheHit verifies that a cache hit bypasses ReadAt entirely.
func TestHydratorFetchData_CacheHit(t *testing.T) {
	ctx := context.Background()
	mtime := time.Now().Truncate(time.Second)
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}

	tb := &trackingBackend{
		readAtBackend: readAtBackend{
			mockBackend: mockBackend{name: "t", connected: true},
			readAtData:  data,
		},
	}
	hitC := &hitCache{data: data, etag: "etag1", mtime: mtime}

	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(tb, hitC, provider, &noopEmitter{}, "backend-1")

	req := FetchRequest{
		LocalPath: provider.localPath + "/file.txt",
		Offset:    0,
		Length:    int64(len(data)),
	}

	if err := h.OnFetchData(ctx, req); err != nil {
		t.Fatalf("OnFetchData (cache hit): %v", err)
	}

	if tb.readAtCalls != 0 {
		t.Errorf("cache hit: ReadAt called %d times, expected 0", tb.readAtCalls)
	}
}

// TestHydratorFetchData_MultiChunk verifies that a request spanning multiple chunks
// triggers one ReadAt call per chunk.
func TestHydratorFetchData_MultiChunk(t *testing.T) {
	ctx := context.Background()
	const smallChunk = 128 // bytes — much smaller than defaultChunkSize
	const numChunks = 3

	cb := &chunkedBackend{
		readAtBackend: readAtBackend{
			mockBackend: mockBackend{name: "t", connected: true},
		},
		chunkSz: smallChunk,
	}

	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(cb, cache.NewNoopCache(), provider, &noopEmitter{}, "backend-1")

	req := FetchRequest{
		LocalPath: provider.localPath + "/big.bin",
		Offset:    0,
		Length:    numChunks * smallChunk,
	}

	if err := h.OnFetchData(ctx, req); err != nil {
		t.Fatalf("OnFetchData (multi-chunk): %v", err)
	}

	if cb.readAtN != numChunks {
		t.Errorf("multi-chunk: ReadAt called %d times, expected %d", cb.readAtN, numChunks)
	}
}

// TestHydratorFetchData_Throttle verifies that sync:progress is not emitted more
// than once per 100 ms window even when many chunks are transferred rapidly.
func TestHydratorFetchData_Throttle(t *testing.T) {
	ctx := context.Background()
	const smallChunk = 64 // bytes
	const numChunks = 5

	cb := &chunkedBackend{
		readAtBackend: readAtBackend{
			mockBackend: mockBackend{name: "t", connected: true},
		},
		chunkSz: smallChunk,
	}

	emitter := &countingEmitter{}
	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(cb, cache.NewNoopCache(), provider, emitter, "backend-1")

	req := FetchRequest{
		LocalPath: provider.localPath + "/file.txt",
		Offset:    0,
		Length:    numChunks * smallChunk,
	}

	start := time.Now()
	if err := h.OnFetchData(ctx, req); err != nil {
		t.Fatalf("OnFetchData (throttle): %v", err)
	}
	elapsed := time.Since(start)

	// Upper bound: 1 emit per 100 ms window + 1 for the first (lastEmit starts at zero).
	maxEmits := 2 + int(elapsed.Milliseconds()/100)

	emitter.mu.Lock()
	count := emitter.count
	emitter.mu.Unlock()

	if count < 1 {
		t.Error("throttle: expected at least 1 sync:progress event")
	}
	if count > maxEmits {
		t.Errorf("throttle: emitted %d events in %v, max=%d (one per 100ms)", count, elapsed, maxEmits)
	}
}

func TestHydratorLocalToRemote(t *testing.T) {
	root := "/tmp/syncroot"
	provider := NewSyncProvider(root, "{test}", "T")
	h := NewHydrator(nil, nil, provider, nil, "b1")

	tests := []struct {
		local string
		want  string
	}{
		{root + "/docs/file.txt", "/docs/file.txt"},
		{root + "/", "/"},
		{root, "/"},
		{"/other/path/file.txt", "/other/path/file.txt"},
	}
	for _, tt := range tests {
		got := h.localToRemote(tt.local)
		if got != tt.want {
			t.Errorf("localToRemote(%q) = %q, want %q", tt.local, got, tt.want)
		}
	}
}

// ─── Regression tests — bug #133 ─────────────────────────────────────────────
//
// Two root causes fixed in 9bd66ec:
//  1. ghdOnFetchPlaceholders never sent CfExecute ack → OS timeout (~30s).
//  2. CF_CALLBACK_INFO.NormalizedPath (PCWSTR) cast as char* → only 1 byte read
//     per UTF-16 wchar → paths silently truncated to 1 character.
//
// On Linux the CGO path (provider.go windows) is not compiled; we test the Go
// layer — OnFetchPlaceholders error propagation and localToRemote path handling.

// errorListBackend returns a configured error from List().
type errorListBackend struct {
	mockBackend
	listErr error
}

func (b *errorListBackend) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	return nil, b.listErr
}

// blockingListBackend blocks in List() until its context is cancelled.
type blockingListBackend struct {
	mockBackend
}

func (b *blockingListBackend) List(ctx context.Context, _ string) ([]plugins.FileInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestRegression133_OnFetchPlaceholders_BackendError reproduces bug #133 root cause 1:
// when backend.List() fails, OnFetchPlaceholders must return a non-nil error (no panic,
// no silent discard).  Before the fix the ack was missing and the OS timed out after 30s.
func TestRegression133_OnFetchPlaceholders_BackendError(t *testing.T) {
	b := &errorListBackend{
		mockBackend: mockBackend{name: "t", connected: true},
		listErr:     errors.New("network unreachable"),
	}
	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(b, nil, provider, nil, "b1")

	err := h.OnFetchPlaceholders(context.Background(), provider.localPath)
	if err == nil {
		t.Fatal("OnFetchPlaceholders: expected non-nil error from backend List(), got nil")
	}
}

// TestRegression133_OnFetchPlaceholders_ContextCancelled verifies that
// OnFetchPlaceholders returns promptly when the context is already cancelled —
// no hang, no 30-second wait.  Regression for bug #133 (missing ack caused the
// OS to block the Explorer thread until its internal timeout fired).
func TestRegression133_OnFetchPlaceholders_ContextCancelled(t *testing.T) {
	parentCtx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel: List() will select ctx.Done() immediately

	b := &blockingListBackend{
		mockBackend: mockBackend{name: "t", connected: true},
	}
	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(b, nil, provider, nil, "b1")

	result := make(chan error, 1)
	go func() { result <- h.OnFetchPlaceholders(parentCtx, provider.localPath) }()

	select {
	case err := <-result:
		if err == nil {
			t.Error("OnFetchPlaceholders: expected non-nil error with cancelled context")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnFetchPlaceholders: did not return within 2s with cancelled context (regression #133 hang)")
	}
}

// TestRegression133_LocalToRemote_FullPathNotTruncated reproduces bug #133 root cause 2:
// CF_CALLBACK_INFO.NormalizedPath is PCWSTR (UTF-16LE); casting it as char* caused
// only 1 byte to be read per 2-byte code unit, silently producing paths like "/" or
// a single character.  localToRemote must return the full path for deep/unicode paths.
func TestRegression133_LocalToRemote_FullPathNotTruncated(t *testing.T) {
	root := "/tmp/GhostDrive/MFS"
	provider := NewSyncProvider(root, "{test}", "T")
	h := NewHydrator(nil, nil, provider, nil, "b1")

	cases := []struct {
		name  string
		local string
		want  string
	}{
		{
			"deep nested path",
			root + "/subfolder/subsubfolder/deep/file.txt",
			"/subfolder/subsubfolder/deep/file.txt",
		},
		{
			"path with spaces",
			root + "/My Documents/report Q1.xlsx",
			"/My Documents/report Q1.xlsx",
		},
		{
			"path with unicode accents",
			root + "/données/fichier-çàü.txt",
			"/données/fichier-çàü.txt",
		},
		{
			"path with hyphens and digits",
			root + "/backup-2026/img-001.png",
			"/backup-2026/img-001.png",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := h.localToRemote(tt.local)
			if got != tt.want {
				t.Errorf("localToRemote(%q) = %q, want %q (path truncated — regression #133)", tt.local, got, tt.want)
			}
			// Guard: the result must never be a single character (symptom of the PCWSTR bug).
			if len(got) <= 1 && got != "/" {
				t.Errorf("localToRemote returned suspiciously short path %q — PCWSTR truncation?", got)
			}
		})
	}
}

// TestRegression133_LocalToRemote_OutsideSyncRoot verifies that a path outside
// the sync root is returned as-is, not truncated or silently mangled.  Before the
// HasPrefix guard was added, a 1-char PCWSTR-corrupted path would have passed the
// TrimPrefix check and produced an incorrect relative path.
func TestRegression133_LocalToRemote_OutsideSyncRoot(t *testing.T) {
	root := "/tmp/GhostDrive/MFS"
	provider := NewSyncProvider(root, "{test}", "T")
	h := NewHydrator(nil, nil, provider, nil, "b1")

	cases := []struct {
		local string
	}{
		{"/tmp/other/file.txt"},
		{"/"},
		{"/GhostDrive/other-backend/file.txt"},
	}
	for _, tt := range cases {
		got := h.localToRemote(tt.local)
		// Outside sync root: must be returned unchanged (as-is guard added in #133).
		if got != tt.local {
			t.Errorf("localToRemote outside sync root: got %q, want %q (path mangled)", got, tt.local)
		}
	}
}
