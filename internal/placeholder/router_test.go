//go:build windows

package placeholder

import (
	"context"
	"fmt"
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
	}, nil)
}

// ── v2.0 routing tests ────────────────────────────────────────────────────────
// In v2.0 the drive root is a virtual listing; every backend is exposed under
// its own named sub-folder (/<BackendName>/...).

// TestRoute_Root_ReturnsNil verifies that route("/") returns nil because the
// virtual root is handled by callers (Getattr / Readdir) before route() is called.
func TestRoute_Root_ReturnsNil(t *testing.T) {
	fs := newRouterFS()
	assert.Nil(t, fs.route("/"), "route('/') must return nil in v2.0 (virtual root)")
}

// TestRoute_BackendRoot verifies that route("/<name>") maps to the backend with
// relPath="/", stripping the backend-name prefix.
func TestRoute_BackendRoot(t *testing.T) {
	fs := newRouterFS()
	r := fs.route("/NAS")
	require.NotNil(t, r)
	assert.Equal(t, "/", r.relPath, "route('/<name>') must yield relPath='/'")
}

// TestRoute_BackendSubPath verifies that route("/<name>/sub/path") strips the
// backend name and returns the remainder as relPath.
func TestRoute_BackendSubPath(t *testing.T) {
	fs := newRouterFS()
	r := fs.route("/NAS/docs/file.txt")
	require.NotNil(t, r)
	assert.Equal(t, "/docs/file.txt", r.relPath, "sub-path must strip the backend name prefix")
}

// TestRoute_UnknownBackend verifies that an unrecognised first path segment
// returns nil (→ ENOENT to FUSE).
func TestRoute_UnknownBackend(t *testing.T) {
	fs := newRouterFS()
	assert.Nil(t, fs.route("/unknown/path"), "unknown backend name must return nil")
	assert.Nil(t, fs.route("/anything"), "unknown backend name (root only) must return nil")
}

// TestRoute_MultiBackend_Dispatch verifies that a two-backend filesystem routes
// each path to the correct backend instance.
func TestRoute_MultiBackend_Dispatch(t *testing.T) {
	b1 := &mockBackend{}
	b2 := &mockBackend{}
	fs := newGhostFileSystem([]MountedBackend{
		{ID: "n1", Name: "NAS1", Backend: b1, Config: plugins.BackendConfig{ID: "n1", Name: "NAS1"}},
		{ID: "n2", Name: "WebDAV", Backend: b2, Config: plugins.BackendConfig{ID: "n2", Name: "WebDAV"}},
	}, nil)

	r1 := fs.route("/NAS1/data/file.txt")
	require.NotNil(t, r1, "NAS1 path must be routed")
	assert.Equal(t, b1, r1.backend, "/NAS1/... must route to b1")
	assert.Equal(t, "/data/file.txt", r1.relPath)

	r2 := fs.route("/WebDAV/docs")
	require.NotNil(t, r2, "WebDAV path must be routed")
	assert.Equal(t, b2, r2.backend, "/WebDAV/... must route to b2")
	assert.Equal(t, "/docs", r2.relPath)
}

// TestRoute_CaseInsensitive verifies that backend-name matching is case-insensitive.
func TestRoute_CaseInsensitive(t *testing.T) {
	fs := newRouterFS()
	r := fs.route("/nas/file.txt") // "nas" vs registered "NAS"
	require.NotNil(t, r, "route must be case-insensitive for backend name")
	assert.Equal(t, "/file.txt", r.relPath)
}

func TestRoute_EmptyFileSystem_ReturnsNil(t *testing.T) {
	fs := newGhostFileSystem([]MountedBackend{}, nil)
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

// TestGhostFS_Readdir_Root_ListsBackendDirs verifies that Readdir("/") returns:
//   - virtual drive files (desktop.ini, ghostdrive.ico)
//   - one S_IFDIR entry per registered backend (v2.0)
func TestGhostFS_Readdir_Root_ListsBackendDirs(t *testing.T) {
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
	// v2.0: backend name must appear as a virtual sub-directory at the drive root.
	assert.Contains(t, names, "NAS", "backend name must be listed as a subfolder in v2.0")
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

// ── Getattr ErrFileNotFound → ENOENT (non-regression #97) ────────────────────

// statErrBackend is a minimal StorageBackend whose Stat always returns a
// configurable error, used to verify Getattr error mapping.
type statErrBackend struct {
	plugins.StorageBackend
	statErr error
}

func (m *statErrBackend) Stat(_ context.Context, _ string) (*plugins.FileInfo, error) {
	return nil, m.statErr
}

// TestGetattr_BackendVirtualDir verifies that Getattr("/<backendName>") returns
// S_IFDIR without calling any backend — it's a virtual directory in v2.0.
func TestGetattr_BackendVirtualDir(t *testing.T) {
	fs := newRouterFS()
	var stat fuse.Stat_t
	ret := fs.Getattr("/NAS", &stat, 0)
	assert.Equal(t, 0, ret, "Getattr '/<backendName>' must succeed")
	assert.Equal(t, uint32(fuse.S_IFDIR|0755), stat.Mode, "backend virtual dir must be S_IFDIR|0755")
}

// TestGetattr_UnknownFirstSegment_ReturnsENOENT verifies that Getattr of an
// unknown first-level path component returns ENOENT.
func TestGetattr_UnknownFirstSegment_ReturnsENOENT(t *testing.T) {
	fs := newRouterFS()
	var stat fuse.Stat_t
	ret := fs.Getattr("/unknown", &stat, 0)
	assert.Equal(t, -fuse.ENOENT, ret, "unknown first segment must return ENOENT")
}

func TestGhostFS_Getattr_ErrFileNotFound_ReturnsENOENT(t *testing.T) {
	// Direct ErrFileNotFound must map to -ENOENT, not -EIO.
	b := &statErrBackend{statErr: plugins.ErrFileNotFound}
	fs := newGhostFileSystem([]MountedBackend{
		{ID: "b1", Name: "NAS", Backend: b, Config: plugins.BackendConfig{ID: "b1", Name: "NAS"}},
	}, nil)
	var stat fuse.Stat_t
	ret := fs.Getattr("/NAS/missing.txt", &stat, 0)
	assert.Equal(t, -fuse.ENOENT, ret, "ErrFileNotFound must return -ENOENT (fix for issue #97)")
}

func TestGhostFS_Getattr_WrappedErrFileNotFound_ReturnsENOENT(t *testing.T) {
	// Wrapped ErrFileNotFound (errors.Is chain) must also map to -ENOENT.
	b := &statErrBackend{statErr: fmt.Errorf("stat failed: %w", plugins.ErrFileNotFound)}
	fs := newGhostFileSystem([]MountedBackend{
		{ID: "b1", Name: "NAS", Backend: b, Config: plugins.BackendConfig{ID: "b1", Name: "NAS"}},
	}, nil)
	var stat fuse.Stat_t
	ret := fs.Getattr("/NAS/missing.txt", &stat, 0)
	assert.Equal(t, -fuse.ENOENT, ret, "wrapped ErrFileNotFound must return -ENOENT via errors.Is chain")
}
