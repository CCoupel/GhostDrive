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
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CCoupel/GhostDrive/internal/logger"
	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/CCoupel/GhostDrive/plugins/moosefs/internal/mfsclient"
)

// Version is injected at build time via ldflags:
//
//	-X 'github.com/CCoupel/GhostDrive/plugins/moosefs.Version=1.5.2'
var Version = "unknown"

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

	// Adaptive Watch polling parameters (set at Connect time).
	// After a change is detected the interval resets to pollIntervalMin.
	// Each poll without a change multiplies the interval by pollBackoffFactor
	// until it reaches pollIntervalMax.
	pollIntervalMin  time.Duration // minimum poll interval — after a change  (default 2 s)
	pollIntervalMax  time.Duration // maximum poll interval — idle backoff cap  (default 30 s)
	pollBackoffFactor float64      // backoff multiplier (default 2.0)

	lastCfg plugins.BackendConfig

	// reconnMu serialises reconnection attempts so that only one goroutine
	// dials the master at a time.  Other goroutines that detect a connection
	// error concurrently will block here and, once the first reconnect
	// succeeds, find b.connected == true and return nil immediately.
	reconnMu sync.Mutex
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
		Version:     Version,
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
				Key:      "pollIntervalMin",
				Label:    "Intervalle Watch min (ms)",
				Type:     plugins.ParamTypeNumber,
				Required: false,
				Default:  "2000",
				HelpText: "Intervalle de polling après un changement détecté",
			},
			{
				Key:      "pollIntervalMax",
				Label:    "Intervalle Watch max (ms)",
				Type:     plugins.ParamTypeNumber,
				Required: false,
				Default:  "30000",
				HelpText: "Plafond du backoff adaptatif (alias de pollInterval pour la rétrocompatibilité)",
			},
			{
				Key:      "pollBackoffFactor",
				Label:    "Facteur backoff Watch",
				Type:     plugins.ParamTypeString,
				Required: false,
				Default:  "2.0",
				HelpText: "Multiplicateur de l'intervalle après chaque poll sans changement",
			},
			{
				Key:         "uploadConcurrency",
				Label:       "Concurrence upload",
				Type:        plugins.ParamTypeNumber,
				Required:    false,
				Default:     "4",
				Placeholder: "1–16",
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

	// ── Adaptive Watch polling ────────────────────────────────────────────────
	// pollIntervalMax defaults to pollInterval for backward compatibility.
	pollMax := 30 * time.Second
	if s := cfg.Params["pollIntervalMax"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			pollMax = time.Duration(n) * time.Millisecond
		}
	} else if s := cfg.Params["pollInterval"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			pollMax = time.Duration(n) * time.Millisecond
		}
	}

	pollMin := 2 * time.Second
	if s := cfg.Params["pollIntervalMin"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			pollMin = time.Duration(n) * time.Millisecond
		}
	}
	if pollMin > pollMax {
		pollMin = pollMax // clamp: min cannot exceed max
	}

	pollFactor := 2.0
	if s := cfg.Params["pollBackoffFactor"]; s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 1.0 {
			pollFactor = f
		}
	}

	logger.Info("connect: dialing %s:%d subDir=%q", masterHost, masterPort, subDir)
	client, err := dialAndRegister(cfg)
	if err != nil {
		logger.Error("connect: failed: %v", err)
		return fmt.Errorf("moosefs: connect: %w", err)
	}
	logger.Info("connect: registered sessionID=%d ready", client.SessionID())

	b.mu.Lock()
	defer b.mu.Unlock()

	// Disconnect previous client if any.
	if b.client != nil {
		_ = b.client.Close()
	}

	b.connected = true
	b.client = client
	b.subDir = subDir
	b.pollIntervalMin = pollMin
	b.pollIntervalMax = pollMax
	b.pollBackoffFactor = pollFactor
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
func (b *Backend) state() (connected bool, c *mfsclient.Client, subDir string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected, b.client, b.subDir
}

