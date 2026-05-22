/*
 * storage_provider_winrt_windows.c
 *
 * Implements WinRT StorageProviderSyncRootManager::Register via COM C ABI.
 *
 * Windows overlay icons (☁️ ✓✓ ⟳) require the sync root to be registered
 * as a Windows Storage Provider.  The WinRT API
 *   Windows.Storage.Provider.StorageProviderSyncRootManager::Register
 * activates cloudfilesshell.dll's overlay handler for the registered path.
 *
 * This file implements the equivalent call using the raw COM ABI:
 *  · IUnknown / IInspectable vtable calls via function-pointer casts
 *  · RoInitialize / RoActivateInstance / RoGetActivationFactory from
 *    runtimeobject.dll (linked via -lruntimeobject)
 *  · SHChangeNotify from shell32.dll (linked via -lshell32) to flush Explorer
 *
 * Interface IIDs are from the Windows 10 SDK .winmd metadata.
 * Vtable method indices follow the WinRT COM ABI convention:
 *   IUnknown     [0..2]:  QueryInterface, AddRef, Release
 *   IInspectable [3..5]:  GetIids, GetRuntimeClassName, GetTrustLevel
 *   Interface methods from index 6 onward.
 *
 * If any step fails (wrong GUID, missing WinRT, older Windows), the function
 * returns a non-zero HRESULT and the Go caller falls back to registry writes.
 */

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <roapi.h>
#include <winstring.h>
#include <inspectable.h>
#include <asyncinfo.h>
#include <shlobj.h>
#include <stdlib.h>
#include <string.h>

/* ── COM vtable helper macros ────────────────────────────────────────────────
 *
 * Calling COM/WinRT methods in C:
 *   The vtable is a contiguous array of function pointers.
 *   *(void**)iface  →  pointer to vtable array
 *   ((FnType*)(*(void**)iface))[idx](iface, args...)
 *
 * We define typed helpers per call signature to avoid varargs UB.
 */

static inline ULONG _com_addref(void *iface) {
    typedef ULONG (STDMETHODCALLTYPE *FN)(void*);
    return ((FN *)*(void **)iface)[1](iface);
}
static inline void _com_release(void *iface) {
    if (!iface) return;
    typedef ULONG (STDMETHODCALLTYPE *FN)(void*);
    ((FN *)*(void **)iface)[2](iface);
}
static inline HRESULT _com_qi(void *iface, const GUID *riid, void **ppv) {
    typedef HRESULT (STDMETHODCALLTYPE *FN)(void*, const GUID*, void**);
    return ((FN *)*(void **)iface)[0](iface, riid, ppv);
}

/* Call vtable[idx](iface, HSTRING) */
static inline HRESULT _com_put_hs(void *iface, int idx, HSTRING v) {
    typedef HRESULT (STDMETHODCALLTYPE *FN)(void*, HSTRING);
    return ((FN *)*(void **)iface)[idx](iface, v);
}
/* Call vtable[idx](iface, INT32) */
static inline HRESULT _com_put_i32(void *iface, int idx, INT32 v) {
    typedef HRESULT (STDMETHODCALLTYPE *FN)(void*, INT32);
    return ((FN *)*(void **)iface)[idx](iface, v);
}
/* Call vtable[idx](iface, BOOL) — same layout as INT32 on x64 */
static inline HRESULT _com_put_bool(void *iface, int idx, BOOL v) {
    typedef HRESULT (STDMETHODCALLTYPE *FN)(void*, BOOL);
    return ((FN *)*(void **)iface)[idx](iface, v);
}
/* Call vtable[idx](iface, void*) */
static inline HRESULT _com_put_ptr(void *iface, int idx, void *v) {
    typedef HRESULT (STDMETHODCALLTYPE *FN)(void*, void*);
    return ((FN *)*(void **)iface)[idx](iface, v);
}
/* Call vtable[idx](iface, HSTRING, void**) */
static inline HRESULT _com_hs_ppv(void *iface, int idx, HSTRING h, void **ppv) {
    typedef HRESULT (STDMETHODCALLTYPE *FN)(void*, HSTRING, void**);
    return ((FN *)*(void **)iface)[idx](iface, h, ppv);
}
/* IAsyncInfo::get_Status — vtable[7] */
static inline HRESULT _asyncinfo_status(void *pAsyncInfo, AsyncStatus *out) {
    typedef HRESULT (STDMETHODCALLTYPE *FN)(void*, AsyncStatus*);
    return ((FN *)*(void **)pAsyncInfo)[7](pAsyncInfo, out);
}
/* IAsyncOperation<T>::GetResults — vtable[8] */
static inline HRESULT _asyncop_results(void *pAsyncOp, void **out) {
    typedef HRESULT (STDMETHODCALLTYPE *FN)(void*, void**);
    return ((FN *)*(void **)pAsyncOp)[8](pAsyncOp, out);
}

