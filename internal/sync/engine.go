package sync

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	gosync "sync"
	"time"

	"github.com/CCoupel/GhostDrive/internal/config"
	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/CCoupel/GhostDrive/plugins"
)

// Engine orchestrates bidirectional synchronization between a local directory
// and a remote backend.
type Engine struct {
	backend    plugins.StorageBackend
	localDir   string
	remotePath string
	cfg        config.AppConfig
	emitter    EventEmitter

	mu         gosync.RWMutex
	state      types.SyncState
	cancelFunc context.CancelFunc
	paused     bool
}

// NewEngine creates a new SyncEngine.
// localDir is the local synchronization directory.
// remotePath is the root path on the backend.
// emitter is used to emit Wails events; pass nil to use NoopEmitter.
func NewEngine(
	backend plugins.StorageBackend,
	localDir, remotePath string,
	cfg config.AppConfig,
	emitter EventEmitter,
) *Engine {
	if emitter == nil {
		emitter = &NoopEmitter{}
	}
	return &Engine{
		backend:    backend,
		localDir:   localDir,
		remotePath: remotePath,
		cfg:        cfg,
		emitter:    emitter,
		state: types.SyncState{
			Status: types.SyncIdle,
			Errors: []types.SyncErrorInfo{},
		},
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

// GetState returns the current sync state (safe for concurrent access).
func (e *Engine) GetState() types.SyncState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
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
				remoteEvents = make(chan plugins.FileEvent) // prevent tight loop
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
	dispatcher := NewDispatcher(e.backend, e.emitter, e.localDir)
	remotePath := path.Join(e.remotePath, evt.Path)

	switch evt.Type {
	case plugins.FileEventCreated, plugins.FileEventModified:
		return dispatcher.Dispatch(ctx, []SyncAction{{
			Type:       ActionUpload,
			LocalPath:  filepath.Join(e.localDir, evt.Path),
			RemotePath: remotePath,
		}})
	case plugins.FileEventDeleted:
		return dispatcher.Dispatch(ctx, []SyncAction{{
			Type:       ActionDelete,
			LocalPath:  filepath.Join(e.localDir, evt.Path),
			RemotePath: remotePath,
		}})
	}
	return nil
}

func (e *Engine) handleRemoteEvent(ctx context.Context, evt plugins.FileEvent) error {
	dispatcher := NewDispatcher(e.backend, e.emitter, e.localDir)
	localPath := filepath.Join(e.localDir, evt.Path)

	switch evt.Type {
	case plugins.FileEventCreated, plugins.FileEventModified:
		return dispatcher.Dispatch(ctx, []SyncAction{{
			Type:       ActionDownload,
			LocalPath:  localPath,
			RemotePath: path.Join(e.remotePath, evt.Path),
		}})
	case plugins.FileEventDeleted:
		// Remote deletion → could remove locally; V1 is conservative (no local delete on remote event)
		e.emitter.Emit("sync:error", map[string]any{
			"path":    evt.Path,
			"message": "remote file deleted — manual review required (V1 policy)",
		})
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
	e.mu.Lock()
	defer e.mu.Unlock()
	syncErr := types.SyncErrorInfo{
		Path:    path,
		Message: message,
		Time:    time.Now(),
	}
	e.state.Errors = append(e.state.Errors, syncErr)
	e.state.Status = types.SyncError
	e.emitter.Emit("sync:error", syncErr)
}
