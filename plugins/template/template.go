//go:build ignore

// Package template is a copy-paste starting point for a new GhostDrive storage
// backend plugin. It implements the plugins.StorageBackend interface with stub
// methods so the project compiles immediately; replace each stub with real logic.
//
// # How to use this template
//
//  1. Copy this file to plugins/<name>/<name>.go
//  2. Rename the package declaration from `template` to `<name>`
//  3. Remove the `//go:build ignore` line at the top of the file
//  4. Replace every occurrence of "template" in identifiers and comments with <name>
//  5. Implement each method (recommended order: Connect, Stat, List, Upload,
//     Download, Delete, Move, CreateDir, Watch)
//  6. Register the plugin in plugins/registry.go
//  7. Document the Params schema in contracts/backend-config.md
//
// See docs/plugin-development.md for the full step-by-step guide.
package template

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/CCoupel/GhostDrive/plugins"
)

// ─── Sentinel errors ─────────────────────────────────────────────────────────

// Wrap the shared sentinels so callers can use errors.Is against both the
// plugin-specific and the shared sentinel at once.
var (
	// ErrNotConnected is returned by operations that require an active connection.
	ErrNotConnected = fmt.Errorf("template: %w", plugins.ErrNotConnected)
	// ErrFileNotFound is returned when the requested remote path does not exist.
	ErrFileNotFound = fmt.Errorf("template: %w", plugins.ErrFileNotFound)
)

// ─── Backend struct ───────────────────────────────────────────────────────────

// Backend implements plugins.StorageBackend for the <name> storage system.
type Backend struct {
	mu        sync.RWMutex
	connected bool
	// TODO: add backend-specific fields here (e.g. client, baseURL, rootPath…)
}

// New creates an unconnected Backend. Call Connect before any other method.
func New() *Backend {
	return &Backend{}
}

// ─── Identification ───────────────────────────────────────────────────────────

// Name returns the plugin identifier. Must match BackendConfig.Type in the
// config file.
func (b *Backend) Name() string { return "template" }

// ─── Connection ───────────────────────────────────────────────────────────────

// Connect initialises the backend from the provided configuration.
// Required Params: TODO — document expected keys here.
func (b *Backend) Connect(cfg plugins.BackendConfig) error {
	// TODO: validate required params
	// example:
	//   rootPath, ok := cfg.Params["rootPath"]
	//   if !ok || rootPath == "" {
	//       return fmt.Errorf("template: connect: missing 'rootPath' param")
	//   }

	// TODO: probe the backend (open connection, verify path exists, etc.)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = true
	return nil
}

// Disconnect releases resources and marks the backend as disconnected.
func (b *Backend) Disconnect() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	// TODO: close open connections, stop background goroutines, etc.
	b.connected = false
	return nil
}

// IsConnected returns true when Connect has succeeded and Disconnect has not
// been called. Thread-safe; does not perform I/O.
func (b *Backend) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected
}

// ─── File operations ──────────────────────────────────────────────────────────

// Upload copies the local file at local to the remote path remote.
// progress may be nil; if non-nil it is called with monotonically increasing
// (done, total) byte counts.
func (b *Backend) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}
	// TODO: open local file, stream to remote
	// Use progressReader below if progress != nil
	_ = local
	_ = remote
	_ = progress
	return errors.New("template: Upload not implemented") // TODO: remove when implemented
}

// Download copies the remote file at remote to the local path local.
// The parent directory of local is created if it does not exist.
func (b *Backend) Download(ctx context.Context, remote, local string, progress plugins.ProgressCallback) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}
	// TODO: open/create local file, stream from remote
	// Use progressWriter below if progress != nil
	_ = remote
	_ = local
	_ = progress
	return errors.New("template: Download not implemented") // TODO: remove when implemented
}

// Delete removes the file or directory at remote.
func (b *Backend) Delete(ctx context.Context, remote string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}
	// TODO: delete remote entry; return ErrFileNotFound (wrapped) if absent
	_ = remote
	return errors.New("template: Delete not implemented") // TODO: remove when implemented
}

// Move renames or relocates the entry at oldPath to newPath on the remote.
func (b *Backend) Move(ctx context.Context, oldPath, newPath string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}
	// TODO: rename/move entry on the remote
	_ = oldPath
	_ = newPath
	return errors.New("template: Move not implemented") // TODO: remove when implemented
}

// ─── Navigation ───────────────────────────────────────────────────────────────

// List returns the direct children of the directory at path (never nil, never
// includes the directory itself).
func (b *Backend) List(ctx context.Context, path string) ([]plugins.FileInfo, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}
	// TODO: list directory contents; return empty slice if empty
	_ = path
	return nil, errors.New("template: List not implemented") // TODO: remove when implemented
}

// Stat returns metadata for the file or directory at path.
func (b *Backend) Stat(ctx context.Context, path string) (*plugins.FileInfo, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}
	// TODO: return FileInfo for path; return ErrFileNotFound (wrapped) if absent
	_ = path
	return nil, errors.New("template: Stat not implemented") // TODO: remove when implemented
}

// CreateDir creates a directory at path. No-op if it already exists.
// Does NOT create intermediate parent directories.
func (b *Backend) CreateDir(ctx context.Context, path string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}
	// TODO: create directory; treat "already exists" as success
	_ = path
	return errors.New("template: CreateDir not implemented") // TODO: remove when implemented
}

// ─── Watch ────────────────────────────────────────────────────────────────────

// GetQuota is not supported by this backend; returns (-1, -1, nil).
func (b *Backend) GetQuota(_ context.Context) (int64, int64, error) {
	return -1, -1, nil
}

// Watch starts monitoring path for changes. The returned channel is closed
// when ctx is cancelled. Buffer size must be at least 64.
func (b *Backend) Watch(ctx context.Context, path string) (<-chan plugins.FileEvent, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	ch := make(chan plugins.FileEvent, 64)

	go func() {
		defer close(ch)
		// TODO: replace with native events or a polling loop
		// Example polling skeleton:
		//
		//   ticker := time.NewTicker(30 * time.Second)
		//   defer ticker.Stop()
		//   for {
		//       select {
		//       case <-ticker.C:
		//           // compare snapshot with previous; send events to ch
		//       case <-ctx.Done():
		//           return
		//       }
		//   }
		<-ctx.Done()
	}()

	return ch, nil
}

// ─── Progress helpers (copy-paste from plugins/webdav) ────────────────────────

// progressReader wraps an io.Reader and fires a ProgressCallback after each
// Read. Use it when streaming a local file to the remote backend.
type progressReader struct {
	r        io.Reader
	total    int64
	done     int64
	callback plugins.ProgressCallback
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.r.Read(p)
	pr.done += int64(n)
	if pr.callback != nil {
		pr.callback(pr.done, pr.total)
	}
	return
}

// progressWriter wraps an io.Writer and fires a ProgressCallback after each
// Write. Use it when streaming a remote file to a local destination.
type progressWriter struct {
	w        io.Writer
	total    int64
	done     int64
	callback plugins.ProgressCallback
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.w.Write(p)
	pw.done += int64(n)
	if pw.callback != nil {
		pw.callback(pw.done, pw.total)
	}
	return
}
