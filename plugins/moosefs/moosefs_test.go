// Package moosefs_test provides integration tests for the MooseFS StorageBackend.
//
// Tests run against an in-memory fake MooseFS server embedded in this file.
// No external MooseFS installation is required.
//
// Each test creates its own server and backend instance to guarantee isolation.
package moosefs

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/CCoupel/GhostDrive/plugins/moosefs/internal/mfsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Test constants ───────────────────────────────────────────────────────────

// testPollInterval is a short Watch poll interval for fast tests (ms).
const testPollInterval = "20"

// ─── Integration fake server ─────────────────────────────────────────────────

// modeDir is the POSIX directory mode bit.
const modeDir = 0o040000

// modeFile is the POSIX regular file mode bit.
const modeFile = 0o100000

// integNode is a file or directory in the in-memory tree.
type integNode struct {
	nodeID  uint32
	name    string
	parent  uint32
	isDir   bool
	content []byte
	mode    uint32
	modTime int64
}

// integFakeServer is a minimal in-memory MooseFS TCP server.
type integFakeServer struct {
	ln     net.Listener
	mu     sync.Mutex
	nodes  map[uint32]*integNode
	nextID atomic.Uint32
	done   chan struct{}
}

// newIntegFakeServer creates a fake server seeded with the root node only.
func newIntegFakeServer() *integFakeServer {
	s := &integFakeServer{
		nodes: make(map[uint32]*integNode),
		done:  make(chan struct{}),
	}
	s.nextID.Store(2)
	s.nodes[mfsclient.RootNodeID] = &integNode{
		nodeID:  mfsclient.RootNodeID,
		name:    "/",
		parent:  0,
		isDir:   true,
		mode:    modeDir | 0o755,
		modTime: time.Now().Unix(),
	}
	return s
}

// start binds to a random port, accepts, and returns the "host:port" string.
func (s *integFakeServer) start(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s.ln = ln
	go s.run()
	t.Cleanup(func() {
		_ = ln.Close()
		<-s.done
	})
	return ln.Addr().String()
}

func (s *integFakeServer) run() {
	defer close(s.done)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *integFakeServer) handleConn(conn net.Conn) {
	defer conn.Close()
	for {
		cmd, payload, err := mfsclient.ReadFrame(conn)
		if err != nil {
			return
		}
		s.dispatch(conn, cmd, payload)
	}
}

func (s *integFakeServer) dispatch(conn net.Conn, cmd uint32, payload []byte) {
	switch cmd {
	case mfsclient.CmdFUSEREADDIR:
		s.svrReadDir(conn, payload)
	case mfsclient.CmdFUSEGETATTR:
		s.svrGetAttr(conn, payload)
	case mfsclient.CmdFUSEMKNOD:
		s.svrMknod(conn, payload)
	case mfsclient.CmdFUSEMKDIR:
		s.svrMkdir(conn, payload)
	case mfsclient.CmdFUSEWRITE:
		s.svrWrite(conn, payload)
	case mfsclient.CmdFUSEREAD:
		s.svrRead(conn, payload)
	case mfsclient.CmdFUSEUNLINK:
		s.svrUnlink(conn, payload)
	case mfsclient.CmdFUSERMDIR:
		s.svrRmdir(conn, payload)
	default:
		_ = mfsclient.WriteFrame(conn, cmd+100, []byte{mfsclient.StatusERROR})
	}
}

func (s *integFakeServer) alloc() uint32 { return s.nextID.Add(1) - 1 }

func sOK(conn net.Conn, ans uint32, data []byte) {
	_ = mfsclient.WriteFrame(conn, ans, append([]byte{mfsclient.StatusOK}, data...))
}

func sErr(conn net.Conn, ans uint32, status uint8) {
	_ = mfsclient.WriteFrame(conn, ans, []byte{status})
}

