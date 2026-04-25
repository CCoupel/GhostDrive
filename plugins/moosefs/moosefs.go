package moosefs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/fsnotify/fsnotify"
)

// Sentinel errors
var (
	ErrNotConnected = fmt.Errorf("moosefs: backend not connected")
	ErrFileNotFound = fmt.Errorf("moosefs: file not found")
)

// Backend implements plugins.StorageBackend for MooseFS via a FUSE mount point.
// V1 approach: the FUSE mount is assumed to be already mounted by the user.
// The backend treats the mount path as a regular local filesystem.
type Backend struct {
	mu        sync.RWMutex
	mountPath string
	connected bool
}

// New creates an unconnected MooseFS backend.
func New() *Backend {
	return &Backend{}
}

// Name returns the plugin identifier.
func (b *Backend) Name() string { return "moosefs" }

// Connect validates that the mount path exists and is accessible.
// Expected params: "mountPath" — path to the MooseFS FUSE mount point.
func (b *Backend) Connect(cfg plugins.BackendConfig) error {
	mountPath, ok := cfg.Params["mountPath"]
	if !ok || mountPath == "" {
		return fmt.Errorf("moosefs: connect: missing 'mountPath' param")
	}

	info, err := os.Stat(mountPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("moosefs: connect: mount path %q does not exist", mountPath)
		}
		return fmt.Errorf("moosefs: connect: stat %q: %w", mountPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("moosefs: connect: mount path %q is not a directory", mountPath)
	}

	b.mu.Lock()
	b.mountPath = mountPath
	b.connected = true
	b.mu.Unlock()

	return nil
}

// Disconnect marks the backend as disconnected.
func (b *Backend) Disconnect() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = false
	return nil
}

// IsConnected returns the current connection state.
func (b *Backend) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected
}

// Upload copies a local file to the remote path within the mount.
func (b *Backend) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	dst := b.mountedPath(remote)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("moosefs: upload: create parent dirs for %s: %w", dst, err)
	}

	src, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("moosefs: upload: open local %s: %w", local, err)
	}
	defer src.Close()

	srcInfo, err := src.Stat()
	if err != nil {
		return fmt.Errorf("moosefs: upload: stat local %s: %w", local, err)
	}
	total := srcInfo.Size()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("moosefs: upload: create remote %s: %w", dst, err)
	}
	defer dstFile.Close()

	var reader io.Reader = src
	if progress != nil {
		reader = &progressReader{r: src, total: total, callback: progress}
	}

	if _, err := io.Copy(dstFile, reader); err != nil {
		return fmt.Errorf("moosefs: upload: copy %s -> %s: %w", local, dst, err)
	}

	return nil
}

// Download copies a file from the mount to a local path.
func (b *Backend) Download(ctx context.Context, remote, local string, progress plugins.ProgressCallback) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	src := b.mountedPath(remote)
	srcFile, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("moosefs: download %s: %w", remote, ErrFileNotFound)
		}
		return fmt.Errorf("moosefs: download: open %s: %w", src, err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("moosefs: download: stat %s: %w", src, err)
	}

	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return fmt.Errorf("moosefs: download: create parent dirs for %s: %w", local, err)
	}

	dstFile, err := os.Create(local)
	if err != nil {
		return fmt.Errorf("moosefs: download: create local %s: %w", local, err)
	}
	defer dstFile.Close()

	var reader io.Reader = srcFile
	if progress != nil {
		reader = &progressReader{r: srcFile, total: srcInfo.Size(), callback: progress}
	}

	if _, err := io.Copy(dstFile, reader); err != nil {
		return fmt.Errorf("moosefs: download: copy %s -> %s: %w", src, local, err)
	}

	return nil
}

// Delete removes a file or directory from the mount.
func (b *Backend) Delete(ctx context.Context, remote string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	path := b.mountedPath(remote)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("moosefs: delete %s: %w", remote, err)
	}
	return nil
}

// Move renames a file or directory within the mount.
func (b *Backend) Move(ctx context.Context, oldPath, newPath string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	src := b.mountedPath(oldPath)
	dst := b.mountedPath(newPath)
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("moosefs: move %s -> %s: %w", oldPath, newPath, err)
	}
	return nil
}

// List returns the contents of a directory on the mount.
func (b *Backend) List(ctx context.Context, path string) ([]plugins.FileInfo, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	dir := b.mountedPath(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("moosefs: list %s: %w", path, ErrFileNotFound)
		}
		return nil, fmt.Errorf("moosefs: list %s: %w", path, err)
	}

	result := make([]plugins.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, plugins.FileInfo{
			Name:    entry.Name(),
			Path:    filepath.ToSlash(filepath.Join(path, entry.Name())),
			Size:    info.Size(),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime(),
		})
	}
	return result, nil
}

// Stat returns information about a single file on the mount.
func (b *Backend) Stat(ctx context.Context, path string) (*plugins.FileInfo, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	full := b.mountedPath(path)
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("moosefs: stat %s: %w", path, ErrFileNotFound)
		}
		return nil, fmt.Errorf("moosefs: stat %s: %w", path, err)
	}

	return &plugins.FileInfo{
		Name:    info.Name(),
		Path:    path,
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}

// CreateDir creates a directory on the mount.
func (b *Backend) CreateDir(ctx context.Context, path string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	full := b.mountedPath(path)
	if err := os.MkdirAll(full, 0755); err != nil {
		return fmt.Errorf("moosefs: mkdir %s: %w", path, err)
	}
	return nil
}

// Watch monitors the mounted directory using fsnotify and emits FileEvents.
func (b *Backend) Watch(ctx context.Context, path string) (<-chan plugins.FileEvent, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	full := b.mountedPath(path)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("moosefs: watch: create watcher: %w", err)
	}

	if err := watcher.Add(full); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("moosefs: watch: add path %s: %w", full, err)
	}

	ch := make(chan plugins.FileEvent, 64)

	go func() {
		defer close(ch)
		defer watcher.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case fsEvent, ok := <-watcher.Events:
				if !ok {
					return
				}
				evtType := mooseFSEventType(fsEvent.Op)
				if evtType == "" {
					continue
				}
				select {
				case ch <- plugins.FileEvent{
					Type:      evtType,
					Path:      filepath.ToSlash(fsEvent.Name),
					Timestamp: time.Now(),
					Source:    "remote",
				}:
				case <-ctx.Done():
					return
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return ch, nil
}

// ─── Internal helpers ────────────────────────────────────────────────────────

// ─── Quota ────────────────────────────────────────────────────────────────────

// GetQuota is not supported by the MooseFS backend; returns (-1, -1, nil).
func (b *Backend) GetQuota(_ context.Context) (int64, int64, error) {
	return -1, -1, nil
}

func (b *Backend) mountedPath(remote string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return filepath.Join(b.mountPath, filepath.FromSlash(remote))
}

func mooseFSEventType(op fsnotify.Op) plugins.FileEventType {
	switch {
	case op&fsnotify.Create != 0:
		return plugins.FileEventCreated
	case op&fsnotify.Write != 0:
		return plugins.FileEventModified
	case op&fsnotify.Remove != 0:
		return plugins.FileEventDeleted
	case op&fsnotify.Rename != 0:
		return plugins.FileEventRenamed
	default:
		return ""
	}
}

// ─── Progress helper ─────────────────────────────────────────────────────────

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
