//go:build windows

package placeholder

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const driveIconsKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\DriveIcons`

// extractedIconPath returns where the embedded icon is written on disk.
func extractedIconPath() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = os.TempDir()
	}
	return filepath.Join(appData, "GhostDrive", "ghostdrive.ico")
}

// ensureIconOnDisk writes the embedded icon to APPDATA\GhostDrive\ghostdrive.ico
// if it is not already present (idempotent).
func ensureIconOnDisk() (string, error) {
	path := extractedIconPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("drive icon: mkdir: %w", err)
	}
	if err := os.WriteFile(path, driveIconICO, 0644); err != nil {
		return "", fmt.Errorf("drive icon: write: %w", err)
	}
	return path, nil
}

// setDriveLetterIcon registers the GhostDrive icon for the given drive letter
// (e.g. "G:" or "G") under HKCU so Windows Explorer shows it on the volume.
// Non-fatal: logs errors rather than blocking the mount.
func setDriveLetterIcon(mountPoint string) {
	letter := strings.ToUpper(strings.TrimSuffix(mountPoint, ":"))
	if len(letter) != 1 {
		return
	}

	icoPath, err := ensureIconOnDisk()
	if err != nil {
		log.Printf("placeholder: drive icon: %v", err)
		return
	}

	// Create/open  HKCU\...\DriveIcons\<letter>\DefaultIcon
	subKey := driveIconsKey + `\` + letter + `\DefaultIcon`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, subKey, registry.SET_VALUE)
	if err != nil {
		log.Printf("placeholder: drive icon: registry create %s: %v", subKey, err)
		return
	}
	defer k.Close()

	if err := k.SetStringValue("", icoPath+",0"); err != nil {
		log.Printf("placeholder: drive icon: registry set %s: %v", subKey, err)
		return
	}

	notifyExplorerIconChange()
}

// clearDriveLetterIcon removes the registry icon entry for the given drive letter.
func clearDriveLetterIcon(mountPoint string) {
	letter := strings.ToUpper(strings.TrimSuffix(mountPoint, ":"))
	if len(letter) != 1 {
		return
	}
	// Delete the leaf key first, then the parent letter key.
	_ = registry.DeleteKey(registry.CURRENT_USER,
		driveIconsKey+`\`+letter+`\DefaultIcon`)
	_ = registry.DeleteKey(registry.CURRENT_USER,
		driveIconsKey+`\`+letter)

	notifyExplorerIconChange()
}

// notifyExplorerIconChange broadcasts SHCNE_ASSOCCHANGED so Windows Explorer
// refreshes shell icons immediately without needing an F5.
func notifyExplorerIconChange() {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("SHChangeNotify")
	const (
		shcneAssocChanged = 0x08000000
		shcnfIDList       = 0x0000
	)
	proc.Call(shcneAssocChanged, shcnfIDList, 0, 0)
}
