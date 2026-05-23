package sync

import (
	"context"
	"errors"
	"fmt"
	gosync "sync"

	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/CCoupel/GhostDrive/plugins"
)

const maxConcurrent = 4

// CFSyncStateSynced mirrors cfapi.SyncStateSynced (= 2).
// Using a named constant avoids magic literals and decouples sync/ from the cfapi/ package.
const CFSyncStateSynced = 2

// CFSyncStateCloudOnly mirrors cfapi.SyncStateCloudOnly (= 0).
// Used by the unpin sequence in App.PinFile (#142).
const CFSyncStateCloudOnly = 0

// EventEmitter is the interface the dispatcher uses to emit Wails events.
// It is injectable to allow testing without Wails runtime.
type EventEmitter interface {
	Emit(event string, data any)
}

// NoopEmitter is an EventEmitter that discards all events (used in tests).
type NoopEmitter struct{}

func (n *NoopEmitter) Emit(_ string, _ any) {}

// CFStateManager is the interface for updating the Windows Cloud Filter API
// sync state (file badge) after a successful download or upload.
// Satisfied by *cfapi.CFManager on Windows; nil elsewhere.
//
// state values mirror cfapi.SyncState:
//
//	0 = CloudOnly  ☁️
//	1 = Syncing    ⟳
//	2 = Synced     ✓✓
//	3 = Pinned     ⚡
//	4 = Unpinned
type CFStateManager interface {
	SetSyncState(backendID, localPath string, state int) error
}

// Dispatcher executes SyncActions with bounded concurrency.
type Dispatcher struct {
	backend      plugins.StorageBackend
	emitter      EventEmitter
	localRoot    string
	sem          chan struct{}
	backendID    string                                      // for CF state updates; empty means no-op
	cfManager    CFStateManager                              // optional; nil → CF state not updated
	stateUpdater func(localPath string, state types.FileState) // optional; nil → no-op (#136)
}

// NewDispatcher creates a Dispatcher with a bounded semaphore.
// localRoot is used for path-traversal validation on download/upload tasks.
func NewDispatcher(backend plugins.StorageBackend, emitter EventEmitter, localRoot string) *Dispatcher {
	if emitter == nil {
		emitter = &NoopEmitter{}
	}
	return &Dispatcher{
		backend:   backend,
		emitter:   emitter,
		localRoot: localRoot,
		sem:       make(chan struct{}, maxConcurrent),
	}
}

// SetCFManager injects a CFStateManager so the dispatcher can update Windows
// file badges after successful downloads/uploads.
// backendID identifies this backend in CFStateManager.SetSyncState calls.
func (d *Dispatcher) SetCFManager(backendID string, m CFStateManager) {
	d.backendID = backendID
	d.cfManager = m
}

// SetStateUpdater injects a callback called on every file state transition (#136).
// The callback may be called concurrently from multiple goroutines; it must be
// thread-safe. Pass nil to disable state tracking.
func (d *Dispatcher) SetStateUpdater(fn func(localPath string, state types.FileState)) {
	d.stateUpdater = fn
}

// updateState calls d.stateUpdater if non-nil. Errors are silently ignored
// following the pattern of the existing CF badge update code.
func (d *Dispatcher) updateState(localPath string, state types.FileState) {
	if d.stateUpdater != nil && localPath != "" {
		d.stateUpdater(localPath, state)
	}
}

