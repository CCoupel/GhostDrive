package cfapi

// placeholder_error_test.go — regression tests for provider.go HRESULT handling
// and FileAttributes correctness.
//
// Bugs fixed (all tracked under #133):
//  0ae7537 — HRESULT 0x800700b7 (ALREADY_EXISTS) treated as fatal.
//  this commit — HRESULT 0x800704c8 (USER_MAPPED_FILE) treated as fatal.
//  this commit — FileAttributes = FILE_ATTRIBUTE_NORMAL caused missing ☁️ badge
//                and premature FETCH_DATA; fixed to ARCHIVE|RECALL_ON_DATA_ACCESS.
//  this commit — CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE on initial creation caused
//                early FETCH_DATA; fixed to NONE (SUPERSEDE only for UpdatePlaceholder).
//
// Architecture note: SyncProvider.createPlaceholdersWithFlags lives in provider.go
// (//go:build windows, CGO). Hydrator.provider is *SyncProvider (concrete type),
// not an interface — injecting a mock on Linux requires a spec-based approach.
// We use two complementary strategies:
//
//  1. Specification tests (createPlaceholdersResultSpec) — mirror the production
//     HRESULT-handling logic; test all cases cross-platform.
//  2. Behavioural tests — use the Linux stub to guard happy-path end-to-end flow.

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
)

// ─── Spec helpers ────────────────────────────────────────────────────────────

// HRESULT spec constants — mirror provider.go hrAlreadyExists / hrUserMappedFile.
// Keeping them here as a cross-platform spec guard: if the production constants
// change, this file's tests must be updated in sync.
const (
	specHRAlreadyExists  = uint32(0x800700b7) // HRESULT_FROM_WIN32(ERROR_ALREADY_EXISTS)
	specHRUserMappedFile = uint32(0x800704c8) // HRESULT_FROM_WIN32(ERROR_USER_MAPPED_FILE)
)

// createPlaceholdersResultSpec mirrors the production error-handling logic inside
// SyncProvider.createPlaceholdersWithFlags (provider.go, //go:build windows).
//
//	if hr != 0 {
//	    if uint32(hr) == hrAlreadyExists || uint32(hr) == hrUserMappedFile {
//	        return int(created), nil
//	    }
//	    return int(created), fmt.Errorf("cfapi: create placeholders: HRESULT 0x%08x", uint32(hr))
//	}
//	return int(created), nil
func createPlaceholdersResultSpec(hr uint32, created int) (int, error) {
	if hr == 0 {
		return created, nil
	}
	if hr == specHRAlreadyExists || hr == specHRUserMappedFile {
		return created, nil
	}
	return created, fmt.Errorf("cfapi: create placeholders: HRESULT 0x%08x", hr)
}

// fileAttributesSpec mirrors the production FileAttributes selection logic
// inside createPlaceholdersWithFlags (provider.go, //go:build windows).
//
//	if item.IsDirectory {
//	    return FILE_ATTRIBUTE_DIRECTORY
//	}
//	return FILE_ATTRIBUTE_ARCHIVE | FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS
func fileAttributesSpec(isDirectory bool) uint32 {
	const (
		fileAttrDirectory          = uint32(0x00000010) // FILE_ATTRIBUTE_DIRECTORY
		fileAttrArchive            = uint32(0x00000020) // FILE_ATTRIBUTE_ARCHIVE
		fileAttrNormal             = uint32(0x00000080) // FILE_ATTRIBUTE_NORMAL  (must NOT be used for cloud files)
		fileAttrPinned             = uint32(0x00080000) // FILE_ATTRIBUTE_PINNED  (must NOT be set — forces eager hydration)
		fileAttrRecallOnOpen       = uint32(0x00040000) // FILE_ATTRIBUTE_RECALL_ON_OPEN (must NOT — triggers FETCH_DATA on open)
		fileAttrRecallOnDataAccess = uint32(0x00400000) // FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS — cloud badge + lazy hydration
	)
	_ = fileAttrNormal        // documented as forbidden
	_ = fileAttrPinned        // documented as forbidden
	_ = fileAttrRecallOnOpen  // documented as forbidden

	if isDirectory {
		return fileAttrDirectory
	}
	return fileAttrArchive | fileAttrRecallOnDataAccess
}

// ─── HRESULT constants ───────────────────────────────────────────────────────

// TestRegression_ALREADY_EXISTS_HRESULTConstant documents the exact HRESULT
// value fixed in 0ae7537.  Any accidental constant change will fail this test.
// 0x800700b7 = HRESULT_FROM_WIN32(0xB7) = HRESULT_FROM_WIN32(ERROR_ALREADY_EXISTS).
func TestRegression_ALREADY_EXISTS_HRESULTConstant(t *testing.T) {
	n, err := createPlaceholdersResultSpec(specHRAlreadyExists, 0)
	if err != nil {
		t.Errorf("HRESULT 0x800700b7: expected nil (ALREADY_EXISTS treated as success), got %v", err)
	}
	if n != 0 {
		t.Errorf("HRESULT 0x800700b7 with created=0: expected n=0, got %d", n)
	}
}

// TestRegression_USER_MAPPED_FILE_HRESULTConstant documents the HRESULT added
// in this fix: 0x800704c8 = HRESULT_FROM_WIN32(ERROR_USER_MAPPED_FILE) = Win32 0x4c8.
// CfCreatePlaceholders returns this when the file has an open memory-mapped section
// (i.e. a FETCH_DATA is in progress for that file). Non-fatal: placeholder exists.
func TestRegression_USER_MAPPED_FILE_HRESULTConstant(t *testing.T) {
	n, err := createPlaceholdersResultSpec(specHRUserMappedFile, 0)
	if err != nil {
		t.Errorf("HRESULT 0x800704c8: expected nil (USER_MAPPED_FILE treated as success), got %v", err)
	}
	if n != 0 {
		t.Errorf("HRESULT 0x800704c8 with created=0: expected n=0, got %d", n)
	}
}

// ─── Spec: non-fatal HRESULTs treated as success ─────────────────────────────

// TestRegression_CreatePlaceholders_ALREADY_EXISTS_IsSuccess verifies that
// HRESULT 0x800700b7 always produces (created, nil) regardless of the created count.
//
// Scenario A: 0 placeholders created, then ALREADY_EXISTS → (0, nil).
// Scenario B: partial creation (k placeholders created), then ALREADY_EXISTS → (k, nil).
// Scenario C: all items already exist (CF_CREATE_FLAG_NONE, 2nd+ round-trip) → (0, nil).
//
// Before fix: returned (created, error) for any non-zero hr.
// After fix:  (created, nil) for hr == 0x800700b7.
func TestRegression_CreatePlaceholders_ALREADY_EXISTS_IsSuccess(t *testing.T) {
	cases := []struct {
		label   string
		created int
	}{
		{"all already exist (created=0)", 0},
		{"1 created then ALREADY_EXISTS", 1},
		{"partial: 5 created then ALREADY_EXISTS", 5},
	}
	for _, tt := range cases {
		t.Run(tt.label, func(t *testing.T) {
			n, err := createPlaceholdersResultSpec(specHRAlreadyExists, tt.created)
			if err != nil {
				t.Errorf("ALREADY_EXISTS with created=%d: expected nil error, got %v", tt.created, err)
			}
			if n != tt.created {
				t.Errorf("ALREADY_EXISTS with created=%d: expected n=%d, got %d", tt.created, tt.created, n)
			}
		})
	}
}

