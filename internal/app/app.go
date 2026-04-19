package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	gosync "sync"
	"time"

	"github.com/CCoupel/GhostDrive/internal/backends"
	"github.com/CCoupel/GhostDrive/internal/config"
	"github.com/CCoupel/GhostDrive/internal/sync"
	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/CCoupel/GhostDrive/plugins"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the main application struct bound to the Wails frontend.
type App struct {
	ctx     context.Context
	cfgPath string
	cfg     config.AppConfig

	mu      gosync.RWMutex
	manager *backends.BackendManager
	engines map[string]*sync.Engine
}

// NewApp creates a new App. Configuration is loaded in Startup once the
// Wails context is available. cfgPath is the path to config.json; pass ""
// to use the platform default.
func NewApp(cfgPath string) *App {
	if cfgPath == "" {
		if p, err := config.ConfigPath(); err == nil {
			cfgPath = p
		}
	}
	return &App{
		cfgPath: cfgPath,
		cfg:     config.DefaultConfig(),
		engines: make(map[string]*sync.Engine),
	}
}

// startup is called by Wails after the frontend is ready.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx

	// Load configuration
	path := a.cfgPath
	if path == "" {
		var err error
		path, err = config.ConfigPath()
		if err != nil {
			a.emitError("app: cannot determine config path: " + err.Error())
			path = "config.json"
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Printf("app: load config: %v — using defaults", err)
		a.emitError("app: load config: " + err.Error())
		cfg = config.DefaultConfig()
	}
	a.mu.Lock()
	a.cfg = cfg
	a.cfgPath = path
	a.manager = backends.NewBackendManager(a)
	a.mu.Unlock()

	// Reconnect saved backends
	for _, bc := range cfg.Backends {
		if bc.Enabled {
			if err := a.manager.Add(bc); err != nil {
				a.emitError(fmt.Sprintf("app: reconnect backend %s: %v", bc.Name, err))
			}
		}
	}

	a.emit("app:ready", map[string]any{
		"version":       cfg.Version,
		"backendsCount": len(cfg.Backends),
	})
}

// shutdown is called by Wails when the application is about to quit.
func (a *App) Shutdown(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, engine := range a.engines {
		engine.Stop()
		delete(a.engines, id)
	}
}

// ─── Config ──────────────────────────────────────────────────────────────────

// GetConfig returns the current application configuration.
func (a *App) GetConfig() config.AppConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

// SaveConfig persists the application configuration.
func (a *App) SaveConfig(cfg config.AppConfig) error {
	a.mu.Lock()
	a.cfg = cfg
	path := a.cfgPath
	a.mu.Unlock()
	return config.Save(cfg, path)
}

// GetVersion returns the application version.
func (a *App) GetVersion() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.Version
}

// GetAvailableBackendTypes returns the list of compiled-in plugin types.
// The frontend uses this to populate the "Add backend" type selector.
func (a *App) GetAvailableBackendTypes() []string {
	return backends.AvailableTypes()
}

// ─── Backends ────────────────────────────────────────────────────────────────

// AddBackend validates, saves and connects a new backend.
func (a *App) AddBackend(bc plugins.BackendConfig) (plugins.BackendConfig, error) {
	if err := validateBackendConfig(bc); err != nil {
		return bc, fmt.Errorf("validation: %w", err)
	}

	if err := a.manager.Add(bc); err != nil {
		return bc, fmt.Errorf("connection: %w", err)
	}

	// Persist
	a.mu.Lock()
	a.cfg.Backends = append(a.cfg.Backends, bc)
	path := a.cfgPath
	cfg := a.cfg
	a.mu.Unlock()

	if err := config.Save(cfg, path); err != nil {
		return bc, fmt.Errorf("save: %w", err)
	}
	return bc, nil
}

// RemoveBackend stops sync and removes the backend with the given ID.
func (a *App) RemoveBackend(backendID string) error {
	// Stop sync if running
	_ = a.StopSync(backendID)

	if err := a.manager.Remove(backendID); err != nil {
		return err
	}

	// Remove from config
	a.mu.Lock()
	backends := make([]plugins.BackendConfig, 0, len(a.cfg.Backends))
	for _, b := range a.cfg.Backends {
		if b.ID != backendID {
			backends = append(backends, b)
		}
	}
	a.cfg.Backends = backends
	path := a.cfgPath
	cfg := a.cfg
	a.mu.Unlock()

	return config.Save(cfg, path)
}

