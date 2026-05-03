// Package mfsclient_test provides unit tests for the mfsclient TCP client.
//
// Tests run against an in-memory fake MooseFS server (fakeMFSServer) that
// implements the same binary protocol as the Client.  No external MooseFS
// installation is required.
package mfsclient

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Fake MooseFS server ──────────────────────────────────────────────────────

// fakeNode represents a single file or directory in the in-memory tree.
type fakeNode struct {
	nodeID  uint32
	name    string
	parent  uint32
	isDir   bool
	content []byte
	mode    uint32
	modTime int64
}

// fakeMFSServer is a minimal in-memory MooseFS server for testing.
type fakeMFSServer struct {
	listener net.Listener
	mu       sync.Mutex
	nodes    map[uint32]*fakeNode
	nextID   atomic.Uint32
	done     chan struct{}
}

// newFakeMFSServer creates and initialises a fake server with only the root
// node present.
func newFakeMFSServer() *fakeMFSServer {
	s := &fakeMFSServer{
		nodes: make(map[uint32]*fakeNode),
		done:  make(chan struct{}),
	}
	s.nextID.Store(2) // root = 1
	root := &fakeNode{
		nodeID:  RootNodeID,
		name:    "/",
		parent:  0,
		isDir:   true,
		mode:    0o755,
		modTime: time.Now().Unix(),
	}
	s.nodes[RootNodeID] = root
	return s
}

// Start binds to a random port, starts accepting, and returns the listen
// address in "host:port" form.
func (s *fakeMFSServer) Start() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("fakeMFSServer: listen: " + err.Error())
	}
	s.listener = ln
	go s.acceptLoop()
	return ln.Addr().String()
}

// Stop closes the listener and waits for the accept goroutine to exit.
func (s *fakeMFSServer) Stop() {
	_ = s.listener.Close()
	<-s.done
}

func (s *fakeMFSServer) acceptLoop() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go s.handleConn(conn)
	}
}

func (s *fakeMFSServer) handleConn(conn net.Conn) {
	defer conn.Close()
	for {
		cmd, payload, err := ReadFrame(conn)
		if err != nil {
			return
		}
		s.dispatch(conn, cmd, payload)
	}
}

func (s *fakeMFSServer) dispatch(conn net.Conn, cmd uint32, payload []byte) {
	switch cmd {
	case CmdFUSEREADDIR:
		s.handleReadDir(conn, payload)
	case CmdFUSEGETATTR:
		s.handleGetAttr(conn, payload)
	case CmdFUSEMKNOD:
		s.handleMknod(conn, payload)
	case CmdFUSEMKDIR:
		s.handleMkdir(conn, payload)
	case CmdFUSEWRITE:
		s.handleWrite(conn, payload)
	case CmdFUSEREAD:
		s.handleRead(conn, payload)
	case CmdFUSEUNLINK:
		s.handleUnlink(conn, payload)
	case CmdFUSERMDIR:
		s.handleRmdir(conn, payload)
	default:
		// Unknown command: respond with a generic error.
		_ = WriteFrame(conn, cmd+100, []byte{StatusERROR})
	}
}

// ─── Helper: allocate a new nodeID ───────────────────────────────────────────

func (s *fakeMFSServer) allocID() uint32 {
	return s.nextID.Add(1) - 1
}

// ─── Handler helpers ──────────────────────────────────────────────────────────

func writeOK(conn net.Conn, ans uint32, data []byte) {
	payload := append([]byte{StatusOK}, data...)
	_ = WriteFrame(conn, ans, payload)
}

func writeErr(conn net.Conn, ans uint32, status uint8) {
	_ = WriteFrame(conn, ans, []byte{status})
}

// ─── Command handlers ─────────────────────────────────────────────────────────

func (s *fakeMFSServer) handleReadDir(conn net.Conn, payload []byte) {
	nodeID, _, err := ReadUint32(payload, 0)
	if err != nil {
		writeErr(conn, AnsFUSEREADDIR, StatusERROR)
		return
	}

	s.mu.Lock()
	parent, ok := s.nodes[nodeID]
	var children []*fakeNode
	if ok && parent.isDir {
		for _, n := range s.nodes {
			if n.parent == nodeID && n.nodeID != RootNodeID {
				children = append(children, n)
			}
		}
	}
	s.mu.Unlock()

	if !ok {
		writeErr(conn, AnsFUSEREADDIR, StatusENOENT)
		return
	}

	// Encode: [count uint32][entries...]
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(len(children)))
	for _, child := range children {
		buf = PutUint32(buf, child.nodeID)
		if child.isDir {
			buf = append(buf, 1)
		} else {
			buf = append(buf, 0)
		}
		buf = PutString(buf, child.name)
	}
	writeOK(conn, AnsFUSEREADDIR, buf)
}

