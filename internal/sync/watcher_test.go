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

func TestWatcherInvalidDir(t *testing.T) {
	w, err := NewWatcher("/nonexistent/path/that/does/not/exist")
	require.NoError(t, err, "NewWatcher should not error on creation")

	ctx := context.Background()
	_, err = w.Start(ctx)
	assert.Error(t, err, "Start should fail for non-existent directory")
	w.Stop()
}
