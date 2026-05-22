/*
 * cfapi.h — Windows Cloud Filter API definitions, MinGW-w64 compatible.
 *
 * Adapted from Windows SDK 10.0.22621.0 for CGO/MinGW compilation.
 * Key changes vs the SDK header:
 *  - Replaced #include <winapifamily.h> (pulls in vcruntime.h) with #include <windows.h>
 *  - Forward-declared CORRELATION_VECTOR (not always in MinGW sys headers)
 *  - Removed _MSC_VER / NTDDI_VERSION version guards for simplicity
 *  - Removed SAL annotations (_In_, _Out_, _Field_size_bytes_, …) — MinGW's
 *    sal.h already defines them as empty; duplicate empty defines are harmless
 *  - #pragma warning / #pragma region are ignored by GCC — kept for reference
 *
 * Minimum Windows target: 10.0.17763.0 (RS5 / 1809).
 * Link: -lcldapi  (MinGW) or cldapi.lib (MSVC)
 */

#ifndef GHD_CFAPI_COMPAT_H
#define GHD_CFAPI_COMPAT_H

#ifdef _WIN32

/* ------------------------------------------------------------------
 * Base Windows headers — MinGW-w64's own, no vcruntime.h dependency.
 * ------------------------------------------------------------------ */
#define WIN32_LEAN_AND_MEAN
#ifndef WINVER
#define WINVER 0x0A00
#endif
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0A00
#endif
#include <windows.h>   /* LARGE_INTEGER, DWORD, HRESULT, GUID, USN, … */
#include <winnt.h>     /* NTSTATUS, FILE_BASIC_INFO, … */

/* ------------------------------------------------------------------
 * CORRELATION_VECTOR — added in Windows 10 RS4; not always in MinGW.
 * Define as opaque if the type isn't already provided.
 * ------------------------------------------------------------------ */
#ifndef _CORRELATION_VECTOR_DEFINED
#define _CORRELATION_VECTOR_DEFINED
typedef struct _CORRELATION_VECTOR {
    CHAR Version;
    CHAR VectorLength;
    ULONGLONG Value[2];
} CORRELATION_VECTOR;
typedef CORRELATION_VECTOR* PCORRELATION_VECTOR;
typedef const CORRELATION_VECTOR* PCCORRELATION_VECTOR;
#endif /* _CORRELATION_VECTOR_DEFINED */

/* ------------------------------------------------------------------
 * NTSTATUS / USN guards — defined in winnt.h, but guard just in case.
 * ------------------------------------------------------------------ */
#ifndef _NTSTATUS_
#define _NTSTATUS_
typedef LONG NTSTATUS;
#endif
#ifndef USN
typedef LONGLONG USN;
#endif

/* ------------------------------------------------------------------
 * DEFINE_ENUM_FLAG_OPERATORS — C-only version (no-op; operators are
 * C++ only).  MinGW defines DEFINE_ENUM_FLAG_OPERATORS in winnt.h for
 * C++ but not for C — define a safe fallback.
 * ------------------------------------------------------------------ */
#ifndef DEFINE_ENUM_FLAG_OPERATORS
#define DEFINE_ENUM_FLAG_OPERATORS(ENUMTYPE) /* C fallback: no-op */
#endif

/* ------------------------------------------------------------------
 * CF_SIZE_OF_OP_PARAM — not in cfapi.h but used by CF providers.
 * Computes the required ParamSize for a CF_OPERATION_PARAMETERS field.
 * ------------------------------------------------------------------ */
#ifndef CF_SIZE_OF_OP_PARAM
#define CF_SIZE_OF_OP_PARAM(FieldName) \
    (FIELD_OFFSET(CF_OPERATION_PARAMETERS, FieldName) + \
     sizeof(((CF_OPERATION_PARAMETERS*)0)->FieldName))
#endif

/* ==================================================================
 *  CF API constants
 * ================================================================== */

#define CF_EOF                                  (-1LL)
#define CF_REQUEST_KEY_DEFAULT                  (0)
#define CF_MAX_PRIORITY_HINT                    15
#define CF_PLACEHOLDER_MAX_FILE_IDENTITY_LENGTH 4096
#define CF_MAX_PROVIDER_NAME_LENGTH             255
#define CF_MAX_PROVIDER_VERSION_LENGTH          255

/* ==================================================================
 *  Opaque key types
 * ================================================================== */

#define DECLARE_OPAQUE_KEY(name) \
    typedef struct name##__ { LONGLONG Internal; } name, *P##name

DECLARE_OPAQUE_KEY(CF_CONNECTION_KEY);

typedef LARGE_INTEGER CF_TRANSFER_KEY;
typedef LARGE_INTEGER CF_REQUEST_KEY;

/* ==================================================================
 *  DEFINE_USHORT_ENUM — C version (typedef USHORT)
 * ================================================================== */
#define DEFINE_USHORT_ENUM(ENUMTYPE) typedef USHORT ENUMTYPE##_USHORT