// Dispatch executes all actions concurrently (up to maxConcurrent goroutines).
// It returns the first non-nil error, but waits for all goroutines to finish.
func (d *Dispatcher) Dispatch(ctx context.Context, actions []SyncAction) error {
	var (
		wg       gosync.WaitGroup
		mu       gosync.Mutex
		firstErr error
	)

	for _, action := range actions {
		a := action // capture
		wg.Add(1)

		// Acquire semaphore slot — continue (not break) on cancellation to skip
		// launching the goroutine, which would call wg.Done() a second time and
		// cause a panic: negative WaitGroup counter.
		select {
		case d.sem <- struct{}{}:
		case <-ctx.Done():
			wg.Done()
			continue
		}

		go func() {
			defer wg.Done()
			defer func() { <-d.sem }()

			if err := d.execute(ctx, a); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return firstErr
}

// execute runs a single SyncAction.
func (d *Dispatcher) execute(ctx context.Context, a SyncAction) error {
	switch a.Type {
	case ActionUpload:
		// #136 — P → U before upload, U → S/E after.
		d.updateState(a.LocalPath, types.FileStatePending)
		task := SyncTask{
			LocalPath:  a.LocalPath,
			RemotePath: a.RemotePath,
			LocalRoot:  d.localRoot,
			Direction:  DirectionUpload,
		}
		d.updateState(a.LocalPath, types.FileStateUploading)
		if err := Upload(ctx, task, d.backend, d.emitter); err != nil {
			d.updateState(a.LocalPath, types.FileStateError)
			return fmt.Errorf("dispatch: %w", err)
		}
		d.updateState(a.LocalPath, types.FileStateSynced)

	case ActionDownload:
		// #136 — P before download, S after (download direction handled as pending→synced).
		d.updateState(a.LocalPath, types.FileStatePending)
		task := SyncTask{
			LocalPath:  a.LocalPath,
			RemotePath: a.RemotePath,
			LocalRoot:  d.localRoot,
			Direction:  DirectionDownload,
		}
		if err := Download(ctx, task, d.backend, d.emitter); err != nil {
			d.updateState(a.LocalPath, types.FileStateError)
			return fmt.Errorf("dispatch: %w", err)
		}
		d.updateState(a.LocalPath, types.FileStateSynced)
		// Phase 4 — update CF badge to ✓✓ after successful download.
		if d.cfManager != nil && d.backendID != "" && a.LocalPath != "" {
			_ = d.cfManager.SetSyncState(d.backendID, a.LocalPath, CFSyncStateSynced)
		}

	case ActionDelete:
		if err := d.backend.Delete(ctx, a.RemotePath); err != nil {
			d.emitter.Emit("sync:error", map[string]any{
				"path":    a.RemotePath,
				"message": err.Error(),
			})
			return fmt.Errorf("dispatch: delete %s: %w", a.RemotePath, err)
		}
		// File removed from remote — remove from state cache too.
		d.updateState(a.LocalPath, types.FileStateUnknown)

	case ActionMkdir:
		if err := d.backend.CreateDir(ctx, a.RemotePath); err != nil {
			d.emitter.Emit("sync:error", map[string]any{
				"path":    a.RemotePath,
				"message": err.Error(),
			})
			return fmt.Errorf("dispatch: mkdir %s: %w", a.RemotePath, err)
		}

	case ActionRename:
		// #139 — try atomic server-side rename; fall back to upload+delete if unsupported.
		d.updateState(a.LocalPath, types.FileStateUploading)
		if err := d.backend.Rename(ctx, a.SrcRemotePath, a.RemotePath); err != nil {
			if errors.Is(err, plugins.ErrNotSupported) {
				// Fallback: upload new path, delete old remote path.
				uploadTask := SyncTask{
					LocalPath:  a.LocalPath,
					RemotePath: a.RemotePath,
					LocalRoot:  d.localRoot,
					Direction:  DirectionUpload,
				}
				if err2 := Upload(ctx, uploadTask, d.backend, d.emitter); err2 != nil {
					d.updateState(a.LocalPath, types.FileStateError)
					return fmt.Errorf("dispatch: rename fallback upload %s: %w", a.RemotePath, err2)
				}
				if err2 := d.backend.Delete(ctx, a.SrcRemotePath); err2 != nil {
					// Non-fatal: new file is uploaded; stale old path may remain.
					d.emitter.Emit("sync:error", map[string]any{
						"path":    a.SrcRemotePath,
						"message": "rename fallback delete: " + err2.Error(),
					})
				}
			} else {
				d.updateState(a.LocalPath, types.FileStateError)
				return fmt.Errorf("dispatch: rename %s → %s: %w", a.SrcRemotePath, a.RemotePath, err)
			}
		}
		d.updateState(a.LocalPath, types.FileStateSynced)

	case ActionCopy:
		// #140 — try server-side copy; fall back to download+upload if unsupported.
		d.updateState(a.LocalPath, types.FileStatePending)
		if err := d.backend.Copy(ctx, a.SrcRemotePath, a.RemotePath); err != nil {
			if errors.Is(err, plugins.ErrNotSupported) {
				// Fallback: download source to temp, upload to destination.
				downloadTask := SyncTask{
					LocalPath:  a.LocalPath,
					RemotePath: a.SrcRemotePath,
					LocalRoot:  d.localRoot,
					Direction:  DirectionDownload,
				}
				if err2 := Download(ctx, downloadTask, d.backend, d.emitter); err2 != nil {
					d.updateState(a.LocalPath, types.FileStateError)
					return fmt.Errorf("dispatch: copy fallback download %s: %w", a.SrcRemotePath, err2)
				}
				uploadTask := SyncTask{
					LocalPath:  a.LocalPath,
					RemotePath: a.RemotePath,
					LocalRoot:  d.localRoot,
					Direction:  DirectionUpload,
				}
				if err2 := Upload(ctx, uploadTask, d.backend, d.emitter); err2 != nil {
					d.updateState(a.LocalPath, types.FileStateError)
					return fmt.Errorf("dispatch: copy fallback upload %s: %w", a.RemotePath, err2)
				}
			} else {
				d.updateState(a.LocalPath, types.FileStateError)
				return fmt.Errorf("dispatch: copy %s → %s: %w", a.SrcRemotePath, a.RemotePath, err)
			}
		}
		d.updateState(a.LocalPath, types.FileStateSynced)

	default:
		return fmt.Errorf("dispatch: unknown action type %q", a.Type)
	}

	return nil
}
