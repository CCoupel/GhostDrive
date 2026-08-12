// Package mfsclient — tests for the chunk server client (csclient.go).
//
// fakeCSServer implements an in-memory MooseFS chunk server that speaks the
// real CS protocol (MooseFS 4.x opcodes 200-212 + WRITE_STATUS=211).
// It is also used by client_test.go to back the fakeMFSServer when testing
// the high-level Read/Write methods.
package mfsclient

import (
	"encoding/binary"
	"hash/crc32"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── fakeCSServer ─────────────────────────────────────────────────────────────

// fakeCSServer is an in-memory MooseFS chunk server for testing.
// It stores chunk data in memory and speaks the real CS protocol (MooseFS 4.x).
// WRITE_STATUS uses opcode 211 (CstoclFuseWriteStatus) per MooseFS 4.x.
//
// Each accepted TCP connection is served in a loop (multiple READ/WRITE ops per
// connection) to support connection-pool reuse tests.  connCount tracks how many
// distinct TCP connections have been accepted.
type fakeCSServer struct {
	listener  net.Listener
	mu        sync.Mutex
	chunks    map[uint64][]byte // chunkID → raw chunk data
	done      chan struct{}
	connCount atomic.Int64 // number of accepted TCP connections
}

// newFakeCSServer creates an idle chunk server.  Call Start() to bind and listen.
func newFakeCSServer() *fakeCSServer {
	return &fakeCSServer{
		chunks: make(map[uint64][]byte),
		done:   make(chan struct{}),
	}
}

// Start binds to a random localhost port and begins accepting connections.
// Returns the server's IP (uint32 big-endian) and port for use in ChunkInfo
// responses.
func (s *fakeCSServer) Start() (ip uint32, port uint16) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("fakeCSServer: listen: " + err.Error())
	}
	s.listener = ln
	go s.acceptLoop()

	addr := ln.Addr().(*net.TCPAddr)
	ipBytes := addr.IP.To4()
	return binary.BigEndian.Uint32(ipBytes), uint16(addr.Port)
}

// Stop closes the listener and waits for the accept goroutine to exit.
func (s *fakeCSServer) Stop() {
	_ = s.listener.Close()
	<-s.done
}

// SetChunkData directly sets the byte slice for chunkID (for pre-seeding reads).
func (s *fakeCSServer) SetChunkData(chunkID uint64, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := make([]byte, len(data))
	copy(buf, data)
	s.chunks[chunkID] = buf
}

// GetChunkData returns a copy of the stored bytes for chunkID.
func (s *fakeCSServer) GetChunkData(chunkID uint64) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := s.chunks[chunkID]
	if src == nil {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// storeBlock writes block at dataOffset within chunkID, extending the buffer as
// needed.
func (s *fakeCSServer) storeBlock(chunkID uint64, dataOffset uint32, block []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	end := dataOffset + uint32(len(block))
	if uint32(len(s.chunks[chunkID])) < end {
		newBuf := make([]byte, end)
		copy(newBuf, s.chunks[chunkID])
		s.chunks[chunkID] = newBuf
	}
	copy(s.chunks[chunkID][dataOffset:], block)
}

// readRange returns a copy of bytes [offset, offset+size) from chunkID.
// Returns nil (not an error) when offset is past the end of the stored data.
func (s *fakeCSServer) readRange(chunkID uint64, offset, size uint32) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := s.chunks[chunkID]
	if offset >= uint32(len(data)) {
		return nil
	}
	end := offset + size
	if end > uint32(len(data)) {
		end = uint32(len(data))
	}
	out := make([]byte, end-offset)
	copy(out, data[offset:end])
	return out
}

// ─── Accept / dispatch ────────────────────────────────────────────────────────

func (s *fakeCSServer) acceptLoop() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		s.connCount.Add(1)
		go s.handleConn(conn)
	}
}

// handleConn serves multiple sequential READ/WRITE operations on a single
// TCP connection.  This matches real MooseFS CS behaviour and allows the
// csPool to reuse connections across calls — needed for pool tests.
func (s *fakeCSServer) handleConn(conn net.Conn) {
	defer conn.Close()
	for {
		cmd, payload, err := ReadFrame(conn)
		if err != nil {
			return // EOF or connection closed by client
		}
		switch cmd {
		case CltocsFuseRead:
			s.serveRead(conn, payload)
		case CltocsFuseWrite:
			s.serveWrite(conn, payload)
		default:
			return // unexpected command — close connection
		}
	}
}

// ─── Read handler ─────────────────────────────────────────────────────────────

// serveRead processes CLTOCS_READ: [chunkId:64][version:32][offset:32][size:32]
// and replies with zero or more CSTOCL_READ_DATA frames followed by
// CSTOCL_READ_STATUS.
func (s *fakeCSServer) serveRead(conn net.Conn, payload []byte) {
	if len(payload) < 20 {
		return
	}
	chunkID, off, _ := ReadUint64(payload, 0)
	_, off, _ = ReadUint32(payload, off) // version (not validated in fake)
	offset, off, _ := ReadUint32(payload, off)
	size, _, _ := ReadUint32(payload, off)

	chunk := s.readRange(chunkID, offset, size)

	if len(chunk) > 0 {
		// Send CSTOCL_READ_DATA: [chunkId:64][blocknum:16][blockOffset:16][size:32][crc:32][data]
		blockNum := uint16(offset / 65536)
		blockOff := uint16(offset % 65536)
		checksum := crc32.ChecksumIEEE(chunk)

		var resp []byte
		resp = PutUint64(resp, chunkID)
		resp = PutUint16(resp, blockNum)
		resp = PutUint16(resp, blockOff)
		resp = PutUint32(resp, uint32(len(chunk)))
		resp = PutUint32(resp, checksum)
		resp = append(resp, chunk...)
		_ = WriteFrame(conn, CstoclFuseReadData, resp)
	}

	// Send CSTOCL_READ_STATUS: [chunkId:64][status:8]
	var status []byte
	status = PutUint64(status, chunkID)
	status = PutUint8(status, StatusOK)
	_ = WriteFrame(conn, CstoclFuseReadStatus, status)
}

// ─── Write handler ────────────────────────────────────────────────────────────

// serveWrite processes the write handshake initiated by CLTOCS_WRITE
// (already received in payload).
//
// Protocol flow (MooseFS writedata.c — confirmed):
//  1. Receive CLTOCS_WRITE.
//  2. Send CSTOCL_WRITE_STATUS(writeid=0, OK) — mandatory write-init ACK.
//  3. Read CLTOCS_WRITE_DATA frames, accumulating data.
//  4. On CLTOCS_WRITE_END, send final CSTOCL_WRITE_STATUS(last_writeid, OK).
func (s *fakeCSServer) serveWrite(conn net.Conn, payload []byte) {
	// CLTOCS_WRITE payload (MooseFS >= 1.7.32):
	// [protocolid:8=1][chunkId:64][version:32][N*(ip:32+port:16)]
	// Minimum 13 bytes (protocolid + chunkId + version, N=0).
	if len(payload) < 13 {
		return
	}
	// payload[0] = protocolid (must be 1); chunkId starts at offset 1.
	chunkID, _, _ := ReadUint64(payload, 1)
	// version and chain entries are parsed but not used in the fake

	// Send mandatory write-init ACK: CSTOCL_WRITE_STATUS(writeid=0, OK).
	// The client must receive this before sending any WRITE_DATA frames.
	var ack []byte
	ack = PutUint64(ack, chunkID)
	ack = PutUint32(ack, 0) // writeId = 0 (no data written yet)
	ack = PutUint8(ack, StatusOK)
	if err := WriteFrame(conn, CstoclFuseWriteStatus, ack); err != nil {
		return
	}

	var lastWriteID uint32
	for {
		cmd, data, err := ReadFrame(conn)
		if err != nil {
			return
		}

		switch cmd {
		case CltocsFuseWriteData:
			// MooseFS 4.x CLTOCS_WRITE_DATA (212):
			// [chunkId:64][writeId:32][blocknum:16][blockOffset:16][size:32][crc:32][data:size]
			if len(data) < 24 { // minimum: 8+4+2+2+4+4 = 24 bytes before data
				return
			}
			var off int
			writeID, off, _ := ReadUint32(data, 8) // writeId:32 at offset 8 after chunkId:64
			lastWriteID = writeID
			blockNum, off, _ := ReadUint16(data, off) // blockNum at offset 12
			blockOff, off, _ := ReadUint16(data, off) // blockOff at offset 14
			size, off, _ := ReadUint32(data, off)     // size at offset 16
			off += 4                                  // skip CRC at offset 20
			if off+int(size) > len(data) {
				return
			}
			block := data[off : off+int(size)]
			dataOffset := uint32(blockNum)*65536 + uint32(blockOff)
			s.storeBlock(chunkID, dataOffset, block)

		case CltocsFuseWriteEnd: // CLTOCS_WRITE_FINISH (213)
			// Send final CSTOCL_WRITE_STATUS echoing the last writeId.
			var resp []byte
			resp = PutUint64(resp, chunkID)
			resp = PutUint32(resp, lastWriteID)
			resp = PutUint8(resp, StatusOK)
			_ = WriteFrame(conn, CstoclFuseWriteStatus, resp)
			return // write session complete

		default:
			return // unexpected frame
		}
	}
}

