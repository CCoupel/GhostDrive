// cgo_cfapi.c — Implementation of the Windows Cloud Filter API C wrappers.
// Only compiled on Windows (via provider.go build tag //go:build windows).

#ifdef _WIN32

#include "cgo_cfapi_windows.h"
#include <stdlib.h>
#include <objbase.h>  /* CLSIDFromString */
#include <shlobj.h>   /* SHChangeNotify */

// ---------------------------------------------------------------------------
// UTF-8 helpers
// ---------------------------------------------------------------------------

LPWSTR ghd_utf8_to_wchar(const char* utf8) {
    if (!utf8) return NULL;
    int len = MultiByteToWideChar(CP_UTF8, 0, utf8, -1, NULL, 0);
    if (len <= 0) return NULL;
    LPWSTR wstr = (LPWSTR)malloc(len * sizeof(WCHAR));
    if (!wstr) return NULL;
    MultiByteToWideChar(CP_UTF8, 0, utf8, -1, wstr, len);
    return wstr;
}

void ghd_free_wchar(LPWSTR wstr) {
    free(wstr);
}

// ghd_wchar_to_utf8 converts a NUL-terminated wide string (LPCWSTR / PCWSTR)
// to a heap-allocated UTF-8 string.  Caller must free with ghd_free_utf8.
// Used to convert CF_CALLBACK_INFO.NormalizedPath (PCWSTR) to Go strings —
// casting PCWSTR directly to char* only reads the first byte of each wchar.
char* ghd_wchar_to_utf8(LPCWSTR wstr) {
    if (!wstr) return NULL;
    int len = WideCharToMultiByte(CP_UTF8, 0, wstr, -1, NULL, 0, NULL, NULL);
    if (len <= 0) return NULL;
    char* utf8 = (char*)malloc(len);
    if (!utf8) return NULL;
    WideCharToMultiByte(CP_UTF8, 0, wstr, -1, utf8, len, NULL, NULL);
    return utf8;
}

// ---------------------------------------------------------------------------
// Sync root registration
// ---------------------------------------------------------------------------

HRESULT ghd_cf_register(LPCWSTR syncRootPath, LPCWSTR providerID, LPCWSTR displayName) {
    // Build the CF_SYNC_REGISTRATION structure.
    // In SDK 10.0.17763+ ProviderName and ProviderVersion are LPCWSTR pointers,
    // not fixed-size arrays — assign directly (do not use wcsncpy_s).
    CF_SYNC_REGISTRATION reg = {0};
    reg.StructSize      = sizeof(reg);
    reg.ProviderName    = displayName;  // points to caller-owned wide string
    reg.ProviderVersion = L"2.1";

    // SyncRootIdentityBlob must be non-NULL (≥1 byte) so Windows can distinguish
    // this sync root from a plain offline-files root.  Without it, Explorer shows
    // a briefcase icon instead of the cloud overlay (☁️).
    static const BYTE ghd_sync_root_identity[1] = {0x01};
    reg.SyncRootIdentity       = ghd_sync_root_identity;
    reg.SyncRootIdentityLength = 1;

    // StorageProviderID must be a GUID — parse from string.
    HRESULT hr = CLSIDFromString(providerID, &reg.ProviderId);
    if (FAILED(hr)) return hr;

    CF_SYNC_POLICIES policies = {0};
    policies.StructSize = sizeof(policies);
    policies.Hydration.Primary = CF_HYDRATION_POLICY_PROGRESSIVE;
    policies.Hydration.Modifier = CF_HYDRATION_POLICY_MODIFIER_NONE;
    policies.Population.Primary = CF_POPULATION_POLICY_PARTIAL;
    policies.Population.Modifier = CF_POPULATION_POLICY_MODIFIER_NONE;
    policies.InSync = CF_INSYNC_POLICY_TRACK_FILE_CREATION_TIME |
                      CF_INSYNC_POLICY_TRACK_FILE_LAST_WRITE_TIME;
    policies.HardLink = CF_HARDLINK_POLICY_NONE;
    policies.PlaceholderManagement = CF_PLACEHOLDER_MANAGEMENT_POLICY_DEFAULT;

    return CfRegisterSyncRoot(syncRootPath, &reg, &policies,
                              CF_REGISTER_FLAG_NONE);
}

