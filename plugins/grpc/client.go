// Package grpc implements the gRPC bridge that allows GhostDrive to communicate
// with out-of-process storage plugins. GRPCBackend wraps a gRPC client
// connection and satisfies the plugins.StorageBackend interface so the rest
// of the application is unaware of the transport layer.
package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	storagepb "github.com/CCoupel/GhostDrive/plugins/proto"

	"github.com/CCoupel/GhostDrive/plugins"
	"google.golang.org/grpc"
)

const (
	// uploadChunkSize is the maximum bytes per Upload RPC chunk (64 KB).
	uploadChunkSize = 64 * 1024
)

// GRPCBackend wraps a gRPC client connection and implements plugins.StorageBackend.
// It is created by the GRPCLoader for each loaded plugin binary.
//
// Thread-safety: all public methods are safe for concurrent use.
type GRPCBackend struct {
	conn   *grpc.ClientConn
	client storagepb.StorageServiceClient
}

// NewGRPCBackend creates a GRPCBackend from an existing gRPC client connection.
// The caller (typically GRPCLoader) is responsible for the connection lifecycle.
func NewGRPCBackend(conn *grpc.ClientConn) *GRPCBackend {
	return &GRPCBackend{
		conn:   conn,
		client: storagepb.NewStorageServiceClient(conn),
	}
}

// ── Identification ────────────────────────────────────────────────────────────

// Name implements plugins.StorageBackend.
func (b *GRPCBackend) Name() string {
	resp, err := b.client.Name(context.Background(), &storagepb.NameRequest{})
	if err != nil {
		return "unknown"
	}
	return resp.GetName()
}

// Version returns the plugin version via a best-effort Name extended RPC.
// Returns "unknown" when the plugin does not expose version information.
func (b *GRPCBackend) Version() string {
	return "unknown"
}

// ── Connection ────────────────────────────────────────────────────────────────

// Connect implements plugins.StorageBackend.
func (b *GRPCBackend) Connect(cfg plugins.BackendConfig) error {
	resp, err := b.client.Connect(context.Background(), &storagepb.ConnectRequest{
		Config: backendConfigToProto(cfg),
	})
	if err != nil {
		return mapGRPCError("grpc: Connect", err)
	}
	if resp.GetError() != "" {
		return fmt.Errorf("grpc: Connect: %s", resp.GetError())
	}
	return nil
}

// Disconnect implements plugins.StorageBackend.
func (b *GRPCBackend) Disconnect() error {
	resp, err := b.client.Disconnect(context.Background(), &storagepb.DisconnectRequest{})
	if err != nil {
		return mapGRPCError("grpc: Disconnect", err)
	}
	if resp.GetError() != "" {
		return fmt.Errorf("grpc: Disconnect: %s", resp.GetError())
	}
	return nil
}

// IsConnected implements plugins.StorageBackend.
func (b *GRPCBackend) IsConnected() bool {
	resp, err := b.client.IsConnected(context.Background(), &storagepb.IsConnectedRequest{})
	if err != nil {
		return false
	}
	return resp.GetConnected()
}

// ── File operations ───────────────────────────────────────────────────────────

// Upload implements plugins.StorageBackend.
// It streams the local file in 64 KB chunks and computes progress locally.
func (b *GRPCBackend) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("grpc: Upload: open local file: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("grpc: Upload: stat local file: %w", err)
	}
	totalBytes := fi.Size()

	stream, err := b.client.Upload(ctx)
	if err != nil {
		return mapGRPCError("grpc: Upload", err)
	}

	// First message carries metadata.
	firstMsg := true
	buf := make([]byte, uploadChunkSize)
	var bytesSent int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := f.Read(buf)
		if n > 0 {
			chunk := &storagepb.UploadChunk{
				Data: buf[:n],
			}
			if firstMsg {
				chunk.LocalPath = local
				chunk.RemotePath = remote
				chunk.TotalBytes = totalBytes
				firstMsg = false
			}
			if sendErr := stream.Send(chunk); sendErr != nil {
				return mapGRPCError("grpc: Upload: send chunk", sendErr)
			}
			bytesSent += int64(n)
			if progress != nil {
				progress(bytesSent, totalBytes)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("grpc: Upload: read local file: %w", readErr)
		}
	}

	result, err := stream.CloseAndRecv()
	if err != nil {
		return mapGRPCError("grpc: Upload: close stream", err)
	}
	if result.GetError() != "" {
		return fmt.Errorf("grpc: Upload: %s", result.GetError())
	}
	return nil
}

