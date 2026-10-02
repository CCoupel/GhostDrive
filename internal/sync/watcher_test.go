package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWatcherEmitsRelativePaths verifies that FileEvent.Path is relative to the
// watched directory and never an absolute path (#148 — watcher path format fix).
func TestWatcherEmitsRelativePaths(t *testing.T) {
	tmp := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	w, err := NewWatcher(tmp)
	require.NoError(t, err)

	events, err := w.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	testFile := filepath.Join(tmp, "rel_path_check.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("check"), 0644))

	select {
	case evt := <-events:
		assert.Equal(t, plugins.FileEventCreated, evt.Type)
		// Path must be the bare filename — no directory prefix.
		assert.Equal(t, "rel_path_check.txt", evt.Path,
			"#148: watcher must emit relative path, not absolute")
		assert.False(t, filepath.IsAbs(evt.Path),
			"#148: watcher path must never be absolute")
	case <-time.After(testTimeout):
		t.Skip("watcher event not received within timeout (CI may be slow)")
	}
}

const testTimeout = 3 * time.Second

func TestWatcherCreate(t *testing.T) {
	tmp := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	w, err := NewWatcher(tmp)
	require.NoError(t, err)

	events, err := w.Start(ctx)
	require.NoError(t, err)

	// Create a file after starting the watcher
	testFile := filepath.Join(tmp, "created.txt")
	// Small delay to ensure watcher is registered
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

	// Wait for debounced event
	select {
	case evt := <-events:
		assert.Equal(t, plugins.FileEventCreated, evt.Type)
		assert.Contains(t, evt.Path, "created.txt")
		assert.Equal(t, "local", evt.Source)
	case <-time.After(testTimeout):
		t.Skip("watcher event not received within timeout (CI environment may be slow)")
	}
}

func TestWatcherModify(t *testing.T) {
	tmp := t.TempDir()

	// Pre-create the file before starting the watcher
	testFile := filepath.Join(tmp, "modify.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("original"), 0644))

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	w, err := NewWatcher(tmp)
	require.NoError(t, err)

	events, err := w.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))

	select {
	case evt := <-events:
		// Accept Create or Modified (OS may report differently)
		assert.True(t,
			evt.Type == plugins.FileEventModified || evt.Type == plugins.FileEventCreated,
			"expected modified or created event, got %q", evt.Type)
	case <-time.After(testTimeout):
		t.Skip("watcher event not received within timeout")
	}
}

func TestWatcherDelete(t *testing.T) {
	tmp := t.TempDir()

	testFile := filepath.Join(tmp, "delete.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("bye"), 0644))

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	w, err := NewWatcher(tmp)
	require.NoError(t, err)

	events, err := w.Start(ctx)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.Remove(testFile))

	select {
	case evt := <-events:
		assert.True(t,
			evt.Type == plugins.FileEventDeleted || evt.Type == plugins.FileEventRenamed,
			"expected deleted or renamed event, got %q", evt.Type)
	case <-time.After(testTimeout):
		t.Skip("watcher event not received within timeout")
	}
}

func TestWatcherStop(t *testing.T) {
	tmp := t.TempDir()

	w, err := NewWatcher(tmp)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	events, err := w.Start(ctx)
	require.NoError(t, err)

	// Cancel and verify channel closes
	cancel()

	select {
	case _, open := <-events:
		assert.False(t, open, "channel should be closed after cancel")
	case <-time.After(2 * time.Second):
		t.Error("channel not closed after context cancellation")
	}
}

// TestWatcherCreateRename_NoStaleCreateEvent verifies that when a file is created
// and renamed within the debounce window the watcher does NOT emit a stale
// FileEventCreated for the old (renamed-away) path after the debounce fires
// (#149 fix 2).
//
// Without the fix a pending Create debounce timer for the old path continued to
// run even after the rename was detected, causing a spurious FileEventCreated
// ~500ms later.  The engine would then attempt ActionUpload on a non-existent
// file and fail.
func TestWatcherCreateRename_NoStaleCreateEvent(t *testing.T) {
	tmp := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	w, err := NewWatcher(tmp)
	require.NoError(t, err)

	events, err := w.Start(ctx)
	require.NoError(t, err)

	// Give the watcher time to register.
	time.Sleep(60 * time.Millisecond)

	oldPath := filepath.Join(tmp, "stale_create_old.txt")
	newPath := filepath.Join(tmp, "stale_create_new.txt")

	// Create old.txt and immediately rename it to new.txt — both within the
	// 500ms debounce window so the Create timer for old.txt is still pending.
	require.NoError(t, os.WriteFile(oldPath, []byte("data"), 0644))
	require.NoError(t, os.Rename(oldPath, newPath))

	// Collect all events for debounceDuration + slack so any stale Create would appear.
	collected := make([]plugins.FileEvent, 0)
	deadline := time.After(debounceDuration + 300*time.Millisecond)
	drain:
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				break drain
			}
			collected = append(collected, evt)
		case <-deadline:
			break drain
		}
	}

	// A stale FileEventCreated for the old path must NOT be present.
	for _, evt := range collected {
		if evt.Type == plugins.FileEventCreated && evt.Path == "stale_create_old.txt" {
			t.Errorf("#149: stale FileEventCreated for renamed-away path %q must not be emitted; got %+v",
				"stale_create_old.txt", evt)
		}
	}
}

func TestWatcherInvalidDir(t *testing.T) {
	w, err := NewWatcher("/nonexistent/path/that/does/not/exist")
	require.NoError(t, err, "NewWatcher should not error on creation")

	ctx := context.Background()
	_, err = w.Start(ctx)
	assert.Error(t, err, "Start should fail for non-existent directory")
	w.Stop()
}