HRESULT ghd_cf_deregister(LPCWSTR syncRootPath) {
    return CfUnregisterSyncRoot(syncRootPath);
}

// ---------------------------------------------------------------------------
// Go-side callbacks (exported by provider.go via //export)
// These declarations allow the C callbacks below to call back into Go.
// ---------------------------------------------------------------------------

// Forward-declare the Go-exported callback functions.
extern void ghdOnFetchData(uintptr_t callbackInfoPtr, uintptr_t opInfoPtr);
extern void ghdOnCancelFetch(uintptr_t callbackInfoPtr, uintptr_t opInfoPtr);
extern void ghdOnFetchPlaceholders(uintptr_t callbackInfoPtr, uintptr_t opInfoPtr);
extern void ghdOnDeleteCompletion(uintptr_t callbackInfoPtr, uintptr_t opInfoPtr);
extern void ghdOnRenameCompletion(uintptr_t callbackInfoPtr, uintptr_t opInfoPtr);

// C callback shims — invoked by CF API, bridge to Go.
static void CALLBACK ghd_cb_fetch_data(
        const CF_CALLBACK_INFO* info,
        const CF_CALLBACK_PARAMETERS* params) {
    (void)params;
    ghdOnFetchData((uintptr_t)info, (uintptr_t)params);
}

static void CALLBACK ghd_cb_cancel_fetch(
        const CF_CALLBACK_INFO* info,
        const CF_CALLBACK_PARAMETERS* params) {
    (void)params;
    ghdOnCancelFetch((uintptr_t)info, (uintptr_t)params);
}

static void CALLBACK ghd_cb_fetch_placeholders(
        const CF_CALLBACK_INFO* info,
        const CF_CALLBACK_PARAMETERS* params) {
    (void)params;
    ghdOnFetchPlaceholders((uintptr_t)info, (uintptr_t)params);
}

static void CALLBACK ghd_cb_delete_completion(
        const CF_CALLBACK_INFO* info,
        const CF_CALLBACK_PARAMETERS* params) {
    (void)params;
    ghdOnDeleteCompletion((uintptr_t)info, (uintptr_t)params);
}

static void CALLBACK ghd_cb_rename_completion(
        const CF_CALLBACK_INFO* info,
        const CF_CALLBACK_PARAMETERS* params) {
    (void)params;
    ghdOnRenameCompletion((uintptr_t)info, (uintptr_t)params);
}

// ---------------------------------------------------------------------------
// Sync root connection
// ---------------------------------------------------------------------------

HRESULT ghd_cf_connect(
        LPCWSTR localPath,
        CF_CALLBACK_REGISTRATION* callbacks,
        ghd_connection_key_t* outKey) {

    // Register the static C callback shims.
    CF_CALLBACK_REGISTRATION cbs[] = {
        { CF_CALLBACK_TYPE_FETCH_DATA,         ghd_cb_fetch_data         },
        { CF_CALLBACK_TYPE_CANCEL_FETCH_DATA,  ghd_cb_cancel_fetch       },
        { CF_CALLBACK_TYPE_FETCH_PLACEHOLDERS, ghd_cb_fetch_placeholders },
        { CF_CALLBACK_TYPE_NOTIFY_DELETE_COMPLETION, ghd_cb_delete_completion },
        { CF_CALLBACK_TYPE_NOTIFY_RENAME_COMPLETION, ghd_cb_rename_completion },
        CF_CALLBACK_REGISTRATION_END
    };

    CF_CONNECTION_KEY key;
    HRESULT hr = CfConnectSyncRoot(localPath, cbs, NULL,
                                   CF_CONNECT_FLAG_REQUIRE_PROCESS_INFO |
                                   CF_CONNECT_FLAG_REQUIRE_FULL_FILE_PATH,
                                   &key);
    if (SUCCEEDED(hr) && outKey) {
        *outKey = key.Internal;
    }
    return hr;
}

HRESULT ghd_cf_disconnect(ghd_connection_key_t key) {
    CF_CONNECTION_KEY k;
    k.Internal = key;
    return CfDisconnectSyncRoot(k);
}

// ---------------------------------------------------------------------------
// Placeholder creation
// ---------------------------------------------------------------------------