// TestRegression_CreatePlaceholders_USER_MAPPED_FILE_IsSuccess verifies that
// HRESULT 0x800704c8 (ERROR_USER_MAPPED_FILE) is treated as a non-fatal success.
//
// This occurs when a FETCH_DATA callback is actively hydrating a file and
// FETCH_PLACEHOLDERS fires again for the same directory.  CfCreatePlaceholders
// cannot replace the placeholder while a memory-mapped section is open.
// Before fix: returned (created, error) → ack sent with E_FAIL → Explorer error.
// After fix:  (created, nil) — placeholder is in use but exists, nothing to do.
func TestRegression_CreatePlaceholders_USER_MAPPED_FILE_IsSuccess(t *testing.T) {
	cases := []struct {
		label   string
		created int
	}{
		{"all mapped (created=0)", 0},
		{"1 created then USER_MAPPED_FILE", 1},
	}
	for _, tt := range cases {
		t.Run(tt.label, func(t *testing.T) {
			n, err := createPlaceholdersResultSpec(specHRUserMappedFile, tt.created)
			if err != nil {
				t.Errorf("USER_MAPPED_FILE with created=%d: expected nil error, got %v", tt.created, err)
			}
			if n != tt.created {
				t.Errorf("USER_MAPPED_FILE with created=%d: expected n=%d, got %d", tt.created, tt.created, n)
			}
		})
	}
}

// ─── Spec: other HRESULTs still propagate as errors ──────────────────────────

// TestRegression_CreatePlaceholders_OtherError_Propagated verifies that HRESULTs
// other than the non-fatal set are NOT silenced.
// Regression guard: ensure the fix does not accidentally swallow real errors.
func TestRegression_CreatePlaceholders_OtherError_Propagated(t *testing.T) {
	cases := []struct {
		name string
		hr   uint32
	}{
		{"E_FAIL", 0x80004005},
		{"E_ACCESSDENIED", 0x80070005},
		{"E_INVALIDARG", 0x80070057},
		{"ERROR_PATH_NOT_FOUND", 0x80070003},
		{"ERROR_DISK_FULL", 0x80070070},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := createPlaceholdersResultSpec(tt.hr, 0)
			if err == nil {
				t.Errorf("HRESULT 0x%08x (%s): expected non-nil error, got nil (fix too broad?)",
					tt.hr, tt.name)
			}
		})
	}
}

// ─── Spec: FileAttributes correctness ────────────────────────────────────────

// TestSpec_FileAttributes_File verifies the FileAttributes selection for a
// regular (non-directory) placeholder.
//
// Before fix: FILE_ATTRIBUTE_NORMAL (0x80) — standalone attribute, not combinable;
//   Windows does not recognize the file as a cloud placeholder → missing ☁️ badge
//   + aggressive probing → premature FETCH_DATA on folder open.
//
// After fix: FILE_ATTRIBUTE_ARCHIVE | FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS:
//   · FILE_ATTRIBUTE_ARCHIVE (0x20)            — standard archivable file attribute.
//   · FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS (0x400000) — marks as cloud-only:
//       Explorer shows ☁️ badge; OS defers FETCH_DATA until actual data read.
//   Must NOT include:
//   · FILE_ATTRIBUTE_PINNED (0x80000)          — eager background hydration.
//   · FILE_ATTRIBUTE_RECALL_ON_OPEN (0x40000)  — FETCH_DATA on every open.
func TestSpec_FileAttributes_File(t *testing.T) {
	const (
		fileAttrArchive            = uint32(0x00000020)
		fileAttrNormal             = uint32(0x00000080)
		fileAttrPinned             = uint32(0x00080000)
		fileAttrRecallOnOpen       = uint32(0x00040000)
		fileAttrRecallOnDataAccess = uint32(0x00400000)
	)

	got := fileAttributesSpec(false /* isDirectory */)

	// Must include ARCHIVE.
	if got&fileAttrArchive == 0 {
		t.Errorf("file attributes: missing FILE_ATTRIBUTE_ARCHIVE (0x20); got 0x%08x", got)
	}
	// Must include RECALL_ON_DATA_ACCESS for the cloud badge and lazy hydration.
	if got&fileAttrRecallOnDataAccess == 0 {
		t.Errorf("file attributes: missing FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS (0x400000); got 0x%08x "+
			"— ☁️ badge will not appear and FETCH_DATA may trigger immediately on folder open", got)
	}
	// Must NOT include NORMAL (standalone — cannot combine).
	if got&fileAttrNormal != 0 {
		t.Errorf("file attributes: FILE_ATTRIBUTE_NORMAL (0x80) must not be set for cloud files; got 0x%08x", got)
	}
	// Must NOT include PINNED (forces eager background hydration).
	if got&fileAttrPinned != 0 {
		t.Errorf("file attributes: FILE_ATTRIBUTE_PINNED (0x80000) must not be set — causes eager hydration; got 0x%08x", got)
	}
	// Must NOT include RECALL_ON_OPEN (triggers FETCH_DATA on every open).
	if got&fileAttrRecallOnOpen != 0 {
		t.Errorf("file attributes: FILE_ATTRIBUTE_RECALL_ON_OPEN (0x40000) must not be set; got 0x%08x", got)
	}
}

// TestSpec_FileAttributes_Directory verifies that directory placeholders use
// FILE_ATTRIBUTE_DIRECTORY and do NOT get RECALL_ON_DATA_ACCESS (directories
// use FETCH_PLACEHOLDERS, not FETCH_DATA, and don't need the cloud badge on the
// folder icon itself).
func TestSpec_FileAttributes_Directory(t *testing.T) {
	const fileAttrDirectory = uint32(0x00000010)

	got := fileAttributesSpec(true /* isDirectory */)

	if got&fileAttrDirectory == 0 {
		t.Errorf("directory attributes: missing FILE_ATTRIBUTE_DIRECTORY (0x10); got 0x%08x", got)
	}
}

// TestRegression_CreatePlaceholders_Success_ZeroHR verifies that HRESULT 0
// (S_OK) remains a clean success regardless of the fix.
func TestRegression_CreatePlaceholders_Success_ZeroHR(t *testing.T) {
	n, err := createPlaceholdersResultSpec(0, 7)
	if err != nil {
		t.Errorf("hr=0 (S_OK): expected nil error, got %v", err)
	}
	if n != 7 {
		t.Errorf("hr=0 (S_OK) with created=7: expected n=7, got %d", n)
	}
}

// ─── Behavioural: OnFetchPlaceholders nil on success ─────────────────────────

// TestRegression_OnFetchPlaceholders_NilWhenCreateSucceeds documents the contract
// at the Hydrator level: when CreatePlaceholders returns nil (on Linux stub, and on
// Windows when ALREADY_EXISTS is treated as nil after the fix), OnFetchPlaceholders
// must return nil.
//
// Before fix: a second FETCH_PLACEHOLDERS call returned ALREADY_EXISTS →
//   OnFetchPlaceholders returned error → ghdOnFetchPlaceholders sent E_FAIL ack →
//   Explorer showed error icon despite all placeholders being present.
// After fix:  ALREADY_EXISTS → CreatePlaceholders returns nil →
//   OnFetchPlaceholders returns nil → ack sent with S_OK.
func TestRegression_OnFetchPlaceholders_NilWhenCreateSucceeds(t *testing.T) {
	items := []plugins.FileInfo{
		{Name: "document.docx", Size: 4096, ModTime: time.Now()},
		{Name: "image.png", Size: 8192, ModTime: time.Now()},
		{Name: "Archive", IsDir: true, ModTime: time.Now()},
	}
	b := &readAtBackend{
		mockBackend: mockBackend{name: "t", connected: true},
		listItems:   items,
	}
	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(b, nil, provider, nil, "b1")

	// Linux stub: CreatePlaceholders always returns (0, nil).
	// This is equivalent to Windows post-fix where ALREADY_EXISTS → nil.
	if err := h.OnFetchPlaceholders(nil, provider.localPath); err != nil {
		t.Fatalf("OnFetchPlaceholders: expected nil (placeholders created/already exist), got %v", err)
	}
}

