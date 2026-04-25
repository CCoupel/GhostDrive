//go:build windows

package main

import (
	_ "embed"
	"os"
	"time"

	"github.com/CCoupel/GhostDrive/internal/app"
	"github.com/CCoupel/GhostDrive/internal/types"
	"github.com/getlantern/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed assets/tray-idle.ico
var iconIdle []byte

//go:embed assets/tray-syncing.ico
var iconSyncing []byte

//go:embed assets/tray-paused.ico
var iconPaused []byte

//go:embed assets/tray-error.ico
var iconError []byte

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
	mQuit := systray.AddMenuItem("Quitter", "")

	// State watcher: update icon + tooltip every 3s
	go func() {
		for {
			time.Sleep(3 * time.Second)
			if ctx := ghostApp.Context(); ctx == nil {
				continue
			}
			state := ghostApp.GetSyncState()
			updateTrayIcon(state.Status)
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
	default:
		systray.SetIcon(iconIdle)
		systray.SetTooltip("GhostDrive — À jour")
	}
}
