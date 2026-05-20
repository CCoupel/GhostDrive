//go:build windows

package placeholder

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/CCoupel/GhostDrive/internal/logger"
	syncdispatch "github.com/CCoupel/GhostDrive/internal/sync"
	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/winfsp/cgofuse/fuse"
)

const cacheTTL = time.Hour

// FUSE open-flag constants (POSIX O_WRONLY / O_RDWR).
const (
	fuseOWronly = 1
	fuseORdwr   = 2
)

// openEntry tracks a live file handle returned from Open or Create.
type openEntry struct {
	tempPath  string
	writeable bool
	dirty     bool // true after at least one Write() — guards against 0-byte Create artifacts
}

// MetaUpdatedEvent is the payload of the "meta:updated" Wails event.
// It is emitted by watchLoop whenever a FileEvent is received from Watch().
type MetaUpdatedEvent struct {
	BackendID string `json:"backendID"`
	Path      string `json:"path"`
	EventType string `json:"eventType"` // "created"|"modified"|"deleted"|"renamed"
}

// GhostFileSystem implements fuse.FileSystemInterface by routing calls to the
// appropriate StorageBackend based on the path prefix /<BackendName>/...
type GhostFileSystem struct {
	fuse.FileSystemBase

	// backendsMu guards backends, watchContexts and watchBaseCtx.
	// FUSE callbacks acquire a read-lock and snapshot the slice; UpdateBackends
	// acquires the write-lock.  Never hold backendsMu while performing backend
	// I/O — only use it to snapshot the slice header.
	backendsMu gosync.RWMutex
	backends   []MountedBackend

	// watchBaseCtx is the root context passed to startWatchLoops by Mount().
	// Child contexts are derived from it in updateWatchLoops, one per backend.
	watchBaseCtx context.Context
	// watchContexts maps backendID → cancel function for its watchLoop goroutine.
	// Protected by backendsMu.
	watchContexts map[string]context.CancelFunc

	// host is the WinFsp FileSystemHost that owns this filesystem instance.
	// Set once by WinFspDrive.Mount() immediately after fuse.NewFileSystemHost()
	// and before host.Mount() starts FUSE dispatch — safe to read without a lock.
	// Used to call host.Notify() after uploads so Explorer refreshes automatically.
	host *fuse.FileSystemHost

	// meta is the in-memory LRU cache for Stat/List metadata.
	// Invalidated on writes (Release, Unlink, Rename, Mkdir, Create) and by
	// push events from Watch() goroutines.
	meta *metaCache

	// emitter emits Wails events to the frontend (e.g. "meta:updated").
	// May be nil if no emitter was provided; guard every Emit call with a nil check.
	emitter syncdispatch.EventEmitter

	mu            gosync.Mutex
	fhSeq         atomic.Uint64
	handles       map[uint64]*openEntry
	desktopIni    []byte // content of the virtual /desktop.ini
	desktopIniTmp string // temp path for desktop.ini reads
	iconTmp       string // temp path for ghostdrive.ico reads
}

func newGhostFileSystem(backends []MountedBackend, emitter syncdispatch.EventEmitter) *GhostFileSystem {
	dir := filepath.Join(os.TempDir(), "ghostdrive")
	_ = os.MkdirAll(dir, 0755)

	diniContent := buildDesktopIni()
	diniTmp := filepath.Join(dir, "desktop.ini")
	_ = os.WriteFile(diniTmp, diniContent, 0644)

	iconTmp := filepath.Join(dir, "ghostdrive.ico")
	_ = os.WriteFile(iconTmp, driveIconICO, 0644)

	// Determine cache TTL from first backend config (fallback: defaultMetaCacheTTL).
	cacheTTLMeta := defaultMetaCacheTTL
	if len(backends) > 0 {
		if v := backends[0].Config.Params["metaCacheTTL"]; v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				cacheTTLMeta = time.Duration(secs) * time.Second
			}
		}
	}

	if emitter == nil {
		emitter = &syncdispatch.NoopEmitter{}
	}

	return &GhostFileSystem{
		backends:      backends,
		watchContexts: make(map[string]context.CancelFunc),
		handles:       make(map[uint64]*openEntry),
		desktopIni:    diniContent,
		desktopIniTmp: diniTmp,
		iconTmp:       iconTmp,
		meta:          newMetaCache(cacheTTLMeta),
		emitter:       emitter,
	}
}

// buildDesktopIni returns the content for the virtual desktop.ini that tells
// Windows Explorer to use the embedded GhostDrive icon for the drive root.
// The icon is served as a virtual /ghostdrive.ico file within the same drive,
// so no absolute path or reference to the executable is needed.
func buildDesktopIni() []byte {
	return []byte("[.ShellClassInfo]\r\nIconFile=ghostdrive.ico\r\nIconIndex=0\r\n")
}

// ── Utility helpers ──────────────────────────────────────────────────────────

// remoteParent returns the parent directory component of a remote (FUSE/POSIX)
// path.  Uses strings to avoid shadowing the "path" package with local "path"
// parameters in method bodies.
func remoteParent(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return "/"
	}
	if idx == 0 {
		return "/"
	}
	return p[:idx]
}

// tsFromTime converts a time.Time to a fuse.Timespec.
func tsFromTime(t time.Time) fuse.Timespec {
	return fuse.Timespec{Sec: t.Unix(), Nsec: int64(t.Nanosecond())}
}

