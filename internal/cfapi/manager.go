// Package cfapi provides the Cloud Filter API integration for GhostDrive
// Files On-Demand. On Windows it registers sync roots, connects CF callbacks,
// creates placeholders, and drives file hydration. On other platforms all
// operations are no-ops (see provider_stub.go).
package cfapi

import (
	"context"
	"fmt"
	"log"
	gosync "sync"

	"github.com/CCoupel/GhostDrive/internal/cache"
	"github.com/CCoupel/GhostDrive/internal/config"
	"github.com/CCoupel/GhostDrive/plugins"
)

// EventEmitter is the interface used by the Hydrator to emit Wails events.
// Satisfied by *app.App and *sync.backendEmitter (structural typing).
type EventEmitter interface {
	Emit(event string, data any)
}

// BackendEntry pairs a StorageBackend with its identity and local sync path.
// It is equivalent to placeholder.MountedBackend but defined here to avoid
// an import cycle (placeholder imports sync, sync may import cfapi).
type BackendEntry struct {
	ID        string
	Name      string
	Backend   plugins.StorageBackend
	LocalPath string
}

// providerEntry holds runtime state for one registered sync root.
type providerEntry struct {
	provider   *SyncProvider
	hydrator   *Hydrator
	backendID  string
	cancelFunc context.CancelFunc // cancels any in-flight OnFetchPlaceholders goroutine
}

// CompletionCallbacks holds optional sync-engine callbacks for CF completion events.
// OnDeleteCompletion is called when Windows reports a successful local delete via CF API.
// OnRenameCompletion is called when Windows reports a successful local rename via CF API.
// Both are set by app.go after the sync engine starts, so the CF provider can propagate
// remote CF operations (delete, rename) back to the backend (#v2.2-bugD).
type CompletionCallbacks struct {
	OnDeleteCompletion func(localPath string)
	OnRenameCompletion func(oldPath, newPath string)
}

// PinIntent represents a deferred pin/unpin request for a file that is currently
// being transferred.  Stored in CFManager.intentions until the transfer completes (#143).
type PinIntent struct {
	BackendID string
	LocalPath string
	Pin       bool
}

// CFManager manages one SyncProvider per enabled backend.
// It is safe for concurrent use.
type CFManager struct {
	cfg     *config.AppConfig
	emitter EventEmitter

	mu       gosync.RWMutex
	entries  map[string]*providerEntry // backendID → entry

	// intentions holds deferred PinIntents keyed by localPath (#143).
	// sync.Map is used for lock-free concurrent access from Engine goroutines.
	intentions gosync.Map
}

// NewCFManager creates a CFManager.
// cfg is used for CloudProviderID and display name.
// emitter receives cf:sync_state and cf:hydration_progress events.
func NewCFManager(cfg *config.AppConfig, emitter EventEmitter) *CFManager {
	return &CFManager{
		cfg:     cfg,
		emitter: emitter,
		entries: make(map[string]*providerEntry),
	}
}