// ─── Early-CANTCONNECT fake server ────────────────────────────────────────────

// earlyCANTCONNECTServer listens on a random port and, after receiving
// CLTOCS_WRITE, immediately sends CSTOCL_WRITE_STATUS(CANTCONNECT) without
// reading any WRITE_DATA.  This simulates a CS that detects the chain peer is
// unreachable before the client has sent data (fast-failure path).
type earlyCANTCONNECTServer struct {
	listener net.Listener
	done     chan struct{}
}

func newEarlyCANTCONNECTServer() *earlyCANTCONNECTServer {
	return &earlyCANTCONNECTServer{done: make(chan struct{})}
}

func (s *earlyCANTCONNECTServer) Start() (ip uint32, port uint16) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("earlyCANTCONNECTServer: listen: " + err.Error())
	}
	s.listener = ln
	go func() {
		defer close(s.done)
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		s.handle(conn)
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return binary.BigEndian.Uint32(addr.IP.To4()), uint16(addr.Port)
}

func (s *earlyCANTCONNECTServer) Stop() {
	_ = s.listener.Close()
	<-s.done
}

func (s *earlyCANTCONNECTServer) handle(conn net.Conn) {
	// Consume CLTOCS_WRITE init frame.
	cmd, payload, err := ReadFrame(conn)
	if err != nil || cmd != CltocsFuseWrite || len(payload) < 13 {
		return
	}
	chunkID, _, _ := ReadUint64(payload, 1) // payload[0]=protocolid, [1:9]=chunkId

	// Immediately send CSTOCL_WRITE_STATUS(CANTCONNECT) — no WRITE_DATA received.
	var resp []byte
	resp = PutUint64(resp, chunkID)
	resp = PutUint32(resp, 0) // writeId = 0 (no WRITE_DATA received)
	resp = PutUint8(resp, StatusCSCANTCONNECT)
	_ = WriteFrame(conn, CstoclFuseWriteStatus, resp)
	// Server closes after sending — simulates chain establishment failure.
}

// ─── Chain-only CANTCONNECT fake server ───────────────────────────────────────

// chainOnlyCANTCONNECTCS is a fake chunk server that returns WRITE_STATUS
// CANTCONNECT on the mandatory write-init ACK when the CLTOCS_WRITE payload
// includes chain entries (payload length > 13 bytes), but accepts writes
// normally when no chain is present (payload length == 13 bytes — async write).
//
// This simulates a CS that is reachable but whose chain peer is unreachable:
//   - syncChain=true (chain entries present, len > 13)  → CANTCONNECT
//   - syncChain=false (no chain entries, len == 13)     → write accepted normally
//
// Used in TestWrite_FallbackCascade (Scenarios A and B).
type chainOnlyCANTCONNECTCS struct {
	inner *fakeCSServer // handles normal (async) writes; NOT started as a listener
	ln    net.Listener
	done  chan struct{}
}

func newChainOnlyCANTCONNECTCS() *chainOnlyCANTCONNECTCS {
	return &chainOnlyCANTCONNECTCS{
		inner: newFakeCSServer(), // not started — used for serveWrite + data storage only
		done:  make(chan struct{}),
	}
}

// Start binds to a random localhost port and begins accepting connections.
// Returns (ip uint32 big-endian, port uint16).
func (s *chainOnlyCANTCONNECTCS) Start() (ip uint32, port uint16) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("chainOnlyCANTCONNECTCS: listen: " + err.Error())
	}
	s.ln = ln
	go s.acceptLoop()
	addr := ln.Addr().(*net.TCPAddr)
	ipBytes := addr.IP.To4()
	return binary.BigEndian.Uint32(ipBytes), uint16(addr.Port)
}

// Stop closes the listener and waits for the accept goroutine to exit.
func (s *chainOnlyCANTCONNECTCS) Stop() {
	_ = s.ln.Close()
	<-s.done
}

// GetChunkData returns the stored chunk bytes (for data integrity verification
// after a successful async write to this server).
func (s *chainOnlyCANTCONNECTCS) GetChunkData(chunkID uint64) []byte {
	return s.inner.GetChunkData(chunkID)
}

func (s *chainOnlyCANTCONNECTCS) acceptLoop() {
	defer close(s.done)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *chainOnlyCANTCONNECTCS) handleConn(conn net.Conn) {
	defer conn.Close()

	cmd, payload, err := ReadFrame(conn)
	if err != nil {
		return
	}
	if cmd != CltocsFuseWrite || len(payload) < 13 {
		return
	}

	chunkID, _, _ := ReadUint64(payload, 1) // payload[0]=protocolid, [1:9]=chunkId

	if len(payload) > 13 {
		// Chain entries present in CLTOCS_WRITE payload → CANTCONNECT.
		// CSTOCL_WRITE_STATUS: [chunkId:64][writeId:32=0][status:8] = 13 bytes.
		var resp []byte
		resp = PutUint64(resp, chunkID)
		resp = PutUint32(resp, 0) // writeId = 0 (no data received)
		resp = PutUint8(resp, StatusCSCANTCONNECT)
		_ = WriteFrame(conn, CstoclFuseWriteStatus, resp)
		return
	}

	// No chain (payload == 13 bytes) → delegate to inner fakeCSServer's write handler.
	s.inner.serveWrite(conn, payload)
}

// ─── Bad-CRC fake server ──────────────────────────────────────────────────────

// badCRCServer listens on a random port and serves a single ReadChunk response
// where the CRC field is intentionally wrong (correct CRC XOR 0xDEADBEEF).
type badCRCServer struct {
	listener net.Listener
	done     chan struct{}
}

func newBadCRCServer() *badCRCServer {
	return &badCRCServer{done: make(chan struct{})}
}

func (s *badCRCServer) Start() (ip uint32, port uint16) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("badCRCServer: listen: " + err.Error())
	}
	s.listener = ln
	go func() {
		defer close(s.done)
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		s.handle(conn)
	}()
	addr := ln.Addr().(*net.TCPAddr)
	ip32 := binary.BigEndian.Uint32(addr.IP.To4())
	return ip32, uint16(addr.Port)
}

func (s *badCRCServer) Stop() {
	_ = s.listener.Close()
	<-s.done
}