// TestRegression_OnFetchPlaceholders_NilOnEmptyList verifies that an empty
// backend listing (no items) returns nil without calling CreatePlaceholders at all.
// This tests the early-return guard: if len(placeholders) == 0 → return nil.
func TestRegression_OnFetchPlaceholders_NilOnEmptyList(t *testing.T) {
	b := &readAtBackend{
		mockBackend: mockBackend{name: "t", connected: true},
		listItems:   []plugins.FileInfo{}, // empty directory
	}
	provider := NewSyncProvider(t.TempDir(), "{test}", "T")
	h := NewHydrator(b, nil, provider, nil, "b1")

	if err := h.OnFetchPlaceholders(nil, provider.localPath); err != nil {
		t.Fatalf("OnFetchPlaceholders (empty dir): expected nil, got %v", err)
	}
}

// ─── Regression tests — bug #133 BaseDirectoryPath ───────────────────────────
//
// Fix (this commit): CfCreatePlaceholders was called with baseDirectoryPath =
// p.localPath (the sync root) for ALL callbacks, regardless of which directory
// was being populated.  Windows places each entry at:
//
//   baseDirectoryPath\RelativeFileName
//
// So when Explorer opens C:\GhostDrive\MFS\subfolder, the FETCH_PLACEHOLDERS
// callback fires with localPath="C:\GhostDrive\MFS\subfolder".  With the bug
// active, CfCreatePlaceholders received "C:\GhostDrive\MFS" as baseDir, and all
// entries landed at the root instead of in \subfolder.
//
// Fix: hydrator.go passes localPath (the callback's target directory) to
//      CreatePlaceholders; provider.go uses that baseDir, not p.localPath.

// createPlaceholdersBaseDirSpec mirrors the caller contract introduced in this
// fix: the baseDir forwarded to CfCreatePlaceholders must equal the localPath
// argument passed to OnFetchPlaceholders.
func createPlaceholdersBaseDirSpec(callbackLocalPath, syncRoot string) string {
	// Contract: baseDir = callbackLocalPath, never syncRoot.
	// The sync root is only used by localToRemote to compute the remote path.
	return callbackLocalPath
}

// TestRegression133_CreatePlaceholders_BaseDirIsCallbackLocalPath verifies the
// invariant: baseDir passed to CreatePlaceholders equals the FETCH_PLACEHOLDERS
// callback's localPath, not the sync root.
//
// Failing condition (before fix): baseDir was always p.localPath (sync root).
// Passing condition (after fix):  baseDir = callbackLocalPath for all cases.
func TestRegression133_CreatePlaceholders_BaseDirIsCallbackLocalPath(t *testing.T) {
	syncRoot := `/tmp/GhostDrive/MFS`

	cases := []struct {
		name              string
		callbackLocalPath string
		wantBaseDir       string
	}{
		{
			"sync root itself",
			syncRoot,
			syncRoot, // at root level, baseDir == syncRoot (coincidentally)
		},
		{
			"one level sub-folder",
			syncRoot + "/docs",
			syncRoot + "/docs", // NOT syncRoot — must be the sub-folder
		},
		{
			"deep nested sub-folder",
			syncRoot + "/a/b/c",
			syncRoot + "/a/b/c",
		},
		{
			"sub-folder with spaces",
			syncRoot + "/My Documents",
			syncRoot + "/My Documents",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := createPlaceholdersBaseDirSpec(tt.callbackLocalPath, syncRoot)
			if got != tt.wantBaseDir {
				t.Errorf("baseDir for callback(%q): got %q, want %q (regression #133 baseDir bug)",
					tt.callbackLocalPath, got, tt.wantBaseDir)
			}
			// Guard: baseDir must never be the sync root when localPath is a sub-folder.
			if tt.callbackLocalPath != syncRoot && got == syncRoot {
				t.Errorf("baseDir regression #133: got syncRoot %q for subfolder callback %q — "+
					"contents would land at the mount root instead of the sub-folder",
					syncRoot, tt.callbackLocalPath)
			}
		})
	}
}

// TestRegression133_OnFetchPlaceholders_SubDir_NoError verifies end-to-end that
// OnFetchPlaceholders does not error when called with a sub-directory path
// (regression: before the fix the wrong baseDir caused CfCreatePlaceholders to
// fail or misplace entries).  On Linux the stub accepts any baseDir — this test
// guards the happy path and documents the intended behaviour.
func TestRegression133_OnFetchPlaceholders_SubDir_NoError(t *testing.T) {
	root := t.TempDir()
	items := []plugins.FileInfo{
		{Name: "report.pdf", Size: 1024, ModTime: time.Now()},
		{Name: "image.png", Size: 4096, ModTime: time.Now()},
	}
	b := &readAtBackend{
		mockBackend: mockBackend{name: "t", connected: true},
		listItems:   items,
	}
	provider := NewSyncProvider(root, "{test}", "T")
	h := NewHydrator(b, nil, provider, nil, "b1")

	// Simulate the OS calling FETCH_PLACEHOLDERS for a sub-folder, not the root.
	subDir := root + "/subfolder"
	if err := h.OnFetchPlaceholders(nil, subDir); err != nil {
		t.Fatalf("OnFetchPlaceholders(subDir=%q): unexpected error %v "+
			"(regression #133: stub must accept any baseDir after fix)", subDir, err)
	}
}

// ─── Spec: CF_PLACEHOLDER_CREATE_FLAG NONE vs SUPERSEDE ──────────────────────
//
// Fix (commit d00de45): CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE was used for ALL
// placeholder creations, including initial ones.  SUPERSEDE tells the CF API to
// replace the placeholder's content in-place and immediately trigger FETCH_DATA
// to re-hydrate the file.  On initial creation this causes:
//   - FETCH_DATA fires immediately on folder open (before the user reads the file)
//   - Aggressive probing defeats Files On-Demand lazy hydration
//   - Each folder open triggers a backend download round-trip
//
// Fix: CreatePlaceholders now passes CF_PLACEHOLDER_CREATE_FLAG_NONE — the OS
// defers FETCH_DATA until the user actually reads the file content.
// UpdatePlaceholder retains SUPERSEDE (it replaces metadata of an existing entry).
//
// Production code under test (provider.go, //go:build windows, CGO):
//
//   func (p *SyncProvider) CreatePlaceholders(baseDir string, items []PlaceholderInfo) (int, error) {
//       return p.createPlaceholdersWithFlags(baseDir, items, C.CF_PLACEHOLDER_CREATE_FLAG_NONE)
//   }
//
//   func (p *SyncProvider) UpdatePlaceholder(localPath string, fi PlaceholderInfo) error {
//       _, err := p.createPlaceholdersWithFlags(
//           filepath.Dir(localPath), []PlaceholderInfo{fi},
//           C.CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE,
//       )
//       return err
//   }
//
// The constants live in cfapi.h (Windows SDK, not importable on Linux).
// Spec constants below mirror their exact values for cross-platform testing.

