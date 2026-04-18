package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/fsnotify/fsnotify"
)

const debounceDuration = 500 * time.Millisecond

// Watcher monitors a local directory and emits FileEvents on changes.
// Events are debounced to avoid bursts during large file operations.
type Watcher struct {
	dir     string
	watcher *fsnotify.Watcher
}

// NewWatcher creates a Watcher for the given directory.
func NewWatcher(dir string) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("sync: watcher: create fsnotify: %w", err)
	}
	return &Watcher{dir: dir, watcher: w}, nil
}

// Start begins watching the directory and returns a channel of FileEvents.
// The channel is closed when ctx is cancelled. The caller must drain the channel.
func (w *Watcher) Start(ctx context.Context) (<-chan plugins.FileEvent, error) {
	if err := w.watcher.Add(w.dir); err != nil {
		return nil, fmt.Errorf("sync: watcher: watch %s: %w", w.dir, err)
	}

	ch := make(chan plugins.FileEvent, 64)

	go func() {
		defer close(ch)
		defer w.watcher.Close()

		// pending holds debounce timers keyed by path.
		// pendingTypes preserves the first event type (create+write → created).
		pending := map[string]*time.Timer{}
		pendingTypes := map[string]plugins.FileEventType{}

		emit := func(evt plugins.FileEvent) {
			select {
			case ch <- evt:
			case <-ctx.Done():
			}
		}

		for {
			select {
			case <-ctx.Done():
				for _, t := range pending {
					t.Stop()
				}
				return

			case err, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
				_ = err

			case fsEvent, ok := <-w.watcher.Events:
				if !ok {
					return
				}

				path := filepath.ToSlash(fsEvent.Name)
				evtType := fsEventType(fsEvent.Op)
				if evtType == "" {
					continue
				}

				if t, exists := pending[path]; exists {
					t.Stop()
					delete(pending, path)
					// First-event-wins: preserve original type (e.g. created before write)
					evtType = pendingTypes[path]
				} else {
					pendingTypes[path] = evtType
				}

				capturedPath := path
				capturedType := evtType
				capturedCtx := ctx

				pending[capturedPath] = time.AfterFunc(debounceDuration, func() {
					select {
					case <-capturedCtx.Done():
						return
					default:
					}
					emit(plugins.FileEvent{
						Type:      capturedType,
						Path:      capturedPath,
						Timestamp: time.Now(),
						Source:    "local",
					})
				})
			}
		}
	}()

	return ch, nil
}

// Stop shuts down the underlying fsnotify watcher.
func (w *Watcher) Stop() error {
	return w.watcher.Close()
}

// fsEventType converts fsnotify operations to FileEventType.
func fsEventType(op fsnotify.Op) plugins.FileEventType {
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
