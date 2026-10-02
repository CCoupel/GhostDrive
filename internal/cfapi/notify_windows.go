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
	"strings"
	"unsafe"
)

// NotifyLocalChange signals the Windows shell to refresh Explorer for the
// parent directory of localPath via SHChangeNotify(SHCNE_UPDATEDIR).
// Called after a remote→local sync operation (download, delete, rename) so
// Explorer reflects the new state without requiring F5 (#150).
// Implements sync.LocalRefresher.
//
// #154 — SHChangeNotify(SHCNE_UPDATEDIR, SHCNF_PATH, ...) requires the
// directory path to end with a backslash on Windows; without it the shell
// ignores the notification and Explorer does not refresh until F5.
func (m *CFManager) NotifyLocalChange(localPath string) {
	dir := filepath.Dir(localPath)
	// #154 — append trailing separator so SHCNF_PATH resolves correctly.
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	cDir := C.CString(dir)
	defer C.free(unsafe.Pointer(cDir))
	wDir := C.ghd_utf8_to_wchar(cDir)
	if wDir == nil {
		return
	}
	defer C.ghd_free_wchar(wDir)
	C.ghd_notify_dir_change(wDir)
}
