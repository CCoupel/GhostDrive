package sync

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

// ─── #150 — remote→local propagation fixes ──────────────────────────────────

// TestEngine_HandleRemoteEvent_Delete_RemovesLocally verifies that a remote
// FileEventDeleted now propagates to the local filesystem via os.Remove (#150
// bug 2: "remote file deleted — manual review required (V1 policy)" was the
// previous no-op; v2.2 removes the file locally).
func TestEngine_HandleRemoteEvent_Delete_RemovesLocally(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, _ := newTestEngine(t, backend, tmp)

	// Create a local file (simulating a previously synced file).
	localFile := filepath.Join(tmp, "gone.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("synced"), 0644))
	engine.SetFileState(localFile, types.FileStateSynced)

	evt := plugins.FileEvent{
		Type:   plugins.FileEventDeleted,
		Path:   "gone.txt",
		Source: "remote",
	}

	err := engine.handleRemoteEvent(context.Background(), evt)
	require.NoError(t, err, "#150: handleRemoteEvent delete must not error")

	// Local file must be gone.
	_, statErr := os.Stat(localFile)
	assert.True(t, os.IsNotExist(statErr),
		"#150: local file must be removed after remote FileEventDeleted")

	// State must be cleared.
	assert.Equal(t, types.FileStateUnknown, engine.GetFileState(localFile),
		"#150: file state must be cleared after remote delete")
}

// TestEngine_HandleRemoteEvent_Delete_SuppressesLocalFeedback verifies that
// after handleRemoteEvent calls os.Remove, the resulting local watcher event
// is suppressed so handleLocalEvent does not dispatch a second ActionDelete to
// the already-gone remote file (#150 suppress mechanism).
func TestEngine_HandleRemoteEvent_Delete_SuppressesLocalFeedback(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, _ := newTestEngine(t, backend, tmp)

	localFile := filepath.Join(tmp, "suppress_me.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("data"), 0644))
	engine.SetFileState(localFile, types.FileStateSynced)

	// Seed a remote file so ActionDelete would have something to delete.
	backend.addRemoteFile("/remote/suppress_me.txt", 4, time.Now())

	// Simulate remote delete.
	err := engine.handleRemoteEvent(context.Background(), plugins.FileEvent{
		Type:   plugins.FileEventDeleted,
		Path:   "suppress_me.txt",
		Source: "remote",
	})
	require.NoError(t, err)

	// The path must be suppressed immediately after the remote event.
	assert.True(t, engine.isLocalEventSuppressed(localFile),
		"#150: local path must be suppressed after handleRemoteEvent delete")

	// Simulate the local watcher feedback: Delete event for the same path.
	// With suppress active, handleLocalEvent must skip this without calling backend.Delete.
	deleteCalled := backend.deleteCount
	err = engine.handleLocalEvent(context.Background(), plugins.FileEvent{
		Type:   plugins.FileEventDeleted,
		Path:   "suppress_me.txt",
		Source: "local",
	})
	require.NoError(t, err, "#150: suppressed handleLocalEvent must not error")
	assert.Equal(t, deleteCalled, backend.deleteCount,
		"#150: backend.Delete must NOT be called for suppressed local feedback event")
}

// TestEngine_HandleRemoteEvent_Rename_SuppressesBothPaths verifies that after
// a remote FileEventRenamed, both the old and new local paths are suppressed so
// the resulting fsnotify pair (Rename(old)+Create(new)) does not trigger a
// redundant ActionRename back to the backend (#150).
func TestEngine_HandleRemoteEvent_Rename_SuppressesBothPaths(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, _ := newTestEngine(t, backend, tmp)

	// Create old local file (already synced).
	oldLocal := filepath.Join(tmp, "alpha.txt")
	require.NoError(t, os.WriteFile(oldLocal, []byte("content"), 0644))
	engine.SetFileState(oldLocal, types.FileStateSynced)

	// Seed remote new name.
	backend.addRemoteFile("/remote/beta.txt", 7, time.Now())

	err := engine.handleRemoteEvent(context.Background(), plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    "beta.txt",
		OldPath: "alpha.txt",
		Source:  "remote",
	})
	require.NoError(t, err, "#150: handleRemoteEvent rename must not error")

	newLocal := filepath.Join(tmp, "beta.txt")

	// Old path must be suppressed.
	assert.True(t, engine.isLocalEventSuppressed(oldLocal),
		"#150: old path must be suppressed after remote rename")
	// New path must be suppressed.
	assert.True(t, engine.isLocalEventSuppressed(newLocal),
		"#150: new path must be suppressed after remote rename")

	// Simulate watcher feedback: FileEventRenamed{beta, alpha}.
	// With suppress active, no ActionRename must be sent to the backend.
	renameBefore := backend.renameCount
	err = engine.handleLocalEvent(context.Background(), plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    "beta.txt",
		OldPath: "alpha.txt",
		Source:  "local",
	})
	require.NoError(t, err, "#150: suppressed handleLocalEvent rename must not error")
	assert.Equal(t, renameBefore, backend.renameCount,
		"#150: backend.Rename must NOT be called for suppressed local feedback event")
}

