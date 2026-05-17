// Package placeholder — v2.0 manager coherence tests.
//
// Verifies the UpdateBackends / BackendPaths invariants defined in
// contracts/v2.0-vfs-foundation.md §6.2 and §6.3:
//
//   - When UpdateBackends() fails, BackendPaths must NOT contain the backend
//     (prevents tray from showing a backend as "active" after a VFS error).
//   - When UpdateBackends() succeeds, BackendPaths must contain the backend
//     with the correct path (<mountPoint>\<Name>\).
//   - Deactivating a backend (UpdateBackends with reduced list) removes it
//     from BackendPaths.
//
// These tests use an in-process mock VirtualDrive to run on every platform
// without requiring WinFsp.
package placeholder

import (
	"fmt"
	"sync"
	"testing"

	syncdispatch "github.com/CCoupel/GhostDrive/internal/sync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Mock VirtualDrive ────────────────────────────────────────────────────────

// mockDriveWithPaths is a VirtualDrive whose UpdateBackends either succeeds
// (updating BackendPaths in Status()) or fails (leaves BackendPaths unchanged).
// Used to test DriveManager.UpdateBackends / GetUnifiedStatus invariants.
type mockDriveWithPaths struct {
	mu           sync.Mutex
	backendPaths map[string]string
	updateErr    error  // if non-nil, UpdateBackends returns this error
	mountPoint   string // base path used to construct BackendPaths (e.g. "G:")
}

func (m *mockDriveWithPaths) Mount(_ string, _ []MountedBackend) error    { return nil }
func (m *mockDriveWithPaths) Unmount() error                              { return nil }
func (m *mockDriveWithPaths) IsMounted() bool                             { return true }
func (m *mockDriveWithPaths) SetEmitter(_ syncdispatch.EventEmitter)      {}

func (m *mockDriveWithPaths) Status() DriveStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	paths := make(map[string]string, len(m.backendPaths))
	for k, v := range m.backendPaths {
		paths[k] = v
	}
	return DriveStatus{
		Mounted:      true,
		MountPoint:   m.mountPoint,
		BackendID:    unifiedDriveKey,
		BackendName:  "GhostDrive",
		BackendPaths: paths,
	}
}

