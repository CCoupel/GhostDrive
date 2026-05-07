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
// It embeds a fakeCSServer to handle chunk I/O via the real CS protocol.
type fakeMFSServer struct {
	listener net.Listener
	mu       sync.Mutex
	nodes    map[uint32]*fakeNode
	nextID   atomic.Uint32
	done     chan struct{}
	cs       *fakeCSServer // embedded chunk server for Read/Write operations
	csIP     uint32        // CS listen IP (uint32 big-endian)
	csPort   uint16        // CS listen port
}

// newFakeMFSServer creates and initialises a fake server with only the root
// node present.
func newFakeMFSServer() *fakeMFSServer {
	s := &fakeMFSServer{
		nodes: make(map[uint32]*fakeNode),
		done:  make(chan struct{}),
		cs:    newFakeCSServer(),
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

// Start binds to a random port, starts the embedded chunk server, begins
// accepting master connections, and returns the listen address in "host:port".
func (s *fakeMFSServer) Start() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("fakeMFSServer: listen: " + err.Error())
	}
	s.listener = ln
	// Start the fake CS so it is ready before any client connects.
	s.csIP, s.csPort = s.cs.Start()
	go s.acceptLoop()
	return ln.Addr().String()
}

// Stop closes the listener, stops the embedded CS, and waits for the accept
// goroutine to exit.
func (s *fakeMFSServer) Stop() {
	_ = s.listener.Close()
	s.cs.Stop()
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
	case CltomFuseRegister:
		s.handleRegister(conn, payload)
	case CltomFuseStatFS:
		s.handleStatFS(conn, payload)
	case CltomFuseLookup:
		s.handleLookup(conn, payload)
	case CltomFuseReadDir:
		s.handleReadDir(conn, payload)
	case CltomFuseGetAttr:
		s.handleGetAttr(conn, payload)
	case CltomFuseMknod:
		s.handleMknod(conn, payload)
	case CltomFuseMkdir:
		s.handleMkdir(conn, payload)
	case CmdFUSEWRITE: // Phase 1 stub opcode — kept for backward compat; no longer called by client
		s.handleWrite(conn, payload)
	case CmdFUSEREAD: // Phase 1 stub opcode — kept for backward compat; no longer called by client
		s.handleRead(conn, payload)
	case CltomFuseReadChunk:
		s.handleReadChunk(conn, payload)
	case CltomFuseWriteChunk:
		s.handleWriteChunk(conn, payload)
	case CltomFuseWriteChunkEnd:
		s.handleWriteChunkEnd(conn, payload)
	case CltomFuseUnlink:
		s.handleUnlink(conn, payload)
	case CltomFuseRmdir:
		s.handleRmdir(conn, payload)
	default:
		// Unknown command: respond with a generic error frame.
		_ = WriteFrame(conn, cmd+100, []byte{StatusERROR})
	}
}

// ─── Helper: allocate a new nodeID ───────────────────────────────────────────

func (s *fakeMFSServer) allocID() uint32 {
	return s.nextID.Add(1) - 1
}

// ─── Handler helpers ──────────────────────────────────────────────────────────

// buildTestAttrs encodes the 35-byte MooseFS wire attrs for a fakeNode.
func buildTestAttrs(n *fakeNode) []byte {
	var mode16 uint16
	if n.isDir {
		mode16 = (2 << 12) | uint16(n.mode&0x0FFF)
	} else {
		mode16 = (1 << 12) | uint16(n.mode&0x0FFF)
	}
	var buf []byte
	buf = PutUint8(buf, 0)                         // flags
	buf = PutUint16(buf, mode16)                   // mode
	buf = PutUint32(buf, 0)                        // uid
	buf = PutUint32(buf, 0)                        // gid
	buf = PutUint32(buf, uint32(n.modTime))        // atime
	buf = PutUint32(buf, uint32(n.modTime))        // mtime
	buf = PutUint32(buf, uint32(n.modTime))        // ctime
	buf = PutUint32(buf, 1)                        // nlink
	buf = PutUint64(buf, uint64(len(n.content)))   // size
	return buf // 35 bytes
}

