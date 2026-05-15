// Package placeholder — DriveManager manages a pool of per-backend virtual drives.
// Cross-platform: uses placeholder.New() which returns NullDrive on non-Windows.
// No build tags needed — NullDrive handles non-Windows transparently.
package placeholder

import (
	"fmt"
	"strings"
	"sync"

	syncdispatch "github.com/CCoupel/GhostDrive/internal/sync"
)

// driveBackendEmitter wraps a base EventEmitter to intercept "sync:offline",
// "sync:error", and "sync:online" events emitted by watchLoop and bridge them
// to DriveManager.SetSyncError(backendID, ...) so that GetDriveStatuses() always
// reflects the real reachability state — even when the sync engine is not running
// (AutoSync disabled or StartSync not yet called) (#118).
type driveBackendEmitter struct {
	backendID string
	dm        *DriveManager
	base      syncdispatch.EventEmitter // may be NoopEmitter; never nil
}

// Emit forwards the event to the base emitter and, for the three reachability
// sentinels, calls SetSyncError to update the DriveStatus visible to the tray.
func (e *driveBackendEmitter) Emit(event string, data any) {
	e.base.Emit(event, data)
	switch event {
	case "sync:offline":
		e.dm.SetSyncError(e.backendID, "backend unreachable (watchLoop offline)")
	case "sync:error":
		msg := "watch error"
		if m, ok := data.(map[string]any); ok {
			if s, ok2 := m["message"].(string); ok2 && s != "" {
				msg = s
			}
		}
		e.dm.SetSyncError(e.backendID, msg)
	case "sync:online":
		e.dm.SetSyncError(e.backendID, "")
	}
}

// driveEntry pairs a VirtualDrive with its owning backend metadata so that
// GetStatus / GetAllStatuses can decorate DriveStatus with BackendID/BackendName.
type driveEntry struct {
	drive     VirtualDrive
	id        string // backendID
	name      string // human-readable backend name
	syncError string // runtime sync error from engine events; managed by SetSyncError (#117b)
}

// DriveManager manages a pool of per-backend VirtualDrives keyed by backendID.
// Thread-safe: all public methods acquire mu.
//
// Design constraints:
//   - EventEmitter: used by GhostFileSystem.watchLoop to emit "meta:updated" events.
//   - No Wails dependency: the placeholder package must not import the Wails runtime.
type DriveManager struct {
	mu      sync.RWMutex
	drives  map[string]driveEntry // keyed by backendID
	emitter syncdispatch.EventEmitter
}

// NewDriveManager creates an empty DriveManager.
// emitter is used by GhostFileSystem.watchLoop to emit Wails events (e.g. "meta:updated").
// Pass nil to use a no-op emitter (events are silently discarded).
// High-level drive events (drive:mounted, drive:unmounted, drive:error) are still
// emitted by app.go after calls to the manager.
func NewDriveManager(emitter syncdispatch.EventEmitter) *DriveManager {
	if emitter == nil {
		emitter = &syncdispatch.NoopEmitter{}
	}
	return &DriveManager{
		drives:  make(map[string]driveEntry),
		emitter: emitter,
	}
}

// Mount creates a new VirtualDrive for backendID, mounts it at mountPoint with
// the single provided MountedBackend, and stores it in the pool.
//
// If a drive already exists for backendID it is unmounted first (best-effort;
// unmount errors are ignored to allow recovery).
//
// Returns an error if the underlying VirtualDrive.Mount fails.
// On non-Windows the drive will always return ErrNotSupported — callers should
// log but not treat as fatal (the sync engine still works without the virtual drive).
func (dm *DriveManager) Mount(backendID, mountPoint string, mb MountedBackend) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Unmount and discard any existing drive for this backend.
	if existing, ok := dm.drives[backendID]; ok {
		_ = existing.drive.Unmount() // best-effort cleanup
		delete(dm.drives, backendID)
	}

	drive := New()
	// Wrap the pool-level emitter with a per-backend bridge that intercepts
	// "sync:offline" / "sync:error" / "sync:online" emitted by watchLoop and
	// routes them to SetSyncError(backendID, ...) so GetDriveStatuses() always
	// reflects reachability state regardless of whether AutoSync is enabled (#118).
	perBackendEmitter := &driveBackendEmitter{
		backendID: backendID,
		dm:        dm,
		base:      dm.emitter,
	}
	drive.SetEmitter(perBackendEmitter)
	if err := drive.Mount(mountPoint, []MountedBackend{mb}); err != nil {
		return fmt.Errorf("drivemanager: mount backend %q at %q: %w", backendID, mountPoint, err)
	}
	dm.drives[backendID] = driveEntry{drive: drive, id: backendID, name: mb.Name}
	return nil
}

