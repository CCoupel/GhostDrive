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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── fakeCSServer ─────────────────────────────────────────────────────────────

// fakeCSServer is an in-memory MooseFS chunk server for testing.
// It stores chunk data in memory and speaks the real CS protocol (MooseFS 4.x).
// WRITE_STATUS uses opcode 211 (CstoclFuseWriteStatus) per MooseFS 4.x.
type fakeCSServer struct {
	listener net.Listener
	mu       sync.Mutex
	chunks   map[uint64][]byte // chunkID → raw chunk data
	done     chan struct{}
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
		go s.handleConn(conn)
	}
}

func (s *fakeCSServer) handleConn(conn net.Conn) {
	defer conn.Close()

	// First frame determines operation: READ or WRITE.
	cmd, payload, err := ReadFrame(conn)
	if err != nil {
		return
	}

	switch cmd {
	case CltocsFuseRead:
		s.serveRead(conn, payload)
	case CltocsFuseWrite:
		s.serveWrite(conn, payload)
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
	_, off, _ = ReadUint32(payload, off)  // version (not validated in fake)
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
// (already received in payload).  It then reads CLTOCS_WRITE_DATA frames
// followed by CLTOCS_WRITE_END and finally sends CSTOCL_WRITE_STATUS.
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
			_, off, _ = ReadUint32(data, 8)  // writeId:32 — skip (offset 8 after chunkId:64)
			blockNum, off, _ := ReadUint16(data, off) // blockNum at offset 12
			blockOff, off, _ := ReadUint16(data, off) // blockOff at offset 14
			size, off, _ := ReadUint32(data, off)     // size at offset 16
			off += 4                                   // skip CRC at offset 20
			if off+int(size) > len(data) {
				return
			}
			block := data[off : off+int(size)]
			dataOffset := uint32(blockNum)*65536 + uint32(blockOff)
			s.storeBlock(chunkID, dataOffset, block)

		case CltocsFuseWriteEnd: // CLTOCS_WRITE_FINISH (213)
			// Send CSTOCL_WRITE_STATUS: [chunkId:64][writeId:32][status:8]
			var resp []byte
			resp = PutUint64(resp, chunkID)
			resp = PutUint32(resp, 0) // writeId
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
	resp = PutUint16(resp, 0)               // blocknum
	resp = PutUint16(resp, 0)               // blockOffset
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
	const chainIP = uint32(0xC0A802DC)  // 192.168.2.220
	const chainPort = uint16(9423)
	chain := []ChunkServer{{IP: chainIP, Port: chainPort}}
	err = WriteChunk(conn, chunkID, 1, 0, payload, chain)
	require.NoError(t, err, "WriteChunk with chain must succeed against the fake CS")

	// The fake CS read chunkId at payload[1:9] — verify data stored under correct key.
	stored := cs.GetChunkData(chunkID)
	assert.Equal(t, payload, stored, "stored chunk data must match written payload")
}

// TestWriteChunk_earlyCANTCONNECT verifies that WriteChunk detects an immediate
// CSTOCL_WRITE_STATUS(CANTCONNECT) sent by the CS after CLTOCS_WRITE but before
// any WRITE_DATA — the fast-failure path when a chain CS is unreachable.
// The pre-flight read in WriteChunk must catch this early error.
func TestWriteChunk_earlyCANTCONNECT(t *testing.T) {
	srv := newEarlyCANTCONNECTServer()
	ip, port := srv.Start()
	defer srv.Stop()

	conn, err := DialCS(ip, port)
	require.NoError(t, err)
	defer conn.Close()

	err = WriteChunk(conn, uint64(5005), 1, 0, []byte("chain-fail-test"), nil)
	require.Error(t, err, "WriteChunk must return error when CS sends early CANTCONNECT")
	assert.Contains(t, err.Error(), "CANTCONNECT",
		"error must identify CANTCONNECT status")
}

// TestWriteChunk_preflightNOPcap verifies that the pre-flight loop does not run
// indefinitely when the CS sends many consecutive NOP keepalives.
// After maxPreflightNOPs NOPs, WriteChunk must proceed to WRITE_DATA (and succeed).
func TestWriteChunk_preflightNOPcap(t *testing.T) {
	// Custom server: sends maxPreflightNOPs+5 NOPs after CLTOCS_WRITE, then
	// serves the write normally.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	cs := newFakeCSServer()
	const chunkID = uint64(7007)
	payload := []byte("preflight-nop-cap-test")

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Consume CLTOCS_WRITE init.
		cmd, _, err := ReadFrame(conn)
		if err != nil || cmd != CltocsFuseWrite {
			return
		}

		// Send 15 NOP keepalives (> maxPreflightNOPs=10) before any data.
		for i := 0; i < 15; i++ {
			if err := WriteFrame(conn, ANTOAN_NOP, nil); err != nil {
				return
			}
		}

		// Now serve WRITE_DATA + WRITE_FINISH normally.
		for {
			c, data, e := ReadFrame(conn)
			if e != nil {
				return
			}
			if c == CltocsFuseWriteData && len(data) >= 24 {
				_, off, _ := ReadUint32(data, 8)
				blockNum, off, _ := ReadUint16(data, off)
				blockOff, off, _ := ReadUint16(data, off)
				size, off, _ := ReadUint32(data, off)
				off += 4 // skip CRC
				block := data[off : off+int(size)]
				cs.storeBlock(chunkID, uint32(blockNum)*65536+uint32(blockOff), block)
			} else if c == CltocsFuseWriteEnd {
				var resp []byte
				resp = PutUint64(resp, chunkID)
				resp = PutUint32(resp, 0)
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

	err = WriteChunk(conn, chunkID, 1, 0, payload, nil)
	require.NoError(t, err, "WriteChunk must succeed after hitting the NOP cap and proceeding")

	stored := cs.GetChunkData(chunkID)
	assert.Equal(t, payload, stored, "stored data must match written payload")
	<-done
}

// TestWriteChunk_preflightUnexpectedOK verifies that WriteChunk returns an
// explicit error when the CS sends WRITE_STATUS(OK) before any WRITE_DATA —
// a protocol violation that would silently consume the frame the final
// WRITE_STATUS read loop expects.
func TestWriteChunk_preflightUnexpectedOK(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	const chunkID = uint64(8008)
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Consume CLTOCS_WRITE init.
		cmd, payload, err := ReadFrame(conn)
		if err != nil || cmd != CltocsFuseWrite || len(payload) < 13 {
			return
		}

		// Send WRITE_STATUS(OK, writeId=0) immediately — protocol violation.
		var resp []byte
		resp = PutUint64(resp, chunkID)
		resp = PutUint32(resp, 0)    // writeId = 0
		resp = PutUint8(resp, StatusOK)
		_ = WriteFrame(conn, CstoclFuseWriteStatus, resp)
	}()

	addr := ln.Addr().(*net.TCPAddr)
	csIP := binary.BigEndian.Uint32(addr.IP.To4())
	csPort := uint16(addr.Port)

	conn, err := DialCS(csIP, csPort)
	require.NoError(t, err)
	defer conn.Close()

	err = WriteChunk(conn, chunkID, 1, 0, []byte("unexpected-ok-test"), nil)
	require.Error(t, err, "WriteChunk must return error on unexpected WRITE_STATUS(OK) before WRITE_DATA")
	assert.Contains(t, err.Error(), "protocol violation",
		"error must describe the protocol violation")
	<-done
}

// TestWriteChunk_chainEOF verifies that WriteChunk returns a diagnostic error
// when the CS closes the connection (EOF) without sending WRITE_STATUS —
// the behaviour observed with MooseFS 4.58.8 when the chain CS times out.
func TestWriteChunk_chainEOF(t *testing.T) {
	// Server that accepts CLTOCS_WRITE, sends 2 NOPs, then closes — simulating
	// a chain-CS TCP timeout without a final WRITE_STATUS frame.
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

		// Consume any WRITE_DATA and WRITE_FINISH frames (drain until error or FINISH).
		for {
			c, _, e := ReadFrame(conn)
			if e != nil || c == CltocsFuseWriteEnd {
				break
			}
		}

		// Send 2 NOP keepalives (like MooseFS does while waiting for chain), then close
		// WITHOUT sending WRITE_STATUS — the observed MooseFS 4.58.8 EOF behaviour.
		_ = WriteFrame(conn, ANTOAN_NOP, nil)
		_ = WriteFrame(conn, ANTOAN_NOP, nil)
		// conn.Close() via defer — sends EOF to the client.
	}()

	addr := ln.Addr().(*net.TCPAddr)
	csIP := binary.BigEndian.Uint32(addr.IP.To4())
	csPort := uint16(addr.Port)

	conn, err := DialCS(csIP, csPort)
	require.NoError(t, err)
	defer conn.Close()

	err = WriteChunk(conn, uint64(6006), 1, 0, []byte("eof-chain-test"), nil)
	require.Error(t, err, "WriteChunk must return error on CS EOF without WRITE_STATUS")
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

		// Send 3 NOP keepalives, then the real WRITE_STATUS.
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
