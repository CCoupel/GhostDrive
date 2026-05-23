package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/fsnotify/fsnotify"
)

const debounceDuration = 500 * time.Millisecond

// renamePairTimeout is the window within which a fsnotify.Rename + fsnotify.Create
// pair must arrive to be detected as a rename. If no Create arrives within this
// window after a Rename, the Rename is treated as a file deletion.
// 150ms < debounceDuration so the pair is resolved before the debounce fires.
const renamePairTimeout = 150 * time.Millisecond

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

		// pendingRenames holds the old path of a Rename event keyed by directory,
		// waiting for a Create event in the same directory to complete the pair (#139).
		// Key: parent directory (slash-separated), Value: old path.
		pendingRenames := map[string]string{}
		// renameTimers holds the timeout timer for each pending rename dir.
		renameTimers := map[string]*time.Timer{}

		emit := func(evt plugins.FileEvent) {
			select {
			case ch <- evt:
			case <-ctx.Done():
			}
		}

		// dirOf returns the parent directory of a slash-separated path.
		dirOf := func(p string) string {
			if i := strings.LastIndex(p, "/"); i >= 0 {
				return p[:i]
			}
			return "."
		}

		for {
			select {
			case <-ctx.Done():
				for _, t := range pending {
					t.Stop()
				}
				for _, t := range renameTimers {
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

				// ── Rename-pair detection (#139) ────────────────────────────
				if evtType == plugins.FileEventRenamed {
					// fsnotify.Rename = the old path was renamed away.
					// Start a timer; if a Create arrives in the same dir within
					// renamePairTimeout, we emit FileEventRenamed. Otherwise emit Deleted.
					dir := dirOf(path)
					if t, exists := renameTimers[dir]; exists {
						t.Stop()
					}
					pendingRenames[dir] = path
					capturedDir := dir
					capturedOld := path
					capturedCtx := ctx
					renameTimers[dir] = time.AfterFunc(renamePairTimeout, func() {
						select {
						case <-capturedCtx.Done():
							return
						default:
						}
						// No Create arrived → treat as deletion.
						emit(plugins.FileEvent{
							Type:      plugins.FileEventDeleted,
							Path:      capturedOld,
							Timestamp: time.Now(),
							Source:    "local",
						})
						delete(pendingRenames, capturedDir)
					})
					continue
				}

				if evtType == plugins.FileEventCreated {
					// Check if a rename old-path is pending for this directory.
					dir := dirOf(path)
					if oldPath, ok := pendingRenames[dir]; ok {
						// Pair detected → emit FileEventRenamed.
						if t := renameTimers[dir]; t != nil {
							t.Stop()
						}
						delete(pendingRenames, dir)
						delete(renameTimers, dir)
						emit(plugins.FileEvent{
							Type:      plugins.FileEventRenamed,
							Path:      path,
							OldPath:   oldPath,
							Timestamp: time.Now(),
							Source:    "local",
						})
						continue
					}
					// No pending rename for this dir → fall through as normal Create.
				}

				// ── Standard debounce ────────────────────────────────────────
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
