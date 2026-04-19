package main

import (
	"embed"
	"io/fs"
	"log"
	"os"

	"github.com/CCoupel/GhostDrive/internal/app"
	"github.com/CCoupel/GhostDrive/internal/singleinstance"
	"github.com/CCoupel/GhostDrive/internal/types"
	wails "github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	ok, err := singleinstance.Acquire()
	if err != nil {
		log.Fatalf("ghostdrive: single-instance check: %v", err)
	}
	if !ok {
		log.Println("ghostdrive: already running — exiting")
		os.Exit(0)
	}

	ghostApp := app.NewApp("")

	// Start notification-area icon on Windows (no-op on other platforms).
	runSystray(ghostApp)

	frontendFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatalf("ghostdrive: embedded assets: %v", err)
	}

	if err := wails.Run(&options.App{
		Title:             "GhostDrive",
		Width:             480,
		Height:            640,
		MinWidth:          400,
		MinHeight:         500,
		AssetServer:       &assetserver.Options{Assets: frontendFS},
		BackgroundColour:  &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:         ghostApp.Startup,
		OnShutdown:        ghostApp.Shutdown,
		Bind:              []interface{}{ghostApp},
		HideWindowOnClose: true,
		StartHidden:       ghostApp.GetConfig().StartMinimized,
		Menu:              buildTrayMenu(ghostApp),
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
	}); err != nil {
		log.Fatalf("ghostdrive: %v", err)
	}
}

// buildTrayMenu builds the GhostDrive menu (application menu bar on Windows).
// It maps to the tray menu contract in contracts/tray-menu.md.
// Note: native notification-area icon (systray) is deferred to v0.3.0
// as Wails v2 does not expose a systray API — only HideWindowOnClose is used.
func buildTrayMenu(ghostApp *app.App) *menu.Menu {
	openWindow := func(_ *menu.CallbackData) {
		if ctx := ghostApp.Context(); ctx != nil {
			wailsruntime.WindowShow(ctx)
		}
	}

	forceSyncAll := func(_ *menu.CallbackData) {
		cfg := ghostApp.GetConfig()
		for _, bc := range cfg.Backends {
			if bc.Enabled {
				_ = ghostApp.ForceSync(bc.ID)
			}
		}
		ghostApp.Emit("tray:action", map[string]any{"action": "sync"})
	}

	togglePause := func(_ *menu.CallbackData) {
		state := ghostApp.GetSyncState()
		cfg := ghostApp.GetConfig()
		for _, bc := range cfg.Backends {
			if bc.Enabled {
				if state.Status == types.SyncPaused {
					_ = ghostApp.StartSync(bc.ID)
				} else {
					_ = ghostApp.PauseSync(bc.ID)
				}
			}
		}
		ghostApp.Emit("tray:action", map[string]any{"action": "pause"})
	}

	openSettings := func(_ *menu.CallbackData) {
		if ctx := ghostApp.Context(); ctx != nil {
			wailsruntime.WindowShow(ctx)
		}
		ghostApp.Emit("tray:open-settings", nil)
		ghostApp.Emit("tray:action", map[string]any{"action": "settings"})
	}

	return menu.NewMenuFromItems(
		menu.Text("Ouvrir GhostDrive", keys.CmdOrCtrl("o"), openWindow),
		menu.Separator(),
		menu.Text("Synchroniser maintenant", keys.CmdOrCtrl("s"), forceSyncAll),
		menu.Text("Pause / Reprendre", keys.CmdOrCtrl("p"), togglePause),
		menu.Separator(),
		menu.Text("Paramètres", keys.CmdOrCtrl(","), openSettings),
		menu.Separator(),
		menu.Text("Quitter", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
			ghostApp.Quit()
		}),
	)
}