/* ── Interface GUIDs (Windows 10 SDK, from WinRT .winmd metadata) ──────────
 *
 * GUIDs are stable and do not change across Windows 10/11 versions.
 * They are derived from the WinRT interface signature hash (SHA-1 variant).
 */

/* IStorageProviderSyncRootInfo
 * Windows.Storage.Provider.StorageProviderSyncRootInfo default interface */
static const GUID IID_GHD_IStorageProviderSyncRootInfo = {
    0xba6295c3, 0x312e, 0x544b,
    {0x9d, 0x4a, 0x65, 0xe4, 0x63, 0xb2, 0x9f, 0xce}
};

/* IStorageProviderSyncRootManagerStatics
 * Activation factory for StorageProviderSyncRootManager */
static const GUID IID_GHD_IStorageProviderSyncRootManagerStatics = {
    0xcf0b9ce8, 0x3cb3, 0x5e52,
    {0x9d, 0x77, 0x87, 0x49, 0xe5, 0x51, 0xc2, 0x7d}
};

/* IStorageFolderStatics
 * Activation factory statics for Windows.Storage.StorageFolder */
static const GUID IID_GHD_IStorageFolderStatics = {
    0x08f6d879, 0x4b1e, 0x40b6,
    {0xb3, 0xa5, 0xc7, 0xcc, 0x04, 0xfd, 0xe7, 0x7d}
};

/* IAsyncInfo — standard WinRT async info {00000036-0000-0000-C000-000000000046}
 * MinGW 16.1.0 does NOT export IID_IAsyncInfo from libruntimeobject.a, so we
 * define it locally as a static const GUID instead of relying on the extern
 * declaration from asyncinfo.h.  Using a GHD-prefixed name avoids any clash
 * with a future MinGW version that does export the symbol. */
static const GUID IID_GHD_IAsyncInfo = {
    0x00000036, 0x0000, 0x0000,
    {0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}
};

/* ── IStorageProviderSyncRootInfo vtable method indices ─────────────────────
 *
 * Derived from the WinRT IDL / .winmd metadata property order.
 * Each property has get_ (even index) and put_ (odd index).
 */
#define SPRI_IDX_PUT_ID                  7   /* put_Id(HSTRING) */
#define SPRI_IDX_PUT_DISPLAYNAME        11   /* put_DisplayNameResource(HSTRING) */
#define SPRI_IDX_PUT_ICON               13   /* put_IconResource(HSTRING) */
#define SPRI_IDX_PUT_HYDRATION          15   /* put_HydrationPolicy(INT32) */
#define SPRI_IDX_PUT_HYDRATION_MOD      17   /* put_HydrationPolicyModifier(INT32) */
#define SPRI_IDX_PUT_POPULATION         19   /* put_PopulationPolicy(INT32) */
#define SPRI_IDX_PUT_INSYNC             21   /* put_InSyncPolicy(INT32) */
#define SPRI_IDX_PUT_HARDLINK           23   /* put_HardlinkPolicy(INT32) */
#define SPRI_IDX_PUT_SIBLINGS           25   /* put_ShowSiblingsAsGroup(boolean) */
#define SPRI_IDX_PUT_VERSION            27   /* put_Version(HSTRING) */
#define SPRI_IDX_PUT_PATH               31   /* put_Path(IStorageFolder*) */

/* IStorageProviderSyncRootManagerStatics::Register index */
#define SPRM_IDX_REGISTER               6

/* IStorageFolderStatics::GetFolderFromPathAsync index */
#define SF_IDX_GET_FOLDER_FROM_PATH     6

/* ── StorageProviderHydrationPolicy / PopulationPolicy enum values ───────── */
#define SPHP_FULL           2   /* Full hydration on demand */
#define SPPP_ALWAYS_FULL    2   /* AlwaysFull: all placeholders present */

/* ── Main entry point ────────────────────────────────────────────────────── */

/*
 * ghd_register_storage_provider_winrt
 *
 * Calls WinRT StorageProviderSyncRootManager::Register via COM C ABI.
 * On success: returns S_OK and calls SHChangeNotify to flush Explorer.
 * On failure: returns HRESULT — Go caller falls back to registry writes.
 *
 * id           — e.g. L"GhostDrive!S-1-5-21-1234!MFS"
 * displayName  — e.g. L"GhostDrive — MFS"
 * syncRootPath — e.g. L"C:\\GhostDrive\\MFS"
 */
