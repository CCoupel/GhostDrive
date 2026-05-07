// Package moosefs implements a GhostDrive StorageBackend for MooseFS clusters.
//
// # Architecture
//
// The backend connects to a MooseFS master server via a minimal TCP client
// (mfsclient) that speaks the binary frame protocol defined in
// plugins/moosefs/internal/mfsclient/protocol.go.
//
// All remote paths are slash-separated strings.  Internally, paths are
// translated to MooseFS nodeIDs by walking the directory tree from
// mfsclient.RootNodeID using ReadDir calls (see resolve.go).
//
// # Configuration (BackendConfig.Params)
//
//   - masterHost (required) — IP or hostname of the MooseFS master server
//   - masterPort (default "9421") — TCP port of the master
//   - subDir (default "/") — base directory on the cluster; all paths are
//     relative to this root
//   - pollInterval (default "30000") — Watch polling interval in milliseconds
//
// # Limitations (v1.5.x)
//
//   - Move is implemented as Download + Delete + Upload (no native RENAME).
//   - GetQuota always returns (-1, -1, nil) — MooseFS quota via FUSE protocol
//     is not exposed in the minimal v1.5.x implementation.
package moosefs

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/CCoupel/GhostDrive/plugins/moosefs/internal/mfsclient"
)

// ─── Sentinel errors ──────────────────────────────────────────────────────────

var (
	// ErrNotConnected wraps the shared sentinel.
	ErrNotConnected = fmt.Errorf("moosefs: %w", plugins.ErrNotConnected)
	// ErrFileNotFound wraps the shared sentinel.
	ErrFileNotFound = fmt.Errorf("moosefs: %w", plugins.ErrFileNotFound)
)

// chunkSize is the I/O chunk size used by Upload and Download.
const chunkSize = 64 * 1024 // 64 KiB

// ─── Backend ──────────────────────────────────────────────────────────────────

// Backend is the MooseFS StorageBackend implementation.
// All exported methods are safe for concurrent use.
type Backend struct {
	mu        sync.RWMutex
	connected bool
	client    *mfsclient.Client
	subDir    string // backend root on the cluster (default "/")
	pollMs    int    // Watch polling interval in milliseconds
	lastCfg   plugins.BackendConfig
}

// New returns an unconnected Backend.  Call Connect before any other method.
func New() *Backend { return &Backend{} }

// ─── Identification ───────────────────────────────────────────────────────────

// Name returns "moosefs".
func (b *Backend) Name() string { return "moosefs" }

// Describe returns the static plugin descriptor used by the UI to generate the
// Zone 2 (Remote) configuration form.  Safe to call before Connect.
func (b *Backend) Describe() plugins.PluginDescriptor {
	return plugins.PluginDescriptor{
		Type:        "moosefs",
		DisplayName: "MooseFS",
		Description: "Synchronise via un cluster MooseFS (protocole natif TCP)",
		Params: []plugins.ParamSpec{
			{
				Key:         "masterHost",
				Label:       "Adresse master",
				Type:        plugins.ParamTypeString,
				Required:    true,
				Placeholder: "192.168.1.10",
			},
			{
				Key:      "masterPort",
				Label:    "Port master",
				Type:     plugins.ParamTypeNumber,
				Required: false,
				Default:  "9421",
			},
			{
				Key:         "subDir",
				Label:       "Sous-répertoire",
				Type:        plugins.ParamTypeString,
				Required:    false,
				Default:     "/",
				Placeholder: "/GhostDrive",
			},
			{
				Key:      "pollInterval",
				Label:    "Intervalle Watch (ms)",
				Type:     plugins.ParamTypeNumber,
				Required: false,
				Default:  "30000",
			},
		},
	}
}

// ─── Connection ───────────────────────────────────────────────────────────────

