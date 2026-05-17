//go:build windows

package placeholder

import (
	"strings"

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

// route resolves a FUSE path to the backend that owns it and the relative path
// within that backend (v2.0 multi-backend unified drive).
//
// The first segment of the path identifies the backend by name (case-insensitive):
//
//	"/MonNAS"           → {backend: MonNAS, relPath: "/"}
//	"/MonNAS/docs/f.txt" → {backend: MonNAS, relPath: "/docs/f.txt"}
//	"/WebDAV/"          → {backend: WebDAV, relPath: "/"}
//
// Returns nil when no backend matches the first segment (→ ENOENT to FUSE).
// Callers (Getattr, Readdir) handle the virtual root "/" and virtual files
// (desktop.ini, ghostdrive.ico) BEFORE invoking route, so this function is
// never called for "/" or those virtual paths.
func (fs *GhostFileSystem) route(path string) *routeResult {
	if len(fs.backends) == 0 {
		return nil
	}

	// Strip leading slash and split into at most 2 parts:
	// [backendName, rest-of-path].
	trimmed := strings.TrimLeft(path, "/")
	if trimmed == "" {
		// Pure root — handled by callers before route() is invoked.
		return nil
	}

	parts := strings.SplitN(trimmed, "/", 2)
	backendName := parts[0]

	var relPath string
	if len(parts) == 2 && parts[1] != "" {
		relPath = "/" + parts[1]
	} else {
		relPath = "/"
	}

	// Search for the backend whose Name matches backendName (case-insensitive).
	for _, mb := range fs.backends {
		if strings.EqualFold(mb.Name, backendName) {
			return &routeResult{
				backend: mb.Backend,
				config:  mb.Config,
				relPath: relPath,
			}
		}
	}

	// No backend matches the first path segment.
	return nil
}
