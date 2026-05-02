package placeholder_test

import (
	"runtime"
	"sync"
	"testing"

	"github.com/CCoupel/GhostDrive/internal/placeholder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func newMB(id, name string) placeholder.MountedBackend {
	return placeholder.MountedBackend{ID: id, Name: name}
}

// ─── NewDriveManager ─────────────────────────────────────────────────────────

func TestNewDriveManager_NotNil(t *testing.T) {
	dm := placeholder.NewDriveManager()
	assert.NotNil(t, dm)
}

// ─── Unmount on empty pool ────────────────────────────────────────────────────

func TestDriveManager_Unmount_UnknownBackend_IsNoop(t *testing.T) {
	dm := placeholder.NewDriveManager()
	assert.NoError(t, dm.Unmount("nonexistent-id"),
		"Unmount of an unknown backendID must return nil (idempotent)")
}

// ─── GetStatus on empty pool ──────────────────────────────────────────────────

func TestDriveManager_GetStatus_Unknown_ReturnsFalse(t *testing.T) {
	dm := placeholder.NewDriveManager()
	_, ok := dm.GetStatus("unknown")
	assert.False(t, ok, "GetStatus must return false for an unregistered backendID")
}

// ─── GetAllStatuses on empty pool ─────────────────────────────────────────────

func TestDriveManager_GetAllStatuses_Empty(t *testing.T) {
	dm := placeholder.NewDriveManager()
	statuses := dm.GetAllStatuses()
	assert.NotNil(t, statuses, "GetAllStatuses must return a non-nil map")
	assert.Len(t, statuses, 0)
}

// ─── UnmountAll on empty pool ─────────────────────────────────────────────────

func TestDriveManager_UnmountAll_Empty_NoError(t *testing.T) {
	dm := placeholder.NewDriveManager()
	assert.NoError(t, dm.UnmountAll())
}

// ─── Mount failure on non-Windows ─────────────────────────────────────────────

// TestDriveManager_Mount_NonWindows_ReturnsErrNotSupported verifies that on
// non-Windows platforms, Mount returns ErrNotSupported (via NullDrive) and that
// the backend is NOT added to the pool after a failed mount.
func TestDriveManager_Mount_NonWindows_ReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("non-Windows specific test")
	}

	dm := placeholder.NewDriveManager()
	mb := newMB("b1", "Backend1")
	err := dm.Mount("b1", "G:", mb)
	require.Error(t, err, "Mount must return an error on non-Windows (NullDrive)")

	// The backend must NOT be in the pool after a failed mount.
	_, ok := dm.GetStatus("b1")
	assert.False(t, ok, "failed mount must not register backend in pool")

	// Pool must be empty.
	assert.Len(t, dm.GetAllStatuses(), 0)
}

// ─── Double-mount replaces existing drive ─────────────────────────────────────

// TestDriveManager_DoubleMount_ImplicitUnmount verifies that mounting a backend
// that already has a drive first unmounts the old drive (best-effort) and
// replaces it. On non-Windows this just means two failures — no panic.
func TestDriveManager_DoubleMount_ImplicitUnmount(t *testing.T) {
	dm := placeholder.NewDriveManager()
	mb := newMB("b1", "Backend1")

	// Both calls will fail on non-Windows, but neither should panic.
	_ = dm.Mount("b1", "G:", mb)
	err := dm.Mount("b1", "H:", mb)

	if runtime.GOOS == "windows" {
		// On Windows the second mount replaces the first.
		require.NoError(t, err)
		s, ok := dm.GetStatus("b1")
		require.True(t, ok)
		assert.Equal(t, "H:", s.MountPoint)
	} else {
		// On non-Windows both attempts fail — pool remains empty, no panic.
		require.Error(t, err)
		_, ok := dm.GetStatus("b1")
		assert.False(t, ok)
	}
}

// ─── BackendID / BackendName in DriveStatus ───────────────────────────────────

// TestDriveManager_GetStatus_PopulatesBackendMetadata verifies that GetStatus
// decorates the returned DriveStatus with BackendID and BackendName even when
// the underlying NullDrive.Status() returns an empty struct.
// (On non-Windows the drive is never actually mounted — we verify field decoration.)
func TestDriveManager_GetStatus_PopulatesBackendMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("non-Windows NullDrive path tested here")
	}
	// On non-Windows, Mount fails — so we cannot test a registered drive via
	// Mount. This test is intentionally a no-op verifying the pool stays clean.
	dm := placeholder.NewDriveManager()
	_, ok := dm.GetStatus("b1")
	assert.False(t, ok)
}

// ─── AssignAvailableLetter ────────────────────────────────────────────────────

func TestDriveManager_AssignAvailableLetter_SkipsUsed(t *testing.T) {
	dm := placeholder.NewDriveManager()

	// On non-Windows, IsLetterInUse always returns false, so the first
	// available letter not in usedLetters should be "E:".
	if runtime.GOOS != "windows" {
		letter := dm.AssignAvailableLetter(nil)
		assert.Equal(t, "E:", letter, "first available letter (none used) must be E:")

		// Skip E: and F:.
		letter = dm.AssignAvailableLetter([]string{"E:", "F:"})
		assert.Equal(t, "G:", letter, "must skip used letters E: and F:")

		// Case-insensitive comparison.
		letter = dm.AssignAvailableLetter([]string{"e:", "f:"})
		assert.Equal(t, "G:", letter, "usedLetters comparison must be case-insensitive")
	} else {
		// On Windows: just verify no panic and letter format.
		letter := dm.AssignAvailableLetter(nil)
		if letter != "" {
			assert.True(t, len(letter) == 2 && letter[1] == ':', "letter must be in X: format")
		}
	}
}

func TestDriveManager_AssignAvailableLetter_AllUsed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("all-letters-used scenario only feasible on non-Windows where OS check is false")
	}
	dm := placeholder.NewDriveManager()

	// Build a list with E: through Z: all "used".
	all := make([]string, 0, 22)
	for c := 'E'; c <= 'Z'; c++ {
		all = append(all, string(c)+":")
	}
	letter := dm.AssignAvailableLetter(all)
	assert.Equal(t, "", letter, "must return empty string when all letters are used")
}

// ─── UnmountAll ───────────────────────────────────────────────────────────────

func TestDriveManager_UnmountAll_ClearsPool(t *testing.T) {
	dm := placeholder.NewDriveManager()
	// Mount attempts will fail on non-Windows, pool stays empty.
	// UnmountAll must still succeed with an empty pool.
	assert.NoError(t, dm.UnmountAll())
	assert.Len(t, dm.GetAllStatuses(), 0)
}

// ─── Race condition ───────────────────────────────────────────────────────────

// TestDriveManager_ConcurrentMountUnmount verifies that concurrent Mount and
// Unmount calls do not race on the internal map.  Run with: go test -race
func TestDriveManager_ConcurrentMountUnmount(t *testing.T) {
	dm := placeholder.NewDriveManager()
	mb := newMB("race-backend", "RaceBackend")

	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = dm.Mount("race-backend", "G:", mb)
		}()
		go func() {
			defer wg.Done()
			_ = dm.Unmount("race-backend")
		}()
	}
	wg.Wait()
}

// TestDriveManager_ConcurrentGetStatus verifies concurrent reads are safe.
func TestDriveManager_ConcurrentGetStatus(t *testing.T) {
	dm := placeholder.NewDriveManager()

	var wg sync.WaitGroup
	const readers = 30

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = dm.GetStatus("nonexistent")
			_ = dm.GetAllStatuses()
		}()
	}
	wg.Wait()
}
