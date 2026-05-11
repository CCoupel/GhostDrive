// Package mfsclient — chunk server I/O (Phase 2).
//
// This file implements the low-level TCP protocol between GhostDrive and a
// MooseFS chunk server (default port 9420).  It is intentionally separate
// from client.go (master protocol) to keep the two protocol layers distinct.
//
// # Read protocol
//
//	Client → CS  CLTOCS_READ (200):  [chunkId:64][version:32][offset:32][size:32]
//	CS → Client  CSTOCL_READ_DATA (202), zero or more times:
//	             [chunkId:64][blocknum:16][blockOffset:16][size:32][crc:32][data:size]
//	CS → Client  CSTOCL_READ_STATUS (201):  [chunkId:64][status:8]
//
// # Write protocol  (MooseFS 4.x — confirmed against writedata.c source)
//
//	Client → CS  CLTOCS_WRITE (210):       [protocolid:8=1][chunkId:64][version:32][N*(ip:32+port:16)]
//	                                        N is implicit: (payloadLen−13)/6  (protocolid byte counts)
//	                                        N=0: direct write, no replication chain (recommended for
//	                                              FUSE clients — master replicates async post-commit)
//	                                        N≥1: CS must forward to listed peers for replication
//	                                              (synchronous; unreachable peers cause CANTCONNECT)
//	CS → Client  CSTOCL_WRITE_STATUS (211):[chunkId:64][writeId:32=0][status:8]
//	                                        MANDATORY write-init ACK sent by CS once the replication
//	                                        chain is established (waitforstatus=1 in writedata.c).
//	                                        status=OK: chain ready, client may send WRITE_DATA.
//	                                        status=CANTCONNECT: chain peer unreachable; abort.
//	                                        CS may send ANTOAN_NOP keepalives while connecting peers.
//	Client → CS  CLTOCS_WRITE_DATA (212):  [chunkId:64][writeId:32][blocknum:16][blockOffset:16][size:32][crc:32][data:size]
//	                                        blocknum    = (chunkOffset + written) / 65536
//	                                        blockOffset = (chunkOffset + written) % 65536
//	                                        writeId     = monotonic frame counter (1, 2, …)
//	Client → CS  CLTOCS_WRITE_FINISH (213):[chunkId:64][version:32]
//	CS → Client  CSTOCL_WRITE_STATUS (211):[chunkId:64][writeId:32][status:8]
//	                                        CS echoes writeId from the last WRITE_DATA frame.
//	                                        CS may send ANTOAN_NOP keepalives before this frame.
package mfsclient

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net"

	"github.com/CCoupel/GhostDrive/internal/logger"
)

// DialCS opens a TCP connection to the MooseFS chunk server at the given IP
// address (uint32 big-endian network byte order) and port.
// Returns a raw net.Conn ready for ReadChunk or WriteChunk.
func DialCS(ip uint32, port uint16) (net.Conn, error) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, ip)
	addr := fmt.Sprintf("%d.%d.%d.%d:%d", b[0], b[1], b[2], b[3], port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("csclient: dial %s: %w", addr, err)
	}
	return conn, nil
}

// ReadChunk reads size bytes starting at offset within chunk chunkID/version
// from the chunk server connection cs.
//
// Internally it sends one CLTOCS_READ frame and collects all CSTOCL_READ_DATA
// frames until CSTOCL_READ_STATUS is received.  Returns the concatenated data.
// An empty (nil) result indicates that the requested offset is past EOF for
// that chunk.
func ReadChunk(cs net.Conn, chunkID uint64, version uint32, offset uint32, size uint32) ([]byte, error) {
	// Build and send CLTOCS_READ request.
	var payload []byte
	payload = PutUint64(payload, chunkID)
	payload = PutUint32(payload, version)
	payload = PutUint32(payload, offset)
	payload = PutUint32(payload, size)

	if err := WriteFrame(cs, CltocsFuseRead, payload); err != nil {
		return nil, fmt.Errorf("csclient: ReadChunk %d: send: %w", chunkID, err)
	}

	// Collect READ_DATA frames until READ_STATUS.
	var result []byte
	for {
		cmd, data, err := ReadFrame(cs)
		if err != nil {
			return nil, fmt.Errorf("csclient: ReadChunk %d: recv: %w", chunkID, err)
		}

		switch cmd {
		case CstoclFuseReadData:
			// [chunkId:64][blocknum:16][blockOffset:16][size:32][crc:32][data:size]
			// Header is 8+2+2+4+4 = 20 bytes; data starts at offset 20.
			const hdrLen = 20
			if len(data) < hdrLen {
				return nil, fmt.Errorf("csclient: ReadChunk %d: READ_DATA too short (%d bytes)", chunkID, len(data))
			}
			blocknum, _, _ := ReadUint16(data, 8) // after chunkId(8) — for error reporting
			dataSize, _, err := ReadUint32(data, 12) // after chunkId(8)+blocknum(2)+blockOffset(2)
			if err != nil {
				return nil, fmt.Errorf("csclient: ReadChunk %d: READ_DATA size field: %w", chunkID, err)
			}
			frameCRC, _, err := ReadUint32(data, 16) // CRC field after size
			if err != nil {
				return nil, fmt.Errorf("csclient: ReadChunk %d: READ_DATA crc field: %w", chunkID, err)
			}
			if hdrLen+int(dataSize) > len(data) {
				return nil, fmt.Errorf("csclient: ReadChunk %d: READ_DATA payload truncated (hdr=%d size=%d have=%d)",
					chunkID, hdrLen, dataSize, len(data))
			}
			block := data[hdrLen : hdrLen+int(dataSize)]
			gotCRC := crc32.ChecksumIEEE(block)
			if gotCRC != frameCRC {
				return nil, fmt.Errorf("mfsclient: csclient: CRC mismatch chunk %d block %d: got %08x want %08x",
					chunkID, blocknum, gotCRC, frameCRC)
			}
			result = append(result, block...)

		case CstoclFuseReadStatus:
			// [chunkId:64][status:8]
			if len(data) < 9 {
				return nil, fmt.Errorf("csclient: ReadChunk %d: READ_STATUS too short (%d bytes)", chunkID, len(data))
			}
			status := data[8]
			if status != StatusOK {
				return nil, fmt.Errorf("csclient: ReadChunk %d: server status 0x%02x", chunkID, status)
			}
			return result, nil

		default:
			return nil, fmt.Errorf("csclient: ReadChunk %d: unexpected response cmd %d", chunkID, cmd)
		}
	}
}

