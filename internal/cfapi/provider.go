//go:build windows

package cfapi

// #cgo CFLAGS: -I. -I${SRCDIR}/include
// #cgo LDFLAGS: -lcldapi -lruntimeobject -lshell32
// #include "cgo_cfapi_windows.h"
// #include <stdlib.h>
import "C"
import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"
	"unsafe"
)

// providerRegistry maps a connection key (int64) to the *SyncProvider that owns it.
// This global registry is necessary because CGO C callbacks cannot capture Go closures;
// instead the C shim calls the ghdOn* exported functions which look up the provider here.
var (
	providerMu       sync.RWMutex
	providerRegistry = make(map[int64]*SyncProvider)
)

// SyncProvider manages the CF API lifecycle for one backend (one sync root).
// One SyncProvider corresponds to one registered + connected sync root.
type SyncProvider struct {
	localPath   string
	providerID  string
	displayName string

	mu            sync.Mutex
	connectionKey int64 // CF_CONNECTION_KEY — 0 means not connected
	callbacks     CFCallbacks
}

// NewSyncProvider creates a SyncProvider for the given local path.
func NewSyncProvider(localPath, providerID, displayName string) *SyncProvider {
	return &SyncProvider{
		localPath:   localPath,
		providerID:  providerID,
		displayName: displayName,
	}
}

// Register calls CfRegisterSyncRoot for this provider's local path.
func (p *SyncProvider) Register() error {
	wPath := C.ghd_utf8_to_wchar(C.CString(p.localPath))
	defer C.ghd_free_wchar(wPath)
	wID := C.ghd_utf8_to_wchar(C.CString(p.providerID))
	defer C.ghd_free_wchar(wID)
	wName := C.ghd_utf8_to_wchar(C.CString(p.displayName))
	defer C.ghd_free_wchar(wName)

	hr := C.ghd_cf_register(wPath, wID, wName)
	if hr != 0 {
		return fmt.Errorf("cfapi: register sync root %s: HRESULT 0x%08x", p.localPath, uint32(hr))
	}
	return nil
}

// Deregister calls CfUnregisterSyncRoot.
func (p *SyncProvider) Deregister() error {
	wPath := C.ghd_utf8_to_wchar(C.CString(p.localPath))
	defer C.ghd_free_wchar(wPath)

	hr := C.ghd_cf_deregister(wPath)
	if hr != 0 {
		return fmt.Errorf("cfapi: deregister sync root %s: HRESULT 0x%08x", p.localPath, uint32(hr))
	}
	return nil
}

// Connect calls CfConnectSyncRoot with the given callbacks and stores the connection key.
func (p *SyncProvider) Connect(cbs CFCallbacks) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	wPath := C.ghd_utf8_to_wchar(C.CString(p.localPath))
	defer C.ghd_free_wchar(wPath)

	var key C.ghd_connection_key_t
	hr := C.ghd_cf_connect(wPath, nil, &key)
	if hr != 0 {
		return fmt.Errorf("cfapi: connect sync root %s: HRESULT 0x%08x", p.localPath, uint32(hr))
	}

	p.connectionKey = int64(key)
	p.callbacks = cbs

	providerMu.Lock()
	providerRegistry[p.connectionKey] = p
	providerMu.Unlock()

	return nil
}

// Disconnect calls CfDisconnectSyncRoot.
func (p *SyncProvider) Disconnect() error {
	p.mu.Lock()
	key := p.connectionKey
	p.connectionKey = 0
	p.mu.Unlock()

	if key == 0 {
		return nil
	}

	providerMu.Lock()
	delete(providerRegistry, key)
	providerMu.Unlock()

	hr := C.ghd_cf_disconnect(C.ghd_connection_key_t(key))
	if hr != 0 {
		return fmt.Errorf("cfapi: disconnect sync root %s: HRESULT 0x%08x", p.localPath, uint32(hr))
	}
	return nil
}

