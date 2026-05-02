package grpc

import (
	"context"
	"fmt"
	"net"

	"github.com/CCoupel/GhostDrive/plugins"
	storagepb "github.com/CCoupel/GhostDrive/plugins/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1 << 20 // 1 MB in-memory buffer

// ServeInProcess starts an in-process gRPC server via a bufconn listener,
// backed by the provided StorageBackend implementation. It returns:
//   - A GRPCBackend wired to the in-process server (ready to use).
//   - A cleanup function that stops the server and closes the listener and
//     connection — call it in App.Shutdown().
//   - An error if the client dial fails.
//
// Each call creates an independent (server, listener, client) triple so that
// multiple local backend instances remain isolated.
//
// Goroutine lifetime: the server goroutine terminates when cleanup() is called
// (srv.Stop() drains pending RPCs and returns).
func ServeInProcess(impl plugins.StorageBackend) (*GRPCBackend, func(), error) {
	l := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	storagepb.RegisterStorageServiceServer(srv, &GRPCBackendServer{Impl: impl})

	go func() { _ = srv.Serve(l) }()

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return l.DialContext(ctx)
	}

	//nolint:staticcheck // grpc.Dial is deprecated but grpc.NewClient requires an address resolver
	conn, err := grpc.Dial( //nolint:staticcheck
		"bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		srv.Stop()
		_ = l.Close()
		return nil, nil, fmt.Errorf("inprocess: dial: %w", err)
	}

	cleanup := func() {
		srv.Stop()
		_ = l.Close()
		_ = conn.Close()
	}
	return NewGRPCBackend(conn), cleanup, nil
}