HRESULT ghd_register_storage_provider_winrt(
    const wchar_t *id,
    const wchar_t *displayName,
    const wchar_t *syncRootPath)
{
    HRESULT hr = S_OK;
    BOOL    roInitialised = FALSE;

    /* HSTRING locals — WindowsDeleteString(NULL) is safe (no-op) */
    HSTRING hClassInfo    = NULL;
    HSTRING hClassMgr     = NULL;
    HSTRING hClassFolder  = NULL;
    HSTRING hId           = NULL;
    HSTRING hDisplayName  = NULL;
    HSTRING hIconRes      = NULL;
    HSTRING hVersion      = NULL;
    HSTRING hPath         = NULL;

    /* COM interface locals */
    void *pInfo        = NULL; /* IStorageProviderSyncRootInfo* */
    void *pMgrFactory  = NULL; /* IStorageProviderSyncRootManagerStatics* */
    void *pSFFactory   = NULL; /* IStorageFolderStatics* */
    void *pAsyncOp     = NULL; /* IAsyncOperation<StorageFolder>* */
    void *pAsyncInfo   = NULL; /* IAsyncInfo* */
    void *pFolder      = NULL; /* IStorageFolder* */

    /* ── 1. Initialise WinRT apartment ─────────────────────────────────── */
    hr = RoInitialize(RO_INIT_MULTITHREADED);
    if (SUCCEEDED(hr) && hr != S_FALSE) {
        roInitialised = TRUE;
    } else if (hr == RPC_E_CHANGED_MODE) {
        /* Apartment already initialised with a different (but compatible) model.
         * Proceed without taking ownership of the initialisation. */
        hr = S_OK;
    } else if (FAILED(hr)) {
        goto cleanup;
    } else {
        /* S_FALSE: already initialised by this thread, reference counted */
        roInitialised = TRUE;
        hr = S_OK;
    }

    /* ── 2. Create StorageProviderSyncRootInfo instance ────────────────── */
    {
        static const wchar_t kInfoClass[] =
            L"Windows.Storage.Provider.StorageProviderSyncRootInfo";
        if (FAILED(hr = WindowsCreateString(kInfoClass,
                (UINT32)(sizeof(kInfoClass)/sizeof(wchar_t) - 1),
                &hClassInfo))) goto cleanup;
    }
    if (FAILED(hr = RoActivateInstance(hClassInfo, (IInspectable **)&pInfo)))
        goto cleanup;

    /* QI for typed interface (validates the GUID) */
    {
        void *pTyped = NULL;
        if (FAILED(hr = _com_qi(pInfo, &IID_GHD_IStorageProviderSyncRootInfo, &pTyped)))
            goto cleanup;
        _com_release(pInfo);
        pInfo = pTyped;
    }

    /* ── 3. Set properties ─────────────────────────────────────────────── */
#define HS(var, str) \
    if (FAILED(hr = WindowsCreateString((str), \
            (UINT32)wcslen(str), &(var)))) goto cleanup

    HS(hId,          id);
    HS(hDisplayName, displayName);
    HS(hIconRes,     L"%SystemRoot%\\System32\\imageres.dll,-1043");
    HS(hVersion,     L"2.1");

#undef HS

#define SET_HS(idx, h)  if (FAILED(hr = _com_put_hs (pInfo, (idx), (h)))) goto cleanup
#define SET_I32(idx, v) if (FAILED(hr = _com_put_i32(pInfo, (idx), (v)))) goto cleanup

    SET_HS (SPRI_IDX_PUT_ID,          hId);
    SET_HS (SPRI_IDX_PUT_DISPLAYNAME, hDisplayName);
    SET_HS (SPRI_IDX_PUT_ICON,        hIconRes);
    SET_I32(SPRI_IDX_PUT_HYDRATION,      SPHP_FULL);
    SET_I32(SPRI_IDX_PUT_HYDRATION_MOD,  0); /* HydrationPolicyModifier::None */
    SET_I32(SPRI_IDX_PUT_POPULATION,     SPPP_ALWAYS_FULL);
    SET_I32(SPRI_IDX_PUT_INSYNC,         3); /* FileCreationTime | DirCreationTime */
    SET_I32(SPRI_IDX_PUT_HARDLINK,       0); /* HardlinkPolicy::None */
    SET_I32(SPRI_IDX_PUT_SIBLINGS,       0); /* ShowSiblingsAsGroup = FALSE */
    SET_HS (SPRI_IDX_PUT_VERSION,     hVersion);

#undef SET_HS
#undef SET_I32

    /* ── 4. Obtain StorageFolder for the sync root path (async) ─────────
     *
     * StorageFolder.GetFolderFromPathAsync returns an IAsyncOperation.
     * For a local path on desktop Windows the operation completes in <1 ms.
     * We poll IAsyncInfo::get_Status with SleepEx(1, TRUE) (allows APC,
     * required for WinRT apartment message pump on STA threads).
     */
    {
        static const wchar_t kFolderClass[] = L"Windows.Storage.StorageFolder";
        if (FAILED(hr = WindowsCreateString(kFolderClass,
                (UINT32)(sizeof(kFolderClass)/sizeof(wchar_t) - 1),
                &hClassFolder))) goto cleanup;
    }
    if (FAILED(hr = RoGetActivationFactory(hClassFolder,
            &IID_GHD_IStorageFolderStatics, &pSFFactory)))
        goto cleanup;

    if (FAILED(hr = WindowsCreateString(syncRootPath,
            (UINT32)wcslen(syncRootPath), &hPath))) goto cleanup;

    /* GetFolderFromPathAsync(HSTRING, IAsyncOperation<StorageFolder>**) */
    if (FAILED(hr = _com_hs_ppv(pSFFactory, SF_IDX_GET_FOLDER_FROM_PATH,
            hPath, &pAsyncOp))) goto cleanup;

    /* Wait for async operation (IAsyncInfo::get_Status until Completed) */
    if (FAILED(hr = _com_qi(pAsyncOp, &IID_GHD_IAsyncInfo, &pAsyncInfo))) goto cleanup;
    {
        AsyncStatus status = Started;
        DWORD deadline = GetTickCount() + 5000; /* 5 s timeout */
        while (status == Started) {
            if (GetTickCount() >= deadline) { hr = HRESULT_FROM_WIN32(WAIT_TIMEOUT); goto cleanup; }
            hr = _asyncinfo_status(pAsyncInfo, &status);
            if (FAILED(hr)) goto cleanup;
            if (status == Started) SleepEx(1, TRUE);
        }
        if (status != Completed) {
            hr = E_FAIL;
            goto cleanup;
        }
    }

    /* IAsyncOperation<StorageFolder>::GetResults() → IStorageFolder* */
    if (FAILED(hr = _asyncop_results(pAsyncOp, &pFolder))) goto cleanup;
    if (!pFolder) { hr = E_UNEXPECTED; goto cleanup; }

    /* put_Path(IStorageFolder*) */
    if (FAILED(hr = _com_put_ptr(pInfo, SPRI_IDX_PUT_PATH, pFolder))) goto cleanup;

    /* ── 5. Obtain StorageProviderSyncRootManagerStatics and call Register */
    {
        static const wchar_t kMgrClass[] =
            L"Windows.Storage.Provider.StorageProviderSyncRootManager";
        if (FAILED(hr = WindowsCreateString(kMgrClass,
                (UINT32)(sizeof(kMgrClass)/sizeof(wchar_t) - 1),
                &hClassMgr))) goto cleanup;
    }
    if (FAILED(hr = RoGetActivationFactory(hClassMgr,
            &IID_GHD_IStorageProviderSyncRootManagerStatics, &pMgrFactory)))
        goto cleanup;

    /* Register(IStorageProviderSyncRootInfo*) */
    if (FAILED(hr = _com_put_ptr(pMgrFactory, SPRM_IDX_REGISTER, pInfo))) goto cleanup;

    /* ── 6. Notify Explorer to reload cloud provider overlay icons ───────
     *
     * StorageProviderSyncRootManager::Register internally calls SHChangeNotify.
     * We mirror this to ensure Explorer picks up the registration immediately
     * without requiring a restart.
     */
    SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_IDLIST | SHCNF_FLUSH, NULL, NULL);

cleanup:
    _com_release(pFolder);
    _com_release(pAsyncInfo);
    _com_release(pAsyncOp);
    _com_release(pSFFactory);
    _com_release(pMgrFactory);
    _com_release(pInfo);

    WindowsDeleteString(hPath);
    WindowsDeleteString(hVersion);
    WindowsDeleteString(hIconRes);
    WindowsDeleteString(hDisplayName);
    WindowsDeleteString(hId);
    WindowsDeleteString(hClassFolder);
    WindowsDeleteString(hClassMgr);
    WindowsDeleteString(hClassInfo);

    if (roInitialised) RoUninitialize();
    return hr;
}