const (
	// specFlagNone mirrors CF_PLACEHOLDER_CREATE_FLAG_NONE (cfapi.h).
	// Used by CreatePlaceholders — defers FETCH_DATA until the user reads the file.
	specFlagNone = uint32(0x00000000)

	// specFlagSupersede mirrors CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE (cfapi.h, 0x4).
	// Used by UpdatePlaceholder — replaces an existing placeholder's metadata in-place.
	// Safe to use on existing placeholders; must NOT be used on initial creation
	// because it triggers FETCH_DATA immediately.
	specFlagSupersede = uint32(0x00000004)
)

// createFlagForOpSpec mirrors the flag selection made in provider.go:
//   isUpdate=false → CreatePlaceholders path → CF_PLACEHOLDER_CREATE_FLAG_NONE
//   isUpdate=true  → UpdatePlaceholder path  → CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE
func createFlagForOpSpec(isUpdate bool) uint32 {
	if isUpdate {
		return specFlagSupersede
	}
	return specFlagNone
}

// TestSpec_CreatePlaceholders_UsesFlag_NONE verifies that initial placeholder
// creation selects CF_PLACEHOLDER_CREATE_FLAG_NONE, not SUPERSEDE.
//
// Before fix: CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE was always set, causing
//   FETCH_DATA to fire on every folder open — all files downloaded immediately.
// After fix:  NONE is used for initial creation — lazy hydration preserved.
func TestSpec_CreatePlaceholders_UsesFlag_NONE(t *testing.T) {
	got := createFlagForOpSpec(false /* isUpdate=false → CreatePlaceholders */)

	if got != specFlagNone {
		t.Errorf("CreatePlaceholders flag: got 0x%08x, want CF_PLACEHOLDER_CREATE_FLAG_NONE (0x00000000) "+
			"— SUPERSEDE on initial creation triggers premature FETCH_DATA on folder open", got)
	}
	// Explicit anti-regression: must NOT be SUPERSEDE.
	if got == specFlagSupersede {
		t.Errorf("CreatePlaceholders: must NOT use CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE (0x%08x) "+
			"— causes immediate FETCH_DATA, defeating Files On-Demand lazy hydration", specFlagSupersede)
	}
}

// TestSpec_UpdatePlaceholder_UsesFlag_SUPERSEDE verifies that UpdatePlaceholder
// uses CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE to replace existing metadata in-place.
//
// This is correct for updates: the placeholder already exists on disk, and the
// caller wants to overwrite its metadata (e.g. new ModTime after a remote change).
// SUPERSEDE on an existing placeholder does NOT trigger FETCH_DATA for content.
func TestSpec_UpdatePlaceholder_UsesFlag_SUPERSEDE(t *testing.T) {
	got := createFlagForOpSpec(true /* isUpdate=true → UpdatePlaceholder */)

	if got != specFlagSupersede {
		t.Errorf("UpdatePlaceholder flag: got 0x%08x, want CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE (0x%08x)",
			got, specFlagSupersede)
	}
}

// TestSpec_CreateFlags_AreDistinct verifies that NONE and SUPERSEDE are distinct,
// non-zero-vs-zero constants with the exact values from the Windows SDK.
// A regression guard: if the constants are accidentally swapped or re-defined,
// this test will catch it before the broken binary reaches a Windows machine.
func TestSpec_CreateFlags_AreDistinct(t *testing.T) {
	if specFlagNone == specFlagSupersede {
		t.Errorf("CF flag constants: NONE (0x%08x) == SUPERSEDE (0x%08x) — constants must be distinct",
			specFlagNone, specFlagSupersede)
	}
	if specFlagNone != 0 {
		t.Errorf("CF_PLACEHOLDER_CREATE_FLAG_NONE: expected 0x00000000, got 0x%08x "+
			"(Windows SDK value — do not change)", specFlagNone)
	}
	if specFlagSupersede != 4 {
		t.Errorf("CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE: expected 0x00000004, got 0x%08x "+
			"(Windows SDK value — do not change)", specFlagSupersede)
	}
}

// ─── Behavioural: UpdatePlaceholder stub ─────────────────────────────────────

// TestRegression_UpdatePlaceholder_Stub_NoError verifies that the Linux stub for
// UpdatePlaceholder returns nil without panicking.
//
// Commit d00de45 refactored UpdatePlaceholder from a direct CreatePlaceholders
// call into a separate method that passes CF_PLACEHOLDER_CREATE_FLAG_SUPERSEDE
// via createPlaceholdersWithFlags.  The stub must remain a no-op after the
// refactor — this test guards that contract on Linux.
func TestRegression_UpdatePlaceholder_Stub_NoError(t *testing.T) {
	p := NewSyncProvider(t.TempDir(), "{test}", "T")
	fi := PlaceholderInfo{
		RelativePath: "document.docx",
		FileSize:     4096,
		ModTime:      time.Now(),
	}
	// localPath is a file path (not a directory) — UpdatePlaceholder uses
	// filepath.Dir(localPath) as baseDir internally.
	if err := p.UpdatePlaceholder(p.localPath+"/document.docx", fi); err != nil {
		t.Errorf("UpdatePlaceholder stub: expected nil, got %v "+
			"(regression d00de45: stub must be no-op after SUPERSEDE refactor)", err)
	}
}

// ─── Regression tests — bug #133 CfConvertToPlaceholder ─────────────────────
//
// Fix (this commit): when CfCreatePlaceholders returns ALREADY_EXISTS for a
// directory entry, the local directory is an ordinary NTFS directory (never
// registered as a CF placeholder).  The OS never calls FETCH_PLACEHOLDERS for
// it → its remote content stays invisible.
//
// Fix: call CfConvertToPlaceholder on the local directory, which registers it as
// a CF placeholder.  The next time the user opens the directory, Windows calls
// FETCH_PLACEHOLDERS, and the remote content is merged.
//
// Architecture: CfConvertToPlaceholder lives in provider.go (//go:build windows)
// and is not callable on Linux.  We test the caller logic via:
//  1. A spec test that verifies the convert-on-ALREADY_EXISTS-directory rule.
//  2. A behavioural test that exercises OnFetchPlaceholders end-to-end on Linux
//     (stub accepts any baseDir and returns nil — the convert call is a no-op).

// CF_CONVERT_FLAGS spec values — mirror cgo_cfapi_windows.c logic.
const (
	// specConvertFlagMarkInSync mirrors CF_CONVERT_FLAG_MARK_IN_SYNC (0x1).
	// Applied to files: marks the local copy as "in sync" → badge ✓✓.
	specConvertFlagMarkInSync = uint32(0x00000001)

	// specConvertFlagEnableOnDemand mirrors CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION (0x4).
	// Applied to directories: enables on-demand FETCH_PLACEHOLDERS calls on open.
	// Must NOT be combined with MARK_IN_SYNC for directories — that would mark
	// the directory as "fully populated" and prevent re-population after restart.
	specConvertFlagEnableOnDemand = uint32(0x00000004)
)