// writeSuccess sends a response frame with [msgid:32][data...].
func writeSuccess(conn net.Conn, ans uint32, msgid uint32, data []byte) {
	buf := PutUint32(nil, msgid)
	buf = append(buf, data...)
	_ = WriteFrame(conn, ans, buf)
}

// writeStatusReply sends [msgid:32][status:8].
func writeStatusReply(conn net.Conn, ans uint32, msgid uint32, status uint8) {
	buf := PutUint32(nil, msgid)
	buf = append(buf, status)
	_ = WriteFrame(conn, ans, buf)
}

// ─── Command handlers ─────────────────────────────────────────────────────────

func (s *fakeMFSServer) handleRegister(conn net.Conn, payload []byte) {
	// [blob:64B][rcode:8][...] — we only care about rcode for NEWSESSION
	if len(payload) < 65 {
		_ = WriteFrame(conn, MatoclFuseRegister, []byte{StatusERROR})
		return
	}
	rcode := payload[64]
	if rcode == RegisterNewSession {
		// Build a success response:
		// [version:32][sessionId:32][metaId:64][sesflags:8][rootuid:32][rootgid:32]
		// [mapalluid:32][mapallgid:32][mingoal:8][maxgoal:8][mintrashtime:32][maxtrashtime:32]
		var resp []byte
		resp = PutUint32(resp, 263168) // version
		resp = PutUint32(resp, 42)     // sessionId
		resp = PutUint64(resp, 0)      // metaId
		resp = PutUint8(resp, 0)       // sesflags
		resp = PutUint32(resp, 0)      // rootuid
		resp = PutUint32(resp, 0)      // rootgid
		resp = PutUint32(resp, 0)      // mapalluid
		resp = PutUint32(resp, 0)      // mapallgid
		resp = PutUint8(resp, 1)       // mingoal
		resp = PutUint8(resp, 9)       // maxgoal
		resp = PutUint32(resp, 0)      // mintrashtime
		resp = PutUint32(resp, 0)      // maxtrashtime
		_ = WriteFrame(conn, MatoclFuseRegister, resp)
		return
	}
	_ = WriteFrame(conn, MatoclFuseRegister, []byte{StatusERROR})
}

func (s *fakeMFSServer) handleStatFS(conn net.Conn, payload []byte) {
	var msgid uint32
	if len(payload) >= 4 {
		msgid = binary.BigEndian.Uint32(payload[0:4])
	}
	// Return: 1TB total, 500GB avail, 0 trash, 0 sustained, 1M inodes
	const TB = int64(1) << 40
	const GB500 = int64(500) << 30
	var resp []byte
	resp = PutUint32(resp, msgid)
	resp = PutUint64(resp, uint64(TB))
	resp = PutUint64(resp, uint64(GB500))
	resp = PutUint64(resp, 0) // trashspace
	resp = PutUint64(resp, 0) // sustainedspace
	resp = PutUint32(resp, 1_000_000) // inodes
	_ = WriteFrame(conn, MatoclFuseStatFS, resp)
}

func (s *fakeMFSServer) handleLookup(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][uid:32][gcnt:32][gid:32]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = ReadUint32(payload, 0)
	if err != nil {
		writeStatusReply(conn, MatoclFuseLookup, 0, StatusERROR)
		return
	}
	parentID, off, err = ReadUint32(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseLookup, msgid, StatusERROR)
		return
	}
	name, _, err := ReadStringU8(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseLookup, msgid, StatusERROR)
		return
	}

	s.mu.Lock()
	var found *fakeNode
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name {
			found = n
			break
		}
	}
	s.mu.Unlock()

	if found == nil {
		writeStatusReply(conn, MatoclFuseLookup, msgid, StatusENOENT)
		return
	}

	// Success: [msgid:32][inode:32][attrs:35]
	attrs := buildTestAttrs(found)
	data := PutUint32(nil, found.nodeID)
	data = append(data, attrs...)
	writeSuccess(conn, MatoclFuseLookup, msgid, data)
}