// Non-fatal HRESULT codes from CfCreatePlaceholders.
// In all these cases the placeholder is already present (or in-use) — nothing to do.
const (
	// hrAlreadyExists = HRESULT_FROM_WIN32(ERROR_ALREADY_EXISTS) — placeholder already on disk.
	// Returned on the 2nd+ FETCH_PLACEHOLDERS round-trip when CF_CREATE_FLAG_NONE is used.
	hrAlreadyExists = uint32(0x800700b7)

	// hrUserMappedFile = HRESULT_FROM_WIN32(ERROR_USER_MAPPED_FILE) — file has an open
	// memory-mapped section (FETCH_DATA is in progress for this file).  The placeholder
	// exists under a hydration lock; treat as non-fatal.
	hrUserMappedFile = uint32(0x800704c8)
)

// CreatePlaceholders creates CF placeholders inside baseDir.
// baseDir must be the directory currently being populated — the callback's localPath
// for OnFetchPlaceholders.  Passing the sync root as baseDir causes sub-folder
// contents to land at the mount root (bug #133); callers must pass the actual
// target directory.
// Uses CF_PLACEHOLDER_CREATE_FLAG_NONE — no auto-hydration on creation.
// Returns the number of placeholders successfully created.
func (p *SyncProvider) CreatePlaceholders(baseDir string, items []PlaceholderInfo) (int, error) {
	return p.createPlaceholdersWithFlags(baseDir, items, C.CF_PLACEHOLDER_CREATE_FLAG_NONE)
}

// UpdatePlaceholder updates an existing placeholder's metadata in-place.
// Uses CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE to overwrite the existing entry.
func (p *SyncProvider) UpdatePlaceholder(localPath string, fi PlaceholderInfo) error {
	// filepath.Dir extracts the directory containing localPath — the correct
	// baseDirectoryPath for a single-file placeholder update.
	_, err := p.createPlaceholdersWithFlags(
		filepath.Dir(localPath),
		[]PlaceholderInfo{fi},
		C.CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE,
	)
	return err
}

