//go:build !windows

package cfapi

// RegisterStorageProvider is a no-op on non-Windows platforms.
// On Windows it writes the SyncRootManager registry keys that Windows requires
// to recognise GhostDrive as a cloud storage provider and display cloud overlay
// icons (☁️, ✓✓, ⟳) in Explorer.
func RegisterStorageProvider(_, _, _ string) error {
	return nil
}
