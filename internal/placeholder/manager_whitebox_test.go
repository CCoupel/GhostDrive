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
	dm := NewDriveManager(nil)
	injectEntry(dm, "backend-1", "MyBackend")

	s, ok := dm.GetStatus("backend-1")
	require.True(t, ok, "GetStatus must return true for a registered backendID")
	assert.Equal(t, "backend-1", s.BackendID, "BackendID must match the registered ID")
	assert.Equal(t, "MyBackend", s.BackendName, "BackendName must match the registered name")
}

// TestDriveManager_GetStatus_WithMultipleEntries verifies that GetStatus
// returns data only for the requested backend when multiple are registered.
func TestDriveManager_GetStatus_WithMultipleEntries(t *testing.T) {
	dm := NewDriveManager(nil)
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
	dm := NewDriveManager(nil)
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
	dm := NewDriveManager(nil)
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
	dm := NewDriveManager(nil)
	injectEntry(dm, "del-1", "ToDelete")

	require.NoError(t, dm.Unmount("del-1"), "Unmount of a registered NullDrive must succeed")

	_, ok := dm.GetStatus("del-1")
	assert.False(t, ok, "backend must be removed from pool after Unmount")
	assert.Len(t, dm.GetAllStatuses(), 0, "pool must be empty after sole entry is unmounted")
}

// TestDriveManager_Unmount_RemovesOnlyTargeted verifies that Unmount only
// removes the targeted backend and leaves other entries intact.
func TestDriveManager_Unmount_RemovesOnlyTargeted(t *testing.T) {
	dm := NewDriveManager(nil)
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
	dm := NewDriveManager(nil)
	injectEntry(dm, "u1", "U1")
	injectEntry(dm, "u2", "U2")
	injectEntry(dm, "u3", "U3")

	err := dm.UnmountAll()
	assert.NoError(t, err, "UnmountAll must succeed when all drives unmount cleanly (NullDrive)")
	assert.Len(t, dm.GetAllStatuses(), 0, "pool must be empty after UnmountAll")
}

// ─── SetSyncError / DriveStatus.SyncError (#117b) ────────────────────────────

// TestDriveManager_SetSyncError_PopulatesField verifies that SetSyncError records
// the error message and that GetStatus/GetAllStatuses surface it (#117b).
func TestDriveManager_SetSyncError_PopulatesField(t *testing.T) {
	dm := NewDriveManager(nil)
	injectEntry(dm, "se-1", "SyncErrBackend")

	dm.SetSyncError("se-1", "backend unreachable (reconnexion en cours…)")

	s, ok := dm.GetStatus("se-1")
	require.True(t, ok)
	assert.Equal(t, "backend unreachable (reconnexion en cours…)", s.SyncError,
		"GetStatus must expose the SyncError set via SetSyncError (#117b)")

	all := dm.GetAllStatuses()
	require.Contains(t, all, "se-1")
	assert.Equal(t, "backend unreachable (reconnexion en cours…)", all["se-1"].SyncError,
		"GetAllStatuses must include SyncError in the returned map (#117b)")
}

// TestDriveManager_SetSyncError_ClearsField verifies that passing an empty errMsg
// clears the SyncError field (backend recovered or sync stopped) (#117b).
func TestDriveManager_SetSyncError_ClearsField(t *testing.T) {
	dm := NewDriveManager(nil)
	injectEntry(dm, "se-2", "ClearBackend")

	dm.SetSyncError("se-2", "sync error: something failed")
	dm.SetSyncError("se-2", "") // clear

	s, ok := dm.GetStatus("se-2")
	require.True(t, ok)
	assert.Empty(t, s.SyncError, "SyncError must be empty after clearing (#117b)")
}

// TestDriveManager_SetSyncError_UnknownBackend_IsNoop verifies that calling
// SetSyncError on an unregistered backendID does not panic (#117b).
func TestDriveManager_SetSyncError_UnknownBackend_IsNoop(t *testing.T) {
	dm := NewDriveManager(nil)
	assert.NotPanics(t, func() {
		dm.SetSyncError("nonexistent", "this must not panic")
	}, "SetSyncError on an unregistered backend must be a no-op (#117b)")
}

// ─── driveBackendEmitter (#118) ───────────────────────────────────────────────

// captureEmitter records every (event, data) pair for test assertions.
type captureEmitter struct {
	events []capturedEvent
}

type capturedEvent struct {
	event string
	data  any
}

