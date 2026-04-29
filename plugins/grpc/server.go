package grpc

import (
	"context"
	"errors"
	"io"
	"os"

	goplugin "github.com/hashicorp/go-plugin"
	storagepb "github.com/CCoupel/GhostDrive/plugins/proto"

	"github.com/CCoupel/GhostDrive/plugins"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// maxUploadSize is the maximum total bytes accepted per Upload RPC (10 GB).
	// Exceeding this limit causes the stream to be aborted with codes.ResourceExhausted
	// to prevent DoS via disk saturation.
	maxUploadSize = 10 * 1024 * 1024 * 1024
)

// ── GRPCBackendServer ────────────────────────────────────────────────────────

// GRPCBackendServer implements the proto-generated StorageServiceServer.
// It wraps a plugins.StorageBackend provided by the plugin binary and translates
// between the gRPC wire format and the Go interface.
type GRPCBackendServer struct {
	storagepb.UnimplementedStorageServiceServer
	Impl plugins.StorageBackend
}

// ── Identification ────────────────────────────────────────────────────────────

func (s *GRPCBackendServer) Name(_ context.Context, _ *storagepb.NameRequest) (*storagepb.NameResponse, error) {
	return &storagepb.NameResponse{Name: s.Impl.Name()}, nil
}

// ── Connection ────────────────────────────────────────────────────────────────

func (s *GRPCBackendServer) Connect(_ context.Context, req *storagepb.ConnectRequest) (*storagepb.ConnectResponse, error) {
	cfg := backendConfigFromProto(req.GetConfig())
	if err := s.Impl.Connect(cfg); err != nil {
		return &storagepb.ConnectResponse{Error: err.Error()}, nil
	}
	return &storagepb.ConnectResponse{}, nil
}

func (s *GRPCBackendServer) Disconnect(_ context.Context, _ *storagepb.DisconnectRequest) (*storagepb.DisconnectResponse, error) {
	if err := s.Impl.Disconnect(); err != nil {
		return &storagepb.DisconnectResponse{Error: err.Error()}, nil
	}
	return &storagepb.DisconnectResponse{}, nil
}

func (s *GRPCBackendServer) IsConnected(_ context.Context, _ *storagepb.IsConnectedRequest) (*storagepb.IsConnectedResponse, error) {
	return &storagepb.IsConnectedResponse{Connected: s.Impl.IsConnected()}, nil
}

// ── File operations ───────────────────────────────────────────────────────────

// Upload receives client-streamed chunks, reassembles them in a temp file,
// then calls the underlying StorageBackend.Upload.
func (s *GRPCBackendServer) Upload(stream storagepb.StorageService_UploadServer) error {
	var remotePath string
	firstMsg := true
	var totalWritten int64

	tmpFile, err := os.CreateTemp("", "ghostdrive-upload-*")
	if err != nil {
		return status.Errorf(codes.Internal, "upload: create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			tmpFile.Close()
			return status.Errorf(codes.Internal, "upload: recv: %v", recvErr)
		}
		if firstMsg {
			remotePath = chunk.GetRemotePath()
			firstMsg = false
		}
		if n := len(chunk.GetData()); n > 0 {
			totalWritten += int64(n)
			if totalWritten > maxUploadSize {
				tmpFile.Close()
				return status.Errorf(codes.ResourceExhausted,
					"upload: taille maximale dépassée (%d octets > %d)", totalWritten, maxUploadSize)
			}
			if _, writeErr := tmpFile.Write(chunk.GetData()); writeErr != nil {
				tmpFile.Close()
				return status.Errorf(codes.Internal, "upload: write temp: %v", writeErr)
			}
		}
	}
	tmpFile.Close()

	if err := s.Impl.Upload(stream.Context(), tmpPath, remotePath, nil); err != nil {
		return stream.SendAndClose(&storagepb.UploadResult{Error: err.Error()})
	}
	return stream.SendAndClose(&storagepb.UploadResult{})
}