// Download implements plugins.StorageBackend.
// It receives server-streamed chunks and writes them to the local path.
func (b *GRPCBackend) Download(ctx context.Context, remote, local string, progress plugins.ProgressCallback) error {
	stream, err := b.client.Download(ctx, &storagepb.DownloadRequest{
		RemotePath: remote,
		LocalPath:  local,
	})
	if err != nil {
		return mapGRPCError("grpc: Download", err)
	}

	// Create parent directory if needed.
	if err := os.MkdirAll(parentDir(local), 0755); err != nil {
		return fmt.Errorf("grpc: Download: create parent dir: %w", err)
	}

	f, err := os.Create(local)
	if err != nil {
		return fmt.Errorf("grpc: Download: create local file: %w", err)
	}
	defer f.Close()

	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return mapGRPCError("grpc: Download: recv chunk", err)
		}
		if chunk.GetError() != "" {
			return fmt.Errorf("grpc: Download: %s", chunk.GetError())
		}
		if len(chunk.GetData()) > 0 {
			if _, writeErr := f.Write(chunk.GetData()); writeErr != nil {
				return fmt.Errorf("grpc: Download: write local file: %w", writeErr)
			}
		}
		if progress != nil && chunk.GetBytesTotal() > 0 {
			progress(chunk.GetBytesDone(), chunk.GetBytesTotal())
		}
	}
	return nil
}

// Delete implements plugins.StorageBackend.
func (b *GRPCBackend) Delete(ctx context.Context, remote string) error {
	resp, err := b.client.Delete(ctx, &storagepb.DeleteRequest{Path: remote})
	if err != nil {
		return mapGRPCError("grpc: Delete", err)
	}
	if resp.GetError() != "" {
		return fmt.Errorf("grpc: Delete: %s", resp.GetError())
	}
	return nil
}

// Move implements plugins.StorageBackend.
func (b *GRPCBackend) Move(ctx context.Context, oldPath, newPath string) error {
	resp, err := b.client.Move(ctx, &storagepb.MoveRequest{OldPath: oldPath, NewPath: newPath})
	if err != nil {
		return mapGRPCError("grpc: Move", err)
	}
	if resp.GetError() != "" {
		return fmt.Errorf("grpc: Move: %s", resp.GetError())
	}
	return nil
}

// ── Navigation ────────────────────────────────────────────────────────────────

// List implements plugins.StorageBackend.
func (b *GRPCBackend) List(ctx context.Context, path string) ([]plugins.FileInfo, error) {
	resp, err := b.client.List(ctx, &storagepb.ListRequest{Path: path})
	if err != nil {
		return nil, mapGRPCError("grpc: List", err)
	}
	if resp.GetError() != "" {
		return nil, fmt.Errorf("grpc: List: %s", resp.GetError())
	}
	files := make([]plugins.FileInfo, 0, len(resp.GetFiles()))
	for _, pf := range resp.GetFiles() {
		files = append(files, fileInfoFromProto(pf))
	}
	return files, nil
}

// Stat implements plugins.StorageBackend.
func (b *GRPCBackend) Stat(ctx context.Context, path string) (*plugins.FileInfo, error) {
	resp, err := b.client.Stat(ctx, &storagepb.StatRequest{Path: path})
	if err != nil {
		return nil, mapGRPCError("grpc: Stat", err)
	}
	if resp.GetError() != "" {
		return nil, fmt.Errorf("grpc: Stat: %s", resp.GetError())
	}
	fi := fileInfoFromProto(resp.GetFile())
	return &fi, nil
}

// CreateDir implements plugins.StorageBackend.
func (b *GRPCBackend) CreateDir(ctx context.Context, path string) error {
	resp, err := b.client.CreateDir(ctx, &storagepb.CreateDirRequest{Path: path})
	if err != nil {
		return mapGRPCError("grpc: CreateDir", err)
	}
	if resp.GetError() != "" {
		return fmt.Errorf("grpc: CreateDir: %s", resp.GetError())
	}
	return nil
}

// ── Watch ─────────────────────────────────────────────────────────────────────

