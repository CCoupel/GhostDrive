// cgo_cfapi.h — C wrappers for the Windows Cloud Filter API (cfapi.h).
// Only compiled on Windows (build tag enforced by provider.go).
//
// Link: -lcldapi (MinGW) or cldapi.lib (MSVC).
// Minimum SDK: Windows 10 1809 (10.0.17763.0).

#ifndef GHD_CFAPI_H
#define GHD_CFAPI_H

#ifdef _WIN32

/* Use the in-tree MinGW-compatible cfapi.h (Option B from build-fix handoff).
 * The SDK's <cfapi.h> transitively includes <vcruntime.h> via <winapifamily.h>,
 * which is MSVC-only and breaks MinGW CGO builds.
 * ${SRCDIR}/include is added to CGO_CFLAGS in provider.go. */
#include "cfapi.h"

/* cldapi.lib / -lcldapi is specified via CGO_LDFLAGS in provider.go.
 * The #pragma comment below is MSVC-only; remove it to avoid MinGW warnings. */

// ---------------------------------------------------------------------------
// Opaque handle types passed to Go as uintptr_t.
// ---------------------------------------------------------------------------

// ghd_connection_key_t wraps CF_CONNECTION_KEY (LARGE_INTEGER) as an int64.
typedef __int64 ghd_connection_key_t;

// ---------------------------------------------------------------------------
// Sync root registration / deregistration.
// ---------------------------------------------------------------------------

// ghd_cf_register registers a sync root at syncRootPath.
// providerID must be a GUID string like "{xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}".
// displayName is the human-readable provider name shown in Explorer.
// Returns HRESULT.
HRESULT ghd_cf_register(LPCWSTR syncRootPath, LPCWSTR providerID, LPCWSTR displayName);

// ghd_cf_deregister removes the sync root registration.
// Returns HRESULT.
HRESULT ghd_cf_deregister(LPCWSTR syncRootPath);

// ---------------------------------------------------------------------------
// Sync root connection / disconnection.
// ---------------------------------------------------------------------------

// ghd_cf_connect connects to the sync root at localPath with the provided
// callback registration array (CF_CALLBACK_REGISTRATION[]).
// On success, writes the connection key into *outKey.
// Returns HRESULT.
HRESULT ghd_cf_connect(
    LPCWSTR localPath,
    CF_CALLBACK_REGISTRATION* callbacks,
    ghd_connection_key_t* outKey
);

// ghd_cf_disconnect disconnects the given connection key.
// Returns HRESULT.
HRESULT ghd_cf_disconnect(ghd_connection_key_t key);

// ---------------------------------------------------------------------------
// Placeholder management.
// ---------------------------------------------------------------------------

// ghd_cf_create_placeholders creates count placeholder entries under localPath.
// On success, *outCreated contains the number of placeholders successfully created.
// Returns HRESULT of the batch operation.
HRESULT ghd_cf_create_placeholders(
    LPCWSTR localPath,
    CF_PLACEHOLDER_CREATE_INFO* items,
    DWORD count,
    DWORD* outCreated
);

// ---------------------------------------------------------------------------
// Sync state / pin state.
// ---------------------------------------------------------------------------

// ghd_cf_set_sync_state sets the in-sync state of localPath.
// state: 0 = CF_IN_SYNC_STATE_NOT_IN_SYNC, 1 = CF_IN_SYNC_STATE_IN_SYNC.
// Returns HRESULT.
HRESULT ghd_cf_set_sync_state(LPCWSTR localPath, int state);

// ghd_cf_set_pin_state sets the pin state of localPath.
// state: 0 = CF_PIN_STATE_UNSPECIFIED, 1 = CF_PIN_STATE_PINNED,
//        2 = CF_PIN_STATE_UNPINNED, 3 = CF_PIN_STATE_EXCLUDED,
//        4 = CF_PIN_STATE_INHERIT.
// Returns HRESULT.
HRESULT ghd_cf_set_pin_state(LPCWSTR localPath, int state);

