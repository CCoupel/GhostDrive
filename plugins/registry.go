package plugins

import (
	"fmt"
	"sort"
	"sync"
)

// ─── Plugin registry ─────────────────────────────────────────────────────────

var (
	registryMu sync.RWMutex
	registry   = map[string]func() StorageBackend{}
)

// Register registers a factory function for a backend type name.
// It is called from the init() of each plugin package so that the backend
// is available as soon as the package is imported.
//
// If name is already registered, the previous factory is silently replaced
// (last registration wins).  This behaviour allows test code to substitute
// implementations.
func Register(name string, factory func() StorageBackend) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// Get returns a new instance of the named backend by calling its registered
// factory.  Returns an error when name has not been registered.
func Get(name string) (StorageBackend, error) {
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("plugins: unknown backend type %q", name)
	}
	return factory(), nil
}

// ListBackends returns the names of all registered backends, sorted
// alphabetically.  The result is a fresh slice on every call.
func ListBackends() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
