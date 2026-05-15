//go:build windows

package placeholder

// Whitebox tests for unexported helpers in filesystem_windows.go.
// Build tag "windows" mirrors the production file's constraint.

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToFUSEPath verifies that toFUSEPath always returns a path starting with "/".
// Both MooseFS and WebDAV List() strip leading slashes via strings.TrimLeft so
// Watch() events arrive without them. Without normalisation, host.Notify() and
// cache invalidation would silently misbehave.
func TestToFUSEPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},              // empty → unchanged
		{"/", "/"},           // already absolute → unchanged
		{"/foo/bar", "/foo/bar"}, // already absolute → unchanged
		{"foo", "/foo"},       // missing slash → prepend
		{"foo/bar", "/foo/bar"}, // nested path → prepend
		{"newfile.txt", "/newfile.txt"}, // real Watch() payload → prepend
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, toFUSEPath(tt.input))
		})
	}
}

// TestIsCacheFresh_ZeroByteFile_ReturnsFalse verifies that a 0-byte cache file
// is treated as stale even if it was just created.
// Rationale: a 0-byte file can result from an interrupted download or from the
// WinFsp Create pre-upload phase; the correct behavior is to re-download.
func TestIsCacheFresh_ZeroByteFile_ReturnsFalse(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "cache-*.bin")
	require.NoError(t, err)
	f.Close()
	// File is 0 bytes, just created — must be considered stale.
	assert.False(t, isCacheFresh(f.Name()),
		"a 0-byte cache file must be considered stale regardless of modtime")
}

// TestIsCacheFresh_NonZeroFile_Fresh_ReturnsTrue verifies that a non-zero file
// with a recent modtime is considered fresh.
func TestIsCacheFresh_NonZeroFile_Fresh_ReturnsTrue(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "cache-*.bin")
	require.NoError(t, err)
	_, err = f.Write([]byte("hello"))
	require.NoError(t, err)
	f.Close()
	// File has content and was just written — must be considered fresh.
	assert.True(t, isCacheFresh(f.Name()),
		"a non-empty file with a recent modtime must be considered fresh")
}

// TestIsCacheFresh_StaleFile_ReturnsFalse verifies that a non-zero file whose
// modtime exceeds cacheTTL is considered stale.
func TestIsCacheFresh_StaleFile_ReturnsFalse(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "cache-*.bin")
	require.NoError(t, err)
	_, err = f.Write([]byte("stale content"))
	require.NoError(t, err)
	f.Close()

	// Set mtime far in the past (2 × cacheTTL ago).
	staleTime := time.Now().Add(-2 * cacheTTL)
	require.NoError(t, os.Chtimes(f.Name(), staleTime, staleTime))

	assert.False(t, isCacheFresh(f.Name()),
		"a non-empty file older than cacheTTL must be considered stale")
}
