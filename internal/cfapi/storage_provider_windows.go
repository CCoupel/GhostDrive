//go:build windows

package cfapi

// #cgo CFLAGS: -I. -I${SRCDIR}/include
// #include "cgo_cfapi_windows.h"
// #include <stdlib.h>
import "C"
import (
	"fmt"
	"log"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	// syncRootManagerBase is the registry path where Windows stores cloud provider metadata.
	// StorageProviderSyncRootManager::Register writes its data here.
	syncRootManagerBase = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\SyncRootManager`
)

// RegisterStorageProvider registers GhostDrive as a Windows cloud storage provider
// so that Explorer displays cloud overlay icons (☁️, ✓✓, ⟳) on sync root files.
//
// Strategy (two-tier):
//  1. WinRT COM: calls ghd_register_storage_provider_winrt which calls
//     Windows.Storage.Provider.StorageProviderSyncRootManager::Register.
//     This is the native API and activates cloudfilesshell.dll's overlay handler.
//  2. Registry fallback: if WinRT fails (older Windows, wrong GUID, etc.),
//     writes the equivalent registry keys under SyncRootManager directly.
//     Less immediate (requires Explorer restart) but ensures the registration
//     persists for future sessions.
//
// Must be called BEFORE p.Register() (CfRegisterSyncRoot) in CFManager.Start.
func RegisterStorageProvider(syncRootPath, backendName, displayName string) error {
	sid, err := currentUserSID()
	if err != nil {
		log.Printf("cfapi: StorageProvider: get user SID: %v — skipping registration", err)
		return nil // non-fatal
	}

	// SyncRootId format required by Windows: "{Provider}!{UserSid}!{AccountId}"
	syncRootID := "GhostDrive!" + sid + "!" + backendName

	// ── 1. Try WinRT COM registration (primary) ──────────────────────────────
	wID := C.ghd_utf8_to_wchar(C.CString(syncRootID))
	defer C.ghd_free_wchar(wID)
	wDisplay := C.ghd_utf8_to_wchar(C.CString(displayName))
	defer C.ghd_free_wchar(wDisplay)
	wPath := C.ghd_utf8_to_wchar(C.CString(syncRootPath))
	defer C.ghd_free_wchar(wPath)

	hr := C.ghd_register_storage_provider_winrt(
		C.LPCWSTR(wID),
		C.LPCWSTR(wDisplay),
		C.LPCWSTR(wPath),
	)
	if hr == 0 {
		log.Printf("cfapi: StorageProvider registered via WinRT: id=%q path=%q",
			syncRootID, syncRootPath)
		return nil
	}

	// ── 2. WinRT failed — registry fallback ──────────────────────────────────
	log.Printf("cfapi: WinRT StorageProvider registration failed (HRESULT 0x%08x) "+
		"— falling back to registry", uint32(hr))

	if err := registerStorageProviderRegistry(syncRootPath, syncRootID, displayName, sid); err != nil {
		return err
	}
	// Notify Explorer to reload shell extension registrations so cloud overlay
	// icons (☁️ ✓✓ ⟳) appear immediately without restarting Explorer.
	// Pass wPath so SHCNE_UPDATEDIR targets the specific sync root folder.
	C.ghd_notify_icon_refresh(C.LPCWSTR(wPath))
	log.Printf("cfapi: SHChangeNotify fired after registry StorageProvider registration")
	return nil
}

// registerStorageProviderRegistry writes the SyncRootManager registry keys directly.
// This is equivalent to what StorageProviderSyncRootManager::Register writes
// but does not trigger an immediate Explorer notification.
// Keys persist across reboots; Explorer picks them up on next start.
func registerStorageProviderRegistry(syncRootPath, syncRootID, displayName, sid string) error {
	keyPath := syncRootManagerBase + `\` + syncRootID

	k, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("cfapi: StorageProvider registry CreateKey %q: %w", syncRootID, err)
	}
	defer k.Close()

	entries := []struct {
		name  string
		value interface{}
	}{
		{"DisplayNameResource", displayName},
		{"IconResource", `%SystemRoot%\System32\imageres.dll,-1043`},
		{"Flags", uint32(2)},             // 0x2 = allow unpackaged Win32 app registration
		{"HydrationPolicy", uint32(2)},   // Full
		{"PopulationPolicy", uint32(2)},  // AlwaysFull
		{"InSyncPolicy", uint32(3)},      // FileCreationTime | DirectoryCreationTime
		{"HardlinkPolicy", uint32(0)},    // None
		{"Version", "2.1"},
	}
	for _, e := range entries {
		switch v := e.value.(type) {
		case string:
			if err := k.SetStringValue(e.name, v); err != nil {
				return fmt.Errorf("cfapi: StorageProvider registry set %s: %w", e.name, err)
			}
		case uint32:
			if err := k.SetDWordValue(e.name, v); err != nil {
				return fmt.Errorf("cfapi: StorageProvider registry set %s: %w", e.name, err)
			}
		}
	}

	// UserSyncRoots\{UserSid} = syncRootPath
	subKeyPath := keyPath + `\UserSyncRoots`
	sk, _, err := registry.CreateKey(registry.CURRENT_USER, subKeyPath, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("cfapi: StorageProvider registry CreateKey UserSyncRoots: %w", err)
	}
	defer sk.Close()

	if err := sk.SetStringValue(sid, syncRootPath); err != nil {
		return fmt.Errorf("cfapi: StorageProvider registry set UserSyncRoots\\%s: %w", sid, err)
	}

	// Policies subkey — required by cloudfilesshell.dll to recognise this sync
	// root as a cloud provider (not plain offline files) and display ☁️ overlay.
	kp, _, errP := registry.CreateKey(registry.CURRENT_USER, keyPath+`\Policies`, registry.ALL_ACCESS)
	if errP == nil {
		defer kp.Close()
		_ = kp.SetDWordValue("HydrationPolicy", 2)  // Full
		_ = kp.SetDWordValue("PopulationPolicy", 2) // AlwaysFull
		_ = kp.SetDWordValue("HardDeletePolicy", 0) // None
	} else {
		log.Printf("cfapi: StorageProvider registry CreateKey Policies: %v (non-fatal)", errP)
	}

	// ── Diagnostic read-back: confirm keys were actually written ──────────────
	if v, _, e := k.GetStringValue("DisplayNameResource"); e == nil {
		log.Printf("cfapi: registry[DisplayNameResource]=%q", v)
	} else {
		log.Printf("cfapi: registry[DisplayNameResource] read error: %v", e)
	}
	if v, _, e := sk.GetStringValue(sid); e == nil {
		log.Printf("cfapi: registry[UserSyncRoots/%s]=%q", sid, v)
	} else {
		log.Printf("cfapi: registry[UserSyncRoots/%s] read error: %v", sid, e)
	}

	log.Printf("cfapi: StorageProvider registered via registry: id=%q path=%q",
		syncRootID, syncRootPath)
	return nil
}

// currentUserSID returns the SID of the current user as a string (e.g. "S-1-5-21-...").
func currentUserSID() (string, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return "", fmt.Errorf("OpenCurrentProcessToken: %w", err)
	}
	defer token.Close()

	user, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("GetTokenUser: %w", err)
	}

	sid := user.User.Sid.String()
	if sid == "" {
		return "", fmt.Errorf("SID.String: empty result")
	}

	return sid, nil
}
