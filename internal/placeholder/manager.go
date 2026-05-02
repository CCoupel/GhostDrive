// Package placeholder — DriveManager manages a pool of per-backend virtual drives.
// Cross-platform: uses placeholder.New() which returns NullDrive on non-Windows.
// No build tags needed — NullDrive handles non-Windows transparently.
package placeholder

import (
	"fmt"
	"strings"
	"sync"
)

// driveEntry pairs a VirtualDrive with its owning backend metadata so that
// GetStatus / GetAllStatuses can decorate DriveStatus with BackendID/BackendName.
type driveEntry struct {
	drive VirtualDrive
	id    string // backendID
	name  string // human-readable backend name
}

// DriveManager manages a pool of per-backend VirtualDrives keyed by backendID.
// Thread-safe: all public methods acquire mu.
//
// Design constraints:
//   - No EventEmitter: events are emitted by app.go after calls to the manager.
//   - No Wails dependency: the placeholder package must not import the Wails runtime.
type DriveManager struct {
	mu     sync.RWMutex
	drives map[string]driveEntry // keyed by backendID
}

// NewDriveManager creates an empty DriveManager with no EventEmitter.
// Events (drive:mounted, drive:unmounted, drive:error) are emitted by app.go.
func NewDriveManager() *DriveManager {
	return &DriveManager{
		drives: make(map[string]driveEntry),
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
// The returned status has BackendID and BackendName populated.
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
	return s, true
}

// GetAllStatuses returns a snapshot map of backendID → DriveStatus for all
// drives currently in the pool. BackendID and BackendName are set on each status.
func (dm *DriveManager) GetAllStatuses() map[string]DriveStatus {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	result := make(map[string]DriveStatus, len(dm.drives))
	for id, entry := range dm.drives {
		s := entry.drive.Status()
		s.BackendID = entry.id
		s.BackendName = entry.name
		result[id] = s
	}
	return result
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