// createPlaceholdersWithFlags is the shared implementation for CreatePlaceholders
// and UpdatePlaceholder.  flags selects between initial creation (NONE) and
// in-place update (SUPERSEDE).
//
// Entries are processed one at a time (not as a batch) so each entry's result can
// be handled individually — in particular ALREADY_EXISTS for directory entries
// triggers CfConvertToPlaceholder (see below).
func (p *SyncProvider) createPlaceholdersWithFlags(baseDir string, items []PlaceholderInfo, flags C.CF_PLACEHOLDER_CREATE_FLAGS) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	// baseDir as LPCWSTR — shared across all per-entry CfCreatePlaceholders calls.
	// BUG FIX #133: use baseDir (the directory being populated by this callback),
	// NOT p.localPath (the sync root).  CfCreatePlaceholders places each entry
	// at baseDirectoryPath\RelativeFileName — if we always pass the sync root,
	// sub-folder contents end up at the root of the mount point.
	wBaseDir := C.ghd_utf8_to_wchar(C.CString(baseDir))
	defer C.ghd_free_wchar(wBaseDir)

	total := 0
	var firstErr error

	for _, item := range items {
		// Build a single CF_PLACEHOLDER_CREATE_INFO for this entry.
		var cItem C.CF_PLACEHOLDER_CREATE_INFO

		wRelPath := C.ghd_utf8_to_wchar(C.CString(item.RelativePath))
		// LPCWSTR is const WCHAR* — cast from LPWSTR (non-const) is required in CGO.
		cItem.RelativeFileName = C.LPCWSTR(wRelPath)

		// LARGE_INTEGER is a union — CGO represents it as [8]byte.
		// Access QuadPart via unsafe pointer to set the 64-bit value.
		*(*C.LONGLONG)(unsafe.Pointer(&cItem.FsMetadata.FileSize)) = C.LONGLONG(item.FileSize)

		// Convert ModTime to LARGE_INTEGER (100-nanosecond intervals since 1601-01-01).
		// CF_FS_METADATA embeds FILE_BASIC_INFO, which holds the timestamp fields.
		ft := timeToFileTime(item.ModTime)
		cItem.FsMetadata.BasicInfo.CreationTime = ft
		cItem.FsMetadata.BasicInfo.LastWriteTime = ft
		cItem.FsMetadata.BasicInfo.LastAccessTime = ft
		cItem.FsMetadata.BasicInfo.ChangeTime = ft

		if item.IsDirectory {
			cItem.FsMetadata.BasicInfo.FileAttributes = C.FILE_ATTRIBUTE_DIRECTORY
		} else {
			// FILE_ATTRIBUTE_ARCHIVE (0x20)               — normal archivable file.
			//   Must NOT use FILE_ATTRIBUTE_NORMAL (0x80) alone: it is a standalone flag
			//   that cannot be combined with other attributes.
			// FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS (0x400000) — cloud-only placeholder:
			//   · Tells Explorer to show the ☁️ cloud badge.
			//   · Prevents immediate FETCH_DATA: the OS only triggers hydration when the
			//     file content is actually read (not just when the folder is opened).
			// Must NOT include:
			//   FILE_ATTRIBUTE_PINNED         (0x80000) — forces eager background hydration.
			//   FILE_ATTRIBUTE_RECALL_ON_OPEN (0x40000) — triggers FETCH_DATA on open.
			cItem.FsMetadata.BasicInfo.FileAttributes =
				C.FILE_ATTRIBUTE_ARCHIVE | C.FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS
		}

		// FileIdentity: use FileID bytes as opaque provider identity.
		var cFileID *C.char
		if len(item.FileID) > 0 {
			cFileID = C.CString(item.FileID)
			cItem.FileIdentityLength = C.DWORD(len(item.FileID))
			// LPCVOID is const void* — CGO requires explicit cast from unsafe.Pointer.
			cItem.FileIdentity = C.LPCVOID(unsafe.Pointer(cFileID))
		}

		cItem.Flags = flags

		var created C.DWORD
		hr := C.ghd_cf_create_placeholders(wBaseDir, &cItem, 1, &created)

		// Free per-entry C allocations immediately after the call.
		C.ghd_free_wchar(wRelPath)
		if cFileID != nil {
			C.free(unsafe.Pointer(cFileID))
		}

		if hr != 0 {
			switch uint32(hr) {
			case hrAlreadyExists:
				// The local entry (file or directory) exists as an ordinary NTFS
				// entry — never registered as a CF placeholder.  Without conversion:
				//   · Directories: OS never calls FETCH_PLACEHOLDERS → remote content
				//     stays invisible.
				//   · Files: no CF attributes → no badge (not ☁️ nor ✓✓).
				//
				// Two helpers are used to set different CF_CONVERT_FLAGS:
				//   · Files → ghd_convert_to_placeholder: CF_CONVERT_FLAG_MARK_IN_SYNC
				//       → badge ✓✓ (locally present + in sync). MARK_IN_SYNC on files
				//       is correct: the local copy IS the definitive version.
				//   · Directories → ghd_convert_dir_to_placeholder:
				//       CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION (no MARK_IN_SYNC)
				//       → "partial" population state → OS calls FETCH_PLACEHOLDERS on
				//       every open, merging remote content. Using MARK_IN_SYNC on
				//       directories marks them as "fully populated" and the OS stops
				//       calling FETCH_PLACEHOLDERS after a restart (regression).
				fullPath := filepath.Join(baseDir, item.RelativePath)
				wFull := C.ghd_utf8_to_wchar(C.CString(fullPath))
				var xhr C.HRESULT
				if item.IsDirectory {
					xhr = C.ghd_convert_dir_to_placeholder(wFull)
				} else {
					xhr = C.ghd_convert_to_placeholder(wFull)
				}
				if xhr != 0 {
					log.Printf("cfapi: CfConvertToPlaceholder %s isDir=%v: HRESULT 0x%08x",
						fullPath, item.IsDirectory, uint32(xhr))
				}
				C.ghd_free_wchar(wFull)
				total++
			case hrUserMappedFile:
				// File has an open memory-mapped section (FETCH_DATA in progress).
				// Placeholder exists; nothing to do.
				total++
			default:
				if firstErr == nil {
					firstErr = fmt.Errorf("cfapi: create placeholders: HRESULT 0x%08x", uint32(hr))
				}
			}
			continue
		}
		total++
	}

	return total, firstErr
}