// Start registers and connects the CF sync root for one backend.
// bc is the BackendEntry describing the backend.
// ch is the chunk cache to use during hydration (may be nil → noop).
func (m *CFManager) Start(bc BackendEntry, backend plugins.StorageBackend, ch cache.ChunkCache) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.entries[bc.ID]; exists {
		return nil // already started
	}

	if bc.LocalPath == "" {
		return fmt.Errorf("cfapi: backend %s has no LocalPath — cannot register CF sync root", bc.Name)
	}
	if ch == nil {
		ch = cache.NewNoopCache()
	}

	providerID := "{00000000-0000-0000-0000-000000000000}"
	if m.cfg != nil && m.cfg.CloudProviderID != "" {
		providerID = m.cfg.CloudProviderID
	}

	displayName := "GhostDrive"
	if bc.Name != "" {
		displayName = "GhostDrive — " + bc.Name
	}

	// Register the StorageProvider with Windows Explorer BEFORE CfRegisterSyncRoot.
	// This writes the SyncRootManager registry keys that Windows requires to display
	// cloud overlay icons (☁️, ✓✓, ⟳). Without this registration the OS shows no
	// cloud badge even when FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS is correctly set.
	// Non-fatal: log the error but proceed — sync functionality is unaffected.
	if err := RegisterStorageProvider(bc.LocalPath, bc.Name, displayName); err != nil {
		log.Printf("cfapi: StorageProvider registration %s: %v", bc.Name, err)
	}

	p := NewSyncProvider(bc.LocalPath, providerID, displayName)

	if err := p.Register(); err != nil {
		// On Linux (stub) this is a no-op. On Windows log but don't abort.
		log.Printf("cfapi: register sync root %s (%s): %v", bc.Name, bc.LocalPath, err)
	}

	hydrator := NewHydrator(backend, ch, p, m.emitter, bc.ID)

	cbs := CFCallbacks{
		OnFetchData:         hydrator.OnFetchData,
		OnCancelFetch:       hydrator.OnCancelFetch,
		OnFetchPlaceholders: hydrator.OnFetchPlaceholders,
	}

	if err := p.Connect(cbs); err != nil {
		log.Printf("cfapi: connect sync root %s (%s): %v", bc.Name, bc.LocalPath, err)
	}

	// Eagerly create placeholders for the root directory.
	// A cancellable context is stored so Stop() can abort the goroutine before
	// disconnecting the provider (MAJEUR-3).
	ctx, cancel := context.WithCancel(context.Background())
	if backend != nil && backend.IsConnected() {
		go func() {
			if err := hydrator.OnFetchPlaceholders(ctx, bc.LocalPath); err != nil {
				log.Printf("cfapi: fetch placeholders %s: %v", bc.Name, err)
			}
		}()
	} else {
		// No goroutine launched — cancel immediately to avoid leak.
		cancel()
		cancel = func() {} // make Stop() idempotent
	}

	m.entries[bc.ID] = &providerEntry{
		provider:   p,
		hydrator:   hydrator,
		backendID:  bc.ID,
		cancelFunc: cancel,
	}
	return nil
}

// SetCompletionCallbacks injects delete and rename completion handlers for the given backend.
// These handlers are invoked when Windows CF API fires NOTIFY_DELETE_COMPLETION or
// NOTIFY_RENAME_COMPLETION — i.e. when an operation on GhD: (the CF sync root) completes.
// Must be called after Start(backendID) and before user interactions (#v2.2-bugD).
func (m *CFManager) SetCompletionCallbacks(backendID string, cbs CompletionCallbacks) {
	m.mu.RLock()
	e, ok := m.entries[backendID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	e.provider.SetCompletionCallbacks(cbs.OnDeleteCompletion, cbs.OnRenameCompletion)
}

// Stop disconnects and deregisters the CF sync root for a backend.
// It cancels any in-flight OnFetchPlaceholders goroutine before disconnecting
// to prevent calls on an already-deregistered provider (MAJEUR-3).
func (m *CFManager) Stop(backendID string) error {
	m.mu.Lock()
	e, ok := m.entries[backendID]
	if ok {
		delete(m.entries, backendID)
	}
	m.mu.Unlock()

	if !ok {
		return nil
	}

	// Cancel the goroutine first, before provider operations.
	if e.cancelFunc != nil {
		e.cancelFunc()
	}

	if err := e.provider.Disconnect(); err != nil {
		log.Printf("cfapi: disconnect %s: %v", backendID, err)
	}
	if err := e.provider.Deregister(); err != nil {
		log.Printf("cfapi: deregister %s: %v", backendID, err)
	}
	return nil
}

// StartAll starts CF sync roots for all provided backends.
// caches maps backendID → ChunkCache; pass nil or empty map to use noop caches.
func (m *CFManager) StartAll(backends []BackendEntry, caches map[string]cache.ChunkCache) error {
	for _, bc := range backends {
		var ch cache.ChunkCache
		if caches != nil {
			ch = caches[bc.ID]
		}
		if err := m.Start(bc, bc.Backend, ch); err != nil {
			log.Printf("cfapi: StartAll: backend %s: %v", bc.Name, err)
			// Non-fatal: continue with remaining backends.
		}
	}
	return nil
}

// StopAll disconnects and deregisters all sync roots.
func (m *CFManager) StopAll() error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.entries))
	for id := range m.entries {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		if err := m.Stop(id); err != nil {
			log.Printf("cfapi: StopAll: %s: %v", id, err)
		}
	}
	return nil
}

