// Package plugins defines the StorageBackend interface and shared types used by
// all GhostDrive storage backend plugins.
//
// # Architecture
//
// Each backend is an independent Go package under plugins/<name>/ that
// implements the StorageBackend interface. Plugins are compiled into the main
// binary — there is no dynamic loading (.dll / .so / gRPC). The registry in
// plugins/registry.go maps backend type names to factory functions.
//
// # Plugin lifecycle
//
//	Connect → Watch → Upload / Download / Delete / Move → Disconnect
//
// A plugin must not perform any I/O before Connect is called and must not
// panic after Disconnect is called.
package plugins

import (
	"context"
	"errors"
	"time"
)

// ─── Sentinel errors ─────────────────────────────────────────────────────────

// ErrNotConnected is returned by any operation that requires an active
// connection when Connect has not been called or has failed.
// Plugins should wrap this sentinel:
//
//	return fmt.Errorf("myplugin: upload: %w", plugins.ErrNotConnected)
var ErrNotConnected = errors.New("backend: not connected")

// ErrFileNotFound is returned when the requested remote path does not exist.
// Plugins should wrap this sentinel:
//
//	return fmt.Errorf("myplugin: stat %s: %w", path, plugins.ErrFileNotFound)
var ErrFileNotFound = errors.New("backend: file not found")

// ─── Shared types ─────────────────────────────────────────────────────────────

// FileInfo represents a file or directory entry, either local or remote.
// Plugins populate all applicable fields; leave zero-value for fields that the
// backend does not provide (e.g. ETag on backends that do not support it).
type FileInfo struct {
	// Name is the base name of the entry (no path separator).
	Name string `json:"name"`
	// Path is the slash-separated path as returned by the backend, relative to
	// the configured RemotePath root. Never starts with a drive letter.
	Path string `json:"path"`
	// Size is the byte count of the file content. Zero for directories.
	Size int64 `json:"size"`
	// IsDir is true when the entry is a directory.
	IsDir bool `json:"isDir"`
	// ModTime is the last-modified timestamp reported by the backend.
	ModTime time.Time `json:"modTime"`
	// ETag is the HTTP entity tag or equivalent version token, when available.
	ETag string `json:"etag"`
	// IsPlaceholder indicates that the file is a Files-On-Demand placeholder
	// (content not yet hydrated locally).
	IsPlaceholder bool `json:"isPlaceholder"`
	// IsCached indicates that the file content is present in the local cache.
	IsCached bool `json:"isCached"`
}

// FileEventType categorises a change detected on a watched path.
type FileEventType string

const (
	// FileEventCreated fires when a new file or directory appears.
	FileEventCreated FileEventType = "created"
	// FileEventModified fires when an existing entry's content or metadata changes.
	FileEventModified FileEventType = "modified"
	// FileEventDeleted fires when an entry is removed.
	FileEventDeleted FileEventType = "deleted"
	// FileEventRenamed fires when an entry is moved; OldPath is populated.
	FileEventRenamed FileEventType = "renamed"
)

// FileEvent represents a detected change emitted by Watch.
// The Source field distinguishes events originating locally ("local") from
// those originating on the remote backend ("remote").
type FileEvent struct {
	// Type is the kind of change.
	Type FileEventType `json:"type"`
	// Path is the slash-separated remote path of the affected entry.
	Path string `json:"path"`
	// OldPath is the previous path for FileEventRenamed events; empty otherwise.
	OldPath string `json:"oldPath,omitempty"`
	// Timestamp is when the event was detected (not necessarily when the change occurred).
	Timestamp time.Time `json:"timestamp"`
	// Source is "local" when the change originates on the local filesystem,
	// or "remote" when detected on the backend.
	Source string `json:"source"` // "local" | "remote"
}

// BackendConfig carries the configuration for a single backend instance.
// It is read from the application config file and passed verbatim to Connect.
type BackendConfig struct {
	// ID is a user-assigned unique identifier for this backend instance.
	ID string `json:"id"`
	// Name is a human-readable label displayed in the UI.
	Name string `json:"name"`
	// Type selects which plugin handles this backend.
	// Valid values: "local" — dynamic plugin types registered at runtime via go-plugin
	Type string `json:"type"`
	// Enabled controls whether this backend participates in sync.
	Enabled bool `json:"enabled"`
	// AutoSync controls whether the sync engine starts automatically when the
	// backend connects. When false, the user must trigger sync manually via
	// ForceSync. Default: false (opt-in). Zero-value preserves backward
	// compatibility with existing config files that lack this field.
	AutoSync bool `json:"autoSync"`
	// Params contains plugin-specific key/value configuration.
	// See contracts/backend-config.md for the param schema of each plugin type.
	Params map[string]string `json:"params"`
	// SyncDir is the absolute local path to the folder being synchronised.
	// Deprecated: use LocalPath instead; kept for backward-compatibility with
	// existing config files. AddBackend sets SyncDir = LocalPath automatically.
	SyncDir string `json:"syncDir"`
	// RemotePath is the root path on the remote storage (e.g. "/GhostDrive").
	// All plugin operations use slash-separated paths relative to this root.
	RemotePath string `json:"remotePath"`
	// LocalPath is the local sync point — where GhostDrive creates the local
	// copy of the remote data.  In Auto mode it is derived from GhostDriveRoot
	// and the backend Name; in Manual mode it is set by the user.
	LocalPath string `json:"localPath"`
	// Warning carries a non-blocking validation message returned by AddBackend
	// when a soft conflict is detected (e.g. rootPath already used by another
	// backend).  Empty when no warning applies.
	Warning string `json:"warning,omitempty"`
}