// SetSyncState sets the in-sync/pin state of localPath.
func (p *SyncProvider) SetSyncState(localPath string, state SyncState) error {
	wPath := C.ghd_utf8_to_wchar(C.CString(localPath))
	defer C.ghd_free_wchar(wPath)

	switch state {
	case SyncStatePinned:
		hr := C.ghd_cf_set_pin_state(wPath, 1 /* CF_PIN_STATE_PINNED */)
		if hr != 0 {
			return fmt.Errorf("cfapi: set pin state %s: HRESULT 0x%08x", localPath, uint32(hr))
		}
	case SyncStateUnpinned:
		hr := C.ghd_cf_set_pin_state(wPath, 2 /* CF_PIN_STATE_UNPINNED */)
		if hr != 0 {
			return fmt.Errorf("cfapi: set unpin state %s: HRESULT 0x%08x", localPath, uint32(hr))
		}
	case SyncStateSynced:
		hr := C.ghd_cf_set_sync_state(wPath, 1 /* CF_IN_SYNC_STATE_IN_SYNC */)
		if hr != 0 {
			return fmt.Errorf("cfapi: set in-sync state %s: HRESULT 0x%08x", localPath, uint32(hr))
		}
	default:
		hr := C.ghd_cf_set_sync_state(wPath, 0 /* CF_IN_SYNC_STATE_NOT_IN_SYNC */)
		if hr != 0 {
			return fmt.Errorf("cfapi: set not-in-sync state %s: HRESULT 0x%08x", localPath, uint32(hr))
		}
	}
	return nil
}

// ReportProgress reports hydration progress to the OS for an active FETCH_DATA
// callback.  total is the total transfer length in bytes (req.Length); done is
// the number of bytes sent so far.  This drives the ⟳ progress indicator in
// Windows Explorer while a file is being hydrated.
//
// Progress reporting is best-effort: errors are returned but should be logged
// and not treated as fatal — CfExecute(TRANSFER_DATA) is the authoritative path.
func (p *SyncProvider) ReportProgress(req FetchRequest, total, done int64) error {
	if req.opInfo == 0 {
		return nil
	}
	var totalLI, doneLI C.LARGE_INTEGER
	*(*C.LONGLONG)(unsafe.Pointer(&totalLI)) = C.LONGLONG(total)
	*(*C.LONGLONG)(unsafe.Pointer(&doneLI)) = C.LONGLONG(done)
	hr := C.ghd_cf_report_progress_cb(C.uintptr_t(req.opInfo), totalLI, doneLI)
	if hr != 0 {
		return fmt.Errorf("cfapi: report progress: HRESULT 0x%08x", uint32(hr))
	}
	return nil
}

// ExecuteTransfer sends data to Windows in response to a FETCH_DATA callback.
func (p *SyncProvider) ExecuteTransfer(req FetchRequest, data []byte, finalBlock bool) error {
	if req.opInfo == 0 {
		return fmt.Errorf("cfapi: execute transfer: nil opInfo")
	}
	// LARGE_INTEGER is a union — CGO represents it as [8]byte.
	// Set QuadPart (the 64-bit member) via unsafe pointer.
	var offset, length C.LARGE_INTEGER
	*(*C.LONGLONG)(unsafe.Pointer(&offset)) = C.LONGLONG(req.Offset)
	*(*C.LONGLONG)(unsafe.Pointer(&length)) = C.LONGLONG(len(data))

	final := C.BOOL(0)
	if finalBlock {
		final = C.BOOL(1)
	}

	var dataPtr *C.BYTE
	if len(data) > 0 {
		dataPtr = (*C.BYTE)(unsafe.Pointer(&data[0]))
	}

	hr := C.ghd_cf_execute_transfer(C.uintptr_t(req.opInfo), offset, length, dataPtr, final)
	if hr != 0 {
		return fmt.Errorf("cfapi: execute transfer: HRESULT 0x%08x", uint32(hr))
	}
	return nil
}

