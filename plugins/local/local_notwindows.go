//go:build !windows

package local

import (
	"context"
	"fmt"
	"syscall"
)

// GetQuota returns the free and total disk space (in bytes) for the directory
// configured as rootPath, using syscall.Statfs.
// Returns (0, 0, ErrNotConnected) when the backend is not connected.
func (b *Backend) GetQuota(_ context.Context) (free, total int64, err error) {
	b.mu.RLock()
	connected := b.connected
	rootPath := b.rootPath
	b.mu.RUnlock()

	if !connected {
		return 0, 0, fmt.Errorf("local: get_quota: %w", ErrNotConnected)
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(rootPath, &stat); err != nil {
		return 0, 0, fmt.Errorf("local: get_quota: %w", err)
	}

	// Bavail = blocks available to unprivileged processes
	// Blocks = total data blocks in filesystem
	// Bsize  = filesystem block size
	return int64(stat.Bavail) * int64(stat.Bsize),
		int64(stat.Blocks) * int64(stat.Bsize),
		nil
}
