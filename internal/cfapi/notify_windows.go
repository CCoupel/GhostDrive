//go:build windows

// Package cfapi — Windows implementation of CFManager.NotifyLocalChange.
// Calls SHChangeNotify(SHCNE_UPDATEDIR, ...) for the parent directory of the
// changed local path so Explorer reflects remote→local sync operations without
// requiring F5 (#150).

package cfapi

// #cgo CFLAGS: -I. -I${SRCDIR}/include
// #cgo LDFLAGS: -lshell32
// #include "cgo_cfapi_windows.h"
// #include <stdlib.h>
import "C"
import (
	"path/filepath"
	"unsafe"
)

// NotifyLocalChange signals the Windows shell to refresh Explorer for the
// parent directory of localPath via SHChangeNotify(SHCNE_UPDATEDIR).
// Called after a remote→local sync operation (download, delete, rename) so
// Explorer reflects the new state without requiring F5 (#150).
// Implements sync.LocalRefresher.
func (m *CFManager) NotifyLocalChange(localPath string) {
	dir := filepath.Dir(localPath)
	cDir := C.CString(dir)
	defer C.free(unsafe.Pointer(cDir))
	wDir := C.ghd_utf8_to_wchar(cDir)
	if wDir == nil {
		return
	}
	defer C.ghd_free_wchar(wDir)
	C.ghd_notify_dir_change(wDir)
}