func (c *captureEmitter) Emit(event string, data any) {
	c.events = append(c.events, capturedEvent{event: event, data: data})
}

// TestDriveBackendEmitter_SyncOffline_PopulatesSyncError verifies that a
// "sync:offline" emitted via driveBackendEmitter sets DriveStatus.SyncError (#118).
func TestDriveBackendEmitter_SyncOffline_PopulatesSyncError(t *testing.T) {
	dm := NewDriveManager(nil)
	injectEntry(dm, "be-1", "OfflineBackend")

	base := &captureEmitter{}
	e := &driveBackendEmitter{backendID: "be-1", dm: dm, base: base}

	e.Emit("sync:offline", map[string]any{"backendID": "be-1"})

	s, ok := dm.GetStatus("be-1")
	require.True(t, ok)
	assert.NotEmpty(t, s.SyncError, "SyncError must be non-empty after sync:offline (#118)")

	require.Len(t, base.events, 1, "base emitter must receive the forwarded event")
	assert.Equal(t, "sync:offline", base.events[0].event)
}

// TestDriveBackendEmitter_SyncOnline_ClearsSyncError verifies that a
// "sync:online" emitted via driveBackendEmitter clears DriveStatus.SyncError (#118).
func TestDriveBackendEmitter_SyncOnline_ClearsSyncError(t *testing.T) {
	dm := NewDriveManager(nil)
	injectEntry(dm, "be-2", "OnlineBackend")
	dm.SetSyncError("be-2", "previously offline")

	base := &captureEmitter{}
	e := &driveBackendEmitter{backendID: "be-2", dm: dm, base: base}

	e.Emit("sync:online", map[string]any{"backendID": "be-2"})

	s, ok := dm.GetStatus("be-2")
	require.True(t, ok)
	assert.Empty(t, s.SyncError, "SyncError must be cleared after sync:online (#118)")

	require.Len(t, base.events, 1)
	assert.Equal(t, "sync:online", base.events[0].event)
}

// TestDriveBackendEmitter_SyncError_PopulatesSyncError verifies that a
// "sync:error" with a message payload sets DriveStatus.SyncError to that message (#118).
func TestDriveBackendEmitter_SyncError_PopulatesSyncError(t *testing.T) {
	dm := NewDriveManager(nil)
	injectEntry(dm, "be-3", "ErrorBackend")

	base := &captureEmitter{}
	e := &driveBackendEmitter{backendID: "be-3", dm: dm, base: base}

	e.Emit("sync:error", map[string]any{
		"backendID": "be-3",
		"message":   "Watch() channel closed unexpectedly",
	})

	s, ok := dm.GetStatus("be-3")
	require.True(t, ok)
	assert.Equal(t, "Watch() channel closed unexpectedly", s.SyncError,
		"SyncError must be set to the message payload from sync:error (#118)")
}

// TestDriveBackendEmitter_OtherEvent_ForwardsOnly verifies that unrelated events
// are forwarded to the base emitter without touching SyncError (#118).
func TestDriveBackendEmitter_OtherEvent_ForwardsOnly(t *testing.T) {
	dm := NewDriveManager(nil)
	injectEntry(dm, "be-4", "OtherBackend")

	base := &captureEmitter{}
	e := &driveBackendEmitter{backendID: "be-4", dm: dm, base: base}

	e.Emit("meta:updated", map[string]any{"path": "/foo/bar"})

	s, ok := dm.GetStatus("be-4")
	require.True(t, ok)
	assert.Empty(t, s.SyncError, "SyncError must not be touched for unrelated events (#118)")

	require.Len(t, base.events, 1, "base emitter must receive the forwarded event")
	assert.Equal(t, "meta:updated", base.events[0].event)
}

// TestDriveBackendEmitter_SyncError_DefaultMessage verifies that a "sync:error"
// with a non-map or empty payload still sets a non-empty fallback message (#118).
func TestDriveBackendEmitter_SyncError_DefaultMessage(t *testing.T) {
	dm := NewDriveManager(nil)
	injectEntry(dm, "be-5", "DefaultMsgBackend")

	base := &captureEmitter{}
	e := &driveBackendEmitter{backendID: "be-5", dm: dm, base: base}

	e.Emit("sync:error", nil) // nil payload — must fall back to default message

	s, ok := dm.GetStatus("be-5")
	require.True(t, ok)
	assert.NotEmpty(t, s.SyncError, "SyncError must have a fallback message even with nil payload (#118)")
}