// WriteChunk writes data to chunk chunkID/version at the given offset within
// the chunk via the chunk server connection cs.
//
// chain lists the additional chunk servers that cs must forward data to for
// synchronous replication.  Pass nil (or empty) for direct write: cs stores
// the data locally and the MooseFS master schedules async replication to reach
// the configured goal after WRITE_CHUNK_END.  Passing a non-nil chain causes cs
// to connect to each listed peer during the write-init ACK phase; if any peer
// is unreachable the CS returns CANTCONNECT immediately.
//
// CLTOCS_WRITE payload format (MooseFS >= 1.7.32 / all 4.x):
//
//	[protocolid:8=1][chunkid:64][version:32][(N-1)*(ip:32+port:16)]
//	payloadLen = 13 + len(chain)*6
//
// protocolid=1 is mandatory: if absent (or wrong), the CS reads chunkid[0] as
// protocolid, shifts all subsequent fields by one byte and misparses the chain
// IP/port → CANTCONNECT.
//
// Protocol flow (confirmed against MooseFS writedata.c):
//  1. Client sends CLTOCS_WRITE.
//  2. CS sends mandatory write-init ACK: WRITE_STATUS(writeid=0, OK|CANTCONNECT).
//  3. Client sends CLTOCS_WRITE_DATA frames (one per 65536-byte block).
//  4. Client sends CLTOCS_WRITE_END.
//  5. CS sends final WRITE_STATUS echoing the last writeId.
func WriteChunk(cs net.Conn, chunkID uint64, version uint32, offset uint32, data []byte, chain []ChunkServer) error {
	// 1. Send CLTOCS_WRITE init frame.
	// protocolid:8=1 must be the first byte (MooseFS >= 1.7.32 requirement).
	// The CS will respond with a mandatory write-init ACK (step 2) before the client
	// may send WRITE_DATA frames (step 3).
	var initPayload []byte
	initPayload = PutUint8(initPayload, 1) // protocolid:8 = 1
	initPayload = PutUint64(initPayload, chunkID)
	initPayload = PutUint32(initPayload, version)
	for _, srv := range chain {
		initPayload = PutUint32(initPayload, srv.IP)
		initPayload = PutUint16(initPayload, srv.Port)
	}

	if err := WriteFrame(cs, CltocsFuseWrite, initPayload); err != nil {
		return fmt.Errorf("csclient: WriteChunk %d: send init: %w", chunkID, err)
	}

	// 2. Read mandatory write-init ACK from the CS.
	// Per writedata.c (MooseFS source, waitforstatus=1), the CS always sends
	// CSTOCL_WRITE_STATUS(writeid=0, OK) before the client may send WRITE_DATA:
	//   chain=nil  → ACK is immediate (CS writes locally, no peer to connect).
	//   chain≠nil  → ACK arrives after CS connects all listed peers; ANTOAN_NOP
	//                keepalives may arrive while connections are in progress;
	//                unreachable peers produce WRITE_STATUS(writeid=0, CANTCONNECT).
	for {
		ackCmd, ackResp, err := ReadFrame(cs)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("csclient: WriteChunk %d: CS closed during write-init ACK"+
					" — chain CS unreachable or CS rejected write"+
					" (check CS-to-CS connectivity and MooseFS master logs): %w", chunkID, err)
			}
			return fmt.Errorf("csclient: WriteChunk %d: read write-init ACK: %w", chunkID, err)
		}
		if ackCmd == ANTOAN_NOP {
			continue // keepalive while CS is connecting chain peers (chain≠nil only)
		}
		if ackCmd != CstoclFuseWriteStatus {
			return fmt.Errorf("csclient: WriteChunk %d: expected WRITE_STATUS ACK (cmd=%d), got cmd=%d",
				chunkID, CstoclFuseWriteStatus, ackCmd)
		}
		if len(ackResp) < 13 {
			return fmt.Errorf("csclient: WriteChunk %d: WRITE_STATUS ACK too short (%d bytes)",
				chunkID, len(ackResp))
		}
		ackStatus := ackResp[12]
		if ackStatus != StatusOK {
			return fmt.Errorf("csclient: WriteChunk %d: write-init failed: server status 0x%02x (%s)",
				chunkID, ackStatus, CSStatusName(ackStatus))
		}
		break // ACK OK — CS ready, proceed to WRITE_DATA
	}

	// 3. Send CLTOCS_WRITE_DATA frames, one per 65536-byte block.
	// Each frame includes a writeId:32 monotonic counter starting at 1.
	// The CS echoes the last writeId in its CSTOCL_WRITE_STATUS response.
	const blockSize = 65536
	total := uint32(len(data))
	written := uint32(0)
	var writeID uint32 // incremented per frame; echoed back in WRITE_STATUS

	for written < total {
		pos := offset + written          // absolute position within the chunk
		blockNum := uint16(pos / blockSize)
		blockOff := uint16(pos % blockSize)

		// Fill the rest of the current block, capped by remaining data.
		canFill := blockSize - uint32(blockOff)
		end := written + canFill
		if end > total {
			end = total
		}
		block := data[written:end]
		checksum := crc32.ChecksumIEEE(block)

		writeID++
		var framePayload []byte
		framePayload = PutUint64(framePayload, chunkID)
		framePayload = PutUint32(framePayload, writeID) // writeId:32 — required by MooseFS 4.x
		framePayload = PutUint16(framePayload, blockNum)
		framePayload = PutUint16(framePayload, blockOff)
		framePayload = PutUint32(framePayload, uint32(len(block)))
		framePayload = PutUint32(framePayload, checksum)
		framePayload = append(framePayload, block...)

		if err := WriteFrame(cs, CltocsFuseWriteData, framePayload); err != nil {
			return fmt.Errorf("csclient: WriteChunk %d: send data (block %d): %w", chunkID, blockNum, err)
		}
		written = end
	}

	// 4. Send CLTOCS_WRITE_END.
	var endPayload []byte
	endPayload = PutUint64(endPayload, chunkID)
	endPayload = PutUint32(endPayload, version)

	if err := WriteFrame(cs, CltocsFuseWriteEnd, endPayload); err != nil {
		return fmt.Errorf("csclient: WriteChunk %d: send end: %w", chunkID, err)
	}

	// 5. Read final CSTOCL_WRITE_STATUS: [chunkId:64][writeId:32][status:8] = 13 bytes.
	// MooseFS 4.x sends opcode 211 (CstoclFuseWriteStatus), confirmed 4.58.4.
	// The CS may send ANTOAN_NOP (cmd=0) keepalives before the real status frame;
	// loop until we receive a non-NOP frame.
	var cmd uint32
	var resp []byte
	for {
		var err error
		cmd, resp, err = ReadFrame(cs)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// CS closed the connection without sending final WRITE_STATUS.
				// Uncommon after the write-init ACK was received; may indicate
				// a CS crash or network failure during the write.
				return fmt.Errorf("csclient: WriteChunk %d: CS closed without final WRITE_STATUS"+
					" (check CS logs for crash or OOM): %w", chunkID, err)
			}
			return fmt.Errorf("csclient: WriteChunk %d: recv status: %w", chunkID, err)
		}
		logger.Debug("csclient: WriteChunk %d: recv cmd=%d resp_len=%d", chunkID, cmd, len(resp))
		if cmd == ANTOAN_NOP {
			continue // keepalive — wait for real status
		}
		break
	}
	if cmd != CstoclFuseWriteStatus {
		return fmt.Errorf("csclient: WriteChunk %d: expected WRITE_STATUS (opcode %d), got %d",
			chunkID, CstoclFuseWriteStatus, cmd)
	}
	if len(resp) < 13 {
		return fmt.Errorf("csclient: WriteChunk %d: WRITE_STATUS too short (%d bytes)", chunkID, len(resp))
	}
	status := resp[12]
	if status != StatusOK {
		return fmt.Errorf("csclient: WriteChunk %d: server write status 0x%02x (%s)",
			chunkID, status, CSStatusName(status))
	}
	return nil
}