// nowTs returns a Timespec for the current time.
func nowTs() fuse.Timespec { return tsFromTime(time.Now()) }

// cachePath returns the canonical temp path for a given backend+remote path.
func cachePath(backendID, remotePath string) string {
	h := sha256.Sum256([]byte(backendID + "\x00" + remotePath))
	return filepath.Join(os.TempDir(), "ghostdrive",
		fmt.Sprintf("%x", h[:8]), filepath.Base(remotePath))
}

// isCacheFresh reports whether a cached file is younger than cacheTTL.
// A 0-byte file is always considered stale: it may result from an interrupted
// download or from a Create pre-upload before actual content was written.
// Re-downloading a genuinely 0-byte remote file is trivial (negligible cost).
func isCacheFresh(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0 && time.Since(info.ModTime()) < cacheTTL
}

// ensureDownloaded downloads remotePath via backend to a temp file, reusing a
// fresh cache entry when available.
func ensureDownloaded(cfg plugins.BackendConfig, backend plugins.StorageBackend, remotePath string) (string, error) {
	local := cachePath(cfg.ID, remotePath)
	if isCacheFresh(local) {
		return local, nil
	}
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return "", fmt.Errorf("placeholder: cache dir: %w", err)
	}
	if err := backend.Download(context.Background(), remotePath, local, nil); err != nil {
		return "", fmt.Errorf("placeholder: download %s: %w", remotePath, err)
	}
	// Log post-download cache size for diagnostic (helps detect 0-byte download issues).
	if fi, statErr := os.Stat(local); statErr == nil {
		logger.Debug("placeholder: ensureDownloaded %s → local size=%d", remotePath, fi.Size())
	}
	return local, nil
}

// ── Getattr ──────────────────────────────────────────────────────────────────

func (fs *GhostFileSystem) Getattr(path string, stat *fuse.Stat_t, _ uint64) int {
	now := nowTs()

	// Virtual root directory.
	// UF_READONLY signals Explorer to look for desktop.ini in this folder.
	if path == "/" {
		stat.Mode = fuse.S_IFDIR | 0755
		stat.Nlink = 2
		stat.Flags = fuse.UF_READONLY
		stat.Atim, stat.Mtim, stat.Ctim = now, now, now
		return 0
	}

	// Virtual desktop.ini — makes Windows Explorer show the GhostDrive icon.
	// Must carry FILE_ATTRIBUTE_SYSTEM (UF_SYSTEM) or Explorer ignores it.
	if strings.EqualFold(path, "/desktop.ini") {
		stat.Mode = fuse.S_IFREG | 0444
		stat.Nlink = 1
		stat.Size = int64(len(fs.desktopIni))
		stat.Flags = fuse.UF_HIDDEN | fuse.UF_SYSTEM
		stat.Atim, stat.Mtim, stat.Ctim = now, now, now
		return 0
	}

	// Virtual ghostdrive.ico — the embedded drive icon served from the FUSE root.
	if strings.EqualFold(path, "/ghostdrive.ico") {
		stat.Mode = fuse.S_IFREG | 0444
		stat.Nlink = 1
		stat.Size = int64(len(driveIconICO))
		stat.Atim, stat.Mtim, stat.Ctim = now, now, now
		return 0
	}

	// Virtual backend root directory: "/<backendName>" maps to a synthetic
	// S_IFDIR that does not exist on the backend itself.
	// This check runs before route() so that Getattr("/<name>") always
	// returns a valid directory stat even before any file inside it is listed.
	trimmed := strings.TrimLeft(path, "/")
	if !strings.Contains(trimmed, "/") && trimmed != "" {
		// Snapshot backends under RLock — safe to iterate after unlock because
		// UpdateBackends always replaces the slice (never modifies in place).
		fs.backendsMu.RLock()
		backends := fs.backends
		fs.backendsMu.RUnlock()
		for _, mb := range backends {
			if strings.EqualFold(mb.Name, trimmed) {
				stat.Mode = fuse.S_IFDIR | 0755
				stat.Nlink = 2
				stat.Atim, stat.Mtim, stat.Ctim = now, now, now
				return 0
			}
		}
		// Unknown first-level name — fall through to backend or ENOENT.
	}

	r := fs.route(path)
	if r == nil {
		return -fuse.ENOENT
	}

	cacheKey := r.config.ID + ":" + r.relPath
	var info *plugins.FileInfo
	if cached, hit := fs.meta.getStat(cacheKey); hit {
		info = cached
	} else {
		var err error
		info, err = r.backend.Stat(context.Background(), r.relPath)
		if err != nil {
			logger.Debug("[placeholder/getattr] path=%q stat_err=%q", path, err.Error())
			if errors.Is(err, plugins.ErrFileNotFound) {
				logger.Debug("[placeholder/getattr] path=%q → ENOENT (errors.Is match)", path)
				return -fuse.ENOENT
			}
			if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such") {
				logger.Debug("[placeholder/getattr] path=%q → ENOENT (string match)", path)
				return -fuse.ENOENT
			}
			logger.Error("[placeholder/getattr] path=%q → EIO (unmatched err=%q)", path, err.Error())
			return -fuse.EIO
		}
		if info == nil {
			logger.Debug("[placeholder/getattr] path=%q → ENOENT (nil info)", path)
			return -fuse.ENOENT
		}
		fs.meta.putStat(cacheKey, info)
	}

	mts := tsFromTime(info.ModTime)
	if info.IsDir {
		stat.Mode = fuse.S_IFDIR | 0755
		stat.Nlink = 2
	} else {
		stat.Mode = fuse.S_IFREG | 0644
		stat.Nlink = 1
		stat.Size = info.Size
	}
	stat.Atim, stat.Mtim, stat.Ctim = mts, mts, mts
	return 0
}