/* ==================================================================
 *  Filesystem metadata
 * ================================================================== */

typedef struct CF_FS_METADATA {
    FILE_BASIC_INFO BasicInfo;
    LARGE_INTEGER   FileSize;
} CF_FS_METADATA;

/* ==================================================================
 *  Placeholder creation
 * ================================================================== */

typedef enum CF_PLACEHOLDER_CREATE_FLAGS {
    CF_PLACEHOLDER_CREATE_FLAG_NONE                         = 0x00000000,
    CF_PLACEHOLDER_CREATE_FLAG_DISABLE_ON_DEMAND_POPULATION = 0x00000001,
    CF_PLACEHOLDER_CREATE_FLAG_MARK_IN_SYNC                 = 0x00000002,
    CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE                    = 0x00000004,
    CF_PLACEHOLDER_CREATE_FLAG_ALWAYS_FULL                  = 0x00000008,
} CF_PLACEHOLDER_CREATE_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_PLACEHOLDER_CREATE_FLAGS);

typedef struct CF_PLACEHOLDER_CREATE_INFO {
    LPCWSTR                   RelativeFileName;
    CF_FS_METADATA            FsMetadata;
    LPCVOID                   FileIdentity;
    DWORD                     FileIdentityLength;
    CF_PLACEHOLDER_CREATE_FLAGS Flags;
    HRESULT                   Result;
    USN                       CreateUsn;
} CF_PLACEHOLDER_CREATE_INFO;

typedef enum CF_CREATE_FLAGS {
    CF_CREATE_FLAG_NONE          = 0x00000000,
    CF_CREATE_FLAG_STOP_ON_ERROR = 0x00000001,
} CF_CREATE_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CREATE_FLAGS);

/* ==================================================================
 *  Sync provider status
 * ================================================================== */

typedef enum CF_SYNC_PROVIDER_STATUS {
    CF_PROVIDER_STATUS_DISCONNECTED       = 0x00000000,
    CF_PROVIDER_STATUS_IDLE               = 0x00000001,
    CF_PROVIDER_STATUS_POPULATE_NAMESPACE = 0x00000002,
    CF_PROVIDER_STATUS_POPULATE_METADATA  = 0x00000004,
    CF_PROVIDER_STATUS_POPULATE_CONTENT   = 0x00000008,
    CF_PROVIDER_STATUS_SYNC_INCREMENTAL   = 0x00000010,
    CF_PROVIDER_STATUS_SYNC_FULL          = 0x00000020,
    CF_PROVIDER_STATUS_CONNECTIVITY_LOST  = 0x00000040,
    CF_PROVIDER_STATUS_CLEAR_FLAGS        = 0x80000000,
    CF_PROVIDER_STATUS_TERMINATED         = 0xC0000001,
    CF_PROVIDER_STATUS_ERROR              = 0xC0000002,
} CF_SYNC_PROVIDER_STATUS;
DEFINE_ENUM_FLAG_OPERATORS(CF_SYNC_PROVIDER_STATUS);

/* ==================================================================
 *  Process info (passed in callbacks)
 * ================================================================== */

typedef struct CF_PROCESS_INFO {
    DWORD  StructSize;
    DWORD  ProcessId;
    PCWSTR ImagePath;
    PCWSTR PackageName;
    PCWSTR ApplicationId;
    PCWSTR CommandLine;
    DWORD  SessionId;
} CF_PROCESS_INFO;

/* ==================================================================
 *  Registration / policies
 * ================================================================== */

typedef enum CF_REGISTER_FLAGS {
    CF_REGISTER_FLAG_NONE                                  = 0x00000000,
    CF_REGISTER_FLAG_UPDATE                                = 0x00000001,
    CF_REGISTER_FLAG_DISABLE_ON_DEMAND_POPULATION_ON_ROOT  = 0x00000002,
    CF_REGISTER_FLAG_MARK_IN_SYNC_ON_ROOT                  = 0x00000004,
} CF_REGISTER_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_REGISTER_FLAGS);

typedef enum CF_HYDRATION_POLICY_PRIMARY {
    CF_HYDRATION_POLICY_PARTIAL     = 0,
    CF_HYDRATION_POLICY_PROGRESSIVE = 1,
    CF_HYDRATION_POLICY_FULL        = 2,
    CF_HYDRATION_POLICY_ALWAYS_FULL = 3,
} CF_HYDRATION_POLICY_PRIMARY;
DEFINE_USHORT_ENUM(CF_HYDRATION_POLICY_PRIMARY);

typedef enum CF_HYDRATION_POLICY_MODIFIER {
    CF_HYDRATION_POLICY_MODIFIER_NONE                         = 0x0000,
    CF_HYDRATION_POLICY_MODIFIER_VALIDATION_REQUIRED          = 0x0001,
    CF_HYDRATION_POLICY_MODIFIER_STREAMING_ALLOWED            = 0x0002,
    CF_HYDRATION_POLICY_MODIFIER_AUTO_DEHYDRATION_ALLOWED     = 0x0004,
    CF_HYDRATION_POLICY_MODIFIER_ALLOW_FULL_RESTART_HYDRATION = 0x0008,
} CF_HYDRATION_POLICY_MODIFIER;
DEFINE_USHORT_ENUM(CF_HYDRATION_POLICY_MODIFIER);
DEFINE_ENUM_FLAG_OPERATORS(CF_HYDRATION_POLICY_MODIFIER);

