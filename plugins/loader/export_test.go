// export_test.go exposes internal symbols for white-box integration tests.
// This file is compiled only when running "go test" — it does NOT form part
// of the public API.
package loader

import goplugin "github.com/hashicorp/go-plugin"

// GetPluginClientForTest returns the underlying go-plugin Client for the named
// plugin so that integration tests can inspect process state (e.g.
// client.Exited() after Shutdown). The caller must NOT call Kill() on the
// returned client; use KillPluginProcess or Shutdown instead.
//
// Returns (nil, false) when the plugin is not found.
func (l *GRPCLoader) GetPluginClientForTest(name string) (*goplugin.Client, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	entry, ok := l.entries[name]
	if !ok {
		return nil, false
	}
	return entry.client, true
}
