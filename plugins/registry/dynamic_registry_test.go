package registry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import static plugins so they register themselves via init().
	_ "github.com/CCoupel/GhostDrive/plugins/local"
	_ "github.com/CCoupel/GhostDrive/plugins/moosefs"
	_ "github.com/CCoupel/GhostDrive/plugins/webdav"

	"github.com/CCoupel/GhostDrive/plugins/registry"
)

// TestDynamicRegistry_StartEmpty verifies that starting the registry against
// an empty (or non-existent) plugins directory does not return an error.
func TestDynamicRegistry_StartEmpty(t *testing.T) {
	dir := t.TempDir() // empty directory, no .exe files
	r := registry.NewDynamicRegistry(dir)

	err := r.Start()
	assert.NoError(t, err)

	_ = r.Stop()
}

// TestDynamicRegistry_ListAvailablePlugins_IncludesStatic verifies that the
// list always contains at least the three static plugins: local, webdav, moosefs.
func TestDynamicRegistry_ListAvailablePlugins_IncludesStatic(t *testing.T) {
	dir := t.TempDir()
	r := registry.NewDynamicRegistry(dir)
	require.NoError(t, r.Start())
	defer r.Stop()

	infos := r.ListAvailablePlugins()
	names := make(map[string]bool, len(infos))
	for _, info := range infos {
		names[info.Name] = true
	}

	assert.True(t, names["local"], "static plugin 'local' must be present")
	assert.True(t, names["webdav"], "static plugin 'webdav' must be present")
	assert.True(t, names["moosefs"], "static plugin 'moosefs' must be present")
}

// TestDynamicRegistry_ListDynamicPlugins_EmptyWhenNoBinaries verifies that
// the dynamic-only list is empty when no plugin binaries exist.
func TestDynamicRegistry_ListDynamicPlugins_EmptyWhenNoBinaries(t *testing.T) {
	dir := t.TempDir()
	r := registry.NewDynamicRegistry(dir)
	require.NoError(t, r.Start())
	defer r.Stop()

	dynamic := r.ListDynamicPlugins()
	assert.Empty(t, dynamic)
}

// TestDynamicRegistry_Reload verifies that Reload can be called without error
// and that the plugin list remains consistent after reload.
func TestDynamicRegistry_Reload(t *testing.T) {
	dir := t.TempDir()
	r := registry.NewDynamicRegistry(dir)
	require.NoError(t, r.Start())

	err := r.Reload()
	assert.NoError(t, err)

	infos := r.ListAvailablePlugins()
	names := make(map[string]bool, len(infos))
	for _, info := range infos {
		names[info.Name] = true
	}
	assert.True(t, names["local"], "static plugins must survive a Reload")

	_ = r.Stop()
}

// TestDynamicRegistry_StopIdempotent verifies that calling Stop multiple times
// does not panic or return an error.
func TestDynamicRegistry_StopIdempotent(t *testing.T) {
	dir := t.TempDir()
	r := registry.NewDynamicRegistry(dir)
	require.NoError(t, r.Start())

	assert.NoError(t, r.Stop())
	assert.NoError(t, r.Stop(), "second Stop must be idempotent")
}
