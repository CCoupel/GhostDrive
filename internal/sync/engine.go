package sync

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	gosync "sync"
	"strings"
	"time"

	"github.com/CCoupel/GhostDrive/internal/config"
	"github.com/CCoupel/GhostDrive/internal/logger"
	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/CCoupel/GhostDrive/plugins"
)

// intentConsumeFunc is the signature of cfapi.CFManager.ConsumeIntent, injected
// into Engine to avoid an import cycle (cfapi → app → sync).
type intentConsumeFunc func(localPath string) (backendID string, pin bool, ok bool)

// suppressSlack is added on top of debounceDuration to compute the TTL for
// locally suppressed paths.  It absorbs channel queuing and scheduling jitter
// so that watcher events triggered by handleRemoteEvent's os.Remove/os.Rename
// are reliably filtered before handleLocalEvent sees them (#150).
const suppressSlack = 1000 * time.Millisecond

// Engine orchestrates bidirectional synchronization between a local directory
// and a remote backend.
type Engine struct {
	backend    plugins.StorageBackend
	localDir   string
	remotePath string
	cfg        config.AppConfig
	emitter    EventEmitter
	backendID  string         // stable backend UUID — used for CF state updates
	cfManager  CFStateManager // optional; nil → no CF badge updates

	// fileStates is the per-file synchronization state cache (#136).
	// Keys are absolute local paths; values are types.FileState.
	// Uses sync.Map for lock-free concurrent reads/writes from Dispatcher goroutines.
	fileStates gosync.Map

	// suppressedPaths tracks local paths temporarily ignored by handleLocalEvent.
	// Set by handleRemoteEvent before os.Remove/os.Rename to prevent the resulting
	// fsnotify events from re-triggering a remote ActionDelete/ActionRename (#150).
	// Keys: absolute local paths; Values: time.Time expiry.
	suppressedPaths gosync.Map

	// consumeIntent is injected by App to consume pin/unpin intentions after
	// a file transitions U→S (#143).  nil means no intent queue.
	consumeIntent intentConsumeFunc
	// pinFile is injected by App to execute a deferred PinFile after U→S (#143).
	pinFile func(backendID, localPath string, pin bool) error

	mu         gosync.RWMutex
	state      types.SyncState
	cancelFunc context.CancelFunc
	paused     bool
}

// NewEngine creates a new SyncEngine.
// backendID is the stable backend UUID (from BackendConfig.ID) used for CF state updates.
// localDir is the local synchronization directory.
// remotePath is the root path on the backend.
// emitter is used to emit Wails events; pass nil to use NoopEmitter.
func NewEngine(
	backendID string,
	backend plugins.StorageBackend,
	localDir, remotePath string,
	cfg config.AppConfig,
	emitter EventEmitter,
) *Engine {
	if emitter == nil {
		emitter = &NoopEmitter{}
	}
	return &Engine{
		backendID:  backendID,
		backend:    backend,
		localDir:   localDir,
		remotePath: remotePath,
		cfg:        cfg,
		emitter:    emitter,
		state: types.SyncState{
			Status:          types.SyncIdle,
			Errors:          []types.SyncErrorInfo{},
			Backends:        []types.BackendSyncState{},
			ActiveTransfers: []types.ProgressEvent{},
		},
	}
}

// SetCFManager injects a CFStateManager so the engine can update Windows file
// badges after sync operations.  Must be called before Start().
func (e *Engine) SetCFManager(m CFStateManager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfManager = m
}

// ─── Suppress mechanism (#150) ────────────────────────────────────────────────

// suppressLocalEvent marks localPath to be ignored by handleLocalEvent for
// debounceDuration+suppressSlack (≈1.5s total). Call this in handleRemoteEvent
// BEFORE any os.Remove or os.Rename so the resulting fsnotify feedback events
// are silently dropped instead of being re-dispatched as local changes.
func (e *Engine) suppressLocalEvent(localPath string) {
	expiry := time.Now().Add(debounceDuration + suppressSlack)
	e.suppressedPaths.Store(localPath, expiry)
}