func (s *integFakeServer) svrReadDir(conn net.Conn, payload []byte) {
	nodeID, _, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEREADDIR, mfsclient.StatusERROR)
		return
	}
	s.mu.Lock()
	_, ok := s.nodes[nodeID]
	var children []*integNode
	if ok {
		for _, n := range s.nodes {
			if n.parent == nodeID && n.nodeID != mfsclient.RootNodeID {
				cp := *n
				children = append(children, &cp)
			}
		}
	}
	s.mu.Unlock()

	if !ok {
		sErr(conn, mfsclient.AnsFUSEREADDIR, mfsclient.StatusENOENT)
		return
	}
	buf := mfsclient.PutUint32(nil, uint32(len(children)))
	for _, c := range children {
		buf = mfsclient.PutUint32(buf, c.nodeID)
		var isDir byte
		if c.isDir {
			isDir = 1
		}
		buf = append(buf, isDir)
		buf = mfsclient.PutString(buf, c.name)
	}
	sOK(conn, mfsclient.AnsFUSEREADDIR, buf)
}

func (s *integFakeServer) svrGetAttr(conn net.Conn, payload []byte) {
	nodeID, _, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEGETATTR, mfsclient.StatusERROR)
		return
	}
	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	s.mu.Unlock()
	if !ok {
		sErr(conn, mfsclient.AnsFUSEGETATTR, mfsclient.StatusENOENT)
		return
	}
	buf := mfsclient.PutUint32(nil, n.nodeID)
	buf = mfsclient.PutUint64(buf, uint64(len(n.content)))
	buf = mfsclient.PutUint32(buf, n.mode)
	buf = mfsclient.PutInt64(buf, n.modTime)
	sOK(conn, mfsclient.AnsFUSEGETATTR, buf)
}

func (s *integFakeServer) svrMknod(conn net.Conn, payload []byte) {
	parentID, off, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEMKNOD, mfsclient.StatusERROR)
		return
	}
	mode, off, err := mfsclient.ReadUint32(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEMKNOD, mfsclient.StatusERROR)
		return
	}
	name, _, err := mfsclient.ReadString(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEMKNOD, mfsclient.StatusERROR)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[parentID]; !ok {
		sErr(conn, mfsclient.AnsFUSEMKNOD, mfsclient.StatusENOENT)
		return
	}
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name {
			sOK(conn, mfsclient.AnsFUSEMKNOD, mfsclient.PutUint32(nil, n.nodeID))
			return
		}
	}
	id := s.alloc()
	s.nodes[id] = &integNode{
		nodeID: id, name: name, parent: parentID, isDir: false,
		mode: modeFile | mode, modTime: time.Now().Unix(),
	}
	sOK(conn, mfsclient.AnsFUSEMKNOD, mfsclient.PutUint32(nil, id))
}

func (s *integFakeServer) svrMkdir(conn net.Conn, payload []byte) {
	parentID, off, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEMKDIR, mfsclient.StatusERROR)
		return
	}
	mode, off, err := mfsclient.ReadUint32(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEMKDIR, mfsclient.StatusERROR)
		return
	}
	name, _, err := mfsclient.ReadString(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEMKDIR, mfsclient.StatusERROR)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[parentID]; !ok {
		sErr(conn, mfsclient.AnsFUSEMKDIR, mfsclient.StatusENOENT)
		return
	}
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name && n.isDir {
			sOK(conn, mfsclient.AnsFUSEMKDIR, mfsclient.PutUint32(nil, n.nodeID))
			return
		}
	}
	id := s.alloc()
	s.nodes[id] = &integNode{
		nodeID: id, name: name, parent: parentID, isDir: true,
		mode: modeDir | mode, modTime: time.Now().Unix(),
	}
	sOK(conn, mfsclient.AnsFUSEMKDIR, mfsclient.PutUint32(nil, id))
}

