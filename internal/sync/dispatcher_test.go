package sync

// Tests for Dispatcher.execute — ActionRename, ActionCopy, and StateUpdater (#136 #139 #140).
//
// Coverage:
//   - ActionRename native (backend supports Rename)
//   - ActionRename fallback to upload+delete when backend returns ErrNotSupported
//   - ActionCopy server-side (backend supports Copy)
//   - ActionCopy fallback to download+upload when backend returns ErrNotSupported
//   - StateUpdater called at the right points during ActionUpload (P→U→S and P→U→E)
//   - StateUpdater nil guard — no panic when not injected

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── stateRecorder ────────────────────────────────────────────────────────────

// stateRecorder captures every (path, state) pair passed to a stateUpdater.
type stateRecorder struct {
	calls []stateCall
}

type stateCall struct {
	Path  string
	State types.FileState
}

func (r *stateRecorder) record(path string, state types.FileState) {
	r.calls = append(r.calls, stateCall{Path: path, State: state})
}

func (r *stateRecorder) statesFor(path string) []types.FileState {
	var out []types.FileState
	for _, c := range r.calls {
		if c.Path == path {
			out = append(out, c.State)
		}
	}
	return out
}

// ─── errBackend wraps mockBackend to inject errors for specific operations ────

type errBackend struct {
	*mockBackend
	renameErr error
	copyErr   error
	uploadErr error
}

func (e *errBackend) Rename(_ context.Context, _, _ string) error  { return e.renameErr }
func (e *errBackend) Copy(_ context.Context, _, _ string) error    { return e.copyErr }
func (e *errBackend) Upload(_ context.Context, local, remote string, cb plugins.ProgressCallback) error {
	if e.uploadErr != nil {
		return e.uploadErr
	}
	return e.mockBackend.Upload(context.Background(), local, remote, cb)
}

// ─── ActionRename ─────────────────────────────────────────────────────────────

// TestDispatcher_ActionRename_Native verifies that when the backend supports
// Rename the dispatcher calls Rename and transitions the file to Synced.
func TestDispatcher_ActionRename_Native(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// Seed old remote path.
	backend.addRemoteFile("/remote/old.txt", 5, time.Now())

	// Create the destination local file (the dispatcher uploads it on fallback; not needed here).
	localDst := filepath.Join(tmp, "new.txt")
	require.NoError(t, os.WriteFile(localDst, []byte("hello"), 0644))

	rec := &stateRecorder{}
	d := NewDispatcher(backend, &NoopEmitter{}, tmp)
	d.SetStateUpdater(rec.record)

	action := SyncAction{
		Type:          ActionRename,
		LocalPath:     localDst,
		RemotePath:    "/remote/new.txt",
		SrcLocalPath:  filepath.Join(tmp, "old.txt"),
		SrcRemotePath: "/remote/old.txt",
	}

	err := d.execute(context.Background(), action)
	require.NoError(t, err)

	// Old path gone, new path exists in backend.
	_, oldExists := backend.files["/remote/old.txt"]
	_, newExists := backend.files["/remote/new.txt"]
	assert.False(t, oldExists, "old remote path must be deleted after rename")
	assert.True(t, newExists, "new remote path must exist after rename")

	// State transitions: U (before rename) → S (after success).
	states := rec.statesFor(localDst)
	assert.Contains(t, states, types.FileStateUploading, "must transition to Uploading before rename")
	assert.Contains(t, states, types.FileStateSynced, "must transition to Synced after rename")
}

// TestDispatcher_ActionRename_FallbackOnErrNotSupported verifies that when
// backend.Rename returns ErrNotSupported the dispatcher falls back to
// Upload(new) + Delete(old) and still marks the file Synced.
func TestDispatcher_ActionRename_FallbackOnErrNotSupported(t *testing.T) {
	tmp := t.TempDir()
	inner := newMockBackend()

	// Seed old remote path.
	inner.addRemoteFile("/remote/old.txt", 5, time.Now())

	// Create local destination file (needed for Upload fallback).
	localDst := filepath.Join(tmp, "new.txt")
	require.NoError(t, os.WriteFile(localDst, []byte("fallback content"), 0644))

	eb := &errBackend{
		mockBackend: inner,
		renameErr:   plugins.ErrNotSupported,
	}

	rec := &stateRecorder{}
	d := NewDispatcher(eb, &NoopEmitter{}, tmp)
	d.SetStateUpdater(rec.record)

	action := SyncAction{
		Type:          ActionRename,
		LocalPath:     localDst,
		RemotePath:    "/remote/new.txt",
		SrcLocalPath:  filepath.Join(tmp, "old.txt"),
		SrcRemotePath: "/remote/old.txt",
	}

	err := d.execute(context.Background(), action)
	require.NoError(t, err, "ErrNotSupported fallback must not return an error")

	// Fallback: new file uploaded, old file deleted.
	_, newExists := inner.files["/remote/new.txt"]
	_, oldExists := inner.files["/remote/old.txt"]
	assert.True(t, newExists, "fallback: new remote path must be uploaded")
	assert.False(t, oldExists, "fallback: old remote path must be deleted")

	// Final state must be Synced.
	states := rec.statesFor(localDst)
	assert.Contains(t, states, types.FileStateSynced, "must be Synced after fallback rename")
}