// ProgressCallback is invoked periodically during Upload and Download to
// report transfer progress. done is the number of bytes transferred so far;
// total is the expected total (may be -1 when unknown). The callback must
// not block.
type ProgressCallback func(done, total int64)

// ─── Interface ───────────────────────────────────────────────────────────────

// StorageBackend is the contract that every GhostDrive storage plugin must
// implement. All methods must be safe to call concurrently from multiple
// goroutines unless stated otherwise.
//
// Path conventions:
//   - All remote paths are slash-separated (forward slash, even on Windows).
//   - Paths are relative to the RemotePath root supplied at Connect time.
//   - Leading slashes are accepted but not required.
//   - Plugins must never interpret or mangle Windows drive letters.
//
// Error conventions:
//   - Wrap ErrNotConnected when the backend is not connected.
//   - Wrap ErrFileNotFound when the requested path does not exist.
//   - Always prefix with the plugin name: fmt.Errorf("myplugin: op: %w", err).
//   - Never return nil when Connect has not been called and a connection is
//     required.
type StorageBackend interface {
	// ── Identification ──────────────────────────────────────────────────────

	// Name returns the plugin identifier in lowercase (e.g. "webdav", "local").
	// The value must match the BackendConfig.Type field used in the config file.
	// Immutable; may be called before Connect.
	Name() string

	// ── Connection ──────────────────────────────────────────────────────────

	// Connect initialises the backend using the provided configuration.
	// It must validate required Params and probe the backend (e.g. a PROPFIND
	// for WebDAV, or verifying the root path exists for local).
	// Returns a descriptive error if the backend is unreachable or misconfigured.
	// Calling Connect on an already-connected backend reconnects it.
	Connect(config BackendConfig) error

	// Disconnect releases any resources held by the backend (open connections,
	// background goroutines, etc.) and marks it as disconnected.
	// After Disconnect, all operations except Connect must return ErrNotConnected.
	// Safe to call on an already-disconnected backend (no-op).
	Disconnect() error

	// IsConnected returns true if Connect has succeeded and Disconnect has not
	// been called since. Thread-safe; does not perform I/O.
	IsConnected() bool

	// ── File operations ─────────────────────────────────────────────────────

	// Upload copies the local file at local to the remote path remote.
	// Intermediate directories on the remote are NOT created automatically;
	// call CreateDir first if needed.
	// progress may be nil. If non-nil it is called with monotonically increasing
	// done values.
	// Pre-condition: IsConnected() == true, else returns ErrNotConnected.
	Upload(ctx context.Context, local, remote string, progress ProgressCallback) error

	// Download copies the remote file at remote to the local path local.
	// The parent directory of local is created if it does not exist.
	// progress may be nil.
	// Returns ErrFileNotFound (wrapped) when remote does not exist.
	// Pre-condition: IsConnected() == true, else returns ErrNotConnected.
	Download(ctx context.Context, remote, local string, progress ProgressCallback) error

	// Delete removes the file or directory at remote.
	// Removing a non-empty directory is implementation-defined (plugins may
	// refuse with an error or recursively delete).
	// Returns ErrFileNotFound (wrapped) when remote does not exist.
	// Pre-condition: IsConnected() == true, else returns ErrNotConnected.
	Delete(ctx context.Context, remote string) error

	// Move renames or moves the entry at oldPath to newPath on the remote.
	// Overwrites newPath if it already exists.
	// Pre-condition: IsConnected() == true, else returns ErrNotConnected.
	Move(ctx context.Context, oldPath, newPath string) error

	// ── Navigation ──────────────────────────────────────────────────────────

	// List returns the direct children of the directory at path.
	// The directory entry itself is NOT included in the result.
	// Returns an empty slice (not nil) when the directory is empty.
	// Returns ErrFileNotFound (wrapped) when path does not exist or is a file.
	// Pre-condition: IsConnected() == true, else returns nil, ErrNotConnected.
	List(ctx context.Context, path string) ([]FileInfo, error)

	// Stat returns metadata for the file or directory at path.
	// Returns ErrFileNotFound (wrapped) when path does not exist.
	// Pre-condition: IsConnected() == true, else returns nil, ErrNotConnected.
	Stat(ctx context.Context, path string) (*FileInfo, error)

	// CreateDir creates the directory at path.
	// If the directory already exists, the call is a no-op (no error).
	// Intermediate parent directories are NOT created; use recursive calls.
	// Pre-condition: IsConnected() == true, else returns ErrNotConnected.
	CreateDir(ctx context.Context, path string) error

	// ── Watch ────────────────────────────────────────────────────────────────

	// Watch starts monitoring path for changes and emits FileEvents on the
	// returned channel. The channel is closed when ctx is cancelled.
	// Implementations may use native push notifications (inotify, FSEvents) or
	// polling; document the approach and the minimum detectable interval.
	// The channel buffer size should be at least 64 to absorb burst events.
	// Pre-condition: IsConnected() == true, else returns nil, ErrNotConnected.
	Watch(ctx context.Context, path string) (<-chan FileEvent, error)

	// ── Quota ────────────────────────────────────────────────────────────────

	// GetQuota returns the free and total space (in bytes) for the backend's
	// storage.  Plugins that do not support quota reporting must return
	// (-1, -1, nil) rather than an error.
	// Pre-condition: IsConnected() == true, else returns 0, 0, ErrNotConnected.
	GetQuota(ctx context.Context) (free, total int64, err error)
}