typedef struct CF_HYDRATION_POLICY {
    CF_HYDRATION_POLICY_PRIMARY_USHORT  Primary;
    CF_HYDRATION_POLICY_MODIFIER_USHORT Modifier;
} CF_HYDRATION_POLICY;

typedef enum CF_POPULATION_POLICY_PRIMARY {
    CF_POPULATION_POLICY_PARTIAL     = 0,
    CF_POPULATION_POLICY_FULL        = 2,
    CF_POPULATION_POLICY_ALWAYS_FULL = 3,
} CF_POPULATION_POLICY_PRIMARY;
DEFINE_USHORT_ENUM(CF_POPULATION_POLICY_PRIMARY);

typedef enum CF_POPULATION_POLICY_MODIFIER {
    CF_POPULATION_POLICY_MODIFIER_NONE = 0x0000,
} CF_POPULATION_POLICY_MODIFIER;
DEFINE_USHORT_ENUM(CF_POPULATION_POLICY_MODIFIER);
DEFINE_ENUM_FLAG_OPERATORS(CF_POPULATION_POLICY_MODIFIER);

typedef struct CF_POPULATION_POLICY {
    CF_POPULATION_POLICY_PRIMARY_USHORT  Primary;
    CF_POPULATION_POLICY_MODIFIER_USHORT Modifier;
} CF_POPULATION_POLICY;

typedef enum CF_INSYNC_POLICY {
    CF_INSYNC_POLICY_NONE                               = 0x00000000,
    CF_INSYNC_POLICY_TRACK_FILE_CREATION_TIME           = 0x00000001,
    CF_INSYNC_POLICY_TRACK_FILE_READONLY_ATTRIBUTE      = 0x00000002,
    CF_INSYNC_POLICY_TRACK_FILE_HIDDEN_ATTRIBUTE        = 0x00000004,
    CF_INSYNC_POLICY_TRACK_FILE_SYSTEM_ATTRIBUTE        = 0x00000008,
    CF_INSYNC_POLICY_TRACK_DIRECTORY_CREATION_TIME      = 0x00000010,
    CF_INSYNC_POLICY_TRACK_DIRECTORY_READONLY_ATTRIBUTE = 0x00000020,
    CF_INSYNC_POLICY_TRACK_DIRECTORY_HIDDEN_ATTRIBUTE   = 0x00000040,
    CF_INSYNC_POLICY_TRACK_DIRECTORY_SYSTEM_ATTRIBUTE   = 0x00000080,
    CF_INSYNC_POLICY_TRACK_FILE_LAST_WRITE_TIME         = 0x00000100,
    CF_INSYNC_POLICY_TRACK_DIRECTORY_LAST_WRITE_TIME    = 0x00000200,
    CF_INSYNC_POLICY_TRACK_FILE_ALL                     = 0x0055550f,
    CF_INSYNC_POLICY_TRACK_DIRECTORY_ALL                = 0x00aaaaf0,
    CF_INSYNC_POLICY_TRACK_ALL                          = 0x00ffffff,
    CF_INSYNC_POLICY_PRESERVE_INSYNC_FOR_SYNC_ENGINE    = 0x80000000,
} CF_INSYNC_POLICY;
DEFINE_ENUM_FLAG_OPERATORS(CF_INSYNC_POLICY);

typedef enum CF_HARDLINK_POLICY {
    CF_HARDLINK_POLICY_NONE    = 0x00000000,
    CF_HARDLINK_POLICY_ALLOWED = 0x00000001,
} CF_HARDLINK_POLICY;
DEFINE_ENUM_FLAG_OPERATORS(CF_HARDLINK_POLICY);

typedef enum CF_PLACEHOLDER_MANAGEMENT_POLICY {
    CF_PLACEHOLDER_MANAGEMENT_POLICY_DEFAULT                  = 0x00000000,
    CF_PLACEHOLDER_MANAGEMENT_POLICY_CREATE_UNRESTRICTED      = 0x00000001,
    CF_PLACEHOLDER_MANAGEMENT_POLICY_CONVERT_TO_UNRESTRICTED  = 0x00000002,
    CF_PLACEHOLDER_MANAGEMENT_POLICY_UPDATE_UNRESTRICTED      = 0x00000004,
} CF_PLACEHOLDER_MANAGEMENT_POLICY;

typedef struct CF_SYNC_POLICIES {
    ULONG                         StructSize;
    CF_HYDRATION_POLICY           Hydration;
    CF_POPULATION_POLICY          Population;
    CF_INSYNC_POLICY              InSync;
    CF_HARDLINK_POLICY            HardLink;
    CF_PLACEHOLDER_MANAGEMENT_POLICY PlaceholderManagement;
} CF_SYNC_POLICIES;

