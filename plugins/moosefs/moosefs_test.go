// Package moosefs_test provides integration tests for the MooseFS StorageBackend.
//
// Tests run against an in-memory fake MooseFS server embedded in this file.
// No external MooseFS installation is required.
//
// Each test creates its own server and backend instance to guarantee isolation.
package moosefs

import (
	"context"
	"encoding/binary"
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
		mode:    0o755,
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
	case mfsclient.CltomFuseRegister:
		s.svrRegister(conn, payload)
	case mfsclient.CltomFuseStatFS:
		s.svrStatFS(conn, payload)
	case mfsclient.CltomFuseLookup:
		s.svrLookup(conn, payload)
	case mfsclient.CltomFuseReadDir:
		s.svrReadDir(conn, payload)
	case mfsclient.CltomFuseGetAttr:
		s.svrGetAttr(conn, payload)
	case mfsclient.CltomFuseMknod:
		s.svrMknod(conn, payload)
	case mfsclient.CltomFuseMkdir:
		s.svrMkdir(conn, payload)
	case mfsclient.CmdFUSEWRITE: // Phase 1 stub opcode
		s.svrWrite(conn, payload)
	case mfsclient.CmdFUSEREAD: // Phase 1 stub opcode
		s.svrRead(conn, payload)
	case mfsclient.CltomFuseUnlink:
		s.svrUnlink(conn, payload)
	case mfsclient.CltomFuseRmdir:
		s.svrRmdir(conn, payload)
	default:
		_ = mfsclient.WriteFrame(conn, cmd+100, []byte{mfsclient.StatusERROR})
	}
}

func (s *integFakeServer) alloc() uint32 { return s.nextID.Add(1) - 1 }

// ─── Attr helpers ─────────────────────────────────────────────────────────────

// buildIntegAttrs encodes the 35-byte MooseFS wire attrs for an integNode.
func buildIntegAttrs(n *integNode) []byte {
	var mode16 uint16
	if n.isDir {
		mode16 = (2 << 12) | uint16(n.mode&0x0FFF)
	} else {
		mode16 = (1 << 12) | uint16(n.mode&0x0FFF)
	}
	var buf []byte
	buf = mfsclient.PutUint8(buf, 0)                       // flags
	buf = mfsclient.PutUint16(buf, mode16)                 // mode
	buf = mfsclient.PutUint32(buf, 0)                      // uid
	buf = mfsclient.PutUint32(buf, 0)                      // gid
	buf = mfsclient.PutUint32(buf, uint32(n.modTime))      // atime
	buf = mfsclient.PutUint32(buf, uint32(n.modTime))      // mtime
	buf = mfsclient.PutUint32(buf, uint32(n.modTime))      // ctime
	buf = mfsclient.PutUint32(buf, 1)                      // nlink
	buf = mfsclient.PutUint64(buf, uint64(len(n.content))) // size
	return buf // 35 bytes
}

// integWriteSuccess sends [msgid:32][data...] response.
func integWriteSuccess(conn net.Conn, ans uint32, msgid uint32, data []byte) {
	buf := mfsclient.PutUint32(nil, msgid)
	buf = append(buf, data...)
	_ = mfsclient.WriteFrame(conn, ans, buf)
}

// integWriteStatus sends [msgid:32][status:8] response.
func integWriteStatus(conn net.Conn, ans uint32, msgid uint32, status uint8) {
	buf := mfsclient.PutUint32(nil, msgid)
	buf = append(buf, status)
	_ = mfsclient.WriteFrame(conn, ans, buf)
}

// ─── Register / StatFS / Lookup handlers ─────────────────────────────────────

