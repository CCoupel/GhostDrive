// Package local implements a GhostDrive storage backend that reads from and
// writes to a local (or network-mounted) directory.
//
// # Configuration
//
// Required param: "rootPath" — absolute path to the local root directory.
// All remote paths passed to the backend methods are resolved relative to
// rootPath.  Example config:
//
//	BackendConfig{
//	    Type:   "local",
//	    Params: map[string]string{"rootPath": "/mnt/nas/GhostDrive"},
//	}
//
// # Watch
//
// Watch uses fsnotify (v1.9.0+) for native filesystem notifications.
// Events are emitted as soon as the OS delivers them; the minimum detectable
// interval is OS-dependent (typically < 100 ms on Linux inotify / Windows
// ReadDirectoryChangesW).
package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/fsnotify/fsnotify"
)

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	// ErrNotConnected wraps the shared sentinel so callers can use
	// errors.Is against both this and plugins.ErrNotConnected.
	ErrNotConnected = fmt.Errorf("local: %w", plugins.ErrNotConnected)
	// ErrFileNotFound wraps the shared sentinel so callers can use
	// errors.Is against both this and plugins.ErrFileNotFound.
	ErrFileNotFound = fmt.Errorf("local: %w", plugins.ErrFileNotFound)
)

// ─── Backend struct ───────────────────────────────────────────────────────────

// Backend implements plugins.StorageBackend for a local or mounted directory.
type Backend struct {
	mu        gosync.RWMutex
	connected bool
	rootPath  string
}

// New creates an unconnected Backend. Call Connect before any other method.
func New() *Backend { return &Backend{} }

func init() {
	plugins.Register("local", func() plugins.StorageBackend { return New() })
}

// ─── Identification ───────────────────────────────────────────────────────────

// Name returns the plugin identifier ("local").
func (b *Backend) Name() string { return "local" }

// ─── Connection ───────────────────────────────────────────────────────────────

// Connect initialises the backend from the provided configuration.
// Required Params: "rootPath" — absolute path to the local root directory.
// Returns an error if rootPath is missing, does not exist, or is not a directory.
func (b *Backend) Connect(cfg plugins.BackendConfig) error {
	rootPath, ok := cfg.Params["rootPath"]
	if !ok || rootPath == "" {
		return fmt.Errorf("local: connect: missing 'rootPath' param")
	}

	info, err := os.Stat(rootPath)
	if err != nil {
		return fmt.Errorf("local: connect: rootPath inaccessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local: connect: rootPath is not a directory: %s", rootPath)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.rootPath = rootPath
	b.connected = true
	return nil
}

// Disconnect marks the backend as disconnected and clears internal state.
// Safe to call on an already-disconnected backend (no-op).
func (b *Backend) Disconnect() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = false
	b.rootPath = ""
	return nil
}

// IsConnected returns true when Connect has succeeded and Disconnect has not
// been called since. Thread-safe; does not perform I/O.
func (b *Backend) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected
}

// ─── Internal path helper ─────────────────────────────────────────────────────

// absPath resolves rel relative to rootPath and verifies that the result
// does not escape rootPath (path traversal protection).  It captures both
// b.connected and b.rootPath under a single RLock to eliminate TOCTOU races.
//
// Returns ErrNotConnected if the backend is not connected.
// Returns an error containing "s'échappe de rootPath" if rel would escape.
func (b *Backend) absPath(rel string) (string, error) {
	// Capture connected + rootPath atomically to prevent TOCTOU between the
	// IsConnected check and the subsequent use of rootPath.
	b.mu.RLock()
	connected := b.connected
	rootPath := b.rootPath
	b.mu.RUnlock()

	if !connected {
		return "", ErrNotConnected
	}

	abs := filepath.Clean(filepath.Join(rootPath, filepath.FromSlash(rel)))
	cleanRoot := filepath.Clean(rootPath)

	// Accept exactly cleanRoot or any path that starts with cleanRoot + separator.
	if abs != cleanRoot && !strings.HasPrefix(abs, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("local: chemin invalide : %q s'échappe de rootPath", rel)
	}
	return abs, nil
}

