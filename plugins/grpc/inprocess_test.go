// Package grpc_test — tests for ServeInProcess (inprocess.go).
//
// ServeInProcess starts an in-process gRPC server backed by a real
// StorageBackend and returns a GRPCBackend client wired to it via a bufconn
// listener.  These tests validate the full end-to-end path for static plugins
// that are served in-process (e.g. the "local" plugin in app.Startup).
//
// TODO: ce fichier attend inprocess.go du dev-backend.
// Il compilera et sera exécutable dès que ServeInProcess sera implémenté dans
// plugins/grpc/inprocess.go.
package grpc_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcbridge "github.com/CCoupel/GhostDrive/plugins/grpc"
	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/CCoupel/GhostDrive/plugins/local"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// newLocalBackend creates a local.Backend connected to a fresh temp directory.
// The caller is responsible for calling Disconnect when done.
func newLocalBackend(t *testing.T) (*local.Backend, string) {
	t.Helper()
	dir := t.TempDir()
	b := local.New()
	require.NoError(t, b.Connect(plugins.BackendConfig{
		Params: map[string]string{"rootPath": dir},
	}))
	return b, dir
}

// ─── ServeInProcess: basic lifecycle ─────────────────────────────────────────

// TestServeInProcess_Describe verifies that the GRPCBackend returned by
// ServeInProcess correctly delegates Describe() through the gRPC bridge.
// The descriptor must reflect the underlying local plugin.
func TestServeInProcess_Describe(t *testing.T) {
	impl := local.New()

	backend, cleanup, err := grpcbridge.ServeInProcess(impl)
	require.NoError(t, err, "ServeInProcess must not return an error")
	require.NotNil(t, backend, "ServeInProcess must return a non-nil GRPCBackend")
	require.NotNil(t, cleanup, "ServeInProcess must return a non-nil cleanup function")
	defer cleanup()

	d := backend.Describe()

	assert.Equal(t, "local", d.Type,
		"Describe().Type must equal \"local\" after ServeInProcess(local.New())")
	assert.NotEmpty(t, d.DisplayName,
		"Describe().DisplayName must not be empty")
	assert.GreaterOrEqual(t, len(d.Params), 1,
		"Describe().Params must contain at least one ParamSpec")
}

// TestServeInProcess_Name verifies that the GRPCBackend returned by
// ServeInProcess reports the correct plugin name via the Name() RPC.
func TestServeInProcess_Name(t *testing.T) {
	impl := local.New()

	backend, cleanup, err := grpcbridge.ServeInProcess(impl)
	require.NoError(t, err)
	defer cleanup()

	assert.Equal(t, "local", backend.Name(),
		"Name() must return \"local\" for a ServeInProcess(local.New()) backend")
}

// TestServeInProcess_ConnectAndIsConnected verifies the Connect → IsConnected
// lifecycle through the in-process gRPC bridge.
func TestServeInProcess_ConnectAndIsConnected(t *testing.T) {
	dir := t.TempDir()
	impl := local.New()

	backend, cleanup, err := grpcbridge.ServeInProcess(impl)
	require.NoError(t, err)
	defer cleanup()

	// Before Connect: IsConnected must return false.
	assert.False(t, backend.IsConnected(),
		"IsConnected must be false before Connect is called")

	// Connect with a valid rootPath.
	connectErr := backend.Connect(plugins.BackendConfig{
		Params: map[string]string{"rootPath": dir},
	})
	require.NoError(t, connectErr, "Connect must succeed with a valid rootPath")

	assert.True(t, backend.IsConnected(),
		"IsConnected must be true after a successful Connect")
}

// TestServeInProcess_ListEmpty verifies that List on the root of a freshly
// created temp directory returns an empty (non-nil) slice without error.
func TestServeInProcess_ListEmpty(t *testing.T) {
	dir := t.TempDir()
	impl := local.New()

	backend, cleanup, err := grpcbridge.ServeInProcess(impl)
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, backend.Connect(plugins.BackendConfig{
		Params: map[string]string{"rootPath": dir},
	}))

	entries, listErr := backend.List(context.Background(), "/")
	require.NoError(t, listErr, "List must not return an error on an empty directory")
	assert.NotNil(t, entries,
		"List must return a non-nil slice (not nil) even when the directory is empty")
	assert.Empty(t, entries,
		"List must return an empty slice for a freshly created directory")
}

// TestServeInProcess_ListWithFiles verifies that List returns file entries
// that were created inside the rootPath directory.
func TestServeInProcess_ListWithFiles(t *testing.T) {
	dir := t.TempDir()
	impl := local.New()

	// Create a couple of files in the temp dir before connecting.
	for _, name := range []string{"alpha.txt", "beta.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644))
	}

	backend, cleanup, err := grpcbridge.ServeInProcess(impl)
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, backend.Connect(plugins.BackendConfig{
		Params: map[string]string{"rootPath": dir},
	}))

	entries, listErr := backend.List(context.Background(), "/")
	require.NoError(t, listErr)
	assert.Len(t, entries, 2,
		"List must return all files present in the rootPath directory")
}

// ─── ServeInProcess: cleanup ──────────────────────────────────────────────────

// TestServeInProcess_CleanupIdempotent verifies that the cleanup function
// can be called twice without panicking.
// This is important because App.Shutdown may call cleanup even if a previous
// shutdown attempt partially ran.
func TestServeInProcess_CleanupIdempotent(t *testing.T) {
	impl := local.New()

	_, cleanup, err := grpcbridge.ServeInProcess(impl)
	require.NoError(t, err)
	require.NotNil(t, cleanup)

	assert.NotPanics(t, func() {
		cleanup()
	}, "first cleanup() call must not panic")

	assert.NotPanics(t, func() {
		cleanup()
	}, "second cleanup() call must not panic")
}

// ─── ServeInProcess: isolation ────────────────────────────────────────────────

// TestServeInProcess_TwoInstancesAreIndependent verifies that two calls to
// ServeInProcess return independent (server, listener, client) triples.
// Connecting one backend must not affect the state of the other.
func TestServeInProcess_TwoInstancesAreIndependent(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	// Write a file only in dirA.
	require.NoError(t, os.WriteFile(filepath.Join(dirA, "only-in-a.txt"), []byte("a"), 0644))

	implA := local.New()
	implB := local.New()

	backendA, cleanupA, errA := grpcbridge.ServeInProcess(implA)
	require.NoError(t, errA)
	defer cleanupA()

	backendB, cleanupB, errB := grpcbridge.ServeInProcess(implB)
	require.NoError(t, errB)
	defer cleanupB()

	// Connect each backend to its own directory.
	require.NoError(t, backendA.Connect(plugins.BackendConfig{
		Params: map[string]string{"rootPath": dirA},
	}))
	require.NoError(t, backendB.Connect(plugins.BackendConfig{
		Params: map[string]string{"rootPath": dirB},
	}))

	// backendA should see one file; backendB should see none.
	entriesA, errListA := backendA.List(context.Background(), "/")
	require.NoError(t, errListA)
	assert.Len(t, entriesA, 1,
		"backendA must see exactly one file (only-in-a.txt)")

	entriesB, errListB := backendB.List(context.Background(), "/")
	require.NoError(t, errListB)
	assert.Empty(t, entriesB,
		"backendB must see no files (different rootPath from backendA)")

	// Stopping backendA must not affect backendB.
	cleanupA()
	cleanupA = func() {} // prevent double-call by defer

	// backendB should still be operational.
	assert.True(t, backendB.IsConnected(),
		"backendB must still be connected after cleanupA() is called")
}