// ── Readdir ──────────────────────────────────────────────────────────────────

func (fs *GhostFileSystem) Readdir(path string,
	fill func(name string, stat *fuse.Stat_t, ofst int64) bool,
	_ int64, _ uint64) int {

	fill(".", nil, 0)
	fill("..", nil, 0)

	// Root lists virtual files plus one synthetic sub-directory per backend.
	// In v2.0 the drive root never exposes backend file contents directly;
	// every backend is accessible under its own named sub-folder.
	if path == "/" {
		now := nowTs()
		// Virtual desktop.ini and ghostdrive.ico for Windows Explorer drive icon.
		// desktop.ini needs FILE_ATTRIBUTE_SYSTEM (via UF_SYSTEM) for Explorer to process it.
		diniSt := &fuse.Stat_t{Mode: fuse.S_IFREG | 0444, Nlink: 1, Size: int64(len(fs.desktopIni)),
			Flags: fuse.UF_HIDDEN | fuse.UF_SYSTEM}
		diniSt.Atim, diniSt.Mtim, diniSt.Ctim = now, now, now
		fill("desktop.ini", diniSt, 0)
		icoSt := &fuse.Stat_t{Mode: fuse.S_IFREG | 0444, Nlink: 1, Size: int64(len(driveIconICO))}
		icoSt.Atim, icoSt.Mtim, icoSt.Ctim = now, now, now
		fill("ghostdrive.ico", icoSt, 0)
		// One virtual S_IFDIR per registered backend.
		// Snapshot under RLock — safe to iterate after unlock (slice is replaced, not mutated).
		fs.backendsMu.RLock()
		backends := fs.backends
		fs.backendsMu.RUnlock()
		for _, mb := range backends {
			dirSt := &fuse.Stat_t{Mode: fuse.S_IFDIR | 0755, Nlink: 2}
			dirSt.Atim, dirSt.Mtim, dirSt.Ctim = now, now, now
			if !fill(mb.Name, dirSt, 0) {
				break
			}
		}
		return 0
	}

	r := fs.route(path)
	if r == nil {
		return -fuse.ENOENT
	}

	cacheKey := r.config.ID + ":" + r.relPath
	var entries []plugins.FileInfo
	if cached, hit := fs.meta.getList(cacheKey); hit {
		entries = cached
	} else {
		var err error
		entries, err = r.backend.List(context.Background(), r.relPath)
		if err != nil {
			logger.Error("placeholder: Readdir %s: %v", path, err)
			return -fuse.EIO
		}
		fs.meta.putList(cacheKey, entries)
	}

	now := nowTs()
	for _, e := range entries {
		st := &fuse.Stat_t{}
		mts := tsFromTime(e.ModTime)
		if e.IsDir {
			st.Mode = fuse.S_IFDIR | 0755
			st.Nlink = 2
		} else {
			st.Mode = fuse.S_IFREG | 0644
			st.Nlink = 1
			st.Size = e.Size
		}
		st.Atim, st.Mtim, st.Ctim = mts, mts, now
		if !fill(e.Name, st, 0) {
			break
		}
	}
	return 0
}

// ── Open / Read / Write / Release ────────────────────────────────────────────

func (fs *GhostFileSystem) Open(path string, flags int) (int, uint64) {
	// Virtual read-only files at the drive root — served from pre-written temp files.
	if strings.EqualFold(path, "/desktop.ini") {
		fh := fs.fhSeq.Add(1)
		fs.mu.Lock()
		fs.handles[fh] = &openEntry{tempPath: fs.desktopIniTmp, writeable: false}
		fs.mu.Unlock()
		return 0, fh
	}
	if strings.EqualFold(path, "/ghostdrive.ico") {
		fh := fs.fhSeq.Add(1)
		fs.mu.Lock()
		fs.handles[fh] = &openEntry{tempPath: fs.iconTmp, writeable: false}
		fs.mu.Unlock()
		return 0, fh
	}

	r := fs.route(path)
	if r == nil {
		return -fuse.ENOENT, ^uint64(0)
	}

	writeable := (flags&fuseOWronly != 0) || (flags&fuseORdwr != 0)

	// Allocate the file handle first so the value is used consistently for
	// both the temp-dir name and the handle key — prevents races between
	// concurrent Open() calls reading the same fhSeq before it increments.
	fh := fs.fhSeq.Add(1)
	logger.Debug("placeholder: Open fh=%d path=%s writeable=%v", fh, path, writeable)

	var localPath string
	if writeable {
		// Prepare a temp dir using the allocated fh (race-free).
		dir := filepath.Join(os.TempDir(), "ghostdrive-write", fmt.Sprintf("%d", fh))
		if err := os.MkdirAll(dir, 0755); err != nil {
			return -fuse.EIO, ^uint64(0)
		}
		localPath = filepath.Join(dir, filepath.Base(path))
		// Pre-populate with existing backend content so that partial writes
		// (O_RDWR) do not silently discard bytes outside the written range.
		// Stat() failing means the file is new — proceed with an empty temp.
		if _, err := r.backend.Stat(context.Background(), r.relPath); err == nil {
			if dlErr := r.backend.Download(context.Background(), r.relPath, localPath, nil); dlErr != nil {
				logger.Warn("placeholder: Open pre-download %s: %v", path, dlErr)
				// Non-fatal: full-overwrite writes still work correctly.
			}
		}
	} else {
		// Lazy download for read-only access.
		var err error
		localPath, err = ensureDownloaded(r.config, r.backend, r.relPath)
		if err != nil {
			logger.Error("placeholder: Open %s: %v", path, err)
			return -fuse.EIO, ^uint64(0)
		}
	}

	fs.mu.Lock()
	fs.handles[fh] = &openEntry{tempPath: localPath, writeable: writeable}
	fs.mu.Unlock()
	return 0, fh
}

