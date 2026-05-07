//go:build integration

// Package moosefs provides integration tests that run against a real MooseFS
// cluster.  These tests are skipped by default and only run when the
// -mfs-addr flag is provided.
//
// Usage:
//
//	go test -tags integration -run TestInteg ./plugins/moosefs/... -mfs-addr 192.168.1.231:9421
package moosefs

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var mfsAddr = flag.String("mfs-addr", "", "real MooseFS master address host:port (e.g. 192.168.1.231:9421)")

func skipIfNoServer(t *testing.T) (host, portStr string) {
	t.Helper()
	if *mfsAddr == "" {
		t.Skip("set -mfs-addr to run integration tests against a real MooseFS cluster")
	}
	h, p, err := net.SplitHostPort(*mfsAddr)
	require.NoError(t, err, "invalid -mfs-addr format, expected host:port")
	return h, p
}

func newIntegBackend(t *testing.T) *Backend {
	t.Helper()
	host, portStr := skipIfNoServer(t)
	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"masterHost": host,
			"masterPort": portStr,
			"subDir":     "/",
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Disconnect() })
	return b
}

// TestInteg_Connect verifies that the backend connects and registers
// successfully against a real MooseFS master.
func TestInteg_Connect(t *testing.T) {
	host, portStr := skipIfNoServer(t)
	b := New()
	err := b.Connect(plugins.BackendConfig{
		Params: map[string]string{"masterHost": host, "masterPort": portStr},
	})
	require.NoError(t, err)
	assert.True(t, b.IsConnected())
	_ = b.Disconnect()
}

// TestInteg_GetAttr_root verifies GETATTR on the root node.
func TestInteg_GetAttr_root(t *testing.T) {
	b := newIntegBackend(t)
	fi, err := b.Stat(context.Background(), "/")
	require.NoError(t, err)
	assert.True(t, fi.IsDir)
}

// TestInteg_ReadDir_root verifies ReadDir on the root directory.
func TestInteg_ReadDir_root(t *testing.T) {
	b := newIntegBackend(t)
	entries, err := b.List(context.Background(), "/")
	require.NoError(t, err)
	t.Logf("root has %d entries", len(entries))
	// Root may be empty on a fresh cluster — just verify no error.
}

// TestInteg_StatFS verifies StatFS returns positive values.
func TestInteg_StatFS(t *testing.T) {
	b := newIntegBackend(t)
	free, total, err := b.GetQuota(context.Background())
	require.NoError(t, err)
	assert.Greater(t, total, int64(0), "total space must be positive")
	assert.GreaterOrEqual(t, free, int64(0), "free space must be non-negative")
	assert.LessOrEqual(t, free, total, "free <= total")
	t.Logf("StatFS: free=%d GB, total=%d GB", free>>30, total>>30)
}

// TestInteg_UploadDownload verifies a full Upload → Download → compare cycle.
func TestInteg_UploadDownload(t *testing.T) {
	b := newIntegBackend(t)
	ctx := context.Background()

	// Use a unique remote name to avoid conflicts with concurrent runs.
	remoteName := fmt.Sprintf("/ghostdrive-integ-%d.bin", time.Now().UnixNano())

	// Create local source file.
	content := []byte("GhostDrive integration test — " + remoteName)
	srcPath := filepath.Join(t.TempDir(), "upload_src.bin")
	require.NoError(t, os.WriteFile(srcPath, content, 0644))

	// Upload.
	require.NoError(t, b.Upload(ctx, srcPath, remoteName, nil))
	t.Cleanup(func() { _ = b.Delete(ctx, remoteName) })

	// Download.
	dstPath := filepath.Join(t.TempDir(), "download_dst.bin")
	require.NoError(t, b.Download(ctx, remoteName, dstPath, nil))

	// Compare.
	got, err := os.ReadFile(dstPath)
	require.NoError(t, err)
	assert.Equal(t, content, got, "downloaded content must match uploaded content")
}

// TestInteg_CreateDir verifies directory creation and listing.
func TestInteg_CreateDir(t *testing.T) {
	b := newIntegBackend(t)
	ctx := context.Background()

	dirName := fmt.Sprintf("/ghostdrive-integ-dir-%d", time.Now().UnixNano())

	require.NoError(t, b.CreateDir(ctx, dirName))
	t.Cleanup(func() { _ = b.Delete(ctx, dirName) })

	fi, err := b.Stat(ctx, dirName)
	require.NoError(t, err)
	assert.True(t, fi.IsDir)
}
