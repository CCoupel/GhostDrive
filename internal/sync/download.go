package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	gosync "sync"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
)

// Download retrieves a remote file and writes it atomically to the local path.
// Parent directories are created as needed.
// Emits sync:progress (throttled to 100ms) and sync:error on failure.
func Download(ctx context.Context, task SyncTask, backend plugins.StorageBackend, emit EventEmitter) error {
	if err := validateLocalPath(task.LocalPath, task.LocalRoot); err != nil {
		emitSyncError(emit, task.LocalPath, err.Error())
		return err
	}
	if err := os.MkdirAll(filepath.Dir(task.LocalPath), 0755); err != nil {
		emitSyncError(emit, task.LocalPath, fmt.Sprintf("download: mkdirall: %v", err))
		return fmt.Errorf("sync: download %s: mkdir: %w", task.LocalPath, err)
	}

	// Atomic write: download to a sibling temp file, then rename.
	tmpPath := task.LocalPath + ".ghostdrive.tmp"

	var (
		mu       gosync.Mutex
		lastEmit time.Time
	)

	progress := func(done, total int64) {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(lastEmit) < 100*time.Millisecond {
			return
		}
		lastEmit = time.Now()

		payload := map[string]any{
			"path":       task.LocalPath,
			"direction":  "download",
			"bytesDone":  done,
			"bytesTotal": total,
		}
		if total > 0 {
			payload["percent"] = float64(done) / float64(total) * 100
		}
		emit.Emit("sync:progress", payload)
	}

	// Use a temporary task pointing to the tmp path so the backend writes there.
	tmpTask := SyncTask{
		LocalPath:  tmpPath,
		RemotePath: task.RemotePath,
	}

	if err := backend.Download(ctx, tmpTask.RemotePath, tmpTask.LocalPath, progress); err != nil {
		_ = os.Remove(tmpPath)
		emitSyncError(emit, task.LocalPath, err.Error())
		return fmt.Errorf("sync: download %s: %w", task.RemotePath, err)
	}

	if err := os.Rename(tmpPath, task.LocalPath); err != nil {
		_ = os.Remove(tmpPath)
		emitSyncError(emit, task.LocalPath, fmt.Sprintf("download: rename: %v", err))
		return fmt.Errorf("sync: download %s: rename: %w", task.LocalPath, err)
	}

	return nil
}