// TestBackendConnection instantiates a temporary backend and tests connectivity.
func (a *App) TestBackendConnection(bc plugins.BackendConfig) (types.BackendStatus, error) {
	status := types.BackendStatus{BackendID: bc.ID}
	b, err := backends.InstantiateBackend(bc)
	if err != nil {
		status.Error = err.Error()
		return status, err
	}
	if err := b.Connect(bc); err != nil {
		status.Error = err.Error()
		return status, err
	}
	defer b.Disconnect()

	info, err := b.Stat(context.Background(), "/")
	if err == nil && info != nil {
		status.TotalSpace = info.Size
	}
	status.Connected = true
	return status, nil
}

// GetBackendStatuses returns connection status for all configured backends.
func (a *App) GetBackendStatuses() []types.BackendStatus {
	return a.manager.ListStatuses()
}

// ListFiles lists remote files for a backend at the given path.
func (a *App) ListFiles(backendID string, path string) ([]plugins.FileInfo, error) {
	b, ok := a.manager.Get(backendID)
	if !ok {
		return nil, fmt.Errorf("not found: %s", backendID)
	}
	return b.List(context.Background(), path)
}

// DownloadFile downloads a remote file into the backend's local SyncDir.
func (a *App) DownloadFile(backendID string, remotePath string) error {
	b, ok := a.manager.Get(backendID)
	if !ok {
		return fmt.Errorf("not found: %s", backendID)
	}
	bc, _ := a.manager.GetConfig(backendID)
	localPath := filepath.Join(bc.SyncDir, filepath.Base(remotePath))
	return b.Download(context.Background(), remotePath, localPath, nil)
}

// GetCacheStats returns local cache statistics (stub — cache implemented in v1).
func (a *App) GetCacheStats() types.CacheStats {
	return types.CacheStats{}
}

// ClearCache empties the local cache (stub — cache implemented in v1).
func (a *App) ClearCache() error {
	return nil
}

// OpenSyncFolder opens the local sync directory in the system file manager.
func (a *App) OpenSyncFolder(backendID string) error {
	bc, ok := a.manager.GetConfig(backendID)
	if !ok {
		return fmt.Errorf("not found: %s", backendID)
	}
	return openFolder(bc.SyncDir)
}

// ─── Sync ─────────────────────────────────────────────────────────────────────

// GetSyncState returns the aggregated sync state across all backends.
func (a *App) GetSyncState() types.SyncState {
	a.mu.RLock()
	engines := make(map[string]*sync.Engine, len(a.engines))
	for k, v := range a.engines {
		engines[k] = v
	}
	a.mu.RUnlock()

	backendStates := make([]types.BackendSyncState, 0, len(engines))
	for id, e := range engines {
		s := e.GetState()
		name := id
		if bc, ok := a.manager.GetConfig(id); ok {
			name = bc.Name
		}
		backendStates = append(backendStates, types.BackendSyncState{
			BackendID:   id,
			BackendName: name,
			Status:      s.Status,
			Progress:    s.Progress,
			CurrentFile: s.CurrentFile,
			Pending:     s.Pending,
			LastSync:    s.LastSync,
		})
	}

	global := aggregateStatus(backendStates)
	var progress float64
	if len(backendStates) > 0 {
		var sum float64
		for _, b := range backendStates {
			sum += b.Progress
		}
		progress = sum / float64(len(backendStates))
	}

	return types.SyncState{
		Status:          global,
		Progress:        progress,
		Backends:        backendStates,
		ActiveTransfers: []types.ProgressEvent{},
	}
}

// StartSync starts the sync engine for a backend.
func (a *App) StartSync(backendID string) error {
	a.mu.Lock()
	if _, exists := a.engines[backendID]; exists {
		a.mu.Unlock()
		return fmt.Errorf("already running")
	}
	b, ok := a.manager.Get(backendID)
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("not found: %s", backendID)
	}
	bc, _ := a.manager.GetConfig(backendID)
	cfg := a.cfg
	engine := sync.NewEngine(b, bc.SyncDir, bc.RemotePath, cfg, a)
	a.engines[backendID] = engine
	a.mu.Unlock() // release before Start to avoid holding lock during I/O
	return engine.Start(a.ctx)
}