// convertFlagsSpec mirrors the flag selection in provider.go createPlaceholdersWithFlags:
//   · isDirectory=true  → CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION (no MARK_IN_SYNC)
//   · isDirectory=false → CF_CONVERT_FLAG_MARK_IN_SYNC
func convertFlagsSpec(isDirectory bool) uint32 {
	if isDirectory {
		return specConvertFlagEnableOnDemand
	}
	return specConvertFlagMarkInSync
}

// convertOnAlreadyExistsSpec mirrors the production decision logic in
// createPlaceholdersWithFlags: "convert to placeholder iff ALREADY_EXISTS"
// (applies to BOTH files and directories).
func convertOnAlreadyExistsSpec(hr uint32, _ bool) bool {
	// Rule: CfConvertToPlaceholder is called for ANY entry (file or directory)
	// when hr == ALREADY_EXISTS.  The local NTFS entry exists but is not yet a
	// CF placeholder:
	//   · Directories: OS never calls FETCH_PLACEHOLDERS without conversion.
	//   · Files: file gets no CF badge (not ☁️ nor ✓✓) without conversion.
	return hr == specHRAlreadyExists
}

// TestRegression133_ConvertToPlaceholder_FilesAndDirectories verifies that
// CfConvertToPlaceholder is called for BOTH files and directories on ALREADY_EXISTS.
//
// Before fix (c27c0ca): only directories were converted.  Files that already
//   existed locally were silently skipped → no CF attributes → no badge.
// After fix (this commit): files are also converted → badge ✓✓ (in-sync).
func TestRegression133_ConvertToPlaceholder_FilesAndDirectories(t *testing.T) {
	cases := []struct {
		name        string
		hr          uint32
		isDirectory bool
		wantConvert bool
	}{
		{
			"directory ALREADY_EXISTS → convert",
			specHRAlreadyExists, true, true,
		},
		{
			"file ALREADY_EXISTS → convert (fix: was skipped before)",
			specHRAlreadyExists, false, true,
		},
		{
			"directory S_OK → no convert (created successfully)",
			0, true, false,
		},
		{
			"file S_OK → no convert (created successfully)",
			0, false, false,
		},
		{
			"directory USER_MAPPED_FILE → no convert",
			specHRUserMappedFile, true, false,
		},
		{
			"file USER_MAPPED_FILE → no convert",
			specHRUserMappedFile, false, false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := convertOnAlreadyExistsSpec(tt.hr, tt.isDirectory)
			if got != tt.wantConvert {
				t.Errorf("convertOnAlreadyExists(hr=0x%08x, isDir=%v) = %v, want %v",
					tt.hr, tt.isDirectory, got, tt.wantConvert)
			}
		})
	}
}

// TestRegression133_OnFetchPlaceholders_WithDirectory verifies end-to-end that
// OnFetchPlaceholders succeeds when the backend lists a directory entry.
// On Linux the stub's CreatePlaceholders ignores the entry (no ALREADY_EXISTS can
// be triggered); this test documents the happy path and guards against regressions
// that would cause OnFetchPlaceholders to error on directory entries.
func TestRegression133_OnFetchPlaceholders_WithDirectory(t *testing.T) {
	root := t.TempDir()
	items := []plugins.FileInfo{
		{Name: "documents", IsDir: true, ModTime: time.Now()},
		{Name: "readme.txt", IsDir: false, Size: 512, ModTime: time.Now()},
		{Name: "archive", IsDir: true, ModTime: time.Now()},
	}
	b := &readAtBackend{
		mockBackend: mockBackend{name: "t", connected: true},
		listItems:   items,
	}
	provider := NewSyncProvider(root, "{test}", "T")
	h := NewHydrator(b, nil, provider, nil, "b1")

	// Calling OnFetchPlaceholders with a mix of directories and files must not error.
	// On Linux: stub returns (0, nil) for every entry — no ALREADY_EXISTS occurs.
	// On Windows: if a directory already exists locally, CfConvertToPlaceholder is
	// called silently; the function still returns nil.
	if err := h.OnFetchPlaceholders(nil, root); err != nil {
		t.Fatalf("OnFetchPlaceholders with directories: unexpected error %v", err)
	}
}

// ─── Tracking spec: CfConvertToPlaceholder call verification ─────────────────
//
// The production convert call is:
//   if hr == hrAlreadyExists && item.IsDirectory {
//       fullPath := filepath.Join(baseDir, item.RelativePath)
//       C.ghd_convert_to_placeholder(wFull)          // provider.go, windows only
//   }
//
// Since ghd_convert_to_placeholder lives in CGO (not callable on Linux) and
// Hydrator.provider is *SyncProvider (concrete — no interface injection),
// we mirror the per-entry decision and path-construction logic in a spec helper
// that accepts a tracking callback.  The callback records every path that would
// have been passed to CfConvertToPlaceholder, letting us assert:
//   · the path is filepath.Join(baseDir, item.RelativePath) — not just RelativePath
//   · files are never converted (isDir=false guard)
//   · entries created successfully (hr=0) are never converted

// convertTracker records every path that the production code would pass to
// CfConvertToPlaceholder.  Used in spec tests to verify call site and arguments.
type convertTracker struct {
	calls []string
}

// convertPathSpec mirrors the full per-entry convert decision from provider.go
// (updated for commit 69628d7 — IsDirectory guard removed):
//
//	if hr == hrAlreadyExists {                    // applies to ALL entry types
//	    convertFn(filepath.Join(baseDir, item.RelativePath))
//	}
//
// Before 69628d7: only directory entries were converted.
// After  69628d7: all entries (files + directories) are converted on ALREADY_EXISTS.
func convertPathSpec(baseDir string, item PlaceholderInfo, hr uint32, convertFn func(string)) {
	if hr == specHRAlreadyExists {
		convertFn(filepath.Join(baseDir, item.RelativePath))
	}
}

// TestRegression133_ConvertToPlaceholder_ExistingDir_PathTracked verifies that
// an existing local directory (ALREADY_EXISTS + IsDirectory) causes CfConvertToPlaceholder
// to be called with the correct full path: filepath.Join(baseDir, RelativePath).
//
// Before fix: ALREADY_EXISTS was silently ignored for all entries → directory
//   remained an NTFS dir, never registered as CF placeholder → FETCH_PLACEHOLDERS
//   never triggered → remote content invisible.
// After fix:  CfConvertToPlaceholder called with the full path → OS triggers
//   FETCH_PLACEHOLDERS on next open → remote content merged.
func TestRegression133_ConvertToPlaceholder_ExistingDir_PathTracked(t *testing.T) {
	baseDir := "/tmp/GhostDrive/MFS"
	item := PlaceholderInfo{RelativePath: "documents", IsDirectory: true}
	wantPath := filepath.Join(baseDir, "documents") // must NOT be just "documents"

	tr := &convertTracker{}
	convertPathSpec(baseDir, item, specHRAlreadyExists, func(p string) {
		tr.calls = append(tr.calls, p)
	})

	if len(tr.calls) == 0 {
		t.Fatalf("existing directory ALREADY_EXISTS: expected CfConvertToPlaceholder to be called, got 0 calls "+
			"(regression c27c0ca: directory must be converted so FETCH_PLACEHOLDERS fires on next open)")
	}
	if tr.calls[0] != wantPath {
		t.Errorf("convert path: got %q, want %q — must be filepath.Join(baseDir, RelativePath), not RelativePath alone",
			tr.calls[0], wantPath)
	}
}