// ─── File operations ──────────────────────────────────────────────────────────

// Upload copies the local file at local to the remote path remote
// (relative to rootPath).  Parent directories under rootPath are created
// automatically if they do not exist.
// progress may be nil.
func (b *Backend) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("local: upload %s: %w", remote, err)
	}
	destPath, err := b.absPath(remote)
	if err != nil {
		return fmt.Errorf("local: upload %s: %w", remote, err)
	}

	src, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("local: upload %s: open source: %w", local, err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("local: upload %s: stat source: %w", local, err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("local: upload %s: create parent dirs: %w", remote, err)
	}

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("local: upload %s: create dest: %w", remote, err)
	}
	defer dst.Close()

	var reader io.Reader = src
	if progress != nil {
		reader = &progressReader{r: src, total: info.Size(), callback: progress}
	}
	if _, err := io.Copy(dst, reader); err != nil {
		return fmt.Errorf("local: upload %s: copy: %w", remote, err)
	}
	return nil
}

// Download copies the remote file at remote (relative to rootPath) to the
// local path local.  The parent directory of local is created if needed.
// Returns ErrFileNotFound (wrapped) when remote does not exist.
// progress may be nil.
func (b *Backend) Download(ctx context.Context, remote, local string, progress plugins.ProgressCallback) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("local: download %s: %w", remote, err)
	}
	srcPath, err := b.absPath(remote)
	if err != nil {
		return fmt.Errorf("local: download %s: %w", remote, err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("local: download %s: %w", remote, ErrFileNotFound)
		}
		return fmt.Errorf("local: download %s: open source: %w", remote, err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("local: download %s: stat source: %w", remote, err)
	}

	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return fmt.Errorf("local: download %s: create local dir: %w", local, err)
	}

	dst, err := os.Create(local)
	if err != nil {
		return fmt.Errorf("local: download %s: create local file: %w", local, err)
	}
	defer dst.Close()

	var writer io.Writer = dst
	if progress != nil {
		writer = &progressWriter{w: dst, total: info.Size(), callback: progress}
	}
	if _, err := io.Copy(writer, src); err != nil {
		return fmt.Errorf("local: download %s: copy: %w", remote, err)
	}
	return nil
}

// Delete removes the file or directory at remote (relative to rootPath).
// Returns ErrFileNotFound (wrapped) when remote does not exist.
func (b *Backend) Delete(ctx context.Context, remote string) error {
	abs, err := b.absPath(remote)
	if err != nil {
		return fmt.Errorf("local: delete %s: %w", remote, err)
	}
	if err := os.Remove(abs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("local: delete %s: %w", remote, ErrFileNotFound)
		}
		return fmt.Errorf("local: delete %s: %w", remote, err)
	}
	return nil
}

// Move renames or moves the entry at oldPath to newPath (both relative to
// rootPath).  Returns ErrFileNotFound (wrapped) when oldPath does not exist.
func (b *Backend) Move(ctx context.Context, oldPath, newPath string) error {
	absOld, err := b.absPath(oldPath)
	if err != nil {
		return fmt.Errorf("local: move %s -> %s: %w", oldPath, newPath, err)
	}
	absNew, err := b.absPath(newPath)
	if err != nil {
		return fmt.Errorf("local: move %s -> %s: %w", oldPath, newPath, err)
	}
	if err := os.Rename(absOld, absNew); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("local: move %s -> %s: %w", oldPath, newPath, ErrFileNotFound)
		}
		return fmt.Errorf("local: move %s -> %s: %w", oldPath, newPath, err)
	}
	return nil
}

// ─── Navigation ───────────────────────────────────────────────────────────────