// isLocalEventSuppressed returns true if localPath was recently mutated by a
// remote-event handler and the resulting watcher event should be skipped.
// Expired entries are lazily deleted on first check.
func (e *Engine) isLocalEventSuppressed(localPath string) bool {
	v, ok := e.suppressedPaths.Load(localPath)
	if !ok {
		return false
	}
	expiry, ok := v.(time.Time)
	if !ok {
		e.suppressedPaths.Delete(localPath)
		return false
	}
	if time.Now().After(expiry) {
		e.suppressedPaths.Delete(localPath)
		return false
	}
	return true
}

// SetIntentCallbacks injects the pin intent consumer and executor for #143.
// consumeFn reads and removes a pending PinIntent for a local path.
// pinFn executes the deferred PinFile operation.
// Both may be nil (no-op).
func (e *Engine) SetIntentCallbacks(consumeFn intentConsumeFunc, pinFn func(backendID, localPath string, pin bool) error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.consumeIntent = consumeFn
	e.pinFile = pinFn
}

// ─── FileState API (#136) ─────────────────────────────────────────────────────

// GetFileState returns the current synchronization state for a local file path.
// Returns FileStateUnknown ("") if the path is not tracked.
func (e *Engine) GetFileState(localPath string) types.FileState {
	if v, ok := e.fileStates.Load(localPath); ok {
		if s, ok := v.(types.FileState); ok {
			return s
		}
	}
	return types.FileStateUnknown
}

// SetFileState stores the synchronization state for a local file path.
// If state == FileStateUnknown, the entry is deleted from the map.
func (e *Engine) SetFileState(localPath string, state types.FileState) {
	if state == types.FileStateUnknown {
		e.fileStates.Delete(localPath)
		return
	}
	e.fileStates.Store(localPath, state)
}

// makeStateUpdater returns a func suitable for Dispatcher.SetStateUpdater.
// It updates fileStates and emits sync:file-state-changed.
// When a file transitions to U→S, it consumes any pending pin intent (#143).
func (e *Engine) makeStateUpdater() func(localPath string, state types.FileState) {
	return func(localPath string, state types.FileState) {
		prev := e.GetFileState(localPath)
		// #149 — guard: never downgrade a Synced file to Local. The reconciler may race
		// with a concurrent upload and incorrectly classify a just-uploaded file as
		// local-only (because the remote listing snapshot was captured before the upload
		// completed). Allowing S→L would cause ActionDelete to be skipped for a genuinely
		// synced file, making the deleted file reappear after the next reconciliation.
		if prev == types.FileStateSynced && state == types.FileStateLocal {
			return
		}
		e.SetFileState(localPath, state)
		if prev == state {
			return // no-op: avoid flooding the frontend with identical state events
		}
		e.emitter.Emit("sync:file-state-changed", map[string]string{
			"backendID": e.backendID,
			"localPath": localPath,
			"state":     string(state),
		})
		// Consume pending pin/unpin intent when transfer completes (#143).
		if prev == types.FileStateUploading && state == types.FileStateSynced {
			e.mu.RLock()
			consumeFn := e.consumeIntent
			pinFn := e.pinFile
			e.mu.RUnlock()
			if consumeFn != nil {
				if bid, pin, ok := consumeFn(localPath); ok && pinFn != nil {
					go func() { _ = pinFn(bid, localPath, pin) }()
				}
			}
		}
	}
}

// Start begins the sync engine: initial full reconciliation + watcher loop.
// It is non-blocking — the engine runs in background goroutines.
// Call Stop() to shut down.
func (e *Engine) Start(ctx context.Context) error {
	if !e.backend.IsConnected() {
		return fmt.Errorf("sync: engine: backend not connected")
	}

	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	if e.cancelFunc != nil {
		e.mu.Unlock()
		cancel()
		return fmt.Errorf("sync: engine: already running")
	}
	e.cancelFunc = cancel
	e.mu.Unlock()

	go e.run(ctx)
	return nil
}

// Stop gracefully shuts down the engine.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancelFunc != nil {
		e.cancelFunc()
		e.cancelFunc = nil
	}
}

// Pause suspends sync processing (does not stop the watcher).
func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.paused {
		e.paused = true
		e.state = types.SyncState{
			Status:   types.SyncPaused,
			LastSync: e.state.LastSync,
			Errors:   e.state.Errors,
		}
	}
}

// Resume resumes a paused engine.
func (e *Engine) Resume() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.paused {
		e.paused = false
		e.state = types.SyncState{
			Status:   types.SyncIdle,
			LastSync: e.state.LastSync,
			Errors:   e.state.Errors,
		}
	}
}