// Connect initialises the backend using cfg.
//
// Required Params:
//   - "masterHost" — MooseFS master IP or hostname
//
// Optional Params:
//   - "masterPort"   — TCP port (default 9421)
//   - "subDir"       — base path on the cluster (default "/")
//   - "pollInterval" — Watch poll interval in milliseconds (default 30000)
//
// Connect probes the server by calling GetAttr on the root node.
func (b *Backend) Connect(cfg plugins.BackendConfig) error {
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}

	masterHost := strings.TrimSpace(cfg.Params["masterHost"])
	if masterHost == "" {
		return fmt.Errorf("moosefs: connect: 'masterHost' param is required")
	}

	masterPort := 9421
	if s := cfg.Params["masterPort"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			masterPort = n
		}
	}

	subDir := "/"
	if s := cfg.Params["subDir"]; s != "" {
		subDir = s
	}

	pollMs := 30_000
	if s := cfg.Params["pollInterval"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			pollMs = n
		}
	}

	log.Printf("connect: dialing %s:%d subDir=%q", masterHost, masterPort, subDir)
	client, err := mfsclient.Dial(masterHost, masterPort)
	if err != nil {
		log.Printf("connect: dial failed: %v", err)
		return fmt.Errorf("moosefs: connect: %w", err)
	}

	// Authenticate with the master server.
	if err := client.Register(); err != nil {
		_ = client.Close()
		log.Printf("connect: register failed: %v", err)
		return fmt.Errorf("moosefs: connect: register: %w", err)
	}
	log.Printf("connect: registered sessionID=%d", client.SessionID())

	// Probe: verify that the root node is reachable.
	if _, probeErr := client.GetAttr(mfsclient.RootNodeID); probeErr != nil {
		_ = client.Close()
		log.Printf("connect: probe failed: %v", probeErr)
		return fmt.Errorf("moosefs: connect: probe failed: %w", probeErr)
	}
	log.Printf("connect: ready")

	b.mu.Lock()
	defer b.mu.Unlock()

	// Disconnect previous client if any.
	if b.client != nil {
		_ = b.client.Close()
	}

	b.connected = true
	b.client = client
	b.subDir = subDir
	b.pollMs = pollMs
	b.lastCfg = cfg
	return nil
}

// Disconnect closes the TCP connection and marks the backend as disconnected.
// Safe to call on an already-disconnected backend (no-op).
func (b *Backend) Disconnect() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.connected {
		return nil
	}
	b.connected = false
	if b.client != nil {
		_ = b.client.Close()
		b.client = nil
	}
	return nil
}

// IsConnected returns true when Connect has succeeded and Disconnect has not
// been called since.  Thread-safe; does not perform I/O.
func (b *Backend) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// state returns a consistent snapshot of (connected, client, subDir) under
// a read lock.  The caller must not mutate the returned client.
func (b *Backend) state() (connected bool, c *mfsclient.Client, subDir string, pollMs int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected, b.client, b.subDir, b.pollMs
}

// ─── File operations ──────────────────────────────────────────────────────────

// Upload reads the local file at local and writes it to the remote path
// remote in 64 KiB chunks via Mknod + Write.
// The remote parent directory must already exist.
func (b *Backend) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	connected, c, subDir, _ := b.state()
	if !connected {
		return ErrNotConnected
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("moosefs: upload %s: %w", remote, err)
	}

	// Open local file.
	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("moosefs: upload %s: open local: %w", remote, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("moosefs: upload %s: stat local: %w", remote, err)
	}
	totalSize := info.Size()

	// Resolve parent and base name.
	parentID, baseName, err := resolveParent(ctx, c, subDir, remote)
	if err != nil {
		return fmt.Errorf("moosefs: upload %s: resolve parent: %w", remote, err)
	}

	// If the remote file already exists, unlink it before creating a new node.
	// This avoids a silent corruption bug: without the unlink, a shorter
	// replacement would leave stale bytes beyond the new EOF because the server
	// does not truncate on Mknod when the node already exists.
	if _, statErr := resolvePath(ctx, c, subDir, remote); statErr == nil {
		// Best-effort: ignore the error — if Unlink fails the subsequent Mknod
		// returns the existing nodeID, which Write will overwrite from offset 0.
		_ = c.Unlink(parentID, baseName)
	}

	// Create the remote node.
	nodeID, err := c.Mknod(parentID, baseName, 0o644)
	if err != nil {
		return fmt.Errorf("moosefs: upload %s: mknod: %w", remote, err)
	}

	// Write content in chunks.
	buf := make([]byte, chunkSize)
	var offset uint64
	var done int64

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("moosefs: upload %s: context cancelled: %w", remote, err)
		}

		n, readErr := f.Read(buf)
		if n > 0 {
			if writeErr := c.Write(nodeID, offset, buf[:n]); writeErr != nil {
				log.Printf("upload %s: write at offset %d: %v", remote, offset, writeErr)
				return fmt.Errorf("moosefs: upload %s: write at offset %d: %w", remote, offset, writeErr)
			}
			offset += uint64(n)
			done += int64(n)
			if progress != nil {
				progress(done, totalSize)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("moosefs: upload %s: read local: %w", remote, readErr)
		}
	}
	return nil
}