func (s *badCRCServer) handle(conn net.Conn) {
	// Consume the CLTOCS_READ request.
	cmd, payload, err := ReadFrame(conn)
	if err != nil || cmd != CltocsFuseRead || len(payload) < 20 {
		return
	}
	chunkID, _, _ := ReadUint64(payload, 0)

	block := []byte("some chunk data")
	correctCRC := crc32.ChecksumIEEE(block)
	badCRC := correctCRC ^ 0xDEADBEEF // deliberately wrong

	// Send CSTOCL_READ_DATA with bad CRC.
	var resp []byte
	resp = PutUint64(resp, chunkID)
	resp = PutUint16(resp, 0) // blocknum
	resp = PutUint16(resp, 0) // blockOffset
	resp = PutUint32(resp, uint32(len(block)))
	resp = PutUint32(resp, badCRC)
	resp = append(resp, block...)
	_ = WriteFrame(conn, CstoclFuseReadData, resp)

	// Send CSTOCL_READ_STATUS OK.
	var status []byte
	status = PutUint64(status, chunkID)
	status = PutUint8(status, StatusOK)
	_ = WriteFrame(conn, CstoclFuseReadStatus, status)
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestDialCS_connect verifies that DialCS reaches the fake CS TCP listener.
func TestDialCS_connect(t *testing.T) {
	cs := newFakeCSServer()
	ip, port := cs.Start()
	defer cs.Stop()

	conn, err := DialCS(ip, port)
	require.NoError(t, err, "DialCS must connect to the fake CS listener")
	assert.NotNil(t, conn)
	_ = conn.Close()
}

// TestReadChunk_success verifies the full READ protocol round-trip against the
// fake chunk server: the server replies with READ_DATA + READ_STATUS.
func TestReadChunk_success(t *testing.T) {
	cs := newFakeCSServer()
	ip, port := cs.Start()
	defer cs.Stop()

	const chunkID = uint64(1001)
	content := []byte("chunk-read-test-data")
	cs.SetChunkData(chunkID, content)

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	got, err := ReadChunk(conn, chunkID, 1, 0, uint32(len(content)))
	require.NoError(t, err)
	assert.Equal(t, content, got, "ReadChunk must return the stored chunk data")
}

// TestReadChunk_CRCMismatch verifies that ReadChunk returns an error when the
// chunk server sends a READ_DATA frame with an incorrect CRC-32 checksum.
func TestReadChunk_CRCMismatch(t *testing.T) {
	srv := newBadCRCServer()
	ip, port := srv.Start()
	defer srv.Stop()

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	_, err = ReadChunk(conn, 9999, 1, 0, 64)
	require.Error(t, err, "ReadChunk must return an error on CRC mismatch")
	assert.Contains(t, err.Error(), "CRC mismatch", "error message must mention CRC mismatch")
}

// TestWriteChunk_success verifies the full WRITE protocol round-trip: the
// client sends WRITE + WRITE_DATA + WRITE_END and the fake CS responds with
// WRITE_STATUS, then the stored data matches what was sent.
func TestWriteChunk_success(t *testing.T) {
	cs := newFakeCSServer()
	ip, port := cs.Start()
	defer cs.Stop()

	const chunkID = uint64(2002)
	payload := []byte("chunk-write-test-data")

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	err = WriteChunk(conn, chunkID, 1, 0, payload, nil)
	require.NoError(t, err, "WriteChunk must succeed against the fake CS")

	// Verify the data was stored at offset 0.
	stored := cs.GetChunkData(chunkID)
	assert.Equal(t, payload, stored, "stored chunk data must match written payload")
}

// TestWriteChunk_withChain verifies that WriteChunk encodes protocolid:8=1 and
// chain servers correctly in the CLTOCS_WRITE init frame.
// Expected layout: [protocolid:8=1][chunkId:64][version:32][ip:32][port:16]
// = 1+8+4+4+2 = 19 bytes for 1 chain entry.
func TestWriteChunk_withChain(t *testing.T) {
	cs := newFakeCSServer()
	ip, port := cs.Start()
	defer cs.Stop()

	const chunkID = uint64(3003)
	payload := []byte("chain-write-test")

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	// Intercept the init frame by wrapping the connection in a recorder.
	// Instead, we verify indirectly: the fake CS parses chunkId at offset 1
	// (protocolid byte), so a successful round-trip proves the layout is correct.
	const chainIP = uint32(0xC0A802DC) // 192.168.2.220
	const chainPort = uint16(9423)
	chain := []ChunkServer{{IP: chainIP, Port: chainPort}}
	err = WriteChunk(conn, chunkID, 1, 0, payload, chain)
	require.NoError(t, err, "WriteChunk with chain must succeed against the fake CS")

	// The fake CS read chunkId at payload[1:9] — verify data stored under correct key.
	stored := cs.GetChunkData(chunkID)
	assert.Equal(t, payload, stored, "stored chunk data must match written payload")
}

// TestWriteChunk_earlyCANTCONNECT verifies that WriteChunk correctly handles
// CSTOCL_WRITE_STATUS(CANTCONNECT) returned as the write-init ACK.
// Per writedata.c, the CS sends WRITE_STATUS(writeid=0, CANTCONNECT) in the ACK
// slot when it cannot reach a chain peer.  WriteChunk must abort with a clear error.
func TestWriteChunk_earlyCANTCONNECT(t *testing.T) {
	srv := newEarlyCANTCONNECTServer()
	ip, port := srv.Start()
	defer srv.Stop()

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	err = WriteChunk(conn, uint64(5005), 1, 0, []byte("chain-fail-test"), nil)
	require.Error(t, err, "WriteChunk must return error when write-init ACK contains CANTCONNECT")
	assert.Contains(t, err.Error(), "CANTCONNECT",
		"error must identify CANTCONNECT status")
}

// TestWriteChunk_chainEOF verifies that WriteChunk returns a diagnostic error
// when the CS closes the connection during the write-init ACK phase without
// sending a WRITE_STATUS frame — the behaviour observed with MooseFS 4.58.8
// when the chain CS times out (~5 s TCP) and the CS gives up.
func TestWriteChunk_chainEOF(t *testing.T) {
	// Server that accepts CLTOCS_WRITE, sends 2 NOP keepalives (simulating chain
	// connection attempts), then closes WITHOUT sending the mandatory write-init ACK.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Consume CLTOCS_WRITE.
		cmd, initPayload, err := ReadFrame(conn)
		if err != nil || cmd != CltocsFuseWrite || len(initPayload) < 13 {
			return
		}

		// Send 2 NOP keepalives (CS polling chain peer), then close with EOF
		// WITHOUT sending the mandatory WRITE_STATUS ACK — MooseFS 4.58.8 behavior
		// when chain peer times out.
		_ = WriteFrame(conn, ANTOAN_NOP, nil)
		_ = WriteFrame(conn, ANTOAN_NOP, nil)
		// conn.Close() via defer — sends EOF to the client during ACK read.
	}()

	addr := ln.Addr().(*net.TCPAddr)
	csIP := binary.BigEndian.Uint32(addr.IP.To4())
	csPort := uint16(addr.Port)

	conn, err := DialCS(csIP, csPort)
	require.NoError(t, err)
	defer conn.Close()

	err = WriteChunk(conn, uint64(6006), 1, 0, []byte("eof-chain-test"), nil)
	require.Error(t, err, "WriteChunk must return error on CS EOF without write-init ACK")
	assert.Contains(t, err.Error(), "chain CS",
		"error must mention chain CS connectivity as likely cause")

	<-done
}

// TestWriteChunk_NOPskip verifies that WriteChunk silently ignores ANTOAN_NOP
// (cmd=0) keepalive frames sent by the CS before the real WRITE_STATUS.
func TestWriteChunk_NOPskip(t *testing.T) {
	base := newFakeCSServer()

	// Custom listener: injects 3 NOP frames before the real WRITE_STATUS.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	const chunkID = uint64(4004)
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Consume CLTOCS_WRITE init frame.
		cmd, initPayload, err := ReadFrame(conn)
		if err != nil || cmd != CltocsFuseWrite || len(initPayload) < 13 {
			return
		}
		// chunkId at offset 1 (protocolid byte first).

		// Send mandatory write-init ACK before client sends WRITE_DATA.
		var ack []byte
		ack = PutUint64(ack, chunkID)
		ack = PutUint32(ack, 0) // writeId = 0
		ack = PutUint8(ack, StatusOK)
		if err := WriteFrame(conn, CstoclFuseWriteStatus, ack); err != nil {
			return
		}

		// Consume WRITE_DATA + WRITE_END frames, storing data.
		for {
			cmd, data, err := ReadFrame(conn)
			if err != nil {
				return
			}
			if cmd == CltocsFuseWriteData {
				if len(data) < 24 {
					return
				}
				_, off, _ := ReadUint32(data, 8) // skip writeId
				blockNum, off, _ := ReadUint16(data, off)
				blockOff, off, _ := ReadUint16(data, off)
				size, off, _ := ReadUint32(data, off)
				off += 4 // skip CRC
				block := data[off : off+int(size)]
				base.storeBlock(chunkID, uint32(blockNum)*65536+uint32(blockOff), block)
			} else if cmd == CltocsFuseWriteEnd {
				break
			} else {
				return
			}
		}

		// Send 3 NOP keepalives before the final WRITE_STATUS.
		for i := 0; i < 3; i++ {
			_ = WriteFrame(conn, ANTOAN_NOP, nil)
		}
		var resp []byte
		resp = PutUint64(resp, chunkID)
		resp = PutUint32(resp, 0) // writeId
		resp = PutUint8(resp, StatusOK)
		_ = WriteFrame(conn, CstoclFuseWriteStatus, resp)
	}()

	addr := ln.Addr().(*net.TCPAddr)
	ip := binary.BigEndian.Uint32(addr.IP.To4())
	port := uint16(addr.Port)

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	payload := []byte("nop-skip-test")
	err = WriteChunk(conn, chunkID, 1, 0, payload, nil)
	require.NoError(t, err, "WriteChunk must succeed even when CS sends NOP keepalives before WRITE_STATUS")

	<-done
	stored := base.GetChunkData(chunkID)
	assert.Equal(t, payload, stored, "stored chunk data must match written payload")
}