func (s *integFakeServer) svrRegister(conn net.Conn, payload []byte) {
	if len(payload) < 65 {
		_ = mfsclient.WriteFrame(conn, mfsclient.MatoclFuseRegister, []byte{mfsclient.StatusERROR})
		return
	}
	rcode := payload[64]
	if rcode == mfsclient.RegisterNewSession {
		var resp []byte
		resp = mfsclient.PutUint32(resp, 263168) // version
		resp = mfsclient.PutUint32(resp, 42)     // sessionId
		resp = mfsclient.PutUint64(resp, 0)      // metaId
		resp = mfsclient.PutUint8(resp, 0)       // sesflags
		resp = mfsclient.PutUint32(resp, 0)      // rootuid
		resp = mfsclient.PutUint32(resp, 0)      // rootgid
		resp = mfsclient.PutUint32(resp, 0)      // mapalluid
		resp = mfsclient.PutUint32(resp, 0)      // mapallgid
		resp = mfsclient.PutUint8(resp, 1)       // mingoal
		resp = mfsclient.PutUint8(resp, 9)       // maxgoal
		resp = mfsclient.PutUint32(resp, 0)      // mintrashtime
		resp = mfsclient.PutUint32(resp, 0)      // maxtrashtime
		_ = mfsclient.WriteFrame(conn, mfsclient.MatoclFuseRegister, resp)
		return
	}
	_ = mfsclient.WriteFrame(conn, mfsclient.MatoclFuseRegister, []byte{mfsclient.StatusERROR})
}

func (s *integFakeServer) svrStatFS(conn net.Conn, payload []byte) {
	var msgid uint32
	if len(payload) >= 4 {
		msgid = binary.BigEndian.Uint32(payload[0:4])
	}
	const TB = int64(1) << 40
	const GB500 = int64(500) << 30
	var resp []byte
	resp = mfsclient.PutUint32(resp, msgid)
	resp = mfsclient.PutUint64(resp, uint64(TB))
	resp = mfsclient.PutUint64(resp, uint64(GB500))
	resp = mfsclient.PutUint64(resp, 0)         // trashspace
	resp = mfsclient.PutUint64(resp, 0)         // sustainedspace
	resp = mfsclient.PutUint32(resp, 1_000_000) // inodes
	_ = mfsclient.WriteFrame(conn, mfsclient.MatoclFuseStatFS, resp)
}

func (s *integFakeServer) svrLookup(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][uid:32][gcnt:32][gid:32]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = mfsclient.ReadUint32(payload, 0)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseLookup, 0, mfsclient.StatusERROR)
		return
	}
	parentID, off, err = mfsclient.ReadUint32(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseLookup, msgid, mfsclient.StatusERROR)
		return
	}
	name, _, err := mfsclient.ReadStringU8(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseLookup, msgid, mfsclient.StatusERROR)
		return
	}

	s.mu.Lock()
	var found *integNode
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name {
			found = n
			break
		}
	}
	s.mu.Unlock()

	if found == nil {
		integWriteStatus(conn, mfsclient.MatoclFuseLookup, msgid, mfsclient.StatusENOENT)
		return
	}

	attrs := buildIntegAttrs(found)
	data := mfsclient.PutUint32(nil, found.nodeID)
	data = append(data, attrs...)
	integWriteSuccess(conn, mfsclient.MatoclFuseLookup, msgid, data)
}

// ─── Directory / file handlers ────────────────────────────────────────────────

func (s *integFakeServer) svrReadDir(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][uid:32][gcnt:32][gid:32][flags:8][maxentries:32][skipcnt:64]
	if len(payload) < 8 {
		integWriteStatus(conn, mfsclient.MatoclFuseReadDir, 0, mfsclient.StatusERROR)
		return
	}
	msgid := binary.BigEndian.Uint32(payload[0:4])
	nodeID := binary.BigEndian.Uint32(payload[4:8])

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
		integWriteStatus(conn, mfsclient.MatoclFuseReadDir, msgid, mfsclient.StatusENOENT)
		return
	}

	// Encode: [namelen:8][name][inode:32][type:8]
	var data []byte
	for _, c := range children {
		data = mfsclient.PutUint8(data, uint8(len(c.name)))
		data = append(data, []byte(c.name)...)
		data = mfsclient.PutUint32(data, c.nodeID)
		var nodeType uint8
		if c.isDir {
			nodeType = 2
		} else {
			nodeType = 1
		}
		data = mfsclient.PutUint8(data, nodeType)
	}
	integWriteSuccess(conn, mfsclient.MatoclFuseReadDir, msgid, data)
}