// StopSync stops the sync engine for a backend.
func (a *App) StopSync(backendID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, exists := a.engines[backendID]
	if !exists {
		return nil
	}
	e.Stop()
	delete(a.engines, backendID)
	return nil
}

// PauseSync pauses the sync engine for a backend.
func (a *App) PauseSync(backendID string) error {
	a.mu.RLock()
	e, exists := a.engines[backendID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("not found: %s", backendID)
	}
	e.Pause()
	a.emitSyncState()
	return nil
}

// ForceSync triggers an immediate full sync for a backend.
func (a *App) ForceSync(backendID string) error {
	a.mu.RLock()
	e, exists := a.engines[backendID]
	a.mu.RUnlock()
	if !exists {
		// Auto-start if not running
		return a.StartSync(backendID)
	}
	go func() {
		if err := e.ForceSync(a.ctx); err != nil {
			a.emitError(fmt.Sprintf("force sync %s: %v", backendID, err))
		}
	}()
	return nil
}

// Quit terminates the application.
func (a *App) Quit() {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx != nil {
		wailsruntime.Quit(ctx)
	}
}

// Context returns the Wails application context (valid after Startup).
func (a *App) Context() context.Context {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ctx
}

// ─── EventEmitter (implements sync.EventEmitter) ──────────────────────────────

// Emit sends a Wails event to the frontend.
func (a *App) Emit(event string, data any) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, event, data)
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func (a *App) emit(event string, data any) {
	a.Emit(event, data)
}

func (a *App) emitError(msg string) {
	a.emit("app:error", map[string]any{
		"message": msg,
		"time":    time.Now(),
	})
}

func (a *App) emitSyncState() {
	a.emit("sync:state-changed", a.GetSyncState())
}

// aggregateStatus computes the global SyncStatus from per-backend states.
func aggregateStatus(states []types.BackendSyncState) types.SyncStatus {
	if len(states) == 0 {
		return types.SyncIdle
	}
	allPaused := true
	for _, s := range states {
		switch s.Status {
		case types.SyncError:
			return types.SyncError
		case types.SyncSyncing:
			return types.SyncSyncing
		}
		if s.Status != types.SyncPaused {
			allPaused = false
		}
	}
	if allPaused {
		return types.SyncPaused
	}
	return types.SyncIdle
}

// validateBackendConfig checks required fields.
func validateBackendConfig(bc plugins.BackendConfig) error {
	if bc.Name == "" || len(bc.Name) > 64 {
		return fmt.Errorf("name requis, max 64 chars")
	}
	if bc.Type != "webdav" && bc.Type != "moosefs" {
		return fmt.Errorf("type invalide: %q", bc.Type)
	}
	if !filepath.IsAbs(bc.SyncDir) {
		return fmt.Errorf("syncDir doit être un chemin absolu")
	}
	if _, err := os.Stat(bc.SyncDir); err != nil {
		return fmt.Errorf("syncDir inaccessible: %w", err)
	}
	if len(bc.RemotePath) == 0 || bc.RemotePath[0] != '/' {
		return fmt.Errorf("remotePath doit commencer par /")
	}
	if strings.Contains(filepath.ToSlash(filepath.Clean(bc.RemotePath)), "..") {
		return fmt.Errorf("remotePath ne doit pas contenir de segments ..")
	}
	switch bc.Type {
	case "webdav":
		if bc.Params["url"] == "" {
			return fmt.Errorf("url requis pour WebDAV")
		}
		if bc.Params["username"] == "" {
			return fmt.Errorf("username requis pour WebDAV")
		}
		if bc.Params["password"] == "" {
			return fmt.Errorf("password requis pour WebDAV")
		}
	case "moosefs":
		if bc.Params["master"] == "" {
			return fmt.Errorf("master requis pour MooseFS")
		}
		if bc.Params["mountPath"] == "" {
			return fmt.Errorf("mountPath requis pour MooseFS")
		}
	}
	return nil
}

// openFolder opens a directory in the OS file manager.
func openFolder(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
