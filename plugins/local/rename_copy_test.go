package local

// Tests for Backend.Rename() and Backend.Copy() (v2.2 #139 #140).
//
// Rename uses os.Rename (atomic on POSIX).
// Copy uses io.Copy from src to dst.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Rename ──────────────────────────────────────────────────────────────────

// TestRename_OK verifies that Rename moves a file via os.Rename and that the
// source is gone while the destination contains the original content.
func TestRename_OK(t *testing.T) {
	b, dir := newConnected(t)
	ctx := context.Background()

	// Create source file.
	srcContent := []byte("rename me locally")
	srcAbs := writeFile(t, dir, "rename-src.txt", srcContent)
	_ = srcAbs

	require.NoError(t, b.Rename(ctx, "rename-src.txt", "rename-dst.txt"))

	// Source must be gone.
	_, err := os.Stat(filepath.Join(dir, "rename-src.txt"))
	assert.True(t, os.IsNotExist(err), "source must not exist after Rename")

	// Destination must have the original content.
	got, err := os.ReadFile(filepath.Join(dir, "rename-dst.txt"))
	require.NoError(t, err, "destination must exist after Rename")
	assert.Equal(t, srcContent, got)
}

// TestRename_NotFound verifies that renaming a non-existent source returns
// a wrapped ErrFileNotFound (plugins.ErrFileNotFound).
func TestRename_NotFound(t *testing.T) {
	b, _ := newConnected(t)

	err := b.Rename(context.Background(), "ghost.txt", "nowhere.txt")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound,
		"Rename of non-existent file must return ErrFileNotFound")
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// TestRename_NotConnected verifies that Rename on a disconnected backend
// returns an error without panicking.
func TestRename_NotConnected(t *testing.T) {
	b := New()
	err := b.Rename(context.Background(), "src.txt", "dst.txt")
	require.Error(t, err)
}

// TestRename_InSubdir verifies that Rename works for files inside sub-directories.
func TestRename_InSubdir(t *testing.T) {
	b, dir := newConnected(t)
	ctx := context.Background()

	writeFile(t, dir, "sub/before.txt", []byte("subdir rename"))

	require.NoError(t, b.Rename(ctx, "sub/before.txt", "sub/after.txt"))

	_, errSrc := os.Stat(filepath.Join(dir, "sub", "before.txt"))
	assert.True(t, os.IsNotExist(errSrc))

	_, errDst := os.Stat(filepath.Join(dir, "sub", "after.txt"))
	assert.NoError(t, errDst)
}

// ─── Copy ────────────────────────────────────────────────────────────────────

// TestCopy_OK verifies that Copy duplicates a file via io.Copy and that
// both source and destination exist with identical content.
func TestCopy_OK(t *testing.T) {
	b, dir := newConnected(t)
	ctx := context.Background()

	content := []byte("copy me locally")
	writeFile(t, dir, "copy-src.txt", content)

	require.NoError(t, b.Copy(ctx, "copy-src.txt", "copy-dst.txt"))

	// Source still exists.
	srcData, err := os.ReadFile(filepath.Join(dir, "copy-src.txt"))
	require.NoError(t, err, "source must still exist after Copy")
	assert.Equal(t, content, srcData)

	// Destination exists with same content.
	dstData, err := os.ReadFile(filepath.Join(dir, "copy-dst.txt"))
	require.NoError(t, err, "destination must exist after Copy")
	assert.Equal(t, content, dstData)
}

// TestCopy_SourceNotFound verifies that copying a non-existent source returns
// a wrapped ErrFileNotFound.
func TestCopy_SourceNotFound(t *testing.T) {
	b, _ := newConnected(t)

	err := b.Copy(context.Background(), "ghost.txt", "nowhere.txt")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFileNotFound)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// TestCopy_NotConnected verifies that Copy on a disconnected backend returns
// an error without panicking.
func TestCopy_NotConnected(t *testing.T) {
	b := New()
	err := b.Copy(context.Background(), "src.txt", "dst.txt")
	require.Error(t, err)
}

// TestCopy_CreatesParentDirs verifies that Copy creates the destination parent
// directory hierarchy if it does not already exist.
func TestCopy_CreatesParentDirs(t *testing.T) {
	b, dir := newConnected(t)
	ctx := context.Background()

	writeFile(t, dir, "flat.txt", []byte("flat file"))

	// Destination is in a sub-directory that doesn't exist yet.
	require.NoError(t, b.Copy(ctx, "flat.txt", "deep/nested/copy.txt"))

	dstData, err := os.ReadFile(filepath.Join(dir, "deep", "nested", "copy.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("flat file"), dstData)
}

// TestCopy_OverwritesDestination verifies that Copy succeeds even when the
// destination file already exists (it is overwritten silently).
func TestCopy_OverwritesDestination(t *testing.T) {
	b, dir := newConnected(t)
	ctx := context.Background()

	writeFile(t, dir, "over-src.txt", []byte("new content"))
	writeFile(t, dir, "over-dst.txt", []byte("old content"))

	require.NoError(t, b.Copy(ctx, "over-src.txt", "over-dst.txt"))

	got, err := os.ReadFile(filepath.Join(dir, "over-dst.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("new content"), got, "destination must be overwritten")
}