func (s *integFakeServer) svrGetAttr(conn net.Conn, payload []byte) {
	// [msgid:32][inode:32]
	if len(payload) < 8 {
		integWriteStatus(conn, mfsclient.MatoclFuseGetAttr, 0, mfsclient.StatusERROR)
		return
	}
	msgid := binary.BigEndian.Uint32(payload[0:4])
	nodeID := binary.BigEndian.Uint32(payload[4:8])

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	s.mu.Unlock()

	if !ok {
		integWriteStatus(conn, mfsclient.MatoclFuseGetAttr, msgid, mfsclient.StatusENOENT)
		return
	}

	attrs := buildIntegAttrs(n)
	integWriteSuccess(conn, mfsclient.MatoclFuseGetAttr, msgid, attrs)
}

func (s *integFakeServer) svrMknod(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][type:8][mode:16][umask:16][uid:32][gcnt:32][gid:32][rdev:32]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = mfsclient.ReadUint32(payload, 0)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseMknod, 0, mfsclient.StatusERROR)
		return
	}
	parentID, off, err = mfsclient.ReadUint32(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseMknod, msgid, mfsclient.StatusERROR)
		return
	}
	name, _, err := mfsclient.ReadStringU8(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseMknod, msgid, mfsclient.StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodes[parentID]; !ok {
		integWriteStatus(conn, mfsclient.MatoclFuseMknod, msgid, mfsclient.StatusENOENT)
		return
	}
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name {
			// Idempotent — return existing node.
			attrs := buildIntegAttrs(n)
			data := mfsclient.PutUint32(nil, n.nodeID)
			data = append(data, attrs...)
			integWriteSuccess(conn, mfsclient.MatoclFuseMknod, msgid, data)
			return
		}
	}
	id := s.alloc()
	nn := &integNode{
		nodeID: id, name: name, parent: parentID, isDir: false,
		mode: 0o644, modTime: time.Now().Unix(),
	}
	s.nodes[id] = nn
	attrs := buildIntegAttrs(nn)
	data := mfsclient.PutUint32(nil, id)
	data = append(data, attrs...)
	integWriteSuccess(conn, mfsclient.MatoclFuseMknod, msgid, data)
}

func (s *integFakeServer) svrMkdir(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][mode:16][umask:16][uid:32][gcnt:32][gid:32][copysgid:8]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = mfsclient.ReadUint32(payload, 0)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseMkdir, 0, mfsclient.StatusERROR)
		return
	}
	parentID, off, err = mfsclient.ReadUint32(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseMkdir, msgid, mfsclient.StatusERROR)
		return
	}
	name, _, err := mfsclient.ReadStringU8(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseMkdir, msgid, mfsclient.StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodes[parentID]; !ok {
		integWriteStatus(conn, mfsclient.MatoclFuseMkdir, msgid, mfsclient.StatusENOENT)
		return
	}
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name && n.isDir {
			// Idempotent — return existing node.
			attrs := buildIntegAttrs(n)
			data := mfsclient.PutUint32(nil, n.nodeID)
			data = append(data, attrs...)
			integWriteSuccess(conn, mfsclient.MatoclFuseMkdir, msgid, data)
			return
		}
	}
	id := s.alloc()
	nn := &integNode{
		nodeID: id, name: name, parent: parentID, isDir: true,
		mode: 0o755, modTime: time.Now().Unix(),
	}
	s.nodes[id] = nn
	attrs := buildIntegAttrs(nn)
	data := mfsclient.PutUint32(nil, id)
	data = append(data, attrs...)
	integWriteSuccess(conn, mfsclient.MatoclFuseMkdir, msgid, data)
}

