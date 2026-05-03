// Command ghostdrive-moosefs is the standalone gRPC plugin binary for the
// GhostDrive MooseFS storage backend.
//
// # Architecture
//
// This binary is discovered by the GhostDrive plugin loader at runtime:
// drop it next to the GhostDrive binary and (re)start GhostDrive or call
// ReloadPlugins().  No manual registration is required.
//
// # Build
//
// From the repository root:
//
//	make -f plugins/moosefs/Makefile build        # → ghostdrive-moosefs.exe (Windows AMD64)
//	make -f plugins/moosefs/Makefile build-linux  # → ghostdrive-moosefs     (Linux AMD64)
//
// # Install
//
//	copy ghostdrive-moosefs.exe "<AppDir>\"  # Windows
//	cp   ghostdrive-moosefs     <AppDir>/    # Linux
package main

import (
	"log"

	goplugin "github.com/hashicorp/go-plugin"

	sdk "github.com/CCoupel/GhostDrive/plugins/sdk/go"
	"github.com/CCoupel/GhostDrive/plugins/moosefs"
)

func main() {
	log.SetPrefix("[moosefs] ")
	goplugin.Serve(sdk.ServeConfig(moosefs.New()))
}
