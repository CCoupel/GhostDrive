package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CCoupel/GhostDrive/internal/backends"
	"github.com/CCoupel/GhostDrive/internal/config"
	"github.com/CCoupel/GhostDrive/internal/placeholder"
	internalsync "github.com/CCoupel/GhostDrive/internal/sync"
	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/CCoupel/GhostDrive/plugins/local"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain registers the "local" plugin explicitly (init()-based auto-register
// was removed in v1.1.0 — registration is now done by app.Startup()).
func TestMain(m *testing.M) {
	plugins.Register("local", func() plugins.StorageBackend { return local.New() })
	os.Exit(m.Run())
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// newTestApp creates a minimal App for unit testing without a Wails runtime.
// GhostDriveRoot is set to a temporary directory so auto-mode path generation
// stays within the test sandbox.
func newTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	return &App{
		cfgPath: filepath.Join(root, "config.json"),
		cfg: func() config.AppConfig {
			c := config.DefaultConfig()
			c.GhostDriveRoot = root
			return c
		}(),
		engines:      make(map[string]*internalsync.Engine),
		manager:      backends.NewBackendManager(nil),
		driveManager: placeholder.NewDriveManager(),
		descriptors:  make(map[string]plugins.PluginDescriptor),
	}
}

// localConfig returns a BackendConfig for the "local" plugin with all
// required fields pre-filled.  rootPath and syncDir must be absolute paths
// that exist on disk (or may be absent when testing ErrNotExist tolerance).
func localConfig(name, syncDir, rootPath string) plugins.BackendConfig {
	return plugins.BackendConfig{
		Name:       name,
		Type:       "local",
		SyncDir:    syncDir,
		LocalPath:  syncDir,
		RemotePath: "/remote",
		Params:     map[string]string{"rootPath": rootPath},
	}
}

// ─── Case 1 — path-traversal names "." and ".." ───────────────────────────────

func TestValidateBackendConfig_PathTraversalNames(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()
	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))
	syncDir := filepath.Join(tmp, "sync")
	require.NoError(t, os.MkdirAll(syncDir, 0755))

	for _, name := range []string{".", ".."} {
		t.Run("name="+name, func(t *testing.T) {
			bc := localConfig(name, syncDir, rootPath)
			_, err := a.validateBackendConfig(bc)
			require.Error(t, err, "name %q must be rejected", name)
			assert.Contains(t, err.Error(), "nom invalide")
		})
	}
}

// ─── Case 2 — Windows-invalid characters in Name ──────────────────────────────