/*
 * CF_SYNC_REGISTRATION — ProviderName and ProviderVersion are LPCWSTR pointers
 * (not fixed-size arrays) in SDK 10.0.17763+.  Assign pointers directly:
 *   reg.ProviderName    = L"GhostDrive";
 *   reg.ProviderVersion = L"2.1";
 */
typedef struct CF_SYNC_REGISTRATION {
    ULONG   StructSize;
    LPCWSTR ProviderName;
    LPCWSTR ProviderVersion;
    LPCVOID SyncRootIdentity;
    DWORD   SyncRootIdentityLength;
    LPCVOID FileIdentity;
    DWORD   FileIdentityLength;
    GUID    ProviderId;
} CF_SYNC_REGISTRATION;

/* ==================================================================
 *  Sync root registration / deregistration
 * ================================================================== */

STDAPI CfRegisterSyncRoot(
    LPCWSTR                       SyncRootPath,
    const CF_SYNC_REGISTRATION*   Registration,
    const CF_SYNC_POLICIES*       Policies,
    CF_REGISTER_FLAGS             RegisterFlags
);

STDAPI CfUnregisterSyncRoot(
    LPCWSTR SyncRootPath
);

/* ==================================================================
 *  Callback info & parameters
 * ================================================================== */

typedef struct CF_CALLBACK_INFO {
    DWORD              StructSize;
    CF_CONNECTION_KEY  ConnectionKey;
    LPVOID             CallbackContext;
    PCWSTR             VolumeGuidName;
    PCWSTR             VolumeDosName;
    DWORD              VolumeSerialNumber;
    LARGE_INTEGER      SyncRootFileId;
    LPCVOID            SyncRootIdentity;
    DWORD              SyncRootIdentityLength;
    LARGE_INTEGER      FileId;
    LARGE_INTEGER      FileSize;
    LPCVOID            FileIdentity;
    DWORD              FileIdentityLength;
    PCWSTR             NormalizedPath;
    CF_TRANSFER_KEY    TransferKey;
    UCHAR              PriorityHint;
    PCORRELATION_VECTOR CorrelationVector;
    CF_PROCESS_INFO*   ProcessInfo;
    CF_REQUEST_KEY     RequestKey;
} CF_CALLBACK_INFO;

typedef enum CF_CALLBACK_CANCEL_FLAGS {
    CF_CALLBACK_CANCEL_FLAG_NONE       = 0x00000000,
    CF_CALLBACK_CANCEL_FLAG_IO_TIMEOUT = 0x00000001,
    CF_CALLBACK_CANCEL_FLAG_IO_ABORTED = 0x00000002,
} CF_CALLBACK_CANCEL_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_CANCEL_FLAGS);

typedef enum CF_CALLBACK_FETCH_DATA_FLAGS {
    CF_CALLBACK_FETCH_DATA_FLAG_NONE               = 0x00000000,
    CF_CALLBACK_FETCH_DATA_FLAG_RECOVERY           = 0x00000001,
    CF_CALLBACK_FETCH_DATA_FLAG_EXPLICIT_HYDRATION = 0x00000002,
} CF_CALLBACK_FETCH_DATA_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_FETCH_DATA_FLAGS);

typedef enum CF_CALLBACK_VALIDATE_DATA_FLAGS {
    CF_CALLBACK_VALIDATE_DATA_FLAG_NONE               = 0x00000000,
    CF_CALLBACK_VALIDATE_DATA_FLAG_EXPLICIT_HYDRATION = 0x00000002,
} CF_CALLBACK_VALIDATE_DATA_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_VALIDATE_DATA_FLAGS);

typedef enum CF_CALLBACK_FETCH_PLACEHOLDERS_FLAGS {
    CF_CALLBACK_FETCH_PLACEHOLDERS_FLAG_NONE = 0x00000000,
} CF_CALLBACK_FETCH_PLACEHOLDERS_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_FETCH_PLACEHOLDERS_FLAGS);

typedef enum CF_CALLBACK_OPEN_COMPLETION_FLAGS {
    CF_CALLBACK_OPEN_COMPLETION_FLAG_NONE                    = 0x00000000,
    CF_CALLBACK_OPEN_COMPLETION_FLAG_PLACEHOLDER_UNKNOWN     = 0x00000001,
    CF_CALLBACK_OPEN_COMPLETION_FLAG_PLACEHOLDER_UNSUPPORTED = 0x00000002,
} CF_CALLBACK_OPEN_COMPLETION_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_OPEN_COMPLETION_FLAGS);

typedef enum CF_CALLBACK_CLOSE_COMPLETION_FLAGS {
    CF_CALLBACK_CLOSE_COMPLETION_FLAG_NONE    = 0x00000000,
    CF_CALLBACK_CLOSE_COMPLETION_FLAG_DELETED = 0x00000001,
} CF_CALLBACK_CLOSE_COMPLETION_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_CLOSE_COMPLETION_FLAGS);