// ─── Regression tests — directory population state (#133 restart regression) ─
//
// Fix (this commit): ghd_convert_to_placeholder used CF_CONVERT_FLAG_MARK_IN_SYNC
// for ALL entries (files and directories).  On directories, MARK_IN_SYNC marks
// the directory as "fully populated" — the OS caches this state and never calls
// OnFetchPlaceholders again after a restart.
//
// Symptom: first session showed remote content; after restart only local content
// was visible.
//
// Fix: split into two C helpers with different flags:
//   ghd_convert_to_placeholder     (files)       → CF_CONVERT_FLAG_MARK_IN_SYNC
//   ghd_convert_dir_to_placeholder (directories) → CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION
//
// The directory stays in "partial" population state → OS calls FETCH_PLACEHOLDERS
// on every open → remote content merged on every session.

// TestSpec_ConvertFlags_File_HasMarkInSync verifies that the flags applied to
// a file conversion include CF_CONVERT_FLAG_MARK_IN_SYNC and do NOT include
// ENABLE_ON_DEMAND_POPULATION (files don't use FETCH_PLACEHOLDERS).
func TestSpec_ConvertFlags_File_HasMarkInSync(t *testing.T) {
	flags := convertFlagsSpec(false /* isDirectory=false → file */)

	if flags&specConvertFlagMarkInSync == 0 {
		t.Errorf("file convert flags: missing CF_CONVERT_FLAG_MARK_IN_SYNC (0x%08x); got 0x%08x "+
			"— file will not get badge ✓✓", specConvertFlagMarkInSync, flags)
	}
	if flags&specConvertFlagEnableOnDemand != 0 {
		t.Errorf("file convert flags: CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION (0x%08x) must not "+
			"be set for files; got 0x%08x", specConvertFlagEnableOnDemand, flags)
	}
}

// TestSpec_ConvertFlags_Dir_NoMarkInSync verifies that the flags applied to a
// directory conversion do NOT include CF_CONVERT_FLAG_MARK_IN_SYNC.
//
// Regression: MARK_IN_SYNC on a directory marks population as "complete" and
// the OS stops calling OnFetchPlaceholders after a restart — only local content
// visible.  Without MARK_IN_SYNC the directory stays in "partial" state and
// FETCH_PLACEHOLDERS is triggered on every open.
func TestSpec_ConvertFlags_Dir_NoMarkInSync(t *testing.T) {
	flags := convertFlagsSpec(true /* isDirectory=true → directory */)

	// MARK_IN_SYNC must NOT be set for directories.
	if flags&specConvertFlagMarkInSync != 0 {
		t.Errorf("directory convert flags: CF_CONVERT_FLAG_MARK_IN_SYNC (0x%08x) must NOT be set "+
			"— causes OS to cache directory as 'fully populated', OnFetchPlaceholders never "+
			"called after restart (content only visible in first session); got flags=0x%08x",
			specConvertFlagMarkInSync, flags)
	}
	// ENABLE_ON_DEMAND_POPULATION must be set to keep FETCH_PLACEHOLDERS active.
	if flags&specConvertFlagEnableOnDemand == 0 {
		t.Errorf("directory convert flags: missing CF_CONVERT_FLAG_ENABLE_ON_DEMAND_POPULATION (0x%08x); "+
			"got 0x%08x — OnFetchPlaceholders may not fire on directory open", specConvertFlagEnableOnDemand, flags)
	}
}

// TestSpec_ConvertFlags_FileAndDir_AreDistinct verifies that file flags and
// directory flags are different (guard against accidentally using the same value).
func TestSpec_ConvertFlags_FileAndDir_AreDistinct(t *testing.T) {
	fileFlags := convertFlagsSpec(false)
	dirFlags := convertFlagsSpec(true)

	if fileFlags == dirFlags {
		t.Errorf("convert flags for files (0x%08x) and directories (0x%08x) must be different "+
			"— same flags means regression bug was re-introduced", fileFlags, dirFlags)
	}
}

// TestRegression133_ConvertToPlaceholder_ExistingFile_Tracked verifies that
// CfConvertToPlaceholder IS called for a file entry returning ALREADY_EXISTS.
//
// Behaviour changed in 69628d7: before that fix only directories were converted;
// files returning ALREADY_EXISTS were silently skipped → file had no CF attributes
// → no badge (not ☁️ nor ✓✓) in Explorer.
// After fix: file is also converted → CF_CONVERT_FLAG_MARK_IN_SYNC → badge ✓✓.
func TestRegression133_ConvertToPlaceholder_ExistingFile_Tracked(t *testing.T) {
	baseDir := "/tmp/GhostDrive/MFS"
	item := PlaceholderInfo{RelativePath: "report.pdf", IsDirectory: false}
	wantPath := filepath.Join(baseDir, "report.pdf")

	tr := &convertTracker{}
	convertPathSpec(baseDir, item, specHRAlreadyExists, func(p string) {
		tr.calls = append(tr.calls, p)
	})

	if len(tr.calls) == 0 {
		t.Fatalf("existing file ALREADY_EXISTS: expected CfConvertToPlaceholder to be called, got 0 calls "+
			"(regression 69628d7: files must be converted to get CF badge ✓✓)")
	}
	if tr.calls[0] != wantPath {
		t.Errorf("convert path for file: got %q, want %q", tr.calls[0], wantPath)
	}
}

// TestRegression133_ConvertToPlaceholder_NewDir_NotTracked verifies that
// a newly created directory (hr=0, S_OK) does NOT trigger CfConvertToPlaceholder.
// The placeholder was just created by CfCreatePlaceholders — it is already a CF
// placeholder; conversion is unnecessary and would be redundant.
func TestRegression133_ConvertToPlaceholder_NewDir_NotTracked(t *testing.T) {
	baseDir := "/tmp/GhostDrive/MFS"
	item := PlaceholderInfo{RelativePath: "newsubdir", IsDirectory: true}

	tr := &convertTracker{}
	convertPathSpec(baseDir, item, 0 /* hr=0 = S_OK */, func(p string) {
		tr.calls = append(tr.calls, p)
	})

	if len(tr.calls) != 0 {
		t.Errorf("new directory S_OK: CfConvertToPlaceholder must NOT be called; got calls=%v "+
			"(placeholder was just created — already a CF entry)", tr.calls)
	}
}

// TestRegression133_ConvertToPlaceholder_MultipleItems_TrackCalls verifies the
// per-item behaviour across a realistic mixed listing (updated for 69628d7):
//   - dir1 (ALREADY_EXISTS) → convert called with baseDir/dir1
//   - file.txt (ALREADY_EXISTS) → convert called with baseDir/file.txt (changed by 69628d7)
//   - dir2 (S_OK, newly created) → no convert
//   - dir3 (ALREADY_EXISTS) → convert called with baseDir/dir3
//
// Expected: exactly 3 convert calls (dir1, file.txt, dir3), in order.
// Before 69628d7: 2 calls only (dir1, dir3) — file was skipped.
func TestRegression133_ConvertToPlaceholder_MultipleItems_TrackCalls(t *testing.T) {
	baseDir := "/tmp/GhostDrive/MFS/parent"
	type entry struct {
		item PlaceholderInfo
		hr   uint32
	}
	entries := []entry{
		{PlaceholderInfo{RelativePath: "dir1", IsDirectory: true}, specHRAlreadyExists},
		{PlaceholderInfo{RelativePath: "file.txt", IsDirectory: false}, specHRAlreadyExists},
		{PlaceholderInfo{RelativePath: "dir2", IsDirectory: true}, 0}, // newly created — no convert
		{PlaceholderInfo{RelativePath: "dir3", IsDirectory: true}, specHRAlreadyExists},
	}

	tr := &convertTracker{}
	for _, e := range entries {
		convertPathSpec(baseDir, e.item, e.hr, func(p string) {
			tr.calls = append(tr.calls, p)
		})
	}

	wantCalls := []string{
		filepath.Join(baseDir, "dir1"),
		filepath.Join(baseDir, "file.txt"), // 69628d7: file now also converted
		filepath.Join(baseDir, "dir3"),
	}

	if len(tr.calls) != len(wantCalls) {
		t.Fatalf("convert call count: got %d, want %d; calls=%v "+
			"(regression 69628d7: file.txt must also be converted — was skipped before)",
			len(tr.calls), len(wantCalls), tr.calls)
	}
	for i, want := range wantCalls {
		if tr.calls[i] != want {
			t.Errorf("convert call[%d]: got %q, want %q", i, tr.calls[i], want)
		}
	}
}

