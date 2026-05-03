// Package mfsclient implements a minimal synchronous TCP client for MooseFS.
//
// Usage:
//
//	c, err := Dial("192.168.1.10", 9421)
//	if err != nil { ... }
//	defer c.Close()
//
//	attrs, err := c.GetAttr(RootNodeID)
//	entries, err := c.ReadDir(RootNodeID)
package mfsclient

import (
	"fmt"
	"net"
	"sync"

	"github.com/CCoupel/GhostDrive/plugins"
)

// Client is a synchronous MooseFS TCP client.
// All methods are safe to call from multiple goroutines; they are protected
// by a single mutex so that request/response frames are never interleaved.
type Client struct {
	mu   sync.Mutex
	conn net.Conn
	addr string // "host:port"
}

// Dial opens a TCP connection to host:port and returns a ready Client.
// Returns an error if the connection cannot be established.
func Dial(host string, port int) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: dial %s: %w", addr, err)
	}
	return &Client{conn: conn, addr: addr}, nil
}

// Close closes the underlying TCP connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// roundtrip sends a request frame and reads the answer frame.
// It verifies that the answer command matches expectedAns.
// Returns the answer payload (including the leading status byte).
// The caller must hold c.mu.
func (c *Client) roundtrip(cmd, expectedAns uint32, payload []byte) ([]byte, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("mfsclient: %w", plugins.ErrNotConnected)
	}

	if err := WriteFrame(c.conn, cmd, payload); err != nil {
		return nil, err
	}

	ansCmd, ansPayload, err := ReadFrame(c.conn)
	if err != nil {
		return nil, err
	}
	if ansCmd != expectedAns {
		return nil, fmt.Errorf("mfsclient: unexpected answer cmd %d (expected %d)", ansCmd, expectedAns)
	}
	return ansPayload, nil
}

// checkStatus reads the status byte from payload[0] and returns an error if
// it is non-zero.  On success it returns the remaining payload starting at
// offset 1.
func checkStatus(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("mfsclient: empty answer payload")
	}
	status := payload[0]
	rest := payload[1:]
	switch status {
	case StatusOK:
		return rest, nil
	case StatusENOENT:
		return nil, fmt.Errorf("mfsclient: %w", plugins.ErrFileNotFound)
	default:
		return nil, fmt.Errorf("mfsclient: server returned status 0x%02x", status)
	}
}

// ─── Operations ───────────────────────────────────────────────────────────────

// ReadDir lists the direct children of directory nodeID.
// Returns an empty (non-nil) slice when the directory is empty.
func (c *Client) ReadDir(nodeID uint32) ([]DirEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, nodeID)
	ans, err := c.roundtrip(CmdFUSEREADDIR, AnsFUSEREADDIR, req)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: ReadDir(%d): %w", nodeID, err)
	}
	rest, err := checkStatus(ans)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: ReadDir(%d): %w", nodeID, err)
	}

	// Parse: [count uint32][entries...]
	count, off, err := ReadUint32(rest, 0)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: ReadDir(%d): %w", nodeID, err)
	}

	entries := make([]DirEntry, 0, count)
	for i := uint32(0); i < count; i++ {
		var e DirEntry
		e.NodeID, off, err = ReadUint32(rest, off)
		if err != nil {
			return nil, fmt.Errorf("mfsclient: ReadDir(%d) entry %d nodeID: %w", nodeID, i, err)
		}
		if off >= len(rest) {
			return nil, fmt.Errorf("mfsclient: ReadDir(%d) entry %d isDir: buffer too short", nodeID, i)
		}
		e.IsDir = rest[off] != 0
		off++
		e.Name, off, err = ReadString(rest, off)
		if err != nil {
			return nil, fmt.Errorf("mfsclient: ReadDir(%d) entry %d name: %w", nodeID, i, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// GetAttr returns the attributes of node nodeID.
// Returns ErrFileNotFound (wrapped) if the node does not exist.
func (c *Client) GetAttr(nodeID uint32) (*Attr, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, nodeID)
	ans, err := c.roundtrip(CmdFUSEGETATTR, AnsFUSEGETATTR, req)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: GetAttr(%d): %w", nodeID, err)
	}
	rest, err := checkStatus(ans)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: GetAttr(%d): %w", nodeID, err)
	}

	// Parse: [nodeID uint32][size uint64][mode uint32][modtime int64]
	var attr Attr
	off := 0
	attr.NodeID, off, err = ReadUint32(rest, off)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: GetAttr(%d) nodeID: %w", nodeID, err)
	}
	attr.Size, off, err = ReadUint64(rest, off)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: GetAttr(%d) size: %w", nodeID, err)
	}
	attr.Mode, off, err = ReadUint32(rest, off)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: GetAttr(%d) mode: %w", nodeID, err)
	}
	attr.ModTime, _, err = ReadInt64(rest, off)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: GetAttr(%d) modtime: %w", nodeID, err)
	}
	return &attr, nil
}

