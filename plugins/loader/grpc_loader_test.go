package loader_test

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CCoupel/GhostDrive/plugins/loader"
)

// mockPluginBinary is the absolute path to the compiled mock plugin binary.
// Set by TestMain; empty string if compilation failed (integration tests skip).
var mockPluginBinary string

// TestMain compiles the mock plugin before all tests in this package.
//
// The mock plugin is compiled from
// github.com/CCoupel/GhostDrive/plugins/testdata/mock-plugin into a temporary
// directory using CGO_ENABLED=0 so that it works on all CI platforms.
// Integration tests that require the mock are guarded by requireMockPlugin.
func TestMain(m *testing.M) {
	os.Exit(runAllTests(m))
}

func runAllTests(m *testing.M) int {
	tmpDir, err := os.MkdirTemp("", "ghostdrive-mock-plugin-build-*")
	if err != nil {
		log.Printf("TestMain: cannot create temp dir: %v — integration tests will be skipped", err)
		return m.Run()
	}
	defer os.RemoveAll(tmpDir)

	moduleRoot, err := findModuleRoot()
	if err != nil {
		log.Printf("TestMain: cannot find module root: %v — integration tests will be skipped", err)
		return m.Run()
	}

	// Use ".exe" extension so the *.exe Glob in Scan finds it on all platforms.
	mockPluginBinary = filepath.Join(tmpDir, "mock-plugin.exe")
	cmd := exec.Command("go", "build", "-o", mockPluginBinary,
		"github.com/CCoupel/GhostDrive/plugins/testdata/mock-plugin")
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("TestMain: build mock plugin failed: %v\n%s\n— integration tests will be skipped", err, out)
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
		t.Skip("mock plugin binary not available — skipping integration test")
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
	// filepath.Glob on a non-existent dir returns nil, nil — no error.
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
// The file is not a valid plugin, so the result must be a "failed" entry —
// but the important thing is that the loader found and attempted to load it,
// proving the extensionless scan path is active.
func TestGRPCLoader_ScanLinuxExecutable(t *testing.T) {
	dir := t.TempDir()

	// Extensionless executable — should be picked up by the Linux scan.
	execFile := filepath.Join(dir, "myplugin")
	require.NoError(t, os.WriteFile(execFile, []byte("not a real plugin"), 0755))

	// Non-executable extensionless file — must be skipped.
	noExec := filepath.Join(dir, "readme")
	require.NoError(t, os.WriteFile(noExec, []byte("not executable"), 0644))

	// Extensionless empty file — must be skipped (size == 0).
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

	// A subdirectory with a name that has no extension — must be ignored.
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
	// A binary that is not a valid go-plugin — the handshake will fail.
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
func TestGRPCLoader_Watchdog_RestartOnCrash(t *testing.T) {
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

	// Watchdog detects exit (≤ 500 ms poll interval) + 10 ms delay + restart.
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

// TestGRPCLoader_Watchdog_MaxRetries removes the plugin binary after initial
// load, then kills the subprocess. The watchdog must fail all restart attempts
// and mark the plugin as "failed".
func TestGRPCLoader_Watchdog_MaxRetries(t *testing.T) {
	requireMockPlugin(t)

	dir := t.TempDir()
	binaryPath := copyMockPlugin(t, dir, "mock.exe")

	l := loader.NewGRPCLoaderWithOptions(loader.LoaderOptions{
		WatchdogDelays: fastDelays(),
	})
	require.NoError(t, l.Scan(dir))
	t.Cleanup(func() { _ = l.Shutdown() })

	// Confirm initial load succeeded.
	require.Eventually(t, func() bool {
		for _, info := range l.GetLoadedPlugins() {
			if info.Name == "mock" && info.Status == "loaded" {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond)

	// Delete the binary so all restart attempts fail immediately.
	require.NoError(t, os.Remove(binaryPath))

	// Kill the subprocess to trigger the watchdog.
	require.NoError(t, l.KillPluginProcess("mock"))

	// After len(WatchdogDelays)=3 consecutive failed restarts the plugin is
	// permanently marked "failed". Allow generous timeout because each watchdog
	// cycle polls for exit up to 500 ms before attempting restart.
	require.Eventually(t, func() bool {
		for _, info := range l.GetLoadedPlugins() {
			if info.Name == "mock" && info.Status == "failed" {
				return true
			}
		}
		return false
	}, 10*time.Second, 200*time.Millisecond,
		"plugin must be permanently 'failed' after max restart attempts")

	infos := l.GetLoadedPlugins()
	require.Len(t, infos, 1)
	assert.Equal(t, "failed", infos[0].Status)
	assert.NotEmpty(t, infos[0].Error, "failed entry must carry the restart error")
}

// TestGRPCLoader_Shutdown_KillsPlugin verifies that Shutdown terminates the
// plugin subprocess (client.Exited() becomes true) and clears all entries.
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

	// Save the go-plugin Client BEFORE Shutdown clears the entries map.
	client, ok := l.GetPluginClientForTest("mock")
	require.True(t, ok, "client must be accessible before Shutdown")
	assert.False(t, client.Exited(), "plugin process must be running before Shutdown")

	// Shutdown cancels watchdogs and kills all subprocesses.
	require.NoError(t, l.Shutdown())

	assert.True(t, client.Exited(), "plugin process must have exited after Shutdown")
	assert.Empty(t, l.GetLoadedPlugins(), "entries must be cleared after Shutdown")
}
