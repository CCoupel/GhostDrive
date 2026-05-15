// Package mfsclient_test provides unit tests for the mfsclient TCP client.
//
// Tests run against an in-memory fake MooseFS server (fakeMFSServer) that
// implements the same binary protocol as the Client.  No external MooseFS
// installation is required.
package mfsclient

import (
	"bytes"
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

	// Optional primary CS (listed first in WRITE_CHUNK responses when cs1Port != 0).
	// Used by TestWrite_FallbackCS2 to inject a "bad" CS1 before the working CS2.
	cs1IP   uint32
	cs1Port uint16

	// If > 0, WRITE_CHUNK responses include only cs1 (cs2 "disappears") starting
	// from this call number.  Call 1 is the first WRITE_CHUNK received.
	// Used by TestWrite_ShrinkingServerList to test the shrink-guard lock-release path.
	singleCSFrom int32

	// Counters for verifying fallback behaviour in tests.
	writeChunkCalls           atomic.Int32
	writeChunkEndCalls        atomic.Int32
	writeChunkEndReleaseCalls atomic.Int32 // subset of writeChunkEndCalls where size==0 (no data committed)

	// Fault injection knobs (zero value = disabled).
	writeChunkErrOnCall           atomic.Int32 // return StatusERROR on Nth WRITE_CHUNK call (1-based)
	writeChunkEndReleaseErrOnCall atomic.Int32 // return StatusERROR for Nth release WRITE_CHUNK_END (1-based)
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
	case CltomFuseRename:
		s.handleRename(conn, payload)
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

	// Encode: [next_skipcnt:64=0][entries...] where each = [namelen:8][name][inode:32][dtype:8]
	// Real MooseFS wire format: 8-byte pagination field before entries, then 1-byte dtype (not full attrs).
	var data []byte
	data = PutUint64(data, 0) // next_skipcnt = 0 (no more pages)
	for _, child := range children {
		var nodeType uint8 = 1 // file
		if child.isDir {
			nodeType = 2
		}
		data = PutUint8(data, uint8(len(child.name)))
		data = append(data, []byte(child.name)...)
		data = PutUint32(data, child.nodeID)
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
// Payload: [msgid:32][nodeID:32][index:32] (chunkopflags:8 optional, accepted if present)
//
// Multi-chunk support: uses the chunk index to return a distinct chunkID per
// chunk and pre-loads the correct slice of node.content into the CS.
//
//   chunkID = (uint64(nodeID) << 32) | uint64(chunkIndex)
//   CS data  = node.content[chunkIndex*ChunkSize : (chunkIndex+1)*ChunkSize]
//
// When chunkIndex*ChunkSize >= fileLength the master would normally return a
// 5-byte StatusOK (EOF), but some MooseFS versions return a proto=2 response
// with nCS=0 instead (e.g. when the file size is exactly a multiple of
// ChunkSize).  This fake server mimics that behaviour to exercise the EOF fix
// in client.go.
//
// Response proto 2 (nCS=1 when chunk exists):
//
//	[msgid:32][protocolid:8=2][length:64][chunkID:64][version:32]
//	1×[ip:32 port:16 cs_ver:32 labelmask:32]
//
// Response proto 2 (nCS=0 when chunk is at/past EOF):
//
//	[msgid:32][protocolid:8=2][length:64][chunkID:64][version:32]
//	(no CS entries)
func (s *fakeMFSServer) handleReadChunk(conn net.Conn, payload []byte) {
	if len(payload) < 12 {
		return
	}
	msgid, off, _ := ReadUint32(payload, 0)
	nodeID, off, _ := ReadUint32(payload, off)
	chunkIndex, _, _ := ReadUint32(payload, off)

	s.mu.Lock()
	n, ok := s.nodes[nodeID]
	s.mu.Unlock()

	if !ok {
		writeStatusReply(conn, MatoclFuseReadChunk, msgid, StatusENOENT)
		return
	}

	fileLen := uint64(len(n.content))
	chunkStart := uint64(chunkIndex) * ChunkSize

	// Compound chunkID: upper 32 bits = nodeID, lower 32 bits = chunkIndex.
	// This ensures each MooseFS chunk of the same inode has a distinct ID in
	// the fake CS store, matching real MooseFS master behaviour.
	chunkID := (uint64(nodeID) << 32) | uint64(chunkIndex)

	if chunkStart < fileLen {
		// Pre-load this chunk's slice into the CS so ReadChunk always sees
		// committed data (mirrors what the real master + CS provide).
		chunkEnd := chunkStart + ChunkSize
		if chunkEnd > fileLen {
			chunkEnd = fileLen
		}
		s.cs.SetChunkData(chunkID, n.content[chunkStart:chunkEnd])
	}

	// Build proto 2 response.
	var resp []byte
	resp = PutUint32(resp, msgid)
	resp = PutUint8(resp, 2)        // protocolid = 2
	resp = PutUint64(resp, fileLen) // current file length
	resp = PutUint64(resp, chunkID) // compound chunkID
	resp = PutUint32(resp, 1)       // chunk version

	if chunkStart < fileLen {
		// Include the CS entry — data is available.
		resp = PutUint32(resp, s.csIP)
		resp = PutUint16(resp, s.csPort)
		resp = PutUint32(resp, 0) // cs_ver (unused in fake)
		resp = PutUint32(resp, 0) // labelmask (unused in fake)
	}
	// If chunkStart >= fileLen: no CS entries (nCS=0) — simulates MooseFS
	// returning a boundary-slot proto response instead of 5-byte StatusOK.
	_ = WriteFrame(conn, MatoclFuseReadChunk, resp)
}

// handleWriteChunk handles CLTOMA_FUSE_WRITE_CHUNK (434).
//
// Payload (MooseFS 4.x / >= 3.0.4): [msgid:32][nodeID:32][index:32][chunkopflags:8]
// Response proto 2:
//
//	[msgid:32][protocolid:8=2][length:64][chunkID:64][version:32]
//	N×[ip:32 port:16 cs_ver:32 labelmask:32]
//
// When s.cs1Port != 0, two CS entries are returned: cs1 first (the "bad" server),
// cs2 second (the working server). This enables fallback tests.
func (s *fakeMFSServer) handleWriteChunk(conn net.Conn, payload []byte) {
	// Minimum 13 bytes: msgid(4)+nodeID(4)+index(4)+chunkopflags(1).
	// Accept 16 bytes too for backward compat with old test callers.
	if len(payload) < 13 {
		return
	}
	msgid, off, _ := ReadUint32(payload, 0)
	nodeID, _, _ := ReadUint32(payload, off)

	s.mu.Lock()
	node, ok := s.nodes[nodeID]
	s.mu.Unlock()

	if !ok {
		writeStatusReply(conn, MatoclFuseWriteChunk, msgid, StatusENOENT)
		return
	}

	// Track how many WRITE_CHUNK calls we receive (used by fallback tests).
	s.writeChunkCalls.Add(1)
	callNum := s.writeChunkCalls.Load() // 1-based call number

	// Fault injection: return StatusERROR on the Nth call if configured.
	if n := s.writeChunkErrOnCall.Load(); n > 0 && callNum == n {
		writeStatusReply(conn, MatoclFuseWriteChunk, msgid, StatusERROR)
		return
	}

	// Build proto 2 response: protocolid=2, file length, chunkid, version, N server entries.
	fileLen := uint64(0)
	if node != nil {
		fileLen = uint64(len(node.content))
	}
	var resp []byte
	resp = PutUint32(resp, msgid)
	resp = PutUint8(resp, 2)              // protocolid = 2
	resp = PutUint64(resp, fileLen)        // current file length
	resp = PutUint64(resp, uint64(nodeID)) // chunkID = nodeID (fake mapping)
	resp = PutUint32(resp, 1)              // chunk version

	// Determine which CSes to include in the response.
	// singleCSFrom > 0: shrink to cs1-only starting from that call number.
	shrunk := s.cs1Port != 0 && s.singleCSFrom > 0 && callNum >= s.singleCSFrom
	if shrunk {
		// Shrunk mode: return only cs1 (cs2 has "left the cluster").
		resp = PutUint32(resp, s.cs1IP)
		resp = PutUint16(resp, s.cs1Port)
		resp = PutUint32(resp, 0) // cs_ver (unused in fake)
		resp = PutUint32(resp, 0) // labelmask (unused in fake)
	} else {
		if s.cs1Port != 0 {
			// Dual-CS mode: list cs1 first (bad), embedded cs2 second (working).
			resp = PutUint32(resp, s.cs1IP)
			resp = PutUint16(resp, s.cs1Port)
			resp = PutUint32(resp, 0) // cs_ver (unused in fake)
			resp = PutUint32(resp, 0) // labelmask (unused in fake)
		}
		// Include the normal embedded CS (cs2 in dual-CS mode, only CS in normal mode).
		resp = PutUint32(resp, s.csIP)
		resp = PutUint16(resp, s.csPort)
		resp = PutUint32(resp, 0) // cs_ver (unused in fake)
		resp = PutUint32(resp, 0) // labelmask (unused in fake)
	}
	_ = WriteFrame(conn, MatoclFuseWriteChunk, resp)
}

// handleWriteChunkEnd handles CLTOMA_FUSE_WRITE_CHUNK_END (436).
//
// Payload (MooseFS >= 3.0.74):
//
//	[msgid:32][chunkID:64][inode:32][chunkindx:32][length:64][chunkopflags:8] = 29 bytes
//
// Payload (MooseFS >= 4.40.0, extended):
//
//	[msgid:32][chunkID:64][inode:32][chunkindx:32][length:64][chunkopflags:8][offset:32][size:32] = 37 bytes
//
// No version field. No lockid field.
//
// Copies the data written to the fakeCSServer back into node.content and
// sets the node's size to `length` (total bytes written so far).
// Response: [msgid:32][status:8]
func (s *fakeMFSServer) handleWriteChunkEnd(conn net.Conn, payload []byte) {
	if len(payload) < 29 { // minimum without 4.40.0 extension
		return
	}
	msgid, off, _ := ReadUint32(payload, 0)
	chunkID, off, _ := ReadUint64(payload, off)
	_, off, _ = ReadUint32(payload, off)       // inode (skip — we recover nodeID from chunkID)
	_, off, _ = ReadUint32(payload, off)       // chunkindx (skip — unused by fake server)
	length, off, _ := ReadUint64(payload, off) // new total file length
	_ = off                                    // chunkopflags:8 [offset:32 size:32] follow (skip)

	// Track how many WRITE_CHUNK_END calls we receive (used by fallback tests).
	s.writeChunkEndCalls.Add(1)

	// In the extended format (≥37 bytes), detect release calls where size==0.
	// Layout: [msgid:4][chunkID:8][inode:4][chunkindx:4][length:8][flags:1][offset:4][size:4]
	// size field starts at byte 33 (4+8+4+4+8+1+4 = 33).
	if len(payload) >= 37 {
		var size uint32
		size, _, _ = ReadUint32(payload, 33)
		if size == 0 {
			s.writeChunkEndReleaseCalls.Add(1)
			// Fault injection: return StatusERROR on the Nth release if configured.
			if n := s.writeChunkEndReleaseErrOnCall.Load(); n > 0 && s.writeChunkEndReleaseCalls.Load() == n {
				writeStatusReply(conn, MatoclFuseWriteChunkEnd, msgid, StatusERROR)
				return
			}
		}
	}

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

func (s *fakeMFSServer) handleRename(conn net.Conn, payload []byte) {
	// [msgid:32][srcParent:32][srcNameLen:8][srcName][dstParent:32][dstNameLen:8][dstName][uid:32][gcnt:32][gid:32]
	var err error
	var msgid, srcParentID, dstParentID uint32
	var off int

	msgid, off, err = ReadUint32(payload, 0)
	if err != nil {
		writeStatusReply(conn, MatoclFuseRename, 0, StatusERROR)
		return
	}
	srcParentID, off, err = ReadUint32(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseRename, msgid, StatusERROR)
		return
	}
	srcName, off, err := ReadStringU8(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseRename, msgid, StatusERROR)
		return
	}
	dstParentID, off, err = ReadUint32(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseRename, msgid, StatusERROR)
		return
	}
	dstName, _, err := ReadStringU8(payload, off)
	if err != nil {
		writeStatusReply(conn, MatoclFuseRename, msgid, StatusERROR)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, n := range s.nodes {
		if n.parent == srcParentID && n.name == srcName {
			n.parent = dstParentID
			n.name = dstName
			writeStatusReply(conn, MatoclFuseRename, msgid, StatusOK)
			return
		}
	}
	writeStatusReply(conn, MatoclFuseRename, msgid, StatusENOENT)
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

// TestRead_MultiChunk_SecondChunk verifies that Read() correctly crosses the
// 64 MiB MooseFS chunk boundary and returns data from chunk 1.
//
// The test injects a file of ChunkSize+100 bytes directly into the fake
// master (bypassing the upload protocol) and then reads:
//   - 10 bytes from offset 0 (chunk 0)   → byte value 0xAA
//   - 100 bytes from offset ChunkSize (chunk 1) → byte value 0xBB
//   - 10 bytes from offset ChunkSize+100 (past EOF) → nil
//
// This covers the production scenario where a download of a file > 64 MiB
// fails with "no chunk servers available" if handleReadChunk ignores the
// chunk index and always returns the same chunkID.
func TestRead_MultiChunk_SecondChunk(t *testing.T) {
	c, srv := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "bigfile.bin", 0o644)
	require.NoError(t, err)

	// Inject content directly: chunk 0 = 0xAA × ChunkSize, chunk 1 = 0xBB × 100.
	content := make([]byte, int(ChunkSize)+100)
	for i := 0; i < int(ChunkSize); i++ {
		content[i] = 0xAA
	}
	for i := int(ChunkSize); i < len(content); i++ {
		content[i] = 0xBB
	}
	srv.mu.Lock()
	srv.nodes[nodeID].content = content
	srv.mu.Unlock()

	// Read 10 bytes from chunk 0 (offset 0).
	got0, err := c.Read(nodeID, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, bytes.Repeat([]byte{0xAA}, 10), got0, "chunk 0 data mismatch")

	// Read 100 bytes from chunk 1 (offset = ChunkSize).
	got1, err := c.Read(nodeID, ChunkSize, 100)
	require.NoError(t, err)
	require.NotNil(t, got1, "chunk 1 must not be nil (no EOF)")
	assert.Equal(t, bytes.Repeat([]byte{0xBB}, 100), got1, "chunk 1 data mismatch")

	// Read past EOF — must return empty (nil), not error.
	gotEOF, err := c.Read(nodeID, uint64(len(content)), 10)
	require.NoError(t, err)
	assert.Empty(t, gotEOF, "read past EOF must return empty")
}

// TestRead_MultiChunk_NCSZeroAtEOF verifies that Read() treats a master
// proto response with nCS=0 as EOF when offset >= info.Length.
//
// This reproduces the production error:
//
//	mfsclient: Read(N, off=67108864): no chunk servers available
//
// which occurs when a file is exactly a multiple of ChunkSize bytes.  In that
// case, some MooseFS master versions return a proto=2/nCS=0 response for chunk
// index 1 rather than the standard 5-byte StatusOK EOF signal.
//
// Before the fix, Read() returned an error; after the fix it returns nil (EOF).
func TestRead_MultiChunk_NCSZeroAtEOF(t *testing.T) {
	c, srv := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "exact64m.bin", 0o644)
	require.NoError(t, err)

	// Inject content of exactly ChunkSize bytes (fills chunk 0 completely).
	// This causes handleReadChunk to return nCS=0 for chunk index 1 since
	// chunkStart(1) = ChunkSize == fileLen.
	content := bytes.Repeat([]byte{0xCC}, 10) // small sentinel data
	// We need fileLen to simulate ChunkSize so that the offset check triggers.
	// Trick: use a custom content slice whose length is exactly ChunkSize by
	// allocating the real size.
	bigContent := make([]byte, int(ChunkSize))
	copy(bigContent, content)
	srv.mu.Lock()
	srv.nodes[nodeID].content = bigContent
	srv.mu.Unlock()

	// Read from chunk 0 (offset 0) — must succeed.
	got0, err := c.Read(nodeID, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, content, got0, "chunk 0 must return injected data")

	// Read at offset ChunkSize — this is the boundary chunk slot.
	// handleReadChunk returns proto=2/nCS=0 with info.Length=ChunkSize.
	// Fixed Read() must return nil (EOF), NOT "no chunk servers available".
	gotBoundary, err := c.Read(nodeID, ChunkSize, 10)
	require.NoError(t, err, "Read at chunk boundary (nCS=0 with offset==fileLen) must return EOF, not error")
	assert.Empty(t, gotBoundary, "boundary read must return empty (EOF)")
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

// ─── ReadFrame security tests ─────────────────────────────────────────────────

// TestReadFrame_tooLarge verifies that ReadFrame rejects a frame whose payload
// length exceeds maxFramePayload (128 MiB), preventing server-induced OOM.
func TestReadFrame_tooLarge(t *testing.T) {
	// Fake server: sends a single frame with length = maxFramePayload + 1.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Write a frame header: cmd=1, length = 128MiB + 1 (0x08000001).
		hdr := make([]byte, 8)
		binary.BigEndian.PutUint32(hdr[0:4], 1)
		binary.BigEndian.PutUint32(hdr[4:8], maxFramePayload+1)
		_, _ = conn.Write(hdr)
		// Do NOT send the payload — ReadFrame must reject before reading.
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer conn.Close()

	_, _, err = ReadFrame(conn)
	require.Error(t, err, "ReadFrame must reject an oversized payload")
	assert.Contains(t, err.Error(), "payload too large", "error message must mention payload too large")
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

// ─── parseChunkInfo / lockid tests ───────────────────────────────────────────

// TestWrite_lockIDParsing verifies that parseChunkInfo correctly extracts the
// optional lockid:32 token appended after the CS entries in a proto 2 response,
// and that a zero-initialised LockID is returned when the master omits it.
//
// This reproduces the diagnostic path for bug #101 (post-upload stat size=0):
// if the master sends a non-zero lockid and we fail to extract it, WRITE_CHUNK_END
// is sent with lockid=0 which may prevent the master from committing the file length.
func TestWrite_lockIDParsing(t *testing.T) {
	const (
		wantChunkID = uint64(0xCAFEBABEDEAD1234)
		wantVersion = uint32(7)
		wantLockID  = uint32(0xDEADBEEF)
	)

	t.Run("proto2_with_lockid", func(t *testing.T) {
		// Build a proto 2 WRITE_CHUNK response (MATOCL_FUSE_WRITE_CHUNK=435)
		// with 1 CS entry followed by a lockid:32 token.
		//   [msgid:32][protocolid:8=2][length:64][chunkid:64][version:32]
		//   [ip:32][port:16][cs_ver:32][labelmask:32]   (14 bytes, proto 2 entry)
		//   [lockid:32]
		var resp []byte
		resp = PutUint32(resp, 0)           // msgid
		resp = PutUint8(resp, 2)            // protocolid = 2
		resp = PutUint64(resp, 0)           // file length
		resp = PutUint64(resp, wantChunkID) // chunkID
		resp = PutUint32(resp, wantVersion) // version
		resp = PutUint32(resp, 0x7F000001)  // ip = 127.0.0.1
		resp = PutUint16(resp, 9422)        // port
		resp = PutUint32(resp, 0)           // cs_ver
		resp = PutUint32(resp, 0)           // labelmask
		resp = PutUint32(resp, wantLockID)  // lockid — trailing 4 bytes

		info, err := parseChunkInfo(resp)
		require.NoError(t, err)
		assert.Equal(t, wantChunkID, info.ChunkID, "chunkID must be decoded correctly")
		assert.Equal(t, wantVersion, info.Version, "version must be decoded correctly")
		assert.Len(t, info.Servers, 1, "exactly 1 CS entry expected")
		assert.Equal(t, wantLockID, info.LockID,
			"lockid must be extracted from the trailing 4 bytes of a proto 2 response")
	})

	t.Run("proto2_no_lockid", func(t *testing.T) {
		// Same response but without the trailing lockid bytes — master did not send one.
		var resp []byte
		resp = PutUint32(resp, 0)
		resp = PutUint8(resp, 2)
		resp = PutUint64(resp, 0)
		resp = PutUint64(resp, wantChunkID)
		resp = PutUint32(resp, wantVersion)
		resp = PutUint32(resp, 0x7F000001)
		resp = PutUint16(resp, 9422)
		resp = PutUint32(resp, 0)
		resp = PutUint32(resp, 0)
		// No lockid appended.

		info, err := parseChunkInfo(resp)
		require.NoError(t, err)
		assert.Equal(t, uint32(0), info.LockID,
			"LockID must be 0 when master omits the trailing lockid field")
	})

	t.Run("proto2_no_cs_with_lockid", func(t *testing.T) {
		// Edge case: 0 CS entries + lockid (master metadata available, CS unreachable).
		// remaining = 4 bytes → n = 4/14 = 0 → lockid is still extracted.
		var resp []byte
		resp = PutUint32(resp, 0)
		resp = PutUint8(resp, 2)
		resp = PutUint64(resp, 0)
		resp = PutUint64(resp, wantChunkID)
		resp = PutUint32(resp, wantVersion)
		resp = PutUint32(resp, wantLockID) // trailing 4 bytes — interpreted as lockid

		info, err := parseChunkInfo(resp)
		require.NoError(t, err)
		assert.Empty(t, info.Servers, "no CS entries expected")
		assert.Equal(t, wantLockID, info.LockID,
			"lockid must be extracted even when there are 0 CS entries")
	})
}

// TestParseChunkInfo_Proto3 verifies that parseChunkInfo correctly handles
// protocolid=3 responses (MooseFS >= 4.0.0, erasure-coded chunks).
//
// Proto=3 has the same wire format as proto=2 (14 bytes per CS entry), but is
// used for chunks split into 4 or 8 independent EC shards.  Before this fix,
// any proto=3 response triggered "unknown protocolid 3" — issue #114.
//
// Two behaviours are tested:
//
//  1. Truncated raw bytes from the production Windows log (first 32 of a longer
//     response): no error, ChunkInfo fields decoded, 0 CS entries (incomplete
//     payload gives N = 7/14 = 0).
//
//  2. Well-formed 4-entry proto=3 response: parseChunkInfo returns a clear
//     "EC not supported" error instead of trying to serve corrupt data from a
//     single shard.
func TestParseChunkInfo_Proto3(t *testing.T) {
	t.Run("raw_bytes_from_issue_114", func(t *testing.T) {
		// First 32 bytes captured from a Windows production log (issue #114).
		// The parseChunkInfo error logger truncates raw bytes at 32 bytes; the
		// full proto=3 response would be 25 (header) + 4×14 = 81 bytes.
		// With only 32 bytes available: remaining = 7, N = 7/14 = 0 (truncated).
		//
		// Hex: 0000000003000000000002a107000000000000aebb00000001c0a8026424ce00
		var raw []byte
		raw = PutUint32(raw, 0)          // msgid = 0
		raw = PutUint8(raw, 3)           // protocolid = 3
		raw = PutUint64(raw, 172295)     // fileLength = 0x0002a107
		raw = PutUint64(raw, 44731)      // chunkID   = 0x0000aebb
		raw = PutUint32(raw, 1)          // version   = 1
		// Partial first CS entry (7 bytes — response truncated at 32 bytes):
		raw = PutUint32(raw, 0xC0A80264) // ip   = 192.168.2.100
		raw = PutUint16(raw, 0x24CE)     // port = 9422
		raw = PutUint8(raw, 0)           // first byte of cs_ver (truncated)
		// Total: 25 header + 7 partial = 32 bytes.
		require.Len(t, raw, 32, "test vector must be exactly 32 bytes")

		info, err := parseChunkInfo(raw)
		require.NoError(t, err, "proto=3 must not return 'unknown protocolid' error")
		assert.Equal(t, uint64(44731), info.ChunkID, "chunkID decoded")
		assert.Equal(t, uint64(172295), info.Length, "file length decoded")
		assert.Equal(t, uint32(1), info.Version, "version decoded")
		assert.Empty(t, info.Servers,
			"0 CS entries expected: truncated payload (7 bytes) < entrySize (14)")
	})

	t.Run("four_part_ec_chunk_returns_error", func(t *testing.T) {
		// Well-formed proto=3 response with 4 CS entries (minimum EC configuration,
		// per MooseFS source: chunks.c / matoclserv.c).
		// parseChunkInfo must parse the 4 servers and then return a clear error
		// rather than silently serving corrupt data from a single EC shard.
		var resp []byte
		resp = PutUint32(resp, 0)      // msgid
		resp = PutUint8(resp, 3)       // protocolid = 3
		resp = PutUint64(resp, 172295) // fileLength
		resp = PutUint64(resp, 44731)  // chunkID
		resp = PutUint32(resp, 1)      // version
		// 4 CS entries × 14 bytes = 56 bytes.
		for i := uint32(0); i < 4; i++ {
			resp = PutUint32(resp, 0xC0A80264+i) // ip: 192.168.2.100 … 103
			resp = PutUint16(resp, 0x24CE)        // port: 9422
			resp = PutUint32(resp, 0)             // cs_ver
			resp = PutUint32(resp, 0)             // labelmask
		}
		require.Len(t, resp, 81, "test vector must be 25-byte header + 4×14 = 81 bytes")

		_, err := parseChunkInfo(resp)
		require.Error(t, err, "proto=3 EC chunk must return an error")
		assert.Contains(t, err.Error(), "erasure-coded",
			"error must identify the EC nature of the chunk")
		assert.Contains(t, err.Error(), "4",
			"error must report the part count")
	})

	t.Run("eight_part_ec_chunk_returns_error", func(t *testing.T) {
		// Same as four_part but with 8 entries (maximum EC configuration).
		var resp []byte
		resp = PutUint32(resp, 0)
		resp = PutUint8(resp, 3)
		resp = PutUint64(resp, 172295)
		resp = PutUint64(resp, 44731)
		resp = PutUint32(resp, 1)
		for i := uint32(0); i < 8; i++ {
			resp = PutUint32(resp, 0xC0A80264+i)
			resp = PutUint16(resp, 0x24CE)
			resp = PutUint32(resp, 0)
			resp = PutUint32(resp, 0)
		}
		require.Len(t, resp, 137, "test vector must be 25-byte header + 8×14 = 137 bytes")

		_, err := parseChunkInfo(resp)
		require.Error(t, err, "proto=3 EC chunk (8 parts) must return an error")
		assert.Contains(t, err.Error(), "8", "error must report the part count")
	})

	t.Run("proto3_zero_servers_no_error", func(t *testing.T) {
		// Proto=3 with 0 CS entries (e.g., master returns proto=3 but no servers
		// are available). N=0 is a degenerate case handled by the existing nCS=0
		// guard in Read() — parseChunkInfo must not error here.
		var resp []byte
		resp = PutUint32(resp, 0)
		resp = PutUint8(resp, 3)
		resp = PutUint64(resp, 172295)
		resp = PutUint64(resp, 44731)
		resp = PutUint32(resp, 1)
		// No CS entries.

		info, err := parseChunkInfo(resp)
		require.NoError(t, err, "proto=3 with 0 servers must not error")
		assert.Empty(t, info.Servers)
	})
}

// ─── Fallback CS2 tests ───────────────────────────────────────────────────────

// TestWrite_FallbackCS2 verifies that Write() automatically falls back to CS2
// when CS1 is unreachable (closes the connection immediately after TCP accept).
//
// Scenario:
//   - The fake master returns 2 chunk servers: cs1 (bad) first, cs2 (good) second.
//   - cs1 accepts the TCP connection then immediately closes it (EOF on write-init ACK).
//   - cs2 accepts and processes the write normally.
//
// Verifies:
//   - Upload succeeds (no error returned by Write).
//   - The master receives 2× WRITE_CHUNK (one per attempt: cs1 then cs2).
//   - The master receives 2× WRITE_CHUNK_END: 1 release (after cs1 failure) + 1 commit (after cs2 success).
//   - The final file content matches the written data.
func TestWrite_FallbackCS2(t *testing.T) {
	// ── CS1: accepts TCP connections and closes them immediately ──────────────
	// This simulates a chunk server that is reachable at TCP level but rejects
	// the write protocol (e.g., dead process, kernel accepts but app not running).
	// WriteChunk will fail with EOF when reading the mandatory write-init ACK.
	cs1Ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	cs1Done := make(chan struct{})
	go func() {
		defer close(cs1Done)
		for {
			conn, err := cs1Ln.Accept()
			if err != nil {
				return // listener closed
			}
			conn.Close() // close immediately — client gets EOF on write-init ACK
		}
	}()
	t.Cleanup(func() {
		_ = cs1Ln.Close()
		<-cs1Done
	})
	cs1Addr := cs1Ln.Addr().(*net.TCPAddr)
	cs1IPBytes := cs1Addr.IP.To4()
	require.NotNil(t, cs1IPBytes, "cs1 must have IPv4 address")
	cs1IPUint32 := uint32(cs1IPBytes[0])<<24 | uint32(cs1IPBytes[1])<<16 |
		uint32(cs1IPBytes[2])<<8 | uint32(cs1IPBytes[3])
	cs1Port := uint16(cs1Addr.Port)

	// ── Fake master: returns [cs1, cs2] in WRITE_CHUNK response ──────────────
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	// Configure cs1 as the bad first entry (the embedded srv.cs remains cs2).
	srv.cs1IP = cs1IPUint32
	srv.cs1Port = cs1Port

	// ── Connect client ────────────────────────────────────────────────────────
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(portStr, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	// ── Create a file node ────────────────────────────────────────────────────
	nodeID, err := c.Mknod(RootNodeID, "fallback_test.bin", 0o644)
	require.NoError(t, err)

	// ── Write — must succeed via cs2 after cs1 fails ──────────────────────────
	data := []byte("fallback-to-cs2-test-data")
	writeErr := c.Write(nodeID, 0, data)
	require.NoError(t, writeErr, "Write must succeed: cs1 fails, cs2 succeeds")

	// ── Verify call counts on the fake master ─────────────────────────────────
	// Each Write attempt calls WRITE_CHUNK once: 2 attempts → 2 WRITE_CHUNK calls.
	assert.Equal(t, int32(2), srv.writeChunkCalls.Load(),
		"master must receive 2 WRITE_CHUNK calls (one per CS attempt)")

	// writeChunkRelease sends WRITE_CHUNK_END after cs1 failure (release, no size change).
	// writeChunkEnd sends WRITE_CHUNK_END after cs2 success (commit, new length).
	// Total: 2 WRITE_CHUNK_END calls.
	assert.Equal(t, int32(2), srv.writeChunkEndCalls.Load(),
		"master must receive 2 WRITE_CHUNK_END calls (1 release + 1 commit)")

	// ── Verify file content ────────────────────────────────────────────────────
	// Read back and verify data integrity.
	got, err := c.Read(nodeID, 0, uint32(len(data)))
	require.NoError(t, err)
	assert.Equal(t, data, got, "data read back must match what was written via cs2 fallback")

	// Verify size via GetAttr.
	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(data)), attr.Size,
		"file size must reflect data written via cs2 after cs1 fallback")
}

// TestWrite_FallbackCS2_DialRefused verifies that Write() falls back to CS2 when
// CS1 is completely unreachable at the TCP level (connection refused — no listener).
//
// This covers the `dialErr != nil` branch inside the Write() retry loop, which is
// distinct from TestWrite_FallbackCS2 (where CS1 accepts the TCP connection but
// then drops it, triggering a WriteChunk/EOF error instead of a dial error).
//
// Verifies:
//   - Upload succeeds (Write returns nil).
//   - The master receives 2× WRITE_CHUNK (one per CS attempt).
//   - The master receives 2× WRITE_CHUNK_END: 1 release (dial failure) + 1 commit.
//   - The final file content and size are correct.
func TestWrite_FallbackCS2_DialRefused(t *testing.T) {
	// ── CS1: bind a port then close the listener immediately ─────────────────
	// After Close(), any DialCS to this port returns "connection refused".
	cs1Ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	cs1Addr := cs1Ln.Addr().(*net.TCPAddr)
	_ = cs1Ln.Close() // close immediately — no goroutine needed

	cs1IPBytes := cs1Addr.IP.To4()
	require.NotNil(t, cs1IPBytes, "cs1 must have IPv4 address")
	cs1IPUint32 := uint32(cs1IPBytes[0])<<24 | uint32(cs1IPBytes[1])<<16 |
		uint32(cs1IPBytes[2])<<8 | uint32(cs1IPBytes[3])
	cs1Port := uint16(cs1Addr.Port)

	// ── Fake master: returns [cs1 (refused), cs2 (good)] ─────────────────────
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	srv.cs1IP = cs1IPUint32
	srv.cs1Port = cs1Port

	// ── Connect client ────────────────────────────────────────────────────────
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(portStr, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	// ── Create a file node ────────────────────────────────────────────────────
	nodeID, err := c.Mknod(RootNodeID, "dial_refused_fallback.bin", 0o644)
	require.NoError(t, err)

	// ── Write — must succeed via cs2 after cs1 dial is refused ────────────────
	data := []byte("dial-refused-fallback-test-data")
	writeErr := c.Write(nodeID, 0, data)
	require.NoError(t, writeErr, "Write must succeed: cs1 dial refused, cs2 succeeds")

	// ── Verify call counts ────────────────────────────────────────────────────
	// 2 WRITE_CHUNK: one fresh lock per attempt (i=0 → cs1, i=1 → cs2).
	assert.Equal(t, int32(2), srv.writeChunkCalls.Load(),
		"master must receive 2 WRITE_CHUNK calls (one per CS attempt)")

	// 2 WRITE_CHUNK_END: release after cs1 dial failure + commit after cs2 success.
	assert.Equal(t, int32(2), srv.writeChunkEndCalls.Load(),
		"master must receive 2 WRITE_CHUNK_END calls (1 release + 1 commit)")

	// ── Verify data integrity ─────────────────────────────────────────────────
	got, err := c.Read(nodeID, 0, uint32(len(data)))
	require.NoError(t, err)
	assert.Equal(t, data, got, "data read back must match what was written via cs2 after dial refusal")

	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(data)), attr.Size,
		"file size must reflect data committed via cs2")
}

