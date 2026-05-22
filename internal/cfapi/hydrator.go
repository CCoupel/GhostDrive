package cfapi

import (
	"context"
	"fmt"
	"log"
	"strings"
	gosync "sync"
	"time"

	"github.com/CCoupel/GhostDrive/internal/cache"
	"github.com/CCoupel/GhostDrive/plugins"
)

const (
	defaultChunkSize        = 4 * 1024 * 1024 // 4 MiB — used when backend.ChunkSize() returns 0
	statCacheTTL            = 30 * time.Second // how long a Stat() result is cached across OnFetchData calls
	fetchPlaceholdersTimeout = 30 * time.Second // max time allowed for one FETCH_PLACEHOLDERS response
)

// statCacheEntry holds a short-lived Stat() result for one remote path.
type statCacheEntry struct {
	etag     string
	mtime    time.Time
	storedAt time.Time
}

// Hydrator bridges CF API FETCH_DATA callbacks to the backend + chunk cache.
// It is created per-backend by CFManager.Start and its methods are wired
// into CFCallbacks.
type Hydrator struct {
	backend    plugins.StorageBackend
	chunkCache cache.ChunkCache
	provider   *SyncProvider
	emitter    EventEmitter
	backendID  string
	chunkSize  int64

	// cancelMu protects in-flight fetch cancellations.
	cancelMu     gosync.Mutex
	fetchCancels map[string]context.CancelFunc // localPath → cancel

	// statMu protects the short-lived Stat() result cache (MAJEUR-4).
	// Prevents N+1 Stat calls when CF triggers multiple FETCH_DATA callbacks
	// for the same file (one per chunk range).
	statMu      gosync.Mutex
	statCacheMap map[string]*statCacheEntry // remotePath → cached stat
}

// NewHydrator creates a Hydrator.
// chunkSize overrides the backend's ChunkSize(); pass 0 to use backend value (or defaultChunkSize).
func NewHydrator(
	backend plugins.StorageBackend,
	ch cache.ChunkCache,
	provider *SyncProvider,
	emitter EventEmitter,
	backendID string,
) *Hydrator {
	cs := int64(0)
	if backend != nil {
		cs = backend.ChunkSize()
	}
	if cs <= 0 {
		cs = defaultChunkSize
	}
	if ch == nil {
		ch = cache.NewNoopCache()
	}
	return &Hydrator{
		backend:      backend,
		chunkCache:   ch,
		provider:     provider,
		emitter:      emitter,
		backendID:    backendID,
		chunkSize:    cs,
		fetchCancels: make(map[string]context.CancelFunc),
		statCacheMap: make(map[string]*statCacheEntry),
	}
}