// Download calls the underlying backend and streams the result in 64 KB chunks.
func (s *GRPCBackendServer) Download(req *storagepb.DownloadRequest, stream storagepb.StorageService_DownloadServer) error {
	ctx := stream.Context()
	remotePath := req.GetRemotePath()

	tmpFile, err := os.CreateTemp("", "ghostdrive-download-*")
	if err != nil {
		return status.Errorf(codes.Internal, "download: create temp: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := s.Impl.Download(ctx, remotePath, tmpPath, nil); err != nil {
		_ = stream.Send(&storagepb.DownloadChunk{Error: err.Error()})
		return nil
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return status.Errorf(codes.Internal, "download: open temp: %v", err)
	}
	defer f.Close()

	fi, _ := f.Stat()
	var totalBytes int64
	if fi != nil {
		totalBytes = fi.Size()
	}

	buf := make([]byte, uploadChunkSize)
	var bytesSent int64
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			bytesSent += int64(n)
			if sendErr := stream.Send(&storagepb.DownloadChunk{
				Data:       buf[:n],
				BytesDone:  bytesSent,
				BytesTotal: totalBytes,
			}); sendErr != nil {
				return sendErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return status.Errorf(codes.Internal, "download: read temp: %v", readErr)
		}
	}
	return nil
}

func (s *GRPCBackendServer) Delete(ctx context.Context, req *storagepb.DeleteRequest) (*storagepb.DeleteResponse, error) {
	if err := s.Impl.Delete(ctx, req.GetPath()); err != nil {
		return nil, mapBackendError(err)
	}
	return &storagepb.DeleteResponse{}, nil
}

func (s *GRPCBackendServer) Move(ctx context.Context, req *storagepb.MoveRequest) (*storagepb.MoveResponse, error) {
	if err := s.Impl.Move(ctx, req.GetOldPath(), req.GetNewPath()); err != nil {
		return nil, mapBackendError(err)
	}
	return &storagepb.MoveResponse{}, nil
}

// ── Navigation ────────────────────────────────────────────────────────────────

func (s *GRPCBackendServer) List(ctx context.Context, req *storagepb.ListRequest) (*storagepb.ListResponse, error) {
	files, err := s.Impl.List(ctx, req.GetPath())
	if err != nil {
		return nil, mapBackendError(err)
	}
	pfiles := make([]*storagepb.FileInfoProto, 0, len(files))
	for _, fi := range files {
		pfiles = append(pfiles, fileInfoToProto(fi))
	}
	return &storagepb.ListResponse{Files: pfiles}, nil
}

func (s *GRPCBackendServer) Stat(ctx context.Context, req *storagepb.StatRequest) (*storagepb.StatResponse, error) {
	fi, err := s.Impl.Stat(ctx, req.GetPath())
	if err != nil {
		return nil, mapBackendError(err)
	}
	return &storagepb.StatResponse{File: fileInfoToProto(*fi)}, nil
}

func (s *GRPCBackendServer) CreateDir(ctx context.Context, req *storagepb.CreateDirRequest) (*storagepb.CreateDirResponse, error) {
	if err := s.Impl.CreateDir(ctx, req.GetPath()); err != nil {
		return nil, mapBackendError(err)
	}
	return &storagepb.CreateDirResponse{}, nil
}

// ── Watch ─────────────────────────────────────────────────────────────────────

// Watch starts the backend Watch and streams events until the client cancels.
func (s *GRPCBackendServer) Watch(req *storagepb.WatchRequest, stream storagepb.StorageService_WatchServer) error {
	ctx := stream.Context()
	ch, err := s.Impl.Watch(ctx, req.GetPath())
	if err != nil {
		_ = stream.Send(&storagepb.WatchEvent{Error: err.Error()})
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if sendErr := stream.Send(&storagepb.WatchEvent{Event: fileEventToProto(ev)}); sendErr != nil {
				return sendErr
			}
		}
	}
}

// ── Quota ─────────────────────────────────────────────────────────────────────