// TestWrite_AllServersFail verifies that Write() returns an error when every
// available chunk server is unreachable across all 4 write strategies.
//
// With 2 chunk servers (cs1 = EOF, cs2 = refused) and 4 cascade strategies:
//   - Strategy 0 (sync CS1, chain=[CS2]): cs1 EOF    → 1 WRITE_CHUNK + 1 release WRITE_CHUNK_END
//   - Strategy 1 (sync CS2, chain=[]):   cs2 refused → 1 WRITE_CHUNK + 1 release WRITE_CHUNK_END
//   - Strategy 2 (async CS1):            cs1 EOF    → 1 WRITE_CHUNK + 1 release WRITE_CHUNK_END
//   - Strategy 3 (async CS2):            cs2 refused → 1 WRITE_CHUNK + 1 release WRITE_CHUNK_END
//
// Verifies:
//   - Write returns a non-nil error containing "all write strategies failed".
//   - The master receives 4× WRITE_CHUNK (one per strategy).
//   - The master receives 4× WRITE_CHUNK_END: all are releases (no commit issued).
func TestWrite_AllServersFail(t *testing.T) {
	// ── CS1: accept TCP connections then close them immediately ───────────────
	cs1Ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	cs1Done := make(chan struct{})
	go func() {
		defer close(cs1Done)
		for {
			conn, err := cs1Ln.Accept()
			if err != nil {
				return
			}
			conn.Close() // drop immediately — WriteChunk gets EOF
		}
	}()
	t.Cleanup(func() {
		_ = cs1Ln.Close()
		<-cs1Done
	})
	cs1Addr := cs1Ln.Addr().(*net.TCPAddr)
	cs1IPBytes := cs1Addr.IP.To4()
	require.NotNil(t, cs1IPBytes, "cs1 must have IPv4 address")
	cs1IPUint32 := uint32(cs1IPBytes[0])<<24 | uint32(cs1IPBytes[1])<<16 |
		uint32(cs1IPBytes[2])<<8 | uint32(cs1IPBytes[3])
	cs1Port := uint16(cs1Addr.Port)

	// ── Fake master: dual-CS mode — cs1 (bad) first, embedded cs2 second ─────
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	srv.cs1IP = cs1IPUint32
	srv.cs1Port = cs1Port

	// ── Connect client ────────────────────────────────────────────────────────
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(portStr, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	// ── Create a file node ────────────────────────────────────────────────────
	nodeID, err := c.Mknod(RootNodeID, "all_fail_test.bin", 0o644)
	require.NoError(t, err)

	// ── Stop the embedded CS so cs2 is also unreachable ───────────────────────
	// srv.Stop() (via t.Cleanup) will call srv.cs.Stop() again; that is safe:
	// listener.Close() is idempotent and <-done on a closed channel returns immediately.
	srv.cs.Stop()

	// ── Write — must fail: all chunk servers are unreachable ─────────────────
	data := []byte("this-write-should-fail-on-all-cs")
	writeErr := c.Write(nodeID, 0, data)
	require.Error(t, writeErr, "Write must return an error when all CSes are unreachable")
	assert.Contains(t, writeErr.Error(), "all write strategies failed",
		"error must indicate that all write strategies were exhausted")

	// ── Verify call counts ────────────────────────────────────────────────────
	// 4 WRITE_CHUNK: one lock acquisition per strategy (4 strategies × 2 servers = 4 tries).
	assert.Equal(t, int32(4), srv.writeChunkCalls.Load(),
		"master must receive 4 WRITE_CHUNK calls (one per strategy)")

	// 4 WRITE_CHUNK_END: all are releases (size==0 — no commit issued for any strategy).
	assert.Equal(t, int32(4), srv.writeChunkEndCalls.Load(),
		"master must receive 4 WRITE_CHUNK_END releases — no commit when all strategies fail")
	assert.Equal(t, int32(4), srv.writeChunkEndReleaseCalls.Load(),
		"all 4 WRITE_CHUNK_END calls must be releases (size==0)")

	// ── Verify the file was NOT modified ─────────────────────────────────────
	// After a failed Write, the file size must remain 0 (no partial commit).
	// Note: GetAttr after cs.Stop() still works because it is a master operation.
	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), attr.Size,
		"file size must remain 0 after a completely failed Write (no partial commit)")
}

