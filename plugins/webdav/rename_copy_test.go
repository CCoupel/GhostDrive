package webdav

// Tests for Backend.Rename() and Backend.Copy() (v2.2 #139 #140).
//
// Rename delegates to Move (HTTP MOVE).  Copy uses HTTP COPY (RFC 4918 §9.8).
// Tests run against an in-memory WebDAV server — no external server required.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Rename ──────────────────────────────────────────────────────────────────

// TestRename_OK verifies that Rename moves a remote file via HTTP MOVE and
// that the source path is gone while the destination path is accessible.
func TestRename_OK(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	// Seed source file.
	local := writeTempFile(t, []byte("rename me"))
	require.NoError(t, b.Upload(ctx, local, "/rename-src.txt", nil))

	// Rename via MOVE.
	require.NoError(t, b.Rename(ctx, "/rename-src.txt", "/rename-dst.txt"))

	// Source must be gone.
	_, errSrc := b.Stat(ctx, "/rename-src.txt")
	assert.ErrorIs(t, errSrc, ErrFileNotFound, "source must not exist after rename")

	// Destination must exist.
	fi, errDst := b.Stat(ctx, "/rename-dst.txt")
	require.NoError(t, errDst, "destination must exist after rename")
	assert.Equal(t, int64(len("rename me")), fi.Size)
}

// TestRename_SourceNotFound verifies that renaming a non-existent source
// returns a wrapped ErrFileNotFound error.
func TestRename_SourceNotFound(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)

	err := b.Rename(context.Background(), "/ghost.txt", "/nowhere.txt")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound,
		"renaming a non-existent file must return ErrFileNotFound")
}

// TestRename_NotConnected verifies that calling Rename on a disconnected backend
// returns an error without panicking.
func TestRename_NotConnected(t *testing.T) {
	b := New()
	err := b.Rename(context.Background(), "/src.txt", "/dst.txt")
	require.Error(t, err)
}

// ─── Copy ────────────────────────────────────────────────────────────────────

// TestCopy_OK verifies that Copy duplicates a remote file via HTTP COPY and
// that both source and destination exist afterwards.
func TestCopy_OK(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	// Seed source file.
	content := []byte("copy me please")
	local := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, local, "/copy-src.txt", nil))

	// Server-side copy.
	require.NoError(t, b.Copy(ctx, "/copy-src.txt", "/copy-dst.txt"))

	// Source must still exist.
	src, err := b.Stat(ctx, "/copy-src.txt")
	require.NoError(t, err, "source must still exist after copy")
	assert.Equal(t, int64(len(content)), src.Size)

	// Destination must exist with the same size.
	dst, err := b.Stat(ctx, "/copy-dst.txt")
	require.NoError(t, err, "destination must exist after copy")
	assert.Equal(t, int64(len(content)), dst.Size)
}

// TestCopy_SourceNotFound verifies that copying a non-existent source returns
// a wrapped ErrFileNotFound.
func TestCopy_SourceNotFound(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)

	err := b.Copy(context.Background(), "/ghost.txt", "/nowhere.txt")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound,
		"copying a non-existent file must return ErrFileNotFound")
}

// TestCopy_OverwritesDestination verifies that copying to an existing
// destination succeeds (Overwrite: T header, HTTP 204 No Content accepted).
func TestCopy_OverwritesDestination(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	// Upload source and existing destination.
	srcContent := []byte("new content from source")
	srcLocal := writeTempFile(t, srcContent)
	require.NoError(t, b.Upload(ctx, srcLocal, "/over-src.txt", nil))

	dstLocal := writeTempFile(t, []byte("old destination content"))
	require.NoError(t, b.Upload(ctx, dstLocal, "/over-dst.txt", nil))

	// Copy overwrites destination — HTTP 204 is a success.
	err := b.Copy(ctx, "/over-src.txt", "/over-dst.txt")
	require.NoError(t, err, "Copy with overwrite must succeed (HTTP 204)")

	// Destination now has source size.
	fi, err := b.Stat(ctx, "/over-dst.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(len(srcContent)), fi.Size)
}

// TestCopy_ServerSide_PreservesSource verifies that after a successful COPY
// both the original and the duplicate exist (pure server-side duplication,
// i.e. the source file is never deleted — unlike Move).
func TestCopy_ServerSide_PreservesSource(t *testing.T) {
	serverURL, cleanup := newTestServer(t)
	defer cleanup()

	b := newTestBackend(t, serverURL)
	ctx := context.Background()

	local := writeTempFile(t, []byte("must stay"))
	require.NoError(t, b.Upload(ctx, local, "/preserve-src.txt", nil))

	require.NoError(t, b.Copy(ctx, "/preserve-src.txt", "/preserve-dst.txt"))

	_, err := b.Stat(ctx, "/preserve-src.txt")
	assert.NoError(t, err, "Copy must not delete the source (unlike Move/Rename)")
}

// TestCopy_NotConnected verifies that calling Copy on a disconnected backend
// returns an error without panicking.
func TestCopy_NotConnected(t *testing.T) {
	b := New()
	err := b.Copy(context.Background(), "/src.txt", "/dst.txt")
	require.Error(t, err)
}