// Watch implements plugins.StorageBackend.
// It returns a buffered channel (size 64) and forwards WatchEvents from the
// server stream until ctx is cancelled.
func (b *GRPCBackend) Watch(ctx context.Context, path string) (<-chan plugins.FileEvent, error) {
	stream, err := b.client.Watch(ctx, &storagepb.WatchRequest{Path: path})
	if err != nil {
		return nil, mapGRPCError("grpc: Watch", err)
	}

	ch := make(chan plugins.FileEvent, 64)
	go func() {
		defer close(ch)
		for {
			ev, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				// Context cancelled or stream closed by server — exit silently.
				return
			}
			if ev.GetError() != "" {
				return
			}
			select {
			case ch <- fileEventFromProto(ev.GetEvent()):
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// ── Quota ─────────────────────────────────────────────────────────────────────

// GetQuota implements plugins.StorageBackend.
//
// The server (plugins/grpc/server.go) propagates errors as gRPC status errors
// since v0.7.0. The resp.GetError() branch below is a backward-compatibility
// fallback for pre-v0.7.0 plugin binaries that returned errors in the response
// body instead of as gRPC status codes. It can be removed once all plugins
// have been recompiled against the v0.7.0+ server stub.
func (b *GRPCBackend) GetQuota(ctx context.Context) (free, total int64, err error) {
	resp, grpcErr := b.client.GetQuota(ctx, &storagepb.QuotaRequest{})
	if grpcErr != nil {
		return 0, 0, mapGRPCError("grpc: GetQuota", grpcErr)
	}
	// Backward-compat: pre-v0.7.0 plugins set this field instead of a gRPC error.
	if resp.GetError() != "" {
		return 0, 0, fmt.Errorf("grpc: GetQuota: %s", resp.GetError())
	}
	return resp.GetFree(), resp.GetTotal(), nil
}

// ── Describe ─────────────────────────────────────────────────────────────────

// Describe implements plugins.StorageBackend.
// Returns a minimal descriptor on RPC failure (never panics).
func (b *GRPCBackend) Describe() plugins.PluginDescriptor {
	resp, err := b.client.Describe(context.Background(), &storagepb.DescribeRequest{})
	if err != nil {
		return plugins.PluginDescriptor{Type: b.Name()}
	}
	params := make([]plugins.ParamSpec, 0, len(resp.GetParams()))
	for _, p := range resp.GetParams() {
		params = append(params, plugins.ParamSpec{
			Key:         p.GetKey(),
			Label:       p.GetLabel(),
			Type:        plugins.ParamType(p.GetType()),
			Required:    p.GetRequired(),
			Default:     p.GetDefaultVal(),
			Placeholder: p.GetPlaceholder(),
			Options:     p.GetOptions(),
			HelpText:    p.GetHelpText(),
		})
	}
	return plugins.PluginDescriptor{
		Type:        resp.GetType(),
		DisplayName: resp.GetDisplayName(),
		Description: resp.GetDescription(),
		Params:      params,
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// mapGRPCError converts well-known gRPC status codes to GhostDrive sentinel errors.
func mapGRPCError(prefix string, err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("%s: %w", prefix, plugins.ErrFileNotFound)
	case codes.FailedPrecondition:
		return fmt.Errorf("%s: %w", prefix, plugins.ErrNotConnected)
	default:
		return fmt.Errorf("%s: %w", prefix, err)
	}
}

// backendConfigToProto converts a BackendConfig to its proto representation.
func backendConfigToProto(bc plugins.BackendConfig) *storagepb.BackendConfigProto {
	return &storagepb.BackendConfigProto{
		Id:         bc.ID,
		Name:       bc.Name,
		Type:       bc.Type,
		Enabled:    bc.Enabled,
		AutoSync:   bc.AutoSync,
		Params:     bc.Params,
		SyncDir:    bc.SyncDir,
		RemotePath: bc.RemotePath,
		LocalPath:  bc.LocalPath,
	}
}

// fileInfoFromProto converts a FileInfoProto to plugins.FileInfo.
func fileInfoFromProto(pf *storagepb.FileInfoProto) plugins.FileInfo {
	if pf == nil {
		return plugins.FileInfo{}
	}
	return plugins.FileInfo{
		Name:          pf.GetName(),
		Path:          pf.GetPath(),
		Size:          pf.GetSize(),
		IsDir:         pf.GetIsDir(),
		ModTime:       time.Unix(pf.GetModTimeUnix(), 0),
		ETag:          pf.GetEtag(),
		IsPlaceholder: pf.GetIsPlaceholder(),
		IsCached:      pf.GetIsCached(),
	}
}

// fileEventFromProto converts a FileEventProto to plugins.FileEvent.
func fileEventFromProto(pe *storagepb.FileEventProto) plugins.FileEvent {
	if pe == nil {
		return plugins.FileEvent{}
	}
	return plugins.FileEvent{
		Type:      plugins.FileEventType(pe.GetEventType()),
		Path:      pe.GetPath(),
		OldPath:   pe.GetOldPath(),
		Timestamp: time.Unix(pe.GetTimestampUnix(), 0),
		Source:    pe.GetSource(),
	}
}

// parentDir returns the parent directory of path.
func parentDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
