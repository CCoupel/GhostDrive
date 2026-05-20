// Package placeholder implements the GhD: virtual drive via WinFsp (Windows)
// and a no-op NullDrive for all other platforms.
package placeholder

import (
	"errors"

	syncdispatch "github.com/CCoupel/GhostDrive/internal/sync"
	"github.com/CCoupel/GhostDrive/plugins"
)

// ErrNotSupported is returned on platforms where WinFsp is unavailable.
var ErrNotSupported = errors.New("winfsp: not supported on this platform")

// VirtualDrive is the interface for the GhD: virtual drive.
type VirtualDrive interface {
	// Mount mounts the drive at mountPoint (e.g. "G:" or `C:\GhostDrive\GhD\`)
	// exposing backends as sub-folders.  No-op and returns nil if already mounted.
	Mount(mountPoint string, backends []MountedBackend) error

	// Unmount dismounts the drive cleanly.  No-op and returns nil if not mounted.
	Unmount() error

	// IsMounted reports whether the drive is currently mounted.
	IsMounted() bool

	// Status returns the current drive status.
	Status() DriveStatus

	// SetEmitter injects the EventEmitter used by GhostFileSystem.watchLoop to
	// emit Wails events (e.g. "meta:updated"). Must be called before Mount().
	// Pass nil to disable event emission (no-op emitter is used internally).
	SetEmitter(e syncdispatch.EventEmitter)

	// UpdateBackends atomically replaces the list of mounted backends on the
	// unified drive without unmounting/remounting.  The FUSE filesystem will
	// immediately start serving the new list of backends from Readdir("/").
	// Returns ErrNotSupported on non-Windows platforms (NullDrive).
	// Returns an error if the drive is not mounted.
	UpdateBackends(backends []MountedBackend) error
}

// MountedBackend pairs a StorageBackend with its identity and config.
type MountedBackend struct {
	ID      string
	Name    string
	Backend plugins.StorageBackend
	Config  plugins.BackendConfig
}

// DriveStatus holds the observable state of the virtual drive.
type DriveStatus struct {
	Mounted      bool              `json:"mounted"`
	MountPoint   string            `json:"mountPoint"`   // e.g. "G:" or `C:\GhostDrive\GhD\`
	BackendID    string            `json:"backendID"`    // backend that owns this drive (set by DriveManager)
	BackendName  string            `json:"backendName"`  // human-readable backend name (set by DriveManager)
	BackendPaths map[string]string `json:"backendPaths"` // backendID → path under the drive root
	LastError    string            `json:"lastError"`    // last mount/unmount error message; empty if none
	SyncError    string            `json:"syncError"`    // runtime sync error from the Watch loop; empty if healthy (#117b)
}
