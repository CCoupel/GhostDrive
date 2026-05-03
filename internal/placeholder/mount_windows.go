//go:build windows

package placeholder

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"time"

	"github.com/winfsp/cgofuse/fuse"
)

// WinFspDrive mounts a virtual drive or directory via WinFsp / cgofuse.
type WinFspDrive struct {
	mu         gosync.Mutex
	mounted    bool
	mountPoint string // "G:" or `C:\GhostDrive\GhD\`
	lastError  string // last error from Mount/Unmount; empty if no error
	backends   []MountedBackend
	host       *fuse.FileSystemHost
	done       chan struct{}
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

	fs := newGhostFileSystem(backends)
	host := fuse.NewFileSystemHost(fs)

	d.host = host
	d.mountPoint = mountPoint
	d.backends = backends
	d.done = make(chan struct{})
	d.lastError = "" // clear on successful mount start

	go func() {
		defer close(d.done)
		// host.Mount blocks until the drive is unmounted.
		if ok := host.Mount(mountPoint, []string{"-o", "uid=-1,gid=-1,volname=GhostDrive"}); !ok {
			log.Printf("placeholder: WinFsp mount %s failed", mountPoint)
			d.mu.Lock()
			d.lastError = fmt.Sprintf("winfsp: mount %s failed", mountPoint)
			d.mounted = false
			d.mu.Unlock()
		}
	}()

	d.mounted = true

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
	d.mu.Unlock()

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
		d.done = nil
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
	d.done = nil
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
func (d *WinFspDrive) Status() DriveStatus {
	d.mu.Lock()
	defer d.mu.Unlock()

	paths := make(map[string]string, len(d.backends))
	if d.mounted {
		for _, mb := range d.backends {
			// Normalise bare drive letters ("G:") to "G:\" so the path is unambiguous.
			base := d.mountPoint
			if len(base) == 2 && base[1] == ':' {
				base = base + `\`
			}
			paths[mb.ID] = base
		}
	}
	return DriveStatus{
		Mounted:      d.mounted,
		MountPoint:   d.mountPoint,
		BackendPaths: paths,
		LastError:    d.lastError,
	}
}
