package loader_test

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/CCoupel/GhostDrive/plugins/loader"
)

// mockPluginBinary is the absolute path to the compiled mock plugin binary.
// Set by TestMain; empty string if compilation failed (integration tests skip).
var mockPluginBinary string

// TestMain compiles the mock plugin binary before all tests in this package.
//
// CI mode: set GHOSTDRIVE_MOCK_PLUGIN to the path of a pre-built static binary
// (CGO_ENABLED=0) so that the race detector is not disabled during go test.
// The CI workflow builds it in a dedicated step before running tests.
//
// Local fallback: when GHOSTDRIVE_MOCK_PLUGIN is unset, TestMain compiles the
// mock plugin on the fly (CGO_ENABLED=0 scoped to the exec.Command only).
// Integration tests that require the mock are guarded by requireMockPlugin.
func TestMain(m *testing.M) {
	os.Exit(runAllTests(m))
}

func runAllTests(m *testing.M) int {
	// CI path: use the pre-built binary to keep CGO_ENABLED=0 out of the test run.
	if prebuilt := os.Getenv("GHOSTDRIVE_MOCK_PLUGIN"); prebuilt != "" {
		if _, err := os.Stat(prebuilt); err == nil {
			mockPluginBinary = prebuilt
			log.Printf("TestMain: using pre-built mock plugin at %q", prebuilt)
			return m.Run()
		}
		log.Printf("TestMain: GHOSTDRIVE_MOCK_PLUGIN=%q not found -- falling back to local build", prebuilt)
	}

	// Local fallback: compile the mock plugin on the fly.
	tmpDir, err := os.MkdirTemp("", "ghostdrive-mock-plugin-build-*")
	if err != nil {
		log.Printf("TestMain: cannot create temp dir: %v -- integration tests will be skipped", err)
		return m.Run()
	}
	defer os.RemoveAll(tmpDir)

	moduleRoot, err := findModuleRoot()
	if err != nil {
		log.Printf("TestMain: cannot find module root: %v -- integration tests will be skipped", err)
		return m.Run()
	}

	// Use ".exe" extension so the *.exe Glob in Scan finds it on all platforms.
	mockPluginBinary = filepath.Join(tmpDir, "mock-plugin.exe")
	cmd := exec.Command("go", "build", "-o", mockPluginBinary,
		"github.com/CCoupel/GhostDrive/plugins/testdata/mock-plugin")
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("TestMain: build mock plugin failed: %v\n%s\n-- integration tests will be skipped", err, out)
		mockPluginBinary = ""
	}

	return m.Run()
}

// findModuleRoot walks up from the working directory to find the directory
// containing go.mod.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found (walked up from %s)", dir)
		}
		dir = parent
	}
}

// requireMockPlugin skips the calling test when the mock plugin binary is
// unavailable (compilation failed or skipped in TestMain).
func requireMockPlugin(t *testing.T) {
	t.Helper()
	if mockPluginBinary == "" {
		t.Skip("mock plugin binary not available -- skipping integration test")
	}
}

// copyMockPlugin copies the global mock binary to dstDir/dstName with 0755
// permissions and returns the full destination path.
func copyMockPlugin(t *testing.T, dstDir, dstName string) string {
	t.Helper()
	data, err := os.ReadFile(mockPluginBinary)
	require.NoError(t, err, "read mock plugin binary")
	dst := filepath.Join(dstDir, dstName)
	require.NoError(t, os.WriteFile(dst, data, 0755), "write mock plugin copy")
	return dst
}

// fastDelays returns a three-element delay slice suitable for watchdog tests.
// All delays are 10 ms so that watchdog cycles complete in well under a second.
func fastDelays() []time.Duration {
	d := 10 * time.Millisecond
	return []time.Duration{d, d, d}
}

// ── Unit tests (no mock binary required) ─────────────────────────────────────

