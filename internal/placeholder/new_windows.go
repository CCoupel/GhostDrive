//go:build windows

package placeholder

// New returns a WinFspDrive on Windows.
func New() VirtualDrive {
	return &WinFspDrive{}
}
