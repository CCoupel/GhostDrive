package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownload_Success(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	emitter := &captureEmitter{}

	backend.addRemoteFile("/remote/doc.txt", 64, time.Now())

	task := SyncTask{
		LocalPath:  filepath.Join(tmp, "doc.txt"),
		RemotePath: "/remote/doc.txt",
		Direction:  DirectionDownload,
	}

	err := Download(context.Background(), task, backend, emitter)
	require.NoError(t, err)

	info, err := os.Stat(task.LocalPath)
	require.NoError(t, err)
	assert.Equal(t, int64(64), info.Size())
}

func TestDownload_CreatesParentDirs(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	emitter := &captureEmitter{}

	backend.addRemoteFile("/remote/a/b/file.txt", 10, time.Now())

	task := SyncTask{
		LocalPath:  filepath.Join(tmp, "a", "b", "file.txt"),
		RemotePath: "/remote/a/b/file.txt",
		Direction:  DirectionDownload,
	}

	err := Download(context.Background(), task, backend, emitter)
	require.NoError(t, err)

	_, err = os.Stat(task.LocalPath)
	assert.NoError(t, err, "file must exist after download with nested dirs")
}

func TestDownload_AtomicWrite_NoTempFileLeft(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	emitter := &captureEmitter{}

	backend.addRemoteFile("/remote/atomic.txt", 8, time.Now())

	localPath := filepath.Join(tmp, "atomic.txt")
	task := SyncTask{
		LocalPath:  localPath,
		RemotePath: "/remote/atomic.txt",
		Direction:  DirectionDownload,
	}

	require.NoError(t, Download(context.Background(), task, backend, emitter))

	// No leftover tmp file
	_, err := os.Stat(localPath + ".ghostdrive.tmp")
	assert.True(t, os.IsNotExist(err), "temp file must not remain after successful download")
}

func TestDownload_MissingRemoteFile(t *testing.T) {
	tmp := t.TempDir()
	backend := newMockBackend()
	emitter := &captureEmitter{}

	task := SyncTask{
		LocalPath:  filepath.Join(tmp, "missing.txt"),
		RemotePath: "/remote/missing.txt",
		Direction:  DirectionDownload,
	}

	err := Download(context.Background(), task, backend, emitter)
	assert.Error(t, err)
	assert.True(t, emitter.hasEvent("sync:error"))

	// No leftover tmp file on failure
	_, statErr := os.Stat(task.LocalPath + ".ghostdrive.tmp")
	assert.True(t, os.IsNotExist(statErr), "temp file must be cleaned up on failure")
}

func TestDownload_BackendError(t *testing.T) {
	tmp := t.TempDir()
	backend := &failingMockBackend{mockBackend: newMockBackend()}
	emitter := &captureEmitter{}

	task := SyncTask{
		LocalPath:  filepath.Join(tmp, "fail.txt"),
		RemotePath: "/remote/fail.txt",
		Direction:  DirectionDownload,
	}

	err := Download(context.Background(), task, backend, emitter)
	assert.Error(t, err)
	assert.True(t, emitter.hasEvent("sync:error"))

	_, statErr := os.Stat(task.LocalPath + ".ghostdrive.tmp")
	assert.True(t, os.IsNotExist(statErr), "temp file must be cleaned up on backend error")
}

func TestDownload_ProgressEvents(t *testing.T) {
	tmp := t.TempDir()
	backend := &progressMockBackend{mockBackend: newMockBackend()}
	emitter := &captureEmitter{}

	backend.addRemoteFile("/remote/large.bin", 2048, time.Now())

	task := SyncTask{
		LocalPath:  filepath.Join(tmp, "large.bin"),
		RemotePath: "/remote/large.bin",
		Direction:  DirectionDownload,
	}

	err := Download(context.Background(), task, backend, emitter)
	require.NoError(t, err)
	assert.True(t, emitter.hasEvent("sync:progress"))
}
