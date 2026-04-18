package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/internal/config"
	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureEmitter records all emitted events for test assertions.
type captureEmitter struct {
	events []emittedEvent
}

type emittedEvent struct {
	Name string
	Data any
}

func (c *captureEmitter) Emit(event string, data any) {
	c.events = append(c.events, emittedEvent{Name: event, Data: data})
}

func (c *captureEmitter) hasEvent(name string) bool {
	for _, e := range c.events {
		if e.Name == name {
			return true
		}
	}
	return false
}

func newTestEngine(t *testing.T, backend plugins.StorageBackend, localDir string) (*Engine, *captureEmitter) {
	t.Helper()
	emitter := &captureEmitter{}
	cfg := config.DefaultConfig()
	engine := NewEngine(backend, localDir, "/remote", cfg, emitter)
	return engine, emitter
}

func TestEngineStartStop(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	engine, _ := newTestEngine(t, backend, tmp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, engine.Start(ctx))
	time.Sleep(100 * time.Millisecond)
	engine.Stop()
}

func TestEngineStartNotConnected(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	backend.connected = false

	engine, _ := newTestEngine(t, backend, tmp)
	err := engine.Start(context.Background())
	assert.Error(t, err)
}

func TestEngineStartAlreadyRunning(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	engine, _ := newTestEngine(t, backend, tmp)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, engine.Start(ctx))
	time.Sleep(50 * time.Millisecond)

	err := engine.Start(ctx)
	assert.Error(t, err, "starting an already-running engine should fail")
	engine.Stop()
}

func TestEnginePauseResume(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, _ := newTestEngine(t, backend, tmp)

	assert.False(t, engine.isPaused())
	engine.Pause()
	assert.True(t, engine.isPaused())
	assert.Equal(t, types.SyncPaused, engine.GetState().Status)

	engine.Resume()
	assert.False(t, engine.isPaused())
	assert.Equal(t, types.SyncIdle, engine.GetState().Status)
}

func TestEngineGetState(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, _ := newTestEngine(t, backend, tmp)

	state := engine.GetState()
	assert.Equal(t, types.SyncIdle, state.Status)
}

func TestEngineForceSync_UploadNewLocalFile(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// Create a local file that doesn't exist remotely
	localFile := filepath.Join(tmp, "upload_me.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("upload this"), 0644))

	engine, emitter := newTestEngine(t, backend, tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, engine.ForceSync(ctx))

	// File should have been uploaded to the backend
	_, ok := backend.files["/remote/upload_me.txt"]
	assert.True(t, ok, "file should be uploaded to backend")

	// State-changed event should have been emitted
	assert.True(t, emitter.hasEvent("sync:state-changed"))
}

func TestEngineForceSync_DownloadNewRemoteFile(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// Add a remote-only file (does not exist locally)
	backend.addRemoteFile("/remote/remote_only.txt", 42, time.Now())

	engine, _ := newTestEngine(t, backend, tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, engine.ForceSync(ctx))

	// File should have been downloaded locally
	_, err := os.Stat(filepath.Join(tmp, "remote_only.txt"))
	assert.NoError(t, err, "remote file should be downloaded locally")
}

func TestEngineForceSync_ConflictLocalWins(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	content := []byte("conflict content")
	localFile := filepath.Join(tmp, "conflict.txt")
	require.NoError(t, os.WriteFile(localFile, content, 0644))

	// Local is newer → upload should happen
	localModTime := time.Now()
	remoteModTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(localFile, localModTime, localModTime))
	backend.addRemoteFile("/remote/conflict.txt", int64(len(content)), remoteModTime)

	engine, _ := newTestEngine(t, backend, tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, engine.ForceSync(ctx))

	fi, ok := backend.files["/remote/conflict.txt"]
	require.True(t, ok)
	assert.Equal(t, int64(len(content)), fi.Size)
}

func TestEngineForceSync_ConflictRemoteWins(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	content := []byte("remote wins content")
	localFile := filepath.Join(tmp, "conflict_remote.txt")
	require.NoError(t, os.WriteFile(localFile, content, 0644))

	// Remote is newer → download should happen
	localModTime := time.Now().Add(-1 * time.Hour)
	remoteModTime := time.Now()
	require.NoError(t, os.Chtimes(localFile, localModTime, localModTime))
	backend.addRemoteFile("/remote/conflict_remote.txt", int64(len(content))+10, remoteModTime)

	engine, _ := newTestEngine(t, backend, tmp)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, engine.ForceSync(ctx))

	// The local file should have been replaced (downloaded)
	info, err := os.Stat(localFile)
	require.NoError(t, err)
	// Mock backend downloads zeros, size comes from backend's fi.Size
	assert.Equal(t, int64(len(content)+10), info.Size())
}

func TestEngineRecordError(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, emitter := newTestEngine(t, backend, tmp)

	engine.recordError("/file", "something failed")

	state := engine.GetState()
	require.Len(t, state.Errors, 1)
	assert.Equal(t, "/file", state.Errors[0].Path)
	assert.Equal(t, "something failed", state.Errors[0].Message)
	assert.Equal(t, types.SyncError, state.Status)
	assert.True(t, emitter.hasEvent("sync:error"))
}

func TestEngineRetryWithBackoff(t *testing.T) {
	// Test that SyncQueue correctly applies backoff on failure
	q := &SyncQueue{}
	task := &SyncTask{
		ID:         "retry-test",
		LocalPath:  "/local/file",
		RemotePath: "/remote/file",
		Direction:  DirectionUpload,
	}
	q.Enqueue(task)

	// First dequeue — should succeed (Retries=0, NextRetry is zero value)
	got, ok := q.Dequeue()
	require.True(t, ok)
	require.NotNil(t, got)

	// Simulate failure and requeue
	requeued := q.Requeue(got)
	assert.True(t, requeued)
	assert.Equal(t, 1, got.Retries)

	// Should not be immediately dequeue-able (next retry is in the future)
	notReady, ok := q.Dequeue()
	assert.False(t, ok)
	assert.Nil(t, notReady)
}