// TestGRPCLoader_ScanEmptyDir verifies that scanning an empty directory
// does not produce errors and results in zero loaded plugins.
func TestGRPCLoader_ScanEmptyDir(t *testing.T) {
	dir := t.TempDir()
	l := loader.NewGRPCLoader()

	err := l.Scan(dir)
	require.NoError(t, err)

	plugins := l.GetLoadedPlugins()
	assert.Empty(t, plugins, "no plugins expected in empty directory")
}

// TestGRPCLoader_ScanDirNotExist verifies that scanning a non-existent
// directory does not panic and returns gracefully (Glob treats missing
// directories as empty matches, so no error is expected).
func TestGRPCLoader_ScanDirNotExist(t *testing.T) {
	l := loader.NewGRPCLoader()
	// filepath.Glob on a non-existent dir returns nil, nil -- no error.
	err := l.Scan("/tmp/ghostdrive-no-such-dir-xyz-12345")
	assert.NoError(t, err)
}

// TestGRPCLoader_ScanInvalidExe verifies that a directory containing a
// file that looks like a plugin (.exe) but is not a valid plugin binary
// results in a "failed" entry rather than a panic.
func TestGRPCLoader_ScanInvalidExe(t *testing.T) {
	dir := t.TempDir()

	// Create a fake .exe that is not a real go-plugin binary.
	fakePath := filepath.Join(dir, "badplugin.exe")
	require.NoError(t, os.WriteFile(fakePath, []byte("not a real exe"), 0755))

	l := loader.NewGRPCLoader()
	// Scan should not panic or block; it records the failure internally.
	err := l.Scan(dir)
	assert.NoError(t, err, "scan itself must not return an error even if a plugin fails to load")

	plugins := l.GetLoadedPlugins()
	// The bad plugin should appear with status "failed".
	require.Len(t, plugins, 1)
	assert.Equal(t, "badplugin", plugins[0].Name)
	assert.Equal(t, "failed", plugins[0].Status)
	assert.NotEmpty(t, plugins[0].Error)
}

// TestGRPCLoader_ShutdownNoPlugins verifies that calling Shutdown when no
// plugins are loaded does not error.
func TestGRPCLoader_ShutdownNoPlugins(t *testing.T) {
	l := loader.NewGRPCLoader()
	assert.NoError(t, l.Shutdown())
}

// TestGRPCLoader_ShutdownAfterFailedScan verifies that Shutdown succeeds
// even after a scan that produced failed entries.
func TestGRPCLoader_ShutdownAfterFailedScan(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "bad.exe")
	require.NoError(t, os.WriteFile(fakePath, []byte("garbage"), 0755))

	l := loader.NewGRPCLoader()
	_ = l.Scan(dir)

	assert.NoError(t, l.Shutdown(), "Shutdown must not error after a failed scan")

	// After shutdown, the entry list must be cleared.
	assert.Empty(t, l.GetLoadedPlugins())
}

// TestPluginInfo_Fields checks that PluginInfo fields are correctly populated
// for a failed plugin (path, name, status).
func TestPluginInfo_Fields(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "myplugin.exe")
	require.NoError(t, os.WriteFile(fakePath, []byte("fake"), 0755))

	l := loader.NewGRPCLoader()
	_ = l.Scan(dir)

	infos := l.GetLoadedPlugins()
	require.Len(t, infos, 1)

	info := infos[0]
	assert.Equal(t, "myplugin", info.Name)
	assert.Equal(t, fakePath, info.Path)
	assert.Equal(t, "failed", info.Status)
}