// TestWriteChunk_writeInitACK_OK verifies that WriteChunk correctly reads the
// mandatory write-init ACK (WRITE_STATUS writeid=0 OK) before sending WRITE_DATA,
// including when the CS sends NOP keepalives before the ACK while establishing
// the replication chain.
func TestWriteChunk_writeInitACK_OK(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	const chunkID = uint64(9009)
	data := []byte("write-init-ack-ok-test")
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Receive CLTOCS_WRITE.
		cmd, initPayload, err := ReadFrame(conn)
		if err != nil || cmd != CltocsFuseWrite || len(initPayload) < 13 {
			return
		}

		// Send 3 NOPs (chain connection in progress), then write-init ACK OK.
		for i := 0; i < 3; i++ {
			_ = WriteFrame(conn, ANTOAN_NOP, nil)
		}
		var ack []byte
		ack = PutUint64(ack, chunkID)
		ack = PutUint32(ack, 0) // writeId = 0
		ack = PutUint8(ack, StatusOK)
		if err := WriteFrame(conn, CstoclFuseWriteStatus, ack); err != nil {
			return
		}

		// Serve WRITE_DATA + WRITE_END, then send final WRITE_STATUS.
		var lastWriteID uint32
		for {
			c, d, e := ReadFrame(conn)
			if e != nil {
				return
			}
			if c == CltocsFuseWriteData && len(d) >= 24 {
				wid, _, _ := ReadUint32(d, 8)
				lastWriteID = wid
			} else if c == CltocsFuseWriteEnd {
				var resp []byte
				resp = PutUint64(resp, chunkID)
				resp = PutUint32(resp, lastWriteID)
				resp = PutUint8(resp, StatusOK)
				_ = WriteFrame(conn, CstoclFuseWriteStatus, resp)
				return
			}
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	csIP := binary.BigEndian.Uint32(addr.IP.To4())
	csPort := uint16(addr.Port)

	conn, err := DialCS(csIP, csPort)
	require.NoError(t, err)
	defer conn.Close()

	err = WriteChunk(conn, chunkID, 1, 0, data, nil)
	require.NoError(t, err, "WriteChunk must succeed when CS sends NOPs then write-init ACK OK")
	<-done
}

// TestWriteChunk_Pipeline verifies the pipeline write behaviour of WriteChunk:
// a concurrent reader goroutine detects CS errors concurrently with the sender,
// enabling early abort when the CS sends a non-OK WRITE_STATUS before WRITE_END.
func TestWriteChunk_Pipeline(t *testing.T) {
	// ── Sub-test 1: multi-block success ───────────────────────────────────────
	// 3 full 65536-byte blocks + 1 partial block → all stored correctly.
	t.Run("success_multiblock", func(t *testing.T) {
		cs := newFakeCSServer()
		ip, port := cs.Start()
		defer cs.Stop()

		const chunkID = uint64(7007)
		// 3×65536 + 1000 bytes — exercises the block-loop code path.
		payload := make([]byte, 3*65536+1000)
		for i := range payload {
			payload[i] = byte(i % 251)
		}

		conn, err := DialCS(ip, port)
		require.NoError(t, err)
		defer conn.Close()

		err = WriteChunk(conn, chunkID, 1, 0, payload, nil)
		require.NoError(t, err, "pipeline WriteChunk must succeed for multi-block write")

		stored := cs.GetChunkData(chunkID)
		assert.Equal(t, payload, stored, "all blocks must be stored in the CS")
	})

	// ── Sub-test 2: CS sends error STATUS after first WRITE_DATA → early abort ─
	t.Run("earlyAbort_CSError", func(t *testing.T) {
		// Custom CS: accepts WRITE init (sends ACK OK), receives one WRITE_DATA,
		// immediately sends WRITE_STATUS(DISCONNECTED) and closes the connection.
		// This simulates a CS that detects a fault mid-stream.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer ln.Close()

		var dataFramesReceived atomic.Int32
		done := make(chan struct{})

		go func() {
			defer close(done)
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()

			// Consume CLTOCS_WRITE init.
			cmd, initPayload, err := ReadFrame(conn)
			if err != nil || cmd != CltocsFuseWrite || len(initPayload) < 13 {
				return
			}
			chunkID, _, _ := ReadUint64(initPayload, 1)

			// Send write-init ACK OK.
			var ack []byte
			ack = PutUint64(ack, chunkID)
			ack = PutUint32(ack, 0) // writeId = 0
			ack = PutUint8(ack, StatusOK)
			if err := WriteFrame(conn, CstoclFuseWriteStatus, ack); err != nil {
				return
			}

			// Receive exactly one WRITE_DATA, send error STATUS, then drain
			// remaining frames until the client closes the connection.
			// NOT closing immediately: a premature RST can corrupt the STATUS
			// payload in-flight so the reader sees "connection reset" instead
			// of DISCONNECTED.  The client will close after seeing earlyErr.
			cmd, _, err = ReadFrame(conn)
			if err != nil || cmd != CltocsFuseWriteData {
				return
			}
			dataFramesReceived.Add(1)

			// Send WRITE_STATUS(DISCONNECTED) to simulate a mid-stream CS fault.
			var resp []byte
			resp = PutUint64(resp, chunkID)
			resp = PutUint32(resp, 1) // writeId = 1 (echoes the WRITE_DATA we just got)
			resp = PutUint8(resp, StatusCSDISCONNECTED)
			if err := WriteFrame(conn, CstoclFuseWriteStatus, resp); err != nil {
				return
			}

			// Drain remaining frames until client closes (or EOF).
			// This prevents a premature RST from corrupting the STATUS payload.
			for {
				_, _, readErr := ReadFrame(conn)
				if readErr != nil {
					return
				}
				dataFramesReceived.Add(1)
			}
		}()

		addr := ln.Addr().(*net.TCPAddr)
		csIP := binary.BigEndian.Uint32(addr.IP.To4())
		csPort := uint16(addr.Port)

		conn, err := DialCS(csIP, csPort)
		require.NoError(t, err)

		// 5-block payload: if early-abort works the sender stops before sending all 5.
		payload := make([]byte, 5*65536)
		writeErr := WriteChunk(conn, uint64(8008), 1, 0, payload, nil)
		// Close the connection explicitly so the CS goroutine's drain loop gets
		// EOF and exits — otherwise it blocks and <-done deadlocks.
		conn.Close()

		require.Error(t, writeErr, "WriteChunk must return error on mid-stream CS error STATUS")
		assert.Contains(t, writeErr.Error(), "DISCONNECTED",
			"error must identify the DISCONNECTED status sent by the CS")

		<-done
		// The CS must have received at least 1 WRITE_DATA frame (the one that
		// triggered the error).  It may have received additional frames if the
		// sender's earlyErr check races with the reader goroutine, but early
		// abort guarantees fewer than all 5 blocks are delivered when the
		// protocol-level error is detected promptly.
		assert.GreaterOrEqual(t, dataFramesReceived.Load(), int32(1),
			"CS must have received at least 1 WRITE_DATA frame")
	})
}

// TestWriteChunk_writeInitACK_CANTCONNECT verifies that WriteChunk returns an
// error with the CANTCONNECT status name when the write-init ACK carries an error.
// This is the canonical chain-peer-unreachable error path per writedata.c.
func TestWriteChunk_writeInitACK_CANTCONNECT(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	const chunkID = uint64(1010)
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Receive CLTOCS_WRITE.
		cmd, initPayload, err := ReadFrame(conn)
		if err != nil || cmd != CltocsFuseWrite || len(initPayload) < 13 {
			return
		}

		// Send write-init ACK with CANTCONNECT status.
		var resp []byte
		resp = PutUint64(resp, chunkID)
		resp = PutUint32(resp, 0) // writeId = 0
		resp = PutUint8(resp, StatusCSCANTCONNECT)
		_ = WriteFrame(conn, CstoclFuseWriteStatus, resp)
	}()

	addr := ln.Addr().(*net.TCPAddr)
	csIP := binary.BigEndian.Uint32(addr.IP.To4())
	csPort := uint16(addr.Port)

	conn, err := DialCS(csIP, csPort)
	require.NoError(t, err)
	defer conn.Close()

	err = WriteChunk(conn, chunkID, 1, 0, []byte("cantconnect-ack-test"), nil)
	require.Error(t, err, "WriteChunk must return error on CANTCONNECT in write-init ACK")
	assert.Contains(t, err.Error(), "CANTCONNECT",
		"error must identify CANTCONNECT status")
	<-done
}

