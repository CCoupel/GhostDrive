// Package sdk provides the base types and helpers for building GhostDrive
// storage plugins using the go-plugin + gRPC transport.
//
// # Quick start
//
// 1. Implement plugins.StorageBackend in your plugin package.
// 2. Copy main.go from this directory into your plugin's main package.
// 3. Replace &EchoPlugin{} with your own implementation.
// 4. Build with: GOOS=windows GOARCH=amd64 go build -o myplugin.exe ./...
// 5. Drop myplugin.exe into <AppDir>/plugins/.
// 6. (Re)start GhostDrive or call ReloadPlugins() from the UI.
//
// See plugins/sdk/go/echo/ for a fully-working example.
package sdk

import (
	grpcbridge "github.com/CCoupel/GhostDrive/plugins/grpc"
	"github.com/CCoupel/GhostDrive/plugins/loader"
	"github.com/CCoupel/GhostDrive/plugins"

	goplugin "github.com/hashicorp/go-plugin"
)

// PluginSet is the canonical go-plugin plugin map for GhostDrive storage plugins.
// Plugin binaries must pass this (or an equivalent map) to plugin.Serve.
func PluginSet(impl plugins.StorageBackend) goplugin.PluginSet {
	return goplugin.PluginSet{
		"storage": &grpcbridge.GRPCPlugin{Impl: impl},
	}
}

// ServeConfig returns a pre-filled plugin.ServeConfig for a GhostDrive storage
// plugin.  Plugin binaries call plugin.Serve(sdk.ServeConfig(impl)).
func ServeConfig(impl plugins.StorageBackend) *goplugin.ServeConfig {
	return &goplugin.ServeConfig{
		HandshakeConfig: loader.HandshakeConfig,
		Plugins:         PluginSet(impl),
		GRPCServer:      goplugin.DefaultGRPCServer,
	}
}