// TestEngine_HandleRemoteEvent_Delete_NonExistentLocal verifies that deleting a
// remote file that was never synced locally (os.IsNotExist) is a no-op error-wise
// — the engine must not return an error for an already-absent local file (#150).
func TestEngine_HandleRemoteEvent_Delete_NonExistentLocal(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, _ := newTestEngine(t, backend, tmp)
	// No local file created — simulate file that never synced locally.

	err := engine.handleRemoteEvent(context.Background(), plugins.FileEvent{
		Type:   plugins.FileEventDeleted,
		Path:   "never_synced.txt",
		Source: "remote",
	})
	require.NoError(t, err,
		"#150: deleting a never-synced local file must not error (os.IsNotExist is OK)")
}

// TestEngine_SuppressExpiry verifies that the suppress TTL expires and the path
// is no longer considered suppressed after the window closes.
func TestEngine_SuppressExpiry(t *testing.T) {
	tmp := t.TempDir()
	engine, _ := newTestEngine(t, newMockBackend(), tmp)

	// Artificially store an already-expired entry.
	expiredPath := filepath.Join(tmp, "expired.txt")
	engine.suppressedPaths.Store(expiredPath, time.Now().Add(-1*time.Second))

	// Must return false — expiry is in the past.
	assert.False(t, engine.isLocalEventSuppressed(expiredPath),
		"expired suppress entry must return false")

	// Entry must be cleaned up lazily.
	_, still := engine.suppressedPaths.Load(expiredPath)
	assert.False(t, still, "expired entry must be deleted on check")
}

// ─── #153 — Windows backslash path normalisation in handleLocalEvent ─────────

// TestEngine_HandleLocalEvent_BackslashPath_Delete verifies that fsnotify paths
// with Windows-style backslashes (e.g. "subdir\file.txt") are correctly converted
// to forward slashes before path.Join builds the remote path (#153).
// Without the fix, the remote path is "subdir\file.txt" (treated as one filename
// component) instead of "subdir/file.txt", causing backend.Delete to target the
// wrong path → 404 → remote file preserved → file revives on next reconciliation.
// This test is Windows-only: on Linux filepath.ToSlash does not convert backslashes
// (they are valid filename characters), so fsnotify never produces such paths.
func TestEngine_HandleLocalEvent_BackslashPath_Delete(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("#153: backslash path normalisation is Windows-only (fsnotify on Linux uses forward slashes)")
	}
	tmp := t.TempDir()
	backend := newMockBackend()

	// Seed the remote file at the CORRECT forward-slash path.
	backend.addRemoteFile("/remote/subdir/file.txt", 10, time.Now())

	engine, _ := newTestEngine(t, backend, tmp)

	// Create the local subdirectory and file (already in synced state).
	subdir := filepath.Join(tmp, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0755))
	localFile := filepath.Join(subdir, "file.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("content"), 0644))
	engine.SetFileState(localFile, types.FileStateSynced)

	// Simulate a fsnotify Delete event with a backslash-separated path
	// (as produced by fsnotify on Windows for files in subdirectories).
	evt := plugins.FileEvent{
		Type:   plugins.FileEventDeleted,
		Path:   `subdir\file.txt`, // backslash — Windows fsnotify style
		Source: "local",
	}

	// Remove the local file first (already deleted by user/OS).
	require.NoError(t, os.Remove(localFile))

	err := engine.handleLocalEvent(context.Background(), evt)
	require.NoError(t, err, "#153: handleLocalEvent delete must not error")

	// Backend must have removed the remote file at the CORRECT path.
	_, stillExists := backend.files["/remote/subdir/file.txt"]
	assert.False(t, stillExists,
		"#153: remote file at forward-slash path must be deleted (backslash normalisation)")

	// The wrongly-joined path must NOT have been used.
	_, wrongPath := backend.files[`/remote/subdir\file.txt`]
	assert.False(t, wrongPath,
		"#153: backend must not have been called with backslash path")
}