// TestGRPCLoader_ScanLinuxExecutable verifies that the loader discovers
// extensionless files with the executable bit set (Linux/macOS plugin
// convention) and attempts to load them as go-plugin binaries.
//
// The file is not a valid plugin, so the result must be a "failed" entry --
// but the important thing is that the loader found and attempted to load it,
// proving the extensionless scan path is active.
func TestGRPCLoader_ScanLinuxExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux executable scan not applicable on Windows")
	}

	dir := t.TempDir()

	// Extensionless executable -- should be picked up by the Linux scan.
	execFile := filepath.Join(dir, "myplugin")
	require.NoError(t, os.WriteFile(execFile, []byte("not a real plugin"), 0755))

	// Non-executable extensionless file -- must be skipped.
	noExec := filepath.Join(dir, "readme")
	require.NoError(t, os.WriteFile(noExec, []byte("not executable"), 0644))

	// Extensionless empty file -- must be skipped (size == 0).
	emptyFile := filepath.Join(dir, "empty")
	require.NoError(t, os.WriteFile(emptyFile, []byte{}, 0755))

	l := loader.NewGRPCLoader()
	require.NoError(t, l.Scan(dir))
	defer l.Shutdown()

	infos := l.GetLoadedPlugins()
	// Only the executable non-empty file should appear; the others are skipped.
	require.Len(t, infos, 1, "only the executable extensionless file must be scanned")
	assert.Equal(t, "myplugin", infos[0].Name)
	assert.Equal(t, execFile, infos[0].Path)
	assert.Equal(t, "failed", infos[0].Status, "non-plugin binary must result in failed status")
}

// TestGRPCLoader_ScanLinuxDir verifies that extensionless directories inside
// the plugins directory are not treated as plugin binaries.
func TestGRPCLoader_ScanLinuxDir(t *testing.T) {
	dir := t.TempDir()

	// A subdirectory with a name that has no extension -- must be ignored.
	subDir := filepath.Join(dir, "asubdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	l := loader.NewGRPCLoader()
	require.NoError(t, l.Scan(dir))
	defer l.Shutdown()

	assert.Empty(t, l.GetLoadedPlugins(), "subdirectories must not be loaded as plugins")
}

// ── Integration tests (require compiled mock plugin) ─────────────────────────

// TestGRPCLoader_ValidPlugin verifies that a valid go-plugin binary is loaded
// successfully: status must be "loaded" and Name() must return "mock".
func TestGRPCLoader_ValidPlugin(t *testing.T) {
	requireMockPlugin(t)

	dir := t.TempDir()
	copyMockPlugin(t, dir, "mock.exe")

	l := loader.NewGRPCLoaderWithOptions(loader.LoaderOptions{
		WatchdogDelays: fastDelays(),
	})
	require.NoError(t, l.Scan(dir))
	t.Cleanup(func() { _ = l.Shutdown() })

	infos := l.GetLoadedPlugins()
	require.Len(t, infos, 1, "exactly one plugin expected")
	assert.Equal(t, "mock", infos[0].Name, "plugin name must come from Name() RPC")
	assert.Equal(t, "loaded", infos[0].Status)
	assert.Empty(t, infos[0].Error)
}

// TestGRPCLoader_HandshakeFailed verifies that a binary that does not respond
// to the go-plugin handshake (wrong or missing magic cookie) results in a
// "failed" entry rather than panicking the loader.
func TestGRPCLoader_HandshakeFailed(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "wrongcookie.exe")
	// A binary that is not a valid go-plugin -- the handshake will fail.
	require.NoError(t, os.WriteFile(badPath, []byte("#!/bin/sh\nexit 0"), 0755))

	l := loader.NewGRPCLoader()
	require.NoError(t, l.Scan(dir))
	t.Cleanup(func() { _ = l.Shutdown() })

	infos := l.GetLoadedPlugins()
	require.Len(t, infos, 1)
	assert.Equal(t, "failed", infos[0].Status, "handshake failure must result in failed status")
	assert.NotEmpty(t, infos[0].Error, "failed entry must carry an error message")
}

