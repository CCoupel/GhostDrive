package main

import (
	"embed"
	"io/fs"
	"log"
	"os"

	"github.com/CCoupel/GhostDrive/internal/app"
	"github.com/CCoupel/GhostDrive/internal/singleinstance"
	wails "github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
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
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
	}); err != nil {
		log.Fatalf("ghostdrive: %v", err)
	}
}