// ForceSync triggers an immediate full reconciliation regardless of the watcher.
func (e *Engine) ForceSync(ctx context.Context) error {
	return e.runFullSync(ctx)
}

// HandleCFDelete propagates a CF API NOTIFY_DELETE_COMPLETION event to the backend.
// Called when Windows reports that a delete operation on the CF sync root completed
// (i.e. user deleted a file in GhD: via Explorer). The delete is forwarded to the
// remote backend so both sides stay consistent (#v2.2-bugD).
func (e *Engine) HandleCFDelete(localPath string) {
	e.mu.RLock()
	ctx := context.Background()
	if e.cancelFunc == nil {
		e.mu.RUnlock()
		return // engine not running
	}
	e.mu.RUnlock()

	// Build the remote path by stripping the local sync root prefix.
	relPath := filepath.ToSlash(strings.TrimPrefix(localPath, e.localDir))
	if relPath == "" || relPath == "/" {
		return // do not delete the root
	}
	remotePath := path.Join(e.remotePath, relPath)

	// Skip if not synced — file may have been local-only (never uploaded).
	if e.GetFileState(localPath) == types.FileStateLocal {
		e.fileStates.Delete(localPath)
		return
	}

	dispatcher := NewDispatcher(e.backend, e.emitter, e.localDir)
	e.mu.RLock()
	cfMgr := e.cfManager
	backendID := e.backendID
	e.mu.RUnlock()
	if cfMgr != nil && backendID != "" {
		dispatcher.SetCFManager(backendID, cfMgr)
	}
	dispatcher.SetStateUpdater(e.makeStateUpdater())

	logger.Info("engine: HandleCFDelete %s → remote %s", localPath, remotePath)
	if err := dispatcher.Dispatch(ctx, []SyncAction{{
		Type:       ActionDelete,
		LocalPath:  localPath,
		RemotePath: remotePath,
	}}); err != nil {
		e.recordError(localPath, "CF delete: "+err.Error())
	}
}

// HandleCFRename propagates a CF API NOTIFY_RENAME_COMPLETION event to the backend.
// Called when Windows reports a rename/move on the CF sync root completed (#v2.2-bugD).
func (e *Engine) HandleCFRename(oldLocalPath, newLocalPath string) {
	e.mu.RLock()
	if e.cancelFunc == nil {
		e.mu.RUnlock()
		return // engine not running
	}
	e.mu.RUnlock()

	oldRelPath := filepath.ToSlash(strings.TrimPrefix(oldLocalPath, e.localDir))
	newRelPath := filepath.ToSlash(strings.TrimPrefix(newLocalPath, e.localDir))
	if oldRelPath == "" || newRelPath == "" {
		return
	}
	oldRemotePath := path.Join(e.remotePath, oldRelPath)
	newRemotePath := path.Join(e.remotePath, newRelPath)

	dispatcher := NewDispatcher(e.backend, e.emitter, e.localDir)
	e.mu.RLock()
	cfMgr := e.cfManager
	backendID := e.backendID
	e.mu.RUnlock()
	if cfMgr != nil && backendID != "" {
		dispatcher.SetCFManager(backendID, cfMgr)
	}
	dispatcher.SetStateUpdater(e.makeStateUpdater())

	// Transfer state from old path to new before dispatch.
	oldState := e.GetFileState(oldLocalPath)
	e.fileStates.Delete(oldLocalPath)
	if oldState != types.FileStateLocal {
		e.SetFileState(newLocalPath, oldState)
	}

	logger.Info("engine: HandleCFRename %s → %s (remote %s → %s)",
		oldLocalPath, newLocalPath, oldRemotePath, newRemotePath)
	if err := dispatcher.Dispatch(context.Background(), []SyncAction{{
		Type:          ActionRename,
		LocalPath:     newLocalPath,
		RemotePath:    newRemotePath,
		SrcLocalPath:  oldLocalPath,
		SrcRemotePath: oldRemotePath,
	}}); err != nil {
		e.recordError(newLocalPath, "CF rename: "+err.Error())
	}
}