func (m *mockDriveWithPaths) UpdateBackends(backends []MountedBackend) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	paths := make(map[string]string, len(backends))
	for _, mb := range backends {
		// Mirror the WinFspDrive.Status() path construction: mountPoint\Name\
		paths[mb.ID] = m.mountPoint + `\` + mb.Name + `\`
	}
	m.backendPaths = paths
	return nil
}

// injectMockUnifiedEntry injects a mockDriveWithPaths at the canonical unified
// key, bypassing MountUnified (which fails on non-Windows).
func injectMockUnifiedEntry(dm *DriveManager, mock *mockDriveWithPaths) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.drives[unifiedDriveKey] = driveEntry{
		drive: mock,
		id:    unifiedDriveKey,
		name:  "GhostDrive",
	}
}

// ─── UpdateBackends failure → BackendPaths absent ─────────────────────────────

// TestDriveManager_UpdateBackends_Fails_BackendAbsentFromStatus verifies the
// invariant from contracts/v2.0-vfs-foundation.md §6.3:
// when UpdateBackends() fails (e.g. VFS error), BackendPaths in DriveStatus
// must NOT contain the backend whose activation was requested.
// This prevents the tray from displaying a backend as "active" when the VFS
// failed to register it.
func TestDriveManager_UpdateBackends_Fails_BackendAbsentFromStatus(t *testing.T) {
	dm := NewDriveManager(nil)
	mock := &mockDriveWithPaths{
		mountPoint: "G:",
		updateErr:  fmt.Errorf("winfsp: simulated UpdateBackends VFS failure"),
	}
	injectMockUnifiedEntry(dm, mock)

	mb := MountedBackend{ID: "b1", Name: "MonNAS"}
	err := dm.UpdateBackends([]MountedBackend{mb})
	require.Error(t, err,
		"UpdateBackends must propagate the VFS error to the caller")

	// Unified drive entry still exists (drive is mounted), but BackendPaths must
	// NOT contain the backend whose activation failed.
	s, ok := dm.GetUnifiedStatus()
	require.True(t, ok,
		"GetUnifiedStatus must still return the unified drive after UpdateBackends failure")
	assert.Empty(t, s.BackendPaths,
		"BackendPaths must be empty when UpdateBackends failed (no optimistic update)")
	assert.NotContains(t, s.BackendPaths, "b1",
		"backend b1 must NOT appear in BackendPaths after a failed UpdateBackends (#120 coherence)")
}

// ─── UpdateBackends success → BackendPaths populated ─────────────────────────

// TestDriveManager_ActivateBackend_BackendPathsPopulated verifies the invariant
// from contracts/v2.0-vfs-foundation.md §6.2:
// when UpdateBackends() succeeds, DriveStatus.BackendPaths must contain the
// activated backend with the correct path (<mountPoint>\<backendName>\).
func TestDriveManager_ActivateBackend_BackendPathsPopulated(t *testing.T) {
	dm := NewDriveManager(nil)
	mock := &mockDriveWithPaths{mountPoint: "G:"}
	injectMockUnifiedEntry(dm, mock)

	mb := MountedBackend{ID: "nas1", Name: "MonNAS"}
	require.NoError(t, dm.UpdateBackends([]MountedBackend{mb}),
		"UpdateBackends must succeed on the mock drive")

	s, ok := dm.GetUnifiedStatus()
	require.True(t, ok,
		"unified status must be present after successful UpdateBackends")
	require.NotNil(t, s.BackendPaths,
		"BackendPaths must not be nil after backend activation")
	assert.Contains(t, s.BackendPaths, "nas1",
		"BackendPaths must contain the activated backend ID")
	assert.Equal(t, `G:\MonNAS\`, s.BackendPaths["nas1"],
		"BackendPaths path must equal mountPoint+\\+backendName+\\ (§6.2)")
}

// ─── UpdateBackends removes backend → BackendPaths updated ───────────────────

// TestDriveManager_DeactivateBackend_RemovedFromBackendPaths verifies that
// disabling a backend (calling UpdateBackends with a reduced list) removes it
// from DriveStatus.BackendPaths while preserving other active backends.
// Corresponds to contracts/v2.0-vfs-foundation.md §6.3:
// "Backend désactivé → absent de GhD:\ ET absent de DriveStatus.BackendPaths".
func TestDriveManager_DeactivateBackend_RemovedFromBackendPaths(t *testing.T) {
	dm := NewDriveManager(nil)
	mock := &mockDriveWithPaths{mountPoint: "G:"}
	injectMockUnifiedEntry(dm, mock)

	mb1 := MountedBackend{ID: "b1", Name: "NAS1"}
	mb2 := MountedBackend{ID: "b2", Name: "WebDAV"}

	// Activate both backends.
	require.NoError(t, dm.UpdateBackends([]MountedBackend{mb1, mb2}),
		"initial activation of both backends must succeed")

	s, _ := dm.GetUnifiedStatus()
	require.Contains(t, s.BackendPaths, "b1",
		"precondition: b1 must be present before deactivation")
	require.Contains(t, s.BackendPaths, "b2",
		"precondition: b2 must be present before deactivation")

	// Deactivate b2 by removing it from the list.
	require.NoError(t, dm.UpdateBackends([]MountedBackend{mb1}),
		"deactivation of b2 must succeed")

	s, ok := dm.GetUnifiedStatus()
	require.True(t, ok, "unified status must remain present after deactivation")
	assert.Contains(t, s.BackendPaths, "b1",
		"b1 must remain in BackendPaths after b2 is deactivated")
	assert.NotContains(t, s.BackendPaths, "b2",
		"b2 must be removed from BackendPaths after deactivation (§6.3)")
}
