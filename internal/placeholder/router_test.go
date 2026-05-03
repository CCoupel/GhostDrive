//go:build windows

package placeholder

import (
	"context"
	"testing"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winfsp/cgofuse/fuse"
)

// mockBackend is a minimal StorageBackend for routing tests.
type mockBackend struct{ plugins.StorageBackend }

func (m *mockBackend) List(_ context.Context, _ string) ([]plugins.FileInfo, error) {
	return nil, nil
}

func newRouterFS() *GhostFileSystem {
	return newGhostFileSystem([]MountedBackend{
		{ID: "b1", Name: "NAS", Backend: &mockBackend{}, Config: plugins.BackendConfig{ID: "b1", Name: "NAS"}},
	})
}

func TestRoute_Root_ReturnsBackend(t *testing.T) {
	fs := newRouterFS()
	r := fs.route("/")
	require.NotNil(t, r)
	assert.Equal(t, "/", r.relPath)
}

func TestRoute_DirectPath(t *testing.T) {
	fs := newRouterFS()
	r := fs.route("/NAS")
	require.NotNil(t, r)
	assert.Equal(t, "/NAS", r.relPath)
}

func TestRoute_SubPath(t *testing.T) {
	fs := newRouterFS()
	r := fs.route("/NAS/docs/file.txt")
	require.NotNil(t, r)
	assert.Equal(t, "/NAS/docs/file.txt", r.relPath)
}

func TestRoute_AnyPath_ReturnsSingleBackend(t *testing.T) {
	fs := newRouterFS()
	r := fs.route("/anything/path")
	require.NotNil(t, r, "single backend: any path must route")
	assert.Equal(t, "/anything/path", r.relPath)
}

func TestRoute_EmptyFileSystem_ReturnsNil(t *testing.T) {
	fs := newGhostFileSystem([]MountedBackend{})
	assert.Nil(t, fs.route("/anything"))
}

// ── Virtual desktop.ini ──────────────────────────────────────────────────────

func TestGhostFS_DesktopIni_Getattr(t *testing.T) {
	fs := newRouterFS()
	var stat fuse.Stat_t
	ret := fs.Getattr("/desktop.ini", &stat, 0)
	assert.Equal(t, 0, ret, "Getattr /desktop.ini must succeed")
	assert.Equal(t, uint32(fuse.S_IFREG|0444), stat.Mode)
	assert.Greater(t, stat.Size, int64(0))
}

func TestGhostFS_DesktopIni_Getattr_CaseInsensitive(t *testing.T) {
	fs := newRouterFS()
	var stat fuse.Stat_t
	ret := fs.Getattr("/Desktop.ini", &stat, 0)
	assert.Equal(t, 0, ret, "Getattr /Desktop.ini must be case-insensitive")
}

func TestGhostFS_Readdir_Root_ContainsVirtualFiles(t *testing.T) {
	fs := newRouterFS()
	var names []string
	fill := func(name string, _ *fuse.Stat_t, _ int64) bool {
		names = append(names, name)
		return true
	}
	ret := fs.Readdir("/", fill, 0, 0)
	assert.Equal(t, 0, ret)
	assert.Contains(t, names, "desktop.ini")
	assert.Contains(t, names, "ghostdrive.ico")
	// Backend name must no longer appear as a subfolder at the drive root.
	assert.NotContains(t, names, "NAS")
}

func TestGhostFS_DesktopIni_Content(t *testing.T) {
	fs := newRouterFS()
	content := string(fs.desktopIni)
	assert.Contains(t, content, "[.ShellClassInfo]")
	assert.Contains(t, content, "IconFile=ghostdrive.ico")
	assert.Contains(t, content, "IconIndex=0")
}

func TestGhostFS_DesktopIni_Open_And_Read(t *testing.T) {
	fs := newRouterFS()
	ret, fh := fs.Open("/desktop.ini", 0)
	require.Equal(t, 0, ret, "Open /desktop.ini must succeed")
	require.NotEqual(t, ^uint64(0), fh)

	buf := make([]byte, 512)
	n := fs.Read("/desktop.ini", buf, 0, fh)
	require.Greater(t, n, 0, "Read must return bytes")
	content := string(buf[:n])
	assert.Contains(t, content, "[.ShellClassInfo]")
	assert.Contains(t, content, "IconFile=ghostdrive.ico")

	fs.Release("/desktop.ini", fh)
}

func TestGhostFS_DriveIcon_Getattr(t *testing.T) {
	fs := newRouterFS()
	var stat fuse.Stat_t
	ret := fs.Getattr("/ghostdrive.ico", &stat, 0)
	assert.Equal(t, 0, ret, "Getattr /ghostdrive.ico must succeed")
	assert.Equal(t, uint32(fuse.S_IFREG|0444), stat.Mode)
	assert.Greater(t, stat.Size, int64(0), "icon must have non-zero size")
}

func TestGhostFS_DriveIcon_Open_And_Read(t *testing.T) {
	fs := newRouterFS()
	ret, fh := fs.Open("/ghostdrive.ico", 0)
	require.Equal(t, 0, ret, "Open /ghostdrive.ico must succeed")
	require.NotEqual(t, ^uint64(0), fh)

	// ICO magic: first 4 bytes are 00 00 01 00
	buf := make([]byte, 4)
	n := fs.Read("/ghostdrive.ico", buf, 0, fh)
	require.Equal(t, 4, n)
	assert.Equal(t, []byte{0x00, 0x00, 0x01, 0x00}, buf[:4], "must be a valid ICO file")

	fs.Release("/ghostdrive.ico", fh)
}