// List returns the direct children of the directory at path (relative).
// Returns an empty (non-nil) slice when the directory is empty.
// Returns ErrFileNotFound (wrapped) when path does not exist.
func (b *Backend) List(ctx context.Context, path string) ([]plugins.FileInfo, error) {
	abs, err := b.absPath(path)
	if err != nil {
		return nil, fmt.Errorf("local: list %s: %w", path, err)
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("local: list %s: %w", path, ErrFileNotFound)
		}
		return nil, fmt.Errorf("local: list %s: %w", path, err)
	}

	result := make([]plugins.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // skip entries we cannot stat (e.g. disappeared in a race)
		}
		result = append(result, plugins.FileInfo{
			Name:    entry.Name(),
			Path:    filepath.ToSlash(filepath.Join(path, entry.Name())),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		})
	}
	return result, nil
}

// Stat returns metadata for the file or directory at path (relative).
// Returns ErrFileNotFound (wrapped) when path does not exist.
func (b *Backend) Stat(ctx context.Context, path string) (*plugins.FileInfo, error) {
	abs, err := b.absPath(path)
	if err != nil {
		return nil, fmt.Errorf("local: stat %s: %w", path, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("local: stat %s: %w", path, ErrFileNotFound)
		}
		return nil, fmt.Errorf("local: stat %s: %w", path, err)
	}

	return &plugins.FileInfo{
		Name:    info.Name(),
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

// CreateDir creates the directory at path (relative).
// No-op if it already exists.  Does NOT create intermediate parents.
func (b *Backend) CreateDir(ctx context.Context, path string) error {
	abs, err := b.absPath(path)
	if err != nil {
		return fmt.Errorf("local: mkdir %s: %w", path, err)
	}
	if err := os.Mkdir(abs, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("local: mkdir %s: %w", path, err)
	}
	return nil
}

// ─── Watch ────────────────────────────────────────────────────────────────────

// Watch monitors path (relative to rootPath) for changes using fsnotify.
// Events are emitted on the returned channel; the channel is closed when ctx
// is cancelled.  Buffer size is 64.
// Returns ErrNotConnected when the backend is not connected.
func (b *Backend) Watch(ctx context.Context, path string) (<-chan plugins.FileEvent, error) {
	abs, err := b.absPath(path)
	if err != nil {
		return nil, fmt.Errorf("local: watch %s: %w", path, err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("local: watch: create watcher: %w", err)
	}

	if err := watcher.Add(abs); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("local: watch %s: add path: %w", path, err)
	}

	ch := make(chan plugins.FileEvent, 64)

	go func() {
		defer close(ch)
		defer watcher.Close()

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				fe := toFileEvent(event)
				select {
				case ch <- fe:
				case <-ctx.Done():
					return
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				// Log fsnotify errors so they are visible without blocking Watch.
				log.Printf("local: watch: fsnotify error: %v", err)
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// toFileEvent converts an fsnotify.Event to a plugins.FileEvent.
func toFileEvent(e fsnotify.Event) plugins.FileEvent {
	fe := plugins.FileEvent{
		Path:      filepath.ToSlash(e.Name),
		Timestamp: time.Now(),
		Source:    "local",
	}
	switch {
	case e.Op.Has(fsnotify.Create):
		fe.Type = plugins.FileEventCreated
	case e.Op.Has(fsnotify.Write):
		fe.Type = plugins.FileEventModified
	case e.Op.Has(fsnotify.Remove):
		fe.Type = plugins.FileEventDeleted
	case e.Op.Has(fsnotify.Rename):
		fe.Type = plugins.FileEventRenamed
	default:
		fe.Type = plugins.FileEventModified
	}
	return fe
}

// ─── Progress helpers ─────────────────────────────────────────────────────────

// progressReader wraps an io.Reader and fires a ProgressCallback after each
// Read.  Used during Upload to report bytes-read progress.
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
// Write.  Used during Download to report bytes-written progress.
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
