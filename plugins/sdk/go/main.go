// main.go — Template entry point for a GhostDrive storage plugin.
//
// This file is a TEMPLATE — it is excluded from regular builds via the
// `ignore` build tag. To use it, copy this file into your plugin's module,
// remove the `//go:build ignore` line, then implement MyPlugin.
//
// Build: GOOS=windows GOARCH=amd64 go build -o myplugin.exe .
// Install: copy myplugin.exe to <AppDir>\plugins\

//go:build ignore

package main

import (
	sdk "github.com/CCoupel/GhostDrive/plugins/sdk/go"
	goplugin "github.com/hashicorp/go-plugin"
)

func main() {
	// Replace &MyPlugin{} with your own plugins.StorageBackend implementation.
	goplugin.Serve(sdk.ServeConfig(&MyPlugin{}))
}
