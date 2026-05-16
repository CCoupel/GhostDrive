// Package mfsclient implements a minimal MooseFS 4.x TCP client.
//
// # Protocol
//
// Every message (both directions) is a binary frame:
//
//	[cmd uint32 BE][payloadLen uint32 BE][payload bytes]
//
// Client sends a request frame, server replies with an answer frame.
//
// # Command / Answer codes
//
// The numeric values below are the real MooseFS protocol opcodes from the
// official source at github.com/moosefs/moosefs (matocl*.h / cltoma*.h).
//
// # Status codes (as returned in answer payloads)
//
// Real MooseFS 4.x values from mfsdef.h:
//
//	0x00 = STATUS_OK
//	0x01 = ERROR_EPERM
//	0x02 = ERROR_ENOTDIR
//	0x03 = ERROR_ENOENT  ← confirmed from real server (Lookup on missing segment)
//	0x04 = ERROR_EACCES
//	0x05 = ERROR_EEXIST
//	0x06 = ERROR_EINVAL
//	0x09 = ERROR_ENOTEMPTY
//	0xFF = STATUS_ERROR (generic)
package mfsclient

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// RootNodeID is the MooseFS root directory node identifier.
const RootNodeID uint32 = 1

// ─── Status codes ────────────────────────────────────────────────────────────

const (
	StatusOK        uint8 = 0
	StatusENOENT    uint8 = 3   // confirmed from real MooseFS server (was 1 — wrong)
	StatusEACCES    uint8 = 4
	StatusEEXIST    uint8 = 5   // MooseFS ERROR_EEXIST (was 6)
	StatusENOTEMPTY uint8 = 9   // MooseFS ERROR_ENOTEMPTY (was 11)
	StatusERROR     uint8 = 255
)

// StatusENOTEMPT is a deprecated alias kept for backward compatibility with
// existing test files that reference it.
//
// Deprecated: use StatusENOTEMPTY.
const StatusENOTEMPT = StatusENOTEMPTY

// ─── Real MooseFS protocol opcodes ───────────────────────────────────────────

const (
	ANTOAN_NOP             uint32 = 0
	CltomFuseRegister      uint32 = 400
	MatoclFuseRegister     uint32 = 401
	CltomFuseStatFS        uint32 = 402
	MatoclFuseStatFS       uint32 = 403
	CltomFuseLookup        uint32 = 406
	MatoclFuseLookup       uint32 = 407
	CltomFuseGetAttr       uint32 = 408
	MatoclFuseGetAttr      uint32 = 409
	CltomFuseMknod         uint32 = 416
	MatoclFuseMknod        uint32 = 417
	CltomFuseMkdir         uint32 = 418
	MatoclFuseMkdir        uint32 = 419
	CltomFuseUnlink        uint32 = 420
	MatoclFuseUnlink       uint32 = 421
	CltomFuseRmdir         uint32 = 422
	MatoclFuseRmdir        uint32 = 423
	CltomFuseRename        uint32 = 424
	MatoclFuseRename       uint32 = 425
	CltomFuseReadDir       uint32 = 428
	MatoclFuseReadDir      uint32 = 429
	CltomFuseReadChunk     uint32 = 432
	MatoclFuseReadChunk    uint32 = 433
	CltomFuseWriteChunk    uint32 = 434
	MatoclFuseWriteChunk   uint32 = 435
	CltomFuseWriteChunkEnd uint32 = 436
	MatoclFuseWriteChunkEnd uint32 = 437
)

// ─── Chunk server (CS) opcodes ────────────────────────────────────────────────
//
// These opcodes are used between the GhostDrive client and MooseFS chunk
// servers (default port 9420).  They are distinct from the master opcodes
// above and must NOT be sent to the master server.