func (s *fakeMFSServer) handleGetAttr(conn net.Conn, payload []byte) {
	nodeID, _, err := ReadUint32(payload, 0)
	if err != nil {
		writeErr(conn, AnsFUSEGETATTR, StatusERROR)
		return
	}

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	s.mu.Unlock()

	if !ok {
		writeErr(conn, AnsFUSEGETATTR, StatusENOENT)
		return
	}

	// Encode: [nodeID uint32][size uint64][mode uint32][modtime int64]
	buf := PutUint32(nil, n.nodeID)
	buf = PutUint64(buf, uint64(len(n.content)))
	buf = PutUint32(buf, n.mode)
	buf = PutInt64(buf, n.modTime)
	writeOK(conn, AnsFUSEGETATTR, buf)
}

func (s *fakeMFSServer) handleMknod(conn net.Conn, payload []byte) {
	parentID, off, err := ReadUint32(payload, 0)
	if err != nil {
		writeErr(conn, AnsFUSEMKNOD, StatusERROR)
		return
	}
	mode, off, err := ReadUint32(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSEMKNOD, StatusERROR)
		return
	}
	name, _, err := ReadString(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSEMKNOD, StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodes[parentID]; !ok {
		writeErr(conn, AnsFUSEMKNOD, StatusENOENT)
		return
	}

	// Check for duplicate
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name {
			// Return existing nodeID (idempotent)
			buf := PutUint32(nil, n.nodeID)
			writeOK(conn, AnsFUSEMKNOD, buf)
			return
		}
	}

	newID := s.allocID()
	s.nodes[newID] = &fakeNode{
		nodeID:  newID,
		name:    name,
		parent:  parentID,
		isDir:   false,
		mode:    mode,
		modTime: time.Now().Unix(),
	}
	buf := PutUint32(nil, newID)
	writeOK(conn, AnsFUSEMKNOD, buf)
}

func (s *fakeMFSServer) handleMkdir(conn net.Conn, payload []byte) {
	parentID, off, err := ReadUint32(payload, 0)
	if err != nil {
		writeErr(conn, AnsFUSEMKDIR, StatusERROR)
		return
	}
	mode, off, err := ReadUint32(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSEMKDIR, StatusERROR)
		return
	}
	name, _, err := ReadString(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSEMKDIR, StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodes[parentID]; !ok {
		writeErr(conn, AnsFUSEMKDIR, StatusENOENT)
		return
	}

	// Check for duplicate
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name && n.isDir {
			buf := PutUint32(nil, n.nodeID)
			writeOK(conn, AnsFUSEMKDIR, buf)
			return
		}
	}

	newID := s.allocID()
	s.nodes[newID] = &fakeNode{
		nodeID:  newID,
		name:    name,
		parent:  parentID,
		isDir:   true,
		mode:    mode,
		modTime: time.Now().Unix(),
	}
	buf := PutUint32(nil, newID)
	writeOK(conn, AnsFUSEMKDIR, buf)
}

func (s *fakeMFSServer) handleWrite(conn net.Conn, payload []byte) {
	nodeID, off, err := ReadUint32(payload, 0)
	if err != nil {
		writeErr(conn, AnsFUSEWRITE, StatusERROR)
		return
	}
	offset, off, err := ReadUint64(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSEWRITE, StatusERROR)
		return
	}
	dataLen, off, err := ReadUint32(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSEWRITE, StatusERROR)
		return
	}
	if off+int(dataLen) > len(payload) {
		writeErr(conn, AnsFUSEWRITE, StatusERROR)
		return
	}
	data := payload[off : off+int(dataLen)]

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	if ok && !n.isDir {
		end := offset + uint64(len(data))
		if end > uint64(len(n.content)) {
			// Extend content slice
			newContent := make([]byte, end)
			copy(newContent, n.content)
			n.content = newContent
		}
		copy(n.content[offset:], data)
		n.modTime = time.Now().Unix()
	}
	s.mu.Unlock()

	if !ok {
		writeErr(conn, AnsFUSEWRITE, StatusENOENT)
		return
	}
	writeOK(conn, AnsFUSEWRITE, nil)
}

