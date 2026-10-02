package moosefs

// Tests for Backend.Copy() ErrNotSupported (v2.2 #140).
//
// MooseFS does not support atomic server-side copy — Copy() must return
// plugins.ErrNotSupported so the Dispatcher can fall back to Download+Upload.
//
// Rename() is also tested here to verify it delegates to the native MooseFS
// Move operation (already tested in moosefs_test.go via TestMove_fileRename;
// we add a thin Rename alias test for completeness).

import (
	"context"
	"errors"
	"testing"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Copy → ErrNotSupported ──────────────────────────────────────────────────

// TestMooseFS_Copy_ReturnsErrNotSupported verifies that Copy() returns
// plugins.ErrNotSupported regardless of the backend state (#140).
// The Dispatcher must detect this and fall back to Download+Upload.
func TestMooseFS_Copy_ReturnsErrNotSupported(t *testing.T) {
	addr := startFakeServer(t)
	b := newTestBackend(t, addr)

	err := b.Copy(context.Background(), "/any/src.txt", "/any/dst.txt")

	require.Error(t, err)
	assert.True(t, errors.Is(err, plugins.ErrNotSupported),
		"MooseFS Copy() must return plugins.ErrNotSupported, got: %v", err)
}

// TestMooseFS_Copy_NotConnected_ReturnsErrNotSupported verifies that even
// without a connection Copy() returns ErrNotSupported (not a connection error),
// since the operation is fundamentally not supported by the protocol.
func TestMooseFS_Copy_NotConnected_ReturnsErrNotSupported(t *testing.T) {
	b := New()
	// Do not connect — backend is disconnected.

	err := b.Copy(context.Background(), "/src.txt", "/dst.txt")

	require.Error(t, err)
	assert.True(t, errors.Is(err, plugins.ErrNotSupported),
		"disconnected MooseFS Copy() must still return ErrNotSupported, got: %v", err)
}

// ─── Rename alias ────────────────────────────────────────────────────────────

// TestMooseFS_Rename_DelegatesToMove verifies that Rename() behaves like
// Move() (native MooseFS rename — the file must appear under the new name).
func TestMooseFS_Rename_DelegatesToMove(t *testing.T) {
	addr := startFakeServer(t)
	b := newTestBackend(t, addr)
	ctx := context.Background()

	// Upload a file so we have something to rename.
	local := writeTempFile(t, []byte("rename via mfs"))
	require.NoError(t, b.Upload(ctx, local, "/rename-before.txt", nil))

	require.NoError(t, b.Rename(ctx, "/rename-before.txt", "/rename-after.txt"))

	// Old name must be gone.
	_, errOld := b.Stat(ctx, "/rename-before.txt")
	assert.Error(t, errOld, "old name must not exist after Rename")

	// New name must exist.
	fi, errNew := b.Stat(ctx, "/rename-after.txt")
	require.NoError(t, errNew, "new name must exist after Rename")
	assert.Greater(t, fi.Size, int64(0))
}

// TestMooseFS_Rename_NotFound verifies that renaming a non-existent file
// returns an error (not a silent no-op).
func TestMooseFS_Rename_NotFound(t *testing.T) {
	addr := startFakeServer(t)
	b := newTestBackend(t, addr)

	err := b.Rename(context.Background(), "/ghost.txt", "/nowhere.txt")
	require.Error(t, err, "Rename of non-existent file must return an error")
}

// TestMooseFS_Copy_ContextCancelled_ReturnsErrNotSupported verifies that
// Copy() returns ErrNotSupported even when the context is already cancelled
// (the ErrNotSupported check happens before any I/O).
func TestMooseFS_Copy_ContextCancelled_ReturnsErrNotSupported(t *testing.T) {
	addr := startFakeServer(t)
	b := newTestBackend(t, addr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := b.Copy(ctx, "/src.txt", "/dst.txt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, plugins.ErrNotSupported),
		"Copy with cancelled context must still return ErrNotSupported, got: %v", err)
}

// Note: startFakeServer, newTestBackend and writeTempFile are defined in
// moosefs_test.go (same package) and are therefore available here without
// re-declaration.