// ---------------------------------------------------------------------------
// Data transfer (FETCH_DATA callback response).
// ---------------------------------------------------------------------------

// ghd_cf_execute_transfer sends data to Windows in response to a FETCH_DATA
// callback.  callbackInfoPtr is the CF_CALLBACK_INFO* cast to uintptr_t;
// it is used to build the CF_OPERATION_INFO internally (ConnectionKey + TransferKey).
// offset and length are the byte range this data covers.
// isFinal must be non-zero for the last (or only) transfer block.
// Returns HRESULT.
HRESULT ghd_cf_execute_transfer(
    uintptr_t callbackInfoPtr,
    LARGE_INTEGER offset,
    LARGE_INTEGER length,
    const BYTE* data,
    BOOL isFinal
);

// ghd_cf_report_error reports a provider error for a FETCH_DATA request.
// callbackInfoPtr is the CF_CALLBACK_INFO* cast to uintptr_t.
// Returns HRESULT.
HRESULT ghd_cf_report_error(uintptr_t callbackInfoPtr, HRESULT providerError);

// ---------------------------------------------------------------------------
// CF_CALLBACK_PARAMETERS field accessors (avoid CGO union access).
// ---------------------------------------------------------------------------

// ghd_fetch_data_offset returns RequiredFileOffset.QuadPart from a
// CF_CALLBACK_PARAMETERS* for a FETCH_DATA callback.
__int64 ghd_fetch_data_offset(const CF_CALLBACK_PARAMETERS* params);

// ghd_fetch_data_length returns RequiredLength.QuadPart from a
// CF_CALLBACK_PARAMETERS* for a FETCH_DATA callback.
__int64 ghd_fetch_data_length(const CF_CALLBACK_PARAMETERS* params);

// ghd_rename_source_path returns the pre-rename source path (UTF-8, heap-allocated)
// from a CF_CALLBACK_PARAMETERS* for a NOTIFY_RENAME_COMPLETION callback.
// Caller must free the returned string with ghd_free_utf8.
char* ghd_rename_source_path(const CF_CALLBACK_PARAMETERS* params);

// ghd_free_utf8 frees a UTF-8 string returned by ghd_rename_source_path or
// ghd_wchar_to_utf8.
void ghd_free_utf8(char* str);

// ghd_wchar_to_utf8 converts a NUL-terminated LPCWSTR (UTF-16LE) to a
// heap-allocated UTF-8 string.  Caller must free with ghd_free_utf8.
// Use this to convert CF_CALLBACK_INFO.NormalizedPath to a Go string;
// casting PCWSTR to char* only reads the first byte of each wchar.
char* ghd_wchar_to_utf8(LPCWSTR wstr);

// ghd_cf_ack_placeholders signals completion of a FETCH_PLACEHOLDERS callback
// via CfExecute(CF_OPERATION_TYPE_TRANSFER_PLACEHOLDERS, PlaceholderCount=0).
// Windows blocks the Explorer thread until this is called — always invoke it
// after OnFetchPlaceholders returns, even when placeholders were already created
// out-of-band via CfCreatePlaceholders.
// completionStatus: S_OK (0) on success, E_FAIL on error.
// Returns HRESULT.
HRESULT ghd_cf_ack_placeholders(uintptr_t callbackInfoPtr, HRESULT completionStatus);

// ghd_cf_report_progress reports hydration progress for a pending transfer.
// transferKey is the CF_TRANSFER_KEY.QuadPart from the FETCH_DATA callback.
// Returns HRESULT.
HRESULT ghd_cf_report_progress(
    ghd_connection_key_t key,
    __int64 transferKey,
    LARGE_INTEGER total,
    LARGE_INTEGER done
);