// ReportError reports a provider error for a pending FETCH_DATA request.
func (p *SyncProvider) ReportError(req FetchRequest, provErr error) error {
	if req.opInfo == 0 {
		return nil
	}
	hr := C.ghd_cf_report_error(C.uintptr_t(req.opInfo), C.HRESULT(-2147467259)) // E_FAIL
	if hr != 0 {
		return fmt.Errorf("cfapi: report error: HRESULT 0x%08x", uint32(hr))
	}
	return nil
}

// ---------------------------------------------------------------------------
// CF_CALLBACK_INFO helpers
// ---------------------------------------------------------------------------

// goPathFromCallbackInfo converts CF_CALLBACK_INFO.NormalizedPath (PCWSTR, UTF-16LE)
// to a Go UTF-8 string using the ghd_wchar_to_utf8 C helper.
// Direct cast (*C.char)(unsafe.Pointer(info.NormalizedPath)) only reads the first
// byte of each UTF-16 wchar — causing garbled paths and silent List failures.
func goPathFromCallbackInfo(info *C.CF_CALLBACK_INFO) string {
	if info == nil || info.NormalizedPath == nil {
		return ""
	}
	cStr := C.ghd_wchar_to_utf8(info.NormalizedPath)
	if cStr == nil {
		return ""
	}
	defer C.ghd_free_utf8(cStr)
	return C.GoString(cStr)
}

// volumePrefix extracts the drive-letter prefix (e.g. "C:") from a Windows path.
// Used to reconstruct an absolute path from a volume-relative NormalizedPath.
//   syncRoot = "C:\GhostDrive\MFS"  →  "C:"
//   syncRoot = "/mnt/ghd"           →  ""  (non-Windows / no drive letter)
func volumePrefix(syncRoot string) string {
	if len(syncRoot) >= 2 && syncRoot[1] == ':' {
		return syncRoot[:2]
	}
	return ""
}

// resolveNormalizedPath converts CF_CALLBACK_INFO.NormalizedPath (PCWSTR, UTF-16LE)
// to an absolute local path.
//
// CF API always returns volume-relative paths: NormalizedPath = "\GhostDrive\MFS"
// (no drive letter prefix).  To compare or use this path alongside the sync root
// (e.g. "C:\GhostDrive\MFS"), the volume prefix must be prepended.
//
// syncRoot is the SyncProvider.localPath ("C:\GhostDrive\MFS") — its drive
// letter is reused as the volume prefix.
func resolveNormalizedPath(info *C.CF_CALLBACK_INFO, syncRoot string) string {
	path := goPathFromCallbackInfo(info)
	// Volume-relative path: starts with single backslash, not UNC (\\server\share).
	if len(path) > 0 && path[0] == '\\' && (len(path) < 2 || path[1] != '\\') {
		path = volumePrefix(syncRoot) + path
	}
	return path
}

// ---------------------------------------------------------------------------
// Go-exported CGO callback entry points (called by C shims in cgo_cfapi.c).
// These functions look up the SyncProvider by connection key and dispatch.
// ---------------------------------------------------------------------------