typedef enum CF_CALLBACK_DEHYDRATE_FLAGS {
    CF_CALLBACK_DEHYDRATE_FLAG_NONE       = 0x00000000,
    CF_CALLBACK_DEHYDRATE_FLAG_BACKGROUND = 0x00000001,
} CF_CALLBACK_DEHYDRATE_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_DEHYDRATE_FLAGS);

typedef enum CF_CALLBACK_DEHYDRATE_COMPLETION_FLAGS {
    CF_CALLBACK_DEHYDRATE_COMPLETION_FLAG_NONE       = 0x00000000,
    CF_CALLBACK_DEHYDRATE_COMPLETION_FLAG_BACKGROUND = 0x00000001,
    CF_CALLBACK_DEHYDRATE_COMPLETION_FLAG_DEHYDRATED = 0x00000002,
} CF_CALLBACK_DEHYDRATE_COMPLETION_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_DEHYDRATE_COMPLETION_FLAGS);

typedef enum CF_CALLBACK_DELETE_FLAGS {
    CF_CALLBACK_DELETE_FLAG_NONE         = 0x00000000,
    CF_CALLBACK_DELETE_FLAG_IS_DIRECTORY = 0x00000001,
    CF_CALLBACK_DELETE_FLAG_IS_UNDELETE  = 0x00000002,
} CF_CALLBACK_DELETE_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_DELETE_FLAGS);

typedef enum CF_CALLBACK_DELETE_COMPLETION_FLAGS {
    CF_CALLBACK_DELETE_COMPLETION_FLAG_NONE = 0x00000000,
} CF_CALLBACK_DELETE_COMPLETION_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_DELETE_COMPLETION_FLAGS);

typedef enum CF_CALLBACK_RENAME_FLAGS {
    CF_CALLBACK_RENAME_FLAG_NONE            = 0x00000000,
    CF_CALLBACK_RENAME_FLAG_IS_DIRECTORY    = 0x00000001,
    CF_CALLBACK_RENAME_FLAG_SOURCE_IN_SCOPE = 0x00000002,
    CF_CALLBACK_RENAME_FLAG_TARGET_IN_SCOPE = 0x00000004,
} CF_CALLBACK_RENAME_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_RENAME_FLAGS);

typedef enum CF_CALLBACK_RENAME_COMPLETION_FLAGS {
    CF_CALLBACK_RENAME_COMPLETION_FLAG_NONE = 0x00000000,
} CF_CALLBACK_RENAME_COMPLETION_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CALLBACK_RENAME_COMPLETION_FLAGS);

typedef enum CF_CALLBACK_DEHYDRATION_REASON {
    CF_CALLBACK_DEHYDRATION_REASON_NONE,
    CF_CALLBACK_DEHYDRATION_REASON_USER_MANUAL,
    CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_LOW_SPACE,
    CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_INACTIVITY,
    CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_OS_UPGRADE,
} CF_CALLBACK_DEHYDRATION_REASON;

/*
 * CF_CALLBACK_PARAMETERS — anonymous union via DUMMYUNIONNAME.
 * MinGW defines DUMMYUNIONNAME as empty (anonymous union) in winnt.h,
 * matching MSVC behaviour.  Members are accessible as params.FetchData.xxx.
 */
typedef struct CF_CALLBACK_PARAMETERS {
    ULONG ParamSize;
    union {
        struct {
            CF_CALLBACK_CANCEL_FLAGS Flags;
            union {
                struct {
                    LARGE_INTEGER FileOffset;
                    LARGE_INTEGER Length;
                } FetchData;
            } DUMMYUNIONNAME;
        } Cancel;

        struct {
            CF_CALLBACK_FETCH_DATA_FLAGS     Flags;
            LARGE_INTEGER                    RequiredFileOffset;
            LARGE_INTEGER                    RequiredLength;
            LARGE_INTEGER                    OptionalFileOffset;
            LARGE_INTEGER                    OptionalLength;
            LARGE_INTEGER                    LastDehydrationTime;
            CF_CALLBACK_DEHYDRATION_REASON   LastDehydrationReason;
        } FetchData;

        struct {
            CF_CALLBACK_VALIDATE_DATA_FLAGS Flags;
            LARGE_INTEGER                   RequiredFileOffset;
            LARGE_INTEGER                   RequiredLength;
        } ValidateData;

        struct {
            CF_CALLBACK_FETCH_PLACEHOLDERS_FLAGS Flags;
            PCWSTR                               Pattern;
        } FetchPlaceholders;

        struct {
            CF_CALLBACK_OPEN_COMPLETION_FLAGS Flags;
        } OpenCompletion;

        struct {
            CF_CALLBACK_CLOSE_COMPLETION_FLAGS Flags;
        } CloseCompletion;

        struct {
            CF_CALLBACK_DEHYDRATE_FLAGS        Flags;
            CF_CALLBACK_DEHYDRATION_REASON     Reason;
        } Dehydrate;

        struct {
            CF_CALLBACK_DEHYDRATE_COMPLETION_FLAGS Flags;
            CF_CALLBACK_DEHYDRATION_REASON         Reason;
        } DehydrateCompletion;

        struct {
            CF_CALLBACK_DELETE_FLAGS Flags;
        } Delete;

        struct {
            CF_CALLBACK_DELETE_COMPLETION_FLAGS Flags;
        } DeleteCompletion;

        struct {
            CF_CALLBACK_RENAME_FLAGS Flags;
            PCWSTR                   TargetPath;
        } Rename;

        struct {
            CF_CALLBACK_RENAME_COMPLETION_FLAGS Flags;
            PCWSTR                              SourcePath;
        } RenameCompletion;

    } DUMMYUNIONNAME;
} CF_CALLBACK_PARAMETERS;