// isConnError returns true when err is a TCP write/read failure (broken
// connection) that warrants a transparent reconnect attempt.
func isConnError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "write frame header") ||
		strings.Contains(s, "read frame header") ||
		strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "connection aborted") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "EOF")
}

// reconnect re-establishes the TCP connection using the last successful config.
//
// It implements a singleflight pattern via reconnMu: if multiple goroutines
// detect a connection error simultaneously only the first one that acquires
// reconnMu actually dials the master.  The others block on reconnMu and, once
// released, see b.connected == true and return nil without creating a second
// session.
//
// Must NOT be called with b.mu held.
func (b *Backend) reconnect() error {
	// Mark as disconnected before acquiring reconnMu so that goroutines waiting
	// on the lock see connected==false and know reconnection is still needed.
	b.mu.Lock()
	b.connected = false
	b.mu.Unlock()

	// Singleflight: serialise concurrent reconnection attempts.
	b.reconnMu.Lock()
	defer b.reconnMu.Unlock()

	// Re-check after acquiring the lock: a goroutine ahead of us may have
	// already reconnected successfully.
	b.mu.RLock()
	alreadyOK := b.connected
	cfg := b.lastCfg
	b.mu.RUnlock()
	if alreadyOK {
		return nil
	}

	logger.Info("reconnect: reconnecting to master")

	newClient, err := dialAndRegister(cfg)
	if err != nil {
		logger.Error("reconnect: failed: %v", err)
		return fmt.Errorf("moosefs: reconnect: %w", err)
	}

	b.mu.Lock()
	if b.client != nil {
		_ = b.client.Close()
	}
	b.client = newClient
	b.connected = true
	b.mu.Unlock()
	logger.Info("reconnect: success sessionID=%d", newClient.SessionID())
	return nil
}

// dialAndRegister opens a TCP connection to the master, registers, and probes.
func dialAndRegister(cfg plugins.BackendConfig) (*mfsclient.Client, error) {
	masterHost := strings.TrimSpace(cfg.Params["masterHost"])
	masterPort := 9421
	if s := cfg.Params["masterPort"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			masterPort = n
		}
	}
	c, err := mfsclient.Dial(masterHost, masterPort)
	if err != nil {
		return nil, err
	}
	if err := c.Register(); err != nil {
		_ = c.Close()
		return nil, err
	}
	if _, err := c.GetAttr(mfsclient.RootNodeID); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// ─── File operations ──────────────────────────────────────────────────────────

// Upload reads the local file at local and writes it to the remote path
// remote in 64 KiB chunks via Mknod + Write.
// The remote parent directory must already exist.
// On connection loss (EOF), it reconnects and retries once.
func (b *Backend) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	err := b.upload(ctx, local, remote, progress)
	if err != nil && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
		logger.Warn("upload %s: connection lost, reconnecting…", remote)
		if reconnErr := b.reconnect(); reconnErr != nil {
			return err
		}
		logger.Info("upload %s: retrying after reconnect", remote)
		err = b.upload(ctx, local, remote, progress)
	}
	return err
}

