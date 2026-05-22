package cfapi

// path_helpers_test.go — cross-platform specification tests for the
// volumePrefix / resolveNormalizedPath helpers introduced in 7286e18.
//
// Both functions live in provider.go (//go:build windows) so they cannot be
// called directly on Linux.  Instead we mirror their pure-Go logic here and
// test the contract.  If the production implementation diverges from this
// specification the tests must be updated together with the code.
//
// Production code under test (provider.go, windows):
//
//   func volumePrefix(syncRoot string) string {
//       if len(syncRoot) >= 2 && syncRoot[1] == ':' {
//           return syncRoot[:2]
//       }
//       return ""
//   }
//
//   func resolveNormalizedPath(info *C.CF_CALLBACK_INFO, syncRoot string) string {
//       path := goPathFromCallbackInfo(info)  // PCWSTR → UTF-8 (CGO)
//       if len(path) > 0 && path[0] == '\\' && (len(path) < 2 || path[1] != '\\') {
//           path = volumePrefix(syncRoot) + path
//       }
//       return path
//   }

import "testing"

// volumePrefixSpec mirrors the production volumePrefix logic for cross-platform
// specification testing.
func volumePrefixSpec(syncRoot string) string {
	if len(syncRoot) >= 2 && syncRoot[1] == ':' {
		return syncRoot[:2]
	}
	return ""
}

// resolvePathSpec applies the volume-relative path resolution logic without CGO.
// rawPath is the already-decoded UTF-8 path (as returned by goPathFromCallbackInfo).
// It mirrors the Go layer of resolveNormalizedPath.
func resolvePathSpec(rawPath, syncRoot string) string {
	if len(rawPath) > 0 && rawPath[0] == '\\' && (len(rawPath) < 2 || rawPath[1] != '\\') {
		rawPath = volumePrefixSpec(syncRoot) + rawPath
	}
	return rawPath
}

// TestRegression133_VolumePrefix verifies that volumePrefix extracts the
// drive-letter prefix correctly — and returns "" when there is none.
func TestRegression133_VolumePrefix(t *testing.T) {
	cases := []struct {
		syncRoot string
		want     string
	}{
		{`C:\GhostDrive\MFS`, "C:"},  // canonical Windows path
		{`D:\Users\sync`, "D:"},      // different drive
		{`c:\lowercase`, "c:"},       // lowercase drive letter is preserved
		{"/mnt/ghd", ""},             // Linux path — no drive letter
		{`\\server\share`, ""},       // UNC path — no drive letter
		{"", ""},                     // empty string
		{"C", ""},                    // too short (no colon)
	}
	for _, tt := range cases {
		got := volumePrefixSpec(tt.syncRoot)
		if got != tt.want {
			t.Errorf("volumePrefix(%q) = %q, want %q", tt.syncRoot, got, tt.want)
		}
	}
}

// TestRegression133_ResolveNormalizedPath_VolumeRelative is the core regression
// test: CF API NormalizedPath is volume-relative ("\GhostDrive\MFS"), not
// absolute ("C:\GhostDrive\MFS"). resolveNormalizedPath must prepend the drive
// letter so localToRemote() can match the sync root correctly.
//
// Before fix: "\GhostDrive\MFS" vs syncRoot "C:\GhostDrive\MFS"
//             → strings.HasPrefix failed → "outside sync root" error → List("") → wrong results.
// After fix:  "C:" + "\GhostDrive\MFS" = "C:\GhostDrive\MFS" → match OK.
func TestRegression133_ResolveNormalizedPath_VolumeRelative(t *testing.T) {
	syncRoot := `C:\GhostDrive\MFS`

	cases := []struct {
		name    string
		rawPath string
		want    string
	}{
		{
			"sync root itself (volume-relative)",
			`\GhostDrive\MFS`,
			`C:\GhostDrive\MFS`,
		},
		{
			"subfolder file (volume-relative)",
			`\GhostDrive\MFS\subfolder\file.txt`,
			`C:\GhostDrive\MFS\subfolder\file.txt`,
		},
		{
			"deep nested path (volume-relative)",
			`\GhostDrive\MFS\a\b\c\deep.txt`,
			`C:\GhostDrive\MFS\a\b\c\deep.txt`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePathSpec(tt.rawPath, syncRoot)
			if got != tt.want {
				t.Errorf("resolveNormalizedPath(%q, %q) = %q, want %q",
					tt.rawPath, syncRoot, got, tt.want)
			}
		})
	}
}