// TestGRPCLoader_Watchdog_RestartOnCrash kills the mock plugin subprocess and
// verifies that the watchdog detects the exit and successfully restarts it.
// Uses short watchdog delays (10 ms) to avoid waiting for the default 1s/2s/4s.
//
// Skipped in v0.8.0: factory mode sets entry.client = nil, so
// KillPluginProcess is a no-op and the test passes trivially without exercising
// any watchdog logic.
// TODO(v0.9.0): add factory-instance crash recovery test that exercises the
// watchdog on an active, app-level backend connection.
func TestGRPCLoader_Watchdog_RestartOnCrash(t *testing.T) {
	t.Skip("KillPluginProcess is a no-op in factory mode (entry.client == nil) — TODO(v0.9.0): add factory-instance crash recovery test")
	requireMockPlugin(t)

	dir := t.TempDir()
	copyMockPlugin(t, dir, "mock.exe")

	l := loader.NewGRPCLoaderWithOptions(loader.LoaderOptions{
		WatchdogDelays: fastDelays(),
	})
	require.NoError(t, l.Scan(dir))
	t.Cleanup(func() { _ = l.Shutdown() })

	// Verify initial load.
	infos := l.GetLoadedPlugins()
	require.Len(t, infos, 1)
	require.Equal(t, "loaded", infos[0].Status, "plugin must be loaded before killing")

	// Kill the subprocess without cancelling the watchdog context.
	require.NoError(t, l.KillPluginProcess("mock"))

	// Watchdog detects exit (<= 500 ms poll interval) + 10 ms delay + restart.
	// Allow up to 5 s for the cycle to complete.
	require.Eventually(t, func() bool {
		for _, info := range l.GetLoadedPlugins() {
			if info.Name == "mock" && info.Status == "loaded" {
				return true
			}
		}
		return false
	}, 5*time.Second, 100*time.Millisecond,
		"plugin must return to 'loaded' status after a watchdog restart")
}

// TestGRPCLoader_Watchdog_MaxRetries is skipped in v0.8.0 because loadPlugin
// now uses factory mode (no persistent subprocess per registered plugin type).
// The watchdog restart path still exists and is exercised by the watchdog()
// function itself when a factory-spawned instance crashes; however there is no
// longer a persistent probe process to kill via KillPluginProcess, so this
// test's trigger mechanism no longer applies.
//
// TODO(v0.9.0): add a dedicated factory-instance crash recovery test that
// exercises the watchdog on an active, app-level backend connection.
func TestGRPCLoader_Watchdog_MaxRetries(t *testing.T) {
	t.Skip("watchdog max-retry test not applicable for factory-mode plugins (v0.8.0)")
}

// TestGRPCLoader_Shutdown_KillsPlugin verifies that Shutdown clears all
// plugin entries.  In v0.8.0 factory mode the probe subprocess is killed
// immediately after loadPlugin, so entry.client is nil and there is no
// persistent process to check Exited() on.  The important invariant is that
// GetLoadedPlugins() is empty after Shutdown.
func TestGRPCLoader_Shutdown_KillsPlugin(t *testing.T) {
	requireMockPlugin(t)

	dir := t.TempDir()
	copyMockPlugin(t, dir, "mock.exe")

	l := loader.NewGRPCLoaderWithOptions(loader.LoaderOptions{
		WatchdogDelays: fastDelays(),
	})
	require.NoError(t, l.Scan(dir))

	infos := l.GetLoadedPlugins()
	require.Len(t, infos, 1)
	require.Equal(t, "loaded", infos[0].Status, "plugin must be loaded before Shutdown")

	// Shutdown cancels watchdog contexts and clears all entries.
	require.NoError(t, l.Shutdown())
	assert.Empty(t, l.GetLoadedPlugins(), "entries must be cleared after Shutdown")
}