func (b *Backend) upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	connected, c, subDir := b.state()
	if !connected {
		return ErrNotConnected
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("moosefs: upload %s: %w", remote, err)
	}

	// Parse uploadConcurrency (default 4, max 16).
	uploadConcurrency := 4
	b.mu.RLock()
	if s := b.lastCfg.Params["uploadConcurrency"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 16 {
			uploadConcurrency = n
		}
	}
	b.mu.RUnlock()

	// Open local file.
	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("moosefs: upload %s: open local: %w", remote, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("moosefs: upload %s: stat local: %w", remote, err)
	}
	totalSize := fi.Size()
	logger.Debug("upload %s: local size=%d concurrency=%d", remote, totalSize, uploadConcurrency)

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
		// returns the existing nodeID, which WriteChunkData will overwrite from offset 0.
		_ = c.Unlink(parentID, baseName)
	}

	// Create the remote node.
	nodeID, err := c.Mknod(parentID, baseName, 0o644)
	if err != nil {
		return fmt.Errorf("moosefs: upload %s: mknod: %w", remote, err)
	}

	// ── Producer / consumer pipeline ─────────────────────────────────────────
	// The producer goroutine reads the file sequentially in 64 KiB blocks and
	// groups consecutive blocks into per-MooseFS-chunk jobs (64 MiB boundary).
	// Jobs are sent on a buffered channel (capacity = uploadConcurrency).
	//
	// The consumer loop (main goroutine) reads from the channel and spawns up to
	// uploadConcurrency goroutines (semaphore) that call WriteChunkData in
	// parallel.
	//
	// Memory upper bound: uploadConcurrency × ChunkSize (e.g. 4 × 64 MiB = 256 MiB)
	// rather than the full file size — O(fileSize) heap allocation is avoided.
	type chunkJob struct {
		offset uint64 // file offset where this chunk starts
		data   []byte // accumulated data for this chunk (≤ mfsclient.ChunkSize)
	}

	jobs := make(chan chunkJob, uploadConcurrency)

	// producerErr carries the terminal result of the producer goroutine:
	// nil on clean EOF, non-nil on I/O error or context cancellation.
	producerErr := make(chan error, 1)

	go func() {
		defer close(jobs)

		readBuf := make([]byte, chunkSize) // 64 KiB I/O buffer (reused across reads)
		var fileOffset uint64
		var cur *chunkJob // chunk currently being accumulated

		// flushCur sends the current pending job on the channel.
		// Returns false if ctx was cancelled while blocked.
		flushCur := func() bool {
			if cur == nil {
				return true
			}
			select {
			case jobs <- *cur:
				cur = nil
				return true
			case <-ctx.Done():
				return false
			}
		}

		for {
			if err := ctx.Err(); err != nil {
				producerErr <- fmt.Errorf("moosefs: upload %s: context cancelled: %w", remote, err)
				return
			}
			n, readErr := f.Read(readBuf)
			if n > 0 {
				chunkIdx := uint32(fileOffset / mfsclient.ChunkSize)
				if cur != nil && uint32(cur.offset/mfsclient.ChunkSize) == chunkIdx {
					// Same MooseFS chunk: append to current job.
					cur.data = append(cur.data, readBuf[:n]...)
				} else {
					// New MooseFS chunk: flush current job (if any) then start new.
					if !flushCur() {
						producerErr <- fmt.Errorf("moosefs: upload %s: context cancelled", remote)
						return
					}
					dataCopy := make([]byte, n)
					copy(dataCopy, readBuf[:n])
					cur = &chunkJob{offset: fileOffset, data: dataCopy}
				}
				fileOffset += uint64(n)
			}
			if readErr == io.EOF {
				if !flushCur() {
					producerErr <- fmt.Errorf("moosefs: upload %s: context cancelled", remote)
					return
				}
				producerErr <- nil
				return
			}
			if readErr != nil {
				producerErr <- fmt.Errorf("moosefs: upload %s: read local: %w", remote, readErr)
				return
			}
		}
	}()

	// Consumer: drain the jobs channel and dispatch write goroutines.
	sem := make(chan struct{}, uploadConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var uploadErr error
	var atomicDone int64

	for job := range jobs {
		job := job
		// Acquire semaphore (blocks when uploadConcurrency goroutines are busy).
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore slot

			// Abort if context cancelled or an earlier chunk failed.
			// Still drains `jobs` implicitly: main loop keeps receiving.
			mu.Lock()
			prevErr := uploadErr
			mu.Unlock()
			if prevErr != nil || ctx.Err() != nil {
				return
			}

			logger.Debug("[moosefs] upload: chunk offset=%d size=%d", job.offset, len(job.data))
			if writeErr := c.WriteChunkData(nodeID, job.offset, job.data); writeErr != nil {
				logger.Error("upload %s: chunk offset=%d: %v", remote, job.offset, writeErr)
				mu.Lock()
				if uploadErr == nil {
					uploadErr = fmt.Errorf("moosefs: upload %s: write at offset %d: %w",
						remote, job.offset, writeErr)
				}
				mu.Unlock()
				return
			}

			done := atomic.AddInt64(&atomicDone, int64(len(job.data)))
			if progress != nil {
				progress(done, totalSize)
			}
		}()
	}

	wg.Wait()

	// Check producer result (read error or context cancellation).
	if pErr := <-producerErr; pErr != nil {
		return pErr
	}

	mu.Lock()
	err = uploadErr
	mu.Unlock()
	if err != nil {
		return err
	}
	logger.Debug("[moosefs] upload: all chunks sent, total=%d", atomicDone)
	return nil
}

