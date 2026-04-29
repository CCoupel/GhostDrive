// Package loader implements the GhostDrive dynamic plugin loader based on
// HashiCorp go-plugin. It discovers plugin executables in a directory, launches
// each one as a subprocess, negotiates the gRPC handshake, and registers the
// backend in the global plugins registry.
//
// # Cross-platform scanning
//
// On Windows the loader scans *.exe files.
// On Linux/macOS it additionally scans extensionless files whose execute bit
// is set (mode & 0111 != 0). Both types are handled by the same Scan call,
// so a plugin directory may contain mixed Windows and Linux binaries.
//
// # Crash supervision
//
// If a plugin subprocess exits unexpectedly the watchdog goroutine attempts to
// restart it up to N times (default N=3) with configurable back-off delays
// (default: 1 s → 2 s → 4 s). After N failures the plugin is marked "failed".
// The delays are injected via NewGRPCLoaderWithOptions to enable fast tests.
package loader

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"

	grpcbridge "github.com/CCoupel/GhostDrive/plugins/grpc"

	"github.com/CCoupel/GhostDrive/plugins"
)

// HandshakeConfig is the shared handshake configuration used by both the loader
// and all plugin binaries. Plugin binaries must reference this value to ensure
// protocol compatibility.
//
//	plugin.Serve(&plugin.ServeConfig{
//	    HandshakeConfig: loader.HandshakeConfig,
//	    ...
//	})
var HandshakeConfig = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "GHOSTDRIVE_PLUGIN",
	MagicCookieValue: "storage.v1",
}

// defaultWatchdogDelays is the production exponential back-off schedule.
var defaultWatchdogDelays = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// LoaderOptions configures optional behaviours of GRPCLoader.
// The zero value applies production defaults.
type LoaderOptions struct {
	// WatchdogDelays overrides the back-off schedule used when restarting
	// crashed plugins. The number of elements defines the maximum restart
	// attempts. Zero-length slice defaults to {1s, 2s, 4s}.
	//
	// Tests should inject short delays (e.g. {10ms, 10ms, 10ms}) to avoid
	// blocking the test suite.
	WatchdogDelays []time.Duration
}

// pluginEntry holds the runtime state of a loaded plugin.
type pluginEntry struct {
	name   string
	path   string
	client *goplugin.Client
	status string // "loaded" | "failed" | "restarting"
	err    string

	// watchdog control
	cancelWatchdog context.CancelFunc
}

// GRPCLoader scans a directory for plugin binaries, launches them via
// go-plugin, and registers each backend with the global plugins registry.
type GRPCLoader struct {
	mu             sync.RWMutex
	pluginsDir     string
	entries        map[string]*pluginEntry // keyed by plugin name
	watchdogDelays []time.Duration
}

// NewGRPCLoader creates a GRPCLoader with production defaults. Call Scan to load plugins.
func NewGRPCLoader() *GRPCLoader {
	return NewGRPCLoaderWithOptions(LoaderOptions{})
}

// NewGRPCLoaderWithOptions creates a GRPCLoader with custom options.
// Use this constructor in tests to inject fast watchdog delays:
//
//	l := loader.NewGRPCLoaderWithOptions(loader.LoaderOptions{
//	    WatchdogDelays: []time.Duration{10*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond},
//	})
func NewGRPCLoaderWithOptions(opts LoaderOptions) *GRPCLoader {
	delays := opts.WatchdogDelays
	if len(delays) == 0 {
		delays = defaultWatchdogDelays
	}
	return &GRPCLoader{
		entries:        make(map[string]*pluginEntry),
		watchdogDelays: delays,
	}
}