func (s *fakeMFSServer) handleReadDir(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][uid:32][gcnt:32][gid:32][flags:8][maxentries:32][skipcnt:64]
	if len(payload) < 8 {
		writeStatusReply(conn, MatoclFuseReadDir, 0, StatusERROR)
		return
	}
	msgid := binary.BigEndian.Uint32(payload[0:4])
	nodeID := binary.BigEndian.Uint32(payload[4:8])

	s.mu.Lock()
	parent, ok := s.nodes[nodeID]
	var children []*fakeNode
	if ok && parent.isDir {
		for _, n := range s.nodes {
			if n.parent == nodeID && n.nodeID != RootNodeID {
				cp := *n
				children = append(children, &cp)
			}
		}
	}
	s.mu.Unlock()

	if !ok {
		writeStatusReply(conn, MatoclFuseReadDir, msgid, StatusENOENT)
		return
	}

	// Encode: [msgid:32][entries...] where each = [namelen:8][name][inode:32][type:8]
	var data []byte
	for _, child := range children {
		data = PutUint8(data, uint8(len(child.name)))
		data = append(data, []byte(child.name)...)
		data = PutUint32(data, child.nodeID)
		var nodeType uint8
		if child.isDir {
			nodeType = 2
		} else {
			nodeType = 1
		}
		data = PutUint8(data, nodeType)
	}
	writeSuccess(conn, MatoclFuseReadDir, msgid, data)
}

func (s *fakeMFSServer) handleGetAttr(conn net.Conn, payload []byte) {
	// [msgid:32][inode:32]
	if len(payload) < 8 {
		writeStatusReply(conn, MatoclFuseGetAttr, 0, StatusERROR)
		return
	}
	msgid := binary.BigEndian.Uint32(payload[0:4])
	nodeID := binary.BigEndian.Uint32(payload[4:8])

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	s.mu.Unlock()

	if !ok {
		writeStatusReply(conn, MatoclFuseGetAttr, msgid, StatusENOENT)
		return
	}

	// Success: [msgid:32][attrs:35]
	attrs := buildTestAttrs(n)
	writeSuccess(conn, MatoclFuseGetAttr, msgid, attrs)
}

func (s *fakeMFSServer) handleMknod(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][type:8][mode:16][umask:16][uid:32][gcnt:32][gid:32][rdev:32]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = ReadUint32(payload, 0)
	if err != nil {
		writeStatusReply(conn, MatoclFuseMknod, 0, StatusERROR)
		return
	}
	parentID, off, err = ReadUint32(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseMknod, msgid, StatusERROR)
		return
	}
	name, off, err := ReadStringU8(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseMknod, msgid, StatusERROR)
		return
	}
	// skip type(1) + mode(2) + umask(2)
	off += 5
	// skip uid(4) + gcnt(4) + gid(4) + rdev(4) — not needed for fake
	_ = off

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodes[parentID]; !ok {
		writeStatusReply(conn, MatoclFuseMknod, msgid, StatusENOENT)
		return
	}

	// Check for duplicate — return existing node (idempotent in fake)
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name {
			attrs := buildTestAttrs(n)
			data := PutUint32(nil, n.nodeID)
			data = append(data, attrs...)
			writeSuccess(conn, MatoclFuseMknod, msgid, data)
			return
		}
	}

	newID := s.allocID()
	newNode := &fakeNode{
		nodeID:  newID,
		name:    name,
		parent:  parentID,
		isDir:   false,
		mode:    0o644,
		modTime: time.Now().Unix(),
	}
	s.nodes[newID] = newNode

	attrs := buildTestAttrs(newNode)
	data := PutUint32(nil, newID)
	data = append(data, attrs...)
	writeSuccess(conn, MatoclFuseMknod, msgid, data)
}