typedef VOID (CALLBACK *CF_CALLBACK)(
    const CF_CALLBACK_INFO*       CallbackInfo,
    const CF_CALLBACK_PARAMETERS* CallbackParameters
);

typedef enum CF_CALLBACK_TYPE {
    CF_CALLBACK_TYPE_FETCH_DATA,
    CF_CALLBACK_TYPE_VALIDATE_DATA,
    CF_CALLBACK_TYPE_CANCEL_FETCH_DATA,
    CF_CALLBACK_TYPE_FETCH_PLACEHOLDERS,
    CF_CALLBACK_TYPE_CANCEL_FETCH_PLACEHOLDERS,
    CF_CALLBACK_TYPE_NOTIFY_FILE_OPEN_COMPLETION,
    CF_CALLBACK_TYPE_NOTIFY_FILE_CLOSE_COMPLETION,
    CF_CALLBACK_TYPE_NOTIFY_DEHYDRATE,
    CF_CALLBACK_TYPE_NOTIFY_DEHYDRATE_COMPLETION,
    CF_CALLBACK_TYPE_NOTIFY_DELETE,
    CF_CALLBACK_TYPE_NOTIFY_DELETE_COMPLETION,
    CF_CALLBACK_TYPE_NOTIFY_RENAME,
    CF_CALLBACK_TYPE_NOTIFY_RENAME_COMPLETION,
    CF_CALLBACK_TYPE_NONE = 0xffffffff,
} CF_CALLBACK_TYPE;

typedef struct CF_CALLBACK_REGISTRATION {
    CF_CALLBACK_TYPE Type;
    CF_CALLBACK      Callback;
} CF_CALLBACK_REGISTRATION;

#define CF_CALLBACK_REGISTRATION_END {CF_CALLBACK_TYPE_NONE, NULL}

/* ==================================================================
 *  Sync root connection
 * ================================================================== */

typedef enum CF_CONNECT_FLAGS {
    CF_CONNECT_FLAG_NONE                          = 0x00000000,
    CF_CONNECT_FLAG_REQUIRE_PROCESS_INFO          = 0x00000002,
    CF_CONNECT_FLAG_REQUIRE_FULL_FILE_PATH        = 0x00000004,
    CF_CONNECT_FLAG_BLOCK_SELF_IMPLICIT_HYDRATION = 0x00000008,
} CF_CONNECT_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CONNECT_FLAGS);

STDAPI CfConnectSyncRoot(
    LPCWSTR                         SyncRootPath,
    const CF_CALLBACK_REGISTRATION* CallbackTable,
    LPCVOID                         CallbackContext,
    CF_CONNECT_FLAGS                ConnectFlags,
    CF_CONNECTION_KEY*              ConnectionKey
);

STDAPI CfDisconnectSyncRoot(
    CF_CONNECTION_KEY ConnectionKey
);

/* ==================================================================
 *  CfExecute — data transfer operation
 * ================================================================== */

typedef enum CF_OPERATION_TYPE {
    CF_OPERATION_TYPE_TRANSFER_DATA,
    CF_OPERATION_TYPE_RETRIEVE_DATA,
    CF_OPERATION_TYPE_ACK_DATA,
    CF_OPERATION_TYPE_RESTART_HYDRATION,
    CF_OPERATION_TYPE_TRANSFER_PLACEHOLDERS,
    CF_OPERATION_TYPE_ACK_DEHYDRATE,
    CF_OPERATION_TYPE_ACK_DELETE,
    CF_OPERATION_TYPE_ACK_RENAME,
} CF_OPERATION_TYPE;

typedef struct CF_SYNC_STATUS {
    ULONG StructSize;
    ULONG Code;
    ULONG DescriptionOffset;
    ULONG DescriptionLength;
    ULONG DeviceIdOffset;
    ULONG DeviceIdLength;
} CF_SYNC_STATUS;

typedef struct CF_OPERATION_INFO {
    ULONG                   StructSize;
    CF_OPERATION_TYPE       Type;
    CF_CONNECTION_KEY       ConnectionKey;
    CF_TRANSFER_KEY         TransferKey;
    const CORRELATION_VECTOR* CorrelationVector;
    const CF_SYNC_STATUS*   SyncStatus;
    CF_REQUEST_KEY          RequestKey;
} CF_OPERATION_INFO;