func (fs *GhostFileSystem) Read(path string, buff []byte, ofst int64, fh uint64) int {
	fs.mu.Lock()
	entry, ok := fs.handles[fh]
	fs.mu.Unlock()
	if !ok {
		return -fuse.EBADF
	}

	f, err := os.Open(entry.tempPath)
	if err != nil {
		logger.Error("placeholder: Read open temp %s: %v", path, err)
		return -fuse.EIO
	}
	defer f.Close()

	if _, err := f.Seek(ofst, io.SeekStart); err != nil {
		return -fuse.EIO
	}
	n, err := f.Read(buff)
	if err != nil && err != io.EOF {
		return -fuse.EIO
	}
	return n
}

func (fs *GhostFileSystem) Write(path string, buff []byte, ofst int64, fh uint64) int {
	fs.mu.Lock()
	entry, ok := fs.handles[fh]
	if ok && entry.writeable {
		entry.dirty = true
	}
	fs.mu.Unlock()
	if !ok || !entry.writeable {
		return -fuse.EBADF
	}

	logger.Debug("placeholder: Write path=%q ofst=%d len=%d fh=%d", path, ofst, len(buff), fh)

	f, err := os.OpenFile(entry.tempPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Error("placeholder: Write open temp %s: %v", path, err)
		return -fuse.EIO
	}
	defer f.Close()

	if _, err := f.Seek(ofst, io.SeekStart); err != nil {
		logger.Error("placeholder: Write seek %s ofst=%d: %v", path, ofst, err)
		return -fuse.EIO
	}
	n, err := f.Write(buff)
	if err != nil {
		logger.Error("placeholder: Write write %s: %v", path, err)
		return -fuse.EIO
	}
	return n
}

// notifyUpload sends WinFsp file-change notifications so Windows Explorer
// refreshes the file's size and timestamp automatically after an upload,
// without requiring a manual F5.
//
// NOTIFY_TRUNCATE signals that the file length has changed (maps to
// FILE_NOTIFY_CHANGE_SIZE in the Windows ReadDirectoryChanges API).
// NOTIFY_UTIME signals that the modification time has changed.
// Together they cause Explorer to re-query Getattr and display the correct size.
//
// Non-fatal: if the WinFsp host is not available or Notify fails the upload
// is still considered successful; the user can press F5 to refresh.
func (fs *GhostFileSystem) notifyUpload(path string) {
	if fs.host == nil {
		return
	}
	ok := fs.host.Notify(path, fuse.NOTIFY_TRUNCATE|fuse.NOTIFY_UTIME)
	if ok {
		logger.Debug("placeholder: notifyUpload: Notify(%s) sent", path)
	} else {
		logger.Warn("placeholder: notifyUpload: Notify(%s) failed (WinFsp notify unavailable)", path)
	}
}

// notifyRootChanged sends WinFsp NOTIFY_UNLINK / NOTIFY_CREATE notifications
// for backends that were removed from or added to the unified drive root after
// an UpdateBackends() call.  This causes Windows Explorer to refresh GhD:\
// automatically without requiring an F5 from the user (#132).
//
// Uses the same host.Notify() mechanism as notifyUpload and handleWatchEvent
// — no additional imports required.  Non-fatal: if the host is unavailable
// the routing table is still up-to-date; the user can press F5.
func (fs *GhostFileSystem) notifyRootChanged(oldBackends, newBackends []MountedBackend) {
	if fs.host == nil {
		return
	}

	oldIDs := make(map[string]bool, len(oldBackends))
	for _, mb := range oldBackends {
		oldIDs[mb.ID] = true
	}
	newIDs := make(map[string]bool, len(newBackends))
	for _, mb := range newBackends {
		newIDs[mb.ID] = true
	}

	// NOTIFY_UNLINK for backends removed from the root — Explorer removes the
	// sub-folder from its directory listing.
	for _, mb := range oldBackends {
		if !newIDs[mb.ID] {
			path := "/" + mb.Name
			ok := fs.host.Notify(path, fuse.NOTIFY_UNLINK)
			logger.Debug("placeholder: notifyRootChanged: UNLINK %s ok=%v", path, ok)
		}
	}

	// NOTIFY_CREATE for backends newly added to the root — Explorer adds the
	// sub-folder to its directory listing.
	for _, mb := range newBackends {
		if !oldIDs[mb.ID] {
			path := "/" + mb.Name
			ok := fs.host.Notify(path, fuse.NOTIFY_CREATE)
			logger.Debug("placeholder: notifyRootChanged: CREATE %s ok=%v", path, ok)
		}
	}
}

