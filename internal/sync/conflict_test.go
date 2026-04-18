package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveConflict_LocalWins(t *testing.T) {
	emitter := &captureEmitter{}

	local := plugins.FileInfo{Path: "doc.txt", ModTime: time.Now()}
	remote := plugins.FileInfo{Path: "doc.txt", ModTime: time.Now().Add(-1 * time.Hour)}

	winner := ResolveConflict(local, remote, emitter, "")
	assert.Equal(t, "local", winner)
	assert.True(t, emitter.hasEvent("sync:conflict-resolved"))
}

func TestResolveConflict_RemoteWins(t *testing.T) {
	emitter := &captureEmitter{}

	local := plugins.FileInfo{Path: "doc.txt", ModTime: time.Now().Add(-1 * time.Hour)}
	remote := plugins.FileInfo{Path: "doc.txt", ModTime: time.Now()}

	winner := ResolveConflict(local, remote, emitter, "")
	assert.Equal(t, "remote", winner)
	assert.True(t, emitter.hasEvent("sync:conflict-resolved"))
}

func TestResolveConflict_EqualTime_RemoteWins(t *testing.T) {
	emitter := &captureEmitter{}

	now := time.Now()
	local := plugins.FileInfo{Path: "doc.txt", ModTime: now}
	remote := plugins.FileInfo{Path: "doc.txt", ModTime: now}

	winner := ResolveConflict(local, remote, emitter, "")
	assert.Equal(t, "remote", winner, "equal ModTime: remote wins by convention")
}

func TestResolveConflict_EventPayload(t *testing.T) {
	emitter := &captureEmitter{}

	local := plugins.FileInfo{Path: "folder/report.docx", ModTime: time.Now()}
	remote := plugins.FileInfo{Path: "folder/report.docx", ModTime: time.Now().Add(-10 * time.Minute)}

	_ = ResolveConflict(local, remote, emitter, "")

	require.Len(t, emitter.events, 1)
	evt := emitter.events[0]
	assert.Equal(t, "sync:conflict-resolved", evt.Name)

	payload, ok := evt.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "folder/report.docx", payload["path"])
	assert.Equal(t, "local", payload["winner"])
	assert.NotEmpty(t, payload["localModTime"])
	assert.NotEmpty(t, payload["remoteModTime"])
	assert.NotEmpty(t, payload["time"])
}

func TestResolveConflict_LogsToFile(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "sync.log")
	emitter := &captureEmitter{}

	local := plugins.FileInfo{Path: "file.txt", ModTime: time.Now()}
	remote := plugins.FileInfo{Path: "file.txt", ModTime: time.Now().Add(-5 * time.Minute)}

	_ = ResolveConflict(local, remote, emitter, logPath)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "CONFLICT")
	assert.Contains(t, string(data), "file.txt")
	assert.Contains(t, string(data), "local")
}