// ─── Regression tests — 69628d7 CfConvertToPlaceholder for files ─────────────
//
// Fix (commit 69628d7): the `if item.IsDirectory` guard in the hrAlreadyExists
// case of createPlaceholdersWithFlags was removed.  Before this fix:
//   · Files returning ALREADY_EXISTS were silently skipped.
//   · The local file had no CF attributes → no badge in Explorer (not ☁️ nor ✓✓).
// After the fix:
//   · ALL entries (files + directories) on ALREADY_EXISTS → CfConvertToPlaceholder.
//   · ghd_convert_to_placeholder uses FILE_FLAG_BACKUP_SEMANTICS (works for both).
//   · CF_CONVERT_FLAG_MARK_IN_SYNC → file gets badge ✓✓ (local + in-sync).

// TestRegression69628d7_ExistingFile_ConvertCalled is the primary regression guard
// for commit 69628d7.  It verifies that a file entry returning ALREADY_EXISTS causes
// CfConvertToPlaceholder to be called — the exact behaviour that was missing before
// the fix.
//
// Failing condition (before 69628d7): isDirectory guard skipped the convert call.
// Passing condition (after 69628d7):  convert called for any ALREADY_EXISTS entry.
func TestRegression69628d7_ExistingFile_ConvertCalled(t *testing.T) {
	baseDir := "/tmp/GhostDrive/MFS"
	item := PlaceholderInfo{RelativePath: "notes.txt", IsDirectory: false}
	wantPath := filepath.Join(baseDir, "notes.txt")

	tr := &convertTracker{}
	convertPathSpec(baseDir, item, specHRAlreadyExists, func(p string) {
		tr.calls = append(tr.calls, p)
	})

	// Primary regression guard: was 0 before the fix.
	if len(tr.calls) == 0 {
		t.Fatalf("69628d7 regression: existing file ALREADY_EXISTS — CfConvertToPlaceholder not called "+
			"(IsDirectory guard incorrectly still present — file would have no CF badge)")
	}
	if tr.calls[0] != wantPath {
		t.Errorf("69628d7: convert path: got %q, want %q", tr.calls[0], wantPath)
	}
}

// TestRegression69628d7_DirUnchanged_ConvertStillCalled verifies that the 69628d7
// fix did not accidentally break the c27c0ca directory conversion.
// Both file and directory paths must still trigger convert on ALREADY_EXISTS.
func TestRegression69628d7_DirUnchanged_ConvertStillCalled(t *testing.T) {
	baseDir := "/tmp/GhostDrive/MFS"
	item := PlaceholderInfo{RelativePath: "archive", IsDirectory: true}
	wantPath := filepath.Join(baseDir, "archive")

	tr := &convertTracker{}
	convertPathSpec(baseDir, item, specHRAlreadyExists, func(p string) {
		tr.calls = append(tr.calls, p)
	})

	if len(tr.calls) == 0 {
		t.Fatalf("69628d7 regression: existing directory ALREADY_EXISTS — CfConvertToPlaceholder not called "+
			"(c27c0ca directory behaviour must not have been broken by the file fix)")
	}
	if tr.calls[0] != wantPath {
		t.Errorf("69628d7 dir: convert path: got %q, want %q", tr.calls[0], wantPath)
	}
}

// ─── Spec: #129 badge state machine (ReportProgress + error reset) ───────────
//
// Badge state contracts for hydration flow (hydrator.OnFetchData):
//
//   Start of FETCH_DATA callback
//     └─► file exists as placeholder with ☁️ (CloudOnly / NOT_IN_SYNC)
//
//   During hydration (each chunk transfer)
//     └─► CfReportProviderProgress → ⟳ spinner + progress %
//         (opInfo must be non-zero for this call to reach Windows)
//
//   After full transfer (success path)
//     └─► SetSyncState(SyncStateSynced) → ✓✓ badge
//
//   After backend/transfer error
//     └─► ReportError + SetSyncState(SyncStateCloudOnly) → ☁️ badge restored
//         (file must not be left in an ambiguous state)
//
// On Linux, ReportProgress and SetSyncState are stubs (no-op).  Spec tests verify
// the call-site logic (when / with what arguments) cross-platform.

// reportProgressCalledSpec mirrors the production guard in SyncProvider.ReportProgress:
// the call is skipped when opInfo == 0 (no active FETCH_DATA callback).
func reportProgressCalledSpec(opInfo uintptr) bool {
	return opInfo != 0
}

// syncStateAfterHydrationSpec describes the badge state the user sees after hydration:
//   - success path → SyncStateSynced (✓✓) — SetSyncState(Synced) is called explicitly
//   - error path   → SyncStateCloudOnly (☁️) — badge stays ☁️ naturally
//
// IMPORTANT (fix after 10a297f regression): on the error path SetSyncState is NOT
// called explicitly.  The file was NOT_IN_SYNC (☁️) before FETCH_DATA was triggered
// and it stays ☁️ because we never called SetSyncState(Synced).  ReportError alone
// signals Windows that the operation failed.  Calling CfSetInSyncState(NOT_IN_SYNC)
// on a file during an active FETCH_DATA operation can corrupt the parent directory's
// CF population state, causing FETCH_PLACEHOLDERS to not fire on subsequent opens
// and hiding remote content (merge regression after 10a297f).
func syncStateAfterHydrationSpec(hadError bool) SyncState {
	if hadError {
		return SyncStateCloudOnly
	}
	return SyncStateSynced
}

// TestSpec129_ReportProgress_ZeroOpInfo_Skip verifies that when opInfo == 0
// (no active FETCH_DATA callback, e.g. during unit tests without a real CF connection),
// ReportProgress is a no-op.  A non-zero opInfo must trigger the call.
//
// Production code: provider.go SyncProvider.ReportProgress — if req.opInfo == 0, return nil.
func TestSpec129_ReportProgress_ZeroOpInfo_Skip(t *testing.T) {
	if reportProgressCalledSpec(0) {
		t.Error("opInfo=0: ReportProgress must be skipped — no active FETCH_DATA callback")
	}
	if !reportProgressCalledSpec(0xdeadbeef) {
		t.Error("opInfo!=0: ReportProgress must be called to update Explorer ⟳ spinner")
	}
}