func (fs *GhostFileSystem) Release(path string, fh uint64) int {
	fs.mu.Lock()
	entry, ok := fs.handles[fh]
	if ok {
		delete(fs.handles, fh)
	}
	fs.mu.Unlock()

	if !ok {
		return 0
	}

	// Upload temp file to backend only if Write() was called (dirty flag).
	// Windows Explorer uses a two-phase copy: Create+Release (0 bytes) then
	// Open+Write+Release (actual content). Skipping the dirty=false phase
	// prevents the 0-byte Create from overwriting the file on the remote.
	if entry.writeable && entry.dirty {
		r := fs.route(path)
		if r != nil {
			// Log temp file size before upload to diagnose 0-byte uploads.
			if fi, statErr := os.Stat(entry.tempPath); statErr == nil {
				logger.Info("placeholder: Release fh=%d path=%s tempSize=%d", fh, path, fi.Size())
			} else {
				logger.Warn("placeholder: Release fh=%d path=%s stat-err=%v", fh, path, statErr)
			}
			// These two DEBUG logs are in the main process (not plugin subprocess)
			// and will always appear in the UI log, regardless of GHOSTDRIVE_DEBUG.
			// If "starting upload" appears but "upload returned" does not, Upload() hangs.
			logger.Debug("placeholder: Release fh=%d starting upload to %s", fh, r.relPath)
			uploadErr := r.backend.Upload(context.Background(), entry.tempPath, r.relPath, nil)
			logger.Debug("placeholder: Release fh=%d upload returned: %v", fh, uploadErr)
			if uploadErr != nil {
				logger.Error("placeholder: Release upload %s: %v", path, uploadErr)
			} else {
				// Post-upload Stat: retry up to 3 times with 100ms delay to handle
				// MooseFS async metadata propagation — GetAttr may return size=0
				// briefly after the last WRITE_CHUNK_END is committed.
				var fi *plugins.FileInfo
				var statErr error
				for attempt := 0; attempt < 3; attempt++ {
					fi, statErr = r.backend.Stat(context.Background(), r.relPath)
					if statErr == nil && fi.Size > 0 {
						break
					}
					if attempt < 2 {
						if statErr == nil {
							logger.Warn("placeholder: Release post-upload stat %s size=0 (attempt %d/3), retrying…", path, attempt+1)
						}
						time.Sleep(100 * time.Millisecond)
					}
				}
				if statErr == nil {
					logger.Info("placeholder: Release post-upload stat %s size=%d", path, fi.Size)
				} else {
					logger.Warn("placeholder: Release post-upload stat failed %s: %v", path, statErr)
				}
				// Invalidate cache for this file and its parent directory so that
				// subsequent Getattr/Readdir reflect the uploaded content immediately.
				fs.meta.invalidate(r.config.ID + ":" + r.relPath)
				fs.meta.invalidate(r.config.ID + ":" + remoteParent(r.relPath))
				// Notify WinFsp/Explorer that the file size changed so Explorer
				// refreshes its view without requiring a manual F5 (issue #103).
				fs.notifyUpload(path)
			}
		}
		_ = os.Remove(entry.tempPath)
	}
	// Read-only cache files are kept on disk until the TTL expires.
	return 0
}

// ── Create ───────────────────────────────────────────────────────────────────

func (fs *GhostFileSystem) Create(path string, _ int, _ uint32) (int, uint64) {
	r := fs.route(path)
	if r == nil {
		return -fuse.ENOENT, ^uint64(0)
	}

	// Allocate fh first to ensure the dir name is unique and race-free.
	fh := fs.fhSeq.Add(1)
	logger.Debug("placeholder: Create fh=%d path=%s", fh, path)
	dir := filepath.Join(os.TempDir(), "ghostdrive-write", fmt.Sprintf("c%d", fh))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return -fuse.EIO, ^uint64(0)
	}
	localPath := filepath.Join(dir, filepath.Base(path))

	// Pre-create the local empty temp file.
	if f, err := os.Create(localPath); err == nil {
		f.Close()
	}

	// Upload the 0-byte file immediately so Getattr returns OK right after Create.
	// Windows Explorer calls Getattr to confirm the file was created; without this
	// upload the file doesn't exist on the remote yet and Explorer shows ENOENT.
	// Release with dirty=false will skip re-uploading; Release with dirty=true
	// (after actual writes) will unlink + re-upload with the real content.
	if err := r.backend.Upload(context.Background(), localPath, r.relPath, nil); err != nil {
		logger.Warn("placeholder: Create pre-upload %s: %v", path, err)
		// Non-fatal: Getattr may briefly return ENOENT but writes will still work.
	}
	// Invalidate parent directory listing so Readdir includes the new entry.
	fs.meta.invalidate(r.config.ID + ":" + remoteParent(r.relPath))
	// Notify the frontend so it can refresh directory listings (#116).
	logger.Debug("placeholder: Create emitting meta:updated for backend=%s path=%s emitter=%T",
		r.config.ID, r.relPath, fs.emitter)
	if fs.emitter != nil {
		fs.emitter.Emit("meta:updated", MetaUpdatedEvent{
			BackendID: r.config.ID,
			Path:      r.relPath,
			EventType: "created",
		})
	}

	fs.mu.Lock()
	fs.handles[fh] = &openEntry{tempPath: localPath, writeable: true}
	fs.mu.Unlock()
	return 0, fh
}

// ── Unlink / Rename / Mkdir ──────────────────────────────────────────────────

