package app

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/CCoupel/GhostDrive/internal/logging"
	"github.com/CCoupel/GhostDrive/internal/placeholder"
	"github.com/CCoupel/GhostDrive/internal/sync"
	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/CCoupel/GhostDrive/plugins"
	grpcbridge "github.com/CCoupel/GhostDrive/plugins/grpc"
	"github.com/CCoupel/GhostDrive/plugins/loader"
	"github.com/CCoupel/GhostDrive/plugins/local"
	pluginsregistry "github.com/CCoupel/GhostDrive/plugins/registry"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the main application struct bound to the Wails frontend.
type App struct {
	ctx     context.Context
	cfgPath string
	cfg     config.AppConfig

	mu           gosync.RWMutex
	manager      *backends.BackendManager
	engines      map[string]*sync.Engine
	driveManager *placeholder.DriveManager          // v1.1.x per-backend drive pool
	dynRegistry  *pluginsregistry.DynamicRegistry   // v0.6.x dynamic plugin loader
	descriptors  map[string]plugins.PluginDescriptor // cached plugin descriptors by type
	localCleanup func()                              // cleanup for ServeInProcess local backend
	logStore     *logging.Store                      // in-process log capture for the Logs UI tab
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
		cfgPath:      cfgPath,
		cfg:          config.DefaultConfig(),
		engines:      make(map[string]*sync.Engine),
		driveManager: placeholder.NewDriveManager(),
		descriptors:  make(map[string]plugins.PluginDescriptor),
		logStore:     logging.NewStore(),
	}
}