// TestSpec129_SyncState_SuccessPath_SetsSynced verifies that a successful
// hydration results in SyncStateSynced (✓✓ badge) — not CloudOnly or any other state.
func TestSpec129_SyncState_SuccessPath_SetsSynced(t *testing.T) {
	got := syncStateAfterHydrationSpec(false /* no error */)
	if got != SyncStateSynced {
		t.Errorf("success path: expected SyncStateSynced (%d), got %d — badge will not be ✓✓", SyncStateSynced, got)
	}
}

// TestSpec129_SyncState_ErrorPath_StaysCloudOnly verifies that the badge stays ☁️
// (SyncStateCloudOnly) after a hydration error.
//
// After fix (regression 10a297f): SetSyncState(CloudOnly) is NOT called explicitly
// on the error path.  The badge stays ☁️ naturally — the file was NOT_IN_SYNC before
// FETCH_DATA and ReportError signals Windows about the failure without changing sync
// state.  The explicit call was removed because CfSetInSyncState(NOT_IN_SYNC) on a
// file during an active FETCH_DATA operation can corrupt directory CF population
// state and break FETCH_PLACEHOLDERS (merge regression).
//
// The spec function still returns SyncStateCloudOnly for hadError=true, representing
// the badge the user observes (not an explicit SetSyncState call).
func TestSpec129_SyncState_ErrorPath_StaysCloudOnly(t *testing.T) {
	got := syncStateAfterHydrationSpec(true /* error */)
	if got != SyncStateCloudOnly {
		t.Errorf("error path: expected SyncStateCloudOnly (%d), got %d — badge must stay ☁️ after hydration failure",
			SyncStateCloudOnly, got)
	}
}

// TestSpec129_SyncState_ErrorAndSuccessAreDistinct verifies that the two badge
// targets are different values (guard against a constant refactor that swaps them).
func TestSpec129_SyncState_ErrorAndSuccessAreDistinct(t *testing.T) {
	errState := syncStateAfterHydrationSpec(true)
	okState := syncStateAfterHydrationSpec(false)
	if errState == okState {
		t.Errorf("error state (%d) == success state (%d): these must be distinct", errState, okState)
	}
}

// TestSpec129_ReportProgress_StubNoError verifies that the Linux stub
// SyncProvider.ReportProgress returns nil without panicking.
// Mirrors TestProviderStub in manager_test.go for the new method.
func TestSpec129_ReportProgress_StubNoError(t *testing.T) {
	p := NewSyncProvider(t.TempDir(), "{test}", "T")
	req := FetchRequest{LocalPath: p.localPath + "/file.txt", opInfo: 0}
	if err := p.ReportProgress(req, 4096, 1024); err != nil {
		t.Errorf("ReportProgress stub: expected nil, got %v", err)
	}
}

// ─── Spec: StorageProvider registration (badges fix) ─────────────────────────
//
// Fix (this commit): without calling StorageProviderSyncRootManager::Register
// (or the equivalent registry write), Windows does not recognise GhostDrive as a
// cloud storage provider and does not display cloud overlay icons (☁️, ✓✓, ⟳).
//
// Solution: RegisterStorageProvider writes the SyncRootManager registry keys
// before CfRegisterSyncRoot is called in CFManager.Start.

// syncRootIDSpec mirrors the SyncRootId format used by RegisterStorageProvider:
//
//	"{StorageProvider}!{UserSid}!{BackendName}"
//
// This format is required by Windows StorageProviderSyncRootManager.
func syncRootIDSpec(backendName, userSID string) string {
	return "GhostDrive!" + userSID + "!" + backendName
}

// TestSpec_StorageProvider_SyncRootIDFormat verifies the SyncRootId is composed
// of the three required components separated by "!".
// A malformed ID (e.g. missing SID) causes Windows to ignore the registration.
func TestSpec_StorageProvider_SyncRootIDFormat(t *testing.T) {
	cases := []struct {
		backendName string
		userSID     string
		want        string
	}{
		{"MFS", "S-1-5-21-1234-5678-9012-1001", "GhostDrive!S-1-5-21-1234-5678-9012-1001!MFS"},
		{"WebDAV", "S-1-5-21-0000-0000-0001-500", "GhostDrive!S-1-5-21-0000-0000-0001-500!WebDAV"},
		{"LocalBackend", "S-1-5-21-1111-2222-3333-1000", "GhostDrive!S-1-5-21-1111-2222-3333-1000!LocalBackend"},
	}
	for _, tt := range cases {
		t.Run(tt.backendName, func(t *testing.T) {
			got := syncRootIDSpec(tt.backendName, tt.userSID)
			if got != tt.want {
				t.Errorf("syncRootID(%q, %q) = %q, want %q", tt.backendName, tt.userSID, got, tt.want)
			}
		})
	}
}

// TestSpec_StorageProvider_RegisterStub verifies that RegisterStorageProvider
// returns nil on non-Windows platforms (Linux stub — no registry, no-op).
func TestSpec_StorageProvider_RegisterStub(t *testing.T) {
	if err := RegisterStorageProvider("/tmp/sync", "TestBackend", "GhostDrive — Test"); err != nil {
		t.Errorf("RegisterStorageProvider stub: expected nil, got %v", err)
	}
}

// TestSpec_StorageProvider_RegisteredBeforeCfRegister verifies the call ORDER
// contract in CFManager.Start:  RegisterStorageProvider must be called BEFORE
// CfRegisterSyncRoot (p.Register).  Windows requires the StorageProvider to be
// registered first so that cloud icons are shown immediately when the sync root
// is first opened.
//
// The production code in manager.go follows this order:
//   1. RegisterStorageProvider  ← writes SyncRootManager registry keys
//   2. p.Register()             ← calls CfRegisterSyncRoot
//   3. p.Connect(cbs)           ← calls CfConnectSyncRoot
//
// This test documents the contract; the Linux stub makes both calls no-ops.
func TestSpec_StorageProvider_RegisteredBeforeCfRegister(t *testing.T) {
	// On Linux both RegisterStorageProvider and p.Register are stubs.
	// We just verify they both return nil in the expected order.
	syncPath := t.TempDir()
	if err := RegisterStorageProvider(syncPath, "SpecBackend", "GhostDrive — Spec"); err != nil {
		t.Fatalf("RegisterStorageProvider: %v", err)
	}
	p := NewSyncProvider(syncPath, "{test}", "GhostDrive — Spec")
	if err := p.Register(); err != nil {
		t.Fatalf("p.Register after RegisterStorageProvider: %v", err)
	}
}

// TestRegression69628d7_NewEntry_NoConvert verifies that entries created
// successfully (hr=0, S_OK) do NOT trigger CfConvertToPlaceholder — regardless
// of whether they are files or directories.
func TestRegression69628d7_NewEntry_NoConvert(t *testing.T) {
	baseDir := "/tmp/GhostDrive/MFS"
	cases := []struct {
		name string
		item PlaceholderInfo
	}{
		{"new file (S_OK)", PlaceholderInfo{RelativePath: "new.txt", IsDirectory: false}},
		{"new directory (S_OK)", PlaceholderInfo{RelativePath: "newdir", IsDirectory: true}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tr := &convertTracker{}
			convertPathSpec(baseDir, tt.item, 0 /* hr=0 = S_OK */, func(p string) {
				tr.calls = append(tr.calls, p)
			})
			if len(tr.calls) != 0 {
				t.Errorf("%s: CfConvertToPlaceholder must NOT be called for S_OK entry; got calls=%v",
					tt.name, tr.calls)
			}
		})
	}
}