typedef enum CF_OPERATION_TRANSFER_DATA_FLAGS {
    CF_OPERATION_TRANSFER_DATA_FLAG_NONE = 0x00000000,
} CF_OPERATION_TRANSFER_DATA_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_OPERATION_TRANSFER_DATA_FLAGS);

typedef enum CF_OPERATION_RETRIEVE_DATA_FLAGS {
    CF_OPERATION_RETRIEVE_DATA_FLAG_NONE = 0x00000000,
} CF_OPERATION_RETRIEVE_DATA_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_OPERATION_RETRIEVE_DATA_FLAGS);

typedef enum CF_OPERATION_ACK_DATA_FLAGS {
    CF_OPERATION_ACK_DATA_FLAG_NONE = 0x00000000,
} CF_OPERATION_ACK_DATA_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_OPERATION_ACK_DATA_FLAGS);

typedef enum CF_OPERATION_RESTART_HYDRATION_FLAGS {
    CF_OPERATION_RESTART_HYDRATION_FLAG_NONE         = 0x00000000,
    CF_OPERATION_RESTART_HYDRATION_FLAG_MARK_IN_SYNC = 0x00000001,
} CF_OPERATION_RESTART_HYDRATION_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_OPERATION_RESTART_HYDRATION_FLAGS);

typedef enum CF_OPERATION_TRANSFER_PLACEHOLDERS_FLAGS {
    CF_OPERATION_TRANSFER_PLACEHOLDERS_FLAG_NONE                           = 0x00000000,
    CF_OPERATION_TRANSFER_PLACEHOLDERS_FLAG_STOP_ON_ERROR                  = 0x00000001,
    CF_OPERATION_TRANSFER_PLACEHOLDERS_FLAG_DISABLE_ON_DEMAND_POPULATION   = 0x00000002,
} CF_OPERATION_TRANSFER_PLACEHOLDERS_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_OPERATION_TRANSFER_PLACEHOLDERS_FLAGS);

typedef enum CF_OPERATION_ACK_DEHYDRATE_FLAGS {
    CF_OPERATION_ACK_DEHYDRATE_FLAG_NONE = 0x00000000,
} CF_OPERATION_ACK_DEHYDRATE_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_OPERATION_ACK_DEHYDRATE_FLAGS);

typedef enum CF_OPERATION_ACK_RENAME_FLAGS {
    CF_OPERATION_ACK_RENAME_FLAG_NONE = 0x00000000,
} CF_OPERATION_ACK_RENAME_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_OPERATION_ACK_RENAME_FLAGS);

typedef enum CF_OPERATION_ACK_DELETE_FLAGS {
    CF_OPERATION_ACK_DELETE_FLAG_NONE = 0x00000000,
} CF_OPERATION_ACK_DELETE_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_OPERATION_ACK_DELETE_FLAGS);

/*
 * CF_OPERATION_PARAMETERS — anonymous union via DUMMYUNIONNAME.
 */
typedef struct CF_OPERATION_PARAMETERS {
    ULONG ParamSize;
    union {
        struct {
            CF_OPERATION_TRANSFER_DATA_FLAGS Flags;
            NTSTATUS                         CompletionStatus;
            LPCVOID                          Buffer;
            LARGE_INTEGER                    Offset;
            LARGE_INTEGER                    Length;
        } TransferData;

        struct {
            CF_OPERATION_RETRIEVE_DATA_FLAGS Flags;
            LPVOID                           Buffer;
            LARGE_INTEGER                    Offset;
            LARGE_INTEGER                    Length;
            LARGE_INTEGER                    ReturnedLength;
        } RetrieveData;

        struct {
            CF_OPERATION_ACK_DATA_FLAGS Flags;
            NTSTATUS                    CompletionStatus;
            LARGE_INTEGER               Offset;
            LARGE_INTEGER               Length;
        } AckData;

        struct {
            CF_OPERATION_RESTART_HYDRATION_FLAGS Flags;
            const CF_FS_METADATA*                FsMetadata;
            LPCVOID                              FileIdentity;
            DWORD                                FileIdentityLength;
        } RestartHydration;

        struct {
            CF_OPERATION_TRANSFER_PLACEHOLDERS_FLAGS Flags;
            NTSTATUS                                 CompletionStatus;
            LARGE_INTEGER                            PlaceholderTotalCount;
            CF_PLACEHOLDER_CREATE_INFO*              PlaceholderArray;
            DWORD                                    PlaceholderCount;
            DWORD                                    EntriesProcessed;
        } TransferPlaceholders;

        struct {
            CF_OPERATION_ACK_DEHYDRATE_FLAGS Flags;
            NTSTATUS                         CompletionStatus;
            LPCVOID                          FileIdentity;
            DWORD                            FileIdentityLength;
        } AckDehydrate;

        struct {
            CF_OPERATION_ACK_RENAME_FLAGS Flags;
            NTSTATUS                      CompletionStatus;
        } AckRename;

        struct {
            CF_OPERATION_ACK_DELETE_FLAGS Flags;
            NTSTATUS                      CompletionStatus;
        } AckDelete;

    } DUMMYUNIONNAME;
} CF_OPERATION_PARAMETERS;