// UploadFile dispatches an ActionUpload for a single local file and waits for
// completion. This is used by PinFile (#142) to upload a specific file before
// dehydrating it, avoiding the full-reconciliation side-effects of ForceSync.
func (e *Engine) UploadFile(ctx context.Context, localPath string) error {
	dispatcher := NewDispatcher(e.backend, e.emitter, e.localDir)
	e.mu.RLock()
	cfMgr := e.cfManager
	backendID := e.backendID
	e.mu.RUnlock()
	if cfMgr != nil && backendID != "" {
		dispatcher.SetCFManager(backendID, cfMgr)
	}
	dispatcher.SetStateUpdater(e.makeStateUpdater())

	rel, err := filepath.Rel(e.localDir, localPath)
	if err != nil {
		return fmt.Errorf("uploadfile: resolve path: %w", err)
	}
	remotePath := path.Join(e.remotePath, filepath.ToSlash(rel))
	return dispatcher.Dispatch(ctx, []SyncAction{{
		Type:       ActionUpload,
		LocalPath:  localPath,
		RemotePath: remotePath,
	}})
}

// GetState returns the current sync state (safe for concurrent access).
// Nil slices are replaced with empty slices so JSON serialisation always
// produces "[]" rather than "null", preventing frontend .map() crashes.
func (e *Engine) GetState() types.SyncState {
	e.mu.RLock()
	s := e.state
	e.mu.RUnlock()

	if s.Errors == nil {
		s.Errors = []types.SyncErrorInfo{}
	}
	if s.Backends == nil {
		s.Backends = []types.BackendSyncState{}
	}
	if s.ActiveTransfers == nil {
		s.ActiveTransfers = []types.ProgressEvent{}
	}
	return s
}

// ─── Internal ────────────────────────────────────────────────────────────────

func (e *Engine) run(ctx context.Context) {
	// Initial full sync
	if err := e.runFullSync(ctx); err != nil && ctx.Err() == nil {
		e.recordError("", err.Error())
	}

	// Start local watcher
	watcher, err := NewWatcher(e.localDir)
	if err != nil {
		e.recordError("", fmt.Sprintf("watcher: %v", err))
		return
	}

	events, err := watcher.Start(ctx)
	if err != nil {
		e.recordError("", fmt.Sprintf("watcher start: %v", err))
		return
	}
	defer watcher.Stop()

	// Start remote watch
	remoteEvents, err := e.backend.Watch(ctx, e.remotePath)
	if err != nil {
		// Non-fatal — some backends may not support watch
		remoteEvents = make(chan plugins.FileEvent)
	}

	for {
		select {
		case <-ctx.Done():
			e.setState(types.SyncState{
				Status:   types.SyncIdle,
				LastSync: e.state.LastSync,
				Errors:   e.state.Errors,
			})
			return

		case evt, ok := <-events:
			if !ok {
				return
			}
			if e.isPaused() {
				continue
			}
			e.emitter.Emit("sync:file-event", evt)
			if err := e.handleLocalEvent(ctx, evt); err != nil && ctx.Err() == nil {
				e.recordError(evt.Path, err.Error())
			}

		case evt, ok := <-remoteEvents:
			if !ok {
				// Channel closed by the backend Watch goroutine.
				// If the context is still live this is a persistent backend
				// failure (e.g. server down) — record it so aggregateStatus
				// promotes the tray icon to the error colour (#115).
				if ctx.Err() == nil {
					e.recordError("", "backend unreachable: remote watch channel closed unexpectedly")
				}
				remoteEvents = make(chan plugins.FileEvent) // prevent tight loop
				continue
			}
			// Sentinel events: Watch() signals backend availability changes.
			// Processed regardless of pause state.
			if evt.Type == plugins.FileEventBackendOffline {
				// First consecutive failure → SyncOffline (orange tray, #115b).
				if ctx.Err() == nil {
					e.recordOffline()
				}
				continue
			}
			if evt.Type == plugins.FileEventBackendOnline {
				// Recovery: poll succeeded after ≥1 failure → back to SyncIdle (#115b).
				if ctx.Err() == nil {
					e.recordOnline()
				}
				continue
			}
			if e.isPaused() {
				continue
			}
			e.emitter.Emit("sync:file-event", evt)
			if err := e.handleRemoteEvent(ctx, evt); err != nil && ctx.Err() == nil {
				e.recordError(evt.Path, err.Error())
			}
		}
	}
}