// OnFetchData is the CF API FETCH_DATA callback.
// It checks the cache, falls back to backend.ReadAt, and writes data via
// provider.ExecuteTransfer. Progress events are throttled to 100ms.
func (h *Hydrator) OnFetchData(ctx context.Context, req FetchRequest) error {
	log.Printf("cfapi: hydrator OnFetchData: localPath=%q offset=%d length=%d", req.LocalPath, req.Offset, req.Length)
	if ctx == nil {
		ctx = context.Background()
	}

	// Register a cancellable context for this fetch so OnCancelFetch can abort it.
	fetchCtx, cancel := context.WithCancel(ctx)
	h.cancelMu.Lock()
	h.fetchCancels[req.LocalPath] = cancel
	h.cancelMu.Unlock()
	defer func() {
		cancel()
		h.cancelMu.Lock()
		delete(h.fetchCancels, req.LocalPath)
		h.cancelMu.Unlock()
	}()

	// Derive the remote path from the local path.
	remotePath := h.localToRemote(req.LocalPath)

	// Read backend metadata for cache validation.
	// getStatCached avoids a network Stat() on every chunk callback for the same file.
	currentETag, currentMTime := h.getStatCached(fetchCtx, remotePath)

	// Align offset down to the nearest chunkSize boundary.
	alignedOffset := (req.Offset / h.chunkSize) * h.chunkSize
	remaining := req.Length
	transferred := int64(0)
	lastEmit := time.Time{}

	for remaining > 0 {
		if fetchCtx.Err() != nil {
			return fetchCtx.Err()
		}

		readLen := h.chunkSize
		if readLen > remaining {
			readLen = remaining
		}
		isFinal := (remaining <= h.chunkSize)

		ckey := cache.ChunkKey{
			BackendID:  h.backendID,
			RemotePath: remotePath,
			Offset:     alignedOffset,
		}

		var data []byte

		// Cache lookup.
		if entry, ok := h.chunkCache.Get(fetchCtx, ckey, currentETag, currentMTime); ok {
			// Cache hit — use stored data.
			data = entry.Data
		} else {
			// Cache miss — fetch from backend.
			if h.backend == nil {
				return fmt.Errorf("cfapi: hydrator: no backend for %s", req.LocalPath)
			}
			var err error
			data, err = h.backend.ReadAt(fetchCtx, remotePath, alignedOffset, h.chunkSize)
			if err != nil {
				_ = h.provider.ReportError(req, err)
				// FIX (regression after 10a297f): do NOT call SetSyncState(CloudOnly) here.
				// ReportError already signals Windows that the FETCH_DATA failed.
				// The file naturally stays NOT_IN_SYNC (☁️) — it was CloudOnly before
				// FETCH_DATA was triggered and we never called SetSyncState(Synced).
				// Calling CfSetInSyncState(NOT_IN_SYNC) on a file during an active
				// FETCH_DATA operation can corrupt the CF state of the parent directory,
				// breaking FETCH_PLACEHOLDERS and hiding remote content (merge regression).
				return fmt.Errorf("cfapi: hydrator: ReadAt %s offset %d: %w", remotePath, alignedOffset, err)
			}

			// Store in cache.
			_ = h.chunkCache.Put(fetchCtx, ckey, cache.ChunkEntry{
				Data:  data,
				ETag:  currentETag,
				MTime: currentMTime,
			})
		}

		// Trim data to requested range within this chunk.
		chunkOffset := req.Offset - alignedOffset
		if chunkOffset < 0 {
			chunkOffset = 0
		}
		end := chunkOffset + readLen
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		if chunkOffset >= int64(len(data)) {
			break // nothing to transfer
		}
		chunk := data[chunkOffset:end]

		// Transfer chunk to Windows.
		transferReq := FetchRequest{
			LocalPath: req.LocalPath,
			Offset:    req.Offset + transferred,
			Length:    int64(len(chunk)),
			opInfo:    req.opInfo,
		}
		if err := h.provider.ExecuteTransfer(transferReq, chunk, isFinal); err != nil {
			_ = h.provider.ReportError(req, err)
			// FIX (regression after 10a297f): do NOT call SetSyncState(CloudOnly) here.
			// Same rationale as the ReadAt error case above: badge stays ☁️ naturally.
			return fmt.Errorf("cfapi: hydrator: ExecuteTransfer %s: %w", req.LocalPath, err)
		}

		transferred += int64(len(chunk))
		remaining -= int64(len(chunk))
		alignedOffset += h.chunkSize

		// Report progress and emit throttled event (shared 100 ms window).
		// ReportProgress drives the ⟳ spinner percentage in Windows Explorer.
		// Emit sends the progress event to the Wails frontend.
		if time.Since(lastEmit) >= 100*time.Millisecond {
			lastEmit = time.Now()
			if err := h.provider.ReportProgress(req, req.Length, transferred); err != nil {
				log.Printf("cfapi: hydrator: ReportProgress %s: %v", req.LocalPath, err)
			}
			if h.emitter != nil {
				h.emitter.Emit("cf:hydration_progress", map[string]any{
					"backendID": h.backendID,
					"localPath": req.LocalPath,
					"byteDone":  req.Offset + transferred,
					"byteTotal": req.Offset + req.Length,
				})
			}
		}
	}

	// Mark file as in-sync after full hydration.
	if err := h.provider.SetSyncState(req.LocalPath, SyncStateSynced); err != nil {
		log.Printf("cfapi: hydrator: SetSyncState synced %s: %v", req.LocalPath, err)
	}
	return nil
}

