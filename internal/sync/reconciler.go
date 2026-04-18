package sync

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
)

// ActionType defines the type of sync action to perform.
type ActionType string

const (
	ActionUpload   ActionType = "upload"
	ActionDownload ActionType = "download"
	ActionDelete   ActionType = "delete"
	ActionMkdir    ActionType = "mkdir"
)

// SyncAction represents a single operation the engine must execute.
type SyncAction struct {
	Type       ActionType
	LocalPath  string
	RemotePath string
}

// ConflictEntry records a conflict that was resolved by last-write-wins.
type ConflictEntry struct {
	Path       string
	LocalMod   time.Time
	RemoteMod  time.Time
	Resolution string // "local-wins" | "remote-wins"
	ResolvedAt time.Time
}

// Reconciler compares local and remote state and generates a list of SyncActions.
type Reconciler struct {
	backend  plugins.StorageBackend
	localDir string
	logPath  string
}

// NewReconciler creates a Reconciler.
// logPath is the path to sync.log for conflict journaling.
func NewReconciler(backend plugins.StorageBackend, localDir, logPath string) *Reconciler {
	return &Reconciler{
		backend:  backend,
		localDir: localDir,
		logPath:  logPath,
	}
}

// Reconcile compares the local directory with the remote path and returns
// the set of actions required to bring them in sync.
//
// Algorithm:
//  1. List remote files via backend.List
//  2. Walk local directory via os.ReadDir (recursive)
//  3. Compare entries by path, ModTime, and ETag
//  4. Resolve conflicts with last-write-wins (V1)
//  5. Return []SyncAction
func (r *Reconciler) Reconcile(ctx context.Context, remotePath string) ([]SyncAction, error) {
	// 1. List remote
	remote, err := r.listRemoteRecursive(ctx, remotePath)
	if err != nil {
		return nil, fmt.Errorf("sync: reconcile: list remote: %w", err)
	}

	// 2. Walk local
	local, err := r.listLocal()
	if err != nil {
		return nil, fmt.Errorf("sync: reconcile: list local: %w", err)
	}

	var actions []SyncAction

	// Build a map of remote files by relative path
	remoteMap := map[string]plugins.FileInfo{}
	for _, fi := range remote {
		rel := r.remoteRelPath(fi.Path, remotePath)
		remoteMap[rel] = fi
	}

	// Build a map of local files by relative path
	localMap := map[string]os.FileInfo{}
	for rel, fi := range local {
		localMap[rel] = fi
	}

	// 3. Compare

	// Files/dirs present locally
	for rel, localFI := range localMap {
		remoteFI, existsRemote := remoteMap[rel]
		localFullPath := filepath.Join(r.localDir, filepath.FromSlash(rel))
		remoteFullPath := remotePath + "/" + rel

		if localFI.IsDir() {
			if !existsRemote {
				actions = append(actions, SyncAction{
					Type:       ActionMkdir,
					LocalPath:  localFullPath,
					RemotePath: remoteFullPath,
				})
			}
			continue
		}

		if !existsRemote {
			// Local only → upload
			actions = append(actions, SyncAction{
				Type:       ActionUpload,
				LocalPath:  localFullPath,
				RemotePath: remoteFullPath,
			})
			continue
		}

		// Both exist → check if they differ
		if r.needsUpdate(localFI.ModTime(), localFI.Size(), remoteFI) {
			// Conflict: both modified — last-write-wins
			if localFI.ModTime().After(remoteFI.ModTime) {
				r.logConflict(ConflictEntry{
					Path:       rel,
					LocalMod:   localFI.ModTime(),
					RemoteMod:  remoteFI.ModTime,
					Resolution: "local-wins",
					ResolvedAt: time.Now(),
				})
				actions = append(actions, SyncAction{
					Type:       ActionUpload,
					LocalPath:  localFullPath,
					RemotePath: remoteFullPath,
				})
			} else {
				r.logConflict(ConflictEntry{
					Path:       rel,
					LocalMod:   localFI.ModTime(),
					RemoteMod:  remoteFI.ModTime,
					Resolution: "remote-wins",
					ResolvedAt: time.Now(),
				})
				actions = append(actions, SyncAction{
					Type:       ActionDownload,
					LocalPath:  localFullPath,
					RemotePath: remoteFullPath,
				})
			}
		}
		// If equal, no action needed
	}

	// Files/dirs present remotely but not locally → download
	for rel, remoteFI := range remoteMap {
		if _, existsLocal := localMap[rel]; !existsLocal {
			localFullPath := filepath.Join(r.localDir, filepath.FromSlash(rel))
			remoteFullPath := remotePath + "/" + rel

			if remoteFI.IsDir {
				// Create local directory
				actions = append(actions, SyncAction{
					Type:       ActionMkdir,
					LocalPath:  localFullPath,
					RemotePath: remoteFullPath,
				})
			} else {
				actions = append(actions, SyncAction{
					Type:       ActionDownload,
					LocalPath:  localFullPath,
					RemotePath: remoteFullPath,
				})
			}
		}
	}

	return actions, nil
}

// listRemoteRecursive returns all remote files flattened.
func (r *Reconciler) listRemoteRecursive(ctx context.Context, remotePath string) ([]plugins.FileInfo, error) {
	entries, err := r.backend.List(ctx, remotePath)
	if err != nil {
		return nil, err
	}

	var result []plugins.FileInfo
	for _, e := range entries {
		result = append(result, e)
		if e.IsDir {
			sub, err := r.listRemoteRecursive(ctx, e.Path)
			if err != nil {
				return nil, err
			}
			result = append(result, sub...)
		}
	}
	return result, nil
}

// listLocal returns a map of relative paths to os.FileInfo for the local directory.
func (r *Reconciler) listLocal() (map[string]os.FileInfo, error) {
	result := map[string]os.FileInfo{}
	err := filepath.Walk(r.localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == r.localDir {
			return nil
		}
		rel, err := filepath.Rel(r.localDir, path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(rel)] = info
		return nil
	})
	return result, err
}

// remoteRelPath returns the relative path of a remote file, given its full remote path
// and the remote base path.
func (r *Reconciler) remoteRelPath(fullPath, basePath string) string {
	base := strings.TrimRight(basePath, "/") + "/"
	rel := strings.TrimPrefix(fullPath, base)
	return strings.TrimLeft(rel, "/")
}

// needsUpdate returns true if the local and remote versions differ.
func (r *Reconciler) needsUpdate(localMod time.Time, localSize int64, remote plugins.FileInfo) bool {
	if localSize != remote.Size {
		return true
	}
	// Truncate to second precision — WebDAV timestamps are not sub-second
	if localMod.Truncate(time.Second).Equal(remote.ModTime.Truncate(time.Second)) {
		return false
	}
	return true
}

// logConflict appends a conflict entry to sync.log.
func (r *Reconciler) logConflict(entry ConflictEntry) {
	if r.logPath == "" {
		return
	}

	f, err := os.OpenFile(r.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	line := fmt.Sprintf("[%s] CONFLICT %s: local=%s remote=%s resolution=%s\n",
		entry.ResolvedAt.UTC().Format(time.RFC3339),
		entry.Path,
		entry.LocalMod.UTC().Format(time.RFC3339),
		entry.RemoteMod.UTC().Format(time.RFC3339),
		entry.Resolution,
	)
	_, _ = io.WriteString(f, line)
}