// ConvertToPlaceholder converts the regular local file at localPath into a CF
// placeholder before SetSyncState is called.  This satisfies the optional
// sync.PlaceholderMaker interface (#151).
//
// Background: Download() writes files via os.Rename(tmp, dest) — neither the
// temp file nor the renamed destination is a CF placeholder.  Calling
// CfSetInSyncState on a non-placeholder writes invalid CF reparse-point data,
// corrupting the parent directory's CF state and causing 0x80070781 /
// 0x8007017c on subsequent user operations (New File, Rename).
// Converting first makes the file a proper placeholder so CfSetInSyncState
// operates correctly.
func (m *CFManager) ConvertToPlaceholder(backendID, localPath string) error {
	m.mu.RLock()
	e, ok := m.entries[backendID]
	m.mu.RUnlock()

	if !ok {
		return nil // backend not registered — silent no-op
	}

	return e.provider.ConvertToPlaceholder(localPath)
}

// SetSyncState exposes SyncProvider.SetSyncState for app.go and the sync engine.
// This method satisfies the sync.CFStateManager interface (backendID, localPath, state int).
func (m *CFManager) SetSyncState(backendID, localPath string, state int) error {
	m.mu.RLock()
	e, ok := m.entries[backendID]
	m.mu.RUnlock()

	if !ok {
		return nil // backend not registered — silent no-op
	}

	ss := SyncState(state)
	if err := e.provider.SetSyncState(localPath, ss); err != nil {
		return fmt.Errorf("cfapi: SetSyncState %s %s: %w", backendID, localPath, err)
	}

	// Emit event for the frontend.
	if m.emitter != nil {
		stateStr := syncStateString(ss)
		m.emitter.Emit("cf:sync_state", map[string]any{
			"backendID": backendID,
			"localPath": localPath,
			"state":     stateStr,
		})
	}
	return nil
}

// PinFile sets the pin state for a file — always local (true) or back to cloud-only (false).
// Exposed as Wails binding PinFile in app.go.
func (m *CFManager) PinFile(backendID, localPath string, pin bool) error {
	state := SyncStateUnpinned
	if pin {
		state = SyncStatePinned
	}
	return m.SetSyncState(backendID, localPath, int(state))
}

// ─── Pin Intent Queue (#143) ──────────────────────────────────────────────────

// QueuePinIntent stores a deferred pin/unpin intention for a file that is
// currently being transferred (state U or P).  Any existing intention for the
// same localPath is overwritten (last-intent-wins).
func (m *CFManager) QueuePinIntent(backendID, localPath string, pin bool) {
	m.intentions.Store(localPath, &PinIntent{
		BackendID: backendID,
		LocalPath: localPath,
		Pin:       pin,
	})
}

// ConsumeIntent reads and removes the pending PinIntent for localPath.
// Returns (backendID, pin, true) if an intent was found; ("", false, false) otherwise.
// Safe for concurrent calls from Engine goroutines.
func (m *CFManager) ConsumeIntent(localPath string) (backendID string, pin bool, ok bool) {
	v, loaded := m.intentions.LoadAndDelete(localPath)
	if !loaded {
		return "", false, false
	}
	intent, _ := v.(*PinIntent)
	if intent == nil {
		return "", false, false
	}
	return intent.BackendID, intent.Pin, true
}

// syncStateString converts a SyncState to its JSON-friendly string.
func syncStateString(s SyncState) string {
	switch s {
	case SyncStateCloudOnly:
		return "cloud_only"
	case SyncStateSyncing:
		return "syncing"
	case SyncStateSynced:
		return "synced"
	case SyncStatePinned:
		return "pinned"
	case SyncStateUnpinned:
		return "unpinned"
	default:
		return "unknown"
	}
}