// ── csPool unit tests ─────────────────────────────────────────────────────────

// TestCSPool_DialsWhenEmpty verifies that Get() on an empty pool dials a new
// TCP connection to the chunk server.
func TestCSPool_DialsWhenEmpty(t *testing.T) {
	srv := newFakeCSServer()
	ip, port := srv.Start()
	defer srv.Stop()

	pool := newCSPool()
	defer pool.CloseAll()

	conn, err := pool.Get(ip, port)
	require.NoError(t, err, "Get on empty pool must dial successfully")
	conn.Close()

	// The server increments connCount in its acceptLoop goroutine — use Eventually
	// to avoid a race between the client close and the server increment.
	require.Eventually(t, func() bool {
		return srv.connCount.Load() == int64(1)
	}, 200*time.Millisecond, 5*time.Millisecond,
		"exactly one TCP connection must be established when pool is empty")
}

// TestCSPool_ReuseIdle verifies that Put() followed by Get() returns the same
// connection without dialling a second TCP connection.
func TestCSPool_ReuseIdle(t *testing.T) {
	srv := newFakeCSServer()
	ip, port := srv.Start()
	defer srv.Stop()

	pool := newCSPool()

	// Dial once and return to pool.
	conn1, err := pool.Get(ip, port)
	require.NoError(t, err)
	pool.Put(conn1, ip, port)

	// Next Get must reuse the pooled connection (same pointer, no second dial).
	conn2, err := pool.Get(ip, port)
	require.NoError(t, err)
	defer conn2.Close()

	assert.Same(t, conn1, conn2,
		"pool must return the same idle connection without dialling again")
	// Use Eventually: the server increment races with the client-side check.
	require.Eventually(t, func() bool {
		return srv.connCount.Load() == int64(1)
	}, 200*time.Millisecond, 5*time.Millisecond,
		"only one TCP connection should have been accepted by the server")
}

// TestCSPool_FullPool_ClosesExtra verifies that when the pool is full, a Put()
// closes the surplus connection rather than silently dropping it.
func TestCSPool_FullPool_ClosesExtra(t *testing.T) {
	// Use max=1 so we can easily fill the pool with a single Put().
	pool := &csPool{idle: make(map[string][]net.Conn), max: 1}

	const (
		testIP   = uint32(0x7F000001) // 127.0.0.1
		testPort = uint16(9420)
	)
	key := csAddr(testIP, testPort)

	c1, c1Remote := net.Pipe()
	c2, c2Remote := net.Pipe()
	defer c1Remote.Close()
	defer c2Remote.Close()

	pool.Put(c1, testIP, testPort) // pool: [c1]
	pool.Put(c2, testIP, testPort) // pool full → c2 must be closed

	// c2 must be unusable after being closed by the pool.
	_, writeErr := c2.Write([]byte("x"))
	assert.Error(t, writeErr, "connection closed by full pool must not accept writes")

	// Pool must still retain c1.
	pool.mu.Lock()
	retained := len(pool.idle[key])
	pool.mu.Unlock()
	assert.Equal(t, 1, retained, "pool must retain the first connection")
	c1.Close() // cleanup
}

// TestCSPool_CloseAll verifies that CloseAll() closes every idle connection
// and empties the pool, making retained connections unusable.
func TestCSPool_CloseAll(t *testing.T) {
	pool := newCSPool()

	c1, c1Remote := net.Pipe()
	c2, c2Remote := net.Pipe()
	defer c1Remote.Close()
	defer c2Remote.Close()

	pool.Put(c1, 0x7F000001, 9420)
	pool.Put(c2, 0x7F000001, 9421) // different port

	pool.CloseAll()

	_, err1 := c1.Write([]byte("x"))
	_, err2 := c2.Write([]byte("x"))
	assert.Error(t, err1, "c1 must be closed after CloseAll")
	assert.Error(t, err2, "c2 must be closed after CloseAll")
}

// TestRead_StalePool_RetryOnce verifies that when the CS connection pool
// contains a stale connection (closed server-side), client.Read() retries
// once with a fresh dial and succeeds — preventing the caller from receiving
// EIO and retrying the FUSE Open() in a tight loop (Windows Explorer tight
// retry behaviour that produced a "parseChunkInfo" log storm — issue #112).
func TestRead_StalePool_RetryOnce(t *testing.T) {
	c, srv := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "stale_retry.txt", 0o644)
	require.NoError(t, err)

	content := []byte("stale-retry-test-content")
	require.NoError(t, c.Write(nodeID, 0, content))

	// Inject a pre-closed (stale) connection into the pool.
	// This simulates a connection that was pooled after a previous successful
	// operation but was then closed by the CS (idle timeout / network reset).
	staleConn, getErr := c.pool.Get(srv.csIP, srv.csPort)
	require.NoError(t, getErr)
	staleConn.Close() // close from client side — server will see EOF on next read
	c.pool.Put(staleConn, srv.csIP, srv.csPort)

	// Reset the CS connection counter so we can observe the retry dial.
	srv.cs.connCount.Store(0)

	// Read() must succeed: it should silently discard the stale connection and
	// retry once with a fresh CS connection.
	got, readErr := c.Read(nodeID, 0, uint32(len(content)))
	require.NoError(t, readErr, "Read() must succeed transparently after a stale CS connection")
	assert.Equal(t, content, got, "Read() must return the correct data after stale retry")

	// The retry must have dialled exactly one new CS connection.
	require.Eventually(t, func() bool {
		return srv.cs.connCount.Load() == int64(1)
	}, 200*time.Millisecond, 5*time.Millisecond,
		"exactly 1 new CS connection must be dialled for the stale-retry")
}

// TestRead_PoolReuse verifies that client.Read() reuses the CS connection
// across multiple sequential calls so that only one TCP connection is
// established against the fake CS (prevents TIME_WAIT exhaustion on Windows —
// WSAEADDRINUSE / error 10048).
func TestRead_PoolReuse(t *testing.T) {
	c, srv := newTestClient(t)

	nodeID, err := c.Mknod(RootNodeID, "pool_reuse.txt", 0o644)
	require.NoError(t, err)

	content := []byte("pool-reuse-integration-test-data")
	require.NoError(t, c.Write(nodeID, 0, content))
	// After Write() returns, the CS connection is back in the pool and
	// srv.cs.connCount == 1 (Write() established the first connection).

	// Five sequential Reads must reuse the pooled connection — no new dial.
	for i := 0; i < 5; i++ {
		got, readErr := c.Read(nodeID, 0, uint32(len(content)))
		require.NoError(t, readErr, "Read #%d must succeed", i+1)
		assert.Equal(t, content, got, "Read #%d must return correct data", i+1)
	}

	// Total CS connections after 1 Write + 5 Reads must be exactly 1.
	assert.Equal(t, int64(1), srv.cs.connCount.Load(),
		"pool must reuse a single CS connection across Write + 5 sequential Reads")
}

// ── Bugfix #160 — ReadChunk NOP handling ──────────────────────────────────────
//
// Root cause (see docs/diagrams/moosefs-ec4-read-statemachine.md and
// _work/reports/plan-20260812-102022.md, D1): ReadChunk's frame switch has no
// branch for ANTOAN_NOP (cmd=0), so a legitimate keepalive sent by a live CS
// mid-read falls into `default:` and aborts the read.  The three other
// protocol loops in this package (client.go:101, csclient.go write-init ACK,
// csclient.go write-status reader) already skip cmd=0; TestWriteChunk_NOPskip
// covers the write path.  The tests below are the read-path counterpart.

