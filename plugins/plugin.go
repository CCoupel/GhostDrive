package plugins

import (
	"context"
	"time"
)

// FileInfo represents a file or directory (local or remote).
type FileInfo struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	Size          int64     `json:"size"`
	IsDir         bool      `json:"isDir"`
	ModTime       time.Time `json:"modTime"`
	ETag          string    `json:"etag"`
	IsPlaceholder bool      `json:"isPlaceholder"`
	IsCached      bool      `json:"isCached"`
}

// FileEventType defines the types of file events.
type FileEventType string

const (
	FileEventCreated  FileEventType = "created"
	FileEventModified FileEventType = "modified"
	FileEventDeleted  FileEventType = "deleted"
	FileEventRenamed  FileEventType = "renamed"
)

// FileEvent represents a detected change (local or remote).
type FileEvent struct {
	Type      FileEventType `json:"type"`
	Path      string        `json:"path"`
	OldPath   string        `json:"oldPath,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	Source    string        `json:"source"` // "local" | "remote"
}

// BackendConfig represents the configuration of a storage backend.
type BackendConfig struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Type    string            `json:"type"` // "webdav" | "moosefs"
	Enabled bool              `json:"enabled"`
	Params  map[string]string `json:"params"`
	SyncDir string            `json:"syncDir"`
}

// ProgressCallback is called during transfers to report progress.
type ProgressCallback func(done, total int64)

// StorageBackend defines the contract that every storage plugin must implement.
type StorageBackend interface {
	// Identification
	Name() string

	// Connection
	Connect(config BackendConfig) error
	Disconnect() error
	IsConnected() bool

	// File operations
	Upload(ctx context.Context, local, remote string, progress ProgressCallback) error
	Download(ctx context.Context, remote, local string, progress ProgressCallback) error
	Delete(ctx context.Context, remote string) error
	Move(ctx context.Context, oldPath, newPath string) error

	// Navigation
	List(ctx context.Context, path string) ([]FileInfo, error)
	Stat(ctx context.Context, path string) (*FileInfo, error)
	CreateDir(ctx context.Context, path string) error

	// Watch (for real-time sync)
	Watch(ctx context.Context, path string) (<-chan FileEvent, error)
}