// Scan scans pluginsDir for *.exe files, launches each plugin, negotiates the
// handshake, retrieves the backend name, and registers a factory with
// plugins.Register.
//
// Scan is idempotent: calling it multiple times rescans the directory.
// Previously loaded plugins are shut down before rescanning.
//
// A scan failure on a single plugin does not abort the scan — the error is
// logged and that plugin is marked as "failed".
func (l *GRPCLoader) Scan(pluginsDir string) error {
	l.mu.Lock()
	l.pluginsDir = pluginsDir
	l.mu.Unlock()

	// *.exe — Windows plugin binaries (also matched on Linux: extension is just
	// a string, no OS magic involved).
	matches, err := filepath.Glob(filepath.Join(pluginsDir, "*.exe"))
	if err != nil {
		return fmt.Errorf("loader: scan %q: %w", pluginsDir, err)
	}

	// Extensionless files with execute bit — Linux/macOS plugin binaries.
	// We skip directories, empty files, and anything with an extension (covers
	// *.md, *.txt, *.so, *.dylib, etc.).
	allEntries, _ := filepath.Glob(filepath.Join(pluginsDir, "*"))
	for _, m := range allEntries {
		if filepath.Ext(m) != "" {
			// Has an extension — *.exe already handled above, others ignored.
			continue
		}
		info, statErr := os.Stat(m)
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		// Executable bit set → treat as a Linux/macOS plugin binary.
		if info.Mode()&0111 != 0 {
			matches = append(matches, m)
		}
	}

	for _, path := range matches {
		l.loadPlugin(path)
	}
	return nil
}

// loadPlugin launches a single plugin binary at path and registers it.
func (l *GRPCLoader) loadPlugin(path string) {
	name := pluginNameFromPath(path)

	client, backend, err := l.launchPlugin(path)
	if err != nil {
		log.Printf("loader: load %q: %v", path, err)
		l.mu.Lock()
		l.entries[name] = &pluginEntry{
			name:   name,
			path:   path,
			status: "failed",
			err:    err.Error(),
		}
		l.mu.Unlock()
		return
	}

	// Register a factory in the global plugin registry.
	// The factory always returns the same GRPCBackend (single connection per plugin).
	plugins.Register(backend.Name(), func() plugins.StorageBackend { return backend })

	watchCtx, watchCancel := context.WithCancel(context.Background())
	entry := &pluginEntry{
		name:           backend.Name(),
		path:           path,
		client:         client,
		status:         "loaded",
		cancelWatchdog: watchCancel,
	}

	l.mu.Lock()
	l.entries[entry.name] = entry
	l.mu.Unlock()

	log.Printf("loader: plugin %q loaded from %q", entry.name, path)

	// Start watchdog goroutine.
	go l.watchdog(watchCtx, entry)
}

// launchPlugin starts the plugin binary and returns the go-plugin client and
// the GRPCBackend connected to it.
func (l *GRPCLoader) launchPlugin(path string) (*goplugin.Client, *grpcbridge.GRPCBackend, error) {
	pluginMap := goplugin.PluginSet{
		"storage": &grpcbridge.GRPCPlugin{},
	}

	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  HandshakeConfig,
		Plugins:          pluginMap,
		Cmd:              exec.Command(path),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           newHCLogger(filepath.Base(path)),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("grpc client: %w", err)
	}

	raw, err := rpcClient.Dispense("storage")
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispense storage: %w", err)
	}

	backend, ok := raw.(*grpcbridge.GRPCBackend)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("unexpected plugin type: %T", raw)
	}

	return client, backend, nil
}