func (s *fakeMFSServer) handleMkdir(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][mode:16][umask:16][uid:32][gcnt:32][gid:32][copysgid:8]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = ReadUint32(payload, 0)
	if err != nil {
		writeStatusReply(conn, MatoclFuseMkdir, 0, StatusERROR)
		return
	}
	parentID, off, err = ReadUint32(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseMkdir, msgid, StatusERROR)
		return
	}
	name, _, err := ReadStringU8(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseMkdir, msgid, StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodes[parentID]; !ok {
		writeStatusReply(conn, MatoclFuseMkdir, msgid, StatusENOENT)
		return
	}

	// Check for duplicate
	for _, n := range s.nodes {
		if n.parent == parentID && n.name == name && n.isDir {
			attrs := buildTestAttrs(n)
			data := PutUint32(nil, n.nodeID)
			data = append(data, attrs...)
			writeSuccess(conn, MatoclFuseMkdir, msgid, data)
			return
		}
	}

	newID := s.allocID()
	newNode := &fakeNode{
		nodeID:  newID,
		name:    name,
		parent:  parentID,
		isDir:   true,
		mode:    0o755,
		modTime: time.Now().Unix(),
	}
	s.nodes[newID] = newNode

	attrs := buildTestAttrs(newNode)
	data := PutUint32(nil, newID)
	data = append(data, attrs...)
	writeSuccess(conn, MatoclFuseMkdir, msgid, data)
}

// handleWrite uses the Phase 1 stub protocol (opcode 507 / ans 607).
// Format: [nodeID:32][offset:64][dataLen:32][data:dataLen]
// Response: [status:8]
func (s *fakeMFSServer) handleWrite(conn net.Conn, payload []byte) {
	nodeID, off, err := ReadUint32(payload, 0)
	if err != nil {
		_ = WriteFrame(conn, AnsFUSEWRITE, []byte{StatusERROR})
		return
	}
	offset, off, err := ReadUint64(payload, off)
	if err != nil {
		_ = WriteFrame(conn, AnsFUSEWRITE, []byte{StatusERROR})
		return
	}
	dataLen, off, err := ReadUint32(payload, off)
	if err != nil {
		_ = WriteFrame(conn, AnsFUSEWRITE, []byte{StatusERROR})
		return
	}
	if off+int(dataLen) > len(payload) {
		_ = WriteFrame(conn, AnsFUSEWRITE, []byte{StatusERROR})
		return
	}
	data := payload[off : off+int(dataLen)]

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	if ok && !n.isDir {
		end := offset + uint64(len(data))
		if end > uint64(len(n.content)) {
			newContent := make([]byte, end)
			copy(newContent, n.content)
			n.content = newContent
		}
		copy(n.content[offset:], data)
		n.modTime = time.Now().Unix()
	}
	s.mu.Unlock()

	if !ok {
		_ = WriteFrame(conn, AnsFUSEWRITE, []byte{StatusENOENT})
		return
	}
	_ = WriteFrame(conn, AnsFUSEWRITE, []byte{StatusOK})
}

// handleRead uses the Phase 1 stub protocol (opcode 506 / ans 606).
// Format: [nodeID:32][offset:64][size:32]
// Response: [status:8][dataLen:32][data:dataLen]
func (s *fakeMFSServer) handleRead(conn net.Conn, payload []byte) {
	nodeID, off, err := ReadUint32(payload, 0)
	if err != nil {
		_ = WriteFrame(conn, AnsFUSEREAD, []byte{StatusERROR})
		return
	}
	offset, off, err := ReadUint64(payload, off)
	if err != nil {
		_ = WriteFrame(conn, AnsFUSEREAD, []byte{StatusERROR})
		return
	}
	size, _, err := ReadUint32(payload, off)
	if err != nil {
		_ = WriteFrame(conn, AnsFUSEREAD, []byte{StatusERROR})
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
		_ = WriteFrame(conn, AnsFUSEREAD, []byte{StatusENOENT})
		return
	}

	buf := []byte{StatusOK}
	buf = PutUint32(buf, uint32(len(chunk)))
	buf = append(buf, chunk...)
	_ = WriteFrame(conn, AnsFUSEREAD, buf)
}

// ─── Chunk server coordination handlers ───────────────────────────────────────