func (fs *GhostFileSystem) Unlink(path string) int {
	r := fs.route(path)
	if r == nil {
		return -fuse.ENOENT
	}
	if err := r.backend.Delete(context.Background(), r.relPath); err != nil {
		logger.Error("placeholder: Unlink %s: %v", path, err)
		return -fuse.EIO
	}
	// Invalidate file and parent directory cache immediately.
	fs.meta.invalidate(r.config.ID + ":" + r.relPath)
	fs.meta.invalidate(r.config.ID + ":" + remoteParent(r.relPath))
	// Notify the frontend so it can refresh directory listings (#116).
	if fs.emitter != nil {
		fs.emitter.Emit("meta:updated", MetaUpdatedEvent{
			BackendID: r.config.ID,
			Path:      r.relPath,
			EventType: "deleted",
		})
	}
	return 0
}

func (fs *GhostFileSystem) Rename(oldpath, newpath string) (errc int) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("placeholder: Rename panic %q → %q: %v", oldpath, newpath, r)
			errc = -fuse.EIO
		}
	}()
	logger.Info("placeholder: Rename entry %q → %q", oldpath, newpath)
	ro := fs.route(oldpath)
	rn := fs.route(newpath)
	if ro == nil || rn == nil {
		return -fuse.ENOENT
	}
	// Cross-backend rename is unsupported.
	if ro.backend != rn.backend {
		return -fuse.EXDEV
	}
	if err := ro.backend.Move(context.Background(), ro.relPath, rn.relPath); err != nil {
		logger.Error("placeholder: Rename %q → %q relPaths=%q→%q: %v",
			oldpath, newpath, ro.relPath, rn.relPath, err)
		return -fuse.EIO
	}
	// Invalidate old and new paths plus their parent directories.
	fs.meta.invalidate(ro.config.ID + ":" + ro.relPath)
	fs.meta.invalidate(ro.config.ID + ":" + remoteParent(ro.relPath))
	fs.meta.invalidate(rn.config.ID + ":" + rn.relPath)
	fs.meta.invalidate(rn.config.ID + ":" + remoteParent(rn.relPath))
	// Notify the frontend so it can refresh affected directory listings (#116).
	if fs.emitter != nil {
		fs.emitter.Emit("meta:updated", MetaUpdatedEvent{
			BackendID: rn.config.ID,
			Path:      rn.relPath,
			EventType: "renamed",
		})
	}
	logger.Info("placeholder: Rename success %q → %q", oldpath, newpath)
	return 0
}

// Rename3 implements fuse.FileSystemRename3 for FUSE3 / WinFsp compatibility.
// WinFsp uses the 3-param FUSE3 rename variant (with flags) on the CGO build.
// Without this, cgofuse returns -EINVAL for any non-zero flags without calling Rename.
func (fs *GhostFileSystem) Rename3(oldpath, newpath string, flags uint32) int {
	logger.Debug("[placeholder/rename3] entry oldpath=%q newpath=%q flags=%#x", oldpath, newpath, flags)
	if flags&fuse.RENAME_EXCHANGE != 0 {
		return -fuse.EINVAL
	}
	return fs.Rename(oldpath, newpath)
}

func (fs *GhostFileSystem) Mkdir(path string, _ uint32) int {
	r := fs.route(path)
	if r == nil {
		return -fuse.ENOENT
	}
	if err := r.backend.CreateDir(context.Background(), r.relPath); err != nil {
		logger.Error("placeholder: Mkdir %s: %v", path, err)
		return -fuse.EIO
	}
	// Invalidate parent directory listing so Explorer shows the new folder.
	fs.meta.invalidate(r.config.ID + ":" + remoteParent(r.relPath))
	// Notify the frontend so it can refresh directory listings (#116).
	if fs.emitter != nil {
		fs.emitter.Emit("meta:updated", MetaUpdatedEvent{
			BackendID: r.config.ID,
			Path:      r.relPath,
			EventType: "created",
		})
	}
	return 0
}

func (fs *GhostFileSystem) Statfs(_ string, stat *fuse.Statfs_t) int {
	// Snapshot under RLock — safe to iterate after unlock (slice replaced, not mutated).
	fs.backendsMu.RLock()
	backends := fs.backends
	fs.backendsMu.RUnlock()

	if len(backends) == 0 {
		// No backends: report a large but finite virtual filesystem.
		stat.Bsize = 4096
		stat.Frsize = 4096
		stat.Blocks = 1 << 40 / 4096
		stat.Bfree = 1 << 39 / 4096
		stat.Bavail = stat.Bfree
		return 0
	}

	// Aggregate free/total across all backends.
	var totalFree, totalTotal int64
	anyValid := false
	for _, mb := range backends {
		free, total, err := mb.Backend.GetQuota(context.Background())
		if err != nil || total <= 0 {
			continue
		}
		totalFree += free
		totalTotal += total
		anyValid = true
	}

	if !anyValid {
		stat.Bsize = 4096
		stat.Frsize = 4096
		stat.Blocks = 1 << 40 / 4096
		stat.Bfree = 1 << 39 / 4096
		stat.Bavail = stat.Bfree
		return 0
	}

	bsize := uint64(4096)
	stat.Bsize = bsize
	stat.Frsize = bsize
	stat.Blocks = uint64(totalTotal) / bsize
	stat.Bfree = uint64(totalFree) / bsize
	stat.Bavail = stat.Bfree
	return 0
}

// ── Watch-based push invalidation ────────────────────────────────────────────