func (e *Engine) runFullSync(ctx context.Context) error {
	e.mu.Lock()
	if e.paused {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()

	e.setState(types.SyncState{
		Status:   types.SyncSyncing,
		LastSync: e.state.LastSync,
		Errors:   e.state.Errors,
	})
	e.emitter.Emit("sync:state-changed", e.GetState())

	logPath := filepath.Join(e.localDir, "sync.log")
	reconciler := NewReconciler(e.backend, e.localDir, logPath)
	dispatcher := NewDispatcher(e.backend, e.emitter, e.localDir)

	// Phase 4 — wire CF state manager so per-file badges are updated.
	e.mu.RLock()
	cfMgr := e.cfManager
	backendID := e.backendID
	e.mu.RUnlock()
	if cfMgr != nil && backendID != "" {
		dispatcher.SetCFManager(backendID, cfMgr)
	}

	// #136 — wire per-file state updater.
	stateUpdater := e.makeStateUpdater()
	dispatcher.SetStateUpdater(stateUpdater)
	reconciler.SetStateUpdater(stateUpdater)

	// #144 — wire conflict event emitter.
	bid := backendID
	reconciler.SetConflictEmitter(func(payload map[string]any) {
		payload["backendID"] = bid
		e.emitter.Emit("sync:conflict", payload)
	})

	actions, err := reconciler.Reconcile(ctx, e.remotePath)
	if err != nil {
		e.setState(types.SyncState{
			Status:   types.SyncError,
			LastSync: e.state.LastSync,
			Errors:   e.state.Errors,
		})
		e.emitter.Emit("sync:state-changed", e.GetState())
		return fmt.Errorf("sync: full sync reconcile: %w", err)
	}

	e.mu.Lock()
	e.state.Pending = len(actions)
	e.mu.Unlock()

	if err := dispatcher.Dispatch(ctx, actions); err != nil && ctx.Err() == nil {
		e.recordError("", err.Error())
	}

	// Phase 4 — mark the sync root as in-sync after full reconciliation.
	if cfMgr != nil && backendID != "" {
		_ = cfMgr.SetSyncState(backendID, e.localDir, CFSyncStateSynced)
	}

	now := time.Now()
	e.setState(types.SyncState{
		Status:   types.SyncIdle,
		LastSync: now,
		Errors:   e.state.Errors,
	})
	e.emitter.Emit("sync:state-changed", e.GetState())

	return nil
}

func (e *Engine) handleLocalEvent(ctx context.Context, evt plugins.FileEvent) error {
	localPath := filepath.Join(e.localDir, evt.Path)

	// #150 — skip local events that were triggered by handleRemoteEvent applying a
	// remote change locally (os.Remove, os.Rename).  Without this guard the watcher
	// feedback would dispatch a second ActionDelete/ActionRename to the backend,
	// potentially creating conflicts or spurious errors.
	if e.isLocalEventSuppressed(localPath) {
		return nil
	}
	if evt.OldPath != "" {
		if e.isLocalEventSuppressed(filepath.Join(e.localDir, evt.OldPath)) {
			return nil
		}
	}

	dispatcher := NewDispatcher(e.backend, e.emitter, e.localDir)
	e.mu.RLock()
	cfMgr := e.cfManager
	backendID := e.backendID
	e.mu.RUnlock()
	if cfMgr != nil && backendID != "" {
		dispatcher.SetCFManager(backendID, cfMgr)
	}
	// #136 — wire state updater.
	dispatcher.SetStateUpdater(e.makeStateUpdater())

	// #153 — convert evt.Path to forward-slash before path.Join.
	// fsnotify on Windows returns paths with OS-native separators (backslashes).
	// path.Join (from "path" package) does NOT normalise backslashes; a path like
	// "subdir\file.txt" is treated as a single filename component, producing
	// "/remote/subdir\file.txt" instead of "/remote/subdir/file.txt".
	// This causes backend operations (Delete, Rename, Upload) to target the wrong
	// remote path → 404 errors → remote file preserved → file revives on next sync.
	remoteRelPath := filepath.ToSlash(evt.Path)
	remotePath := path.Join(e.remotePath, remoteRelPath)

	switch evt.Type {
	case plugins.FileEventCreated, plugins.FileEventModified:
		return dispatcher.Dispatch(ctx, []SyncAction{{
			Type:       ActionUpload,
			LocalPath:  localPath,
			RemotePath: remotePath,
		}})
	case plugins.FileEventDeleted:
		// #141 — skip remote ActionDelete for files that only exist locally (never synced).
		if e.GetFileState(localPath) == types.FileStateLocal {
			e.fileStates.Delete(localPath)
			return nil
		}
		return dispatcher.Dispatch(ctx, []SyncAction{{
			Type:       ActionDelete,
			LocalPath:  localPath,
			RemotePath: remotePath,
		}})
	case plugins.FileEventRenamed:
		// #139 — atomic server-side rename.
		if evt.OldPath == "" {
			break // incomplete pair — ignore
		}
		srcLocalPath := filepath.Join(e.localDir, evt.OldPath)
		// #153 — same forward-slash normalisation for the source remote path.
		srcRemoteRelPath := filepath.ToSlash(evt.OldPath)
		// Transfer state from old path to new (P→S on success via stateUpdater).
		oldState := e.GetFileState(srcLocalPath)
		e.fileStates.Delete(srcLocalPath)
		// #149 — don't inherit FileStateLocal: the renamed file is about to be
		// synced via ActionRename. Inheriting Local state would cause a subsequent
		// delete of the new path to silently skip ActionDelete (via the #141 guard),
		// leaving the remote copy intact and making the file reappear locally after
		// the next reconciliation.
		if oldState != types.FileStateLocal {
			e.SetFileState(localPath, oldState)
		}
		return dispatcher.Dispatch(ctx, []SyncAction{{
			Type:          ActionRename,
			LocalPath:     localPath,
			RemotePath:    remotePath,
			SrcLocalPath:  srcLocalPath,
			SrcRemotePath: path.Join(e.remotePath, srcRemoteRelPath),
		}})
	}
	return nil
}

func (e *Engine) handleRemoteEvent(ctx context.Context, evt plugins.FileEvent) error {
	dispatcher := NewDispatcher(e.backend, e.emitter, e.localDir)
	e.mu.RLock()
	cfMgr := e.cfManager
	backendID := e.backendID
	e.mu.RUnlock()
	if cfMgr != nil && backendID != "" {
		dispatcher.SetCFManager(backendID, cfMgr)
	}
	// #136 — wire state updater.
	dispatcher.SetStateUpdater(e.makeStateUpdater())

	// #155 — normalise evt.Path from the remote Watch() before building the local path.
	// Remote backends may return paths with a leading "/" (e.g. "/file.txt"); on
	// Windows, filepath.Join("C:\\GhostDrive\\Backend", "/file.txt") = "C:\file.txt"
	// (the absolute path from the remote wins, stripping the sync-root prefix).
	// Strip leading slashes/backslashes and convert to OS-native separators first.
	relPath := strings.TrimLeft(filepath.FromSlash(evt.Path), "/\\")
	localPath := filepath.Join(e.localDir, relPath)

	switch evt.Type {
	case plugins.FileEventCreated, plugins.FileEventModified:
		// Suppress the local watcher event that ActionDownload will trigger so
		// handleLocalEvent does not attempt a redundant ActionUpload (#150).
		e.suppressLocalEvent(localPath)
		return dispatcher.Dispatch(ctx, []SyncAction{{
			Type:       ActionDownload,
			LocalPath:  localPath,
			RemotePath: path.Join(e.remotePath, filepath.ToSlash(relPath)),
		}})

	case plugins.FileEventDeleted:
		// #150 — propagate remote deletion to the local filesystem.
		// Previously a "V1 policy" stub only emitted a warning; v2.2 now removes
		// the local file so both sides stay consistent.
		//
		// Suppress the resulting fsnotify Delete event so handleLocalEvent does not
		// dispatch a second ActionDelete to the already-gone remote file.
		e.suppressLocalEvent(localPath)
		if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
			logger.Error("handleRemoteEvent: remove local %s: %v", localPath, err)
			return fmt.Errorf("sync: handleRemoteEvent: remove local %s: %w", localPath, err)
		}
		logger.Info("handleRemoteEvent: removed local %s (remote deleted)", localPath)
		e.fileStates.Delete(localPath)
		// #154 — notify Explorer to refresh the parent directory.
		// Pass localPath (file) so NotifyLocalChange computes filepath.Dir internally.
		if cfMgr != nil && backendID != "" {
			if lr, ok := cfMgr.(LocalRefresher); ok {
				lr.NotifyLocalChange(localPath)
			}
		}

	case plugins.FileEventRenamed:
		// Remote rename → apply locally as an atomic os.Rename (#148).
		// Suppress local events for both old and new paths so handleLocalEvent
		// does not re-dispatch ActionRename back to the already-renamed backend (#150).
		if evt.OldPath == "" {
			break // incomplete event — no old path, nothing to rename
		}
		// #155 — same leading-slash normalisation for the source path.
		oldRelPath := strings.TrimLeft(filepath.FromSlash(evt.OldPath), "/\\")
		oldLocalPath := filepath.Join(e.localDir, oldRelPath)
		// #150 — suppress before touching the filesystem.
		e.suppressLocalEvent(oldLocalPath)
		e.suppressLocalEvent(localPath)
		if err := os.Rename(oldLocalPath, localPath); err != nil {
			if !os.IsNotExist(err) {
				logger.Error("handleRemoteEvent: rename local %s → %s: %v", oldLocalPath, localPath, err)
				return fmt.Errorf("sync: handleRemoteEvent: rename local %s → %s: %w", oldLocalPath, localPath, err)
			}
			// Old local file not found (not yet synced locally, or already gone).
			// #150 — defensively remove any stale placeholder at the old path.
			_ = os.Remove(oldLocalPath)
			// Fall back: download the new remote file.
			return dispatcher.Dispatch(ctx, []SyncAction{{
				Type:       ActionDownload,
				LocalPath:  localPath,
				RemotePath: path.Join(e.remotePath, filepath.ToSlash(relPath)),
			}})
		}
		logger.Info("handleRemoteEvent: renamed local %s → %s (remote renamed)", oldLocalPath, localPath)
		// Rename succeeded — clean up state for old path and mark new path synced.
		e.fileStates.Delete(oldLocalPath)
		e.makeStateUpdater()(localPath, types.FileStateSynced)
		// #154 — notify Explorer after local rename.
		// Pass localPath (file) so NotifyLocalChange computes filepath.Dir internally.
		if cfMgr != nil && backendID != "" {
			if lr, ok := cfMgr.(LocalRefresher); ok {
				lr.NotifyLocalChange(localPath)
			}
		}
	}
	return nil
}

