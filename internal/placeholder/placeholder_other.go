//go:build !windows

package placeholder

import syncdispatch "github.com/CCoupel/GhostDrive/internal/sync"

// NullDrive is a no-op VirtualDrive for non-Windows platforms.
// All mutating operations return ErrNotSupported; read-only queries return
// zero values.
type NullDrive struct{}

// Mount always returns ErrNotSupported on non-Windows platforms.
func (n *NullDrive) Mount(_ string, _ []MountedBackend) error {
	return ErrNotSupported
}

// Unmount is a no-op (the drive is never mounted on non-Windows).
func (n *NullDrive) Unmount() error { return nil }

// IsMounted always returns false on non-Windows platforms.
func (n *NullDrive) IsMounted() bool { return false }

// Status returns an empty DriveStatus.
func (n *NullDrive) Status() DriveStatus { return DriveStatus{} }

// SetEmitter is a no-op on non-Windows platforms; WinFsp is unavailable.
func (n *NullDrive) SetEmitter(_ syncdispatch.EventEmitter) {}

// UpdateBackends always returns ErrNotSupported on non-Windows platforms.
func (n *NullDrive) UpdateBackends(_ []MountedBackend) error { return ErrNotSupported }