// TestGRPCLoader_Factory_UniqueInstances verifies that the v0.8.0 factory-mode
// loader registers a factory that spawns a fresh subprocess for each
// plugins.Get() call, ensuring that two backends of the same type are fully
// independent (distinct interface values, separate gRPC connections).
//
// Note: each plugins.Get("mock") spawns a subprocess that is not tracked by
// the loader.  The subprocesses are OS-reaped when the test process exits.
// This is an accepted limitation for v0.8.0; a proper lifecycle API for
// factory instances is tracked in a separate issue.
func TestGRPCLoader_Factory_UniqueInstances(t *testing.T) {
	requireMockPlugin(t)

	dir := t.TempDir()
	copyMockPlugin(t, dir, "mock.exe")

	l := loader.NewGRPCLoaderWithOptions(loader.LoaderOptions{
		WatchdogDelays: fastDelays(),
	})
	require.NoError(t, l.Scan(dir))
	t.Cleanup(func() { _ = l.Shutdown() })

	infos := l.GetLoadedPlugins()
	require.Len(t, infos, 1)
	require.Equal(t, "loaded", infos[0].Status)

	// Each plugins.Get("mock") call must invoke the factory, spawning a new
	// process and returning a distinct StorageBackend instance.
	b1, err1 := plugins.Get("mock")
	b2, err2 := plugins.Get("mock")
	require.NoError(t, err1, "first plugins.Get must succeed")
	require.NoError(t, err2, "second plugins.Get must succeed")
	require.NotNil(t, b1)
	require.NotNil(t, b2)

	// The two backends must be distinct objects (different pointers).
	// Using interface comparison: two interface values are equal only if they
	// share the same type AND the same underlying pointer value.
	assert.False(t, b1 == b2, "each plugins.Get() call must return a distinct backend instance")
}

// TestGRPCLoader_Shutdown_KillsFactoryClients is the non-regression test for
// issue #86: before the fix, the factory closure discarded the *goplugin.Client
// with "_", so subprocesses spawned via plugins.Get() outlived Shutdown().
// After the fix, every factory-spawned client is tracked in factoryClients and
// killed by Shutdown().
func TestGRPCLoader_Shutdown_KillsFactoryClients(t *testing.T) {
	requireMockPlugin(t)

	dir := t.TempDir()
	copyMockPlugin(t, dir, "mock.exe")

	l := loader.NewGRPCLoaderWithOptions(loader.LoaderOptions{
		WatchdogDelays: fastDelays(),
	})
	require.NoError(t, l.Scan(dir))

	// Safety cleanup in case the test fails before the explicit Shutdown call.
	t.Cleanup(func() { _ = l.Shutdown() })

	infos := l.GetLoadedPlugins()
	require.Len(t, infos, 1)
	require.Equal(t, "loaded", infos[0].Status, "plugin must be loaded before calling plugins.Get")

	// Spawn a factory subprocess via plugins.Get — this is the code path fixed by #86.
	// Before the fix, the client returned by launchPlugin was silently dropped here.
	backend, err := plugins.Get("mock")
	require.NoError(t, err, "plugins.Get must succeed after a successful Scan")
	require.NotNil(t, backend)

	// The loader must have recorded exactly one factory client.
	factoryClients := l.GetFactoryClientsForTest()
	require.Len(t, factoryClients, 1,
		"factory client must be tracked in factoryClients after plugins.Get (regression: was discarded with _ before #86 fix)")

	// Capture the client so we can still inspect it after Shutdown clears the slice.
	spawnedClient := factoryClients[0]
	require.False(t, spawnedClient.Exited(),
		"factory subprocess must be alive before Shutdown is called")

	// Shutdown must kill all factory-spawned subprocesses.
	require.NoError(t, l.Shutdown())

	// Allow a short grace period for the OS to propagate the kill signal, then
	// assert the subprocess is gone.  This would fail before the #86 fix because
	// the client was never stored and therefore never killed.
	require.Eventually(t, func() bool {
		return spawnedClient.Exited()
	}, 2*time.Second, 50*time.Millisecond,
		"factory subprocess must be killed by Shutdown (non-regression for issue #86)")
}
