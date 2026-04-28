// Package registry provides the DynamicRegistry which complements the static
// plugin registry (plugins/registry.go) with dynamically-loaded plugins
// discovered from <AppDir>/plugins/*.exe at startup.
//
// Coexistence with the static registry:
//   - Static plugins ("local", "webdav", "moosefs") are registered via init()
//     in their respective packages; the DynamicRegistry does not touch them.
//   - Dynamic plugins call plugins.Register() using the same global map, so
//     they appear transparently in plugins.ListBackends() and plugins.Get().
//   - plugins/registry.go is NOT modified.
package registry

import (
	"fmt"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/CCoupel/GhostDrive/plugins/loader"
)

// DynamicRegistry manages the lifecycle of dynamically-loaded plugins.
// It wraps a GRPCLoader and exposes Start/Stop/Reload operations used by the
// application startup sequence.
type DynamicRegistry struct {
	loader    *loader.GRPCLoader
	pluginDir string
}

// NewDynamicRegistry creates a DynamicRegistry that will scan pluginsDir on
// Start.  The scan is deferred to Start so that the constructor is cheap and
// does no I/O.
func NewDynamicRegistry(pluginsDir string) *DynamicRegistry {
	return &DynamicRegistry{
		loader:    loader.NewGRPCLoader(),
		pluginDir: pluginsDir,
	}
}

// Start scans pluginDir for plugin binaries and registers them.
// It is called by app.Startup before the backend reconnection loop so that
// dynamic plugins are available for validateBackendConfig checks.
//
// A partial scan failure (one plugin failing to load) does not return an error;
// the failure is recorded in the loader entry and visible via ListAvailablePlugins.
func (r *DynamicRegistry) Start() error {
	if err := r.loader.Scan(r.pluginDir); err != nil {
		return fmt.Errorf("dynamic registry: scan: %w", err)
	}
	return nil
}

// Stop shuts down all loaded plugin subprocesses.
// It is called by app.Shutdown.
func (r *DynamicRegistry) Stop() error {
	if err := r.loader.Shutdown(); err != nil {
		return fmt.Errorf("dynamic registry: shutdown: %w", err)
	}
	return nil
}

// Reload rescans the plugin directory without restarting the application.
// Running backends using a dynamic plugin are NOT automatically reconnected;
// the caller must handle that.
func (r *DynamicRegistry) Reload() error {
	if err := r.Stop(); err != nil {
		return fmt.Errorf("dynamic registry: reload stop: %w", err)
	}
	return r.Start()
}

// ListAvailablePlugins returns information about all plugins: static ones
// (compiled-in) plus dynamic ones discovered by the loader.
//
// Static plugins are returned first (sorted alphabetically by plugins.ListBackends),
// followed by dynamic plugins.  The Status field for static plugins is always
// "loaded" since they are part of the binary.
func (r *DynamicRegistry) ListAvailablePlugins() []loader.PluginInfo {
	// Static plugins — always present.
	staticNames := plugins.ListBackends()
	result := make([]loader.PluginInfo, 0, len(staticNames)+8)
	for _, name := range staticNames {
		result = append(result, loader.PluginInfo{
			Name:    name,
			Version: "static",
			Path:    "",
			Status:  "loaded",
		})
	}

	// Dynamic plugins — from the loader.
	dynamic := r.loader.GetLoadedPlugins()
	for _, d := range dynamic {
		result = append(result, d)
	}
	return result
}

// ListDynamicPlugins returns only the dynamically-loaded plugins (not the
// static ones compiled into the binary).  This is what GetLoadedPlugins Wails
// binding exposes to the frontend.
func (r *DynamicRegistry) ListDynamicPlugins() []loader.PluginInfo {
	return r.loader.GetLoadedPlugins()
}
