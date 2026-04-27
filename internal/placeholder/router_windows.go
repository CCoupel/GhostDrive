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

// route parses a FUSE path of the form "/<BackendName>[/...]" and resolves it
// to the matching backend.  Returns nil for the root ("/") or when no backend
// matches the first path segment.
func (fs *GhostFileSystem) route(path string) *routeResult {
	if path == "/" {
		return nil
	}

	// Strip the leading "/" and split into (backendName, rest).
	trimmed := strings.TrimPrefix(path, "/")
	idx := strings.IndexByte(trimmed, '/')
	var backendName, rest string
	if idx < 0 {
		backendName = trimmed
		rest = "/"
	} else {
		backendName = trimmed[:idx]
		rest = trimmed[idx:] // already has leading "/"
	}
	if rest == "" {
		rest = "/"
	}

	for _, mb := range fs.backends {
		if strings.EqualFold(mb.Name, backendName) {
			return &routeResult{
				backend: mb.Backend,
				config:  mb.Config,
				relPath: rest,
			}
		}
	}
	return nil
}