// ─── Cascade 4-strategy fallback tests ───────────────────────────────────────

// TestWrite_FallbackCascade verifies the 4-strategy write cascade defined in
// defaultWriteStrategies.  Three scenarios are covered:
//
//   A. CS1 returns CANTCONNECT on sync writes (chain present) but CS2 accepts
//      the sync write with an empty chain → strategy 1 (sync CS2) succeeds.
//
//   B. CS1 returns CANTCONNECT on sync writes, CS2 is unreachable (DialCS fails)
//      → strategies 0 and 1 fail → strategy 2 (async CS1) succeeds.
//
//   C. CS1 closes every connection immediately (EOF), CS2 is unreachable →
//      all 4 strategies fail → Write() returns "all write strategies failed".
func TestWrite_FallbackCascade(t *testing.T) {
	// ── Scenario A: sync chain CS1 CANTCONNECT → sync CS2 succeeds ───────────

	t.Run("ScenarioA_SyncChainCS1_CANTCONNECT_SyncCS2_Succeeds", func(t *testing.T) {
		// CS1 = chainOnlyCANTCONNECTCS: CANTCONNECT when chain present, normal async write.
		// CS2 = normal embedded fakeCSServer.
		// Strategy 0 ({csIdx:0, sync}, chain=[CS2]): CS1 CANTCONNECT → fail, release.
		// Strategy 1 ({csIdx:1, sync}, chain=[]):   CS2 with empty chain → success, commit.
		cs1 := newChainOnlyCANTCONNECTCS()
		cs1IP, cs1Port := cs1.Start()
		t.Cleanup(cs1.Stop)

		srv := newFakeMFSServer()
		addr := srv.Start()
		t.Cleanup(srv.Stop)

		// cs1 listed first in WRITE_CHUNK response; embedded srv.cs is CS2.
		srv.cs1IP = cs1IP
		srv.cs1Port = cs1Port

		host, portStr, err := net.SplitHostPort(addr)
		require.NoError(t, err)
		var portNum int
		_, err = fmt.Sscanf(portStr, "%d", &portNum)
		require.NoError(t, err)

		c, err := Dial(host, portNum)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		require.NoError(t, c.Register())

		nodeID, err := c.Mknod(RootNodeID, "cascade_a.bin", 0o644)
		require.NoError(t, err)

		data := []byte("cascade-scenario-a-test-data")
		writeErr := c.Write(nodeID, 0, data)
		require.NoError(t, writeErr,
			"Write must succeed: strategy 0 CANTCONNECT, strategy 1 (sync CS2) succeeds")

		// Strategies 0 and 1 both try a fresh lock → 2 WRITE_CHUNK.
		assert.Equal(t, int32(2), srv.writeChunkCalls.Load(),
			"2 WRITE_CHUNK calls expected (strategy 0 + strategy 1)")
		// Strategy 0 releases, strategy 1 commits → 2 WRITE_CHUNK_END (1 release + 1 commit).
		assert.Equal(t, int32(2), srv.writeChunkEndCalls.Load(),
			"2 WRITE_CHUNK_END calls expected (1 release + 1 commit)")
		assert.Equal(t, int32(1), srv.writeChunkEndReleaseCalls.Load(),
			"1 release WRITE_CHUNK_END expected (strategy 0 CANTCONNECT)")

		// CS2 (embedded srv.cs) stored the data → full data integrity check.
		got, err := c.Read(nodeID, 0, uint32(len(data)))
		require.NoError(t, err)
		assert.Equal(t, data, got, "data read back must match what was written via CS2 (strategy 1)")

		attr, err := c.GetAttr(nodeID)
		require.NoError(t, err)
		assert.Equal(t, uint64(len(data)), attr.Size,
			"file size must reflect data committed via strategy 1 (sync CS2)")
	})

	// ── Scenario B: both sync strategies fail → async CS1 succeeds ───────────

	t.Run("ScenarioB_BothSyncFail_AsyncCS1_Succeeds", func(t *testing.T) {
		// CS1 = chainOnlyCANTCONNECTCS: CANTCONNECT when chain present, normal async.
		// CS2 = embedded srv.cs, stopped before Write() → DialCS fails.
		// Strategy 0 ({csIdx:0, sync}, chain=[CS2]): CS1 CANTCONNECT → fail, release.
		// Strategy 1 ({csIdx:1, sync}, chain=[]):   CS2 DialCS refused → fail, release.
		// Strategy 2 ({csIdx:0, async}, chain=nil): CS1 async → success, commit.
		cs1 := newChainOnlyCANTCONNECTCS()
		cs1IP, cs1Port := cs1.Start()
		t.Cleanup(cs1.Stop)

		srv := newFakeMFSServer()
		addr := srv.Start()
		t.Cleanup(srv.Stop)

		srv.cs1IP = cs1IP
		srv.cs1Port = cs1Port

		host, portStr, err := net.SplitHostPort(addr)
		require.NoError(t, err)
		var portNum int
		_, err = fmt.Sscanf(portStr, "%d", &portNum)
		require.NoError(t, err)

		c, err := Dial(host, portNum)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		require.NoError(t, c.Register())

		nodeID, err := c.Mknod(RootNodeID, "cascade_b.bin", 0o644)
		require.NoError(t, err)

		// Stop CS2 (embedded) so DialCS to it fails.
		srv.cs.Stop()

		data := []byte("cascade-scenario-b-test-data")
		writeErr := c.Write(nodeID, 0, data)
		require.NoError(t, writeErr,
			"Write must succeed: strategies 0+1 fail, strategy 2 (async CS1) succeeds")

		// 3 strategies tried → 3 WRITE_CHUNK.
		assert.Equal(t, int32(3), srv.writeChunkCalls.Load(),
			"3 WRITE_CHUNK calls expected (strategies 0+1+2)")
		// Strategies 0+1 release, strategy 2 commits → 3 WRITE_CHUNK_END (2 releases + 1 commit).
		assert.Equal(t, int32(3), srv.writeChunkEndCalls.Load(),
			"3 WRITE_CHUNK_END calls expected (2 releases + 1 commit)")
		assert.Equal(t, int32(2), srv.writeChunkEndReleaseCalls.Load(),
			"2 release WRITE_CHUNK_END expected (strategies 0 and 1 failed)")

		// Data was written to cs1.inner (chainOnlyCANTCONNECTCS).
		// The fake master pulls from srv.cs (stopped) on WRITE_CHUNK_END, so
		// node.content is zeroed — but the data IS present in cs1.inner.chunks.
		// Verify both the committed size (master view) and the raw bytes (CS view).
		chunkData := cs1.GetChunkData(uint64(nodeID))
		assert.Equal(t, data, chunkData,
			"data must be physically stored in cs1.inner after async CS1 write (strategy 2)")

		attr, err := c.GetAttr(nodeID)
		require.NoError(t, err)
		assert.Equal(t, uint64(len(data)), attr.Size,
			"file size must reflect data committed via async CS1 (strategy 2)")
	})

	// ── Scenario C: all strategies fail → error ───────────────────────────────

	t.Run("ScenarioC_AllStrategiesFail", func(t *testing.T) {
		// CS1 = accepts TCP then closes immediately (EOF on write-init ACK).
		// CS2 = embedded srv.cs, stopped before Write() → DialCS fails.
		// All 4 strategies fail.
		cs1Ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		cs1Done := make(chan struct{})
		go func() {
			defer close(cs1Done)
			for {
				conn, err := cs1Ln.Accept()
				if err != nil {
					return
				}
				conn.Close() // drop immediately — WriteChunk gets EOF on write-init ACK
			}
		}()
		t.Cleanup(func() {
			_ = cs1Ln.Close()
			<-cs1Done
		})
		cs1Addr := cs1Ln.Addr().(*net.TCPAddr)
		cs1IPBytes := cs1Addr.IP.To4()
		require.NotNil(t, cs1IPBytes, "cs1 must have IPv4 address")
		cs1IPUint32 := uint32(cs1IPBytes[0])<<24 | uint32(cs1IPBytes[1])<<16 |
			uint32(cs1IPBytes[2])<<8 | uint32(cs1IPBytes[3])

		srv := newFakeMFSServer()
		addr := srv.Start()
		t.Cleanup(srv.Stop)

		srv.cs1IP = cs1IPUint32
		srv.cs1Port = uint16(cs1Addr.Port)

		host, portStr, err := net.SplitHostPort(addr)
		require.NoError(t, err)
		var portNum int
		_, err = fmt.Sscanf(portStr, "%d", &portNum)
		require.NoError(t, err)

		c, err := Dial(host, portNum)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		require.NoError(t, c.Register())

		nodeID, err := c.Mknod(RootNodeID, "cascade_c.bin", 0o644)
		require.NoError(t, err)

		// Stop CS2 so all 4 strategies exhaust.
		srv.cs.Stop()

		data := []byte("cascade-scenario-c-test-data")
		writeErr := c.Write(nodeID, 0, data)
		require.Error(t, writeErr, "Write must fail when all 4 strategies are exhausted")
		assert.Contains(t, writeErr.Error(), "all write strategies failed",
			"error must indicate that all strategies were exhausted")

		// All 4 strategies tried → 4 WRITE_CHUNK + 4 WRITE_CHUNK_END (all releases).
		assert.Equal(t, int32(4), srv.writeChunkCalls.Load(),
			"4 WRITE_CHUNK calls expected (all strategies tried)")
		assert.Equal(t, int32(4), srv.writeChunkEndCalls.Load(),
			"4 WRITE_CHUNK_END calls expected (all releases, no commit)")
		assert.Equal(t, int32(4), srv.writeChunkEndReleaseCalls.Load(),
			"all 4 WRITE_CHUNK_END calls must be releases (size==0)")

		// File must not have been modified.
		attr, err := c.GetAttr(nodeID)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), attr.Size,
			"file size must remain 0 when all strategies fail (no partial commit)")
	})
}