// GetQuota returns free and total bytes, or a gRPC status error on failure.
// Errors are propagated via mapBackendError so that ErrNotConnected and
// ErrFileNotFound round-trip as their Go sentinels on the client side.
// Plugins that do not support quota must return (-1, -1, nil).
func (s *GRPCBackendServer) GetQuota(ctx context.Context, _ *storagepb.QuotaRequest) (*storagepb.QuotaResponse, error) {
	free, total, err := s.Impl.GetQuota(ctx)
	if err != nil {
		return nil, mapBackendError(err)
	}
	return &storagepb.QuotaResponse{Free: free, Total: total}, nil
}

// ── Error mapping ─────────────────────────────────────────────────────────────

// mapBackendError converts a plugins.StorageBackend error to a gRPC status
// error so that the client-side mapGRPCError can restore the correct sentinel.
//
//   - ErrFileNotFound  → codes.NotFound
//   - ErrNotConnected  → codes.FailedPrecondition
//   - other errors     → codes.Internal
//
// If err is already a gRPC status error (e.g. from a nested gRPC call), it is
// passed through unchanged.
func mapBackendError(err error) error {
	if err == nil {
		return nil
	}
	// Pass through existing gRPC status errors.
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, plugins.ErrFileNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, plugins.ErrNotConnected):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// ── Type conversion helpers ───────────────────────────────────────────────────

// backendConfigFromProto converts a BackendConfigProto to plugins.BackendConfig.
func backendConfigFromProto(p *storagepb.BackendConfigProto) plugins.BackendConfig {
	if p == nil {
		return plugins.BackendConfig{}
	}
	return plugins.BackendConfig{
		ID:         p.GetId(),
		Name:       p.GetName(),
		Type:       p.GetType(),
		Enabled:    p.GetEnabled(),
		AutoSync:   p.GetAutoSync(),
		Params:     p.GetParams(),
		SyncDir:    p.GetSyncDir(),
		RemotePath: p.GetRemotePath(),
		LocalPath:  p.GetLocalPath(),
	}
}

// fileInfoToProto converts plugins.FileInfo to its proto representation.
func fileInfoToProto(fi plugins.FileInfo) *storagepb.FileInfoProto {
	return &storagepb.FileInfoProto{
		Name:          fi.Name,
		Path:          fi.Path,
		Size:          fi.Size,
		IsDir:         fi.IsDir,
		ModTimeUnix:   fi.ModTime.Unix(),
		Etag:          fi.ETag,
		IsPlaceholder: fi.IsPlaceholder,
		IsCached:      fi.IsCached,
	}
}

// fileEventToProto converts plugins.FileEvent to its proto representation.
func fileEventToProto(ev plugins.FileEvent) *storagepb.FileEventProto {
	return &storagepb.FileEventProto{
		EventType:     string(ev.Type),
		Path:          ev.Path,
		OldPath:       ev.OldPath,
		TimestampUnix: ev.Timestamp.Unix(),
		Source:        ev.Source,
	}
}

// ── GRPCPlugin (go-plugin bridge) ────────────────────────────────────────────

// GRPCPlugin is the go-plugin bridge type that wraps GRPCBackendServer.
// It is used both by the loader (client side) and by plugin binaries (server side).
//
// Usage in plugin binary:
//
//	plugin.Serve(&plugin.ServeConfig{
//	    HandshakeConfig: loader.HandshakeConfig,
//	    Plugins:         plugin.PluginSet{"storage": &grpc.GRPCPlugin{Impl: &MyPlugin{}}},
//	    GRPCServer:      plugin.DefaultGRPCServer,
//	})
type GRPCPlugin struct {
	// Plugin embeds the go-plugin interface for backward-compatible non-gRPC methods.
	goplugin.Plugin

	// Impl is the actual StorageBackend implementation — set on the server side.
	// Left nil on the client (loader) side.
	Impl plugins.StorageBackend
}

// GRPCServer registers GRPCBackendServer with the gRPC server.
// Called by go-plugin on the plugin binary side.
func (p *GRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	storagepb.RegisterStorageServiceServer(s, &GRPCBackendServer{Impl: p.Impl})
	return nil
}

// GRPCClient returns a GRPCBackend wrapping the provided gRPC client connection.
// Called by go-plugin on the loader side.
func (p *GRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, conn *grpc.ClientConn) (interface{}, error) {
	return NewGRPCBackend(conn), nil
}