// TestReadChunk_NOPskip verifies that ReadChunk silently skips ANTOAN_NOP
// (cmd=0) keepalive frames interleaved between CSTOCL_READ_DATA frames, and
// still returns the complete, correct chunk data (CA1).
func TestReadChunk_NOPskip(t *testing.T) {
	const chunkID = uint64(11011)
	content := []byte("nop-skip-read-test-data")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()

		cmd, payload, readErr := ReadFrame(conn)
		if readErr != nil || cmd != CltocsFuseRead || len(payload) < 20 {
			return
		}
		gotChunkID, _, _ := ReadUint64(payload, 0)

		// NOP keepalive BEFORE the first READ_DATA frame.
		_ = WriteFrame(conn, ANTOAN_NOP, nil)

		// Real READ_DATA frame.
		checksum := crc32.ChecksumIEEE(content)
		var dataResp []byte
		dataResp = PutUint64(dataResp, gotChunkID)
		dataResp = PutUint16(dataResp, 0) // blocknum
		dataResp = PutUint16(dataResp, 0) // blockOffset
		dataResp = PutUint32(dataResp, uint32(len(content)))
		dataResp = PutUint32(dataResp, checksum)
		dataResp = append(dataResp, content...)
		if writeErr := WriteFrame(conn, CstoclFuseReadData, dataResp); writeErr != nil {
			return
		}

		// NOP keepalives AFTER READ_DATA, BEFORE READ_STATUS.
		_ = WriteFrame(conn, ANTOAN_NOP, nil)
		_ = WriteFrame(conn, ANTOAN_NOP, nil)

		// Real READ_STATUS frame.
		var statusResp []byte
		statusResp = PutUint64(statusResp, gotChunkID)
		statusResp = PutUint8(statusResp, StatusOK)
		_ = WriteFrame(conn, CstoclFuseReadStatus, statusResp)
	}()

	addr := ln.Addr().(*net.TCPAddr)
	ip := binary.BigEndian.Uint32(addr.IP.To4())
	port := uint16(addr.Port)

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	got, err := ReadChunk(conn, chunkID, 1, 0, uint32(len(content)))
	require.NoError(t, err, "ReadChunk must succeed even when the CS sends NOP keepalives interleaved with READ_DATA/READ_STATUS")
	assert.Equal(t, content, got, "returned data must be intact — NOP frames must not corrupt or truncate it")

	<-done
}

// TestReadChunk_NOPBadLength verifies that ReadChunk treats an ANTOAN_NOP
// frame carrying a non-zero payload length as a protocol error rather than
// silently skipping it. A legitimate keepalive always has length 0
// (protocol.go: ANTOAN_NOP payload is empty); a non-empty NOP means the
// client and CS have desynchronised on the frame stream. Conformance target:
// the official MooseFS client (readdata.c:1682-1691) treats this exact case
// as fatal ("got wrong sized nop packet from chunkserver"), not as a skip
// (CA2, plan révision 2 task 20).
func TestReadChunk_NOPBadLength(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		if _, _, readErr := ReadFrame(conn); readErr != nil {
			return
		}
		// A malformed NOP: cmd=0 but with a non-empty payload — real
		// ANTOAN_NOP keepalives always carry zero bytes.
		_ = WriteFrame(conn, ANTOAN_NOP, []byte{0xAA, 0xBB, 0xCC})
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	addr := ln.Addr().(*net.TCPAddr)
	ip := binary.BigEndian.Uint32(addr.IP.To4())
	port := uint16(addr.Port)

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	_, err = ReadChunk(conn, 42, 1, 0, 64)
	require.Error(t, err, "a NOP frame with a non-zero payload length must be a protocol error, not a silently skipped keepalive")
}

// TestReadChunk_NOPFlood verifies that a chunk server which only ever sends
// ANTOAN_NOP keepalives (never READ_DATA nor READ_STATUS) cannot make
// ReadChunk block forever: the anti-flood guard must return a bounded error
// (CA3). A server-controlled, unbounded frame is a denial-of-service vector if
// the client loop has no cap.
//
// Safety note: the assertion runs through a goroutine + select/timeout rather
// than calling ReadChunk synchronously, so this test fails loudly (instead of
// hanging the whole `go test` run) if the anti-flood guard is missing.
func TestReadChunk_NOPFlood(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		if _, _, readErr := ReadFrame(conn); readErr != nil {
			return
		}
		// Flood NOPs until the client disconnects (write fails).
		for {
			if writeErr := WriteFrame(conn, ANTOAN_NOP, nil); writeErr != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-serverDone
	})

	addr := ln.Addr().(*net.TCPAddr)
	ip := binary.BigEndian.Uint32(addr.IP.To4())
	port := uint16(addr.Port)

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	resultCh := make(chan error, 1)
	go func() {
		_, readErr := ReadChunk(conn, 42, 1, 0, 64)
		resultCh <- readErr
	}()

	select {
	case readErr := <-resultCh:
		require.Error(t, readErr, "a CS that only sends NOP keepalives must eventually produce a bounded error, not succeed")
	case <-time.After(5 * time.Second):
		t.Fatal("ReadChunk did not return within 5s against a NOP-flooding CS — anti-flood guard missing or unbounded (CA2)")
	}
}

// TestReadChunk_UnknownCmd verifies that a genuinely unknown opcode (neither
// ANTOAN_NOP=0, CSTOCL_READ_STATUS=201, nor CSTOCL_READ_DATA=202) remains a
// fatal, non-retryable protocol error — distinct from the now-legitimate NOP
// case (CA3).
func TestReadChunk_UnknownCmd(t *testing.T) {
	const unknownCmd = uint32(9999) // not 0, 201, or 202 — see protocol.go opcode tables

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		if _, _, readErr := ReadFrame(conn); readErr != nil {
			return
		}
		_ = WriteFrame(conn, unknownCmd, []byte("garbage"))
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	addr := ln.Addr().(*net.TCPAddr)
	ip := binary.BigEndian.Uint32(addr.IP.To4())
	port := uint16(addr.Port)

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	_, err = ReadChunk(conn, 42, 1, 0, 64)
	require.Error(t, err, "ReadChunk must fail fatally on a genuinely unknown opcode")
	assert.False(t, isStaleConnErr(err),
		"a genuinely unknown opcode must never be classified as a retryable stale-connection error (CA3)")
}

// ── Bugfix #160 — retry policy (forced fresh dial, bounded backoff, pool age) ─
//
// These tests exercise the retry policy through c.readEC4At (defined in
// ecclient.go), reusing the EC4 test helpers from ecclient_test.go
// (makeECClient, makeShardBytes, startFourCSServers) — same package, no new
// imports required. Placed here per the #160 test-writer dispatch (all Phase 4
// tasks default to this file unless stated otherwise).

// TestRetry_ForcesFreshDial verifies that once the first read attempt fails,
// every subsequent retry dials a brand-new connection instead of popping
// another (possibly also stale) connection out of the pool (CA5, D3).
//
// Setup: the pool for shard 0's CS is pre-loaded with 2 pre-closed ("stale")
// connections — fewer than maxIdleCSConns (4), matching the D3 scenario where
// several stale entries can sit in the pool at once. Before the fix, the
// retry-once policy called pool.Get() again on the second attempt, which
// could return the second stale entry and fail outright. After the fix, any
// attempt beyond the first must bypass the pool entirely.
func TestRetry_ForcesFreshDial(t *testing.T) {
	const block = uint32(65536)
	const logicalID = uint64(0xF125DA1)

	shardData := [4][]byte{
		makeShardBytes(0, int(block)),
		makeShardBytes(1, int(block)),
		makeShardBytes(2, int(block)),
		makeShardBytes(3, int(block)),
	}
	info, servers := startFourCSServers(t, logicalID, shardData)
	srv0 := info.Servers[0]

	c := makeECClient(t, info)

	// Pre-load the pool with 2 pre-closed connections for shard 0's address.
	// Both Get() calls must happen BEFORE either Put(): once one closed
	// connection is back in the pool, a later Get() would simply pop it again
	// (Get only checks age, not liveness) instead of dialling a genuinely
	// second connection.
	stale1, dialErr := c.pool.Get(srv0.IP, srv0.Port)
	require.NoError(t, dialErr)
	stale2, dialErr := c.pool.Get(srv0.IP, srv0.Port)
	require.NoError(t, dialErr)
	stale1.Close()
	stale2.Close()
	c.pool.Put(stale1, srv0.IP, srv0.Port)
	c.pool.Put(stale2, srv0.IP, srv0.Port)

	// The server increments connCount from its acceptLoop goroutine, which
	// races with the client-side dial returning — wait for both accepts to
	// be registered before resetting, otherwise a pending increment from
	// setup could land after Store(0) and pollute the assertion below.
	require.Eventually(t, func() bool {
		return servers[0].connCount.Load() == int64(2)
	}, 200*time.Millisecond, 5*time.Millisecond,
		"both stale setup connections must be registered by the server before isolating the count")
	// Isolate the connection count to what happens from this point on —
	// the 2 stale dials above already incremented it.
	servers[0].connCount.Store(0)

	got, err := c.readEC4At(1, info, 0, 0, block)
	require.NoError(t, err, "retry must dial fresh rather than exhausting the pool's stale entries")
	assert.Equal(t, shardData[0], got, "data must match shard 0 after the forced fresh dial")
	assert.Equal(t, int64(1), servers[0].connCount.Load(),
		"exactly one fresh TCP connection must be dialled for the retry — the 2 stale pool entries must be bypassed, not consumed")
}

