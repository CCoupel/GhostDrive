# GhostDrive Plugin SDK — Go

Build custom storage backends for GhostDrive in 5 steps.

## Prerequisites

- Go 1.22+
- GhostDrive module in your `go.work` or `go.mod`
- `make` (or run the build command manually)

## 5-Step Guide

### Step 1 — Copy the template

```sh
cp -r plugins/sdk/go/ my-plugin/
cd my-plugin/
```

### Step 2 — Implement `StorageBackend`

Create a struct that satisfies `plugins.StorageBackend` (see `echo/main.go` for a
complete example):

```go
type MyPlugin struct {
    connected bool
}

func (p *MyPlugin) Name() string    { return "myplugin" }
func (p *MyPlugin) Connect(cfg plugins.BackendConfig) error { ... }
// ... implement all interface methods
```

Update `main.go` to use your implementation:

```go
goplugin.Serve(sdk.ServeConfig(&MyPlugin{}))
```

### Step 3 — Build

```sh
# Windows (default)
make build
# → myplugin.exe  (Windows AMD64)

# Linux / macOS
make build-linux
# → myplugin  (Linux AMD64, no extension, executable bit set)

# Both platforms at once
make build-all
```

Or manually:

```sh
# Windows
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags ignore -ldflags="-s -w" -o myplugin.exe ./echo/

# Linux
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags ignore -ldflags="-s -w" -o myplugin ./echo/
```

> **Multi-platform note**: the Makefile `build-linux` target uses
> `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` — the resulting binary is a fully
> static ELF executable compatible with the GhostDrive Linux loader.

### Step 4 — Install

Copy the binary to the GhostDrive plugins directory:

```
# Windows
<AppDir>\plugins\myplugin.exe

# Linux — the loader detects extensionless executables with the execute bit
<AppDir>/plugins/myplugin
```

Where `<AppDir>` is the directory containing `GhostDrive.exe` (Windows) or
the `ghostdrive` binary (Linux).

### Step 5 — Load

Either:
- Restart GhostDrive (plugins are loaded at startup), or
- Call `ReloadPlugins()` from the Settings > Plugins page.

Your plugin will appear in the **Add Backend** type selector as `"myplugin"`.

---

## Plugin Contract

Your plugin must implement all methods of `plugins.StorageBackend`:

| Method | Description |
|--------|-------------|
| `Name() string` | Unique type identifier (lowercase, no spaces) |
| `Connect(BackendConfig) error` | Initialise with config params |
| `Disconnect() error` | Release resources |
| `IsConnected() bool` | Thread-safe connected check |
| `Upload(ctx, local, remote, progress)` | Upload a file |
| `Download(ctx, remote, local, progress)` | Download a file |
| `Delete(ctx, remote)` | Remove a remote path |
| `Move(ctx, oldPath, newPath)` | Rename/move a remote path |
| `List(ctx, path) []FileInfo` | List directory contents |
| `Stat(ctx, path) *FileInfo` | Get metadata for a path |
| `CreateDir(ctx, path)` | Create a remote directory |
| `Watch(ctx, path) <-chan FileEvent` | Stream change events |
| `GetQuota(ctx) (free, total, err)` | Return storage quota |

Return `(-1, -1, nil)` from `GetQuota` if your backend does not support quota reporting.

---

## See Also

- `echo/main.go` — complete reference implementation
- `contracts/plugin-loader-bindings.md` — Wails bindings documentation
- `plugins/plugin.go` — `StorageBackend` interface definition