// TestEngine_HandleLocalEvent_BackslashPath_Rename verifies the same fix for
// the ActionRename case: OldPath backslash-separated → correct remote SrcRemotePath.
// Windows-only for the same reason as TestEngine_HandleLocalEvent_BackslashPath_Delete.
func TestEngine_HandleLocalEvent_BackslashPath_Rename(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("#153: backslash path normalisation is Windows-only")
	}
	tmp := t.TempDir()
	backend := newMockBackend()

	// Seed remote old path.
	backend.addRemoteFile("/remote/subdir/old.txt", 8, time.Now())

	engine, _ := newTestEngine(t, backend, tmp)

	subdir := filepath.Join(tmp, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0755))
	newLocal := filepath.Join(subdir, "new.txt")
	require.NoError(t, os.WriteFile(newLocal, []byte("data"), 0644))

	evt := plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    `subdir\new.txt`, // Windows fsnotify style
		OldPath: `subdir\old.txt`,
		Source:  "local",
	}

	err := engine.handleLocalEvent(context.Background(), evt)
	require.NoError(t, err, "#153: handleLocalEvent rename must not error")

	// After rename: old remote path gone, new path present.
	_, oldExists := backend.files["/remote/subdir/old.txt"]
	assert.False(t, oldExists, "#153: old remote path must be gone after rename")
	_, newExists := backend.files["/remote/subdir/new.txt"]
	assert.True(t, newExists, "#153: new remote path must exist after rename")
}

// ─── #155 — Leading-slash normalisation in handleRemoteEvent ─────────────────

// TestEngine_HandleRemoteEvent_LeadingSlash_Delete verifies that remote Watch()
// events with a leading "/" in evt.Path are handled correctly (#155).
// Without the fix: filepath.Join("C:\SyncRoot", "/file.txt") = "C:\file.txt" on
// Windows, so os.Remove targets the wrong path and the local file is never removed.
func TestEngine_HandleRemoteEvent_LeadingSlash_Delete(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, _ := newTestEngine(t, backend, tmp)

	// Create a local file that "exists" (will be deleted by the remote event).
	localFile := filepath.Join(tmp, "file.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("synced"), 0644))
	engine.SetFileState(localFile, types.FileStateSynced)

	// Remote event with leading slash — as emitted by WebDAV Watch() implementations.
	evt := plugins.FileEvent{
		Type:   plugins.FileEventDeleted,
		Path:   "/file.txt",
		Source: "remote",
	}

	err := engine.handleRemoteEvent(context.Background(), evt)
	require.NoError(t, err, "#155: handleRemoteEvent delete with leading slash must not error")

	// The LOCAL file must be gone (os.Remove was called on the correct path).
	_, statErr := os.Stat(localFile)
	assert.True(t, os.IsNotExist(statErr),
		"#155: local file must be removed when remote Watch() sends leading-slash path")
}

// TestEngine_HandleRemoteEvent_LeadingSlash_Rename verifies leading-slash
// normalisation for the Rename case in handleRemoteEvent (#155).
func TestEngine_HandleRemoteEvent_LeadingSlash_Rename(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	engine, _ := newTestEngine(t, backend, tmp)

	oldLocal := filepath.Join(tmp, "old.txt")
	require.NoError(t, os.WriteFile(oldLocal, []byte("content"), 0644))

	// Remote rename event with leading slashes (as emitted by some backend plugins).
	evt := plugins.FileEvent{
		Type:    plugins.FileEventRenamed,
		Path:    "/new.txt",
		OldPath: "/old.txt",
		Source:  "remote",
	}

	err := engine.handleRemoteEvent(context.Background(), evt)
	require.NoError(t, err, "#155: handleRemoteEvent rename with leading slash must not error")

	// Old local file must be gone (renamed, not left at original path).
	_, statErr := os.Stat(oldLocal)
	assert.True(t, os.IsNotExist(statErr),
		"#155: old local file must be gone after rename with leading-slash event")

	// New local file must exist.
	newLocal := filepath.Join(tmp, "new.txt")
	_, statErr = os.Stat(newLocal)
	assert.NoError(t, statErr,
		"#155: new local file must exist after rename with leading-slash event")
}