// Download reads the remote file at remote and writes it to local.
// The parent directory of local is created if it does not exist.
func (b *Backend) Download(ctx context.Context, remote, local string, progress plugins.ProgressCallback) error {
	connected, c, subDir := b.state()
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

		// Primary EOF guard: stop before requesting a chunk beyond the file's end.
		// Real MooseFS CS zero-pads reads within a 64 MiB chunk (returns chunkSize
		// bytes even when only a few KB of real data exist). Without this guard the
		// loop iterates until offset reaches ChunkSize (67108864), then requests
		// chunk 1 from the master which returns nCS=0 → "no chunk servers available".
		if offset >= uint64(totalSize) {
			break
		}

		chunk, readErr := c.Read(nodeID, offset, chunkSize)
		if readErr != nil {
			logger.Error("download %s: read at offset %d: %v", remote, offset, readErr)
			return fmt.Errorf("moosefs: download %s: read at offset %d: %w", remote, offset, readErr)
		}
		if len(chunk) == 0 {
			break // EOF signalled by mfsclient
		}

		if _, writeErr := out.Write(chunk); writeErr != nil {
			return fmt.Errorf("moosefs: download %s: write local: %w", remote, writeErr)
		}
		offset += uint64(len(chunk))
		done += int64(len(chunk))
		if progress != nil {
			progress(done, totalSize)
		}

		// Secondary guard: a short read means the CS reached the end of its data.
		if uint32(len(chunk)) < chunkSize {
			break
		}
	}
	return nil
}