HRESULT ghd_cf_create_placeholders(
        LPCWSTR localPath,
        CF_PLACEHOLDER_CREATE_INFO* items,
        DWORD count,
        DWORD* outCreated) {
    DWORD created = 0;
    HRESULT hr = CfCreatePlaceholders(localPath, items, count,
                                      CF_CREATE_FLAG_NONE,
                                      &created);
    if (outCreated) *outCreated = created;
    return hr;
}

// ---------------------------------------------------------------------------
// Sync / pin state
// ---------------------------------------------------------------------------

// CF_IN_SYNC_STATE values: 0 = NOT_IN_SYNC, 1 = IN_SYNC.
HRESULT ghd_cf_set_sync_state(LPCWSTR localPath, int state) {
    HANDLE hFile = CreateFileW(
        localPath,
        FILE_WRITE_ATTRIBUTES,
        FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
        NULL,
        OPEN_EXISTING,
        FILE_FLAG_BACKUP_SEMANTICS,
        NULL
    );
    if (hFile == INVALID_HANDLE_VALUE) {
        return HRESULT_FROM_WIN32(GetLastError());
    }
    CF_IN_SYNC_STATE cfState = (state == 0) ?
        CF_IN_SYNC_STATE_NOT_IN_SYNC : CF_IN_SYNC_STATE_IN_SYNC;
    HRESULT hr = CfSetInSyncState(hFile, cfState, CF_SET_IN_SYNC_FLAG_NONE, NULL);
    CloseHandle(hFile);
    return hr;
}

// CF_PIN_STATE values: 0 = UNSPECIFIED, 1 = PINNED, 2 = UNPINNED,
//                      3 = EXCLUDED, 4 = INHERIT.
HRESULT ghd_cf_set_pin_state(LPCWSTR localPath, int state) {
    HANDLE hFile = CreateFileW(
        localPath,
        FILE_WRITE_ATTRIBUTES,
        FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
        NULL,
        OPEN_EXISTING,
        FILE_FLAG_BACKUP_SEMANTICS,
        NULL
    );
    if (hFile == INVALID_HANDLE_VALUE) {
        return HRESULT_FROM_WIN32(GetLastError());
    }
    CF_PIN_STATE cfState = (CF_PIN_STATE)state;
    HRESULT hr = CfSetPinState(hFile, cfState, CF_SET_PIN_FLAG_NONE, NULL);
    CloseHandle(hFile);
    return hr;
}

// ---------------------------------------------------------------------------
// Data transfer
// ---------------------------------------------------------------------------

// Build a CF_OPERATION_INFO from the CF_CALLBACK_INFO supplied during the callback.
// This is the correct approach: CfExecute needs ConnectionKey + TransferKey from the
// callback info, not from CF_CALLBACK_PARAMETERS (which caused the UB cast in v2.1).
static CF_OPERATION_INFO ghd_build_op_info(const CF_CALLBACK_INFO* info,
                                            CF_OPERATION_TYPE type) {
    CF_OPERATION_INFO opInfo = {0};
    opInfo.StructSize    = sizeof(CF_OPERATION_INFO);
    opInfo.Type          = type;
    opInfo.ConnectionKey = info->ConnectionKey;
    opInfo.TransferKey   = info->TransferKey;
    opInfo.RequestKey    = info->RequestKey;
    return opInfo;
}

HRESULT ghd_cf_execute_transfer(
        uintptr_t callbackInfoPtr,
        LARGE_INTEGER offset,
        LARGE_INTEGER length,
        const BYTE* data,
        BOOL isFinal) {
    if (!callbackInfoPtr) return E_INVALIDARG;
    const CF_CALLBACK_INFO* info = (const CF_CALLBACK_INFO*)callbackInfoPtr;

    CF_OPERATION_INFO opInfo = ghd_build_op_info(info, CF_OPERATION_TYPE_TRANSFER_DATA);

    CF_OPERATION_PARAMETERS params = {0};
    params.ParamSize = CF_SIZE_OF_OP_PARAM(TransferData);
    params.TransferData.Flags = CF_OPERATION_TRANSFER_DATA_FLAG_NONE;
    params.TransferData.CompletionStatus = S_OK;
    params.TransferData.Buffer = data;
    params.TransferData.Offset = offset;
    params.TransferData.Length = length;

    return CfExecute(&opInfo, &params);
}