// handleReadChunk handles CLTOMA_FUSE_READ_CHUNK (432).
//
// Payload: [msgid:32][nodeID:32][index:32]
// Response: [msgid:32][chunkID:64][version:32][N:8=1][ip:32][port:16][csVersion:32]
//
// Before responding, the handler pre-loads the node's content into the
// embedded fakeCSServer so that the subsequent ReadChunk call against the CS
// always sees the latest committed data.
func (s *fakeMFSServer) handleReadChunk(conn net.Conn, payload []byte) {
	if len(payload) < 12 {
		return
	}
	msgid, off, _ := ReadUint32(payload, 0)
	nodeID, _, _ := ReadUint32(payload, off)

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	if ok {
		// Sync node.content → CS so reads always see committed data.
		s.cs.SetChunkData(uint64(nodeID), n.content)
	}
	s.mu.Unlock()

	if !ok {
		writeStatusReply(conn, MatoclFuseReadChunk, msgid, StatusENOENT)
		return
	}

	var resp []byte
	resp = PutUint32(resp, msgid)
	resp = PutUint64(resp, uint64(nodeID)) // chunkID = nodeID (fake mapping)
	resp = PutUint32(resp, 1)              // chunk version
	resp = PutUint8(resp, 1)              // N = 1 server
	resp = PutUint32(resp, s.csIP)
	resp = PutUint16(resp, s.csPort)
	resp = PutUint32(resp, 0) // CS version (unused in fake)
	_ = WriteFrame(conn, MatoclFuseReadChunk, resp)
}

// handleWriteChunk handles CLTOMA_FUSE_WRITE_CHUNK (434).
//
// Payload: [msgid:32][nodeID:32][index:32][lockid:32]
// Response: [msgid:32][chunkID:64][version:32][N:8=1][ip:32][port:16][csVersion:32]
func (s *fakeMFSServer) handleWriteChunk(conn net.Conn, payload []byte) {
	if len(payload) < 16 {
		return
	}
	msgid, off, _ := ReadUint32(payload, 0)
	nodeID, _, _ := ReadUint32(payload, off)

	s.mu.Lock()
	_, ok := s.nodes[nodeID]
	s.mu.Unlock()

	if !ok {
		writeStatusReply(conn, MatoclFuseWriteChunk, msgid, StatusENOENT)
		return
	}

	var resp []byte
	resp = PutUint32(resp, msgid)
	resp = PutUint64(resp, uint64(nodeID)) // chunkID = nodeID (fake mapping)
	resp = PutUint32(resp, 1)              // chunk version
	resp = PutUint8(resp, 1)              // N = 1 server
	resp = PutUint32(resp, s.csIP)
	resp = PutUint16(resp, s.csPort)
	resp = PutUint32(resp, 0) // CS version (unused in fake)
	_ = WriteFrame(conn, MatoclFuseWriteChunk, resp)
}

// handleWriteChunkEnd handles CLTOMA_FUSE_WRITE_CHUNK_END (436).
//
// Payload: [msgid:32][chunkID:64][version:32][length:64][lockid:32]
//
// Copies the data written to the fakeCSServer back into node.content and
// sets the node's size to `length` (total bytes written so far).
// Response: [msgid:32][status:8]
func (s *fakeMFSServer) handleWriteChunkEnd(conn net.Conn, payload []byte) {
	if len(payload) < 28 {
		return
	}
	msgid, off, _ := ReadUint32(payload, 0)
	chunkID, off, _ := ReadUint64(payload, off)
	_, off, _ = ReadUint32(payload, off)   // version (skip)
	length, _, _ := ReadUint64(payload, off) // new total file length

	nodeID := uint32(chunkID) // reverse of fake mapping: chunkID = nodeID

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	if ok && !n.isDir {
		// Pull data written to CS back into node.content.
		csData := s.cs.GetChunkData(chunkID)
		newContent := make([]byte, length)
		copy(newContent, csData)
		n.content = newContent
		n.modTime = time.Now().Unix()
	}
	s.mu.Unlock()

	if !ok {
		writeStatusReply(conn, MatoclFuseWriteChunkEnd, msgid, StatusENOENT)
		return
	}
	writeStatusReply(conn, MatoclFuseWriteChunkEnd, msgid, StatusOK)
}

func (s *fakeMFSServer) handleUnlink(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][uid:32][gcnt:32][gid:32]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = ReadUint32(payload, 0)
	if err != nil {
		writeStatusReply(conn, MatoclFuseUnlink, 0, StatusERROR)
		return
	}
	parentID, off, err = ReadUint32(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseUnlink, msgid, StatusERROR)
		return
	}
	name, _, err := ReadStringU8(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseUnlink, msgid, StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, n := range s.nodes {
		if n.parent == parentID && n.name == name && !n.isDir {
			delete(s.nodes, id)
			writeStatusReply(conn, MatoclFuseUnlink, msgid, StatusOK)
			return
		}
	}
	writeStatusReply(conn, MatoclFuseUnlink, msgid, StatusENOENT)
}

