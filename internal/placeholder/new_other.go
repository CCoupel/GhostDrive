//go:build !windows

package placeholder

// New returns a NullDrive on non-Windows platforms.
func New() VirtualDrive {
	return &NullDrive{}
}