// serveInvalidationMaster serves a minimal fake MooseFS master for
// TestRetry_InvalidatesChunkLocation: REGISTER + READ_CHUNK (proto=2, a
// normal non-EC chunk). It answers the Nth READ_CHUNK request for this
// connection with badAddr for N==1 and goodAddr for every subsequent request,
// so the test can prove whether the client re-queries the master before
// retrying rather than reusing the first (dead) location it was handed.
func serveInvalidationMaster(conn net.Conn, chunkID uint64, fileLen uint64,
	badIP uint32, badPort uint16, goodIP uint32, goodPort uint16, readChunkCalls *atomic.Int64) {
	defer conn.Close()
	for {
		cmd, payload, err := ReadFrame(conn)
		if err != nil {
			return
		}
		switch cmd {
		case CltomFuseRegister:
			if len(payload) < 4 {
				return
			}
			msgid := binary.BigEndian.Uint32(payload[:4])
			var resp []byte
			resp = PutUint32(resp, msgid)
			resp = PutUint32(resp, 0xABCD1234) // sessionID (fixed — no client-visible meaning here)
			resp = PutUint32(resp, 0)          // maxopenfiles (unused)
			_ = WriteFrame(conn, MatoclFuseRegister, resp)

		case CltomFuseReadChunk:
			if len(payload) < 12 {
				return
			}
			msgid, _, _ := ReadUint32(payload, 0)

			n := readChunkCalls.Add(1)
			ip, port := badIP, badPort
			if n > 1 {
				ip, port = goodIP, goodPort
			}

			var resp []byte
			resp = PutUint32(resp, msgid)
			resp = PutUint8(resp, 2) // protocolid = 2 (normal replicated chunk)
			resp = PutUint64(resp, fileLen)
			resp = PutUint64(resp, chunkID)
			resp = PutUint32(resp, 1) // version
			resp = PutUint32(resp, ip)
			resp = PutUint16(resp, port)
			resp = PutUint32(resp, 0) // cs_ver
			resp = PutUint32(resp, 0) // labelmask
			_ = WriteFrame(conn, MatoclFuseReadChunk, resp)

		default:
			return
		}
	}
}

