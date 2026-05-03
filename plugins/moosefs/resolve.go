package moosefs

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/CCoupel/GhostDrive/plugins/moosefs/internal/mfsclient"
)

// resolvePath walks the MooseFS node tree from RootNodeID to find the nodeID
// that corresponds to the given path.
//
// The path is formed by joining subDir (the backend-level sub-directory
// configured at Connect time) with relPath (the caller-supplied path).
//
//   - subDir = "/" and relPath = "a/b/c" → walks root → a → b → c
//   - subDir = "/data" and relPath = "a" → walks root → data → a
//
// Returns mfsclient.RootNodeID when the combined path is empty (i.e. "/"
// or ".").
// Returns plugins.ErrFileNotFound (wrapped) when any path component is missing.
func resolvePath(ctx context.Context, c *mfsclient.Client, subDir, relPath string) (uint32, error) {
	// Combine and clean the path.
	combined := path.Join(subDir, relPath)
	combined = strings.TrimPrefix(combined, "/")

	if combined == "" || combined == "." {
		return mfsclient.RootNodeID, nil
	}

	segments := strings.Split(combined, "/")
	nodeID := mfsclient.RootNodeID

	for _, seg := range segments {
		if seg == "" || seg == "." {
			continue
		}
		// Check context cancellation between levels.
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("moosefs: resolvePath %q: %w", combined, err)
		}

		entries, err := c.ReadDir(nodeID)
		if err != nil {
			return 0, fmt.Errorf("moosefs: resolvePath %q segment %q: %w", combined, seg, err)
		}

		found := false
		for _, e := range entries {
			if e.Name == seg {
				nodeID = e.NodeID
				found = true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("moosefs: resolvePath %q segment %q: %w", combined, seg, plugins.ErrFileNotFound)
		}
	}

	return nodeID, nil
}

// resolveParent is a convenience wrapper around resolvePath that resolves the
// parent directory of relPath and returns (parentNodeID, baseName, error).
//
// For a path like "dir/file.txt", it resolves "dir" and returns ("dir"'s nodeID, "file.txt", nil).
// For a top-level path like "file.txt", it returns (RootNodeID under subDir, "file.txt", nil).
func resolveParent(ctx context.Context, c *mfsclient.Client, subDir, relPath string) (uint32, string, error) {
	cleanPath := strings.TrimRight(relPath, "/")
	dir := path.Dir(cleanPath)
	base := path.Base(cleanPath)

	if base == "." || base == "/" {
		return 0, "", fmt.Errorf("moosefs: resolveParent %q: invalid path", relPath)
	}

	// When dir is "." it means the file is directly under subDir.
	if dir == "." || dir == "/" {
		dir = ""
	}

	parentID, err := resolvePath(ctx, c, subDir, dir)
	if err != nil {
		return 0, "", fmt.Errorf("moosefs: resolveParent %q parent: %w", relPath, err)
	}

	return parentID, base, nil
}