// TestWrite_ShrinkingServerList verifies that Write() correctly releases the
// master lock when a re-lock returns a shorter server list that makes the
// current strategy's csIdx out of range (the "shrink guard" path in Write()).
//
// Scenario (2 servers initially, shrinks to 1 after call 1):
//   - CS1 = chainOnlyCANTCONNECTCS: CANTCONNECT on sync writes, normal async writes.
//   - CS2 = embedded srv.cs (always reachable, but excluded from WRITE_CHUNK responses
//     starting from the 2nd call via singleCSFrom=2).
//
// Strategy trace:
//   - Strategy 0 ({csIdx:0, sync}, call 1): [CS1, CS2] → CS1 CANTCONNECT → release. 1 WRITE_CHUNK.
//   - Strategy 1 ({csIdx:1, sync}, call 2): [CS1] only → csIdx=1 >= 1 → shrink guard → release. 1 WRITE_CHUNK.
//   - Strategy 2 ({csIdx:0, async}, call 3): [CS1] only → csIdx=0 < 1 → CS1 async → SUCCESS. 1 WRITE_CHUNK.
//
// Verifies:
//   - Write() returns nil (upload succeeds via strategy 2).
//   - Master receives 3× WRITE_CHUNK.
//   - Master receives 3× WRITE_CHUNK_END: 2 releases (strategies 0 + 1) + 1 commit.
//   - writeChunkEndReleaseCalls == 2 (both the CANTCONNECT release AND the shrink-guard release).
func TestWrite_ShrinkingServerList(t *testing.T) {
	// ── CS1: chainOnlyCANTCONNECTCS ──────────────────────────────────────────
	cs1 := newChainOnlyCANTCONNECTCS()
	cs1IP, cs1Port := cs1.Start()
	t.Cleanup(cs1.Stop)

	// ── Fake master ───────────────────────────────────────────────────────────
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	srv.cs1IP = cs1IP
	srv.cs1Port = cs1Port
	// From the 2nd WRITE_CHUNK call onward, return only cs1 (cs2 "leaves the cluster").
	srv.singleCSFrom = 2

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(portStr, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	// ── Create a file node ────────────────────────────────────────────────────
	nodeID, err := c.Mknod(RootNodeID, "shrink_test.bin", 0o644)
	require.NoError(t, err)

	// ── Write — must succeed via strategy 2 (async CS1) ──────────────────────
	data := []byte("shrinking-server-list-test-data")
	writeErr := c.Write(nodeID, 0, data)
	require.NoError(t, writeErr,
		"Write must succeed: strategy 0 CANTCONNECT, strategy 1 skipped (shrink guard), strategy 2 async CS1")

	// ── Verify call counts ────────────────────────────────────────────────────
	// 3 strategies reached a lock call → 3 WRITE_CHUNK.
	assert.Equal(t, int32(3), srv.writeChunkCalls.Load(),
		"3 WRITE_CHUNK calls expected (strategy 0, re-lock strategy 1, re-lock strategy 2)")

	// Strategy 0 releases (CANTCONNECT), strategy 1 releases (shrink guard), strategy 2 commits.
	assert.Equal(t, int32(3), srv.writeChunkEndCalls.Load(),
		"3 WRITE_CHUNK_END calls expected (2 releases + 1 commit)")
	assert.Equal(t, int32(2), srv.writeChunkEndReleaseCalls.Load(),
		"2 release WRITE_CHUNK_END expected: CANTCONNECT release + shrink-guard release")

	// ── Verify committed size ─────────────────────────────────────────────────
	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(data)), attr.Size,
		"file size must reflect data committed via async CS1 (strategy 2)")
}