const (
	CltocsFuseRead       uint32 = 200
	CstoclFuseReadStatus uint32 = 201
	CstoclFuseReadData   uint32 = 202

	// Write-path opcodes per MooseFS protocol (MFSCommunication.h):
	//
	//   210 CLTOCS_WRITE       client→CS  [chunkId:64][version:32][N*(ip:32+port:16)]
	//   211 CSTOCL_WRITE_STATUS  CS→client  [chunkId:64][writeId:32][status:8]
	//   212 CLTOCS_WRITE_DATA  client→CS  [chunkId:64][writeId:32][blockNum:16][offset:16][size:32][crc:32][data]
	//   213 CLTOCS_WRITE_FINISH client→CS [chunkId:64][version:32]
	//
	// Opcode 211 is shared between CSTOCL_WRITE_STATUS (CS→client direction) and is
	// unambiguous because the directions are opposite.
	// Confirmed against MooseFS 4.58.4.
	CltocsFuseWrite        uint32 = 210
	CstoclFuseWriteStatus  uint32 = 211 // CS→client write status (confirmed 4.58.4)
	CltocsFuseWriteData    uint32 = 212 // client→CS write data (includes writeId:32)
	CltocsFuseWriteEnd     uint32 = 213 // client→CS write finish (= CLTOCS_WRITE_FINISH)

	// CstoclFuseWriteStatusLegacy was a historically incorrect value used by older
	// GhostDrive builds that confused CSTOCL_WRITE_STATUS with CLTOCS_WRITE_FINISH.
	// It is no longer accepted as a valid write-status opcode.
	// Kept as a named constant only to document the prior bug.
	//
	// Deprecated: do not use in new code.
	CstoclFuseWriteStatusLegacy uint32 = 211 // same as CstoclFuseWriteStatus (was erroneously 213)
)

// ChunkSize is the MooseFS chunk size (64 MiB).
// All chunk index calculations use this constant: index = fileOffset / ChunkSize.
const ChunkSize uint64 = 64 * 1024 * 1024

// ─── Erasure-coding constants ─────────────────────────────────────────────────
//
// MooseFS Pro 4.x uses physical chunk IDs derived from logical chunk IDs for
// each EC shard.  The derivation formula (from hddspacemgr.c::hdd_int_split()):
//
//	physical[part] = logical + EC4ECIDStart + part × EC4ECIDStep
//
// DF0 (part=0): logical + 0x1000000000000000
// DF1 (part=1): logical + 0x1100000000000000
// DF2 (part=2): logical + 0x1200000000000000
// DF3 (part=3): logical + 0x1300000000000000
// CF0 (part=4): logical + 0x1400000000000000  (parity — not used in normal reads)
//
// EC8+2 uses EC8ECIDStart (reserved for issue #115).

const (
	// EC4ECIDStart is the base physical-ID offset for EC4+1 data shard DF0.
	EC4ECIDStart uint64 = 0x1000000000000000
	// EC4ECIDStep is the per-part increment applied to successive EC4 shards.
	EC4ECIDStep uint64 = 0x0100000000000000
	// EC8ECIDStart is the base offset for EC8+2 shards (reserved — issue #115).
	EC8ECIDStart uint64 = 0x2000000000000000
)

// ECPhysicalChunkID returns the physical chunk ID for shard partIdx of an
// EC4+1 logical chunk logicalID.
//
//	partIdx: 0=DF0, 1=DF1, 2=DF2, 3=DF3, 4=CF0 (parity)
//
// Source: MooseFS CE hddspacemgr.c::hdd_int_split() — identical in Pro.
func ECPhysicalChunkID(logicalID uint64, partIdx int) uint64 {
	return logicalID + EC4ECIDStart + uint64(partIdx)*EC4ECIDStep
}

// ─── Chunk-server status codes ───────────────────────────────────────────────
//
// These codes appear in CSTOCL_WRITE_STATUS and CSTOCL_READ_STATUS payloads.
// They are a superset of the master status codes (same base table, MFSCommunication.h).
// Values below are the MFS_ERROR_* constants confirmed from MooseFS 4.x source.

const (
	// StatusCSWRONGOFFSET is returned when the write offset falls outside the chunk.
	StatusCSWRONGOFFSET uint8 = 25 // MFS_ERROR_WRONGOFFSET (0x19)

	// StatusCSCANTCONNECT is returned when the chunk server cannot connect to the
	// next server in the write chain.  Also observed when the write protocol is
	// malformed (e.g., wrong opcodes or missing fields in WRITE_DATA frames).
	StatusCSCANTCONNECT uint8 = 26 // MFS_ERROR_CANTCONNECT (0x1a)

	// StatusCSWRONGCHUNKID is returned when the chunkId does not match any known chunk.
	StatusCSWRONGCHUNKID uint8 = 27 // MFS_ERROR_WRONGCHUNKID (0x1b)

	// StatusCSDISCONNECTED is returned when the chunk server lost a chain connection.
	StatusCSDISCONNECTED uint8 = 28 // MFS_ERROR_DISCONNECTED (0x1c)

	// StatusCSCRC is returned on CRC-32 mismatch in a WRITE_DATA or READ_DATA frame.
	StatusCSCRC uint8 = 29 // MFS_ERROR_CRC (0x1d)
)