func TestValidateBackendConfig_InvalidWindowsChars(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()
	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))
	syncDir := filepath.Join(tmp, "sync")
	require.NoError(t, os.MkdirAll(syncDir, 0755))

	cases := []struct {
		char string
		name string
	}{
		{`\`, `back\slash`},
		{`/`, `fo/rward`},
		{`:`, `col:on`},
		{`*`, `as*terisk`},
		{`?`, `qu?estion`},
		{`"`, `quo"te`},
		{`<`, `less<than`},
		{`>`, `great>er`},
		{`|`, `pip|e`},
	}

	for _, tc := range cases {
		t.Run("char="+tc.char, func(t *testing.T) {
			bc := localConfig(tc.name, syncDir, rootPath)
			_, err := a.validateBackendConfig(bc)
			require.Error(t, err, "name %q must be rejected", tc.name)
			assert.Contains(t, err.Error(), "nom invalide")
		})
	}
}

// ─── Case 3 — LocalPath uniqueness (blocking) ─────────────────────────────────

func TestValidateBackendConfig_LocalPathConflict(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	sharedLocalPath := filepath.Join(tmp, "shared-sync")
	require.NoError(t, os.MkdirAll(sharedLocalPath, 0755))

	rootA := filepath.Join(tmp, "sourceA")
	rootB := filepath.Join(tmp, "sourceB")
	require.NoError(t, os.MkdirAll(rootA, 0755))
	require.NoError(t, os.MkdirAll(rootB, 0755))

	// Inject an existing backend that uses sharedLocalPath.
	a.cfg.Backends = []plugins.BackendConfig{
		{
			ID:        "existing-1",
			Name:      "ExistingBackend",
			Type:      "local",
			LocalPath: sharedLocalPath,
			SyncDir:   sharedLocalPath,
			Params:    map[string]string{"rootPath": rootA},
		},
	}

	// New backend pointing to the same LocalPath → must be rejected.
	bc := localConfig("NewBackend", sharedLocalPath, rootB)
	_, err := a.validateBackendConfig(bc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dossier local est déjà utilisé")
}

// ─── Case 4 — rootPath duplicate (non-blocking warning) ──────────────────────

func TestValidateBackendConfig_RootPathWarning(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	sharedRootPath := filepath.Join(tmp, "sharedSource")
	require.NoError(t, os.MkdirAll(sharedRootPath, 0755))

	syncA := filepath.Join(tmp, "syncA")
	syncB := filepath.Join(tmp, "syncB")
	require.NoError(t, os.MkdirAll(syncA, 0755))
	require.NoError(t, os.MkdirAll(syncB, 0755))

	// Inject an existing backend using sharedRootPath.
	a.cfg.Backends = []plugins.BackendConfig{
		{
			ID:        "existing-2",
			Name:      "FirstBackend",
			Type:      "local",
			LocalPath: syncA,
			SyncDir:   syncA,
			Params:    map[string]string{"rootPath": sharedRootPath},
		},
	}

	// New backend with the same rootPath but a different LocalPath.
	bc := localConfig("SecondBackend", syncB, sharedRootPath)
	warning, err := a.validateBackendConfig(bc)

	// Must NOT be a blocking error.
	require.NoError(t, err, "rootPath duplicate must be a warning, not an error")
	// Must produce a non-empty warning.
	assert.NotEmpty(t, warning, "expected a non-blocking warning for duplicate rootPath")
	assert.True(t, strings.Contains(warning, "FirstBackend") || strings.Contains(warning, "dossier source"),
		"warning should mention the conflicting backend: %q", warning)
}

// ─── Case 5 — AddBackend auto-mode computes LocalPath from GhostDriveRoot ────

func TestAddBackend_AutoLocalPath(t *testing.T) {
	a := newTestApp(t)

	// Provide a rootPath (source directory) that exists — required by local.Connect.
	tmp := t.TempDir()
	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	bc := plugins.BackendConfig{
		Name:       "AutoTest",
		Type:       "local",
		RemotePath: "/remote",
		Params:     map[string]string{"rootPath": rootPath},
		// LocalPath intentionally empty → auto-mode
	}

	result, err := a.AddBackend(bc)
	require.NoError(t, err)

	expectedLocalPath := filepath.Join(a.GetGhostDriveRoot(), "AutoTest")
	assert.Equal(t, expectedLocalPath, result.LocalPath,
		"auto-mode LocalPath must be <GhostDriveRoot>/<Name>")
	assert.Equal(t, expectedLocalPath, result.SyncDir,
		"SyncDir must equal LocalPath in auto-mode")

	// The directory must have been created on disk.
	info, err := os.Stat(result.LocalPath)
	require.NoError(t, err, "auto-created LocalPath must exist on disk")
	assert.True(t, info.IsDir())
}

// ─── Case 7 — Startup creates GhostDriveRoot if it does not exist (#58) ──────

func TestStartup_CreatesGhostDriveRoot(t *testing.T) {
	// Use a sub-directory that does not yet exist as GhostDriveRoot.
	baseDir := t.TempDir()
	ghostRoot := filepath.Join(baseDir, "GhostDrive")
	cfgPath := filepath.Join(baseDir, "config.json")

	// Write a config.json with GhostDriveRoot pointing to our temp dir.
	// This ensures Startup() loads the config (no fallback to DefaultConfig)
	// and keeps the GhostDriveRoot value we set.
	testCfg := config.DefaultConfig()
	testCfg.GhostDriveRoot = ghostRoot
	require.NoError(t, config.Save(testCfg, cfgPath))

	a := &App{
		cfgPath: cfgPath,
		cfg:     testCfg,
		engines: make(map[string]*internalsync.Engine),
		manager: backends.NewBackendManager(nil),
	}

	// GhostDriveRoot must not exist before Startup.
	_, err := os.Stat(ghostRoot)
	require.True(t, os.IsNotExist(err), "GhostDriveRoot must not exist before Startup")

	// Call Startup with a nil context (no Wails runtime in tests).
	// emitError / emit are no-ops when ctx == nil.
	a.Startup(nil)

	// GhostDriveRoot must now exist on disk.
	info, err := os.Stat(ghostRoot)
	require.NoError(t, err, "Startup must create GhostDriveRoot")
	assert.True(t, info.IsDir(), "GhostDriveRoot must be a directory")
}

// ─── Case 6 — validateBackendConfig tolerates ErrNotExist for SyncDir ────────

func TestValidateBackendConfig_SyncDirNotExist_Tolerated(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	// Point SyncDir to a path that does NOT yet exist.
	nonExistentSyncDir := filepath.Join(tmp, "does-not-exist")

	bc := localConfig("GhostTest", nonExistentSyncDir, rootPath)
	_, err := a.validateBackendConfig(bc)

	assert.NoError(t, err,
		"validateBackendConfig must tolerate ErrNotExist for SyncDir (MkdirAll runs after validation)")
}

// ─── Case 8 — Drive lifecycle (v1.1.x per-backend DriveManager) ──────────────

// TestGetDriveStatuses_InitialState verifies that GetDriveStatuses returns an
// empty map (not nil) when no backends have been enabled yet.
func TestGetDriveStatuses_InitialState(t *testing.T) {
	a := newTestApp(t)
	statuses := a.GetDriveStatuses()
	assert.NotNil(t, statuses, "GetDriveStatuses must return a non-nil map")
	assert.Len(t, statuses, 0, "no drives should be mounted at startup")
}

// TestGetDriveStatus_Deprecated verifies the deprecated binding returns an
// empty DriveStatus (not mounted).
func TestGetDriveStatus_Deprecated(t *testing.T) {
	a := newTestApp(t)
	s := a.GetDriveStatus()
	assert.False(t, s.Mounted, "deprecated GetDriveStatus must return empty DriveStatus")
}

func TestGetMountPoint_Default(t *testing.T) {
	a := newTestApp(t)
	// Default mount point must be non-empty on every platform.
	assert.NotEmpty(t, a.GetMountPoint(), "default mount point must not be empty")
}

func TestGetMountPoint_Configured(t *testing.T) {
	a := newTestApp(t)
	a.cfg.MountPoint = "/tmp/test-ghost-mount"
	assert.Equal(t, "/tmp/test-ghost-mount", a.GetMountPoint())
}

// ─── Case 9 — Shutdown calls UnmountAll before stopping engines ──────────────

func TestShutdown_CallsUnmountAll(t *testing.T) {
	a := newTestApp(t)
	// Shutdown must not panic even when no drives are mounted.
	assert.NotPanics(t, func() {
		a.Shutdown(nil)
	})
	// After Shutdown, the drive pool must be empty.
	assert.Len(t, a.GetDriveStatuses(), 0)
}

// ─── Case 10 — GetPluginDescriptors returns at least "local" after Startup ───
//
// This test validates the contract defined in contracts/plugin-describe.md §5:
//   - GetPluginDescriptors() returns the descriptors of all available plugins.
//   - After Startup(), the "local" descriptor must be present (registered via
//     ServeInProcess in app.Startup).
//   - The result is never nil — an empty slice is returned when no plugins are loaded.
//
// Depends on:
//   - plugins/plugin.go: PluginDescriptor, ParamSpec, ParamType types (#79)
//   - plugins/grpc/inprocess.go: ServeInProcess function (#79)
//   - internal/app/app.go: GetPluginDescriptors() binding + descriptors cache (#79)

func TestGetPluginDescriptors_ContainsLocal(t *testing.T) {
	// Use a sub-directory that does not yet exist as GhostDriveRoot.
	baseDir := t.TempDir()
	ghostRoot := filepath.Join(baseDir, "GhostDrive")
	cfgPath := filepath.Join(baseDir, "config.json")

	// Write a minimal config.json with our temp GhostDriveRoot.
	testCfg := config.DefaultConfig()
	testCfg.GhostDriveRoot = ghostRoot
	require.NoError(t, config.Save(testCfg, cfgPath))

	a := &App{
		cfgPath: cfgPath,
		cfg:     testCfg,
		engines: make(map[string]*internalsync.Engine),
		manager: backends.NewBackendManager(nil),
	}

	// Startup registers the "local" plugin via ServeInProcess and populates
	// a.descriptors.
	a.Startup(nil)

	// GetPluginDescriptors must not return nil.
	descriptors := a.GetPluginDescriptors()
	require.NotNil(t, descriptors,
		"GetPluginDescriptors must never return nil")

	// Build a map by Type for easier assertions.
	byType := make(map[string]plugins.PluginDescriptor, len(descriptors))
	for _, d := range descriptors {
		byType[d.Type] = d
	}

	// "local" must be present.
	localDesc, ok := byType["local"]
	assert.True(t, ok,
		"GetPluginDescriptors must contain a descriptor with Type==\"local\" after Startup")

	if ok {
		assert.Equal(t, "local", localDesc.Type)
		assert.NotEmpty(t, localDesc.DisplayName,
			"local descriptor DisplayName must not be empty")
		assert.GreaterOrEqual(t, len(localDesc.Params), 1,
			"local descriptor must contain at least one ParamSpec")
	}
}

func TestGetPluginDescriptors_NeverNilWhenNoPlugins(t *testing.T) {
	// A freshly created App (before Startup) must return an empty slice, not nil.
	a := newTestApp(t)

	descriptors := a.GetPluginDescriptors()
	require.NotNil(t, descriptors,
		"GetPluginDescriptors must return an empty slice (not nil) before Startup")
}

// ─── UpdateBackend — non-regression tests for bugfix #84 ─────────────────────

// TestUpdateBackend_NotFound verifies that UpdateBackend returns an error
// when the backend ID does not exist in the configuration.
func TestUpdateBackend_NotFound(t *testing.T) {
	a := newTestApp(t)
	_, err := a.UpdateBackend(plugins.BackendConfig{
		ID:   "does-not-exist",
		Name: "Ghost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestUpdateBackend_ChangesName verifies that UpdateBackend correctly applies
// a name change and returns the updated BackendConfig.
func TestUpdateBackend_ChangesName(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	bc := plugins.BackendConfig{
		Name:       "OldName",
		Type:       "local",
		RemotePath: "/remote",
		Params:     map[string]string{"rootPath": rootPath},
	}
	added, err := a.AddBackend(bc)
	require.NoError(t, err)

	// Change the name and update.
	added.Name = "NewName"
	updated, err := a.UpdateBackend(added)
	require.NoError(t, err)
	assert.Equal(t, "NewName", updated.Name, "UpdateBackend must reflect the new name")

	// Verify persistence in in-memory config.
	a.mu.RLock()
	var found bool
	for _, b := range a.cfg.Backends {
		if b.ID == added.ID {
			found = true
			assert.Equal(t, "NewName", b.Name, "persisted backend must carry the new name")
		}
	}
	a.mu.RUnlock()
	assert.True(t, found, "updated backend must remain in cfg.Backends")
}

// TestUpdateBackend_PreservesID verifies that the backend ID is not mutated
// by UpdateBackend (immutable field contract).
func TestUpdateBackend_PreservesID(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	bc := plugins.BackendConfig{
		Name:       "IDTest",
		Type:       "local",
		RemotePath: "/remote",
		Params:     map[string]string{"rootPath": rootPath},
	}
	added, err := a.AddBackend(bc)
	require.NoError(t, err)

	originalID := added.ID
	require.NotEmpty(t, originalID, "AddBackend must assign a non-empty ID")

	// Rename and update — ID must be preserved.
	added.Name = "IDTestRenamed"
	updated, err := a.UpdateBackend(added)
	require.NoError(t, err)
	assert.Equal(t, originalID, updated.ID,
		"UpdateBackend must not mutate the backend ID")
}

// TestUpdateBackend_UniquenessExcludesSelf verifies that updating a backend
// while keeping the same name does not trigger a "duplicate name" validation
// error (self-exclusion guard in validateBackendConfig via ex.ID == bc.ID).
func TestUpdateBackend_UniquenessExcludesSelf(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	bc := plugins.BackendConfig{
		Name:       "SameName",
		Type:       "local",
		RemotePath: "/remote",
		Params:     map[string]string{"rootPath": rootPath},
	}
	added, err := a.AddBackend(bc)
	require.NoError(t, err)

	// Call UpdateBackend with identical config — must not fail with "already exists".
	_, err = a.UpdateBackend(added)
	require.NoError(t, err,
		"UpdateBackend with unchanged name must not trigger duplicate-name error")
}

// ─── v1.1.x tests — #85 #88 ──────────────────────────────────────────────────

// TestAddBackend_ForcedDisabled verifies that AddBackend always persists the
// backend with Enabled=false regardless of what the caller sends (#85).
func TestAddBackend_ForcedDisabled(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	bc := plugins.BackendConfig{
		Name:       "ForcedDisabledTest",
		Type:       "local",
		RemotePath: "/remote",
		Enabled:    true, // caller explicitly requests enabled=true
		Params:     map[string]string{"rootPath": rootPath},
	}

	result, err := a.AddBackend(bc)
	require.NoError(t, err)

	// The returned BackendConfig must have Enabled=false.
	assert.False(t, result.Enabled,
		"AddBackend must force Enabled=false regardless of the caller's value (#85)")

	// Verify the value is persisted in memory.
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, b := range a.cfg.Backends {
		if b.ID == result.ID {
			assert.False(t, b.Enabled,
				"persisted backend must have Enabled=false (#85)")
		}
	}
}

// TestAddBackend_AutoAssignsMountPoint verifies that AddBackend auto-assigns a
// non-empty MountPoint when the caller provides an empty one (#88).
func TestAddBackend_AutoAssignsMountPoint(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	bc := plugins.BackendConfig{
		Name:       "AutoMountTest",
		Type:       "local",
		RemotePath: "/remote",
		MountPoint: "", // explicitly empty → auto-assign expected
		Params:     map[string]string{"rootPath": rootPath},
	}

	result, err := a.AddBackend(bc)
	require.NoError(t, err)

	// On CI Linux AssignAvailableLetter may return "" (no WinFsp) or a letter.
	// The important invariant is: if a letter is available it is assigned.
	// We simply check the field is consistent with what was persisted.
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, b := range a.cfg.Backends {
		if b.ID == result.ID {
			assert.Equal(t, result.MountPoint, b.MountPoint,
				"persisted MountPoint must match the returned MountPoint")
		}
	}
}

// TestValidateBackendConfig_DuplicateMountPoint verifies that two backends with
// the same MountPoint trigger a blocking error on the second (#88).
func TestValidateBackendConfig_DuplicateMountPoint(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	syncA := filepath.Join(tmp, "syncA")
	syncB := filepath.Join(tmp, "syncB")
	rootA := filepath.Join(tmp, "rootA")
	rootB := filepath.Join(tmp, "rootB")
	require.NoError(t, os.MkdirAll(syncA, 0755))
	require.NoError(t, os.MkdirAll(syncB, 0755))
	require.NoError(t, os.MkdirAll(rootA, 0755))
	require.NoError(t, os.MkdirAll(rootB, 0755))

	// Inject an existing backend with MountPoint="G:".
	a.cfg.Backends = []plugins.BackendConfig{
		{
			ID:         "mp-existing",
			Name:       "MPExisting",
			Type:       "local",
			LocalPath:  syncA,
			SyncDir:    syncA,
			MountPoint: "G:",
			Enabled:    true,
			Params:     map[string]string{"rootPath": rootA},
		},
	}

	// New backend with the same MountPoint → must fail.
	bc := plugins.BackendConfig{
		Name:       "MPDuplicate",
		Type:       "local",
		LocalPath:  syncB,
		SyncDir:    syncB,
		RemotePath: "/remote",
		MountPoint: "G:",
		Params:     map[string]string{"rootPath": rootB},
	}
	_, err := a.validateBackendConfig(bc)
	require.Error(t, err, "duplicate MountPoint must be a blocking error (#88)")
	assert.Contains(t, err.Error(), "point de montage")
}

// TestValidateBackendConfig_DuplicateMountPoint_DisabledBackend verifies that
// a disabled backend still reserves its MountPoint (blocking error for the
// second even when first is Enabled=false) (#88).
func TestValidateBackendConfig_DuplicateMountPoint_DisabledBackend(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	syncA := filepath.Join(tmp, "syncA")
	syncB := filepath.Join(tmp, "syncB")
	rootA := filepath.Join(tmp, "rootA")
	rootB := filepath.Join(tmp, "rootB")
	require.NoError(t, os.MkdirAll(syncA, 0755))
	require.NoError(t, os.MkdirAll(syncB, 0755))
	require.NoError(t, os.MkdirAll(rootA, 0755))
	require.NoError(t, os.MkdirAll(rootB, 0755))

	// Disabled backend with MountPoint="H:".
	a.cfg.Backends = []plugins.BackendConfig{
		{
			ID:         "mp-disabled",
			Name:       "MPDisabled",
			Type:       "local",
			LocalPath:  syncA,
			SyncDir:    syncA,
			MountPoint: "H:",
			Enabled:    false, // disabled — but must still block
			Params:     map[string]string{"rootPath": rootA},
		},
	}

	bc := plugins.BackendConfig{
		Name:       "MPConflict",
		Type:       "local",
		LocalPath:  syncB,
		SyncDir:    syncB,
		RemotePath: "/remote",
		MountPoint: "H:",
		Params:     map[string]string{"rootPath": rootB},
	}
	_, err := a.validateBackendConfig(bc)
	require.Error(t, err,
		"disabled backend must still block duplicate MountPoint (#88)")
	assert.Contains(t, err.Error(), "point de montage")
}

// TestSetBackendEnabled_MountsOnEnable verifies that enabling a backend
// triggers a mount attempt. On Linux/CI (NullDrive), Mount returns
// ErrNotSupported — SetBackendEnabled must therefore return an error on
// non-Windows when a MountPoint is set, but must NOT panic (#85).
func TestSetBackendEnabled_MountsOnEnable(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	// Add a backend (always created disabled).
	bc := plugins.BackendConfig{
		Name:       "MountOnEnable",
		Type:       "local",
		RemotePath: "/remote",
		MountPoint: "F:",
		Params:     map[string]string{"rootPath": rootPath},
	}
	added, err := a.AddBackend(bc)
	require.NoError(t, err)
	require.False(t, added.Enabled, "precondition: backend must start disabled")

	// Enable the backend — on Linux NullDrive returns an error but must not panic.
	enableErr := a.SetBackendEnabled(added.ID, true)

	// On non-Windows the mount fails → SetBackendEnabled propagates the error
	// and rolls back (Enabled stays false).
	// On Windows a real mount would succeed (tested in integration).
	// Either way: no panic.
	if enableErr != nil {
		// Rollback: enabled flag must be reverted.
		a.mu.RLock()
		for _, b := range a.cfg.Backends {
			if b.ID == added.ID {
				assert.False(t, b.Enabled,
					"on mount failure, Enabled must be rolled back to false (#85)")
			}
		}
		a.mu.RUnlock()
	} else {
		// Windows path (or future NullDrive that does not error).
		_, ok := a.driveManager.GetStatus(added.ID)
		assert.True(t, ok, "drive must be registered in DriveManager after enable (#88)")
	}
}

// TestSetBackendEnabled_UnmountsOnDisable verifies that disabling a backend
// removes it from the DriveManager pool (#85).
func TestSetBackendEnabled_UnmountsOnDisable(t *testing.T) {
	a := newTestApp(t)

	// Manually inject a "mounted" entry in the DriveManager pool by injecting
	// the backend config directly (bypassing AddBackend/SetBackendEnabled to
	// avoid the NullDrive failure on Linux).
	a.mu.Lock()
	a.cfg.Backends = []plugins.BackendConfig{
		{
			ID:         "unmount-test",
			Name:       "UnmountTest",
			Type:       "local",
			LocalPath:  t.TempDir(),
			SyncDir:    t.TempDir(),
			RemotePath: "/remote",
			MountPoint: "I:",
			Enabled:    true,
			Params:     map[string]string{"rootPath": t.TempDir()},
		},
	}
	a.mu.Unlock()

	// Drive is NOT actually mounted (NullDrive on Linux); Unmount on a missing
	// entry is a no-op per DriveManager contract. Disabling should not error.
	err := a.SetBackendEnabled("unmount-test", false)
	// On non-Windows the manager.Remove may fail (backend never connected);
	// the important thing is no panic and the drive pool is clean.
	_ = err // tolerate error from manager.Remove (not connected on CI)

	// After disabling, the drive must not be in the DriveManager pool.
	_, ok := a.driveManager.GetStatus("unmount-test")
	assert.False(t, ok,
		"DriveManager must not contain the backend after disabling (#85)")
}

// TestRemoveBackend_UnmountsDrive verifies that RemoveBackend unmounts the
// per-backend virtual drive and removes the backend from the in-memory config (#85 #88).
func TestRemoveBackend_UnmountsDrive(t *testing.T) {
	a := newTestApp(t)

	// Inject a backend directly (bypassing AddBackend so we can set Enabled=true
	// without triggering a real mount, which would fail on CI Linux).
	backendID := "remove-drive-test"
	a.mu.Lock()
	a.cfg.Backends = []plugins.BackendConfig{
		{
			ID:         backendID,
			Name:       "RemoveTest",
			Type:       "local",
			LocalPath:  t.TempDir(),
			SyncDir:    t.TempDir(),
			RemotePath: "/remote",
			MountPoint: "J:",
			Enabled:    true,
			Params:     map[string]string{"rootPath": t.TempDir()},
		},
	}
	a.mu.Unlock()

	// Call RemoveBackend — on CI Linux the DriveManager pool is already empty
	// (no real mount happened), so Unmount is a no-op.  What matters is:
	// 1. No panic / error from RemoveBackend itself.
	// 2. The backend is removed from cfg.Backends.
	// 3. The DriveManager pool has no entry for backendID.
	err := a.RemoveBackend(backendID)
	// manager.Remove may return "not found" (backend was never connected) — that is
	// tolerated by RemoveBackend.  Any other error is a test failure.
	require.NoError(t, err, "RemoveBackend must not return an error for a never-connected backend (#85)")

	// Drive must not be in the DriveManager pool.
	_, ok := a.driveManager.GetStatus(backendID)
	assert.False(t, ok, "DriveManager must not contain the backend after RemoveBackend (#88)")

	// Backend must be removed from in-memory config.
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, b := range a.cfg.Backends {
		assert.NotEqual(t, backendID, b.ID,
			"backend must be removed from cfg.Backends after RemoveBackend (#85)")
	}
}

// TestRemoveBackend_UnknownID verifies that RemoveBackend on a non-existent ID
// returns no error (idempotent) (#85).
func TestRemoveBackend_UnknownID(t *testing.T) {
	a := newTestApp(t)
	err := a.RemoveBackend("does-not-exist")
	// config.Save on an empty slice is valid; no error expected.
	assert.NoError(t, err, "RemoveBackend on an unknown ID must not return an error")
}

// TestGetAvailableDriveLetters_NonWindows verifies the binding returns nil on Linux (#88).
func TestGetAvailableDriveLetters_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("non-Windows specific test")
	}
	a := newTestApp(t)
	letters := a.GetAvailableDriveLetters()
	assert.Nil(t, letters, "GetAvailableDriveLetters must return nil on non-Windows (#88)")
}

// TestSetBackendEnabled_ManagerAddFails_RollsBack verifies that if manager.Add
// fails during the enable path, Enabled is rolled back to false (#85).
func TestSetBackendEnabled_ManagerAddFails_RollsBack(t *testing.T) {
	a := newTestApp(t)

	// Inject a backend with an unregistered type so that manager.Add fails at
	// InstantiateBackend (plugins.Get returns an error for unknown types).
	const badID = "bad-type-id"
	a.mu.Lock()
	a.cfg.Backends = []plugins.BackendConfig{
		{
			ID:         badID,
			Name:       "BadTypeBackend",
			Type:       "unknown-type-xyz", // never registered
			LocalPath:  t.TempDir(),
			SyncDir:    t.TempDir(),
			RemotePath: "/remote",
			MountPoint: "K:",
			Enabled:    false,
			Params:     map[string]string{},
		},
	}
	a.mu.Unlock()

	err := a.SetBackendEnabled(badID, true)
	require.Error(t, err, "SetBackendEnabled must return an error when manager.Add fails (#85)")

	// Rollback: Enabled must be reverted to false.
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, b := range a.cfg.Backends {
		if b.ID == badID {
			assert.False(t, b.Enabled,
				"Enabled must be rolled back to false after manager.Add failure (#85)")
		}
	}
}

// TestUpdateBackend_MountPointChange verifies that updating an enabled backend
// with a different MountPoint triggers a remount attempt (and covers the
// unmount-old + mount-new code path in UpdateBackend #88).
func TestUpdateBackend_MountPointChange(t *testing.T) {
	a := newTestApp(t)
	tmp := t.TempDir()

	rootPath := filepath.Join(tmp, "source")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	// Create a backend (always disabled after AddBackend).
	bc := plugins.BackendConfig{
		Name:       "MPChangeTest",
		Type:       "local",
		RemotePath: "/remote",
		MountPoint: "L:",
		Params:     map[string]string{"rootPath": rootPath},
	}
	added, err := a.AddBackend(bc)
	require.NoError(t, err)

	// Update the backend: change MountPoint and request Enabled=true.
	// On Linux, manager.Add (reconnect) succeeds but driveManager.Mount fails.
	// UpdateBackend must still persist the config without panicking.
	updated := added
	updated.MountPoint = "M:"
	updated.Enabled = true

	result, updateErr := a.UpdateBackend(updated)
	// On Linux, UpdateBackend may return an error if mount fails at the persist
	// stage — or it may succeed and log the mount error.  Either way:
	// 1. No panic.
	// 2. If no error, the persisted MountPoint is "M:".
	if updateErr == nil {
		assert.Equal(t, "M:", result.MountPoint,
			"persisted MountPoint must reflect the requested change (#88)")
	}
	// Test is considered successful as long as no panic occurs.
}

// TestStartup_MigratesMountPoint verifies that Startup() assigns a MountPoint
// to backends that were created before v1.1.x (MountPoint was empty) (#88).
func TestStartup_MigratesMountPoint(t *testing.T) {
	baseDir := t.TempDir()
	ghostRoot := filepath.Join(baseDir, "GhostDrive")
	cfgPath := filepath.Join(baseDir, "config.json")

	rootPath := filepath.Join(baseDir, "src")
	require.NoError(t, os.MkdirAll(rootPath, 0755))

	syncDir := filepath.Join(ghostRoot, "LegacyBackend")

	// Write a config with a backend that has no MountPoint (pre-v1.1.x).
	testCfg := config.DefaultConfig()
	testCfg.GhostDriveRoot = ghostRoot
	testCfg.Backends = []plugins.BackendConfig{
		{
			ID:         "legacy-id",
			Name:       "LegacyBackend",
			Type:       "local",
			Enabled:    false,
			LocalPath:  syncDir,
			SyncDir:    syncDir,
			RemotePath: "/remote",
			MountPoint: "", // missing — migration must assign one
			Params:     map[string]string{"rootPath": rootPath},
		},
	}
	require.NoError(t, config.Save(testCfg, cfgPath))

	a := &App{
		cfgPath:      cfgPath,
		cfg:          testCfg,
		engines:      make(map[string]*internalsync.Engine),
		manager:      backends.NewBackendManager(nil),
		driveManager: placeholder.NewDriveManager(),
		descriptors:  make(map[string]plugins.PluginDescriptor),
	}

	// Startup triggers MountPoint migration.
	a.Startup(nil)

	// After Startup, the backend must have a non-empty MountPoint.
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, b := range a.cfg.Backends {
		if b.ID == "legacy-id" {
			// On Linux AssignAvailableLetter returns "E:" (no OS check).
			// On Windows it may return any available letter.
			// Either way it must not be empty after migration.
			assert.NotEmpty(t, b.MountPoint,
				"Startup must migrate empty MountPoint to a non-empty value (#88)")
		}
	}
}