HRESULT ghd_cf_report_error(uintptr_t callbackInfoPtr, HRESULT providerError) {
    if (!callbackInfoPtr) return E_INVALIDARG;
    const CF_CALLBACK_INFO* info = (const CF_CALLBACK_INFO*)callbackInfoPtr;

    CF_OPERATION_INFO opInfo = ghd_build_op_info(info, CF_OPERATION_TYPE_TRANSFER_DATA);

    CF_OPERATION_PARAMETERS params = {0};
    params.ParamSize = CF_SIZE_OF_OP_PARAM(TransferData);
    params.TransferData.Flags = CF_OPERATION_TRANSFER_DATA_FLAG_NONE;
    params.TransferData.CompletionStatus = providerError;
    params.TransferData.Buffer = NULL;
    LARGE_INTEGER zero = {0};
    params.TransferData.Offset = zero;
    params.TransferData.Length = zero;

    return CfExecute(&opInfo, &params);
}

// ---------------------------------------------------------------------------
// FETCH_PLACEHOLDERS completion acknowledgement
// ---------------------------------------------------------------------------

// ghd_cf_ack_placeholders signals completion of a CF_CALLBACK_TYPE_FETCH_PLACEHOLDERS
// callback via CfExecute(CF_OPERATION_TYPE_TRANSFER_PLACEHOLDERS).
//
// Windows waits for this acknowledgement after calling the FETCH_PLACEHOLDERS callback.
// If the provider already created placeholders via CfCreatePlaceholders (out-of-band),
// it must still call this function with PlaceholderCount=0 and completionStatus=S_OK
// to release the pending OS operation — otherwise the Explorer window will time out.
HRESULT ghd_cf_ack_placeholders(uintptr_t callbackInfoPtr, HRESULT completionStatus) {
    if (!callbackInfoPtr) return E_INVALIDARG;
    const CF_CALLBACK_INFO* info = (const CF_CALLBACK_INFO*)callbackInfoPtr;

    CF_OPERATION_INFO opInfo = ghd_build_op_info(info,
                                                 CF_OPERATION_TYPE_TRANSFER_PLACEHOLDERS);

    CF_OPERATION_PARAMETERS params = {0};
    params.ParamSize = CF_SIZE_OF_OP_PARAM(TransferPlaceholders);
    params.TransferPlaceholders.Flags             = CF_OPERATION_TRANSFER_PLACEHOLDERS_FLAG_NONE;
    params.TransferPlaceholders.CompletionStatus  = completionStatus;
    params.TransferPlaceholders.PlaceholderArray  = NULL;
    params.TransferPlaceholders.PlaceholderCount  = 0;
    /* PlaceholderTotalCount left zero-initialized — signals "population complete" */

    return CfExecute(&opInfo, &params);
}

// ---------------------------------------------------------------------------
// CF_CALLBACK_PARAMETERS field accessors (called from Go to avoid CGO union access)
// ---------------------------------------------------------------------------

__int64 ghd_fetch_data_offset(const CF_CALLBACK_PARAMETERS* params) {
    if (!params) return 0;
    return params->FetchData.RequiredFileOffset.QuadPart;
}

__int64 ghd_fetch_data_length(const CF_CALLBACK_PARAMETERS* params) {
    if (!params) return 0;
    return params->FetchData.RequiredLength.QuadPart;
}

char* ghd_rename_source_path(const CF_CALLBACK_PARAMETERS* params) {
    if (!params || !params->RenameCompletion.SourcePath) return NULL;
    LPCWSTR src = params->RenameCompletion.SourcePath;
    int len = WideCharToMultiByte(CP_UTF8, 0, src, -1, NULL, 0, NULL, NULL);
    if (len <= 0) return NULL;
    char* utf8 = (char*)malloc(len);
    if (!utf8) return NULL;
    WideCharToMultiByte(CP_UTF8, 0, src, -1, utf8, len, NULL, NULL);
    return utf8;
}

void ghd_free_utf8(char* str) {
    free(str);
}

// ---------------------------------------------------------------------------
// Placeholder conversion
// ---------------------------------------------------------------------------