// CSStatusName returns a human-readable name for a chunk-server status code.
// Used to improve diagnostic error messages.
func CSStatusName(s uint8) string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusENOENT:
		return "ENOENT"
	case StatusEACCES:
		return "EACCES"
	case StatusEEXIST:
		return "EEXIST"
	case StatusENOTEMPTY:
		return "ENOTEMPTY"
	case StatusCSWRONGOFFSET:
		return "WRONGOFFSET"
	case StatusCSCANTCONNECT:
		return "CANTCONNECT"
	case StatusCSWRONGCHUNKID:
		return "WRONGCHUNKID"
	case StatusCSDISCONNECTED:
		return "DISCONNECTED"
	case StatusCSCRC:
		return "CRC"
	case StatusERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ─── Stub opcodes (test-only, Phase 1) ───────────────────────────────────────
//
// These opcodes are NOT part of the real MooseFS protocol.
// They are kept only for backward compatibility with existing fake-server
// handlers in *_test.go files.  The production client no longer emits them.

const (
	CmdFUSEREAD  uint32 = 506
	CmdFUSEWRITE uint32 = 507
	AnsFUSEREAD  uint32 = 606
	AnsFUSEWRITE uint32 = 607
)

// ─── Register blob and codes ──────────────────────────────────────────────────

// FuseRegisterBlobACL is the 64-byte authentication blob sent during REGISTER.
const FuseRegisterBlobACL = "DjI1GAQDULI5d2YjA26ypc3ovkhjvhciTQVx3CS4nYgtBoUcsljiVpsErJENHaw0"

const (
	RegisterNewSession   uint8 = 2
	RegisterReconnect    uint8 = 3
	RegisterCloseSession uint8 = 6
)

// ─── Shared data types ────────────────────────────────────────────────────────

// DirEntry is a single directory listing entry.
// Size and MTime are populated from the 35-byte attrs block embedded in
// each MATOCL_FUSE_READDIR entry by MooseFS 4.x — no extra GetAttr call needed.
type DirEntry struct {
	NodeID uint32
	Name   string
	IsDir  bool
	Size   uint64
	MTime  uint32
}

// Attr holds the metadata of a file or directory node.
// The Mode field uses the real MooseFS wire encoding:
//   - bits 15-12: node type (1=file, 2=dir, ...)
//   - bits 11-0:  POSIX permissions
type Attr struct {
	NodeID  uint32
	Flags   uint8
	Mode    uint16 // raw wire mode: bits 12-15 = type, bits 0-11 = permissions
	UID     uint32
	GID     uint32
	ATime   uint32
	MTime   uint32
	CTime   uint32
	NLink   uint32
	Size    uint64
	// ModTime is a convenience alias for MTime (unix seconds).
	// Populated by ParseAttrs.
	ModTime int64
}

// IsDir returns true when the node is a directory (type bits == 2).
func (a *Attr) IsDir() bool { return (a.Mode >> 12) == 2 }

// ChunkServer describes a MooseFS chunk server location.
type ChunkServer struct {
	IP      uint32
	Port    uint16
	Version uint32
}

// ChunkInfo holds the chunk metadata returned by READ_CHUNK / WRITE_CHUNK.
type ChunkInfo struct {
	ChunkID  uint64
	Version  uint32
	Length   uint64        // current file length BEFORE this write (from WRITE_CHUNK response)
	Servers  []ChunkServer
	LockID   uint32        // optional lock token returned by the master in WRITE_CHUNK responses.
	                       // Present in some MooseFS deployments; NOT included in WRITE_CHUNK_END
	                       // by this client (confirmed against MooseFS 4.58.4 — tests pass on real cluster).
	                       // Retained for diagnostics and future protocol compatibility.
	// ECParts is non-zero when this chunk uses erasure coding (proto=3).
	// 4 = EC4+1 (4 data shards + 1 parity — issue #114).
	// 8 = EC8+2 (8 data shards + 2 parity — reserved for issue #115).
	// 0 = normal replicated chunk (proto=0/1/2).
	// Servers contains only the data shards (DF0..DFn-1); parity (CF0) is
	// not included by the master in normal read responses.
	ECParts  int
}

// ─── Frame I/O helpers ────────────────────────────────────────────────────────

