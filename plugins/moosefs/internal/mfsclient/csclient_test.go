// Package mfsclient — tests for the chunk server client (csclient.go).
//
// fakeCSServer implements an in-memory MooseFS chunk server that speaks the
// real CS protocol (opcodes 200-213).  It is also used by client_test.go to
// back the fakeMFSServer when testing the high-level Read/Write methods.
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
// It stores chunk data in memory and speaks the real CS protocol.
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
	if len(payload) < 13 {
		return
	}
	chunkID, _, _ := ReadUint64(payload, 0)
	// version and N are parsed but not used in the fake

	for {
		cmd, data, err := ReadFrame(conn)
		if err != nil {
			return
		}

		switch cmd {
		case CltocsFuseWriteData:
			// [chunkId:64][blocknum:16][blockOffset:16][size:32][crc:32][data:size]
			if len(data) < 20 {
				return
			}
			blockNum, off, _ := ReadUint16(data, 8) // after chunkId
			blockOff, off, _ := ReadUint16(data, off)
			size, off, _ := ReadUint32(data, off)
			off += 4 // skip CRC
			if off+int(size) > len(data) {
				return
			}
			block := data[off : off+int(size)]
			dataOffset := uint32(blockNum)*65536 + uint32(blockOff)
			s.storeBlock(chunkID, dataOffset, block)

		case CltocsFuseWriteEnd:
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

	err = WriteChunk(conn, chunkID, 1, 0, payload)
	require.NoError(t, err, "WriteChunk must succeed against the fake CS")

	// Verify the data was stored at offset 0.
	stored := cs.GetChunkData(chunkID)
	assert.Equal(t, payload, stored, "stored chunk data must match written payload")
}
