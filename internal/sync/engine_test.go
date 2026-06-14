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
	engine := NewEngine("test-backend", backend, localDir, "/remote", cfg, emitter)
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

func TestEngineRecordOffline(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, emitter := newTestEngine(t, backend, tmp)

	engine.recordOffline()

	state := engine.GetState()
	assert.Equal(t, types.SyncOffline, state.Status, "recordOffline must set SyncOffline state")
	assert.Len(t, state.Errors, 0, "recordOffline must not add to the error list")
	assert.True(t, emitter.hasEvent("sync:offline"), "recordOffline must emit sync:offline")
}

func TestEngineRecordOnline_ClearsOfflineState(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, emitter := newTestEngine(t, backend, tmp)

	// Drive the engine into offline state, then recover.
	engine.recordOffline()
	require.Equal(t, types.SyncOffline, engine.GetState().Status)

	engine.recordOnline()

	state := engine.GetState()
	assert.Equal(t, types.SyncIdle, state.Status, "recordOnline must clear SyncOffline → SyncIdle")
	assert.True(t, emitter.hasEvent("sync:online"), "recordOnline must emit sync:online")
}

func TestEngineRecordOnline_DoesNotOverrideError(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, emitter := newTestEngine(t, backend, tmp)

	// Drive the engine into error state (persistent failure).
	engine.recordError("/file", "server down")
	require.Equal(t, types.SyncError, engine.GetState().Status)

	// A spurious online sentinel must not clear an error state.
	engine.recordOnline()

	state := engine.GetState()
	assert.Equal(t, types.SyncError, state.Status, "recordOnline must not override SyncError")
	assert.True(t, emitter.hasEvent("sync:online"), "recordOnline must still emit sync:online even when state unchanged")
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

// ─── #148 — ActionRename bug fixes ───────────────────────────────────────────

// TestEngine_HandleLocalEvent_Rename_PropagatesRemote verifies that a
// FileEventRenamed emitted by the watcher (with relative paths after the #148
// watcher fix) correctly triggers an ActionRename on the backend with the right
// remote paths.
func TestEngine_HandleLocalEvent_Rename_PropagatesRemote(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// Seed old file on remote; new local file already exists (rename happened locally).
	backend.addRemoteFile("/remote/old.txt", 5, time.Now())
	localNew := filepath.Join(tmp, "new.txt")
	require.NoError(t, os.WriteFile(localNew, []byte("hello"), 0644))

	engine, _ := newTestEngine(t, backend, tmp)

	// Simulate watcher event with relative paths (as emitted after #148 watcher fix).
	evt := plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    "new.txt",  // relative to localDir
		OldPath: "old.txt",  // relative to localDir
		Source:  "local",
	}

	err := engine.handleLocalEvent(context.Background(), evt)
	require.NoError(t, err, "#148: handleLocalEvent rename must not error")

	// Backend: old path gone, new path present.
	_, oldExists := backend.files["/remote/old.txt"]
	_, newExists := backend.files["/remote/new.txt"]
	assert.False(t, oldExists, "#148: remote old.txt must be removed after rename")
	assert.True(t, newExists, "#148: remote new.txt must exist after rename")
}

// TestEngine_HandleRemoteEvent_Rename_RenamesLocally verifies that when the
// remote backend fires a FileEventRenamed the engine renames the local file
// atomically instead of downloading a new copy (#148 fix).
func TestEngine_HandleRemoteEvent_Rename_RenamesLocally(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, _ := newTestEngine(t, backend, tmp)

	// Create old local file (already synced).
	oldLocal := filepath.Join(tmp, "old.txt")
	require.NoError(t, os.WriteFile(oldLocal, []byte("synced content"), 0644))

	evt := plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    "new.txt",  // relative to remote root / localDir
		OldPath: "old.txt",
		Source:  "remote",
	}

	err := engine.handleRemoteEvent(context.Background(), evt)
	require.NoError(t, err, "#148: handleRemoteEvent rename must not error")

	// Old file must be gone locally.
	_, statErr := os.Stat(oldLocal)
	assert.True(t, os.IsNotExist(statErr),
		"#148: old local file must be removed after remote rename")

	// New file must exist (renamed in place, not downloaded).
	newLocal := filepath.Join(tmp, "new.txt")
	_, statErr = os.Stat(newLocal)
	assert.NoError(t, statErr, "#148: new local file must exist after remote rename")
}

// TestEngine_HandleLocalEvent_Rename_DoesNotInheritLocalState verifies that
// renaming a file whose previous state was FileStateLocal does NOT propagate that
// Local state to the new path (#149 fix 1).  After the rename the new path must
// have state Unknown (or whatever the dispatcher sets), so that a subsequent
// FileEventDeleted correctly dispatches ActionDelete instead of being silently
// skipped by the #141 guard.
func TestEngine_HandleLocalEvent_Rename_DoesNotInheritLocalState(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	// Seed remote old.txt so the rename backend call does not fail.
	backend.addRemoteFile("/remote/old.txt", 5, time.Now())
	engine, _ := newTestEngine(t, backend, tmp)

	// Mark old.txt as FileStateLocal (never synced to remote).
	oldLocal := filepath.Join(tmp, "old.txt")
	engine.SetFileState(oldLocal, types.FileStateLocal)

	// Create new.txt locally (rename target).
	newLocal := filepath.Join(tmp, "new.txt")
	require.NoError(t, os.WriteFile(newLocal, []byte("content"), 0644))

	// Simulate watcher's FileEventRenamed (relative paths per #148 fix).
	evt := plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    "new.txt",
		OldPath: "old.txt",
		Source:  "local",
	}
	err := engine.handleLocalEvent(context.Background(), evt)
	require.NoError(t, err, "#149: rename of Local-state file must not error")

	// Old path state must be cleared.
	assert.Equal(t, types.FileStateUnknown, engine.GetFileState(oldLocal),
		"#149: old path state must be cleared after rename")

	// New path must NOT have Local state — it would block ActionDelete (#141 guard).
	newState := engine.GetFileState(newLocal)
	assert.NotEqual(t, types.FileStateLocal, newState,
		"#149: rename must not propagate Local state to new path; got %q", newState)
}

// TestEngine_HandleRemoteEvent_Rename_FallbackDownload verifies that when the
// old local file is absent (not yet synced) the engine falls back to downloading
// the new remote file (#148 fix).
func TestEngine_HandleRemoteEvent_Rename_FallbackDownload(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	// Remote has the new file (already renamed on backend side).
	backend.addRemoteFile("/remote/new.txt", 5, time.Now())
	engine, _ := newTestEngine(t, backend, tmp)

	evt := plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    "new.txt",
		OldPath: "old.txt", // not present locally
		Source:  "remote",
	}

	err := engine.handleRemoteEvent(context.Background(), evt)
	require.NoError(t, err, "#148: fallback download must not error")

	// new.txt should be downloaded locally.
	newLocal := filepath.Join(tmp, "new.txt")
	_, statErr := os.Stat(newLocal)
	assert.NoError(t, statErr, "#148: fallback download must create local new.txt")
}