// WriteFrame encodes and sends a protocol frame on conn.
// Frame format: [cmd uint32 BE][payloadLen uint32 BE][payload bytes].
func WriteFrame(conn net.Conn, cmd uint32, payload []byte) error {
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[0:4], cmd)
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(payload)))

	if _, err := conn.Write(hdr); err != nil {
		return fmt.Errorf("mfsclient: write frame header (cmd %d): %w", cmd, err)
	}
	if len(payload) > 0 {
		if _, err := conn.Write(payload); err != nil {
			return fmt.Errorf("mfsclient: write frame payload (cmd %d): %w", cmd, err)
		}
	}
	return nil
}

// maxFramePayload is the maximum accepted payload size (128 MiB).
// Frames larger than this are rejected to prevent server-induced OOM.
const maxFramePayload = 128 << 20

// ReadFrame reads the next frame from conn and returns its command code and
// payload.  Returns an error on I/O failure, malformed header, or oversized
// payload (> maxFramePayload).
func ReadFrame(conn net.Conn) (cmd uint32, payload []byte, err error) {
	hdr := make([]byte, 8)
	if _, err = io.ReadFull(conn, hdr); err != nil {
		return 0, nil, fmt.Errorf("mfsclient: read frame header: %w", err)
	}

	cmd = binary.BigEndian.Uint32(hdr[0:4])
	length := binary.BigEndian.Uint32(hdr[4:8])

	if length > maxFramePayload {
		return 0, nil, fmt.Errorf("mfsclient: read frame: payload too large (%d bytes)", length)
	}

	if length > 0 {
		payload = make([]byte, length)
		if _, err = io.ReadFull(conn, payload); err != nil {
			return 0, nil, fmt.Errorf("mfsclient: read frame payload (cmd %d): %w", cmd, err)
		}
	}
	return cmd, payload, nil
}

// ─── Payload encoding helpers ─────────────────────────────────────────────────

// PutUint8 appends a single byte to buf and returns the new slice.
func PutUint8(buf []byte, v uint8) []byte {
	return append(buf, v)
}

// PutUint16 appends a big-endian uint16 to buf and returns the new slice.
func PutUint16(buf []byte, v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return append(buf, b...)
}

// PutUint32 appends a big-endian uint32 to buf and returns the new slice.
func PutUint32(buf []byte, v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return append(buf, b...)
}

// PutUint64 appends a big-endian uint64 to buf and returns the new slice.
func PutUint64(buf []byte, v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return append(buf, b...)
}

// PutInt64 appends a big-endian int64 to buf and returns the new slice.
func PutInt64(buf []byte, v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return append(buf, b...)
}

// PutStringU8 appends a length-prefixed string (uint8 + bytes) to buf.
// This is the format used by the real MooseFS protocol for names.
func PutStringU8(buf []byte, s string) []byte {
	buf = append(buf, uint8(len(s)))
	return append(buf, []byte(s)...)
}

// PutString appends a length-prefixed string (uint16 BE + bytes) to buf.
//
// Deprecated: use PutStringU8 for real MooseFS protocol messages.
// Kept for compatibility with legacy fake-server test code.
func PutString(buf []byte, s string) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(len(s)))
	buf = append(buf, b...)
	return append(buf, []byte(s)...)
}

// ─── Payload decoding helpers ─────────────────────────────────────────────────

// ReadUint8 reads a single byte from p[off].
// Returns the value and the new offset, or an error if out of range.
func ReadUint8(p []byte, off int) (uint8, int, error) {
	if off >= len(p) {
		return 0, off, fmt.Errorf("mfsclient: read uint8: buffer too short (off=%d len=%d)", off, len(p))
	}
	return p[off], off + 1, nil
}

// ReadUint16 reads a big-endian uint16 from p[off:off+2].
func ReadUint16(p []byte, off int) (uint16, int, error) {
	if off+2 > len(p) {
		return 0, off, fmt.Errorf("mfsclient: read uint16: buffer too short (off=%d len=%d)", off, len(p))
	}
	return binary.BigEndian.Uint16(p[off : off+2]), off + 2, nil
}

// ReadUint32 reads a big-endian uint32 from p[off:off+4].
// Returns the value and the new offset, or an error if out of range.
func ReadUint32(p []byte, off int) (uint32, int, error) {
	if off+4 > len(p) {
		return 0, off, fmt.Errorf("mfsclient: read uint32: buffer too short (off=%d len=%d)", off, len(p))
	}
	return binary.BigEndian.Uint32(p[off : off+4]), off + 4, nil
}

// ReadUint64 reads a big-endian uint64 from p[off:off+8].
func ReadUint64(p []byte, off int) (uint64, int, error) {
	if off+8 > len(p) {
		return 0, off, fmt.Errorf("mfsclient: read uint64: buffer too short (off=%d len=%d)", off, len(p))
	}
	return binary.BigEndian.Uint64(p[off : off+8]), off + 8, nil
}