func (s *fakeMFSServer) handleRead(conn net.Conn, payload []byte) {
	nodeID, off, err := ReadUint32(payload, 0)
	if err != nil {
		writeErr(conn, AnsFUSEREAD, StatusERROR)
		return
	}
	offset, off, err := ReadUint64(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSEREAD, StatusERROR)
		return
	}
	size, _, err := ReadUint32(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSEREAD, StatusERROR)
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
		writeErr(conn, AnsFUSEREAD, StatusENOENT)
		return
	}

	// Encode: [dataLen uint32][data bytes]
	buf := PutUint32(nil, uint32(len(chunk)))
	buf = append(buf, chunk...)
	writeOK(conn, AnsFUSEREAD, buf)
}

func (s *fakeMFSServer) handleUnlink(conn net.Conn, payload []byte) {
	parentID, off, err := ReadUint32(payload, 0)
	if err != nil {
		writeErr(conn, AnsFUSEUNLINK, StatusERROR)
		return
	}
	name, _, err := ReadString(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSEUNLINK, StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, n := range s.nodes {
		if n.parent == parentID && n.name == name && !n.isDir {
			delete(s.nodes, id)
			writeOK(conn, AnsFUSEUNLINK, nil)
			return
		}
	}
	writeErr(conn, AnsFUSEUNLINK, StatusENOENT)
}

func (s *fakeMFSServer) handleRmdir(conn net.Conn, payload []byte) {
	parentID, off, err := ReadUint32(payload, 0)
	if err != nil {
		writeErr(conn, AnsFUSERMDIR, StatusERROR)
		return
	}
	name, _, err := ReadString(payload, off)
	if err != nil {
		writeErr(conn, AnsFUSERMDIR, StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, n := range s.nodes {
		if n.parent == parentID && n.name == name && n.isDir {
			// Check not empty
			for _, child := range s.nodes {
				if child.parent == id {
					writeErr(conn, AnsFUSERMDIR, StatusENOTEMPT)
					return
				}
			}
			delete(s.nodes, id)
			writeOK(conn, AnsFUSERMDIR, nil)
			return
		}
	}
	writeErr(conn, AnsFUSERMDIR, StatusENOENT)
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

func newTestClient(t *testing.T) (*Client, *fakeMFSServer) {
	t.Helper()
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(port, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c, srv
}

// ─── Dial tests ───────────────────────────────────────────────────────────────

func TestDial_success(t *testing.T) {
	srv := newFakeMFSServer()
	addr := srv.Start()
	defer srv.Stop()

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var port int
	_, err = fmt.Sscanf(portStr, "%d", &port)
	require.NoError(t, err)

	c, err := Dial(host, port)
	require.NoError(t, err)
	assert.NotNil(t, c)
	_ = c.Close()
}

func TestDial_refused(t *testing.T) {
	// Bind to a port, close the listener immediately, then try to dial.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	_ = ln.Close()

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var port int
	_, err = fmt.Sscanf(portStr, "%d", &port)
	require.NoError(t, err)

	_, dialErr := Dial(host, port)
	assert.Error(t, dialErr)
}

// ─── ReadDir tests ────────────────────────────────────────────────────────────

func TestReadDir_root(t *testing.T) {
	c, _ := newTestClient(t)

	entries, err := c.ReadDir(RootNodeID)
	require.NoError(t, err)
	assert.NotNil(t, entries)
	assert.Empty(t, entries) // fresh server has no children under root
}

func TestReadDir_afterMkdir(t *testing.T) {
	c, _ := newTestClient(t)

	// Create a directory
	nodeID, err := c.Mkdir(RootNodeID, "testdir", 0o755)
	require.NoError(t, err)
	assert.Greater(t, nodeID, uint32(1))

	entries, err := c.ReadDir(RootNodeID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "testdir", entries[0].Name)
	assert.True(t, entries[0].IsDir)
}

// ─── GetAttr tests ────────────────────────────────────────────────────────────

func TestGetAttr_root(t *testing.T) {
	c, _ := newTestClient(t)

	attr, err := c.GetAttr(RootNodeID)
	require.NoError(t, err)
	require.NotNil(t, attr)
	assert.Equal(t, RootNodeID, attr.NodeID)
}

func TestGetAttr_notfound(t *testing.T) {
	c, _ := newTestClient(t)

	_, err := c.GetAttr(99999)
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

func TestGetAttr_file(t *testing.T) {
	c, _ := newTestClient(t)

	// Create a file and write some data
	nodeID, err := c.Mknod(RootNodeID, "test.txt", 0o644)
	require.NoError(t, err)

	data := []byte("hello moosefs")
	require.NoError(t, c.Write(nodeID, 0, data))

	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(data)), attr.Size)
}

// ─── Mknod tests ──────────────────────────────────────────────────────────────

func TestMknod_createFile(t *testing.T) {
	c, _ := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "file.txt", 0o644)
	require.NoError(t, err)
	assert.Greater(t, nodeID, RootNodeID)

	// Verify the file is listed
	entries, err := c.ReadDir(RootNodeID)
	require.NoError(t, err)
	found := false
	for _, e := range entries {
		if e.Name == "file.txt" {
			found = true
			assert.False(t, e.IsDir)
			break
		}
	}
	assert.True(t, found, "created file must appear in ReadDir")
}

// ─── Write / Read tests ───────────────────────────────────────────────────────

func TestWrite_appendChunks(t *testing.T) {
	c, _ := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "chunks.bin", 0o644)
	require.NoError(t, err)

	chunk1 := []byte("AAAA")
	chunk2 := []byte("BBBB")
	require.NoError(t, c.Write(nodeID, 0, chunk1))
	require.NoError(t, c.Write(nodeID, uint64(len(chunk1)), chunk2))

	// Verify size
	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(chunk1)+len(chunk2)), attr.Size)
}

