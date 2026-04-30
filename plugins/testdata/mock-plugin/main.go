// Package main implements the GhostDrive mock storage plugin used exclusively
// by integration tests in plugins/loader/.
//
// This binary is NOT committed to the repository. Instead, the test suite's
// TestMain compiles it at runtime via:
//
//	exec.Command("go", "build", "-o", binaryPath,
//	    "github.com/CCoupel/GhostDrive/plugins/testdata/mock-plugin")
//
// The mock plugin implements plugins.StorageBackend with no-op operations:
//   - Name() returns "mock"
//   - Connect/Disconnect/IsConnected manage a simple boolean flag
//   - All file operations succeed silently
//   - GetQuota returns (-1, -1, nil) — quota not supported
//
// It responds correctly to the go-plugin handshake (loader.HandshakeConfig)
// and serves the gRPC StorageService interface.
package main

import (
	"log"

	goplugin "github.com/hashicorp/go-plugin"

	grpcbridge "github.com/CCoupel/GhostDrive/plugins/grpc"
	"github.com/CCoupel/GhostDrive/plugins/loader"
)

func main() {
	log.SetPrefix("[mock-plugin] ")
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: loader.HandshakeConfig,
		Plugins: goplugin.PluginSet{
			"storage": &grpcbridge.GRPCPlugin{Impl: &MockPlugin{}},
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}
