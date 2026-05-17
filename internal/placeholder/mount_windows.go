//go:build windows

package placeholder

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"time"

	syncdispatch "github.com/CCoupel/GhostDrive/internal/sync"
	"github.com/winfsp/cgofuse/fuse"
)

// WinFspDrive mounts a virtual drive or directory via WinFsp / cgofuse.
type WinFspDrive struct {
	mu          gosync.Mutex
	mounted     bool
	mountPoint  string // "G:" or `C:\GhostDrive\GhD\`
	lastError   string // last error from Mount/Unmount; empty if no error
	backends    []MountedBackend
	host        *fuse.FileSystemHost
	fs          *GhostFileSystem // live filesystem instance; set by Mount, accessed by UpdateBackends
	done        chan struct{}
	emitter     syncdispatch.EventEmitter
	watchCancel context.CancelFunc // cancels the watchLoop goroutines on Unmount
}

// SetEmitter injects the EventEmitter used by GhostFileSystem.watchLoop.
// Must be called before Mount(). Thread-safe.
func (d *WinFspDrive) SetEmitter(e syncdispatch.EventEmitter) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.emitter = e
}

// checkWinFsp verifies that the WinFsp runtime DLL is installed.
// WinFsp installs to %ProgramFiles%\WinFsp\bin\ — NOT to System32/SysWOW64.
func checkWinFsp() error {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "WinFsp", "bin", "winfsp-x64.dll"),
		filepath.Join(os.Getenv("ProgramW6432"), "WinFsp", "bin", "winfsp-x64.dll"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "WinFsp", "bin", "winfsp-x86.dll"),
	}
	for _, p := range candidates {
		if p == "" || strings.HasPrefix(p, `\WinFsp`) {
			continue // env var was empty
		}
		if _, err := os.Stat(p); err == nil {
			return nil
		}
	}
	return fmt.Errorf("WinFsp not installed — download from https://winfsp.dev")
}

// validateMountPoint returns an error if mountPoint is not a safe, supported
// WinFsp mount target.  Accepted forms:
//   - "X:"     bare drive letter (e.g. "G:")
//   - "X:\..." absolute directory path on a local drive (e.g. `C:\GhostDrive\GhD\`)
//
// Rejected: UNC paths (\\server\share), device paths (\\.\, \\?\), relative paths.
func validateMountPoint(mp string) error {
	if strings.HasPrefix(mp, `\\`) {
		return fmt.Errorf("winfsp: UNC/device mount point not allowed: %s", mp)
	}
	// Accept bare drive letter "X:" exactly.
	if len(mp) == 2 && mp[1] == ':' {
		c := mp[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return nil
		}
	}
	if !filepath.IsAbs(mp) {
		return fmt.Errorf("winfsp: mount point must be an absolute path or drive letter: %s", mp)
	}
	return nil
}

// Mount mounts the virtual drive at mountPoint (e.g. "G:" or `C:\GhostDrive\GhD\`)
// exposing the backend directly at the drive root.  No-op if already mounted.
// If mountPoint contains a path separator it is treated as a directory mount
// point and created via os.MkdirAll before mounting.
func (d *WinFspDrive) Mount(mountPoint string, backends []MountedBackend) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.mounted {
		return nil // idempotent
	}

	if err := validateMountPoint(mountPoint); err != nil {
		d.lastError = err.Error()
		return err
	}

	// For drive-letter mount points, verify the letter is not already occupied.
	if len(mountPoint) == 2 && mountPoint[1] == ':' {
		if IsLetterInUse(mountPoint) {
			err := fmt.Errorf("lettre %s déjà utilisée — choisissez une autre lettre", mountPoint)
			d.lastError = err.Error()
			return err
		}
	}

	if err := checkWinFsp(); err != nil {
		d.lastError = err.Error()
		return err
	}

	if len(backends) == 0 {
		err := fmt.Errorf("winfsp: no connected backend")
		d.lastError = err.Error()
		return err
	}

	// If mountPoint looks like a directory path (contains \ or /), create it.
	if strings.ContainsAny(mountPoint, `\/`) {
		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			d.lastError = fmt.Sprintf("winfsp: create mount point %s: %v", mountPoint, err)
			return fmt.Errorf("winfsp: create mount point %s: %w", mountPoint, err)
		}
	}

	emitter := d.emitter // captured under lock (d.mu held)
	fs := newGhostFileSystem(backends, emitter)
	host := fuse.NewFileSystemHost(fs)
	// Give the filesystem a back-reference to its host so Release() can call
	// host.Notify() after uploads (issue #103 — Explorer auto-refresh).
	// Safe: written once here before host.Mount() starts FUSE dispatch.
	fs.host = host

	// Create a context for the watchLoop goroutines; it will be cancelled in Unmount.
	watchCtx, watchCancel := context.WithCancel(context.Background())
	d.watchCancel = watchCancel

	d.host = host
	d.fs = fs   // stored for UpdateBackends (v2.0)
	d.mountPoint = mountPoint
	d.backends = backends
	d.done = make(chan struct{})
	d.lastError = "" // clear on successful mount start

	// v2.0: volname is always "GhostDrive" for the unified drive.
	// The previous behaviour (backends[0].Name) would yield confusing drive
	// names when multiple backends are mounted under the same drive letter.
	const volName = "GhostDrive"

	go func() {
		defer close(d.done)
		// host.Mount blocks until the drive is unmounted.
		if ok := host.Mount(mountPoint, []string{"-o", fmt.Sprintf("uid=-1,gid=-1,volname=%s", volName)}); !ok {
			log.Printf("placeholder: WinFsp mount %s failed", mountPoint)
			d.mu.Lock()
			d.lastError = fmt.Sprintf("winfsp: mount %s failed", mountPoint)
			d.mounted = false
			d.mu.Unlock()
		}
	}()

	d.mounted = true

	// Start metadata Watch() goroutines AFTER setting mounted=true and releasing
	// the lock implicitly (the lock is still held here but watchLoops only need
	// the context, not the drive lock).
	fs.startWatchLoops(watchCtx)

	// Set drive icon in registry for drive-letter mount points.
	if len(mountPoint) == 2 && mountPoint[1] == ':' {
		setDriveLetterIcon(mountPoint)
	}

	return nil
}