// TestRetry_InvalidatesChunkLocation verifies invariant 11 of
// docs/diagrams/moosefs-ec4-read-statemachine.md §2.4 (plan révision 2, task
// 25, CA7): before retrying, the client must invalidate the cached chunk
// location and re-query the master for a fresh one — mirroring the official
// client's chunksdatacache_invalidate (readdata.c:1989) — rather than
// re-attacking the same (now known-dead) chunk server from the location it
// already has cached.
//
// This is exercised end-to-end through Client.Read() (black-box: no
// assumption about doCSRead's internal signature) with a fake master that
// answers the first READ_CHUNK with a dead CS address and every subsequent
// READ_CHUNK with a live one. If the client only retries against the
// originally-cached address, this read can never succeed.
func TestRetry_InvalidatesChunkLocation(t *testing.T) {
	const nodeID = uint32(1)
	const block = uint32(4096)
	const chunkID = uint64(0xABCDEF01)
	content := makeShardBytes(3, int(block))

	// "Bad" CS: accepts then immediately closes — a retryable failure. The
	// client must stop attacking this address after invalidating it, not
	// keep retrying it under its own backoff budget.
	badLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	badDone := make(chan struct{})
	go func() {
		defer close(badDone)
		for {
			conn, acceptErr := badLn.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() {
		_ = badLn.Close()
		<-badDone
	})
	badAddr := badLn.Addr().(*net.TCPAddr)
	badIP := binary.BigEndian.Uint32(badAddr.IP.To4())
	badPort := uint16(badAddr.Port)

	// "Good" CS: serves the real chunk content.
	goodCS := newFakeCSServer()
	goodIP, goodPort := goodCS.Start()
	t.Cleanup(goodCS.Stop)
	goodCS.SetChunkData(chunkID, content)

	var readChunkCalls atomic.Int64
	masterLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	masterDone := make(chan struct{})
	go func() {
		defer close(masterDone)
		for {
			conn, acceptErr := masterLn.Accept()
			if acceptErr != nil {
				return
			}
			go serveInvalidationMaster(conn, chunkID, uint64(len(content)), badIP, badPort, goodIP, goodPort, &readChunkCalls)
		}
	}()
	t.Cleanup(func() {
		_ = masterLn.Close()
		<-masterDone
	})

	masterAddr := masterLn.Addr().(*net.TCPAddr)
	c, err := Dial("127.0.0.1", masterAddr.Port)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	got, err := c.Read(nodeID, 0, block)
	require.NoError(t, err, "Read must succeed by invalidating the cached (dead) chunk location and re-querying the master, not by retrying the same address")
	assert.Equal(t, content, got, "data must match the content served by the good CS")
	assert.GreaterOrEqualf(t, readChunkCalls.Load(), int64(2),
		"the client must issue at least 2 READ_CHUNK requests to the master — the initial one plus at least one re-query after invalidating the bad location (CA7, invariant 11)")
}

// TestRetry_BackoffBounded verifies that when every retry attempt fails with a
// retryable error, the total time spent backing off before giving up is
// bounded (CA6). The budget must stay well under the delay before Windows
// Explorer reissues an Open() (on the order of a minute per #160), or
// low-level retries and the CF-API retry would stack.
//
// 5s is a conservative, implementation-agnostic ceiling for this assertion;
// tighten it once the real backoff constants land if a stricter bound is
// wanted.
func TestRetry_BackoffBounded(t *testing.T) {
	const block = uint32(65536)
	const logicalID = uint64(0xBAC0FF)

	shardData := [4][]byte{
		makeShardBytes(0, int(block)),
		makeShardBytes(1, int(block)),
		makeShardBytes(2, int(block)),
		makeShardBytes(3, int(block)),
	}
	info, _ := startFourCSServers(t, logicalID, shardData)

	// Shard 0's CS always accepts then immediately closes — every attempt
	// (including every retry) fails with a retryable EOF, so the read never
	// succeeds and the full retry budget is exhausted.
	badLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var accepted atomic.Int64
	badDone := make(chan struct{})
	go func() {
		defer close(badDone)
		for {
			conn, acceptErr := badLn.Accept()
			if acceptErr != nil {
				return
			}
			accepted.Add(1)
			_ = conn.Close() // immediate EOF on the client's next read
		}
	}()
	t.Cleanup(func() {
		_ = badLn.Close()
		<-badDone
	})
	badAddr := badLn.Addr().(*net.TCPAddr)
	info.Servers[0] = ChunkServer{
		IP:   binary.BigEndian.Uint32(badAddr.IP.To4()),
		Port: uint16(badAddr.Port),
	}

	c := makeECClient(t, info)

	start := time.Now()
	_, err = c.readEC4At(1, info, 0, 0, block)
	elapsed := time.Since(start)

	require.Error(t, err, "a CS that never succeeds must eventually surface an error, not hang forever")
	assert.Lessf(t, elapsed, 5*time.Second,
		"total retry+backoff budget must be bounded — took %v, expected well under Windows' Open() re-issue delay (CA6)", elapsed)
	assert.GreaterOrEqualf(t, accepted.Load(), int64(2),
		"at least one retry (2+ fresh dials) must have been attempted before giving up")
}

// TestRetry_NonRetryableFailsFast verifies that a non-retryable error (CRC
// mismatch) fails immediately, without consuming the retry/backoff budget
// (CA7).
func TestRetry_NonRetryableFailsFast(t *testing.T) {
	const block = uint32(65536)
	const logicalID = uint64(0xC2C001)

	shardData := [4][]byte{
		makeShardBytes(0, int(block)),
		makeShardBytes(1, int(block)),
		makeShardBytes(2, int(block)),
		makeShardBytes(3, int(block)),
	}
	info, _ := startFourCSServers(t, logicalID, shardData)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var accepted atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			accepted.Add(1)
			go func(c net.Conn) {
				defer c.Close()
				cmd, payload, readErr := ReadFrame(c)
				if readErr != nil || cmd != CltocsFuseRead || len(payload) < 8 {
					return
				}
				gotChunkID, _, _ := ReadUint64(payload, 0)
				block := []byte("bad-crc-shard-data")
				badCRC := crc32.ChecksumIEEE(block) ^ 0xDEADBEEF
				var resp []byte
				resp = PutUint64(resp, gotChunkID)
				resp = PutUint16(resp, 0)
				resp = PutUint16(resp, 0)
				resp = PutUint32(resp, uint32(len(block)))
				resp = PutUint32(resp, badCRC)
				resp = append(resp, block...)
				_ = WriteFrame(c, CstoclFuseReadData, resp)
			}(conn)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	addr := ln.Addr().(*net.TCPAddr)
	info.Servers[0] = ChunkServer{
		IP:   binary.BigEndian.Uint32(addr.IP.To4()),
		Port: uint16(addr.Port),
	}

	c := makeECClient(t, info)

	start := time.Now()
	_, err = c.readEC4At(1, info, 0, 0, block)
	elapsed := time.Since(start)

	require.Error(t, err, "a CRC mismatch must fail the read, never succeed")
	assert.Contains(t, err.Error(), "CRC mismatch", "error must identify the CRC mismatch")
	assert.Equal(t, int64(1), accepted.Load(),
		"a CRC mismatch is not retryable — exactly one connection must be attempted, no retry dial")
	assert.Lessf(t, elapsed, 500*time.Millisecond,
		"a non-retryable error must fail fast, not wait through the backoff budget")
}

// TestCSPool_MaxIdleAge verifies that a pooled connection older than
// maxIdleConnAge is closed and replaced by a fresh dial rather than being
// handed back to a caller (CA8, D5).
//
// maxIdleConnAge is a 60s production constant — far too long to sleep through
// in a unit test — so this test injects an already-aged agedConn directly
// into pool.idle (white-box: agedConn/putAt/idle/mu are package-internal,
// same pattern as the pre-existing TestCSPool_FullPool_ClosesExtra) instead
// of waiting out the real duration.
func TestCSPool_MaxIdleAge(t *testing.T) {
	srv := newFakeCSServer()
	ip, port := srv.Start()
	defer srv.Stop()

	pool := newCSPool()
	defer pool.CloseAll()

	// Dial once, then put it back pre-aged past maxIdleConnAge.
	conn1, err := pool.Get(ip, port)
	require.NoError(t, err)

	key := csAddr(ip, port)
	pool.mu.Lock()
	pool.idle[key] = append(pool.idle[key], &agedConn{
		Conn:  conn1,
		putAt: time.Now().Add(-maxIdleConnAge - time.Second),
	})
	pool.mu.Unlock()

	// Get() must NOT hand back the aged connection — it must be closed and
	// replaced by a fresh dial.
	conn2, err := pool.Get(ip, port)
	require.NoError(t, err, "Get must dial fresh when the only idle connection has aged out")
	defer conn2.Close()

	assert.NotSame(t, conn1, conn2, "an aged-out connection must never be served to a caller")

	// The aged connection must have been closed by the pool (not leaked).
	_, writeErr := conn1.Write([]byte("x"))
	assert.Error(t, writeErr, "the aged-out connection must be closed by the pool, not left dangling")

	require.Eventually(t, func() bool {
		return srv.connCount.Load() == int64(2)
	}, 200*time.Millisecond, 5*time.Millisecond,
		"exactly 2 TCP connections must have been dialled: the original + the fresh replacement after aging out")
}

// TestReadChunk_InactivityTimeout verifies the model mandated by CA9 /
// invariant 9 (docs/diagrams/moosefs-ec4-read-statemachine.md §2.3, plan
// révision 2 task 27): ReadChunk must bound wait time by *inactivity*
// between frames, not by a fixed duration for the whole call. This is the
// single biggest risk flagged in the plan's risk table — a global-deadline
// design would abort a legitimately slow but progressing read exactly like
// #160's own root cause (a keepalive arriving after ~512 successful blocks,
// several seconds into the read). Reference model: the official MooseFS
// client's CHUNKSERVER_ACTIVITY_TIMEOUT, re-armed on every received frame
// (readdata.c:78, :1395), not a call-wide timer.
//
// csInactivityTimeout (same package, white-box) is the real bound applied to
// the *gap since the last received frame*, re-armed before every ReadFrame —
// see ReadChunk's SetReadDeadline call. Both sub-tests derive their timing
// from that real constant instead of a guessed duration, so they stay
// correct — neither flaky nor artificially slow — if it is retuned.
func TestReadChunk_InactivityTimeout(t *testing.T) {
	t.Run("progressing_read_not_interrupted", func(t *testing.T) {
		const chunkID = uint64(20020)
		content := []byte("inactivity-timeout-progress-test-data")

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			defer conn.Close()

			cmd, payload, readErr := ReadFrame(conn)
			if readErr != nil || cmd != CltocsFuseRead || len(payload) < 20 {
				return
			}
			gotChunkID, _, _ := ReadUint64(payload, 0)

			// Drip-feed NOP keepalives spaced well inside a single inactivity
			// window, but spanning MORE than csInactivityTimeout in total —
			// a legitimate slow-but-progressing read that must never be
			// killed by a call-wide timer.
			interval := csInactivityTimeout / 2
			const steps = 4 // 4 × (timeout/2) = 2 × timeout in total
			for i := 0; i < steps; i++ {
				time.Sleep(interval)
				if writeErr := WriteFrame(conn, ANTOAN_NOP, nil); writeErr != nil {
					return
				}
			}

			checksum := crc32.ChecksumIEEE(content)
			var dataResp []byte
			dataResp = PutUint64(dataResp, gotChunkID)
			dataResp = PutUint16(dataResp, 0)
			dataResp = PutUint16(dataResp, 0)
			dataResp = PutUint32(dataResp, uint32(len(content)))
			dataResp = PutUint32(dataResp, checksum)
			dataResp = append(dataResp, content...)
			if writeErr := WriteFrame(conn, CstoclFuseReadData, dataResp); writeErr != nil {
				return
			}
			var statusResp []byte
			statusResp = PutUint64(statusResp, gotChunkID)
			statusResp = PutUint8(statusResp, StatusOK)
			_ = WriteFrame(conn, CstoclFuseReadStatus, statusResp)
		}()
		t.Cleanup(func() {
			_ = ln.Close()
			<-done
		})

		addr := ln.Addr().(*net.TCPAddr)
		ip := binary.BigEndian.Uint32(addr.IP.To4())
		port := uint16(addr.Port)

		conn, err := DialCS(ip, port)
		require.NoError(t, err)
		defer conn.Close()

		got, err := ReadChunk(conn, chunkID, 1, 0, uint32(len(content)))
		require.NoError(t, err, "a read that keeps progressing (NOP keepalives spanning more than csInactivityTimeout in total, each well inside one inactivity window) must NOT be interrupted")
		assert.Equal(t, content, got, "data must be intact")
	})

	t.Run("silent_connection_interrupted", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		stopServer := make(chan struct{})
		serverDone := make(chan struct{})
		go func() {
			defer close(serverDone)
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			defer conn.Close()
			_, _, _ = ReadFrame(conn) // consume CLTOCS_READ, then go completely silent
			<-stopServer              // stay mute until the test tells us to stop
		}()
		t.Cleanup(func() {
			close(stopServer)
			_ = ln.Close()
			<-serverDone
		})

		addr := ln.Addr().(*net.TCPAddr)
		ip := binary.BigEndian.Uint32(addr.IP.To4())
		port := uint16(addr.Port)

		conn, err := DialCS(ip, port)
		require.NoError(t, err)
		defer conn.Close()

		resultCh := make(chan error, 1)
		go func() {
			_, readErr := ReadChunk(conn, 42, 1, 0, 64)
			resultCh <- readErr
		}()

		safetyNet := csInactivityTimeout + 10*time.Second
		select {
		case readErr := <-resultCh:
			require.Error(t, readErr, "a connection with zero activity (no DATA, no NOP) must produce a bounded error, not hang")
		case <-time.After(safetyNet):
			t.Fatalf("ReadChunk did not return within %v against a fully silent CS — no inactivity timeout enforced (CA9)", safetyNet)
		}
	})
}
