// Package main implements the GhostDrive "echo" plugin — a minimal but fully
// functional example storage backend that echoes operations back to the caller.
//
// # Purpose
//
// echo serves two purposes:
//   - Reference implementation for plugin authors: it shows how to wire the
//     go-plugin + gRPC transport without hiding any boilerplate.
//   - Manual smoke-test for the GhostDrive plugin loader without requiring
//     real remote storage infrastructure.
//
// # Behaviour
//
// All write operations (Upload, Download, Delete, Move, CreateDir) are no-ops
// that log the call and return nil (success).
// List returns a single static entry ("echo-file.txt").
// GetQuota returns (-1, -1, nil) — quota is not supported.
//
// # Build
//
// Use the Makefile in the parent directory:
//
//	make build          # → echo.exe  (Windows AMD64)
//	make build-linux    # → echo      (Linux AMD64)
//	make build-all      # → both
//
// Or manually:
//
//	# Windows
//	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags ignore -ldflags="-s -w" -o echo.exe .
//	# Linux
//	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags ignore -ldflags="-s -w" -o echo .
//
// # Install
//
//	# Windows
//	copy echo.exe <AppDir>\plugins\echo.exe
//	# Linux
//	cp echo <AppDir>/plugins/echo

//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	sdk "github.com/CCoupel/GhostDrive/plugins/sdk/go"
	"github.com/CCoupel/GhostDrive/plugins"

	goplugin "github.com/hashicorp/go-plugin"
)

func main() {
	log.SetPrefix("[echo] ")
	goplugin.Serve(sdk.ServeConfig(&EchoPlugin{}))
}

// EchoPlugin is a no-op storage backend used for testing and as the canonical
// SDK reference implementation. All write operations log the call and return
// success without touching the filesystem. List returns a single static entry.
//
// Error pattern used throughout — plugin authors should follow the same
// conventions:
//
//	if !e.connected {
//	    return fmt.Errorf("echo: <op>: %w", plugins.ErrNotConnected)
//	}
type EchoPlugin struct {
	connected bool
	config    plugins.BackendConfig
}

// ── Identification ────────────────────────────────────────────────────────────

// Name returns the plugin type identifier. Must be lowercase, no spaces.
// The value is used as the BackendConfig.Type in the application config.
func (e *EchoPlugin) Name() string { return "echo" }

// Describe implements plugins.StorageBackend. Returns the static descriptor
// used by the UI to generate the Zone 2 configuration form.
// Callable before Connect; performs no I/O.
func (e *EchoPlugin) Describe() plugins.PluginDescriptor {
	return plugins.PluginDescriptor{
		Type:        "echo",
		DisplayName: "Echo (test)",
		Description: "Plugin de référence — toutes les opérations sont des no-ops.",
		Params: []plugins.ParamSpec{
			{Key: "delay", Label: "Délai simulé (ms)", Type: plugins.ParamTypeNumber, Required: false, Default: "0"},
			{Key: "rootPath", Label: "Chemin racine (requis)", Type: plugins.ParamTypePath, Required: true, Placeholder: "/tmp/echo"},
		},
	}
}

// ── Connection ────────────────────────────────────────────────────────────────

// Connect validates the required "rootPath" param and marks the plugin as
// connected. Plugins should validate all required params here and return a
// descriptive error if the backend is unreachable or misconfigured.
func (e *EchoPlugin) Connect(cfg plugins.BackendConfig) error {
	if cfg.Params["rootPath"] == "" {
		return fmt.Errorf("echo: connect: rootPath param is required")
	}
	e.config = cfg
	e.connected = true
	log.Printf("connected to rootPath=%q", cfg.Params["rootPath"])
	return nil
}

// Disconnect releases resources and marks the plugin as disconnected.
// After Disconnect, all operations except Connect must return ErrNotConnected.
func (e *EchoPlugin) Disconnect() error {
	e.connected = false
	log.Printf("disconnected")
	return nil
}

// IsConnected returns the current connection state. Must be thread-safe and
// must not perform I/O (called frequently by the sync engine).
func (e *EchoPlugin) IsConnected() bool { return e.connected }

// ── File operations ───────────────────────────────────────────────────────────