// Unmount dismounts the virtual drive and waits up to 5 seconds for the FUSE
// goroutine to finish.  No-op if not mounted.
func (d *WinFspDrive) Unmount() error {
	d.mu.Lock()
	if !d.mounted {
		d.mu.Unlock()
		return nil
	}
	host := d.host
	done := d.done
	watchCancel := d.watchCancel
	d.mu.Unlock()

	// Cancel watchLoop goroutines before unmounting the FUSE host so they
	// do not attempt to access the backend after the drive is gone.
	if watchCancel != nil {
		watchCancel()
	}

	// Capture mount point before releasing the lock.
	d.mu.Lock()
	mp := d.mountPoint
	d.mu.Unlock()

	host.Unmount()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		d.mu.Lock()
		d.mounted = false
		d.mountPoint = ""
		d.backends = nil
		d.host = nil
		d.fs = nil
		d.done = nil
		d.watchCancel = nil
		d.lastError = "winfsp: unmount timed out"
		d.mu.Unlock()
		if len(mp) == 2 && mp[1] == ':' {
			clearDriveLetterIcon(mp)
		}
		return fmt.Errorf("winfsp: unmount timed out")
	}

	d.mu.Lock()
	d.mounted = false
	d.mountPoint = ""
	d.backends = nil
	d.host = nil
	d.fs = nil
	d.done = nil
	d.watchCancel = nil
	d.lastError = "" // clear on clean unmount
	d.mu.Unlock()

	if len(mp) == 2 && mp[1] == ':' {
		clearDriveLetterIcon(mp)
	}
	return nil
}

// IsMounted reports whether the drive is currently mounted.
func (d *WinFspDrive) IsMounted() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.mounted
}

// Status returns the current drive status, including any last error.
// In v2.0, BackendPaths maps each backendID to its sub-folder path under the
// unified drive mount point (e.g. "G:\MonNAS\").
func (d *WinFspDrive) Status() DriveStatus {
	d.mu.Lock()
	defer d.mu.Unlock()

	paths := make(map[string]string, len(d.backends))
	if d.mounted {
		// Normalise bare drive letters ("G:") to "G:\" so sub-paths are unambiguous.
		base := d.mountPoint
		if len(base) == 2 && base[1] == ':' {
			base = base + `\`
		}
		for _, mb := range d.backends {
			// Each backend is accessible as <mountPoint>\<backendName>\
			paths[mb.ID] = base + mb.Name + `\`
		}
	}
	return DriveStatus{
		Mounted:      d.mounted,
		MountPoint:   d.mountPoint,
		BackendName:  "GhostDrive",
		BackendID:    "unified",
		BackendPaths: paths,
		LastError:    d.lastError,
	}
}

// UpdateBackends atomically replaces the list of mounted backends without
// remounting the drive.  The GhostFileSystem will immediately see the new
// list in Readdir("/") and route().
// Returns an error if the drive is not mounted.
func (d *WinFspDrive) UpdateBackends(backends []MountedBackend) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.mounted {
		return fmt.Errorf("winfsp: UpdateBackends: drive is not mounted")
	}

	// Update WinFspDrive's own record.
	d.backends = backends

	// Propagate to the live GhostFileSystem so route() and Readdir("/") pick
	// up the new list on the very next FUSE dispatch cycle.
	if d.fs != nil {
		d.fs.mu.Lock()
		d.fs.backends = backends
		d.fs.mu.Unlock()
	}

	return nil
}