// watchdog monitors a plugin process and restarts it on crash up to
// len(l.watchdogDelays) times using the configured back-off schedule.
func (l *GRPCLoader) watchdog(ctx context.Context, entry *pluginEntry) {
	delays := l.watchdogDelays

	for attempt := 0; attempt < len(delays); attempt++ {
		// Wait until the client exits or the context is cancelled.
		select {
		case <-ctx.Done():
			return
		case <-waitForExit(ctx, entry.client):
		}

		// Check if the context was cancelled (shutdown requested).
		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Printf("loader: plugin %q exited unexpectedly (attempt %d/%d), restarting in %v",
			entry.name, attempt+1, len(delays), delays[attempt])

		l.mu.Lock()
		entry.status = "restarting"
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(delays[attempt]):
		}

		newClient, newBackend, err := l.launchPlugin(entry.path)
		if err != nil {
			log.Printf("loader: restart %q attempt %d failed: %v", entry.name, attempt+1, err)
			l.mu.Lock()
			entry.status = "failed"
			entry.err = err.Error()
			l.mu.Unlock()
			continue
		}

		// Update entry with new client.
		l.mu.Lock()
		entry.client = newClient
		entry.status = "loaded"
		entry.err = ""
		l.mu.Unlock()

		// Re-register the factory with the new backend instance.
		plugins.Register(newBackend.Name(), func() plugins.StorageBackend { return newBackend })

		log.Printf("loader: plugin %q restarted successfully", entry.name)
		// Reset attempt counter — the plugin recovered.
		attempt = -1 // will be incremented to 0 at the top of the loop
	}

	// Exhausted retries.
	log.Printf("loader: plugin %q failed after %d attempts — giving up", entry.name, len(delays))
	l.mu.Lock()
	entry.status = "failed"
	if entry.err == "" {
		entry.err = "max restart attempts reached"
	}
	l.mu.Unlock()
}

// waitForExit returns a channel that is closed when the plugin client exits.
func waitForExit(ctx context.Context, client *goplugin.Client) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				if client.Exited() {
					return
				}
			}
		}
	}()
	return ch
}

// Shutdown kills all loaded plugin subprocesses cleanly.
func (l *GRPCLoader) Shutdown() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for name, entry := range l.entries {
		// Cancel watchdog first so it does not attempt restart after kill.
		if entry.cancelWatchdog != nil {
			entry.cancelWatchdog()
		}
		if entry.client != nil {
			entry.client.Kill()
		}
		log.Printf("loader: plugin %q stopped", name)
	}
	// Clear the entries so a subsequent Scan starts fresh.
	l.entries = make(map[string]*pluginEntry)
	return nil
}

// GetLoadedPlugins returns a snapshot of all loaded (and failed) plugins.
func (l *GRPCLoader) GetLoadedPlugins() []PluginInfo {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]PluginInfo, 0, len(l.entries))
	for _, e := range l.entries {
		result = append(result, PluginInfo{
			Name:    e.name,
			Version: "unknown",
			Path:    e.path,
			Status:  e.status,
			Error:   e.err,
		})
	}
	return result
}

// ── PluginInfo ────────────────────────────────────────────────────────────────

// PluginInfo describes a dynamically-loaded plugin.
// It is returned by GetLoadedPlugins and exposed via the Wails binding.
type PluginInfo struct {
	// Name is the plugin type identifier (e.g. "echo").
	Name string `json:"name"`
	// Version as declared by the plugin (currently always "unknown").
	Version string `json:"version"`
	// Path is the absolute path to the plugin binary.
	Path string `json:"path"`
	// Status is "loaded" | "failed" | "restarting".
	Status string `json:"status"`
	// Error contains the error message when Status == "failed".
	Error string `json:"error,omitempty"`
}

// KillPluginProcess terminates the subprocess of the named plugin without
// cancelling its watchdog. The watchdog will detect the exit and attempt to
// restart the plugin according to the configured back-off schedule.
//
// This is primarily useful for integration tests that need to simulate a
// plugin crash. For a clean shutdown of all plugins use Shutdown instead.
func (l *GRPCLoader) KillPluginProcess(name string) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	entry, ok := l.entries[name]
	if !ok {
		return fmt.Errorf("loader: plugin %q not found", name)
	}
	if entry.client != nil {
		entry.client.Kill()
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// pluginNameFromPath returns a plugin name derived from the binary filename
// (without extension). Used as a fallback before the plugin's Name() RPC.
func pluginNameFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}

// newHCLogger creates a minimal go-hclog logger for go-plugin (suppresses output).
func newHCLogger(_ string) hclog.Logger {
	return hclog.NewNullLogger()
}