// Unmount dismounts and removes the drive for backendID from the pool.
// Returns nil if no drive is registered for backendID (idempotent).
func (dm *DriveManager) Unmount(backendID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	entry, ok := dm.drives[backendID]
	if !ok {
		return nil
	}
	err := entry.drive.Unmount()
	delete(dm.drives, backendID)
	if err != nil {
		return fmt.Errorf("drivemanager: unmount backend %q: %w", backendID, err)
	}
	return nil
}

// GetStatus returns the current DriveStatus for backendID.
// The returned status has BackendID, BackendName, and SyncError populated.
// Returns (DriveStatus{}, false) if no drive is registered for backendID.
func (dm *DriveManager) GetStatus(backendID string) (DriveStatus, bool) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	entry, ok := dm.drives[backendID]
	if !ok {
		return DriveStatus{}, false
	}
	s := entry.drive.Status()
	s.BackendID = entry.id
	s.BackendName = entry.name
	s.SyncError = entry.syncError
	return s, true
}

// GetAllStatuses returns a snapshot map of backendID → DriveStatus for all
// drives currently in the pool. BackendID, BackendName, and SyncError are set.
func (dm *DriveManager) GetAllStatuses() map[string]DriveStatus {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	result := make(map[string]DriveStatus, len(dm.drives))
	for id, entry := range dm.drives {
		s := entry.drive.Status()
		s.BackendID = entry.id
		s.BackendName = entry.name
		s.SyncError = entry.syncError
		result[id] = s
	}
	return result
}

// SetSyncError records or clears a runtime sync error for the drive identified
// by backendID.  An empty errMsg clears the field (backend recovered or sync stopped).
// Called by the per-backend EventEmitter bridge in app.go whenever the sync engine
// emits "sync:offline", "sync:error", or "sync:online" (#117b).
// No-op when no drive is registered for backendID.
func (dm *DriveManager) SetSyncError(backendID, errMsg string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	entry, ok := dm.drives[backendID]
	if !ok {
		return
	}
	entry.syncError = errMsg
	dm.drives[backendID] = entry
}

// AssignAvailableLetter returns the first drive letter ≥ "E:" that is:
//  1. Not in use by the OS (checked via IsLetterInUse).
//  2. Not present in usedLetters (e.g. letters already claimed by other backends).
//
// Returns "" when no letter is available or on non-Windows platforms.
// usedLetters values are matched case-insensitively (e.g. "e:" == "E:").
func (dm *DriveManager) AssignAvailableLetter(usedLetters []string) string {
	used := make(map[string]bool, len(usedLetters))
	for _, l := range usedLetters {
		used[strings.ToUpper(strings.TrimSpace(l))] = true
	}

	for c := 'E'; c <= 'Z'; c++ {
		letter := string(c) + ":"
		if used[letter] {
			continue
		}
		if IsLetterInUse(letter) {
			continue
		}
		return letter
	}
	return ""
}

// UnmountAll dismounts all drives in the pool.
// All drives are attempted even if some fail; all errors are accumulated and
// returned as a single error so that cleanup is never short-circuited.
func (dm *DriveManager) UnmountAll() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	var errs []string
	for id, entry := range dm.drives {
		if err := entry.drive.Unmount(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", id, err))
		}
		delete(dm.drives, id)
	}
	if len(errs) > 0 {
		return fmt.Errorf("drivemanager: unmount all: %s", strings.Join(errs, "; "))
	}
	return nil
}