// Download reads the remote file at remote and writes it to local.
// The parent directory of local is created if it does not exist.
func (b *Backend) Download(ctx context.Context, remote, local string, progress plugins.ProgressCallback) error {
	connected, c, subDir, _ := b.state()
	if !connected {
		return ErrNotConnected
	}

	// Resolve remote node.
	nodeID, err := resolvePath(ctx, c, subDir, remote)
	if err != nil {
		return fmt.Errorf("moosefs: download %s: %w", remote, err)
	}

	// Get total size for progress.
	attr, err := c.GetAttr(nodeID)
	if err != nil {
		return fmt.Errorf("moosefs: download %s: stat: %w", remote, err)
	}
	totalSize := int64(attr.Size)

	// Create local parent directory.
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return fmt.Errorf("moosefs: download %s: create parent dir: %w", local, err)
	}

	out, err := os.Create(local)
	if err != nil {
		return fmt.Errorf("moosefs: download %s: create local file: %w", local, err)
	}
	defer out.Close()

	var offset uint64
	var done int64

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("moosefs: download %s: context cancelled: %w", remote, err)
		}

		chunk, readErr := c.Read(nodeID, offset, chunkSize)
		if readErr != nil {
			log.Printf("download %s: read at offset %d: %v", remote, offset, readErr)
			return fmt.Errorf("moosefs: download %s: read at offset %d: %w", remote, offset, readErr)
		}
		if len(chunk) == 0 {
			break // EOF
		}

		if _, writeErr := out.Write(chunk); writeErr != nil {
			return fmt.Errorf("moosefs: download %s: write local: %w", remote, writeErr)
		}
		offset += uint64(len(chunk))
		done += int64(len(chunk))
		if progress != nil {
			progress(done, totalSize)
		}
	}
	return nil
}

// Delete removes the file or directory at remote.
// Returns ErrFileNotFound (wrapped) when remote does not exist.
func (b *Backend) Delete(ctx context.Context, remote string) error {
	connected, c, subDir, _ := b.state()
	if !connected {
		return ErrNotConnected
	}

	// Resolve the node to find out if it's a file or directory.
	nodeID, err := resolvePath(ctx, c, subDir, remote)
	if err != nil {
		return fmt.Errorf("moosefs: delete %s: %w", remote, err)
	}

	attr, err := c.GetAttr(nodeID)
	if err != nil {
		return fmt.Errorf("moosefs: delete %s: stat: %w", remote, err)
	}

	parentID, baseName, err := resolveParent(ctx, c, subDir, remote)
	if err != nil {
		return fmt.Errorf("moosefs: delete %s: resolve parent: %w", remote, err)
	}

	if attr.IsDir() {
		// It's a directory — use Rmdir.
		if err := c.Rmdir(parentID, baseName); err != nil {
			return fmt.Errorf("moosefs: delete %s: rmdir: %w", remote, err)
		}
	} else {
		// It's a file — use Unlink.
		if err := c.Unlink(parentID, baseName); err != nil {
			return fmt.Errorf("moosefs: delete %s: unlink: %w", remote, err)
		}
	}
	return nil
}