func (s *integFakeServer) svrWrite(conn net.Conn, payload []byte) {
	nodeID, off, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEWRITE, mfsclient.StatusERROR)
		return
	}
	offset, off, err := mfsclient.ReadUint64(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEWRITE, mfsclient.StatusERROR)
		return
	}
	dataLen, off, err := mfsclient.ReadUint32(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEWRITE, mfsclient.StatusERROR)
		return
	}
	if off+int(dataLen) > len(payload) {
		sErr(conn, mfsclient.AnsFUSEWRITE, mfsclient.StatusERROR)
		return
	}
	data := payload[off : off+int(dataLen)]

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	if ok && !n.isDir {
		end := offset + uint64(len(data))
		if end > uint64(len(n.content)) {
			newC := make([]byte, end)
			copy(newC, n.content)
			n.content = newC
		}
		copy(n.content[offset:], data)
		n.modTime = time.Now().Unix()
	}
	s.mu.Unlock()

	if !ok {
		sErr(conn, mfsclient.AnsFUSEWRITE, mfsclient.StatusENOENT)
		return
	}
	sOK(conn, mfsclient.AnsFUSEWRITE, nil)
}

func (s *integFakeServer) svrRead(conn net.Conn, payload []byte) {
	nodeID, off, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEREAD, mfsclient.StatusERROR)
		return
	}
	offset, off, err := mfsclient.ReadUint64(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEREAD, mfsclient.StatusERROR)
		return
	}
	size, _, err := mfsclient.ReadUint32(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEREAD, mfsclient.StatusERROR)
		return
	}

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	var chunk []byte
	if ok && !n.isDir {
		if offset < uint64(len(n.content)) {
			end := offset + uint64(size)
			if end > uint64(len(n.content)) {
				end = uint64(len(n.content))
			}
			chunk = make([]byte, end-offset)
			copy(chunk, n.content[offset:end])
		}
	}
	s.mu.Unlock()

	if !ok {
		sErr(conn, mfsclient.AnsFUSEREAD, mfsclient.StatusENOENT)
		return
	}
	buf := mfsclient.PutUint32(nil, uint32(len(chunk)))
	buf = append(buf, chunk...)
	sOK(conn, mfsclient.AnsFUSEREAD, buf)
}

func (s *integFakeServer) svrUnlink(conn net.Conn, payload []byte) {
	parentID, off, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEUNLINK, mfsclient.StatusERROR)
		return
	}
	name, _, err := mfsclient.ReadString(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSEUNLINK, mfsclient.StatusERROR)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, n := range s.nodes {
		if n.parent == parentID && n.name == name && !n.isDir {
			delete(s.nodes, id)
			sOK(conn, mfsclient.AnsFUSEUNLINK, nil)
			return
		}
	}
	sErr(conn, mfsclient.AnsFUSEUNLINK, mfsclient.StatusENOENT)
}

func (s *integFakeServer) svrRmdir(conn net.Conn, payload []byte) {
	parentID, off, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSERMDIR, mfsclient.StatusERROR)
		return
	}
	name, _, err := mfsclient.ReadString(payload, off)
	if err != nil {
		sErr(conn, mfsclient.AnsFUSERMDIR, mfsclient.StatusERROR)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, n := range s.nodes {
		if n.parent == parentID && n.name == name && n.isDir {
			for _, child := range s.nodes {
				if child.parent == id {
					sErr(conn, mfsclient.AnsFUSERMDIR, mfsclient.StatusENOTEMPT)
					return
				}
			}
			delete(s.nodes, id)
			sOK(conn, mfsclient.AnsFUSERMDIR, nil)
			return
		}
	}
	sErr(conn, mfsclient.AnsFUSERMDIR, mfsclient.StatusENOENT)
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// startFakeServer starts a fake MooseFS server and returns its address.
func startFakeServer(t *testing.T) string {
	t.Helper()
	srv := newIntegFakeServer()
	return srv.start(t)
}

// newTestBackend creates a Backend already connected to the fake server at addr.
func newTestBackend(t *testing.T, addr string) *Backend {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	b := New()
	err = b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"masterHost":   host,
			"masterPort":   portStr,
			"subDir":       "/",
			"pollInterval": testPollInterval,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Disconnect() })
	return b
}