func TestRead_content(t *testing.T) {
	c, _ := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "read_test.txt", 0o644)
	require.NoError(t, err)

	content := []byte("hello moosefs read test")
	require.NoError(t, c.Write(nodeID, 0, content))

	got, err := c.Read(nodeID, 0, uint32(len(content)))
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestRead_eof(t *testing.T) {
	c, _ := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "eof_test.txt", 0o644)
	require.NoError(t, err)

	content := []byte("short")
	require.NoError(t, c.Write(nodeID, 0, content))

	// Reading beyond EOF should return empty
	got, err := c.Read(nodeID, uint64(len(content))*2, 1024)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ─── Unlink tests ─────────────────────────────────────────────────────────────

func TestUnlink_file(t *testing.T) {
	c, _ := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "to_delete.txt", 0o644)
	require.NoError(t, err)
	assert.Greater(t, nodeID, uint32(0))

	require.NoError(t, c.Unlink(RootNodeID, "to_delete.txt"))

	// GetAttr on deleted node must return not-found
	_, err = c.GetAttr(nodeID)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

func TestUnlink_notfound(t *testing.T) {
	c, _ := newTestClient(t)

	err := c.Unlink(RootNodeID, "ghost.txt")
	assert.Error(t, err)
}

// ─── Rmdir tests ──────────────────────────────────────────────────────────────

func TestRmdir_emptyDir(t *testing.T) {
	c, _ := newTestClient(t)

	dirID, err := c.Mkdir(RootNodeID, "empty_dir", 0o755)
	require.NoError(t, err)
	assert.Greater(t, dirID, uint32(0))

	require.NoError(t, c.Rmdir(RootNodeID, "empty_dir"))

	_, err = c.GetAttr(dirID)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

func TestRmdir_notfound(t *testing.T) {
	c, _ := newTestClient(t)
	err := c.Rmdir(RootNodeID, "no_such_dir")
	assert.Error(t, err)
}

// ─── Mkdir tests ──────────────────────────────────────────────────────────────

func TestMkdir_createDir(t *testing.T) {
	c, _ := newTestClient(t)

	nodeID, err := c.Mkdir(RootNodeID, "new_dir", 0o755)
	require.NoError(t, err)
	assert.Greater(t, nodeID, RootNodeID)

	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, nodeID, attr.NodeID)
}