// Mknod creates a regular file named name inside directory parentID with the
// given mode.  Returns the new file's nodeID.
func (c *Client) Mknod(parentID uint32, name string, mode uint32) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, parentID)
	req = PutUint32(req, mode)
	req = PutString(req, name)

	ans, err := c.roundtrip(CmdFUSEMKNOD, AnsFUSEMKNOD, req)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mknod(%d, %q): %w", parentID, name, err)
	}
	rest, err := checkStatus(ans)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mknod(%d, %q): %w", parentID, name, err)
	}

	nodeID, _, err := ReadUint32(rest, 0)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mknod(%d, %q) nodeID: %w", parentID, name, err)
	}
	return nodeID, nil
}

// Mkdir creates a directory named name inside directory parentID with the
// given mode.  Returns the new directory's nodeID.
// If the directory already exists, the server may return an error — the caller
// (Backend.CreateDir) should handle EEXIST as a no-op.
func (c *Client) Mkdir(parentID uint32, name string, mode uint32) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, parentID)
	req = PutUint32(req, mode)
	req = PutString(req, name)

	ans, err := c.roundtrip(CmdFUSEMKDIR, AnsFUSEMKDIR, req)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mkdir(%d, %q): %w", parentID, name, err)
	}
	rest, err := checkStatus(ans)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mkdir(%d, %q): %w", parentID, name, err)
	}

	nodeID, _, err := ReadUint32(rest, 0)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mkdir(%d, %q) nodeID: %w", parentID, name, err)
	}
	return nodeID, nil
}

// Write writes data to node nodeID at offset.
// The server stores the data; subsequent Read calls will return the written bytes.
func (c *Client) Write(nodeID uint32, offset uint64, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, nodeID)
	req = PutUint64(req, offset)
	req = PutUint32(req, uint32(len(data)))
	req = append(req, data...)

	ans, err := c.roundtrip(CmdFUSEWRITE, AnsFUSEWRITE, req)
	if err != nil {
		return fmt.Errorf("mfsclient: Write(%d, off=%d, len=%d): %w", nodeID, offset, len(data), err)
	}
	if _, err = checkStatus(ans); err != nil {
		return fmt.Errorf("mfsclient: Write(%d, off=%d, len=%d): %w", nodeID, offset, len(data), err)
	}
	return nil
}

// Read reads up to size bytes from node nodeID starting at offset.
// Returns the bytes read.  An empty slice indicates EOF.
func (c *Client) Read(nodeID uint32, offset uint64, size uint32) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, nodeID)
	req = PutUint64(req, offset)
	req = PutUint32(req, size)

	ans, err := c.roundtrip(CmdFUSEREAD, AnsFUSEREAD, req)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: Read(%d, off=%d, size=%d): %w", nodeID, offset, size, err)
	}
	rest, err := checkStatus(ans)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: Read(%d, off=%d, size=%d): %w", nodeID, offset, size, err)
	}

	// Parse: [dataLen uint32][data bytes]
	dataLen, off, err := ReadUint32(rest, 0)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: Read(%d) dataLen: %w", nodeID, err)
	}
	if off+int(dataLen) > len(rest) {
		return nil, fmt.Errorf("mfsclient: Read(%d) data: buffer too short", nodeID)
	}
	return rest[off : off+int(dataLen)], nil
}

// Unlink removes the file named name from directory parentID.
// Returns ErrFileNotFound (wrapped) if the file does not exist.
func (c *Client) Unlink(parentID uint32, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, parentID)
	req = PutString(req, name)

	ans, err := c.roundtrip(CmdFUSEUNLINK, AnsFUSEUNLINK, req)
	if err != nil {
		return fmt.Errorf("mfsclient: Unlink(%d, %q): %w", parentID, name, err)
	}
	if _, err = checkStatus(ans); err != nil {
		return fmt.Errorf("mfsclient: Unlink(%d, %q): %w", parentID, name, err)
	}
	return nil
}

// Rmdir removes the empty directory named name from directory parentID.
// Returns ErrFileNotFound (wrapped) if the directory does not exist.
func (c *Client) Rmdir(parentID uint32, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, parentID)
	req = PutString(req, name)

	ans, err := c.roundtrip(CmdFUSERMDIR, AnsFUSERMDIR, req)
	if err != nil {
		return fmt.Errorf("mfsclient: Rmdir(%d, %q): %w", parentID, name, err)
	}
	if _, err = checkStatus(ans); err != nil {
		return fmt.Errorf("mfsclient: Rmdir(%d, %q): %w", parentID, name, err)
	}
	return nil
}