// TestDispatcher_ActionRename_Error verifies that a Rename failure (not ErrNotSupported)
// marks the file as Error and returns the error.
func TestDispatcher_ActionRename_Error(t *testing.T) {
	tmp := t.TempDir()
	inner := newMockBackend()
	inner.addRemoteFile("/remote/old.txt", 5, time.Now())

	localDst := filepath.Join(tmp, "new.txt")
	require.NoError(t, os.WriteFile(localDst, []byte("x"), 0644))

	customErr := errors.New("network timeout")
	eb := &errBackend{
		mockBackend: inner,
		renameErr:   customErr,
	}

	rec := &stateRecorder{}
	d := NewDispatcher(eb, &NoopEmitter{}, tmp)
	d.SetStateUpdater(rec.record)

	err := d.execute(context.Background(), SyncAction{
		Type:          ActionRename,
		LocalPath:     localDst,
		RemotePath:    "/remote/new.txt",
		SrcLocalPath:  filepath.Join(tmp, "old.txt"),
		SrcRemotePath: "/remote/old.txt",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename")

	states := rec.statesFor(localDst)
	assert.Contains(t, states, types.FileStateError, "must transition to Error on rename failure")
}

// ─── ActionCopy ──────────────────────────────────────────────────────────────

// TestDispatcher_ActionCopy_ServerSide verifies server-side copy when the
// backend supports it (no ErrNotSupported).
func TestDispatcher_ActionCopy_ServerSide(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// Seed source remote file.
	backend.addRemoteFile("/remote/src.txt", 10, time.Now())

	// Local destination file (needed only if fallback triggers — it must not here).
	localDst := filepath.Join(tmp, "dst.txt")
	require.NoError(t, os.WriteFile(localDst, []byte(""), 0644))

	rec := &stateRecorder{}
	d := NewDispatcher(backend, &NoopEmitter{}, tmp)
	d.SetStateUpdater(rec.record)

	err := d.execute(context.Background(), SyncAction{
		Type:          ActionCopy,
		LocalPath:     localDst,
		RemotePath:    "/remote/dst.txt",
		SrcLocalPath:  filepath.Join(tmp, "src.txt"),
		SrcRemotePath: "/remote/src.txt",
	})
	require.NoError(t, err)

	// Destination must exist on backend.
	_, ok := backend.files["/remote/dst.txt"]
	assert.True(t, ok, "destination must exist after server-side copy")

	// Source must still exist.
	_, ok = backend.files["/remote/src.txt"]
	assert.True(t, ok, "source must still exist after copy")

	// Final state: Synced.
	states := rec.statesFor(localDst)
	assert.Contains(t, states, types.FileStateSynced, "must be Synced after copy")
}

// TestDispatcher_ActionCopy_FallbackOnErrNotSupported verifies that when
// backend.Copy returns ErrNotSupported the dispatcher falls back to
// Download(src) + Upload(dst).
func TestDispatcher_ActionCopy_FallbackOnErrNotSupported(t *testing.T) {
	tmp := t.TempDir()
	inner := newMockBackend()

	// Seed source remote file.
	inner.addRemoteFile("/remote/src.txt", 3, time.Now())

	localDst := filepath.Join(tmp, "dst.txt")
	require.NoError(t, os.WriteFile(localDst, []byte("xyz"), 0644))

	eb := &errBackend{
		mockBackend: inner,
		copyErr:     plugins.ErrNotSupported,
	}

	rec := &stateRecorder{}
	d := NewDispatcher(eb, &NoopEmitter{}, tmp)
	d.SetStateUpdater(rec.record)

	err := d.execute(context.Background(), SyncAction{
		Type:          ActionCopy,
		LocalPath:     localDst,
		RemotePath:    "/remote/dst.txt",
		SrcLocalPath:  filepath.Join(tmp, "src.txt"),
		SrcRemotePath: "/remote/src.txt",
	})
	require.NoError(t, err, "ErrNotSupported fallback must succeed")

	// Destination uploaded.
	_, ok := inner.files["/remote/dst.txt"]
	assert.True(t, ok, "fallback copy: destination must be uploaded")

	// Final state: Synced.
	states := rec.statesFor(localDst)
	assert.Contains(t, states, types.FileStateSynced)
}

// ─── StateUpdater ────────────────────────────────────────────────────────────

// TestDispatcher_StateUpdater_Called_OnUpload verifies that ActionUpload
// triggers P→U→S state transitions via the StateUpdater.
func TestDispatcher_StateUpdater_Called_OnUpload(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	localFile := filepath.Join(tmp, "upload.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("content"), 0644))

	rec := &stateRecorder{}
	d := NewDispatcher(backend, &NoopEmitter{}, tmp)
	d.SetStateUpdater(rec.record)

	err := d.execute(context.Background(), SyncAction{
		Type:       ActionUpload,
		LocalPath:  localFile,
		RemotePath: "/remote/upload.txt",
	})
	require.NoError(t, err)

	states := rec.statesFor(localFile)
	require.GreaterOrEqual(t, len(states), 2, "at least P and U/S must be recorded")
	assert.Equal(t, types.FileStatePending, states[0], "first transition must be Pending")
	assert.Equal(t, types.FileStateUploading, states[1], "second transition must be Uploading")
	assert.Equal(t, types.FileStateSynced, states[len(states)-1], "last transition must be Synced")
}

// TestDispatcher_StateUpdater_Called_OnUploadError verifies that a failed
// upload transitions to Error.
func TestDispatcher_StateUpdater_Called_OnUploadError(t *testing.T) {
	tmp := t.TempDir()
	inner := newMockBackend()

	// No local file → Upload will fail with os.ErrNotExist.
	localFile := filepath.Join(tmp, "missing.txt")

	rec := &stateRecorder{}
	d := NewDispatcher(inner, &NoopEmitter{}, tmp)
	d.SetStateUpdater(rec.record)

	err := d.execute(context.Background(), SyncAction{
		Type:       ActionUpload,
		LocalPath:  localFile,
		RemotePath: "/remote/missing.txt",
	})
	require.Error(t, err, "upload of missing file must fail")

	states := rec.statesFor(localFile)
	assert.Contains(t, states, types.FileStateError, "failed upload must set Error state")
}

// TestDispatcher_StateUpdater_Nil_NoPanic verifies that Dispatch does not
// panic when no stateUpdater is set (nil guard).
func TestDispatcher_StateUpdater_Nil_NoPanic(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	localFile := filepath.Join(tmp, "upload.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("ok"), 0644))

	// No SetStateUpdater call — stateUpdater is nil.
	d := NewDispatcher(backend, &NoopEmitter{}, tmp)

	require.NotPanics(t, func() {
		_ = d.execute(context.Background(), SyncAction{
			Type:       ActionUpload,
			LocalPath:  localFile,
			RemotePath: "/remote/upload.txt",
		})
	})
}

// TestDispatcher_UnknownActionType_ReturnsError verifies that an unknown
// action type returns a descriptive error and does not panic.
func TestDispatcher_UnknownActionType_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	d := NewDispatcher(newMockBackend(), &NoopEmitter{}, tmp)

	err := d.execute(context.Background(), SyncAction{
		Type:       ActionType("unknown-action"),
		LocalPath:  "/some/path",
		RemotePath: "/remote/path",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action type")
}

// ─── PlaceholderMaker (#151) ─────────────────────────────────────────────────

// mockPlaceholderCFManager implements CFStateManager + PlaceholderMaker for
// testing the ConvertToPlaceholder → SetSyncState ordering fix (#151).
type mockPlaceholderCFManager struct {
	convertCalls []string // paths passed to ConvertToPlaceholder
	syncCalls    []string // paths passed to SetSyncState
}

func (m *mockPlaceholderCFManager) SetSyncState(_, localPath string, _ int) error {
	m.syncCalls = append(m.syncCalls, localPath)
	return nil
}

func (m *mockPlaceholderCFManager) ConvertToPlaceholder(_, localPath string) error {
	m.convertCalls = append(m.convertCalls, localPath)
	return nil
}

// TestDispatcher_ActionDownload_ConvertBeforeSetSync verifies that ActionDownload
// calls ConvertToPlaceholder BEFORE SetSyncState when the cfManager satisfies
// PlaceholderMaker (#151 regression guard).
//
// Root cause: Download() creates a regular NTFS file (not a CF placeholder).
// Calling CfSetInSyncState on a non-placeholder writes invalid CF reparse-point
// data, corrupting the parent directory's CF state and causing 0x80070781 on
// subsequent user operations.
func TestDispatcher_ActionDownload_ConvertBeforeSetSync(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()

	// Seed a remote file so Download() succeeds.
	backend.addRemoteFile("/remote/file.txt", 5, time.Now())

	localFile := filepath.Join(tmp, "file.txt")
	mgr := &mockPlaceholderCFManager{}

	d := NewDispatcher(backend, &NoopEmitter{}, tmp)
	d.SetCFManager("backend-1", mgr)

	err := d.execute(context.Background(), SyncAction{
		Type:       ActionDownload,
		LocalPath:  localFile,
		RemotePath: "/remote/file.txt",
	})
	require.NoError(t, err)

	// ConvertToPlaceholder must be called once for the downloaded file.
	require.Len(t, mgr.convertCalls, 1, "ConvertToPlaceholder must be called once")
	assert.Equal(t, localFile, mgr.convertCalls[0])

	// SetSyncState must be called after ConvertToPlaceholder.
	require.Len(t, mgr.syncCalls, 1, "SetSyncState must be called once")
	assert.Equal(t, localFile, mgr.syncCalls[0])
}

// TestDispatcher_ActionCopy_ConvertBeforeSetSync verifies that ActionCopy also
// calls ConvertToPlaceholder before SetSyncState (#151).
func TestDispatcher_ActionCopy_ConvertBeforeSetSync(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	backend.addRemoteFile("/remote/src.txt", 3, time.Now())

	localDst := filepath.Join(tmp, "dst.txt")
	require.NoError(t, os.WriteFile(localDst, []byte("xyz"), 0644))

	mgr := &mockPlaceholderCFManager{}

	d := NewDispatcher(backend, &NoopEmitter{}, tmp)
	d.SetCFManager("backend-1", mgr)

	err := d.execute(context.Background(), SyncAction{
		Type:          ActionCopy,
		LocalPath:     localDst,
		RemotePath:    "/remote/dst.txt",
		SrcLocalPath:  filepath.Join(tmp, "src.txt"),
		SrcRemotePath: "/remote/src.txt",
	})
	require.NoError(t, err)

	// ConvertToPlaceholder must be called.
	require.Len(t, mgr.convertCalls, 1, "ConvertToPlaceholder must be called for copy")
	assert.Equal(t, localDst, mgr.convertCalls[0])

	// SetSyncState must follow.
	require.Len(t, mgr.syncCalls, 1)
	assert.Equal(t, localDst, mgr.syncCalls[0])
}

// plainCFManager implements only CFStateManager, NOT PlaceholderMaker.
// Used to verify the type-assertion guard in ActionDownload/ActionCopy (#151).
type plainCFManager struct {
	syncCalls []string
}

func (m *plainCFManager) SetSyncState(_, localPath string, _ int) error {
	m.syncCalls = append(m.syncCalls, localPath)
	return nil
}

// TestDispatcher_ActionDownload_NoPlaceholderMaker_NoPanic verifies that if the
// cfManager does NOT implement PlaceholderMaker the dispatcher does not panic
// and still calls SetSyncState (#151 guard — existing CFStateManager impls
// without PlaceholderMaker remain compatible).
func TestDispatcher_ActionDownload_NoPlaceholderMaker_NoPanic(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	backend.addRemoteFile("/remote/file.txt", 2, time.Now())

	localFile := filepath.Join(tmp, "file.txt")
	mgr := &plainCFManager{}

	d := NewDispatcher(backend, &NoopEmitter{}, tmp)
	d.SetCFManager("backend-1", mgr)

	require.NotPanics(t, func() {
		err := d.execute(context.Background(), SyncAction{
			Type:       ActionDownload,
			LocalPath:  localFile,
			RemotePath: "/remote/file.txt",
		})
		require.NoError(t, err)
	}, "must not panic when cfManager does not implement PlaceholderMaker")

	// SetSyncState must still be called even without ConvertToPlaceholder.
	require.Len(t, mgr.syncCalls, 1, "SetSyncState must still be called")
	assert.Equal(t, localFile, mgr.syncCalls[0])
}
