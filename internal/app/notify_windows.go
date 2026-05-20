//go:build windows

package app

import (
	"syscall"
	"unsafe"
)

// notifyShellDirChanged sends SHCNE_UPDATEDIR to Windows Explorer so it
// refreshes the directory listing for mountPoint without requiring an F5.
//
// Must be called at the application layer (not from the WinFsp driver), because
// SHChangeNotify is a Shell API that targets the Explorer process, not the VFS.
// Non-fatal: errors are silently swallowed — Shell notification is best-effort.
func notifyShellDirChanged(mountPoint string) {
	if mountPoint == "" {
		return
	}
	// Normalise bare drive letters ("G:") to "G:\" so the Shell sees a valid
	// directory path for the SHCNE_UPDATEDIR notification.
	if len(mountPoint) == 2 && mountPoint[1] == ':' {
		mountPoint = mountPoint + `\`
	}

	ptr, err := syscall.UTF16PtrFromString(mountPoint)
	if err != nil {
		return
	}

	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("SHChangeNotify")

	const (
		shcneUpdateDir = 0x00001000 // SHCNE_UPDATEDIR — directory contents changed
		shcnfPathW     = 0x0005     // SHCNF_PATHW     — lParam1 is a wide-char path
	)
	proc.Call(
		uintptr(shcneUpdateDir),
		uintptr(shcnfPathW),
		uintptr(unsafe.Pointer(ptr)),
		0,
	)
}