// startWatchLoops records ctx as the root context for all watchLoop goroutines
// and starts one goroutine per backend already in fs.backends.
// Called once by WinFspDrive.Mount() before FUSE dispatch starts.
func (fs *GhostFileSystem) startWatchLoops(ctx context.Context) {
	fs.backendsMu.Lock()
	fs.watchBaseCtx = ctx
	backends := make([]MountedBackend, len(fs.backends))
	copy(backends, fs.backends)
	fs.backendsMu.Unlock()

	// updateWatchLoops with an empty old list starts goroutines for all backends.
	fs.updateWatchLoops(nil, backends)
}

// updateWatchLoops diffs oldBackends vs newBackends and:
//   - cancels watchLoop goroutines for backends removed from the list,
//   - starts new watchLoop goroutines for backends that were just added.
//
// Must be called AFTER fs.backends has already been updated with newBackends.
// Safe to call from any goroutine; holds backendsMu only while manipulating
// the watchContexts map (not during backend I/O or goroutine startup).
func (fs *GhostFileSystem) updateWatchLoops(oldBackends, newBackends []MountedBackend) {
	oldIDs := make(map[string]bool, len(oldBackends))
	for _, mb := range oldBackends {
		oldIDs[mb.ID] = true
	}
	newIDs := make(map[string]bool, len(newBackends))
	for _, mb := range newBackends {
		newIDs[mb.ID] = true
	}

	// pendingStart collects (backend, context) pairs for goroutines that must
	// be started OUTSIDE the lock to avoid lock-order inversion.
	type pendingStart struct {
		mb  MountedBackend
		ctx context.Context
	}
	var toStart []pendingStart

	fs.backendsMu.Lock()
	// Cancel goroutines for backends no longer in the list.
	for _, mb := range oldBackends {
		if !newIDs[mb.ID] {
			if cancel, ok := fs.watchContexts[mb.ID]; ok {
				cancel()
				delete(fs.watchContexts, mb.ID)
			}
		}
	}
	// Create child contexts for newly added backends.
	for _, mb := range newBackends {
		if !oldIDs[mb.ID] {
			if fs.watchBaseCtx == nil {
				// Mount has not been called yet — goroutine will be started by
				// startWatchLoops; nothing to do here.
				continue
			}
			ctx, cancel := context.WithCancel(fs.watchBaseCtx)
			fs.watchContexts[mb.ID] = cancel
			toStart = append(toStart, pendingStart{mb, ctx})
		}
	}
	fs.backendsMu.Unlock()

	// Start goroutines outside the lock.
	for _, p := range toStart {
		go fs.watchLoop(p.ctx, p.mb)
	}
}

// watchLoop is a plugin-agnostic retry loop for backend.Watch().
// It calls Watch() repeatedly, tracking consecutive failures, and propagates
// backend reachability state to the tray via the filesystem emitter (#118):
//
//   - 1st consecutive failure (Watch error or unexpected channel close):
//     emit "sync:offline" → tray turns orange
//   - Nth failure (≥ watchErrThreshold):
//     emit "sync:error"  → tray turns red
//   - Recovery (Watch succeeds after prior failures):
//     emit "sync:online" → tray turns green
//
// Detection is purely structural: it monitors Watch() return values and channel
// lifetime, never relying on plugin-emitted sentinel events.  Plugin-level
// backoff (e.g. WebDAV exponential retry) is transparent and orthogonal.
// Plugin sentinel events (FileEventBackendOffline/Online) are silently skipped
// so they do not bypass the counter logic.
//
// The loop exits only when ctx is cancelled (normal shutdown / unmount).
func (fs *GhostFileSystem) watchLoop(ctx context.Context, mb MountedBackend) {
	const (
		watchErrThreshold   = 5
		watchBackoffInitial = 2 * time.Second
		watchBackoffMax     = 60 * time.Second
	)

	logger.Info("placeholder: watchLoop started for backend %s", mb.ID)

	consecutive := 0
	backoffDelay := watchBackoffInitial

	for ctx.Err() == nil {
		ch, err := mb.Backend.Watch(ctx, "/")
		if err != nil || ch == nil {
			consecutive++
			fs.emitWatchReachability(mb.ID, consecutive, watchErrThreshold,
				fmt.Sprintf("Watch() unavailable: %v", err))
			logger.Warn("placeholder: watchLoop(%s): Watch() failed (attempt %d): %v — retry in %s",
				mb.ID, consecutive, err, backoffDelay)
			fs.watchSleep(ctx, &backoffDelay, watchBackoffMax)
			continue
		}

		logger.Info("placeholder: watchLoop(%s): Watch() active (attempt %d — consecutive resets to 0)",
			mb.ID, consecutive+1)

		// Watch() call succeeded — emit sync:online if recovering from prior failures.
		if consecutive > 0 {
			consecutive = 0
			backoffDelay = watchBackoffInitial
			if fs.emitter != nil {
				fs.emitter.Emit("sync:online", map[string]any{"backendID": mb.ID})
			}
		}

		// Drain events until channel closes or ctx is cancelled.
		channelOpen := true
		for channelOpen {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					// Channel closed unexpectedly — count as failure and retry Watch().
					channelOpen = false
					consecutive++
					fs.emitWatchReachability(mb.ID, consecutive, watchErrThreshold,
						"Watch() channel closed unexpectedly")
					logger.Warn("placeholder: watchLoop(%s): Watch() channel closed (attempt %d) — retry in %s",
						mb.ID, consecutive, backoffDelay)
					fs.watchSleep(ctx, &backoffDelay, watchBackoffMax)
					continue
				}
				// Skip plugin-emitted sentinel events — watchLoop does its own
				// plugin-agnostic reachability detection; sentinels would double-count.
				if ev.Type == plugins.FileEventBackendOffline ||
					ev.Type == plugins.FileEventBackendOnline {
					continue
				}
				fs.handleWatchEvent(mb, ev)
			}
		}
	}
}