STDAPI CfExecute(
    const CF_OPERATION_INFO*  OpInfo,
    CF_OPERATION_PARAMETERS*  OpParams
);

/* ==================================================================
 *  Placeholder creation
 * ================================================================== */

STDAPI CfCreatePlaceholders(
    LPCWSTR                    BaseDirectoryPath,
    CF_PLACEHOLDER_CREATE_INFO* PlaceholderArray,
    DWORD                      PlaceholderCount,
    CF_CREATE_FLAGS            CreateFlags,
    PDWORD                     EntriesProcessed
);

/* ==================================================================
 *  Sync state / pin state
 * ================================================================== */

typedef enum CF_IN_SYNC_STATE {
    CF_IN_SYNC_STATE_NOT_IN_SYNC = 0,
    CF_IN_SYNC_STATE_IN_SYNC     = 1,
} CF_IN_SYNC_STATE;

typedef enum CF_SET_IN_SYNC_FLAGS {
    CF_SET_IN_SYNC_FLAG_NONE = 0x00000000,
} CF_SET_IN_SYNC_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_SET_IN_SYNC_FLAGS);

STDAPI CfSetInSyncState(
    HANDLE              FileHandle,
    CF_IN_SYNC_STATE    InSyncState,
    CF_SET_IN_SYNC_FLAGS InSyncFlags,
    USN*                InSyncUsn
);

typedef enum CF_PIN_STATE {
    CF_PIN_STATE_UNSPECIFIED = 0,
    CF_PIN_STATE_PINNED      = 1,
    CF_PIN_STATE_UNPINNED    = 2,
    CF_PIN_STATE_EXCLUDED    = 3,
    CF_PIN_STATE_INHERIT     = 4,
} CF_PIN_STATE;

typedef enum CF_SET_PIN_FLAGS {
    CF_SET_PIN_FLAG_NONE                  = 0x00000000,
    CF_SET_PIN_FLAG_RECURSE               = 0x00000001,
    CF_SET_PIN_FLAG_RECURSE_ONLY          = 0x00000002,
    CF_SET_PIN_FLAG_RECURSE_STOP_ON_ERROR = 0x00000004,
} CF_SET_PIN_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_SET_PIN_FLAGS);

STDAPI CfSetPinState(
    HANDLE           FileHandle,
    CF_PIN_STATE     PinState,
    CF_SET_PIN_FLAGS PinFlags,
    LPOVERLAPPED     Overlapped
);

/* ==================================================================
 *  Progress reporting
 * ================================================================== */

STDAPI CfReportProviderProgress(
    CF_CONNECTION_KEY ConnectionKey,
    CF_TRANSFER_KEY   TransferKey,
    LARGE_INTEGER     ProviderProgressTotal,
    LARGE_INTEGER     ProviderProgressCompleted
);

/* ==================================================================
 *  Placeholder conversion (CfConvertToPlaceholder)
 *  Minimum Windows target: 10.0.17763.0 (RS5 / 1809)
 * ================================================================== */

typedef enum CF_CONVERT_FLAGS {
    CF_CONVERT_FLAG_NONE                        = 0x00000000,
    CF_CONVERT_FLAG_MARK_IN_SYNC               = 0x00000001,
    CF_CONVERT_FLAG_DEHYDRATE                   = 0x00000002,
    CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION = 0x00000004,
    CF_CONVERT_FLAG_ALWAYS_FULL                 = 0x00000008,
} CF_CONVERT_FLAGS;
DEFINE_ENUM_FLAG_OPERATORS(CF_CONVERT_FLAGS);

/* Convert an existing NTFS file/directory into a Cloud Filter placeholder.
 * Once converted, the OS will call FETCH_PLACEHOLDERS for directory entries
 * that are placeholders with on-demand population enabled.
 *
 * Parameters:
 *   FileHandle          — open handle (FILE_READ_ATTRIBUTES | FILE_WRITE_ATTRIBUTES)
 *   FileIdentity/Length — opaque provider identity (may be NULL/0)
 *   ConvertFlags        — CF_CONVERT_FLAG_MARK_IN_SYNC | CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION
 *   ConvertUsn          — optional output USN (may be NULL)
 *   Overlapped          — async completion (NULL = synchronous)
 */
STDAPI CfConvertToPlaceholder(
    HANDLE           FileHandle,
    LPCVOID          FileIdentity,
    DWORD            FileIdentityLength,
    CF_CONVERT_FLAGS ConvertFlags,
    USN*             ConvertUsn,
    LPOVERLAPPED     Overlapped
);

#endif /* _WIN32 */
#endif /* GHD_CFAPI_COMPAT_H */
