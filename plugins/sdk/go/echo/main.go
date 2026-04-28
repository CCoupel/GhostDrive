// Package main implements the GhostDrive "echo" plugin — a minimal but fully
// functional example storage backend that echoes operations back to the caller.
//
// echo is used for:
//   - Integration testing the plugin loader without real storage infrastructure.
//   - As a reference implementation for plugin authors.
//
// This file is excluded from `go build ./...` via the `ignore` build tag.
// To build the echo plugin binary, use the Makefile:
//
//	make build          # → echo.exe (Windows AMD64)
//
// Or manually:
//
//	GOOS=windows GOARCH=amd64 go build -tags ignore -o echo.exe .
//
// Install:
//
//	copy echo.exe <AppDir>\plugins\echo.exe

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

// EchoPlugin is a no-op storage backend used for testing and as an SDK example.
// All write operations log the call and return success.
// List returns a single static entry ("echo-file.txt").
type EchoPlugin struct {
	connected bool
	config    plugins.BackendConfig
}

// ── Identification ────────────────────────────────────────────────────────────

func (e *EchoPlugin) Name() string { return "echo" }

// ── Connection ────────────────────────────────────────────────────────────────

func (e *EchoPlugin) Connect(cfg plugins.BackendConfig) error {
	if cfg.Params["rootPath"] == "" {
		return fmt.Errorf("echo: connect: rootPath param is required")
	}
	e.config = cfg
	e.connected = true
	log.Printf("connected to rootPath=%q", cfg.Params["rootPath"])
	return nil
}

func (e *EchoPlugin) Disconnect() error {
	e.connected = false
	log.Printf("disconnected")
	return nil
}

func (e *EchoPlugin) IsConnected() bool { return e.connected }

// ── File operations ───────────────────────────────────────────────────────────

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

func (e *EchoPlugin) Delete(_ context.Context, remote string) error {
	if !e.connected {
		return fmt.Errorf("echo: delete: %w", plugins.ErrNotConnected)
	}
	log.Printf("delete: %q", remote)
	return nil
}

func (e *EchoPlugin) Move(_ context.Context, oldPath, newPath string) error {
	if !e.connected {
		return fmt.Errorf("echo: move: %w", plugins.ErrNotConnected)
	}
	log.Printf("move: %q → %q", oldPath, newPath)
	return nil
}

// ── Navigation ────────────────────────────────────────────────────────────────

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

func (e *EchoPlugin) CreateDir(_ context.Context, path string) error {
	if !e.connected {
		return fmt.Errorf("echo: createDir: %w", plugins.ErrNotConnected)
	}
	log.Printf("createDir: %q", path)
	return nil
}

// ── Watch ─────────────────────────────────────────────────────────────────────

// Watch returns an empty, open channel (no events) until the context is cancelled.
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

func (e *EchoPlugin) GetQuota(_ context.Context) (free, total int64, err error) {
	return -1, -1, nil
}