// ghd_convert_to_placeholder_flags is the internal helper shared by the two
// public converters below.  It opens the file/directory, calls
// CfConvertToPlaceholder with the given flags, and closes the handle.
// FILE_FLAG_BACKUP_SEMANTICS is required to open a directory handle and is
// harmless for regular files.
static HRESULT ghd_convert_to_placeholder_flags(LPCWSTR wPath, CF_CONVERT_FLAGS flags) {
    if (!wPath) return E_INVALIDARG;

    HANDLE hFile = CreateFileW(
        wPath,
        FILE_READ_ATTRIBUTES | FILE_WRITE_ATTRIBUTES,
        FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
        NULL,
        OPEN_EXISTING,
        FILE_FLAG_BACKUP_SEMANTICS,  /* required for directory handles */
        NULL
    );
    if (hFile == INVALID_HANDLE_VALUE) return HRESULT_FROM_WIN32(GetLastError());

    HRESULT hr = CfConvertToPlaceholder(
        hFile,
        NULL, 0,  /* FileIdentity / FileIdentityLength — not needed for conversion */
        flags,
        NULL,     /* ConvertUsn — not needed */
        NULL      /* Overlapped — synchronous */
    );
    CloseHandle(hFile);
    return hr;
}

// ghd_convert_to_placeholder converts an existing local **file** into a CF
// placeholder with CF_CONVERT_FLAG_MARK_IN_SYNC.
//
// MARK_IN_SYNC marks the file as "locally present and in sync with the remote" →
// Explorer shows badge ✓✓.  Appropriate for files because the local copy IS the
// definitive version; no further FETCH_DATA round-trip is expected.
HRESULT ghd_convert_to_placeholder(LPCWSTR wPath) {
    return ghd_convert_to_placeholder_flags(wPath, CF_CONVERT_FLAG_MARK_IN_SYNC);
}

// ghd_convert_dir_to_placeholder converts an existing local **directory** into a
// CF placeholder WITHOUT CF_CONVERT_FLAG_MARK_IN_SYNC.
//
// Rationale: MARK_IN_SYNC on a directory marks its population as "complete" —
// the OS caches this state and never calls OnFetchPlaceholders again after a
// restart, so only local content is visible (regression).
// Without MARK_IN_SYNC the directory stays in "partial" state: the OS calls
// OnFetchPlaceholders every time the user opens it, merging remote content.
// CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION keeps on-demand population active.
HRESULT ghd_convert_dir_to_placeholder(LPCWSTR wPath) {
    return ghd_convert_to_placeholder_flags(wPath,
        CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION);
}

HRESULT ghd_cf_report_progress(
        ghd_connection_key_t key,
        __int64 transferKey,
        LARGE_INTEGER total,
        LARGE_INTEGER done) {
    CF_CONNECTION_KEY k;
    k.Internal = key;
    CF_TRANSFER_KEY tk;
    tk.QuadPart = transferKey;
    return CfReportProviderProgress(k, tk, total, done);
}

// ghd_cf_report_progress_cb is a convenience wrapper that extracts ConnectionKey
// and TransferKey directly from CF_CALLBACK_INFO* (passed as uintptr_t).
// This avoids CGO union access issues when extracting LARGE_INTEGER fields in Go.
HRESULT ghd_cf_report_progress_cb(
        uintptr_t callbackInfoPtr,
        LARGE_INTEGER total,
        LARGE_INTEGER done) {
    if (!callbackInfoPtr) return E_INVALIDARG;
    const CF_CALLBACK_INFO* info = (const CF_CALLBACK_INFO*)callbackInfoPtr;
    return ghd_cf_report_progress(
        info->ConnectionKey.Internal,
        info->TransferKey.QuadPart,
        total,
        done
    );
}

// ghd_notify_icon_refresh fires two SHChangeNotify calls to tell Explorer to
// reload cloud overlay icons immediately after a StorageProvider registration.
//
// 1. SHCNE_UPDATEDIR on the sync root path — refreshes the folder's attributes
//    so Explorer re-reads the CF placeholder attributes for all children.
// 2. SHCNE_ASSOCCHANGED (global) — tells Explorer that shell extension
//    associations changed and cloudfilesshell.dll should re-register overlays.
//
// syncRootPath: absolute path of the sync root (wide string), may be NULL.
void ghd_notify_icon_refresh(LPCWSTR syncRootPath) {
    if (syncRootPath) {
        SHChangeNotify(SHCNE_UPDATEDIR, SHCNF_PATH | SHCNF_FLUSH,
                       (LPCVOID)syncRootPath, NULL);
    }
    SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_IDLIST | SHCNF_FLUSH, NULL, NULL);
}

#endif // _WIN32
