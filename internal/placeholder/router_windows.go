//go:build windows

package placeholder

import (
	"github.com/CCoupel/GhostDrive/plugins"
)

// routeResult holds the backend resolved from a FUSE path along with the
// relative path that should be forwarded to that backend.
type routeResult struct {
	backend plugins.StorageBackend
	config  plugins.BackendConfig
	// relPath is the path within the backend, always starts with "/".
	relPath string
}

// route resolves a FUSE path to the single backend mounted on this drive
// (per-backend drives, v1.1.x+).  The path is forwarded as-is: "/" routes to
// the backend root, "/foo/bar" routes to that path within the backend.
// Returns nil when no backend is registered.
// Callers (Getattr, Readdir) handle the virtual root "/" and virtual files
// before invoking route, so this function never needs to return nil for "/".
func (fs *GhostFileSystem) route(path string) *routeResult {
	if len(fs.backends) == 0 {
		return nil
	}
	mb := fs.backends[0]
	return &routeResult{
		backend: mb.Backend,
		config:  mb.Config,
		relPath: path,
	}
}
