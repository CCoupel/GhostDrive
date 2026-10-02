//go:build !windows

// Package cfapi — non-Windows stub for CFManager.NotifyLocalChange.
// SHChangeNotify is Windows-only; this no-op satisfies sync.LocalRefresher on
// Linux/macOS so the sync package compiles and tests cleanly cross-platform.

package cfapi

// NotifyLocalChange is a no-op on non-Windows platforms.
// On Windows this fires SHChangeNotify(SHCNE_UPDATEDIR, ...) — see notify_windows.go.
func (m *CFManager) NotifyLocalChange(_ string) {}
