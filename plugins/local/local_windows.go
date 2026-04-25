//go:build windows

package local

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
)

// GetQuota returns the free and total disk space (in bytes) for the directory
// configured as rootPath, using Windows GetDiskFreeSpaceEx.
// Returns (0, 0, ErrNotConnected) when the backend is not connected.
func (b *Backend) GetQuota(_ context.Context) (free, total int64, err error) {
	b.mu.RLock()
	connected := b.connected
	rootPath := b.rootPath
	b.mu.RUnlock()

	if !connected {
		return 0, 0, fmt.Errorf("local: get_quota: %w", ErrNotConnected)
	}

	rootPathPtr, err := windows.UTF16PtrFromString(rootPath)
	if err != nil {
		return 0, 0, fmt.Errorf("local: get_quota: encode path: %w", err)
	}

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(rootPathPtr, &freeBytesAvailable, &totalBytes, &totalFreeBytes); err != nil {
		return 0, 0, fmt.Errorf("local: get_quota: %w", err)
	}

	// freeBytesAvailable = free bytes available to the calling user (quota-aware)
	// totalBytes = total bytes of the disk
	return int64(freeBytesAvailable), int64(totalBytes), nil
}
