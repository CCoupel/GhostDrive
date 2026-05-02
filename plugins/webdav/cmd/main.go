// Command ghostdrive-webdav is the standalone gRPC plugin binary for the
// GhostDrive WebDAV storage backend.
//
// # Architecture
//
// This binary is discovered by the GhostDrive plugin loader at runtime:
// drop it in <AppDir>/plugins/ and (re)start GhostDrive or call
// ReloadPlugins().  No manual registration is required.
//
// # Build
//
// From the repository root:
//
//	make -f plugins/webdav/Makefile build        # → ghostdrive-webdav.exe (Windows AMD64)
//	make -f plugins/webdav/Makefile build-linux  # → ghostdrive-webdav     (Linux AMD64)
//
// # Install
//
//	copy ghostdrive-webdav.exe "<AppDir>\plugins\"  # Windows
//	cp   ghostdrive-webdav     <AppDir>/plugins/    # Linux
package main

import (
	"log"

	goplugin "github.com/hashicorp/go-plugin"

	sdk "github.com/CCoupel/GhostDrive/plugins/sdk/go"
	"github.com/CCoupel/GhostDrive/plugins/webdav"
)

func main() {
	log.SetPrefix("[webdav] ")
	goplugin.Serve(sdk.ServeConfig(webdav.New()))
}
