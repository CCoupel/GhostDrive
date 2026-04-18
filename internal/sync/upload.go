package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
)

// Upload transfers a local file to the remote backend.
// Emits sync:progress (throttled to 100ms) and sync:error on failure.
func Upload(ctx context.Context, task SyncTask, backend plugins.StorageBackend, emit EventEmitter) error {
	if err := validateLocalPath(task.LocalPath, task.LocalRoot); err != nil {
		emitSyncError(emit, task.LocalPath, err.Error())
		return err
	}
	if _, err := os.Stat(task.LocalPath); err != nil {
		emitSyncError(emit, task.LocalPath, fmt.Sprintf("upload: stat: %v", err))
		return fmt.Errorf("sync: upload %s: %w", task.LocalPath, err)
	}

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
			"path":       task.RemotePath,
			"direction":  "upload",
			"bytesDone":  done,
			"bytesTotal": total,
		}
		if total > 0 {
			payload["percent"] = float64(done) / float64(total) * 100
		}
		emit.Emit("sync:progress", payload)
	}

	if err := backend.Upload(ctx, task.LocalPath, task.RemotePath, progress); err != nil {
		emitSyncError(emit, task.LocalPath, err.Error())
		return fmt.Errorf("sync: upload %s: %w", task.LocalPath, err)
	}
	return nil
}

// emitSyncError emits a sync:error event with path and message.
func emitSyncError(emit EventEmitter, path, message string) {
	emit.Emit("sync:error", map[string]any{
		"path":    path,
		"message": message,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

// validateLocalPath rejects paths that escape the allowed root via traversal.
// If localRoot is empty, only basic clean-path validation is applied.
func validateLocalPath(localPath, localRoot string) error {
	cleaned := filepath.Clean(localPath)
	if localRoot != "" {
		root := filepath.Clean(localRoot)
		if !strings.HasPrefix(cleaned+string(filepath.Separator), root+string(filepath.Separator)) {
			return fmt.Errorf("sync: path traversal detected: %s", localPath)
		}
	}
	return nil
}
