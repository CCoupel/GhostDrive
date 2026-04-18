package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpload_Success(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	emitter := &captureEmitter{}

	localFile := filepath.Join(tmp, "file.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("hello upload"), 0644))

	task := SyncTask{
		LocalPath:  localFile,
		RemotePath: "/remote/file.txt",
		Direction:  DirectionUpload,
	}

	err := Upload(context.Background(), task, backend, emitter)
	require.NoError(t, err)

	fi, ok := backend.files["/remote/file.txt"]
	require.True(t, ok, "file must be in backend after upload")
	assert.Equal(t, int64(len("hello upload")), fi.Size)
}

func TestUpload_MissingLocalFile(t *testing.T) {
	backend := newMockBackend()
	emitter := &captureEmitter{}

	task := SyncTask{
		LocalPath:  "/nonexistent/file.txt",
		RemotePath: "/remote/file.txt",
		Direction:  DirectionUpload,
	}

	err := Upload(context.Background(), task, backend, emitter)
	assert.Error(t, err)
	assert.True(t, emitter.hasEvent("sync:error"), "sync:error must be emitted on missing file")
}

func TestUpload_ProgressEvents(t *testing.T) {
	tmp := t.TempDir()
	backend := &progressMockBackend{mockBackend: newMockBackend()}
	emitter := &captureEmitter{}

	localFile := filepath.Join(tmp, "large.txt")
	require.NoError(t, os.WriteFile(localFile, make([]byte, 1024), 0644))

	task := SyncTask{
		LocalPath:  localFile,
		RemotePath: "/remote/large.txt",
		Direction:  DirectionUpload,
	}

	err := Upload(context.Background(), task, backend, emitter)
	require.NoError(t, err)
	assert.True(t, emitter.hasEvent("sync:progress"), "sync:progress must be emitted during upload")
}

func TestUpload_BackendError(t *testing.T) {
	tmp := t.TempDir()
	backend := &failingMockBackend{mockBackend: newMockBackend()}
	emitter := &captureEmitter{}

	localFile := filepath.Join(tmp, "file.txt")
	require.NoError(t, os.WriteFile(localFile, []byte("data"), 0644))

	task := SyncTask{
		LocalPath:  localFile,
		RemotePath: "/remote/file.txt",
		Direction:  DirectionUpload,
	}

	err := Upload(context.Background(), task, backend, emitter)
	assert.Error(t, err)
	assert.True(t, emitter.hasEvent("sync:error"))
}
