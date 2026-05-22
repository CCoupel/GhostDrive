//go:build !windows

// Package cfapi wraps the Windows Cloud Filter API (CF API) for Files On-Demand.
// On non-Windows platforms all operations are no-ops so the rest of the codebase
// compiles and tests cleanly on Linux.
// Shared types (SyncState, PlaceholderInfo, FetchRequest, CFCallbacks) live in
// types.go (no build constraint) and are available on all platforms.
package cfapi

// SyncProvider manages the CF API lifecycle for one backend (one sync root).
type SyncProvider struct {
	localPath   string
	providerID  string
	displayName string
}

// NewSyncProvider creates a SyncProvider for the given local path.
func NewSyncProvider(localPath, providerID, displayName string) *SyncProvider {
	return &SyncProvider{
		localPath:   localPath,
		providerID:  providerID,
		displayName: displayName,
	}
}

func (p *SyncProvider) Register() error                                              { return nil }
func (p *SyncProvider) Deregister() error                                            { return nil }
func (p *SyncProvider) Connect(_ CFCallbacks) error                                  { return nil }
func (p *SyncProvider) Disconnect() error                                            { return nil }
func (p *SyncProvider) CreatePlaceholders(_ string, _ []PlaceholderInfo) (int, error) { return 0, nil }
func (p *SyncProvider) UpdatePlaceholder(_ string, _ PlaceholderInfo) error          { return nil }
func (p *SyncProvider) SetSyncState(_ string, _ SyncState) error                     { return nil }
func (p *SyncProvider) ExecuteTransfer(_ FetchRequest, _ []byte, _ bool) error       { return nil }
func (p *SyncProvider) ReportError(_ FetchRequest, _ error) error                    { return nil }
func (p *SyncProvider) ReportProgress(_ FetchRequest, _, _ int64) error              { return nil }
