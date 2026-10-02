package types

import "time"

// SyncStatus represents the synchronization state.
type SyncStatus string

const (
	SyncIdle    SyncStatus = "idle"
	SyncSyncing SyncStatus = "syncing"
	SyncPaused  SyncStatus = "paused"
	// SyncOffline indicates a transient backend unreachability (1–4 consecutive
	// Watch poll failures).  Tray icon turns orange.  Becomes SyncError after
	// watchMaxConsecutiveErrors failures (Watch closes its channel).
	SyncOffline SyncStatus = "offline"
	SyncError   SyncStatus = "error"
)

// SyncErrorInfo represents a synchronization error.
type SyncErrorInfo struct {
	Path    string    `json:"path"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

// BackendSyncError is the event payload for sync:error events (includes backendId).
type BackendSyncError struct {
	BackendID string    `json:"backendId"`
	Path      string    `json:"path"`
	Message   string    `json:"message"`
	Time      time.Time `json:"time"`
}

// BackendSyncState represents the sync state for a single backend.
type BackendSyncState struct {
	BackendID   string             `json:"backendId"`
	BackendName string             `json:"backendName"`
	Status      SyncStatus         `json:"status"`
	Progress    float64            `json:"progress"`
	CurrentFile string             `json:"currentFile"`
	Pending     int                `json:"pending"`
	Errors      []BackendSyncError `json:"errors"`
	LastSync    time.Time          `json:"lastSync"`
}

// SyncState represents the global sync state exposed to the frontend.
type SyncState struct {
	Status          SyncStatus         `json:"status"`
	Progress        float64            `json:"progress"`
	CurrentFile     string             `json:"currentFile"`
	Pending         int                `json:"pending"`
	Errors          []SyncErrorInfo    `json:"errors"`
	LastSync        time.Time          `json:"lastSync"`
	Backends        []BackendSyncState `json:"backends"`
	ActiveTransfers []ProgressEvent    `json:"activeTransfers"`
}

// BackendStatus represents the connection status of a backend.
type BackendStatus struct {
	BackendID  string `json:"backendId"`
	Connected  bool   `json:"connected"`
	Error      string `json:"error,omitempty"`
	FreeSpace  int64  `json:"freeSpace"`
	TotalSpace int64  `json:"totalSpace"`
}

// ProgressEvent represents the progress of a file transfer.
type ProgressEvent struct {
	Path       string  `json:"path"`
	Direction  string  `json:"direction"` // "upload" | "download"
	BytesDone  int64   `json:"bytesDone"`
	BytesTotal int64   `json:"bytesTotal"`
	Percent    float64 `json:"percent"`
}

// CacheStats represents statistics for the local cache.
type CacheStats struct {
	SizeMB    float64 `json:"sizeMB"`
	FileCount int     `json:"fileCount"`
	MaxSizeMB int     `json:"maxSizeMB"`
}

// ─── FileState ────────────────────────────────────────────────────────────────

// FileState represents the per-file synchronization state maintained by the
// sync Engine in its fileStates sync.Map.  The zero-value (empty string) means
// the file is not tracked — use FileStateUnknown explicitly for clarity.
//
// State transitions:
//
//	(new local file) → L  → P → U → S
//	(remote error)              U → E
//	(conflict detected)         S → C → S (auto-resolved)
//	(excluded)                  * → X
type FileState string

const (
	// FileStateLocal means the file exists locally only — never synchronised to remote.
	// Guard: engine.handleLocalEvent skips ActionDelete for L-state files (#141).
	FileStateLocal FileState = "L"

	// FileStatePending means the file is queued for upload or download.
	FileStatePending FileState = "P"

	// FileStateUploading means an upload is in progress toward the remote.
	FileStateUploading FileState = "U"

	// FileStateSynced means local and remote are identical.
	FileStateSynced FileState = "S"

	// FileStateConflict means a conflict was detected and auto-resolved (last-write-wins).
	// Emits sync:conflict event (#144).
	FileStateConflict FileState = "C"

	// FileStateError means a sync operation failed for this file.
	FileStateError FileState = "E"

	// FileStateExcluded means the file is explicitly excluded from synchronisation.
	FileStateExcluded FileState = "X"

	// FileStateUnknown is the zero-value: file not tracked or backend not found.
	FileStateUnknown FileState = ""
)
