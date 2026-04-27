package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CCoupel/GhostDrive/internal/backends"
	"github.com/CCoupel/GhostDrive/internal/config"
	"github.com/CCoupel/GhostDrive/internal/placeholder"
	internalsync "github.com/CCoupel/GhostDrive/internal/sync"
	"github.com/CCoupel/GhostDrive/plugins"
	_ "github.com/CCoupel/GhostDrive/plugins/local" // registers "local" via init()
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		engines: make(map[string]*internalsync.Engine),
		manager: backends.NewBackendManager(nil),
		drive:   placeholder.New(),
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

// ─── Case 8 — MountDrive / UnmountDrive pass on NullDrive (non-Windows) ──────

func TestMountDrive_NoBackends_ReturnsError(t *testing.T) {
	a := newTestApp(t)
	// No backends configured → MountDrive must return an error on every platform.
	err := a.MountDrive()
	require.Error(t, err, "MountDrive with no connected backends must fail")
}

func TestUnmountDrive_NotMounted_IsNoop(t *testing.T) {
	a := newTestApp(t)
	// Unmounting when not mounted must not error on any platform.
	assert.NoError(t, a.UnmountDrive())
}

func TestGetDriveStatus_InitialState(t *testing.T) {
	a := newTestApp(t)
	s := a.GetDriveStatus()
	assert.False(t, s.Mounted, "drive must not be mounted at startup")
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

// ─── Case 9 — Shutdown calls Unmount before stopping engines (#57) ────────────

func TestShutdown_CallsUnmount(t *testing.T) {
	a := newTestApp(t)
	// Shutdown must not panic even when drive is not mounted.
	assert.NotPanics(t, func() {
		a.Shutdown(nil)
	})
	// After Shutdown, drive must still report not-mounted.
	assert.False(t, a.GetDriveStatus().Mounted)
}
