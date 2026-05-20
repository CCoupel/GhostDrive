//go:build !windows

package app

// notifyShellDirChanged is a no-op on non-Windows platforms.
func notifyShellDirChanged(_ string) {}