// Move moves oldPath to newPath.
//
// Implementation note (v1.5.x): MooseFS RENAME is not exposed by the minimal
// TCP client.  Move is implemented as:
//
//  1. Download oldPath → local temp file
//  2. Upload temp file → newPath     (if this fails, oldPath is preserved)
//  3. Delete oldPath                 (only reached if Upload succeeded)
//  4. Cleanup temp file
//
// Upload-first order is deliberate: if the upload to newPath fails, the
// source at oldPath is left intact and no data is lost.  The only residual
// risk is a partial newPath if Upload is interrupted mid-way; callers should
// treat newPath as invalid when Move returns an error.
//
// TODO(v1.6.x): implement native FUSE_RENAME for atomic server-side rename.
func (b *Backend) Move(ctx context.Context, oldPath, newPath string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	// Step 1 — Download source to a local temp file.
	tmp, err := os.CreateTemp("", "ghostdrive-moosefs-move-*")
	if err != nil {
		return fmt.Errorf("moosefs: move %s → %s: create temp: %w", oldPath, newPath, err)
	}
	tmpName := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpName)

	if err := b.Download(ctx, oldPath, tmpName, nil); err != nil {
		return fmt.Errorf("moosefs: move %s → %s: download: %w", oldPath, newPath, err)
	}

	// Step 2 — Upload to destination.  Source is still intact at this point.
	if err := b.Upload(ctx, tmpName, newPath, nil); err != nil {
		return fmt.Errorf("moosefs: move %s → %s: upload: %w", oldPath, newPath, err)
	}

	// Step 3 — Delete source only after the upload succeeded.
	if err := b.Delete(ctx, oldPath); err != nil {
		return fmt.Errorf("moosefs: move %s → %s: delete source: %w", oldPath, newPath, err)
	}
	return nil
}

// ─── Navigation ───────────────────────────────────────────────────────────────

// List returns the direct children of the directory at dirPath.
// Returns an empty (non-nil) slice when the directory is empty.
func (b *Backend) List(ctx context.Context, dirPath string) ([]plugins.FileInfo, error) {
	connected, c, subDir, _ := b.state()
	if !connected {
		return nil, ErrNotConnected
	}

	nodeID, err := resolvePath(ctx, c, subDir, dirPath)
	if err != nil {
		return nil, fmt.Errorf("moosefs: list %s: %w", dirPath, err)
	}

	entries, err := c.ReadDir(nodeID)
	if err != nil {
		return nil, fmt.Errorf("moosefs: list %s: readdir: %w", dirPath, err)
	}

	result := make([]plugins.FileInfo, 0, len(entries))
	for _, e := range entries {
		attr, attrErr := c.GetAttr(e.NodeID)
		if attrErr != nil {
			continue // skip unavailable nodes
		}

		entryPath := strings.TrimLeft(dirPath+"/"+e.Name, "/")
		fi := plugins.FileInfo{
			Name:    e.Name,
			Path:    entryPath,
			IsDir:   e.IsDir,
			Size:    int64(attr.Size),
			ModTime: time.Unix(int64(attr.MTime), 0),
		}
		result = append(result, fi)
	}
	return result, nil
}

// Stat returns the metadata of the file or directory at filePath.
// Returns ErrFileNotFound (wrapped) when filePath does not exist.
func (b *Backend) Stat(ctx context.Context, filePath string) (*plugins.FileInfo, error) {
	connected, c, subDir, _ := b.state()
	if !connected {
		return nil, ErrNotConnected
	}

	nodeID, err := resolvePath(ctx, c, subDir, filePath)
	if err != nil {
		return nil, fmt.Errorf("moosefs: stat %s: %w", filePath, err)
	}

	attr, err := c.GetAttr(nodeID)
	if err != nil {
		return nil, fmt.Errorf("moosefs: stat %s: getattr: %w", filePath, err)
	}

	// Infer IsDir from mode (bits 15-12 == 2 means directory).
	isDir := attr.IsDir()
	// For the root node, always treat as dir.
	if nodeID == mfsclient.RootNodeID {
		isDir = true
	}

	// Determine name from the last segment of filePath.
	name := filePath
	if idx := strings.LastIndex(filePath, "/"); idx >= 0 {
		name = filePath[idx+1:]
	}
	if name == "" {
		name = "/"
	}

	return &plugins.FileInfo{
		Name:    name,
		Path:    strings.TrimLeft(filePath, "/"),
		IsDir:   isDir,
		Size:    int64(attr.Size),
		ModTime: time.Unix(int64(attr.MTime), 0),
	}, nil
}