// Delete removes the file or directory at remote.
// Returns ErrFileNotFound (wrapped) when remote does not exist.
func (b *Backend) Delete(ctx context.Context, remote string) error {
	connected, c, subDir := b.state()
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

// Move renames oldPath to newPath using the native MooseFS RENAME opcode (424).
// Works atomically for both files and non-empty directories.
// On a TCP connection error, reconnects once and retries.
func (b *Backend) Move(ctx context.Context, oldPath, newPath string) error {
	for attempt := 0; attempt < 2; attempt++ {
		connected, c, subDir := b.state()
		if !connected {
			return ErrNotConnected
		}

		srcParentID, srcName, err := resolveParent(ctx, c, subDir, oldPath)
		if err != nil {
			if attempt == 0 && isConnError(err) {
				logger.Warn("move %s → %s: connection error, reconnecting: %v", oldPath, newPath, err)
				if reconnErr := b.reconnect(); reconnErr != nil {
					return fmt.Errorf("moosefs: move %s → %s: %w", oldPath, newPath, err)
				}
				continue
			}
			return fmt.Errorf("moosefs: move %s → %s: resolve src: %w", oldPath, newPath, err)
		}

		dstParentID, dstName, err := resolveParent(ctx, c, subDir, newPath)
		if err != nil {
			if attempt == 0 && isConnError(err) {
				logger.Warn("move %s → %s: connection error, reconnecting: %v", oldPath, newPath, err)
				if reconnErr := b.reconnect(); reconnErr != nil {
					return fmt.Errorf("moosefs: move %s → %s: %w", oldPath, newPath, err)
				}
				continue
			}
			return fmt.Errorf("moosefs: move %s → %s: resolve dst: %w", oldPath, newPath, err)
		}

		if err := c.Rename(srcParentID, srcName, dstParentID, dstName); err != nil {
			if attempt == 0 && isConnError(err) {
				logger.Warn("move %s → %s: connection error, reconnecting: %v", oldPath, newPath, err)
				if reconnErr := b.reconnect(); reconnErr != nil {
					return fmt.Errorf("moosefs: move %s → %s: %w", oldPath, newPath, err)
				}
				continue
			}
			return fmt.Errorf("moosefs: move %s → %s: %w", oldPath, newPath, err)
		}
		logger.Info("move %s → %s: OK", oldPath, newPath)
		return nil
	}
	return ErrNotConnected
}

// ─── Navigation ───────────────────────────────────────────────────────────────

// List returns the direct children of the directory at dirPath.
// Returns an empty (non-nil) slice when the directory is empty.
// On a TCP connection error (e.g. WSAECONNABORTED), reconnects once and retries.
func (b *Backend) List(ctx context.Context, dirPath string) ([]plugins.FileInfo, error) {
	for attempt := 0; attempt < 2; attempt++ {
		connected, c, subDir := b.state()
		if !connected {
			return nil, ErrNotConnected
		}

		nodeID, err := resolvePath(ctx, c, subDir, dirPath)
		if err != nil {
			if attempt == 0 && isConnError(err) {
				logger.Warn("list %s: connection error, reconnecting: %v", dirPath, err)
				if reconnErr := b.reconnect(); reconnErr != nil {
					return nil, fmt.Errorf("moosefs: list %s: %w", dirPath, err)
				}
				continue
			}
			return nil, fmt.Errorf("moosefs: list %s: %w", dirPath, err)
		}

		entries, err := c.ReadDir(nodeID)
		if err != nil {
			if attempt == 0 && isConnError(err) {
				logger.Warn("list %s: readdir connection error, reconnecting: %v", dirPath, err)
				if reconnErr := b.reconnect(); reconnErr != nil {
					return nil, fmt.Errorf("moosefs: list %s: readdir: %w", dirPath, err)
				}
				continue
			}
			logger.Error("list %s: readdir(%d) error: %v", dirPath, nodeID, err)
			return nil, fmt.Errorf("moosefs: list %s: readdir: %w", dirPath, err)
		}
		logger.Info("list %s: readdir(%d) returned %d entries", dirPath, nodeID, len(entries))

		// ReadDir returns [namelen:8][name][inode:32][dtype:8] per entry.
		// Filter out "." and ".." pseudo-entries that some servers include.
		result := make([]plugins.FileInfo, 0, len(entries))
		for _, e := range entries {
			if e.Name == "." || e.Name == ".." {
				continue
			}
			entryPath := strings.TrimLeft(dirPath+"/"+e.Name, "/")
			fi := plugins.FileInfo{
				Name:  e.Name,
				Path:  entryPath,
				IsDir: e.IsDir,
			}
			// ReadDir with flags=0 does not return Size or MTime.
			// TODO(#117): use ReadDir with attrs flags to avoid N+1 GetAttr roundtrips.
			// Call GetAttr per entry to populate these fields (#116).
			if attr, attrErr := c.GetAttr(e.NodeID); attrErr != nil {
				logger.Warn("list %s: getattr(%d/%s) failed, using zero values: %v",
					dirPath, e.NodeID, e.Name, attrErr)
			} else {
				fi.Size = int64(attr.Size)
				fi.ModTime = time.Unix(int64(attr.MTime), 0)
				fi.Version = strconv.FormatUint(uint64(attr.CTime), 10) // ctime as opaque version token (#131)
			}
			result = append(result, fi)
		}
		return result, nil
	}
	// Unreachable: the loop always returns on attempt 1.
	return nil, ErrNotConnected
}

// Stat returns the metadata of the file or directory at filePath.
// Returns ErrFileNotFound (wrapped) when filePath does not exist.
func (b *Backend) Stat(ctx context.Context, filePath string) (*plugins.FileInfo, error) {
	for attempt := 0; attempt < 2; attempt++ {
		connected, c, subDir := b.state()
		if !connected {
			return nil, ErrNotConnected
		}
		nodeID, err := resolvePath(ctx, c, subDir, filePath)
		if err != nil {
			if attempt == 0 && isConnError(err) {
				logger.Warn("stat %s: connection error, reconnecting: %v", filePath, err)
				if reconnErr := b.reconnect(); reconnErr != nil {
					return nil, fmt.Errorf("moosefs: stat %s: %w", filePath, err)
				}
				continue
			}
			return nil, fmt.Errorf("moosefs: stat %s: %w", filePath, err)
		}
		attr, err := c.GetAttr(nodeID)
		if err != nil {
			return nil, fmt.Errorf("moosefs: stat %s: getattr: %w", filePath, err)
		}
		isDir := attr.IsDir()
		if nodeID == mfsclient.RootNodeID {
			isDir = true
		}
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
			Version: strconv.FormatUint(uint64(attr.CTime), 10), // ctime as opaque version token (#131)
		}, nil
	}
	return nil, ErrNotConnected
}

