package sync

import (
	"context"
	"fmt"
	gosync "sync"

	"github.com/CCoupel/GhostDrive/plugins"
)

const maxConcurrent = 4

// CFSyncStateSynced mirrors cfapi.SyncStateSynced (= 2).
// Using a named constant avoids magic literals and decouples sync/ from the cfapi/ package.
const CFSyncStateSynced = 2

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
	backend    plugins.StorageBackend
	emitter    EventEmitter
	localRoot  string
	sem        chan struct{}
	backendID  string         // for CF state updates; empty means no-op
	cfManager  CFStateManager // optional; nil → CF state not updated
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
		task := SyncTask{
			LocalPath:  a.LocalPath,
			RemotePath: a.RemotePath,
			LocalRoot:  d.localRoot,
			Direction:  DirectionUpload,
		}
		if err := Upload(ctx, task, d.backend, d.emitter); err != nil {
			return fmt.Errorf("dispatch: %w", err)
		}

	case ActionDownload:
		task := SyncTask{
			LocalPath:  a.LocalPath,
			RemotePath: a.RemotePath,
			LocalRoot:  d.localRoot,
			Direction:  DirectionDownload,
		}
		if err := Download(ctx, task, d.backend, d.emitter); err != nil {
			return fmt.Errorf("dispatch: %w", err)
		}
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

	case ActionMkdir:
		if err := d.backend.CreateDir(ctx, a.RemotePath); err != nil {
			d.emitter.Emit("sync:error", map[string]any{
				"path":    a.RemotePath,
				"message": err.Error(),
			})
			return fmt.Errorf("dispatch: mkdir %s: %w", a.RemotePath, err)
		}

	default:
		return fmt.Errorf("dispatch: unknown action type %q", a.Type)
	}

	return nil
}
