//go:build !windows

package singleinstance

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// lockFile is kept alive at package level so the GC cannot collect it and
// silently release the flock before the process exits.
var lockFile *os.File

// Acquire tries to acquire an exclusive non-blocking flock on a temp file.
// Returns (true, nil) if this is the first instance, (false, nil) if another
// instance is already running, or (false, err) on unexpected failure.
func Acquire() (bool, error) {
	path := filepath.Join(os.TempDir(), "ghostdrive.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if err == unix.EWOULDBLOCK {
			return false, nil
		}
		return false, err
	}
	lockFile = f
	return true, nil
}
