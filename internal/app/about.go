package app

import (
	"runtime/debug"

	"github.com/CCoupel/GhostDrive/plugins/loader"
)

// GitCommit and AppVersion are injected at build time via -ldflags:
//
//	-X 'github.com/CCoupel/GhostDrive/internal/app.GitCommit=<sha7>'
//	-X 'github.com/CCoupel/GhostDrive/internal/app.AppVersion=<semver>'
//
// When not injected (local dev / plain go build) they default to "unknown".
var (
	GitCommit  = "unknown"
	AppVersion = "unknown"
)

// BuildInfo contains version and build metadata for the GhostDrive engine.
// Exposed via the GetBuildInfo Wails binding.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`    // 7 chars from VCS, e.g. "fdcb04a"
	GoVersion string `json:"goVersion"` // e.g. "go1.21.0"
	BuildTime string `json:"buildTime"` // RFC3339 or "unknown"
}

// PluginBuildInfo contains build metadata for a dynamically-loaded plugin.
// Exposed via the GetLoadedPlugins Wails binding.
type PluginBuildInfo struct {
	Name    string `json:"name"`    // plugin type, e.g. "moosefs"
	Version string `json:"version"` // semver parsed from filename, or "unknown"
	Path    string `json:"path"`    // absolute path to the .ghdp binary
}

// GetBuildInfo returns version and VCS metadata for the running GhostDrive binary.
//
// Priority:
//  1. AppVersion / GitCommit injected via -ldflags at build time (wails build / go build).
//  2. vcs.revision from runtime/debug.ReadBuildInfo() as fallback for GitCommit.
//  3. a.cfg.Version (from config.json) as fallback for Version.
//
// Wails binding: window.go.App.GetBuildInfo()
func (a *App) GetBuildInfo() BuildInfo {
	// Resolve version: ldflags override > config.json value.
	version := AppVersion
	if version == "unknown" || version == "" {
		a.mu.RLock()
		version = a.cfg.Version
		a.mu.RUnlock()
	}

	info := BuildInfo{
		Version:   version,
		Commit:    GitCommit,
		GoVersion: "unknown",
		BuildTime: "unknown",
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		info.GoVersion = bi.GoVersion
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				// Fallback: use vcs.revision only when GitCommit was not injected.
				if GitCommit == "unknown" && len(s.Value) >= 7 {
					info.Commit = s.Value[:7]
				} else if GitCommit == "unknown" && s.Value != "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if s.Value != "" {
					info.BuildTime = s.Value
				}
			}
		}
	}
	return info
}

// GetLogCount returns the number of log entries currently held in the in-process
// log store. The frontend uses this to verify that logs are being captured even
// when the live streaming via logs:new events is not working.
//
// Wails binding: window.go.App.GetLogCount()
func (a *App) GetLogCount() int {
	return len(a.logStore.GetEntries(0))
}

// pluginInfoToPluginBuildInfo maps a loader.PluginInfo to a PluginBuildInfo.
func pluginInfoToPluginBuildInfo(p loader.PluginInfo) PluginBuildInfo {
	return PluginBuildInfo{
		Name:    p.Name,
		Version: p.Version,
		Path:    p.Path,
	}
}