// TestWriteChunkData_MultiBlock verifies that WriteChunkData correctly writes
// data larger than a single 65 536-byte block (multiple WRITE_DATA frames) using
// exactly one WRITE_CHUNK master lock and one WRITE_CHUNK_END commit.
//
// Payload: 5 × 65 536 bytes = 327 680 bytes.
//
// Expected behaviour:
//   - 1 WRITE_CHUNK  (writeChunkCalls == 1)
//   - 1 WRITE_CHUNK_END commit (writeChunkEndCalls == 1, releases == 0)
//   - All 327 680 bytes stored correctly in the fake CS.
func TestWriteChunkData_MultiBlock(t *testing.T) {
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(portStr, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	nodeID, err := c.Mknod(RootNodeID, "multiblock.bin", 0o644)
	require.NoError(t, err)

	// Build 5 × 65 536-byte payload with distinctive fill.
	const blocks = 5
	payload := make([]byte, blocks*65536)
	for i := range payload {
		payload[i] = byte((i / 65536) + 1) // block 0→1, block 1→2, …
	}

	err = c.WriteChunkData(nodeID, 0, payload)
	require.NoError(t, err, "WriteChunkData must succeed for multi-block write")

	// ── Verify master call counts ─────────────────────────────────────────────
	assert.Equal(t, int32(1), srv.writeChunkCalls.Load(),
		"WriteChunkData must issue exactly 1 WRITE_CHUNK master call")
	assert.Equal(t, int32(1), srv.writeChunkEndCalls.Load(),
		"WriteChunkData must issue exactly 1 WRITE_CHUNK_END master call (commit)")
	assert.Equal(t, int32(0), srv.writeChunkEndReleaseCalls.Load(),
		"no release WRITE_CHUNK_END expected on success")

	// ── Verify data stored in CS ──────────────────────────────────────────────
	stored := srv.cs.GetChunkData(uint64(nodeID))
	assert.Equal(t, payload, stored,
		"all %d bytes must be stored correctly in the CS", len(payload))

	// ── Verify committed size (master view) ───────────────────────────────────
	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(payload)), attr.Size,
		"GetAttr must reflect the committed file size")
}