// ReadInt64 reads a big-endian int64 from p[off:off+8].
func ReadInt64(p []byte, off int) (int64, int, error) {
	v, newOff, err := ReadUint64(p, off)
	return int64(v), newOff, err
}

// ReadStringU8 reads a length-prefixed string (uint8 + bytes) from p[off:].
// This is the format used by the real MooseFS protocol for names.
func ReadStringU8(p []byte, off int) (string, int, error) {
	if off >= len(p) {
		return "", off, fmt.Errorf("mfsclient: read string (u8) length: buffer too short")
	}
	length := int(p[off])
	off++
	if off+length > len(p) {
		return "", off, fmt.Errorf("mfsclient: read string (u8) data: buffer too short (need %d bytes)", length)
	}
	return string(p[off : off+length]), off + length, nil
}

// ReadString reads a length-prefixed string (uint16 BE + bytes) from p[off:].
//
// Deprecated: use ReadStringU8 for real MooseFS protocol messages.
// Kept for compatibility with legacy fake-server test code.
func ReadString(p []byte, off int) (string, int, error) {
	if off+2 > len(p) {
		return "", off, fmt.Errorf("mfsclient: read string length: buffer too short")
	}
	length := int(binary.BigEndian.Uint16(p[off : off+2]))
	off += 2
	if off+length > len(p) {
		return "", off, fmt.Errorf("mfsclient: read string data: buffer too short (need %d bytes)", length)
	}
	return string(p[off : off+length]), off + length, nil
}

// ─── Attribute parsing ────────────────────────────────────────────────────────

// ParseAttrs decodes MooseFS attr wire format starting at off.
//
// MooseFS 4.x sends 35 or 36 bytes depending on build:
//
//	[flags:8][mode:16][uid:32][gid:32][atime:32][mtime:32][ctime:32][nlink:32][size:64]
//	= 1+2+4+4+4+4+4+4+8 = 35 bytes
//	+ optional [winattr:8] = 36 bytes in some MooseFS 4.x builds
//
// ParseAttrs reads at least 35 bytes and skips the optional winattr if present.
// Returns the populated Attr, the new offset, and any error.
func ParseAttrs(payload []byte, off int) (*Attr, int, error) {
	const attrsLen = 35
	if off+attrsLen > len(payload) {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs: buffer too short (need %d, have %d at off %d)",
			attrsLen, len(payload)-off, off)
	}

	var a Attr
	var err error

	a.Flags, off, err = ReadUint8(payload, off)
	if err != nil {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs flags: %w", err)
	}
	a.Mode, off, err = ReadUint16(payload, off)
	if err != nil {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs mode: %w", err)
	}
	a.UID, off, err = ReadUint32(payload, off)
	if err != nil {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs uid: %w", err)
	}
	a.GID, off, err = ReadUint32(payload, off)
	if err != nil {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs gid: %w", err)
	}
	a.ATime, off, err = ReadUint32(payload, off)
	if err != nil {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs atime: %w", err)
	}
	a.MTime, off, err = ReadUint32(payload, off)
	if err != nil {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs mtime: %w", err)
	}
	a.CTime, off, err = ReadUint32(payload, off)
	if err != nil {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs ctime: %w", err)
	}
	a.NLink, off, err = ReadUint32(payload, off)
	if err != nil {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs nlink: %w", err)
	}
	a.Size, off, err = ReadUint64(payload, off)
	if err != nil {
		return nil, off, fmt.Errorf("mfsclient: ParseAttrs size: %w", err)
	}

	// Skip optional winattr byte present in some MooseFS 4.x builds
	// (total attrs becomes 36 bytes instead of 35).
	if off < len(payload) {
		off++ // consume winattr or any trailing byte
	}

	// Populate convenience field.
	a.ModTime = int64(a.MTime)
	return &a, off, nil
}

// isErrorResponse returns true when payload has the 5-byte error layout:
// [msgid:32][status:8].  Commands that return attrs on success (GETATTR,
// LOOKUP, MKNOD, MKDIR) use this to distinguish success (39-40 bytes) from
// error (5 bytes).
func isErrorResponse(payload []byte) bool { return len(payload) == 5 }

// minSuccessLen is the minimum payload length for commands that return attrs.
// Real MooseFS 4.x servers may send 39 bytes (35-byte attrs) or 40 bytes
// (36-byte attrs with winattr).
const minSuccessLen = 39
