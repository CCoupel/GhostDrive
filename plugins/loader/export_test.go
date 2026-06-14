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

// GetFactoryClientsForTest returns a snapshot of all goplugin.Client instances
// that have been spawned via factory closures (one per plugins.Get() call).
// Used by integration tests to verify that Shutdown kills every factory client
// (non-regression for issue #86 where factory clients were discarded with "_").
func (l *GRPCLoader) GetFactoryClientsForTest() []*goplugin.Client {
	l.factoryMu.Lock()
	defer l.factoryMu.Unlock()
	result := make([]*goplugin.Client, len(l.factoryClients))
	copy(result, l.factoryClients)
	return result
}
