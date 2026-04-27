//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"time"

	"github.com/CCoupel/GhostDrive/internal/app"
	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/getlantern/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Tray icons generated in-memory as 16×16 solid-colour PNG wrapped in an ICO
// container.  No asset files needed — colours follow contracts/sync-icons.md.
var (
	iconIdle      []byte // #94a3b8 gray    — idle, no backend connected
	iconConnected []byte // #22c55e green   — idle, ≥1 backend connected
	iconSyncing   []byte // #3b82f6 blue    — sync in progress
	iconPaused    []byte // #f59e0b amber   — paused or drive error
	iconError     []byte // #ef4444 red     — sync error
)

func init() {
	iconIdle = generateIconICO(0x94, 0xa3, 0xb8)
	iconConnected = generateIconICO(0x22, 0xc5, 0x5e)
	iconSyncing = generateIconICO(0x3b, 0x82, 0xf6)
	iconPaused = generateIconICO(0xf5, 0x9e, 0x0b)
	iconError = generateIconICO(0xef, 0x44, 0x44)
}

// generateIconICO returns a valid ICO file that contains a single 16×16
// solid-colour PNG image.  Windows 7+ supports PNG-in-ICO (Vista icon format).
func generateIconICO(r, g, b uint8) []byte {
	// Build a 16×16 solid-colour RGBA image.
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	c := color.RGBA{R: r, G: g, B: b, A: 255}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, c)
		}
	}

	// Encode as PNG.
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		// Should never happen for a solid-colour RGBA image, but avoid panic.
		log.Printf("tray: generateIconICO: png encode failed: %v", err)
		return nil
	}
	pngData := pngBuf.Bytes()
	pngSize := uint32(len(pngData))

	// Build ICO container:
	//   6-byte ICONDIR  + 16-byte ICONDIRENTRY + PNG data
	var ico bytes.Buffer

	// ICONDIR: reserved(2) + type=1(2) + count=1(2)
	ico.Write([]byte{0, 0, 1, 0, 1, 0})

	// ICONDIRENTRY (16 bytes):
	//   width(1) height(1) colorCount(1) reserved(1)
	//   planes(2) bitCount(2) bytesInRes(4) imageOffset(4)
	entry := make([]byte, 16)
	entry[0] = 16 // width
	entry[1] = 16 // height
	entry[2] = 0  // colorCount (0 = no palette)
	entry[3] = 0  // reserved
	binary.LittleEndian.PutUint16(entry[4:6], 1)        // planes
	binary.LittleEndian.PutUint16(entry[6:8], 32)       // bitCount (RGBA)
	binary.LittleEndian.PutUint32(entry[8:12], pngSize) // bytesInRes
	binary.LittleEndian.PutUint32(entry[12:16], 22)     // imageOffset (6+16)
	ico.Write(entry)

	ico.Write(pngData)
	return ico.Bytes()
}

// runSystray starts the Windows notification-area icon in a goroutine.
// It returns immediately; systray.Run blocks internally on a dedicated OS thread.
func runSystray(ghostApp *app.App) {
	go systray.Run(
		func() { onSystrayReady(ghostApp) },
		func() { /* onExit — Wails shutdown handles cleanup */ },
	)
}

func onSystrayReady(ghostApp *app.App) {
	systray.SetIcon(iconIdle)
	systray.SetTooltip("GhostDrive — À jour")

	mOpen := systray.AddMenuItem("Ouvrir GhostDrive", "")
	systray.AddSeparator()
	mSync := systray.AddMenuItem("Synchroniser maintenant", "")
	mPause := systray.AddMenuItem("Pause / Reprendre", "")
	systray.AddSeparator()
	mSettings := systray.AddMenuItem("Paramètres", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quitter", "")

	// State watcher: refresh icon + tooltip every 3 seconds.
	// Priority (high → low):
	//   1. Red   — SyncError
	//   2. Blue  — SyncSyncing
	//   3. Amber — SyncPaused OR drive LastError != ""
	//   4. Green — SyncIdle + ≥1 backend connected
	//   5. Gray  — SyncIdle + 0 backends connected
	go func() {
		for {
			time.Sleep(3 * time.Second)
			if ghostApp.Context() == nil {
				continue
			}
			state := ghostApp.GetSyncState()
			ds := ghostApp.GetDriveStatus()

			switch {
			case state.Status == types.SyncError:
				systray.SetIcon(iconError)
				systray.SetTooltip("GhostDrive — Erreur de synchronisation")

			case state.Status == types.SyncSyncing:
				systray.SetIcon(iconSyncing)
				systray.SetTooltip("GhostDrive — Synchronisation en cours...")

			case state.Status == types.SyncPaused || ds.LastError != "":
				systray.SetIcon(iconPaused)
				if ds.LastError != "" {
					systray.SetTooltip("GhostDrive — Erreur drive : " + ds.LastError)
				} else {
					systray.SetTooltip("GhostDrive — En pause")
				}

			default: // SyncIdle — check backend connectivity
				connected := 0
				for _, s := range ghostApp.GetBackendStatuses() {
					if s.Connected {
						connected++
					}
				}
				if connected > 0 {
					systray.SetIcon(iconConnected)
					systray.SetTooltip("GhostDrive — À jour")
				} else {
					systray.SetIcon(iconIdle)
					systray.SetTooltip("GhostDrive — Aucun backend connecté")
				}
			}
		}
	}()

	// Menu item event loop
	for {
		select {
		case <-mOpen.ClickedCh:
			ghostApp.Emit("tray:action", map[string]any{"action": "open"})
			if ctx := ghostApp.Context(); ctx != nil {
				wailsruntime.WindowShow(ctx)
			}

		case <-mSync.ClickedCh:
			ghostApp.Emit("tray:action", map[string]any{"action": "sync"})
			state := ghostApp.GetSyncState()
			for _, b := range state.Backends {
				_ = ghostApp.ForceSync(b.BackendID)
			}

		case <-mPause.ClickedCh:
			ghostApp.Emit("tray:action", map[string]any{"action": "pause"})
			state := ghostApp.GetSyncState()
			if state.Status == types.SyncPaused {
				for _, b := range state.Backends {
					_ = ghostApp.ResumeSync(b.BackendID)
				}
			} else {
				for _, b := range state.Backends {
					_ = ghostApp.PauseSync(b.BackendID)
				}
			}

		case <-mSettings.ClickedCh:
			ghostApp.Emit("tray:open-settings", nil)
			ghostApp.Emit("tray:action", map[string]any{"action": "settings"})
			if ctx := ghostApp.Context(); ctx != nil {
				wailsruntime.WindowShow(ctx)
			}

		case <-mQuit.ClickedCh:
			systray.Quit()
			ghostApp.Quit()
			if ghostApp.Context() == nil {
				os.Exit(0)
			}
			return
		}
	}
}

// updateTrayIcon sets the systray icon based on sync status alone (no
// connectivity check).  Prefer the full priority logic in the poll loop.
func updateTrayIcon(status types.SyncStatus) {
	switch status {
	case types.SyncSyncing:
		systray.SetIcon(iconSyncing)
		systray.SetTooltip("GhostDrive — Synchronisation en cours...")
	case types.SyncPaused:
		systray.SetIcon(iconPaused)
		systray.SetTooltip("GhostDrive — En pause")
	case types.SyncError:
		systray.SetIcon(iconError)
		systray.SetTooltip("GhostDrive — Erreur de synchronisation")
	default: // SyncIdle — use gray as safe fallback (poll loop uses green when connected)
		systray.SetIcon(iconIdle)
		systray.SetTooltip("GhostDrive — À jour")
	}
}
