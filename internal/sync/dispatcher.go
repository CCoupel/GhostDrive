package sync

import (
	"context"
	"fmt"
	gosync "sync"

	"github.com/CCoupel/GhostDrive/plugins"
)

const maxConcurrent = 4

// EventEmitter is the interface the dispatcher uses to emit Wails events.
// It is injectable to allow testing without Wails runtime.
type EventEmitter interface {
	Emit(event string, data any)
}

// NoopEmitter is an EventEmitter that discards all events (used in tests).
type NoopEmitter struct{}

func (n *NoopEmitter) Emit(_ string, _ any) {}

// Dispatcher executes SyncActions with bounded concurrency.
type Dispatcher struct {
	backend   plugins.StorageBackend
	emitter   EventEmitter
	localRoot string
	sem       chan struct{}
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

		// Acquire semaphore slot
		select {
		case d.sem <- struct{}{}:
		case <-ctx.Done():
			wg.Done()
			break
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
