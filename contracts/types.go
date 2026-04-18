//go:build ignore
// Ce fichier est un contrat de référence — les types réels sont dans internal/types/types.go et plugins/plugin.go

package contracts

import (
	"context"
	"time"
)

// ─── Types partagés Go ↔ Frontend (sérialisés en JSON par Wails) ───────────

// FileInfo représente un fichier ou répertoire (local ou distant).
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

// FileEventType définit les types d'événements fichier.
type FileEventType string

const (
	FileEventCreated  FileEventType = "created"
	FileEventModified FileEventType = "modified"
	FileEventDeleted  FileEventType = "deleted"
	FileEventRenamed  FileEventType = "renamed"
)

// FileEvent représente un changement détecté (local ou distant).
type FileEvent struct {
	Type      FileEventType `json:"type"`
	Path      string        `json:"path"`
	OldPath   string        `json:"oldPath,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	Source    string        `json:"source"` // "local" | "remote"
}

// SyncStatus représente l'état de la synchronisation.
type SyncStatus string

const (
	SyncIdle    SyncStatus = "idle"
	SyncSyncing SyncStatus = "syncing"
	SyncPaused  SyncStatus = "paused"
	SyncError   SyncStatus = "error"
)

// SyncError représente une erreur de synchronisation.
type SyncError struct {
	Path    string    `json:"path"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

// SyncState représente l'état global de synchronisation exposé au frontend.
type SyncState struct {
	Status      SyncStatus  `json:"status"`
	Progress    float64     `json:"progress"`
	CurrentFile string      `json:"currentFile"`
	Pending     int         `json:"pending"`
	Errors      []SyncError `json:"errors"`
	LastSync    time.Time   `json:"lastSync"`
}

// BackendConfig représente la configuration d'un backend de stockage.
type BackendConfig struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Type    string            `json:"type"` // "webdav" | "moosefs"
	Enabled bool              `json:"enabled"`
	Params  map[string]string `json:"params"`
	SyncDir string            `json:"syncDir"`
}

// BackendStatus représente l'état de connexion d'un backend.
type BackendStatus struct {
	BackendID  string `json:"backendId"`
	Connected  bool   `json:"connected"`
	Error      string `json:"error,omitempty"`
	FreeSpace  int64  `json:"freeSpace"`
	TotalSpace int64  `json:"totalSpace"`
}

// ProgressEvent représente la progression d'un transfert.
type ProgressEvent struct {
	Path       string  `json:"path"`
	Direction  string  `json:"direction"` // "upload" | "download"
	BytesDone  int64   `json:"bytesDone"`
	BytesTotal int64   `json:"bytesTotal"`
	Percent    float64 `json:"percent"`
}

// CacheStats représente les statistiques du cache local.
type CacheStats struct {
	SizeMB    float64 `json:"sizeMB"`
	FileCount int     `json:"fileCount"`
	MaxSizeMB int     `json:"maxSizeMB"`
}

// AppConfig représente la configuration globale (config.json).
type AppConfig struct {
	Version        string          `json:"version"`
	Backends       []BackendConfig `json:"backends"`
	CacheEnabled   bool            `json:"cacheEnabled"`
	CacheDir       string          `json:"cacheDir"`
	CacheSizeMaxMB int             `json:"cacheSizeMaxMB"`
	StartMinimized bool            `json:"startMinimized"`
	AutoStart      bool            `json:"autoStart"`
}

// ─── Interface Plugin StorageBackend ────────────────────────────────────────

// ProgressCallback est appelé pendant les transferts pour signaler la progression.
type ProgressCallback func(done, total int64)

// StorageBackend définit le contrat que tout plugin de stockage doit implémenter.
type StorageBackend interface {
	Name() string
	Connect(config BackendConfig) error
	Disconnect() error
	Upload(ctx context.Context, local, remote string, progress ProgressCallback) error
	Download(ctx context.Context, remote, local string, progress ProgressCallback) error
	Delete(ctx context.Context, remote string) error
	List(ctx context.Context, path string) ([]FileInfo, error)
	Stat(ctx context.Context, path string) (*FileInfo, error)
	Watch(ctx context.Context, path string) (<-chan FileEvent, error)
	CreateDir(ctx context.Context, path string) error
}
