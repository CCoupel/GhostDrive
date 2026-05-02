// Package placeholder — whitebox tests that require access to unexported fields.
// Uses package placeholder (not placeholder_test) so it can directly manipulate
// the internal drives map to set up pool state without going through Mount
// (which always fails on non-Windows via NullDrive.ErrNotSupported).
package placeholder

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// injectEntry adds a NullDrive entry directly to dm.drives bypassing Mount.
// This lets us test GetStatus / GetAllStatuses / Unmount / UnmountAll on a
// non-empty pool without requiring a real WinFsp drive.
func injectEntry(dm *DriveManager, id, name string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.drives[id] = driveEntry{drive: New(), id: id, name: name}
}

// ─── AvailableDriveLetters ────────────────────────────────────────────────────

// TestAvailableDriveLetters_NonWindows verifies that AvailableDriveLetters
// returns nil on non-Windows (drive letters are Windows-only).
func TestAvailableDriveLetters_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("non-Windows specific test")
	}
	letters := AvailableDriveLetters()
	assert.Nil(t, letters, "AvailableDriveLetters must return nil on non-Windows")
}

// ─── GetStatus with registered entry ─────────────────────────────────────────

// TestDriveManager_GetStatus_WithEntry verifies that GetStatus returns (status, true)
// for a registered backend and that BackendID / BackendName are populated.
func TestDriveManager_GetStatus_WithEntry(t *testing.T) {
	dm := NewDriveManager()
	injectEntry(dm, "backend-1", "MyBackend")

	s, ok := dm.GetStatus("backend-1")
	require.True(t, ok, "GetStatus must return true for a registered backendID")
	assert.Equal(t, "backend-1", s.BackendID, "BackendID must match the registered ID")
	assert.Equal(t, "MyBackend", s.BackendName, "BackendName must match the registered name")
}

// TestDriveManager_GetStatus_WithMultipleEntries verifies that GetStatus
// returns data only for the requested backend when multiple are registered.
func TestDriveManager_GetStatus_WithMultipleEntries(t *testing.T) {
	dm := NewDriveManager()
	injectEntry(dm, "b1", "Backend1")
	injectEntry(dm, "b2", "Backend2")

	s1, ok1 := dm.GetStatus("b1")
	require.True(t, ok1)
	assert.Equal(t, "b1", s1.BackendID)
	assert.Equal(t, "Backend1", s1.BackendName)

	s2, ok2 := dm.GetStatus("b2")
	require.True(t, ok2)
	assert.Equal(t, "b2", s2.BackendID)
	assert.Equal(t, "Backend2", s2.BackendName)
}

// ─── GetAllStatuses with registered entries ───────────────────────────────────

// TestDriveManager_GetAllStatuses_WithEntries verifies that GetAllStatuses returns
// every registered drive with BackendID and BackendName correctly set.
func TestDriveManager_GetAllStatuses_WithEntries(t *testing.T) {
	dm := NewDriveManager()
	injectEntry(dm, "a1", "Alpha")
	injectEntry(dm, "a2", "Beta")

	statuses := dm.GetAllStatuses()
	require.Len(t, statuses, 2, "GetAllStatuses must return exactly 2 entries")

	s1, ok := statuses["a1"]
	require.True(t, ok, "status for a1 must be present")
	assert.Equal(t, "a1", s1.BackendID)
	assert.Equal(t, "Alpha", s1.BackendName)

	s2, ok := statuses["a2"]
	require.True(t, ok, "status for a2 must be present")
	assert.Equal(t, "a2", s2.BackendID)
	assert.Equal(t, "Beta", s2.BackendName)
}

// TestDriveManager_GetAllStatuses_IsCopy verifies that the map returned by
// GetAllStatuses is an independent snapshot (modifying it does not affect the pool).
func TestDriveManager_GetAllStatuses_IsCopy(t *testing.T) {
	dm := NewDriveManager()
	injectEntry(dm, "snap-1", "Snap")

	first := dm.GetAllStatuses()
	require.Len(t, first, 1)

	// Mutate the snapshot.
	delete(first, "snap-1")

	// Pool must still have the entry.
	second := dm.GetAllStatuses()
	assert.Len(t, second, 1, "pool must not be affected by mutations of the returned snapshot")
}

// ─── Unmount with registered entry ───────────────────────────────────────────

// TestDriveManager_Unmount_WithEntry verifies the normal unmount path:
// the entry is removed from the pool and no error is returned
// (NullDrive.Unmount is a no-op).
func TestDriveManager_Unmount_WithEntry(t *testing.T) {
	dm := NewDriveManager()
	injectEntry(dm, "del-1", "ToDelete")

	require.NoError(t, dm.Unmount("del-1"), "Unmount of a registered NullDrive must succeed")

	_, ok := dm.GetStatus("del-1")
	assert.False(t, ok, "backend must be removed from pool after Unmount")
	assert.Len(t, dm.GetAllStatuses(), 0, "pool must be empty after sole entry is unmounted")
}

// TestDriveManager_Unmount_RemovesOnlyTargeted verifies that Unmount only
// removes the targeted backend and leaves other entries intact.
func TestDriveManager_Unmount_RemovesOnlyTargeted(t *testing.T) {
	dm := NewDriveManager()
	injectEntry(dm, "keep-1", "Keep1")
	injectEntry(dm, "remove-1", "Remove1")

	require.NoError(t, dm.Unmount("remove-1"))

	_, ok := dm.GetStatus("remove-1")
	assert.False(t, ok, "targeted backend must be removed")

	_, ok = dm.GetStatus("keep-1")
	assert.True(t, ok, "non-targeted backend must remain in pool")
}

// ─── UnmountAll with non-empty pool ──────────────────────────────────────────

// TestDriveManager_UnmountAll_NonEmpty verifies that UnmountAll successfully
// dismounts and removes all entries from a non-empty pool.
func TestDriveManager_UnmountAll_NonEmpty(t *testing.T) {
	dm := NewDriveManager()
	injectEntry(dm, "u1", "U1")
	injectEntry(dm, "u2", "U2")
	injectEntry(dm, "u3", "U3")

	err := dm.UnmountAll()
	assert.NoError(t, err, "UnmountAll must succeed when all drives unmount cleanly (NullDrive)")
	assert.Len(t, dm.GetAllStatuses(), 0, "pool must be empty after UnmountAll")
}