// Upload logs the transfer and invokes the progress callback with (1, 1) to
// indicate immediate completion. Real plugins should stream the file and
// report accurate byte counts.
func (e *EchoPlugin) Upload(_ context.Context, local, remote string, progress plugins.ProgressCallback) error {
	if !e.connected {
		return fmt.Errorf("echo: upload: %w", plugins.ErrNotConnected)
	}
	log.Printf("upload: %q → %q", local, remote)
	if progress != nil {
		progress(1, 1)
	}
	return nil
}

// Download logs the transfer and invokes the progress callback with (1, 1).
// Real plugins should write the remote file content to local and report progress.
func (e *EchoPlugin) Download(_ context.Context, remote, local string, progress plugins.ProgressCallback) error {
	if !e.connected {
		return fmt.Errorf("echo: download: %w", plugins.ErrNotConnected)
	}
	log.Printf("download: %q → %q", remote, local)
	if progress != nil {
		progress(1, 1)
	}
	return nil
}

// Delete logs the operation and returns nil (success). Real plugins should
// remove the entry at remote and return plugins.ErrFileNotFound when missing.
func (e *EchoPlugin) Delete(_ context.Context, remote string) error {
	if !e.connected {
		return fmt.Errorf("echo: delete: %w", plugins.ErrNotConnected)
	}
	log.Printf("delete: %q", remote)
	return nil
}

// Move logs the operation and returns nil (success). Real plugins should
// rename/move oldPath to newPath atomically when possible.
func (e *EchoPlugin) Move(_ context.Context, oldPath, newPath string) error {
	if !e.connected {
		return fmt.Errorf("echo: move: %w", plugins.ErrNotConnected)
	}
	log.Printf("move: %q → %q", oldPath, newPath)
	return nil
}

// ── Navigation ────────────────────────────────────────────────────────────────

// List returns a single static entry ("echo-file.txt") regardless of path.
// Real plugins should return the actual children of the directory at path.
func (e *EchoPlugin) List(_ context.Context, path string) ([]plugins.FileInfo, error) {
	if !e.connected {
		return nil, fmt.Errorf("echo: list: %w", plugins.ErrNotConnected)
	}
	log.Printf("list: %q", path)
	return []plugins.FileInfo{
		{
			Name:    "echo-file.txt",
			Path:    path + "/echo-file.txt",
			Size:    42,
			IsDir:   false,
			ModTime: time.Now(),
		},
	}, nil
}

// Stat returns a static FileInfo for any path. Real plugins should return
// plugins.ErrFileNotFound (wrapped) when path does not exist.
func (e *EchoPlugin) Stat(_ context.Context, path string) (*plugins.FileInfo, error) {
	if !e.connected {
		return nil, fmt.Errorf("echo: stat: %w", plugins.ErrNotConnected)
	}
	log.Printf("stat: %q", path)
	fi := &plugins.FileInfo{
		Name:    "echo-file.txt",
		Path:    path,
		Size:    42,
		IsDir:   false,
		ModTime: time.Now(),
	}
	return fi, nil
}

// CreateDir logs the operation and returns nil (success).
func (e *EchoPlugin) CreateDir(_ context.Context, path string) error {
	if !e.connected {
		return fmt.Errorf("echo: createDir: %w", plugins.ErrNotConnected)
	}
	log.Printf("createDir: %q", path)
	return nil
}

// ── Watch ─────────────────────────────────────────────────────────────────────

// Watch returns an open channel that emits no events until the context is
// cancelled, then closes the channel. Real plugins should emit FileEvents for
// each change detected on path (using inotify, FSEvents, or polling).
// The channel buffer (64) absorbs burst events from the sync engine.
func (e *EchoPlugin) Watch(ctx context.Context, path string) (<-chan plugins.FileEvent, error) {
	if !e.connected {
		return nil, fmt.Errorf("echo: watch: %w", plugins.ErrNotConnected)
	}
	log.Printf("watch: %q (no events)", path)
	ch := make(chan plugins.FileEvent, 64)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// ── Quota ─────────────────────────────────────────────────────────────────────

// GetQuota returns (-1, -1, nil) to indicate that quota reporting is not
// supported. Real plugins should return (free, total, nil) in bytes, or
// (-1, -1, nil) if the backend does not expose quota information.
func (e *EchoPlugin) GetQuota(_ context.Context) (free, total int64, err error) {
	return -1, -1, nil
}