func (s *fakeMFSServer) handleRmdir(conn net.Conn, payload []byte) {
	// [msgid:32][parent:32][namelen:8][name][uid:32][gcnt:32][gid:32]
	var err error
	var msgid, parentID uint32
	var off int

	msgid, off, err = ReadUint32(payload, 0)
	if err != nil {
		writeStatusReply(conn, MatoclFuseRmdir, 0, StatusERROR)
		return
	}
	parentID, off, err = ReadUint32(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseRmdir, msgid, StatusERROR)
		return
	}
	name, _, err := ReadStringU8(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseRmdir, msgid, StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, n := range s.nodes {
		if n.parent == parentID && n.name == name && n.isDir {
			// Check not empty.
			for _, child := range s.nodes {
				if child.parent == id {
					writeStatusReply(conn, MatoclFuseRmdir, msgid, StatusENOTEMPTY)
					return
				}
			}
			delete(s.nodes, id)
			writeStatusReply(conn, MatoclFuseRmdir, msgid, StatusOK)
			return
		}
	}
	writeStatusReply(conn, MatoclFuseRmdir, msgid, StatusENOENT)
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

	// Register with the fake server.
	require.NoError(t, c.Register())
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

// ─── Register tests ───────────────────────────────────────────────────────────

func TestRegister_success(t *testing.T) {
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var port int
	_, err = fmt.Sscanf(portStr, "%d", &port)
	require.NoError(t, err)

	c, err := Dial(host, port)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	err = c.Register()
	require.NoError(t, err)
	assert.Equal(t, uint32(42), c.SessionID(), "sessionID should be 42 from fake server")
}

// ─── StatFS tests ─────────────────────────────────────────────────────────────

func TestStatFS_values(t *testing.T) {
	c, _ := newTestClient(t)

	free, total, err := c.StatFS()
	require.NoError(t, err)

	const TB = int64(1) << 40
	const GB500 = int64(500) << 30

	assert.Equal(t, GB500, free, "free should be 500GB")
	assert.Equal(t, TB, total, "total should be 1TB")
}

// ─── Lookup tests ─────────────────────────────────────────────────────────────

func TestLookup_found(t *testing.T) {
	c, _ := newTestClient(t)

	// Create a directory first.
	nodeID, err := c.Mkdir(RootNodeID, "lookupdir", 0o755)
	require.NoError(t, err)
	assert.Greater(t, nodeID, uint32(1))

	// Lookup by name.
	found, err := c.Lookup(RootNodeID, "lookupdir")
	require.NoError(t, err)
	assert.Equal(t, nodeID, found)
}

func TestLookup_notfound(t *testing.T) {
	c, _ := newTestClient(t)

	_, err := c.Lookup(RootNodeID, "does-not-exist")
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
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

	// Create a directory.
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
	assert.True(t, attr.IsDir())
}

func TestGetAttr_notfound(t *testing.T) {
	c, _ := newTestClient(t)

	_, err := c.GetAttr(99999)
	require.Error(t, err)
	assert.ErrorIs(t, err, plugins.ErrFileNotFound)
}

func TestGetAttr_file(t *testing.T) {
	c, _ := newTestClient(t)

	// Create a file and write some data.
	nodeID, err := c.Mknod(RootNodeID, "test.txt", 0o644)
	require.NoError(t, err)

	data := []byte("hello moosefs")
	require.NoError(t, c.Write(nodeID, 0, data))

	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(data)), attr.Size)
	assert.False(t, attr.IsDir())
}

// ─── Mknod tests ──────────────────────────────────────────────────────────────

func TestMknod_createFile(t *testing.T) {
	c, _ := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "file.txt", 0o644)
	require.NoError(t, err)
	assert.Greater(t, nodeID, RootNodeID)

	// Verify the file is listed.
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

	// Verify size.
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

	// Reading beyond EOF should return empty.
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

	// GetAttr on deleted node must return not-found.
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
	assert.True(t, attr.IsDir())
}