// svrWrite uses the Phase 1 stub protocol (opcode 507 / ans 607).
func (s *integFakeServer) svrWrite(conn net.Conn, payload []byte) {
	nodeID, off, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEWRITE, []byte{mfsclient.StatusERROR})
		return
	}
	offset, off, err := mfsclient.ReadUint64(payload, off)
	if err != nil {
		_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEWRITE, []byte{mfsclient.StatusERROR})
		return
	}
	dataLen, off, err := mfsclient.ReadUint32(payload, off)
	if err != nil {
		_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEWRITE, []byte{mfsclient.StatusERROR})
		return
	}
	if off+int(dataLen) > len(payload) {
		_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEWRITE, []byte{mfsclient.StatusERROR})
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
		_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEWRITE, []byte{mfsclient.StatusENOENT})
		return
	}
	_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEWRITE, []byte{mfsclient.StatusOK})
}

// svrRead uses the Phase 1 stub protocol (opcode 506 / ans 606).
func (s *integFakeServer) svrRead(conn net.Conn, payload []byte) {
	nodeID, off, err := mfsclient.ReadUint32(payload, 0)
	if err != nil {
		_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEREAD, []byte{mfsclient.StatusERROR})
		return
	}
	offset, off, err := mfsclient.ReadUint64(payload, off)
	if err != nil {
		_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEREAD, []byte{mfsclient.StatusERROR})
		return
	}
	size, _, err := mfsclient.ReadUint32(payload, off)
	if err != nil {
		_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEREAD, []byte{mfsclient.StatusERROR})
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
		_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEREAD, []byte{mfsclient.StatusENOENT})
		return
	}
	buf := []byte{mfsclient.StatusOK}
	buf = mfsclient.PutUint32(buf, uint32(len(chunk)))
	buf = append(buf, chunk...)
	_ = mfsclient.WriteFrame(conn, mfsclient.AnsFUSEREAD, buf)
}

func (s *integFakeServer) svrUnlink(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][uid:32][gcnt:32][gid:32]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = mfsclient.ReadUint32(payload, 0)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseUnlink, 0, mfsclient.StatusERROR)
		return
	}
	parentID, off, err = mfsclient.ReadUint32(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseUnlink, msgid, mfsclient.StatusERROR)
		return
	}
	name, _, err := mfsclient.ReadStringU8(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseUnlink, msgid, mfsclient.StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, n := range s.nodes {
		if n.parent == parentID && n.name == name && !n.isDir {
			delete(s.nodes, id)
			integWriteStatus(conn, mfsclient.MatoclFuseUnlink, msgid, mfsclient.StatusOK)
			return
		}
	}
	integWriteStatus(conn, mfsclient.MatoclFuseUnlink, msgid, mfsclient.StatusENOENT)
}

func (s *integFakeServer) svrRmdir(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][uid:32][gcnt:32][gid:32]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = mfsclient.ReadUint32(payload, 0)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseRmdir, 0, mfsclient.StatusERROR)
		return
	}
	parentID, off, err = mfsclient.ReadUint32(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseRmdir, msgid, mfsclient.StatusERROR)
		return
	}
	name, _, err := mfsclient.ReadStringU8(payload, off)
	if err != nil {
		integWriteStatus(conn, mfsclient.MatoclFuseRmdir, msgid, mfsclient.StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, n := range s.nodes {
		if n.parent == parentID && n.name == name && n.isDir {
			for _, child := range s.nodes {
				if child.parent == id {
					integWriteStatus(conn, mfsclient.MatoclFuseRmdir, msgid, mfsclient.StatusENOTEMPTY)
					return
				}
			}
			delete(s.nodes, id)
			integWriteStatus(conn, mfsclient.MatoclFuseRmdir, msgid, mfsclient.StatusOK)
			return
		}
	}
	integWriteStatus(conn, mfsclient.MatoclFuseRmdir, msgid, mfsclient.StatusENOENT)
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
	// Second call: fake server returns the existing node (idempotent success).
	require.NoError(t, b.CreateDir(ctx, "/idemdir"))
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

func TestGetQuota_realValues(t *testing.T) {
	b := newTestBackend(t, startFakeServer(t))
	free, total, err := b.GetQuota(context.Background())
	require.NoError(t, err)
	// Fake server returns 500GB free, 1TB total.
	assert.Greater(t, free, int64(0), "free must be positive")
	assert.Greater(t, total, int64(0), "total must be positive")
	assert.LessOrEqual(t, free, total, "free <= total")
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