//export ghdOnFetchData
func ghdOnFetchData(callbackInfoPtr uintptr, paramsPtr uintptr) {
	log.Printf("cfapi: FETCH_DATA callback ptr=0x%x", callbackInfoPtr)
	if callbackInfoPtr == 0 {
		return
	}
	info := (*C.CF_CALLBACK_INFO)(unsafe.Pointer(callbackInfoPtr))
	key := int64(info.ConnectionKey.Internal)

	providerMu.RLock()
	p, ok := providerRegistry[key]
	providerMu.RUnlock()
	if !ok || p.callbacks.OnFetchData == nil {
		log.Printf("cfapi: FETCH_DATA: no provider for key %d", key)
		return
	}

	// CRITIQUE-1: extract Offset and Length from CF_CALLBACK_PARAMETERS via C helpers
	// (CGO cannot access C union fields directly). Store callbackInfoPtr in opInfo so
	// ExecuteTransfer can build a correct CF_OPERATION_INFO for CfExecute.
	var offset, length int64
	if paramsPtr != 0 {
		params := (*C.CF_CALLBACK_PARAMETERS)(unsafe.Pointer(paramsPtr))
		offset = int64(C.ghd_fetch_data_offset(params))
		length = int64(C.ghd_fetch_data_length(params))
	}

	// NormalizedPath is PCWSTR (UTF-16LE) and volume-relative ("\GhostDrive\MFS").
	// resolveNormalizedPath prepends the drive letter from the sync root.
	localPath := resolveNormalizedPath(info, p.localPath)
	log.Printf("cfapi: FETCH_DATA: localPath=%q offset=%d length=%d", localPath, offset, length)

	req := FetchRequest{
		LocalPath: localPath,
		Offset:    offset,
		Length:    length,
		opInfo:    callbackInfoPtr, // CF_CALLBACK_INFO* — used by ghd_cf_execute_transfer
	}
	_ = p.callbacks.OnFetchData(context.Background(), req)
}

//export ghdOnCancelFetch
func ghdOnCancelFetch(callbackInfoPtr uintptr, _ uintptr) {
	if callbackInfoPtr == 0 {
		return
	}
	info := (*C.CF_CALLBACK_INFO)(unsafe.Pointer(callbackInfoPtr))
	key := int64(info.ConnectionKey.Internal)

	providerMu.RLock()
	p, ok := providerRegistry[key]
	providerMu.RUnlock()
	if !ok || p.callbacks.OnCancelFetch == nil {
		return
	}

	req := FetchRequest{
		LocalPath: resolveNormalizedPath(info, p.localPath),
	}
	p.callbacks.OnCancelFetch(req)
}

//export ghdOnFetchPlaceholders
func ghdOnFetchPlaceholders(callbackInfoPtr uintptr, _ uintptr) {
	// Entry log — confirms the OS callback is firing (helps diagnose timeout issues).
	log.Printf("cfapi: FETCH_PLACEHOLDERS callback ptr=0x%x", callbackInfoPtr)
	if callbackInfoPtr == 0 {
		return
	}
	info := (*C.CF_CALLBACK_INFO)(unsafe.Pointer(callbackInfoPtr))
	key := int64(info.ConnectionKey.Internal)

	providerMu.RLock()
	p, ok := providerRegistry[key]
	providerMu.RUnlock()
	if !ok || p.callbacks.OnFetchPlaceholders == nil {
		log.Printf("cfapi: FETCH_PLACEHOLDERS: no provider for key %d — acking with E_FAIL", key)
		// Must still ack — Windows blocks until we do.
		_ = C.ghd_cf_ack_placeholders(C.uintptr_t(callbackInfoPtr), C.HRESULT(-2147467259)) // E_FAIL
		return
	}

	// NormalizedPath is PCWSTR (UTF-16LE) and volume-relative ("\GhostDrive\MFS").
	// resolveNormalizedPath prepends the drive letter from the sync root.
	localPath := resolveNormalizedPath(info, p.localPath)
	log.Printf("cfapi: FETCH_PLACEHOLDERS: localPath=%q", localPath)

	err := p.callbacks.OnFetchPlaceholders(context.Background(), localPath)

	// BUG FIX: Windows keeps the Explorer thread blocked until the provider calls
	// CfExecute(CF_OPERATION_TYPE_TRANSFER_PLACEHOLDERS) — this is the required
	// completion signal even when placeholders were already created via
	// CfCreatePlaceholders (out-of-band).  Omitting this call causes the OS to
	// wait until its internal timeout fires (~30s), surfacing as:
	// "l'opération cloud n'a pas été terminée avant l'expiration du délai".
	status := C.HRESULT(0) // S_OK
	if err != nil {
		log.Printf("cfapi: FETCH_PLACEHOLDERS: callback error: %v", err)
		status = C.HRESULT(-2147467259) // E_FAIL
	}
	if hr := C.ghd_cf_ack_placeholders(C.uintptr_t(callbackInfoPtr), status); hr != 0 {
		log.Printf("cfapi: FETCH_PLACEHOLDERS: ack CfExecute failed: HRESULT 0x%08x", uint32(hr))
	}
}

