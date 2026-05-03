//go:build windows

package placeholder

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

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
}

// GhostFileSystem implements fuse.FileSystemInterface by routing calls to the
// appropriate StorageBackend based on the path prefix /<BackendName>/...
type GhostFileSystem struct {
	fuse.FileSystemBase
	backends []MountedBackend

	mu            gosync.Mutex
	fhSeq         atomic.Uint64
	handles       map[uint64]*openEntry
	desktopIni    []byte // content of the virtual /desktop.ini
	desktopIniTmp string // temp path for desktop.ini reads
	iconTmp       string // temp path for ghostdrive.ico reads
}

func newGhostFileSystem(backends []MountedBackend) *GhostFileSystem {
	dir := filepath.Join(os.TempDir(), "ghostdrive")
	_ = os.MkdirAll(dir, 0755)

	diniContent := buildDesktopIni()
	diniTmp := filepath.Join(dir, "desktop.ini")
	_ = os.WriteFile(diniTmp, diniContent, 0644)

	iconTmp := filepath.Join(dir, "ghostdrive.ico")
	_ = os.WriteFile(iconTmp, driveIconICO, 0644)

	return &GhostFileSystem{
		backends:      backends,
		handles:       make(map[uint64]*openEntry),
		desktopIni:    diniContent,
		desktopIniTmp: diniTmp,
		iconTmp:       iconTmp,
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
func isCacheFresh(path string) bool {
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()) < cacheTTL
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

	r := fs.route(path)
	if r == nil {
		return -fuse.ENOENT
	}

	info, err := r.backend.Stat(context.Background(), r.relPath)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such") {
			return -fuse.ENOENT
		}
		log.Printf("placeholder: Getattr %s: %v", path, err)
		return -fuse.EIO
	}
	if info == nil {
		return -fuse.ENOENT
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

	// Root lists virtual files plus the backend's own root content.
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
		if len(fs.backends) == 0 {
			return 0
		}
		entries, err := fs.backends[0].Backend.List(context.Background(), "/")
		if err != nil {
			log.Printf("placeholder: Readdir /: %v", err)
			return -fuse.EIO
		}
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

	r := fs.route(path)
	if r == nil {
		return -fuse.ENOENT
	}

	entries, err := r.backend.List(context.Background(), r.relPath)
	if err != nil {
		log.Printf("placeholder: Readdir %s: %v", path, err)
		return -fuse.EIO
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
				log.Printf("placeholder: Open pre-download %s: %v", path, dlErr)
				// Non-fatal: full-overwrite writes still work correctly.
			}
		}
	} else {
		// Lazy download for read-only access.
		var err error
		localPath, err = ensureDownloaded(r.config, r.backend, r.relPath)
		if err != nil {
			log.Printf("placeholder: Open %s: %v", path, err)
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
		log.Printf("placeholder: Read open temp %s: %v", path, err)
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
	fs.mu.Unlock()
	if !ok || !entry.writeable {
		return -fuse.EBADF
	}

	f, err := os.OpenFile(entry.tempPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return -fuse.EIO
	}
	defer f.Close()

	if _, err := f.Seek(ofst, io.SeekStart); err != nil {
		return -fuse.EIO
	}
	n, err := f.Write(buff)
	if err != nil {
		return -fuse.EIO
	}
	return n
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

	// Upload temp file to backend on close of a writeable handle.
	if entry.writeable {
		r := fs.route(path)
		if r != nil {
			if err := r.backend.Upload(context.Background(), entry.tempPath, r.relPath, nil); err != nil {
				log.Printf("placeholder: Release upload %s: %v", path, err)
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
	dir := filepath.Join(os.TempDir(), "ghostdrive-write", fmt.Sprintf("c%d", fh))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return -fuse.EIO, ^uint64(0)
	}
	localPath := filepath.Join(dir, filepath.Base(path))

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
		log.Printf("placeholder: Unlink %s: %v", path, err)
		return -fuse.EIO
	}
	return 0
}

func (fs *GhostFileSystem) Rename(oldpath, newpath string) int {
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
		log.Printf("placeholder: Rename %s → %s: %v", oldpath, newpath, err)
		return -fuse.EIO
	}
	return 0
}

func (fs *GhostFileSystem) Mkdir(path string, _ uint32) int {
	r := fs.route(path)
	if r == nil {
		return -fuse.ENOENT
	}
	if err := r.backend.CreateDir(context.Background(), r.relPath); err != nil {
		log.Printf("placeholder: Mkdir %s: %v", path, err)
		return -fuse.EIO
	}
	return 0
}