// TestRegression133_ResolveNormalizedPath_AlreadyAbsolute verifies that a path
// that already contains a drive letter is NOT double-prefixed.
// Condition: path[0] != '\\' → the if-guard in resolveNormalizedPath is skipped.
func TestRegression133_ResolveNormalizedPath_AlreadyAbsolute(t *testing.T) {
	syncRoot := `C:\GhostDrive\MFS`

	cases := []struct {
		name    string
		rawPath string
	}{
		{"absolute root", `C:\GhostDrive\MFS`},
		{"absolute file", `C:\GhostDrive\MFS\file.txt`},
		{"different drive (already absolute)", `D:\other\path`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePathSpec(tt.rawPath, syncRoot)
			if got != tt.rawPath {
				t.Errorf("already-absolute path got mutated: resolveNormalizedPath(%q) = %q, want %q (no double prefix)",
					tt.rawPath, got, tt.rawPath)
			}
		})
	}
}

// TestRegression133_ResolveNormalizedPath_UNC verifies that UNC paths
// (\\server\share) are NOT prefixed — they start with "\\" (two backslashes)
// which the guard explicitly rejects.
func TestRegression133_ResolveNormalizedPath_UNC(t *testing.T) {
	syncRoot := `C:\GhostDrive\MFS`

	cases := []struct {
		name    string
		rawPath string
	}{
		{"UNC share root", `\\server\share`},
		{"UNC file", `\\server\share\folder\file.txt`},
		{"UNC with IP", `\\192.168.1.1\dav`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePathSpec(tt.rawPath, syncRoot)
			if got != tt.rawPath {
				t.Errorf("UNC path got mutated: resolveNormalizedPath(%q) = %q, want %q (must not prefix UNC)",
					tt.rawPath, got, tt.rawPath)
			}
		})
	}
}

// TestRegression133_ResolveNormalizedPath_NoSyncRoot verifies that when the
// sync root has no drive letter (Linux mount, or empty string), a
// volume-relative path is returned unchanged — volumePrefix returns "" and
// "" + path == path.
func TestRegression133_ResolveNormalizedPath_NoSyncRoot(t *testing.T) {
	cases := []struct {
		name     string
		syncRoot string
		rawPath  string
	}{
		{"linux syncRoot", "/mnt/GhostDrive", `\GhostDrive\MFS`},
		{"empty syncRoot", "", `\some\path`},
		{"UNC syncRoot", `\\nas\share`, `\GhostDrive\MFS`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePathSpec(tt.rawPath, tt.syncRoot)
			if got != tt.rawPath {
				t.Errorf("resolveNormalizedPath(%q, syncRoot=%q) = %q, want unchanged %q",
					tt.rawPath, tt.syncRoot, got, tt.rawPath)
			}
		})
	}
}

// TestRegression133_ResolveNormalizedPath_EdgeCases covers empty and
// root-only inputs that must not panic.
func TestRegression133_ResolveNormalizedPath_EdgeCases(t *testing.T) {
	syncRoot := `C:\GhostDrive\MFS`

	cases := []struct {
		name    string
		rawPath string
		want    string
	}{
		{"empty path", "", ""},
		{"single backslash (volume root)", `\`, `C:\`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePathSpec(tt.rawPath, syncRoot)
			if got != tt.want {
				t.Errorf("resolveNormalizedPath(%q, %q) = %q, want %q",
					tt.rawPath, syncRoot, got, tt.want)
			}
		})
	}
}