//export ghdOnDeleteCompletion
func ghdOnDeleteCompletion(callbackInfoPtr uintptr, _ uintptr) {
	if callbackInfoPtr == 0 {
		return
	}
	info := (*C.CF_CALLBACK_INFO)(unsafe.Pointer(callbackInfoPtr))
	key := int64(info.ConnectionKey.Internal)

	providerMu.RLock()
	p, ok := providerRegistry[key]
	providerMu.RUnlock()
	if !ok || p.callbacks.OnDeleteCompletion == nil {
		return
	}

	p.callbacks.OnDeleteCompletion(resolveNormalizedPath(info, p.localPath))
}

//export ghdOnRenameCompletion
func ghdOnRenameCompletion(callbackInfoPtr uintptr, paramsPtr uintptr) {
	if callbackInfoPtr == 0 {
		return
	}
	info := (*C.CF_CALLBACK_INFO)(unsafe.Pointer(callbackInfoPtr))
	key := int64(info.ConnectionKey.Internal)

	providerMu.RLock()
	p, ok := providerRegistry[key]
	providerMu.RUnlock()
	if !ok || p.callbacks.OnRenameCompletion == nil {
		return
	}

	// NormalizedPath in CF_CALLBACK_INFO is the new (target) path after rename.
	// It is PCWSTR (UTF-16LE) and volume-relative — prepend drive letter.
	newPath := resolveNormalizedPath(info, p.localPath)

	// MAJEUR-5: extract the pre-rename source path from CF_CALLBACK_PARAMETERS.
	// Use the C helper to avoid CGO union access.
	// SourcePath is also PCWSTR and volume-relative — apply the same prefix fix.
	oldPath := newPath // fallback if params unavailable
	if paramsPtr != 0 {
		params := (*C.CF_CALLBACK_PARAMETERS)(unsafe.Pointer(paramsPtr))
		if cSrc := C.ghd_rename_source_path(params); cSrc != nil {
			src := C.GoString(cSrc)
			C.ghd_free_utf8(cSrc)
			// Prepend volume prefix if SourcePath is also volume-relative.
			if len(src) > 0 && src[0] == '\\' && (len(src) < 2 || src[1] != '\\') {
				src = volumePrefix(p.localPath) + src
			}
			oldPath = src
		}
	}

	p.callbacks.OnRenameCompletion(oldPath, newPath)
}

// ---------------------------------------------------------------------------
// Time conversion helpers
// ---------------------------------------------------------------------------

// timeToFileTime converts a Go time.Time to a Windows FILETIME represented as
// a LARGE_INTEGER (100-nanosecond intervals since 1601-01-01 00:00:00 UTC).
// CF_FS_METADATA embeds FILE_BASIC_INFO whose timestamp fields are LARGE_INTEGER;
// LARGE_INTEGER is a union and CGO exposes it as [8]byte, so we set the value
// via an unsafe QuadPart write rather than field access.
func timeToFileTime(t time.Time) C.LARGE_INTEGER {
	// 116444736000000000 = number of 100-ns intervals between 1601-01-01 and 1970-01-01.
	const windowsEpochDelta = int64(116444736000000000)
	ns100 := t.UnixNano()/100 + windowsEpochDelta
	var li C.LARGE_INTEGER
	*(*C.LONGLONG)(unsafe.Pointer(&li)) = C.LONGLONG(ns100)
	return li
}