// CreateDir creates the directory at dirPath.
// If the directory already exists, the call is a no-op.
// The parent directory must already exist.
func (b *Backend) CreateDir(ctx context.Context, dirPath string) error {
	connected, c, subDir, _ := b.state()
	if !connected {
		return ErrNotConnected
	}

	parentID, baseName, err := resolveParent(ctx, c, subDir, dirPath)
	if err != nil {
		return fmt.Errorf("moosefs: createDir %s: resolve parent: %w", dirPath, err)
	}

	_, mkdirErr := c.Mkdir(parentID, baseName, 0o755)
	if mkdirErr != nil {
		// Ignore "already exists" — the server returns the existing nodeID on
		// duplicate Mkdir, so this branch is only reached on a genuine error.
		return fmt.Errorf("moosefs: createDir %s: %w", dirPath, mkdirErr)
	}
	return nil
}

// ─── Watch ────────────────────────────────────────────────────────────────────

// Watch polls watchPath every pollMs milliseconds and emits FileEvents when
// entries are created, modified, or deleted.
// The returned channel (buffered, size 64) is closed when ctx is cancelled.
func (b *Backend) Watch(ctx context.Context, watchPath string) (<-chan plugins.FileEvent, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	_, _, _, pollMs := b.state()

	ch := make(chan plugins.FileEvent, 64)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(time.Duration(pollMs) * time.Millisecond)
		defer ticker.Stop()

		// Establish initial snapshot.
		snapshot := buildWatchSnapshot(ctx, b, watchPath)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !b.IsConnected() {
					return
				}

				current, err := b.List(ctx, watchPath)
				if err != nil {
					continue
				}

				currentMap := make(map[string]plugins.FileInfo, len(current))
				for _, fi := range current {
					currentMap[fi.Path] = fi
				}

				// Detect created and modified.
				for p, fi := range currentMap {
					old, exists := snapshot[p]
					var evType plugins.FileEventType
					if !exists {
						evType = plugins.FileEventCreated
					} else if fi.ModTime != old.ModTime || fi.Size != old.Size {
						evType = plugins.FileEventModified
					}
					if evType != "" {
						select {
						case ch <- plugins.FileEvent{
							Type:      evType,
							Path:      p,
							Timestamp: time.Now(),
							Source:    "remote",
						}:
						case <-ctx.Done():
							return
						}
					}
				}

				// Detect deleted.
				for p := range snapshot {
					if _, exists := currentMap[p]; !exists {
						select {
						case ch <- plugins.FileEvent{
							Type:      plugins.FileEventDeleted,
							Path:      p,
							Timestamp: time.Now(),
							Source:    "remote",
						}:
						case <-ctx.Done():
							return
						}
					}
				}

				snapshot = currentMap
			}
		}
	}()

	return ch, nil
}

// buildWatchSnapshot creates the initial path→FileInfo snapshot for Watch.
func buildWatchSnapshot(ctx context.Context, b *Backend, watchPath string) map[string]plugins.FileInfo {
	entries, err := b.List(ctx, watchPath)
	if err != nil {
		return map[string]plugins.FileInfo{}
	}
	m := make(map[string]plugins.FileInfo, len(entries))
	for _, fi := range entries {
		m[fi.Path] = fi
	}
	return m
}

// ─── Quota ────────────────────────────────────────────────────────────────────

// GetQuota queries the MooseFS master for real cluster filesystem statistics
// via StatFS (CLTOMA_FUSE_STATFS, opcode 402).
//
// Returns (free, total, nil) on success.
// Pre-condition: IsConnected() == true, else returns 0, 0, ErrNotConnected.
func (b *Backend) GetQuota(_ context.Context) (free, total int64, err error) {
	if !b.IsConnected() {
		return 0, 0, ErrNotConnected
	}
	_, c, _, _ := b.state()
	free, total, err = c.StatFS()
	if err != nil {
		return 0, 0, fmt.Errorf("moosefs: getquota: %w", err)
	}
	return free, total, nil
}
