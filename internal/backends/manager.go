package backends

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	gosync "sync"

	"github.com/CCoupel/GhostDrive/internal/sync"
	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/CCoupel/GhostDrive/plugins"
	_ "github.com/CCoupel/GhostDrive/plugins/local" // registers "local" backend via init()
)

// BackendManager manages the lifecycle of storage backends.
type BackendManager struct {
	mu       gosync.RWMutex
	backends map[string]plugins.StorageBackend
	configs  map[string]plugins.BackendConfig
	emitter  sync.EventEmitter
}

// NewBackendManager creates a BackendManager.
// emitter is used to notify of backend status changes; pass nil for noop.
func NewBackendManager(emitter sync.EventEmitter) *BackendManager {
	if emitter == nil {
		emitter = &sync.NoopEmitter{}
	}
	return &BackendManager{
		backends: make(map[string]plugins.StorageBackend),
		configs:  make(map[string]plugins.BackendConfig),
		emitter:  emitter,
	}
}

// Add instantiates, connects and registers a backend.
// It generates an ID if BackendConfig.ID is empty.
func (m *BackendManager) Add(bc plugins.BackendConfig) error {
	if bc.ID == "" {
		bc.ID = generateID()
	}

	b, err := InstantiateBackend(bc)
	if err != nil {
		return fmt.Errorf("backends: instantiate %s: %w", bc.Type, err)
	}

	if err := b.Connect(bc); err != nil {
		return fmt.Errorf("backends: connect %s: %w", bc.Name, err)
	}

	m.mu.Lock()
	m.backends[bc.ID] = b
	m.configs[bc.ID] = bc
	m.mu.Unlock()

	m.emitter.Emit("backend:status-changed", types.BackendStatus{
		BackendID: bc.ID,
		Connected: true,
	})
	return nil
}

// Remove disconnects and removes the backend with the given ID.
func (m *BackendManager) Remove(id string) error {
	m.mu.Lock()
	b, ok := m.backends[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("not found: %s", id)
	}
	delete(m.backends, id)
	delete(m.configs, id)
	m.mu.Unlock()

	_ = b.Disconnect()
	m.emitter.Emit("backend:status-changed", types.BackendStatus{
		BackendID: id,
		Connected: false,
	})
	return nil
}

// Get returns the StorageBackend for the given ID.
func (m *BackendManager) Get(id string) (plugins.StorageBackend, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.backends[id]
	return b, ok
}

// GetConfig returns the BackendConfig for the given ID.
func (m *BackendManager) GetConfig(id string) (plugins.BackendConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bc, ok := m.configs[id]
	return bc, ok
}

// List returns all registered backend IDs.
func (m *BackendManager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.backends))
	for id := range m.backends {
		ids = append(ids, id)
	}
	return ids
}

// ListStatuses returns connection status for all registered backends,
// including disk quota when the backend supports it.
// GetQuota is called outside the lock to avoid deadlocks with plugins
// that acquire internal mutexes during quota calls.
func (m *BackendManager) ListStatuses() []types.BackendStatus {
	type entry struct {
		id string
		b  plugins.StorageBackend
	}

	m.mu.RLock()
	entries := make([]entry, 0, len(m.backends))
	for id, b := range m.backends {
		entries = append(entries, entry{id, b})
	}
	m.mu.RUnlock()

	statuses := make([]types.BackendStatus, 0, len(entries))
	for _, e := range entries {
		s := types.BackendStatus{BackendID: e.id, Connected: e.b.IsConnected()}
		if e.b.IsConnected() {
			if free, total, err := e.b.GetQuota(context.Background()); err == nil {
				s.FreeSpace = free
				s.TotalSpace = total
			}
		}
		statuses = append(statuses, s)
	}
	return statuses
}

// AvailableTypes returns the names of all registered plugin backends, sorted
// alphabetically.  The list grows automatically as plugins register themselves
// via their init() functions.
func AvailableTypes() []string {
	return plugins.ListBackends()
}

// InstantiateBackend creates a new StorageBackend instance from a BackendConfig
// without connecting.  The type must have been registered via plugins.Register
// (each plugin package does this in its init()).
func InstantiateBackend(bc plugins.BackendConfig) (plugins.StorageBackend, error) {
	b, err := plugins.Get(bc.Type)
	if err != nil {
		return nil, fmt.Errorf("backends: unknown type %q", bc.Type)
	}
	return b, nil
}

// GenerateID creates a random 16-byte hex identifier.
// Exported so callers (e.g. app.AddBackend) can assign the ID before
// passing BackendConfig to manager.Add, ensuring the returned config
// carries the definitive ID.
func GenerateID() string { return generateID() }

// generateID creates a random 16-byte hex identifier.
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