// writeTempFile creates a temp file with the given content.
func writeTempFile(t *testing.T, content []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "ghostdrive-mfs-test-*")
	require.NoError(t, err)
	_, err = f.Write(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// ─── Connect tests ────────────────────────────────────────────────────────────

func TestConnect_success(t *testing.T) {
	addr := startFakeServer(t)
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	b := New()
	err = b.Connect(plugins.BackendConfig{
		Params: map[string]string{
			"masterHost": host,
			"masterPort": portStr,
			"subDir":     "/",
		},
	})
	require.NoError(t, err)
	assert.True(t, b.IsConnected())
	_ = b.Disconnect()
}

func TestConnect_unreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	_ = ln.Close()

	host, portStr, _ := net.SplitHostPort(addr)
	b := New()
	err = b.Connect(plugins.BackendConfig{
		Params: map[string]string{"masterHost": host, "masterPort": portStr},
	})
	require.Error(t, err)
	assert.False(t, b.IsConnected())
}

func TestConnect_missingHost(t *testing.T) {
	b := New()
	err := b.Connect(plugins.BackendConfig{Params: map[string]string{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "masterHost")
}

// ─── List tests ───────────────────────────────────────────────────────────────

func TestList_rootDir(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	entries, err := b.List(context.Background(), "/")
	require.NoError(t, err)
	assert.NotNil(t, entries)
	assert.Empty(t, entries)
}

func TestList_withFiles(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	for _, name := range []string{"alpha.txt", "beta.txt"} {
		src := writeTempFile(t, []byte("content-"+name))
		require.NoError(t, b.Upload(ctx, src, "/"+name, nil))
	}
	entries, err := b.List(ctx, "/")
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

// ─── Stat tests ───────────────────────────────────────────────────────────────

func TestStat_existingFile(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	content := []byte("stat test content")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/stat_test.txt", nil))

	fi, err := b.Stat(ctx, "/stat_test.txt")
	require.NoError(t, err)
	require.NotNil(t, fi)
	assert.Equal(t, "stat_test.txt", fi.Name)
	assert.Equal(t, int64(len(content)), fi.Size)
	assert.False(t, fi.IsDir)
}

func TestStat_notFound(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	_, err := b.Stat(context.Background(), "/ghost.txt")
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── CreateDir tests ──────────────────────────────────────────────────────────

func TestCreateDir(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	require.NoError(t, b.CreateDir(ctx, "/newdir"))
	fi, err := b.Stat(ctx, "/newdir")
	require.NoError(t, err)
	assert.True(t, fi.IsDir)
}

func TestCreateDir_idempotent(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	require.NoError(t, b.CreateDir(ctx, "/idemdir"))
	require.NoError(t, b.CreateDir(ctx, "/idemdir")) // must not error
}

// ─── Upload / Download roundtrip ─────────────────────────────────────────────

func TestUploadDownload_roundtrip(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	content := []byte("roundtrip content — テスト")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/roundtrip.txt", nil))

	dst := filepath.Join(t.TempDir(), "downloaded.txt")
	require.NoError(t, b.Download(ctx, "/roundtrip.txt", dst, nil))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestUpload_withProgress(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	payload := make([]byte, 4096)
	src := writeTempFile(t, payload)

	var calls int
	require.NoError(t, b.Upload(ctx, src, "/progress.bin", func(done, total int64) {
		calls++
		assert.Greater(t, done, int64(0))
	}))
	assert.Greater(t, calls, 0)
}

func TestDownload_withProgress(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	payload := make([]byte, 4096)
	src := writeTempFile(t, payload)
	require.NoError(t, b.Upload(ctx, src, "/dl_progress.bin", nil))

	var calls int
	dst := filepath.Join(t.TempDir(), "out.bin")
	require.NoError(t, b.Download(ctx, "/dl_progress.bin", dst, func(done, total int64) {
		calls++
	}))
	assert.Greater(t, calls, 0)
}

// ─── Delete tests ─────────────────────────────────────────────────────────────

func TestDelete_file(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	src := writeTempFile(t, []byte("delete me"))
	require.NoError(t, b.Upload(ctx, src, "/delete_me.txt", nil))
	require.NoError(t, b.Delete(ctx, "/delete_me.txt"))

	_, err := b.Stat(ctx, "/delete_me.txt")
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

// ─── Move tests ───────────────────────────────────────────────────────────────

func TestMove_fileRename(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	content := []byte("move test content")
	src := writeTempFile(t, content)
	require.NoError(t, b.Upload(ctx, src, "/move_src.txt", nil))

	require.NoError(t, b.Move(ctx, "/move_src.txt", "/move_dst.txt"))

	_, err := b.Stat(ctx, "/move_src.txt")
	assert.ErrorIs(t, err, plugins.ErrFileNotFound, "source must not exist after move")

	dst := filepath.Join(t.TempDir(), "moved.txt")
	require.NoError(t, b.Download(ctx, "/move_dst.txt", dst, nil))
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

// ─── Watch tests ──────────────────────────────────────────────────────────────

func TestWatch_detectCreated(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	srcFile := writeTempFile(t, []byte("watch trigger"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)

	time.Sleep(60 * time.Millisecond) // let snapshot settle

	go func() { _ = b.Upload(context.Background(), srcFile, "/watched_new.txt", nil) }()

	select {
	case event, ok := <-ch:
		require.True(t, ok)
		assert.Equal(t, plugins.FileEventCreated, event.Type)
		assert.False(t, event.Timestamp.IsZero())
	case <-ctx.Done():
		t.Fatal("timed out waiting for FileEventCreated")
	}
}

func TestWatch_detectDeleted(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx := context.Background()

	src := writeTempFile(t, []byte("to be deleted"))
	require.NoError(t, b.Upload(ctx, src, "/to_delete.txt", nil))

	watchCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := b.Watch(watchCtx, "/")
	require.NoError(t, err)

	time.Sleep(60 * time.Millisecond) // let snapshot settle

	go func() { _ = b.Delete(context.Background(), "/to_delete.txt") }()

	select {
	case event, ok := <-ch:
		require.True(t, ok)
		assert.Equal(t, plugins.FileEventDeleted, event.Type)
	case <-watchCtx.Done():
		t.Fatal("timed out waiting for FileEventDeleted")
	}
}

func TestWatch_stopsOnContextCancel(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := b.Watch(ctx, "/")
	require.NoError(t, err)
	cancel()

	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("Watch channel not closed within 500 ms after context cancellation")
		}
	}
}

// ─── GetQuota tests ───────────────────────────────────────────────────────────

func TestGetQuota_returnsMinusOne(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	free, total, err := b.GetQuota(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(-1), free)
	assert.Equal(t, int64(-1), total)
}

// ─── Not-connected guard tests ────────────────────────────────────────────────

func TestNotConnected_allOps(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func(*Backend) error
	}{
		{"Upload", func(b *Backend) error { return b.Upload(ctx, "/local", "/remote", nil) }},
		{"Download", func(b *Backend) error { return b.Download(ctx, "/remote", "/local", nil) }},
		{"Delete", func(b *Backend) error { return b.Delete(ctx, "/remote") }},
		{"Move", func(b *Backend) error { return b.Move(ctx, "/src", "/dst") }},
		{"List", func(b *Backend) error { _, err := b.List(ctx, "/"); return err }},
		{"Stat", func(b *Backend) error { _, err := b.Stat(ctx, "/file"); return err }},
		{"CreateDir", func(b *Backend) error { return b.CreateDir(ctx, "/dir") }},
		{"Watch", func(b *Backend) error { _, err := b.Watch(ctx, "/"); return err }},
		{"GetQuota", func(b *Backend) error { _, _, err := b.GetQuota(ctx); return err }},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			b := New() // not connected
			err := tc.fn(b)
			assert.ErrorIs(t, err, plugins.ErrNotConnected, "operation %q must return ErrNotConnected", tc.name)
		})
	}
}

// ─── Describe test ────────────────────────────────────────────────────────────

func TestDescribe(t *testing.T) {
	b := New()
	d := b.Describe()

	assert.Equal(t, "moosefs", d.Type)
	assert.NotEmpty(t, d.DisplayName)
	assert.NotEmpty(t, d.Description)

	keys := make(map[string]bool, len(d.Params))
	for _, p := range d.Params {
		keys[p.Key] = true
	}
	for _, k := range []string{"masterHost", "masterPort", "subDir", "pollInterval"} {
		assert.True(t, keys[k], "Describe() must include param %q", k)
	}
}

// Ensure unused fmt import compiles.
var _ = fmt.Sprintf