// emitWatchReachability emits the appropriate reachability event based on the
// consecutive failure count relative to the threshold.
// It is a no-op when the emitter is nil or when consecutive is between 2 and
// threshold−1 (transient failure already signalled on the first occurrence).
func (fs *GhostFileSystem) emitWatchReachability(backendID string, consecutive, threshold int, detail string) {
	if fs.emitter == nil {
		return
	}
	switch {
	case consecutive == 1:
		// First failure — transient; signal tray to turn orange.
		fs.emitter.Emit("sync:offline", map[string]any{"backendID": backendID})
	case consecutive >= threshold:
		// Persistent failure — signal tray to turn red.
		fs.emitter.Emit("sync:error", map[string]any{
			"backendID": backendID,
			"message":   fmt.Sprintf("backend unreachable after %d Watch() failures: %s", consecutive, detail),
		})
	}
}

// watchSleep waits for the current backoff duration or until ctx is cancelled,
// then doubles the delay (up to max).
func (fs *GhostFileSystem) watchSleep(ctx context.Context, delay *time.Duration, maxDelay time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(*delay):
	}
	*delay *= 2
	if *delay > maxDelay {
		*delay = maxDelay
	}
}

// toFUSEPath ensures path starts with "/" as required by both the metacache
// (which uses FUSE-format keys like "id:/foo/bar") and host.Notify()
// (which passes the path verbatim to fsp_fuse_notify).
//
// MooseFS and WebDAV List() strip leading slashes via strings.TrimLeft so
// Watch() events arrive as "foo/bar" instead of "/foo/bar". Without this
// normalisation, host.Notify() would silently no-op and cache invalidation
// would use the wrong key.
func toFUSEPath(p string) string {
	if p == "" || p[0] == '/' {
		return p
	}
	return "/" + p
}

// handleWatchEvent invalidates the affected cache entries, notifies WinFsp/
// Explorer so the virtual drive (GhD:) refreshes automatically, and emits a
// "meta:updated" Wails event so the frontend can refresh its file listings.
func (fs *GhostFileSystem) handleWatchEvent(mb MountedBackend, ev plugins.FileEvent) {
	// Normalise event paths to FUSE format (must start with "/").
	// MooseFS/WebDAV Watch() events use paths without a leading "/" (from List())
	// but both the metacache and host.Notify() require FUSE-format paths.
	fusePath := toFUSEPath(ev.Path)
	fuseOldPath := toFUSEPath(ev.OldPath)

	// Invalidate the changed path and its parent directory.
	key := mb.ID + ":" + fusePath
	parentKey := mb.ID + ":" + remoteParent(fusePath)
	fs.meta.invalidate(key)
	fs.meta.invalidate(parentKey)

	// For renames, also invalidate the old path and its parent.
	if fuseOldPath != "" {
		fs.meta.invalidate(mb.ID + ":" + fuseOldPath)
		fs.meta.invalidate(mb.ID + ":" + remoteParent(fuseOldPath))
	}

	// Notify WinFsp/Explorer so the virtual drive refreshes its directory
	// listing without requiring a manual F5 (#119 — remote changes visible in GhD:).
	// NOTIFY_CREATE/UNLINK → FILE_NOTIFY_CHANGE_FILE_NAME (new/deleted entry).
	// NOTIFY_TRUNCATE|NOTIFY_UTIME → FILE_NOTIFY_CHANGE_SIZE|LAST_WRITE (modified).
	// path = changed item; WinFsp routes the notification to the watching parent.
	if fs.host != nil {
		switch ev.Type {
		case plugins.FileEventCreated:
			ok := fs.host.Notify(fusePath, fuse.NOTIFY_CREATE)
			logger.Debug("placeholder: handleWatchEvent: Notify CREATE %s ok=%v", fusePath, ok)
		case plugins.FileEventDeleted:
			ok := fs.host.Notify(fusePath, fuse.NOTIFY_UNLINK)
			logger.Debug("placeholder: handleWatchEvent: Notify UNLINK %s ok=%v", fusePath, ok)
		case plugins.FileEventModified:
			ok := fs.host.Notify(fusePath, fuse.NOTIFY_TRUNCATE|fuse.NOTIFY_UTIME)
			logger.Debug("placeholder: handleWatchEvent: Notify MODIFIED %s ok=%v", fusePath, ok)
		case plugins.FileEventRenamed:
			if fuseOldPath != "" {
				fs.host.Notify(fuseOldPath, fuse.NOTIFY_UNLINK)
			}
			ok := fs.host.Notify(fusePath, fuse.NOTIFY_CREATE)
			logger.Debug("placeholder: handleWatchEvent: Notify RENAME %q→%q ok=%v", fuseOldPath, fusePath, ok)
		}
	}

	// Notify the frontend so it can refresh affected directory listings.
	// Use fusePath so RemoteFileList.tsx path-matching works correctly.
	if fs.emitter != nil {
		fs.emitter.Emit("meta:updated", MetaUpdatedEvent{
			BackendID: mb.ID,
			Path:      fusePath,
			EventType: string(ev.Type),
		})
	}
}