// CreateDir creates the directory at dirPath.
// If the directory already exists, the call is a no-op.
// The parent directory must already exist.
func (b *Backend) CreateDir(ctx context.Context, dirPath string) error {
	connected, c, subDir := b.state()
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

// Watch polls watchPath and emits FileEvents when entries are created,
// modified, or deleted.
//
// Adaptive polling: after a change is detected the poll interval resets to
// pollIntervalMin. Each poll without a change multiplies the interval by
// pollBackoffFactor until it reaches pollIntervalMax. This provides
// near-real-time detection immediately after activity while reducing load
// on the MooseFS master during quiet periods.
//
// The returned channel (buffered, size 64) is closed when ctx is cancelled.
func (b *Backend) Watch(ctx context.Context, watchPath string) (<-chan plugins.FileEvent, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	b.mu.RLock()
	pollMin    := b.pollIntervalMin
	pollMax    := b.pollIntervalMax
	pollFactor := b.pollBackoffFactor
	b.mu.RUnlock()

	ch := make(chan plugins.FileEvent, 64)

	go func() {
		defer close(ch)

		// Establish initial snapshot before the first tick.
		snapshot := buildWatchSnapshot(ctx, b, watchPath)

		// Use a Timer (not Ticker) so we can adjust the interval adaptively.
		current := pollMin
		timer := time.NewTimer(current)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if !b.IsConnected() {
					return
				}

				entries, err := b.List(ctx, watchPath)
				if err != nil {
					// Transient error: retry with the current interval unchanged.
					timer.Reset(current)
					continue
				}

				currentMap := make(map[string]plugins.FileInfo, len(entries))
				for _, fi := range entries {
					currentMap[fi.Path] = fi
				}

				changed := false

				// Detect created and modified.
				for p, fi := range currentMap {
					old, exists := snapshot[p]
					var evType plugins.FileEventType
					var metadataOnly bool
					if !exists {
						evType = plugins.FileEventCreated
					} else if fi.ModTime != old.ModTime && fi.Size == old.Size {
						// mtime changed, size stable → attribute/permission change (#130).
						evType = plugins.FileEventMetadataChanged
						metadataOnly = true
					} else if fi.ModTime != old.ModTime || fi.Size != old.Size {
						evType = plugins.FileEventModified
					}
					if evType != "" {
						changed = true
						select {
						case ch <- plugins.FileEvent{
							Type:            evType,
							Path:            p,
							Timestamp:       time.Now(),
							Source:          "remote",
							ModTime:         fi.ModTime,
							PreviousModTime: old.ModTime, // zero when !exists (Created)
							MetadataOnly:    metadataOnly,
						}:
						case <-ctx.Done():
							return
						}
					}
				}

				// Detect deleted.
				for p := range snapshot {
					if _, exists := currentMap[p]; !exists {
						changed = true
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

				// Adaptive interval: reset to min after a change, backoff otherwise.
				current = computeNextInterval(current, pollMin, pollMax, pollFactor, changed)
				timer.Reset(current)
			}
		}
	}()

	return ch, nil
}

// computeNextInterval calculates the next adaptive poll interval.
// If changed is true the interval resets to min.
// Otherwise the interval is multiplied by factor and clamped to max.
func computeNextInterval(current, min, max time.Duration, factor float64, changed bool) time.Duration {
	if changed {
		return min
	}
	next := time.Duration(float64(current) * factor)
	if next > max {
		return max
	}
	return next
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
	for attempt := 0; attempt < 2; attempt++ {
		if !b.IsConnected() {
			return 0, 0, ErrNotConnected
		}
		_, c, _ := b.state()
		free, total, err = c.StatFS()
		if err != nil {
			if attempt == 0 && isConnError(err) {
				logger.Warn("getquota: connection error, reconnecting: %v", err)
				if reconnErr := b.reconnect(); reconnErr != nil {
					logger.Error("getquota: free=%d total=%d err=%v", free, total, err)
					return 0, 0, fmt.Errorf("moosefs: getquota: %w", err)
				}
				continue
			}
			logger.Error("getquota: free=%d total=%d err=%v", free, total, err)
			return 0, 0, fmt.Errorf("moosefs: getquota: %w", err)
		}
		logger.Info("getquota: free=%d total=%d", free, total)
		return free, total, nil
	}
	logger.Error("getquota: free=%d total=%d err=%v", free, total, ErrNotConnected)
	return 0, 0, ErrNotConnected
}

// ─── Range reads ──────────────────────────────────────────────────────────────

// moosefsChunkSize is the native chunk size of MooseFS (64 MiB).
const moosefsChunkSize = 67_108_864

// ReadAt reads up to length bytes from the remote file at the given byte offset
// using the mfsclient Read primitive (native chunk-aware I/O).
// Returns ErrFileNotFound (wrapped) when remote does not exist.
// Pre-condition: IsConnected() == true, else returns nil, ErrNotConnected.
func (b *Backend) ReadAt(ctx context.Context, remote string, offset, length int64) ([]byte, error) {
	connected, c, subDir := b.state()
	if !connected {
		return nil, ErrNotConnected
	}

	nodeID, err := resolvePath(ctx, c, subDir, remote)
	if err != nil {
		return nil, fmt.Errorf("moosefs: readAt %s: %w", remote, err)
	}

	if length <= 0 {
		return []byte{}, nil
	}

	// Guard against silent uint32 wrap-around: the mfsclient Read primitive
	// accepts a uint32 byte count, so a length > 4 GiB would silently truncate.
	// Callers must chunk requests to at most ChunkSize() bytes.
	if length > math.MaxUint32 {
		return nil, fmt.Errorf("moosefs: readAt %s: length %d exceeds uint32 max (%d) — chunk read required",
			remote, length, math.MaxUint32)
	}

	// Safe cast: length <= math.MaxUint32 verified above.
	size := uint32(length)
	data, err := c.Read(nodeID, uint64(offset), size)
	if err != nil {
		return nil, fmt.Errorf("moosefs: readAt %s offset=%d: %w", remote, offset, err)
	}
	return data, nil
}

// ChunkSize returns 67_108_864 (64 MiB) — the native chunk size of MooseFS.
// The chunk cache should align range reads to this boundary for optimal
// performance on MooseFS chunk servers.
func (b *Backend) ChunkSize() int64 { return moosefsChunkSize }
