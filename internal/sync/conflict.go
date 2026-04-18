package sync

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
)

// ResolveConflict applies last-write-wins between local and remote versions.
// Returns "local" or "remote". On equal ModTime, "remote" wins by convention.
// Emits sync:conflict-resolved and appends an entry to logPath (if non-empty).
// If logging fails, emits sync:error instead of silently swallowing the error.
func ResolveConflict(local, remote plugins.FileInfo, emit EventEmitter, logPath string) string {
	winner := "remote"
	if local.ModTime.After(remote.ModTime) {
		winner = "local"
	}

	emit.Emit("sync:conflict-resolved", map[string]any{
		"path":          local.Path,
		"winner":        winner,
		"localModTime":  local.ModTime.UTC().Format(time.RFC3339),
		"remoteModTime": remote.ModTime.UTC().Format(time.RFC3339),
		"time":          time.Now().UTC().Format(time.RFC3339),
	})

	if logPath != "" {
		if err := logConflict(logPath, local, remote, winner); err != nil {
			emitSyncError(emit, local.Path, fmt.Sprintf("conflict log: %v", err))
		}
	}

	return winner
}

// logConflict appends a resolved-conflict line to the sync log.
func logConflict(logPath string, local, remote plugins.FileInfo, winner string) error {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", logPath, err)
	}
	defer f.Close()

	line := fmt.Sprintf("[%s] CONFLICT %s: local=%s remote=%s winner=%s\n",
		time.Now().UTC().Format(time.RFC3339),
		local.Path,
		local.ModTime.UTC().Format(time.RFC3339),
		remote.ModTime.UTC().Format(time.RFC3339),
		winner,
	)
	if _, err := io.WriteString(f, line); err != nil {
		return fmt.Errorf("write %s: %w", logPath, err)
	}
	return nil
}
