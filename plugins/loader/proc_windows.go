//go:build windows

package loader

import (
	"os/exec"
	"syscall"
)

// hideCmdWindow configures cmd to start without a visible console window on
// Windows. This prevents a brief black window from flashing when a plugin
// subprocess is launched.
func hideCmdWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
