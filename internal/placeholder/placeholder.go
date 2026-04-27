// Package placeholder implements the GhD: virtual drive via WinFsp (Windows)
// and a no-op NullDrive for all other platforms.
package placeholder

import (
	"errors"

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
	Mounted      bool
	MountPoint   string            // e.g. "G:" or `C:\GhostDrive\GhD\`
	BackendPaths map[string]string // backendID → path under the drive root
	LastError    string            // last mount/unmount error message; empty if none
}