// ghd_cf_report_progress_cb reports hydration progress from inside a FETCH_DATA
// callback.  callbackInfoPtr is the CF_CALLBACK_INFO* cast to uintptr_t;
// ConnectionKey and TransferKey are extracted internally.
// total: file size (or transfer length) in bytes; done: bytes transferred so far.
// Drives the ⟳ progress indicator in Windows Explorer.
// Returns HRESULT.
HRESULT ghd_cf_report_progress_cb(
    uintptr_t callbackInfoPtr,
    LARGE_INTEGER total,
    LARGE_INTEGER done
);

// ---------------------------------------------------------------------------
// UTF-8 helpers (Go strings → WCHAR).
// ---------------------------------------------------------------------------

// ghd_utf8_to_wchar converts a UTF-8 string to a heap-allocated WCHAR string.
// Caller must free with ghd_free_wchar.
LPWSTR ghd_utf8_to_wchar(const char* utf8);

// ghd_free_wchar frees a WCHAR string allocated by ghd_utf8_to_wchar.
void ghd_free_wchar(LPWSTR wstr);

// ---------------------------------------------------------------------------
// Placeholder conversion
// ---------------------------------------------------------------------------

// ghd_convert_to_placeholder converts an existing local **file** into a CF
// placeholder.  Applies CF_CONVERT_FLAG_MARK_IN_SYNC → badge ✓✓ (locally
// present and in sync with remote).  Use for files only.
// Returns HRESULT (S_OK on success, error otherwise).
HRESULT ghd_convert_to_placeholder(LPCWSTR wPath);

// ghd_convert_dir_to_placeholder converts an existing local **directory** into a
// CF placeholder WITHOUT CF_CONVERT_FLAG_MARK_IN_SYNC.
//
// Using MARK_IN_SYNC on a directory marks its population as "complete" — the OS
// never calls OnFetchPlaceholders again after a restart (regression: only local
// content visible).  Without MARK_IN_SYNC the directory stays in "partial" state:
// OnFetchPlaceholders is called on every open, merging remote content each time.
// CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION keeps on-demand population active.
// Returns HRESULT (S_OK on success, error otherwise).
HRESULT ghd_convert_dir_to_placeholder(LPCWSTR wPath);

// ---------------------------------------------------------------------------
// WinRT StorageProviderSyncRootManager::Register (COM C ABI)
// ---------------------------------------------------------------------------

// ghd_notify_icon_refresh fires two SHChangeNotify events so Explorer reloads
// cloud overlay icons immediately after a StorageProvider registration:
//   1. SHCNE_UPDATEDIR on syncRootPath (may be NULL) — refreshes folder attributes
//   2. SHCNE_ASSOCCHANGED (global) — reloads cloudfilesshell.dll overlay handler
// Call after any StorageProvider registration (WinRT or registry fallback).
void ghd_notify_icon_refresh(LPCWSTR syncRootPath);

// ghd_register_storage_provider_winrt calls WinRT
// Windows.Storage.Provider.StorageProviderSyncRootManager::Register via the
// COM C ABI (vtable calls, no C++/WinRT required).
//
// This is required for Windows Explorer to recognise GhostDrive as a cloud
// storage provider and display cloud overlay icons (☁️ ✓✓ ⟳).  Without this
// call, Explorer shows only a generic offline icon even when the CF API is
// correctly registered and FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS is set.
//
// id           — sync root ID: L"GhostDrive!<UserSID>!<BackendName>"
// displayName  — human-readable name, e.g. L"GhostDrive — MFS"
// syncRootPath — absolute path of the sync root, e.g. L"C:\\GhostDrive\\MFS"
//
// Returns S_OK (0) on success.  On failure returns a non-zero HRESULT;
// the Go caller then falls back to registry-based registration.
//
// Requires: -lruntimeobject (WinRT COM), -lshell32 (SHChangeNotify)
HRESULT ghd_register_storage_provider_winrt(
    const wchar_t *id,
    const wchar_t *displayName,
    const wchar_t *syncRootPath
);

#endif // _WIN32
#endif // GHD_CFAPI_H
