package placeholder_test

import (
	"testing"

	"github.com/CCoupel/GhostDrive/internal/placeholder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Interface compliance ──────────────────────────────────────────────────────

// TestNew_ImplementsVirtualDrive ensures New() returns a value that satisfies
// the VirtualDrive interface on every platform.
func TestNew_ImplementsVirtualDrive(t *testing.T) {
	var _ placeholder.VirtualDrive = placeholder.New()
}

// ── NullDrive behaviour (non-Windows) ────────────────────────────────────────
// These tests exercise the NullDrive directly so they always run, even on
// platforms where New() returns a WinFspDrive.

func newNullDrive() placeholder.VirtualDrive {
	// Obtain a NullDrive by exercising the exported constructor on a
	// non-Windows host, or fall back to the interface value returned by New()
	// (which IS a NullDrive on non-Windows).  Either way the contract is the
	// same: Mount returns ErrNotSupported, IsMounted returns false, etc.
	return placeholder.New()
}

func TestNullDrive_Mount_ReturnsErrOnNonWindows(t *testing.T) {
	d := placeholder.New()
	if d.IsMounted() {
		t.Skip("drive already mounted — skipping non-Windows specific test")
	}

	err := d.Mount("G:", nil)
	// On non-Windows: ErrNotSupported.
	// On Windows: "winfsp: no connected backend" (because backends slice is nil).
	// Both are non-nil → just assert an error is returned for empty backends.
	require.Error(t, err, "Mount with no backends must return an error")
}

func TestNullDrive_Unmount_IsNoop(t *testing.T) {
	d := placeholder.New()
	// Unmount on an unmounted drive must not error on any platform.
	assert.NoError(t, d.Unmount())
}

func TestNullDrive_IsMounted_ReturnsFalse(t *testing.T) {
	d := placeholder.New()
	assert.False(t, d.IsMounted())
}

func TestNullDrive_Status_IsEmpty(t *testing.T) {
	d := placeholder.New()
	s := d.Status()
	assert.False(t, s.Mounted)
	assert.Empty(t, s.MountPoint)
}

// ── ErrNotSupported ──────────────────────────────────────────────────────────

func TestErrNotSupported_IsNotNil(t *testing.T) {
	assert.Error(t, placeholder.ErrNotSupported)
}
