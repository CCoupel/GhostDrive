package loader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CCoupel/GhostDrive/plugins/loader"
)

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