// startup is called by Wails after the frontend is ready.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx

	// Guard against App instances created without NewApp() (e.g. in tests).
	a.mu.Lock()
	if a.descriptors == nil {
		a.descriptors = make(map[string]plugins.PluginDescriptor)
	}
	if a.logStore == nil {
		a.logStore = logging.NewStore()
	}
	a.mu.Unlock()

	// Redirect standard log output through the in-process log store so the
	// Logs UI tab receives all log lines in real time.
	a.logStore.SetOnNew(func(e logging.Entry) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "logs:new", e)
		}
	})
	log.SetOutput(io.MultiWriter(os.Stderr, a.logStore))

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
	cfg.Version = config.AppVersion // always reflect the compiled binary version
	a.mu.Lock()
	a.cfg = cfg
	a.cfgPath = path
	a.manager = backends.NewBackendManager(a)
	if a.driveManager == nil {
		a.driveManager = placeholder.NewDriveManager()
	}
	a.mu.Unlock()

	// #58 — Auto-create GhostDrive root directory on startup.
	// Non-blocking: log the error but do not prevent the application from starting.
	root := a.GetGhostDriveRoot()
	if err := os.MkdirAll(root, 0755); err != nil {
		log.Printf("app: create GhostDriveRoot %q: %v", root, err)
	}

	// v1.1.0 — Register the "local" backend via an in-process gRPC server backed
	// by bufconn. This replaces the previous init()-based auto-registration in
	// plugins/local/local.go and ensures the local backend goes through the same
	// gRPC transport as dynamic plugins.
	//
	// Order matters: this MUST run before dynRegistry.Start() so that "local" is
	// present in the registry when validateBackendConfig runs during reconnection.
	localImpl := local.New()
	grpcLocal, localCleanup, localErr := grpcbridge.ServeInProcess(localImpl)
	if localErr != nil {
		log.Printf("app: ServeInProcess local: %v — local backend unavailable", localErr)
	} else {
		a.localCleanup = localCleanup
		// Cache the descriptor from the first (probe) instance.
		a.mu.Lock()
		a.descriptors["local"] = grpcLocal.Describe()
		a.mu.Unlock()
		// Register the factory: each plugins.Get("local") spawns a fresh in-process
		// pair. In-process servers are cheap and die with the process (acceptable v1).
		plugins.Register("local", func() plugins.StorageBackend {
			b, _, spawnErr := grpcbridge.ServeInProcess(local.New())
			if spawnErr != nil {
				log.Printf("app: local factory spawn: %v", spawnErr)
				return nil
			}
			return b
		})
	}

	// v0.6.x — Scan <AppDir>/*.ghdp and register dynamic backends BEFORE
	// the backend reconnection loop so that dynamic types pass validateBackendConfig.
	appExe, exeErr := os.Executable()
	if exeErr != nil {
		log.Printf("app: os.Executable: %v — using os.Args[0]", exeErr)
		appExe = os.Args[0]
	}
	pluginsDir := filepath.Dir(appExe)
	a.dynRegistry = pluginsregistry.NewDynamicRegistry(pluginsDir)
	if err := a.dynRegistry.Start(); err != nil {
		log.Printf("app: plugin scan %q: %v", pluginsDir, err)
	}

	// Cache descriptors of successfully loaded dynamic plugins.
	if a.dynRegistry != nil {
		a.mu.Lock()
		for _, d := range a.dynRegistry.GetPluginDescriptors() {
			a.descriptors[d.Type] = d
		}
		a.mu.Unlock()
	}

	// v1.1.x — Migration: assign MountPoint to backends that don't have one yet.
	// This ensures backends created before v1.1.x get a letter without user action.
	{
		a.mu.Lock()
		usedLetters := make([]string, 0, len(a.cfg.Backends))
		for _, bc := range a.cfg.Backends {
			if bc.MountPoint != "" {
				usedLetters = append(usedLetters, bc.MountPoint)
			}
		}
		needsSave := false
		for i := range a.cfg.Backends {
			if a.cfg.Backends[i].MountPoint == "" {
				letter := a.driveManager.AssignAvailableLetter(usedLetters)
				if letter != "" {
					a.cfg.Backends[i].MountPoint = letter
					usedLetters = append(usedLetters, letter)
					needsSave = true
				}
			}
		}
		migratedCfg := a.cfg
		migrationPath := a.cfgPath
		a.mu.Unlock()

		if needsSave {
			if saveErr := config.Save(migratedCfg, migrationPath); saveErr != nil {
				log.Printf("app: save MountPoint migration: %v", saveErr)
			}
		}
	}

	// Reconnect saved backends, auto-start sync, and mount per-backend drives.
	a.mu.RLock()
	backendsSnapshot := make([]plugins.BackendConfig, len(a.cfg.Backends))
	copy(backendsSnapshot, a.cfg.Backends)
	a.mu.RUnlock()

	for _, bc := range backendsSnapshot {
		if !bc.Enabled {
			continue
		}
		if err := a.manager.Add(bc); err != nil {
			a.emitError(fmt.Sprintf("app: reconnect backend %s: %v", bc.Name, err))
			continue
		}

		// Mount per-backend virtual drive.
		if bc.MountPoint != "" {
			b, ok := a.manager.Get(bc.ID)
			if ok {
				mb := placeholder.MountedBackend{
					ID:      bc.ID,
					Name:    bc.Name,
					Backend: b,
					Config:  bc,
				}
				if mountErr := a.driveManager.Mount(bc.ID, bc.MountPoint, mb); mountErr != nil {
					log.Printf("app: startup mount %s: %v", bc.Name, mountErr)
					a.emit("drive:error", map[string]any{
						"backendID":   bc.ID,
						"backendName": bc.Name,
						"error":       mountErr.Error(),
					})
				} else {
					a.emit("drive:mounted", map[string]any{
						"backendID":   bc.ID,
						"backendName": bc.Name,
						"mountPoint":  bc.MountPoint,
						"mounted":     true,
					})
				}
			}
		}

		if bc.AutoSync {
			if err := a.StartSync(bc.ID); err != nil {
				a.emitError(fmt.Sprintf("app: auto-start sync %s: %v", bc.Name, err))
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
	// v1.1.x — Unmount all per-backend drives before stopping sync engines.
	if a.driveManager != nil {
		if err := a.driveManager.UnmountAll(); err != nil {
			log.Printf("app: shutdown unmount all: %v", err)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for id, engine := range a.engines {
		engine.Stop()
		delete(a.engines, id)
	}

	// v0.6.x — Stop dynamic plugin subprocesses.
	if a.dynRegistry != nil {
		if err := a.dynRegistry.Stop(); err != nil {
			log.Printf("app: shutdown dynRegistry: %v", err)
		}
	}

	// v1.1.0 — Stop the in-process local backend gRPC server.
	if a.localCleanup != nil {
		a.localCleanup()
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

// GetAvailableBackendTypes returns all available plugin types (static + dynamic).
// The frontend uses this to populate the "Add backend" type selector.
// v0.6.x: includes dynamic plugins loaded from <AppDir>/*.ghdp.
func (a *App) GetAvailableBackendTypes() []string {
	if a.dynRegistry != nil {
		infos := a.dynRegistry.ListAvailablePlugins()
		names := make([]string, 0, len(infos))
		seen := make(map[string]bool, len(infos))
		for _, info := range infos {
			if !seen[info.Name] && info.Status == "loaded" {
				names = append(names, info.Name)
				seen[info.Name] = true
			}
		}
		return names
	}
	return backends.AvailableTypes()
}

// GetLoadedPlugins returns the list of dynamically-loaded plugins with their
// current status. Does not include static (compiled-in) plugins.
//
// Wails binding: window.go.App.GetLoadedPlugins()
func (a *App) GetLoadedPlugins() []loader.PluginInfo {
	if a.dynRegistry == nil {
		return []loader.PluginInfo{}
	}
	result := a.dynRegistry.ListDynamicPlugins()
	if result == nil {
		return []loader.PluginInfo{}
	}
	return result
}

// GetPluginDescriptors returns the descriptors of all available plugins
// (static + dynamically loaded). The frontend uses this to generate the Zone 2
// (Remote) section of the backend configuration form dynamically.
//
// Returns an empty (non-nil) slice when no plugins are available.
// Wails binding: window.go.App.GetPluginDescriptors()
func (a *App) GetPluginDescriptors() []plugins.PluginDescriptor {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]plugins.PluginDescriptor, 0, len(a.descriptors))
	for _, d := range a.descriptors {
		result = append(result, d)
	}
	return result
}

// ReloadPlugins rescans <AppDir>/*.ghdp without restarting the application.
// Backends using a dynamic plugin that was reloaded must be reconnected manually.
// Emits "plugin:reloaded" with the count of newly loaded plugins on success.
//
// Wails binding: window.go.App.ReloadPlugins()
func (a *App) ReloadPlugins() error {
	if a.dynRegistry == nil {
		return fmt.Errorf("dynamic registry not initialised")
	}
	if err := a.dynRegistry.Reload(); err != nil {
		return err
	}
	count := len(a.dynRegistry.ListDynamicPlugins())
	a.emit("plugin:reloaded", map[string]any{"count": count})
	return nil
}

// ─── Backends ────────────────────────────────────────────────────────────────

// AddBackend validates and saves a new backend.
// The backend is always created disabled (bc.Enabled is forced to false regardless
// of what the frontend sends). The user enables it explicitly via SetBackendEnabled.
//
// If bc.LocalPath is empty (Auto mode), a sub-directory is created under
// GetGhostDriveRoot() using the backend Name.
// If bc.MountPoint is empty, a drive letter is auto-assigned (first available ≥ E:).
func (a *App) AddBackend(bc plugins.BackendConfig) (plugins.BackendConfig, error) {
	// v1.1.x — Force disabled at creation: the drive/connection lifecycle is
	// managed exclusively via SetBackendEnabled.
	bc.Enabled = false

	// ── Auto LocalPath ────────────────────────────────────────────────────
	if bc.LocalPath == "" {
		ghostDriveRoot := a.GetGhostDriveRoot()
		localPath := filepath.Clean(filepath.Join(ghostDriveRoot, bc.Name))
		root := filepath.Clean(ghostDriveRoot)
		// Fix B — containment check: block path traversal via Name (e.g. "..")
		if !strings.HasPrefix(localPath, root+string(os.PathSeparator)) {
			return bc, fmt.Errorf("sync-point invalide : %q s'échappe de GhostDriveRoot", bc.Name)
		}
		bc.LocalPath = localPath
	}
	// Keep SyncDir in sync with LocalPath (SyncDir is what the engine uses).
	if bc.SyncDir == "" {
		bc.SyncDir = bc.LocalPath
	}

	// ── Auto MountPoint ───────────────────────────────────────────────────
	if bc.MountPoint == "" {
		a.mu.RLock()
		usedLetters := make([]string, 0, len(a.cfg.Backends))
		for _, ex := range a.cfg.Backends {
			if ex.MountPoint != "" {
				usedLetters = append(usedLetters, ex.MountPoint)
			}
		}
		a.mu.RUnlock()
		bc.MountPoint = a.driveManager.AssignAvailableLetter(usedLetters)
		// MountPoint may remain "" on non-Windows or when all letters are taken.
	}

	// ── Validate BEFORE MkdirAll — avoid orphan directories on error ──────
	warning, err := a.validateBackendConfig(bc)
	if err != nil {
		return bc, fmt.Errorf("validation: %w", err)
	}
	bc.Warning = warning

	// Create the local sync directory only after validation passes.
	if err := os.MkdirAll(bc.SyncDir, 0755); err != nil {
		return bc, fmt.Errorf("creation dossier local: %w", err)
	}

	// Assign the definitive ID (before persisting so the caller gets it back).
	if bc.ID == "" {
		bc.ID = backends.GenerateID()
	}

	// Persist (no manager.Add since Enabled = false)
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

// SetBackendEnabled enables or disables a backend by ID.
//
// Enabling (enabled=true): Connect → Mount drive → persist → start AutoSync.
// Atomically: if Connect or Mount fails, enabled remains false and no state is persisted.
//
// Disabling (enabled=false): Unmount drive → StopSync → Disconnect → persist.
// Emits drive:mounted / drive:unmounted / drive:error accordingly.
func (a *App) SetBackendEnabled(id string, enabled bool) error {
	a.mu.Lock()
	idx := -1
	for i, bc := range a.cfg.Backends {
		if bc.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		a.mu.Unlock()
		return fmt.Errorf("not found: %s", id)
	}
	a.cfg.Backends[idx].Enabled = enabled
	bc := a.cfg.Backends[idx]
	path := a.cfgPath
	a.mu.Unlock()

	if !enabled {
		// v1.1.x — Unmount drive BEFORE stopping sync / disconnecting.
		if unmountErr := a.driveManager.Unmount(id); unmountErr != nil {
			log.Printf("app: SetBackendEnabled unmount %s: %v", bc.Name, unmountErr)
			a.emit("drive:error", map[string]any{
				"backendID":   id,
				"backendName": bc.Name,
				"error":       unmountErr.Error(),
			})
		} else {
			a.emit("drive:unmounted", map[string]any{
				"backendID":   id,
				"backendName": bc.Name,
				"mountPoint":  bc.MountPoint,
			})
		}

		// Persist first (state is definitive), then side-effects.
		a.mu.RLock()
		cfg := a.cfg
		a.mu.RUnlock()
		if err := config.Save(cfg, path); err != nil {
			return err
		}
		_ = a.StopSync(id)
		_ = a.manager.Remove(id)
	} else {
		// Enable path: connect first — only persist on success to avoid disk/memory
		// divergence if manager.Add fails.
		if err := a.manager.Add(bc); err != nil {
			// Rollback in-memory flag (disk was never written).
			a.mu.Lock()
			if idx2 := indexByID(a.cfg.Backends, id); idx2 >= 0 {
				a.cfg.Backends[idx2].Enabled = false
			}
			a.mu.Unlock()
			return fmt.Errorf("reconnect: %w", err)
		}

		// v1.1.x — Mount per-backend virtual drive.
		if bc.MountPoint != "" {
			b, ok := a.manager.Get(id)
			if ok {
				mb := placeholder.MountedBackend{
					ID:      bc.ID,
					Name:    bc.Name,
					Backend: b,
					Config:  bc,
				}
				if mountErr := a.driveManager.Mount(id, bc.MountPoint, mb); mountErr != nil {
					// Mount failed — rollback: disconnect and revert enabled flag.
					log.Printf("app: SetBackendEnabled mount %s: %v", bc.Name, mountErr)
					_ = a.manager.Remove(id)
					a.mu.Lock()
					if idx2 := indexByID(a.cfg.Backends, id); idx2 >= 0 {
						a.cfg.Backends[idx2].Enabled = false
					}
					a.mu.Unlock()
					a.emit("drive:error", map[string]any{
						"backendID":   id,
						"backendName": bc.Name,
						"error":       mountErr.Error(),
					})
					return fmt.Errorf("mount drive: %w", mountErr)
				}
				// Emit mounted event with current drive status.
				if s, ok := a.driveManager.GetStatus(id); ok {
					a.emit("drive:mounted", map[string]any{
						"backendID":    id,
						"backendName":  bc.Name,
						"mountPoint":   s.MountPoint,
						"backendPaths": s.BackendPaths,
						"mounted":      s.Mounted,
					})
				}
			}
		}

		a.mu.RLock()
		cfg := a.cfg
		a.mu.RUnlock()
		if err := config.Save(cfg, path); err != nil {
			return err
		}
		if bc.AutoSync {
			_ = a.StartSync(id)
		}
	}

	b, _ := a.manager.Get(id)
	connected := b != nil && b.IsConnected()
	a.emit("backend:status-changed", types.BackendStatus{BackendID: id, Connected: connected})
	return nil
}

// SetAutoSync enables or disables automatic sync for a backend.
// autoSync=false stops any running engine; autoSync=true starts the engine
// immediately if the backend is connected.
func (a *App) SetAutoSync(id string, autoSync bool) error {
	a.mu.Lock()
	idx := -1
	for i, bc := range a.cfg.Backends {
		if bc.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		a.mu.Unlock()
		return fmt.Errorf("not found: %s", id)
	}
	a.cfg.Backends[idx].AutoSync = autoSync
	cfg := a.cfg
	path := a.cfgPath
	a.mu.Unlock()

	if err := config.Save(cfg, path); err != nil {
		return err
	}

	if !autoSync {
		_ = a.StopSync(id)
	} else {
		if b, ok := a.manager.Get(id); ok && b.IsConnected() {
			_ = a.StartSync(id)
		}
	}

	a.emitSyncState()
	return nil
}

// indexByID returns the index of the BackendConfig with the given ID, or -1.
func indexByID(bcs []plugins.BackendConfig, id string) int {
	for i, bc := range bcs {
		if bc.ID == id {
			return i
		}
	}
	return -1
}

// RemoveBackend stops sync, unmounts the virtual drive, and removes the backend
// with the given ID from memory and persisted config.
func (a *App) RemoveBackend(backendID string) error {
	// Stop sync if running
	_ = a.StopSync(backendID)

	// v1.1.x — Unmount per-backend drive before disconnecting.
	if unmountErr := a.driveManager.Unmount(backendID); unmountErr != nil {
		log.Printf("app: RemoveBackend unmount %s: %v", backendID, unmountErr)
	}

	if err := a.manager.Remove(backendID); err != nil {
		// Ignore "not found" — the backend may not be in memory (never connected,
		// or after a restart) but must still be removed from the persisted config.
		if !strings.Contains(err.Error(), "not found") {
			return err
		}
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

// UpdateBackend replaces the configuration of an existing backend identified by
// newBC.ID. It stops the sync engine, disconnects the old connection, validates
// the new config, reconnects if the backend is enabled, and persists the result.
//
// validateBackendConfig already skips the current backend when checking name/
// path uniqueness (via the ex.ID == bc.ID guard), so keeping the same name does
// not trigger a "duplicate name" error.
func (a *App) UpdateBackend(newBC plugins.BackendConfig) (plugins.BackendConfig, error) {
	// ── Find existing backend ─────────────────────────────────────────────
	a.mu.RLock()
	idx := -1
	var oldBC plugins.BackendConfig
	for i, bc := range a.cfg.Backends {
		if bc.ID == newBC.ID {
			idx = i
			oldBC = bc
			break
		}
	}
	a.mu.RUnlock()

	if idx == -1 {
		return newBC, fmt.Errorf("backend not found: %s", newBC.ID)
	}

	// ── Stop sync engine if running ───────────────────────────────────────
	_ = a.StopSync(newBC.ID)

	// ── Disconnect old connection ─────────────────────────────────────────
	if err := a.manager.Remove(newBC.ID); err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return newBC, fmt.Errorf("disconnect: %w", err)
		}
	}

	// ── Carry over immutable fields ───────────────────────────────────────
	newBC.ID = oldBC.ID
	if newBC.LocalPath == "" {
		newBC.LocalPath = oldBC.LocalPath
	}
	if newBC.SyncDir == "" {
		newBC.SyncDir = newBC.LocalPath
	}

	// ── Validate ──────────────────────────────────────────────────────────
	warning, err := a.validateBackendConfig(newBC)
	if err != nil {
		return newBC, fmt.Errorf("validation: %w", err)
	}
	newBC.Warning = warning

	// ── Reconnect if enabled ──────────────────────────────────────────────
	if newBC.Enabled {
		if err := a.manager.Add(newBC); err != nil {
			return newBC, fmt.Errorf("connection: %w", err)
		}

		// v1.1.x — Handle MountPoint change: if the mount point changed (or drive
		// was not previously mounted), remount on the new mount point.
		oldMountPoint := oldBC.MountPoint
		if newBC.MountPoint != "" && newBC.MountPoint != oldMountPoint {
			// Unmount old drive (if any) — best-effort, already handled above via
			// manager.Remove + Stop, but guard in case DriveManager still has it.
			_ = a.driveManager.Unmount(newBC.ID)
		}
		if newBC.MountPoint != "" {
			b, ok := a.manager.Get(newBC.ID)
			if ok {
				mb := placeholder.MountedBackend{
					ID:      newBC.ID,
					Name:    newBC.Name,
					Backend: b,
					Config:  newBC,
				}
				if mountErr := a.driveManager.Mount(newBC.ID, newBC.MountPoint, mb); mountErr != nil {
					log.Printf("app: UpdateBackend: mount %q: %v", newBC.Name, mountErr)
					a.emit("drive:error", map[string]any{
						"backendID":   newBC.ID,
						"backendName": newBC.Name,
						"error":       mountErr.Error(),
					})
				} else {
					if s, ok := a.driveManager.GetStatus(newBC.ID); ok {
						a.emit("drive:mounted", map[string]any{
							"backendID":    newBC.ID,
							"backendName":  newBC.Name,
							"mountPoint":   s.MountPoint,
							"backendPaths": s.BackendPaths,
							"mounted":      s.Mounted,
						})
					}
				}
			}
		}

		// MAJEUR-2 — restart AutoSync after reconnection
		if newBC.AutoSync {
			if syncErr := a.StartSync(newBC.ID); syncErr != nil {
				log.Printf("app: UpdateBackend: auto-start sync %q: %v", newBC.ID, syncErr)
			}
		}
	}

	// ── Persist ───────────────────────────────────────────────────────────
	a.mu.Lock()
	// MAJEUR-1 — re-find index by ID under the write lock to avoid stale index
	// (idx was captured under a prior RLock; a concurrent RemoveBackend could
	// have shifted or removed the entry in the meantime).
	finalIdx := -1
	for i, bc := range a.cfg.Backends {
		if bc.ID == newBC.ID {
			finalIdx = i
			break
		}
	}
	if finalIdx == -1 {
		a.mu.Unlock()
		return newBC, fmt.Errorf("backend removed concurrently: %s", newBC.ID)
	}
	a.cfg.Backends[finalIdx] = newBC
	path := a.cfgPath
	cfg := a.cfg
	a.mu.Unlock()

	if err := config.Save(cfg, path); err != nil {
		return newBC, fmt.Errorf("save: %w", err)
	}
	return newBC, nil
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

	if free, total, err := b.GetQuota(context.Background()); err == nil {
		status.FreeSpace = free
		status.TotalSpace = total
	} else {
		// #89 — GetQuota failed or unsupported: use -1 to signal "quota unknown".
		status.FreeSpace = -1
		status.TotalSpace = -1
	}
	status.Connected = true
	return status, nil
}

// ─── Logs ─────────────────────────────────────────────────────────────────────

// GetLogs returns all captured log entries with ID > sinceID.
// Pass sinceID=0 to retrieve all stored entries (up to 2000).
func (a *App) GetLogs(sinceID int64) []logging.Entry {
	return a.logStore.GetEntries(sinceID)
}

// ClearLogs removes all stored log entries.
func (a *App) ClearLogs() {
	a.logStore.Clear()
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

// DownloadFile downloads a remote file into the backend's local SyncDir,
// preserving the remote directory structure. Emits sync:progress events
// with percent, bytesDone, bytesTotal, backendId and remotePath so the
// frontend can display inline download progress.
func (a *App) DownloadFile(backendID string, remotePath string) error {
	b, ok := a.manager.Get(backendID)
	if !ok {
		return fmt.Errorf("not found: %s", backendID)
	}
	bc, _ := a.manager.GetConfig(backendID)

	// Preserve directory structure: strip leading separators to avoid
	// filepath.Join treating remotePath as absolute.
	relPath := filepath.Clean(strings.TrimLeft(remotePath, "/\\"))
	localPath := filepath.Join(bc.SyncDir, relPath)

	// Containment check — same pattern as AddBackend:
	// block path traversal via crafted remotePath (e.g. "../../Windows/evil.dll").
	syncDir := filepath.Clean(bc.SyncDir)
	if !strings.HasPrefix(localPath, syncDir+string(os.PathSeparator)) {
		return fmt.Errorf("path traversal detected in remotePath: %s", remotePath)
	}

	// Create intermediate directories before downloading.
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("create local dirs: %w", err)
	}

	progress := func(done, total int64) {
		var percent float64
		if total > 0 {
			percent = float64(done) / float64(total) * 100
		}
		a.emit("sync:progress", map[string]any{
			"path":       remotePath,
			"direction":  "download",
			"bytesDone":  done,
			"bytesTotal": total,
			"percent":    percent,
			"backendId":  backendID,
			"remotePath": remotePath,
		})
	}

	return b.Download(context.Background(), remotePath, localPath, progress)
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

// SelectDirectory ouvre un dialog natif de sélection de dossier.
// Retourne le chemin sélectionné, ou "" si l'utilisateur annule ou en cas d'erreur.
func (a *App) SelectDirectory() string {
	if a.ctx == nil {
		return ""
	}
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Sélectionner le répertoire de synchronisation",
	})
	if err != nil {
		return ""
	}
	return dir
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
			Errors:      []types.BackendSyncError{}, // never nil → never "null" in JSON
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
		Errors:          []types.SyncErrorInfo{},    // never nil → never "null" in JSON
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

// ResumeSync resumes a paused sync engine for a backend.
func (a *App) ResumeSync(backendID string) error {
	a.mu.RLock()
	e, exists := a.engines[backendID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("not found: %s", backendID)
	}
	e.Resume()
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

// ─── Drive Virtuel (WinFsp / per-backend) ─────────────────────────────────────

// GetAvailableDriveLetters returns the list of unused Windows drive letters in
// "X:" format (e.g. ["D:", "G:", "H:"]). On non-Windows platforms returns nil.
func (a *App) GetAvailableDriveLetters() []string {
	return placeholder.AvailableDriveLetters()
}

// GetMountPoint returns the global mount point configuration value.
// Retained for backward-compatibility; in v1.1.x each backend has its own
// MountPoint stored in BackendConfig.MountPoint.
func (a *App) GetMountPoint() string {
	a.mu.RLock()
	mp := a.cfg.MountPoint
	a.mu.RUnlock()
	if mp != "" {
		return mp
	}
	if runtime.GOOS == "windows" {
		return `C:\GhostDrive\GhD\`
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "GhostDrive", "GhD")
	}
	return filepath.Join(home, "GhostDrive", "GhD")
}

// GetDriveStatuses returns the current mount status of every per-backend virtual
// drive keyed by backendID. Delegates to DriveManager.GetAllStatuses().
//
// Wails binding: window.go.App.GetDriveStatuses()
func (a *App) GetDriveStatuses() map[string]placeholder.DriveStatus {
	return a.driveManager.GetAllStatuses()
}

// GetDriveStatus returns an empty DriveStatus.
//
// Deprecated: use GetDriveStatuses() which returns per-backend drive states.
// Retained in v1.1.x for frontend compatibility during migration.
//
// Wails binding: window.go.App.GetDriveStatus()
func (a *App) GetDriveStatus() placeholder.DriveStatus {
	log.Printf("app: GetDriveStatus() is deprecated — use GetDriveStatuses()")
	return placeholder.DriveStatus{}
}

// GetGhostDriveRoot returns the configurable root directory under which
// GhostDrive creates per-backend sync folders in Auto mode.
// Default: C:\GhostDrive\ on Windows, ~/GhostDrive on other platforms.
// Persistent preference configuration is out of scope for v0.4.0.
func (a *App) GetGhostDriveRoot() string {
	a.mu.RLock()
	root := a.cfg.GhostDriveRoot
	a.mu.RUnlock()
	if root != "" {
		return root
	}
	// Platform-specific default.
	if runtime.GOOS == "windows" {
		return `C:\GhostDrive`
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "GhostDrive")
	}
	return filepath.Join(home, "GhostDrive")
}

// windowsInvalidNameChars are the characters forbidden in Windows folder names.
const windowsInvalidNameChars = `\/:*?"<>|`

// validateBackendConfig checks required fields and cross-backend uniqueness
// rules.  It returns a (possibly empty) non-blocking warning message alongside
// any blocking error.
//
// Blocking rules:
//  1. Name: non-empty, ≤64 chars, no Windows-invalid chars, unique
//     case-insensitively among existing backends.
//  2. LocalPath: unique among existing backends (no two backends may share
//     the same local sync folder).
//
// Non-blocking rule:
//  3. rootPath (local plugin): if another backend already uses the same
//     remote source, a warning is returned but no error.
func (a *App) validateBackendConfig(bc plugins.BackendConfig) (warning string, err error) {
	// ── Name ─────────────────────────────────────────────────────────────
	if bc.Name == "" || len(bc.Name) > 64 {
		return "", fmt.Errorf("name requis, max 64 chars")
	}
	// Fix A — block "." and ".." explicitly (not caught by windowsInvalidNameChars
	// but would escape GhostDriveRoot in auto-mode path construction).
	if bc.Name == "." || bc.Name == ".." {
		return "", fmt.Errorf("nom invalide : %q", bc.Name)
	}
	if strings.ContainsAny(bc.Name, windowsInvalidNameChars) {
		return "", fmt.Errorf("nom invalide (caractères interdits : %s)", windowsInvalidNameChars)
	}

	// ── Type ─────────────────────────────────────────────────────────────
	if !plugins.Has(bc.Type) {
		return "", fmt.Errorf("type invalide: %q", bc.Type)
	}

	// ── SyncDir ──────────────────────────────────────────────────────────
	if !filepath.IsAbs(bc.SyncDir) {
		return "", fmt.Errorf("syncDir doit être un chemin absolu")
	}
	// Tolerate ErrNotExist: in auto-mode the directory does not yet exist when
	// validateBackendConfig runs; MkdirAll creates it immediately after.
	if _, err := os.Stat(bc.SyncDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("syncDir inaccessible: %w", err)
	}

	// ── RemotePath ───────────────────────────────────────────────────────
	if len(bc.RemotePath) == 0 || bc.RemotePath[0] != '/' {
		return "", fmt.Errorf("remotePath doit commencer par /")
	}
	if strings.Contains(filepath.ToSlash(filepath.Clean(bc.RemotePath)), "..") {
		return "", fmt.Errorf("remotePath ne doit pas contenir de segments ..")
	}

	// ── Plugin-specific params ────────────────────────────────────────────
	switch bc.Type {
	case "local":
		if bc.Params["rootPath"] == "" {
			return "", fmt.Errorf("rootPath requis pour Local")
		}
	}

	// ── Cross-backend uniqueness checks ───────────────────────────────────
	a.mu.RLock()
	existing := make([]plugins.BackendConfig, len(a.cfg.Backends))
	copy(existing, a.cfg.Backends)
	a.mu.RUnlock()

	nameLower := strings.ToLower(bc.Name)
	localPathClean := filepath.Clean(bc.LocalPath)

	mountPointUpper := strings.ToUpper(strings.TrimSpace(bc.MountPoint))

	for _, ex := range existing {
		// Skip self when editing (same ID).
		if ex.ID != "" && ex.ID == bc.ID {
			continue
		}

		// Rule 1 — name uniqueness (case-insensitive, blocking).
		if strings.ToLower(ex.Name) == nameLower {
			return "", fmt.Errorf("un backend avec ce nom existe déjà : %q", ex.Name)
		}

		// Rule 2 — LocalPath uniqueness (blocking).
		if bc.LocalPath != "" && filepath.Clean(ex.LocalPath) == localPathClean {
			return "", fmt.Errorf("ce dossier local est déjà utilisé par le backend %q", ex.Name)
		}

		// Rule 4 — MountPoint uniqueness (blocking, activated or not).
		// A disabled backend still reserves its mount point so it can be
		// re-enabled without conflicts.
		if bc.MountPoint != "" &&
			strings.ToUpper(strings.TrimSpace(ex.MountPoint)) == mountPointUpper {
			return "", fmt.Errorf("ce point de montage est déjà utilisé par le backend %q", ex.Name)
		}

		// Rule 3 — rootPath duplicate (non-blocking warning).
		// Minor 2: use filepath.Clean before comparison (normalises trailing slashes).
		// Minor 3: break after first match so the warning is not overwritten.
		if bc.Type == "local" && ex.Type == "local" &&
			bc.Params["rootPath"] != "" &&
			filepath.Clean(ex.Params["rootPath"]) == filepath.Clean(bc.Params["rootPath"]) {
			warning = fmt.Sprintf("ce dossier source est déjà utilisé par le backend %q", ex.Name)
			break
		}
	}

	return warning, nil
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