func (e *Engine) setState(s types.SyncState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = s
}

func (e *Engine) isPaused() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.paused
}

func (e *Engine) recordError(path, message string) {
	// Build and record the error under lock, then emit without holding the lock.
	// Holding the lock during Emit() risks deadlock: if the Wails event buffer
	// is full the Emit blocks, and any concurrent GetState() call (e.g. from the
	// systray icon-update goroutine) would then also block waiting for the lock.
	syncErr := types.SyncErrorInfo{
		Path:    path,
		Message: message,
		Time:    time.Now(),
	}
	e.mu.Lock()
	e.state.Errors = append(e.state.Errors, syncErr)
	e.state.Status = types.SyncError
	e.mu.Unlock()
	e.emitter.Emit("sync:error", syncErr)
}

// recordOffline transitions the engine to SyncOffline state and emits
// "sync:offline".  Called when Watch() sends the FileEventBackendOffline
// sentinel on its first consecutive poll failure (#115b).
// Unlike recordError it does not append to the error list — offline is
// transient.  If the backend remains unreachable, Watch eventually closes
// its channel and recordError escalates the state to SyncError.
func (e *Engine) recordOffline() {
	e.mu.Lock()
	e.state.Status = types.SyncOffline
	e.mu.Unlock()
	e.emitter.Emit("sync:offline", map[string]string{
		"message": "backend unreachable: connection lost (offline, retrying with backoff)",
	})
}

// recordOnline clears the SyncOffline state and returns the engine to SyncIdle.
// Called when Watch() sends the FileEventBackendOnline sentinel after a
// successful poll following one or more failures (#115b).
// It only acts when the engine is currently in SyncOffline — it does not
// override SyncError (persistent failure from a closed Watch channel).
func (e *Engine) recordOnline() {
	e.mu.Lock()
	if e.state.Status == types.SyncOffline {
		e.state.Status = types.SyncIdle
	}
	e.mu.Unlock()
	e.emitter.Emit("sync:online", map[string]string{
		"message": "backend reachable again",
	})
}
