//go:build windows

package singleinstance

import (
	"golang.org/x/sys/windows"
)

// Acquire tries to acquire the global single-instance mutex.
// Returns (true, nil) if this is the first instance, (false, nil) if another
// instance is already running, or (false, err) on unexpected failure.
// The mutex handle is intentionally never closed — it must live for the entire
// process lifetime to keep subsequent instances from acquiring it.
func Acquire() (bool, error) {
	_, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr("Local\\GhostDrive"))
	if err == nil {
		return true, nil
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		return false, nil
	}
	return false, err
}
