// Package cfapi wraps the Windows Cloud Filter API (CF API) for Files On-Demand.
// This file contains shared types that must be available on all platforms
// (no //go:build constraint) so that provider.go (windows) and provider_stub.go
// (!windows) can both reference them without duplication.
package cfapi

import (
	"context"
	"time"
)

// SyncState describes the CF synchronisation state of a file or folder.
type SyncState int

const (
	SyncStateCloudOnly SyncState = iota // ☁️ placeholder, not hydrated
	SyncStateSyncing                    // ⟳ hydration in progress
	SyncStateSynced                     // ✓✓ hydrated / in-sync
	SyncStatePinned                     // ⚡ always local
	SyncStateUnpinned                   // return to ☁️ from pinned
)

// PlaceholderInfo describes a file to create as a CF placeholder.
type PlaceholderInfo struct {
	RelativePath string
	FileSize     int64
	ModTime      time.Time
	FileID       string // ETag or opaque version (FileInfo.Version)
	IsDirectory  bool
}

// FetchRequest is passed to the OnFetchData callback.
type FetchRequest struct {
	LocalPath string
	Offset    int64
	Length    int64
	opInfo    uintptr // unexported — CF_CALLBACK_INFO* (opaque handle used by ghd_cf_execute_transfer)
}

// CFCallbacks are the handler functions supplied to CfConnectSyncRoot.
type CFCallbacks struct {
	OnFetchData         func(ctx context.Context, req FetchRequest) error
	OnCancelFetch       func(req FetchRequest)
	OnFetchPlaceholders func(ctx context.Context, localPath string) error
	OnDeleteCompletion  func(localPath string)
	OnRenameCompletion  func(oldPath, newPath string)
}