// TestWriteChunkData_BoundaryCheck verifies that WriteChunkData returns an
// error (without performing any I/O) when the offset+len(data) combination
// would span more than one MooseFS chunk boundary.
func TestWriteChunkData_BoundaryCheck(t *testing.T) {
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(portStr, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	nodeID, err := c.Mknod(RootNodeID, "boundary.bin", 0o644)
	require.NoError(t, err)

	// Write that would cross the first 64 MiB chunk boundary.
	// offset = ChunkSize - 1 byte before end; len(data) = 2 → spans boundary.
	offset := ChunkSize - 1
	data := make([]byte, 2)
	err = c.WriteChunkData(nodeID, offset, data)
	require.Error(t, err, "WriteChunkData must reject writes that cross a chunk boundary")
	assert.Contains(t, err.Error(), "crosses chunk boundary",
		"error must mention chunk boundary")

	// No master calls should have been made (error is pre-flight).
	assert.Equal(t, int32(0), srv.writeChunkCalls.Load(),
		"no WRITE_CHUNK must be issued when boundary check fails")
}

// TestWriteChunkData_EmptyData verifies that WriteChunkData(data=nil/empty) is a
// no-op: it returns nil and does NOT issue any master WRITE_CHUNK request.
func TestWriteChunkData_EmptyData(t *testing.T) {
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(portStr, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	nodeID, err := c.Mknod(RootNodeID, "empty.bin", 0o644)
	require.NoError(t, err)

	// nil slice
	require.NoError(t, c.WriteChunkData(nodeID, 0, nil),
		"WriteChunkData(nil) must return nil")
	// zero-length slice
	require.NoError(t, c.WriteChunkData(nodeID, 1024, []byte{}),
		"WriteChunkData([]byte{}) must return nil")

	// No WRITE_CHUNK must have been issued to the master.
	assert.Equal(t, int32(0), srv.writeChunkCalls.Load(),
		"WriteChunkData with empty data must not issue any WRITE_CHUNK")
}

// ─── Re-lock error tests ──────────────────────────────────────────────────────

// TestWrite_ChunkLockErrorOnRetry verifies that Write() returns an error
// containing "re-lock strategy" when the master rejects the second WRITE_CHUNK
// lock request (the re-lock issued before strategy 2, after strategies 0 and 1
// have been processed).
//
// Setup (single-CS mode — embedded srv.cs stopped before Write()):
//   - WRITE_CHUNK #1 (initial lock)    : OK — returns 1 server entry.
//   - Strategy 0 ({csIdx:0, syncChain:true}) : firstActive=true — no re-lock.
//     DialCS fails (CS stopped) → writeChunkRelease → WRITE_CHUNK_END #1 (release).
//   - Strategy 1 ({csIdx:1, syncChain:true}) : csIdx=1 ≥ 1 server → skip (no re-lock).
//   - Strategy 2 ({csIdx:0, syncChain:false}): !firstActive → re-lock.
//     WRITE_CHUNK #2 → StatusERROR → Write() returns "re-lock strategy 3: …".
//
// Verifies:
//   - Write() returns a non-nil error containing "re-lock strategy".
//   - Master receives 2× WRITE_CHUNK (initial + 1 re-lock).
//   - Master receives 1× WRITE_CHUNK_END (the release after strategy 0 dial failure).
//   - File size remains 0 (no commit).
func TestWrite_ChunkLockErrorOnRetry(t *testing.T) {
	// ── Fake master (single-CS mode) ──────────────────────────────────────────
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	// Inject StatusERROR on the 2nd WRITE_CHUNK (the re-lock for strategy 2).
	srv.writeChunkErrOnCall.Store(2)

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(portStr, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	nodeID, err := c.Mknod(RootNodeID, "relock_err_test.bin", 0o644)
	require.NoError(t, err)

	// Stop the embedded CS so that strategy 0's DialCS fails (forcing a release
	// and clearing firstActive), which allows strategy 2 to issue the re-lock.
	srv.cs.Stop()

	// ── Write — must fail with "re-lock strategy" error ───────────────────────
	data := []byte("this-write-must-fail-on-relock")
	writeErr := c.Write(nodeID, 0, data)
	require.Error(t, writeErr, "Write must return an error when re-lock fails")
	assert.Contains(t, writeErr.Error(), "re-lock strategy",
		"error must identify the re-lock strategy that failed")

	// ── Verify call counts ────────────────────────────────────────────────────
	// 2 WRITE_CHUNK: initial lock (call 1 OK) + re-lock for strategy 2 (call 2 ERROR).
	assert.Equal(t, int32(2), srv.writeChunkCalls.Load(),
		"master must receive 2 WRITE_CHUNK calls (initial + 1 re-lock)")

	// 1 WRITE_CHUNK_END: the release sent after strategy 0's DialCS failure.
	assert.Equal(t, int32(1), srv.writeChunkEndCalls.Load(),
		"master must receive 1 WRITE_CHUNK_END call (release after strategy 0 dial failure)")

	// The single WRITE_CHUNK_END is a release (size==0).
	assert.Equal(t, int32(1), srv.writeChunkEndReleaseCalls.Load(),
		"the WRITE_CHUNK_END must be a release (size==0)")

	// ── Verify the file was NOT modified ─────────────────────────────────────
	// GetAttr still works because the error was at master level, not CS level.
	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), attr.Size,
		"file size must remain 0 after a failed re-lock (no commit issued)")
}

// TestWrite_ShrinkGuard_ReleaseFails verifies that Write() continues gracefully
// when the writeChunkRelease call inside the shrink guard returns an error.
//
// The shrink guard is triggered when a re-lock returns fewer servers than the
// current strategy's csIdx requires.  The release error must be logged as a
// warning (non-fatal) and the cascade must continue to the next strategy.
//
// Setup (dual-CS mode, singleCSFrom=2):
//   - WRITE_CHUNK #1 (call 1): returns [cs1, cs2].
//     Strategy 0 ({csIdx:0, sync}): CS1 CANTCONNECT → release #1 → OK. firstActive=false.
//   - WRITE_CHUNK #2 (re-lock, call 2): returns [cs1] only (singleCSFrom triggered).
//     Strategy 1 ({csIdx:1, sync}): csIdx=1 ≥ 1 → shrink guard → release #2 → ERROR
//     (writeChunkEndReleaseErrOnCall=2) → logged as Warn, non-fatal → continue.
//   - WRITE_CHUNK #3 (re-lock, call 3): returns [cs1].
//     Strategy 2 ({csIdx:0, async}): CS1 async write → SUCCESS → commit.
//
// Verifies:
//   - Write() returns nil (upload succeeds despite release #2 error).
//   - Master receives 3× WRITE_CHUNK.
//   - Master receives 3× WRITE_CHUNK_END (2 releases + 1 commit).
//   - writeChunkEndReleaseCalls == 2.
//   - Data is physically stored in CS1 (byte-level integrity).
//   - attr.Size matches len(data).
func TestWrite_ShrinkGuard_ReleaseFails(t *testing.T) {
	// ── CS1: chainOnlyCANTCONNECTCS ──────────────────────────────────────────
	// CANTCONNECT on sync (chain present), accepts async writes normally.
	cs1 := newChainOnlyCANTCONNECTCS()
	cs1IP, cs1Port := cs1.Start()
	t.Cleanup(cs1.Stop)

	// ── Fake master (dual-CS mode) ────────────────────────────────────────────
	srv := newFakeMFSServer()
	addr := srv.Start()
	t.Cleanup(srv.Stop)

	srv.cs1IP = cs1IP
	srv.cs1Port = cs1Port
	// Shrink to [cs1] only starting from call 2.
	srv.singleCSFrom = 2
	// Inject StatusERROR on the 2nd release WRITE_CHUNK_END (the shrink-guard release).
	srv.writeChunkEndReleaseErrOnCall.Store(2)

	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var portNum int
	_, err = fmt.Sscanf(portStr, "%d", &portNum)
	require.NoError(t, err)

	c, err := Dial(host, portNum)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	nodeID, err := c.Mknod(RootNodeID, "shrink_release_fail.bin", 0o644)
	require.NoError(t, err)

	// ── Write — must succeed via strategy 2 (async CS1) ──────────────────────
	data := []byte("shrink-guard-release-fails-but-write-succeeds")
	writeErr := c.Write(nodeID, 0, data)
	require.NoError(t, writeErr,
		"Write must succeed: shrink-guard release error is non-fatal, strategy 2 (async CS1) succeeds")

	// ── Verify call counts ────────────────────────────────────────────────────
	// 3 WRITE_CHUNK: strategy 0 (call 1) + re-lock strategy 1 (call 2) + re-lock strategy 2 (call 3).
	assert.Equal(t, int32(3), srv.writeChunkCalls.Load(),
		"master must receive 3 WRITE_CHUNK calls (strategies 0, 1, 2)")

	// 3 WRITE_CHUNK_END: release #1 (CANTCONNECT) + release #2 (shrink guard, error) + commit (strategy 2).
	assert.Equal(t, int32(3), srv.writeChunkEndCalls.Load(),
		"master must receive 3 WRITE_CHUNK_END calls (2 releases + 1 commit)")

	// Both releases increment the counter before the error check fires, so the
	// count is 2 even though release #2 returns StatusERROR.
	assert.Equal(t, int32(2), srv.writeChunkEndReleaseCalls.Load(),
		"both releases increment the counter (error injection fires after Add, not before)")

	// ── Verify data integrity ─────────────────────────────────────────────────
	// Strategy 2 writes async to CS1; srv.cs (cs2) is not involved.
	chunkData := cs1.GetChunkData(uint64(nodeID))
	assert.Equal(t, data, chunkData,
		"data must be physically stored in cs1 after async write (strategy 2)")

	attr, err := c.GetAttr(nodeID)
	require.NoError(t, err)
	assert.Equal(t, uint64(len(data)), attr.Size,
		"file size must reflect data committed via strategy 2 (async CS1)")
}
