// Package mfsclient implements a minimal MooseFS 4.x TCP client.
//
// # Protocol
//
// Every message (both directions) is a binary frame:
//
//	[cmd uint32 BE][payloadLen uint32 BE][payload bytes]
//
// Client sends a request frame, server replies with an answer frame.
// The first byte of each answer payload is a status code (0 = success).
//
// # Command / Answer codes
//
// NOTE: The numeric values below are GhostDrive internal identifiers used
// exclusively between this client and the GhostDrive fake-server test fixture.
// They are NOT the real MooseFS protocol opcodes from mfsmaster/matocl*.c.
// Before connecting to a production MooseFS cluster, validate these constants
// against the official source at github.com/moosefs/moosefs.
//
// # Status codes (first byte of answer payload)
//
//	0x00 = STATUS_OK
//	0x01 = STATUS_ENOENT (ErrFileNotFound)
//	0x02 = STATUS_EEXIST
//	0x03 = STATUS_ENOTEMPTY
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
	StatusOK       uint8 = 0x00
	StatusENOENT   uint8 = 0x01
	StatusEEXIST   uint8 = 0x02
	StatusENOTEMPT uint8 = 0x03
	StatusERROR    uint8 = 0xFF
)

// ─── Command codes (client → server) ────────────────────────────────────────

const (
	CmdFUSEGETATTR uint32 = 501
	CmdFUSEMKNOD   uint32 = 502
	CmdFUSEMKDIR   uint32 = 503
	CmdFUSEUNLINK  uint32 = 504
	CmdFUSERMDIR   uint32 = 505
	CmdFUSEREAD    uint32 = 506
	CmdFUSEWRITE   uint32 = 507
	CmdFUSEREADDIR uint32 = 508
)

// ─── Answer codes (server → client) ─────────────────────────────────────────

const (
	AnsFUSEGETATTR uint32 = 601
	AnsFUSEMKNOD   uint32 = 602
	AnsFUSEMKDIR   uint32 = 603
	AnsFUSEUNLINK  uint32 = 604
	AnsFUSERMDIR   uint32 = 605
	AnsFUSEREAD    uint32 = 606
	AnsFUSEWRITE   uint32 = 607
	AnsFUSEREADDIR uint32 = 608
)

// ─── Shared data types ────────────────────────────────────────────────────────

// DirEntry is a single directory listing entry.
type DirEntry struct {
	NodeID uint32
	Name   string
	IsDir  bool
}

// Attr holds the metadata of a file or directory node.
type Attr struct {
	NodeID  uint32
	Size    uint64
	Mode    uint32
	ModTime int64
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

// ReadFrame reads the next frame from conn and returns its command code and
// payload.  Returns an error on I/O failure or malformed header.
func ReadFrame(conn net.Conn) (cmd uint32, payload []byte, err error) {
	hdr := make([]byte, 8)
	if _, err = io.ReadFull(conn, hdr); err != nil {
		return 0, nil, fmt.Errorf("mfsclient: read frame header: %w", err)
	}

	cmd = binary.BigEndian.Uint32(hdr[0:4])
	length := binary.BigEndian.Uint32(hdr[4:8])

	if length > 0 {
		payload = make([]byte, length)
		if _, err = io.ReadFull(conn, payload); err != nil {
			return 0, nil, fmt.Errorf("mfsclient: read frame payload (cmd %d): %w", cmd, err)
		}
	}
	return cmd, payload, nil
}

// ─── Payload encoding helpers ─────────────────────────────────────────────────

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

// PutString appends a length-prefixed string (uint16 BE + bytes) to buf.
func PutString(buf []byte, s string) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(len(s)))
	buf = append(buf, b...)
	return append(buf, []byte(s)...)
}

// ─── Payload decoding helpers ─────────────────────────────────────────────────

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

// ReadString reads a length-prefixed string (uint16 BE + bytes) from p[off:].
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