// OnCancelFetch cancels the in-flight fetch for localPath.
func (h *Hydrator) OnCancelFetch(req FetchRequest) {
	h.cancelMu.Lock()
	cancel, ok := h.fetchCancels[req.LocalPath]
	h.cancelMu.Unlock()
	if ok {
		cancel()
	}
}

// OnFetchPlaceholders lists the backend at the given local path (converted to
// remote path) and creates CF placeholders for any missing entries.
// A 30s deadline is applied to avoid leaving the OS FETCH_PLACEHOLDERS
// operation pending indefinitely if the backend is slow or unreachable.
func (h *Hydrator) OnFetchPlaceholders(ctx context.Context, localPath string) error {
	log.Printf("cfapi: hydrator OnFetchPlaceholders: localPath=%q", localPath)
	if ctx == nil {
		ctx = context.Background()
	}
	// Apply a hard deadline so the ack is sent within the OS timeout window.
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, fetchPlaceholdersTimeout)
	defer cancel()

	if h.backend == nil {
		return nil
	}

	remotePath := h.localToRemote(localPath)
	items, err := h.backend.List(ctx, remotePath)
	if err != nil {
		return fmt.Errorf("cfapi: hydrator: List %s: %w", remotePath, err)
	}

	placeholders := make([]PlaceholderInfo, 0, len(items))
	for _, fi := range items {
		placeholders = append(placeholders, PlaceholderInfo{
			RelativePath: fi.Name,
			FileSize:     fi.Size,
			ModTime:      fi.ModTime,
			FileID:       fi.Version,
			IsDirectory:  fi.IsDir,
		})
	}

	if len(placeholders) == 0 {
		return nil
	}

	// Pass localPath (the directory being populated by this callback) as baseDir,
	// NOT h.provider.localPath (the sync root). See fix #133: using the sync root
	// causes all sub-folder contents to land at the mount root instead of the
	// correct sub-directory.
	_, err = h.provider.CreatePlaceholders(localPath, placeholders)
	return err
}

// getStatCached returns the ETag and mtime for remotePath, using a short-lived
// in-memory cache to avoid N+1 Stat() calls when CF triggers multiple FETCH_DATA
// callbacks for the same file (one per chunk range).
func (h *Hydrator) getStatCached(ctx context.Context, remotePath string) (string, time.Time) {
	h.statMu.Lock()
	if e, ok := h.statCacheMap[remotePath]; ok && time.Since(e.storedAt) < statCacheTTL {
		h.statMu.Unlock()
		return e.etag, e.mtime
	}
	h.statMu.Unlock()

	if h.backend == nil {
		return "", time.Time{}
	}
	fi, err := h.backend.Stat(ctx, remotePath)
	if err != nil {
		return "", time.Time{}
	}

	h.statMu.Lock()
	h.statCacheMap[remotePath] = &statCacheEntry{
		etag:     fi.Version,
		mtime:    fi.ModTime,
		storedAt: time.Now(),
	}
	h.statMu.Unlock()

	return fi.Version, fi.ModTime
}

// localToRemote converts an absolute local path to a relative remote path
// by stripping the sync root prefix (the provider's localPath).
// If localPath does not start with the sync root, it is returned as-is with a warning.
func (h *Hydrator) localToRemote(localPath string) string {
	if h.provider == nil {
		return localPath
	}
	root := h.provider.localPath
	// Guard: ensure the path is actually inside the sync root (MINEUR-4).
	if !strings.HasPrefix(localPath, root) {
		log.Printf("cfapi: localToRemote: path %q is outside sync root %q — returning as-is", localPath, root)
		return localPath
	}
	rel := strings.TrimPrefix(localPath, root)
	if rel == "" {
		rel = "/"
	}
	// Normalise Windows backslashes to forward slashes.
	rel = strings.ReplaceAll(rel, "\\", "/")
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	return rel
}
