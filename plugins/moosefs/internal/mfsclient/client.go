// Package mfsclient implements a minimal synchronous TCP client for MooseFS.
//
// Usage:
//
//	c, err := Dial("192.168.1.10", 9421)
//	if err != nil { ... }
//	defer c.Close()
//
//	if err := c.Register(); err != nil { ... }
//	attrs, err := c.GetAttr(RootNodeID)
//	entries, err := c.ReadDir(RootNodeID)
package mfsclient

import (
	"encoding/hex"
	"fmt"
	"net"
	"sync"

	"github.com/CCoupel/GhostDrive/internal/logger"
	"github.com/CCoupel/GhostDrive/plugins"
)

// mfsClientVersion is the MooseFS client version we declare during REGISTER.
// Encoding: VERSION2INT(maj, mid, min) = (maj<<16) | (mid<<8) | (min*2) when maj>1.
// The *2 multiplier on the patch component is required by the official MooseFS
// VERSION2INT macro; omitting it produces an off-by-one version that the master
// logs as 4.58.2 instead of 4.58.4.
// Must be >= the server's minimum supported client version.
// Keep in sync with the master version deployed on the cluster.
const mfsClientVersion = (4 << 16) | (58 << 8) | (4 * 2) // VERSION2INT(4,58,4) = 277000

// Client is a synchronous MooseFS TCP client.
// All methods are safe to call from multiple goroutines; they are protected
// by a single mutex so that request/response frames are never interleaved.
type Client struct {
	mu        sync.Mutex
	conn      net.Conn
	addr      string // "host:port"
	sessionID uint32 // assigned by master after Register
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

// SessionID returns the session ID assigned by the master after Register.
func (c *Client) SessionID() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// roundtrip sends a request frame and reads the answer frame.
// It verifies that the answer command matches expectedAns.
// Returns the answer payload.
//
// Any NOP frames (cmd=0) received before the expected answer are silently
// discarded — a real MooseFS master sends a NOP keepalive immediately after
// TCP connect, which may be buffered before the first response.
//
// The caller must hold c.mu.
func (c *Client) roundtrip(cmd, expectedAns uint32, payload []byte) ([]byte, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("mfsclient: %w", plugins.ErrNotConnected)
	}

	if err := WriteFrame(c.conn, cmd, payload); err != nil {
		return nil, err
	}

	for {
		ansCmd, ansPayload, err := ReadFrame(c.conn)
		if err != nil {
			return nil, err
		}
		// Discard NOP keepalive frames silently.
		if ansCmd == ANTOAN_NOP {
			continue
		}
		if ansCmd != expectedAns {
			return nil, fmt.Errorf("mfsclient: unexpected answer cmd %d (expected %d)", ansCmd, expectedAns)
		}
		return ansPayload, nil
	}
}

// checkStatus reads the status byte from payload[0] (caller passes payload[4:]
// i.e. after stripping msgid) and returns an error if it is non-zero.
// On success it returns the remaining payload starting at offset 1.
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
	case StatusEEXIST:
		return nil, fmt.Errorf("mfsclient: file already exists")
	case StatusENOTEMPTY:
		return nil, fmt.Errorf("mfsclient: directory not empty")
	default:
		return nil, fmt.Errorf("mfsclient: server returned status 0x%02x", status)
	}
}

// ─── Registration ─────────────────────────────────────────────────────────────

// Register authenticates with the MooseFS master using CLTOMA_FUSE_REGISTER
// (opcode 400).
//
// Note: a real MooseFS master may send a NOP keepalive frame (cmd=0) after
// TCP connect.  This does NOT need to be consumed before sending REGISTER —
// the server processes REGISTER independently of any queued NOP frame.
//
// Payload sent (REGISTER_NEWSESSION = rcode 2):
//
//	[blob:64B][rcode:8=2][version:32]
//	[ileng:32=0]   (empty instance name — accepted by all MooseFS 4.x masters)
//	[pleng:32=2]["/\x00"]  (minimal mount path — null-terminated C-string)
//
// Expected response (MATOCL_FUSE_REGISTER=401):
//
//	Success (len >= 8): [version:32][sessionId:32][...]
//	Error   (len == 1): [status:8]
func (c *Client) Register() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build payload.
	var req []byte
	req = append(req, []byte(FuseRegisterBlobACL)...) // 64 bytes blob
	req = PutUint8(req, RegisterNewSession)            // rcode = 2

	// Declare our client version — see mfsClientVersion constant above.
	// MooseFS 4.x masters reject older clients with EPERM.
	req = PutUint32(req, mfsClientVersion)

	// ileng=0 (empty instance name — accepted by all MooseFS 4.x masters).
	req = PutUint32(req, 0)

	// pleng=2 + path="/\x00" — MooseFS master expects a null-terminated C-string;
	// pleng includes the null byte.  Sending pleng=1 (no null) causes the server
	// to close the connection immediately (EOF).
	req = PutUint32(req, 2)
	req = append(req, '/', 0)

	ans, err := c.roundtrip(CltomFuseRegister, MatoclFuseRegister, req)
	if err != nil {
		return fmt.Errorf("mfsclient: Register: %w", err)
	}

	// Error response = 1 byte status.
	if len(ans) == 1 {
		switch ans[0] {
		case 0x01:
			// EPERM: client IP not in mfsexports.cfg, or client version too old.
			return fmt.Errorf("mfsclient: Register: access denied (EPERM) — check mfsexports.cfg allows this host")
		case 0x02:
			return fmt.Errorf("mfsclient: Register: ENOENT — mount path not found on master")
		default:
			return fmt.Errorf("mfsclient: Register: server returned status 0x%02x", ans[0])
		}
	}
	if len(ans) < 8 {
		return fmt.Errorf("mfsclient: Register: response too short (%d bytes)", len(ans))
	}

	// [version:32][sessionId:32][metaId:64][sesflags:8]...
	sessionID, _, err := ReadUint32(ans, 4)
	if err != nil {
		return fmt.Errorf("mfsclient: Register: read sessionId: %w", err)
	}
	c.sessionID = sessionID
	return nil
}


// ─── StatFS ───────────────────────────────────────────────────────────────────

// StatFS queries the MooseFS master for cluster filesystem statistics.
//
// Request (CLTOMA_FUSE_STATFS = 402):
//
//	[msgid:32=0]
//
// Response (MATOCL_FUSE_STATFS = 403):
//
//	[msgid:32][totalspace:64][availspace:64][trashspace:64][sustainedspace:64][inodes:32]
//
// Returns free = availspace, total = totalspace.
func (c *Client) StatFS() (free, total int64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, 0) // msgid only — MooseFS 4.x StatFS takes no sessionId

	ans, err := c.roundtrip(CltomFuseStatFS, MatoclFuseStatFS, req)
	if err != nil {
		return 0, 0, fmt.Errorf("mfsclient: StatFS: %w", err)
	}

	// [msgid:32][totalspace:64][availspace:64][trash:64][sustained:64][inodes:32]
	if len(ans) < 4+8+8 {
		return 0, 0, fmt.Errorf("mfsclient: StatFS: response too short (%d bytes)", len(ans))
	}

	totalSpace, off, err := ReadUint64(ans, 4)
	if err != nil {
		return 0, 0, fmt.Errorf("mfsclient: StatFS: read total: %w", err)
	}
	availSpace, _, err := ReadUint64(ans, off)
	if err != nil {
		return 0, 0, fmt.Errorf("mfsclient: StatFS: read avail: %w", err)
	}

	return int64(availSpace), int64(totalSpace), nil
}

// ─── Lookup ───────────────────────────────────────────────────────────────────

// Lookup finds the nodeID of name inside directory parentID.
//
// Request (CLTOMA_FUSE_LOOKUP = 406):
//
//	[msgid:32=0][parent:32][namelen:8][name:bytes][uid:32=0][gcnt:32=1][gid:32=0]
//
// Response (MATOCL_FUSE_LOOKUP = 407):
//
//	Success (len==39): [msgid:32][inode:32][attrs:35]
//	Error   (len==5):  [msgid:32][status:8]
func (c *Client) Lookup(parentID uint32, name string) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, 0) // msgid
	req = PutUint32(req, parentID)
	req = PutStringU8(req, name)
	req = PutUint32(req, 0) // uid
	req = PutUint32(req, 1) // gcnt
	req = PutUint32(req, 0) // gid

	ans, err := c.roundtrip(CltomFuseLookup, MatoclFuseLookup, req)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Lookup(%d, %q): %w", parentID, name, err)
	}

	if isErrorResponse(ans) {
		// [msgid:32][status:8]
		status := ans[4]
		if status == StatusENOENT {
			return 0, fmt.Errorf("mfsclient: Lookup(%d, %q): %w", parentID, name, plugins.ErrFileNotFound)
		}
		return 0, fmt.Errorf("mfsclient: Lookup(%d, %q): status 0x%02x", parentID, name, status)
	}

	// Success: [msgid:32][inode:32][attrs:35]
	if len(ans) < minSuccessLen {
		return 0, fmt.Errorf("mfsclient: Lookup(%d, %q): response too short (%d)", parentID, name, len(ans))
	}
	inode, _, err := ReadUint32(ans, 4)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Lookup(%d, %q) inode: %w", parentID, name, err)
	}
	return inode, nil
}

// ─── GetAttr ──────────────────────────────────────────────────────────────────

// GetAttr returns the attributes of node nodeID.
// Returns ErrFileNotFound (wrapped) if the node does not exist.
//
// Request (CLTOMA_FUSE_GETATTR = 408):
//
//	[msgid:32=0][inode:32]
//
// Response (MATOCL_FUSE_GETATTR = 409):
//
//	Success (len==39): [msgid:32][attrs:35]
//	Error   (len==5):  [msgid:32][status:8]
func (c *Client) GetAttr(nodeID uint32) (*Attr, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, 0) // msgid
	req = PutUint32(req, nodeID)

	ans, err := c.roundtrip(CltomFuseGetAttr, MatoclFuseGetAttr, req)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: GetAttr(%d): %w", nodeID, err)
	}

	if isErrorResponse(ans) {
		status := ans[4]
		if status == StatusENOENT {
			return nil, fmt.Errorf("mfsclient: GetAttr(%d): %w", nodeID, plugins.ErrFileNotFound)
		}
		return nil, fmt.Errorf("mfsclient: GetAttr(%d): status 0x%02x", nodeID, status)
	}

	if len(ans) < minSuccessLen {
		return nil, fmt.Errorf("mfsclient: GetAttr(%d): response too short (%d)", nodeID, len(ans))
	}

	attr, _, err := ParseAttrs(ans, 4)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: GetAttr(%d): %w", nodeID, err)
	}
	attr.NodeID = nodeID
	return attr, nil
}

// ─── ReadDir ──────────────────────────────────────────────────────────────────

// ReadDir lists the direct children of directory nodeID.
// Returns an empty (non-nil) slice when the directory is empty.
//
// Request (CLTOMA_FUSE_READDIR = 428):
//
//	[msgid:32=0][parent:32][uid:32=0][gcnt:32=1][gid:32=0][flags:8=0][maxentries:32=0xffff][skipcnt:64=0]
//
// Response (MATOCL_FUSE_READDIR = 429):
//
//	[msgid:32][next_skipcnt:64][entries...]
//	each entry: [namelen:8][name:namelen][inode:32][dtype:8]
//	dtype: 1=file, 2=dir

func (c *Client) ReadDir(nodeID uint32) ([]DirEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, 0)     // msgid
	req = PutUint32(req, nodeID) // parent inode
	req = PutUint32(req, 0)      // uid
	req = PutUint32(req, 1)      // gcnt
	req = PutUint32(req, 0)      // gid
	req = PutUint8(req, 0)       // flags
	req = PutUint32(req, 0xffff) // maxentries
	req = PutUint64(req, 0)      // skipcnt

	ans, err := c.roundtrip(CltomFuseReadDir, MatoclFuseReadDir, req)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: ReadDir(%d): %w", nodeID, err)
	}

	if len(ans) < 4 {
		return nil, fmt.Errorf("mfsclient: ReadDir(%d): response too short (%d bytes) hex=%s",
			nodeID, len(ans), hex.EncodeToString(ans))
	}

	off := 4 // skip msgid

	// Skip next_skipcnt:64 (pagination — 0x7FFFFFFFFFFFFFFF = no more pages)
	if off+8 > len(ans) {
		return nil, fmt.Errorf("mfsclient: ReadDir(%d): response too short for skipcnt (%d bytes)", nodeID, len(ans))
	}
	off += 8

	entries := make([]DirEntry, 0, 16)

	for off < len(ans) {
		var e DirEntry
		var nameLen uint8
		nameLen, off, err = ReadUint8(ans, off)
		if err != nil {
			break
		}
		if off+int(nameLen) > len(ans) {
			// Dump raw bytes to diagnose the actual server response format.
			dump := hex.EncodeToString(ans)
			return nil, fmt.Errorf("mfsclient: ReadDir(%d): entry name truncated at off=%d (namelen=%d, buf=%d) hex=%s",
				nodeID, off, nameLen, len(ans), dump)
		}
		e.Name = string(ans[off : off+int(nameLen)])
		off += int(nameLen)

		e.NodeID, off, err = ReadUint32(ans, off)
		if err != nil {
			return nil, fmt.Errorf("mfsclient: ReadDir(%d): entry inode: %w", nodeID, err)
		}

		var dtype uint8
		dtype, off, err = ReadUint8(ans, off)
		if err != nil {
			return nil, fmt.Errorf("mfsclient: ReadDir(%d): entry dtype: %w", nodeID, err)
		}
		e.IsDir = (dtype == 2)
		// Size and MTime not provided by ReadDir — will remain zero

		entries = append(entries, e)
	}
	return entries, nil
}

// ─── Mknod ────────────────────────────────────────────────────────────────────

// Mknod creates a regular file named name inside directory parentID with the
// given mode.  Returns the new file's nodeID.
//
// Request (CLTOMA_FUSE_MKNOD = 416):
//
//	[msgid:32=0][parent:32][namelen:8][name][type:8=1][mode:16][umask:16=0][uid:32=0][gcnt:32=1][gid:32=0][rdev:32=0]
//
// Response:
//
//	Success (len==39): [msgid:32][inode:32][attrs:35]
//	Error   (len==5):  [msgid:32][status:8]
func (c *Client) Mknod(parentID uint32, name string, mode uint32) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, 0) // msgid
	req = PutUint32(req, parentID)
	req = PutStringU8(req, name)
	req = PutUint8(req, 1)              // type = regular file
	req = PutUint16(req, uint16(mode))  // mode (permissions)
	req = PutUint16(req, 0)             // umask
	req = PutUint32(req, 0)             // uid
	req = PutUint32(req, 1)             // gcnt
	req = PutUint32(req, 0)             // gid
	req = PutUint32(req, 0)             // rdev

	ans, err := c.roundtrip(CltomFuseMknod, MatoclFuseMknod, req)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mknod(%d, %q): %w", parentID, name, err)
	}

	if isErrorResponse(ans) {
		status := ans[4]
		if status == StatusENOENT {
			return 0, fmt.Errorf("mfsclient: Mknod(%d, %q): %w", parentID, name, plugins.ErrFileNotFound)
		}
		return 0, fmt.Errorf("mfsclient: Mknod(%d, %q): status 0x%02x", parentID, name, status)
	}

	if len(ans) < minSuccessLen {
		return 0, fmt.Errorf("mfsclient: Mknod(%d, %q): response too short (%d)", parentID, name, len(ans))
	}
	inode, _, err := ReadUint32(ans, 4)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mknod(%d, %q) inode: %w", parentID, name, err)
	}
	return inode, nil
}

// ─── Mkdir ────────────────────────────────────────────────────────────────────

// Mkdir creates a directory named name inside directory parentID with the
// given mode.  Returns the new directory's nodeID.
//
// Request (CLTOMA_FUSE_MKDIR = 418):
//
//	[msgid:32=0][parent:32][namelen:8][name][mode:16][umask:16=0][uid:32=0][gcnt:32=1][gid:32=0][copysgid:8=0]
//
// Response: idem Mknod.
func (c *Client) Mkdir(parentID uint32, name string, mode uint32) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, 0) // msgid
	req = PutUint32(req, parentID)
	req = PutStringU8(req, name)
	req = PutUint16(req, uint16(mode)) // mode
	req = PutUint16(req, 0)            // umask
	req = PutUint32(req, 0)            // uid
	req = PutUint32(req, 1)            // gcnt
	req = PutUint32(req, 0)            // gid
	req = PutUint8(req, 0)             // copysgid

	ans, err := c.roundtrip(CltomFuseMkdir, MatoclFuseMkdir, req)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mkdir(%d, %q): %w", parentID, name, err)
	}

	if isErrorResponse(ans) {
		status := ans[4]
		if status == StatusENOENT {
			return 0, fmt.Errorf("mfsclient: Mkdir(%d, %q): %w", parentID, name, plugins.ErrFileNotFound)
		}
		if status == StatusEEXIST {
			// Return 0 as a signal that the dir already exists — callers handle this.
			return 0, fmt.Errorf("mfsclient: Mkdir(%d, %q): file already exists", parentID, name)
		}
		return 0, fmt.Errorf("mfsclient: Mkdir(%d, %q): status 0x%02x", parentID, name, status)
	}

	if len(ans) < minSuccessLen {
		return 0, fmt.Errorf("mfsclient: Mkdir(%d, %q): response too short (%d)", parentID, name, len(ans))
	}
	inode, _, err := ReadUint32(ans, 4)
	if err != nil {
		return 0, fmt.Errorf("mfsclient: Mkdir(%d, %q) inode: %w", parentID, name, err)
	}
	return inode, nil
}

// ─── Unlink ───────────────────────────────────────────────────────────────────

// Unlink removes the file named name from directory parentID.
// Returns ErrFileNotFound (wrapped) if the file does not exist.
//
// Request (CLTOMA_FUSE_UNLINK = 420):
//
//	[msgid:32=0][parent:32][namelen:8][name][uid:32=0][gcnt:32=1][gid:32=0]
//
// Response: [msgid:32][status:8]
func (c *Client) Unlink(parentID uint32, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, 0) // msgid
	req = PutUint32(req, parentID)
	req = PutStringU8(req, name)
	req = PutUint32(req, 0) // uid
	req = PutUint32(req, 1) // gcnt
	req = PutUint32(req, 0) // gid

	ans, err := c.roundtrip(CltomFuseUnlink, MatoclFuseUnlink, req)
	if err != nil {
		return fmt.Errorf("mfsclient: Unlink(%d, %q): %w", parentID, name, err)
	}

	if len(ans) < 5 {
		return fmt.Errorf("mfsclient: Unlink(%d, %q): response too short", parentID, name)
	}
	// payload is [msgid:32][status:8]; pass status byte to checkStatus.
	if _, err = checkStatus(ans[4:]); err != nil {
		return fmt.Errorf("mfsclient: Unlink(%d, %q): %w", parentID, name, err)
	}
	return nil
}

// ─── Rmdir ────────────────────────────────────────────────────────────────────

// Rmdir removes the empty directory named name from directory parentID.
// Returns ErrFileNotFound (wrapped) if the directory does not exist.
//
// Request (CLTOMA_FUSE_RMDIR = 422):
//
//	[msgid:32=0][parent:32][namelen:8][name][uid:32=0][gcnt:32=1][gid:32=0]
//
// Response: [msgid:32][status:8]  (identical to Unlink)
func (c *Client) Rmdir(parentID uint32, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, 0) // msgid
	req = PutUint32(req, parentID)
	req = PutStringU8(req, name)
	req = PutUint32(req, 0) // uid
	req = PutUint32(req, 1) // gcnt
	req = PutUint32(req, 0) // gid

	ans, err := c.roundtrip(CltomFuseRmdir, MatoclFuseRmdir, req)
	if err != nil {
		return fmt.Errorf("mfsclient: Rmdir(%d, %q): %w", parentID, name, err)
	}

	if len(ans) < 5 {
		return fmt.Errorf("mfsclient: Rmdir(%d, %q): response too short", parentID, name)
	}
	if _, err = checkStatus(ans[4:]); err != nil {
		return fmt.Errorf("mfsclient: Rmdir(%d, %q): %w", parentID, name, err)
	}
	return nil
}

// ─── Rename ───────────────────────────────────────────────────────────────────

// Rename atomically renames srcName in srcParentID to dstName in dstParentID.
// Works for both files and directories (including non-empty ones).
//
// Request (CLTOMA_FUSE_RENAME = 424):
//
//	[msgid:32=0][srcParent:32][srcNameLen:8][srcName][dstParent:32][dstNameLen:8][dstName][uid:32=0][gcnt:32=1][gid:32=0]
//
// Response (MATOCL_FUSE_RENAME = 425):
//
//	[msgid:32][status:8]
func (c *Client) Rename(srcParentID uint32, srcName string, dstParentID uint32, dstName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := PutUint32(nil, 0) // msgid
	req = PutUint32(req, srcParentID)
	req = PutStringU8(req, srcName)
	req = PutUint32(req, dstParentID)
	req = PutStringU8(req, dstName)
	req = PutUint32(req, 0) // uid
	req = PutUint32(req, 1) // gcnt
	req = PutUint32(req, 0) // gid

	ans, err := c.roundtrip(CltomFuseRename, MatoclFuseRename, req)
	if err != nil {
		return fmt.Errorf("mfsclient: Rename(%d/%q → %d/%q): %w", srcParentID, srcName, dstParentID, dstName, err)
	}
	if len(ans) < 5 {
		return fmt.Errorf("mfsclient: Rename: response too short (%d bytes)", len(ans))
	}
	if _, err = checkStatus(ans[4:]); err != nil {
		return fmt.Errorf("mfsclient: Rename(%d/%q → %d/%q): %w", srcParentID, srcName, dstParentID, dstName, err)
	}
	return nil
}

// ─── Read / Write (Phase 2 — real chunk server I/O) ──────────────────────────

// Write writes data to file node nodeID at offset using the real MooseFS
// chunk-server protocol.
//
// Steps:
//  1. CLTOMA_FUSE_WRITE_CHUNK (434) → master returns ChunkInfo (CS location).
//     c.mu is held only during this master roundtrip.
//  2. DialCS + WriteChunk: data is sent to the chunk server (no lock held —
//     CS I/O does not touch c.conn).
//  3. CLTOMA_FUSE_WRITE_CHUNK_END (436) → master commits the new file length.
//     c.mu is re-acquired for this second master roundtrip.
func (c *Client) Write(nodeID uint32, offset uint64, data []byte) error {
	index := uint32(offset / ChunkSize)
	chunkOffset := uint32(offset % ChunkSize)

	// Phase 1: roundtrip master sous mutex — obtenir la localisation du chunk.
	info, err := func() (*ChunkInfo, error) {
		c.mu.Lock()
		defer c.mu.Unlock()

		// CLTOMA_FUSE_WRITE_CHUNK (434) — MooseFS 4.x (>= 3.0.4) payload:
		//   [msgid:32][inode:32][chunkindx:32][chunkopflags:8]  = 13 bytes
		// chunkopflags: 0x01=CANMODTIME 0x02=CONTINUEOP 0x04=CANUSERESERVESPACE
		req := PutUint32(nil, 0) // msgid
		req = PutUint32(req, nodeID)
		req = PutUint32(req, index)
		req = PutUint8(req, 0) // chunkopflags = 0

		ans, err := c.roundtrip(CltomFuseWriteChunk, MatoclFuseWriteChunk, req)
		if err != nil {
			return nil, fmt.Errorf("mfsclient: Write(%d, off=%d): WRITE_CHUNK: %w", nodeID, offset, err)
		}
		// A 5-byte response is an error: [msgid:32][status:8].
		if len(ans) == 5 {
			return nil, fmt.Errorf("mfsclient: Write(%d, off=%d): WRITE_CHUNK status 0x%02x", nodeID, offset, ans[4])
		}
		info, err := parseChunkInfo(ans)
		if err != nil {
			limit := len(ans)
			if limit > 64 {
				limit = 64
			}
			return nil, fmt.Errorf("mfsclient: Write(%d, off=%d): parse chunk info: %w [raw %d bytes: % x]",
				nodeID, offset, err, len(ans), ans[:limit])
		}
		if len(info.Servers) == 0 {
			return nil, fmt.Errorf("mfsclient: Write(%d, off=%d): no chunk servers available", nodeID, offset)
		}
		logger.Debug("[mfsclient] Write: chunk %d — %d servers found: %v lockid=%d", index, len(info.Servers), info.Servers, info.LockID)
		return info, nil
	}()
	if err != nil {
		return err
	}

	// Phase 2: I/O chunk server hors mutex — c.conn n'est pas utilisé ici.
	srv := info.Servers[0]
	csIP := net.IP([]byte{byte(srv.IP >> 24), byte(srv.IP >> 16), byte(srv.IP >> 8), byte(srv.IP)})
	logger.Debug("[mfsclient] Write: dialing CS %s:%d", csIP, srv.Port)
	cs, err := DialCS(srv.IP, srv.Port)
	if err != nil {
		return fmt.Errorf("mfsclient: Write(%d, off=%d): dial CS: %w", nodeID, offset, err)
	}
	logger.Debug("[mfsclient] Write: CS %s:%d connected", csIP, srv.Port)
	defer cs.Close()

	// Pass nil chain: write to CS1 only. The MooseFS master handles async
	// replication to other CSes after WRITE_CHUNK_END is committed.
	// Passing Servers[1:] as a chain causes CS1 to synchronously forward data
	// to CS2..CSN before returning the write-init ACK; when a chain CS is
	// unreachable this produces a ~5 s TCP timeout followed by CANTCONNECT or EOF,
	// blocking the entire write. Nil chain is a valid mode for FUSE clients.
	if err := WriteChunk(cs, info.ChunkID, info.Version, chunkOffset, data, nil); err != nil {
		return fmt.Errorf("mfsclient: Write(%d, off=%d): write chunk: %w", nodeID, offset, err)
	}

	// Phase 3: roundtrip master sous mutex — commiter la nouvelle longueur.
	newLength := offset + uint64(len(data))
	c.mu.Lock()
	defer c.mu.Unlock()

	logger.Debug("[mfsclient] Write: sending WRITE_CHUNK_END newLength=%d chunkOffset=%d dataLen=%d",
		newLength, chunkOffset, len(data))
	// CLTOMA_FUSE_WRITE_CHUNK_END (436) — format depends on master version:
	//
	//   >= 3.0.74 (29 bytes):
	//     [msgid:32][chunkid:64][inode:32][chunkindx:32][length:64][chunkopflags:8]
	//   >= 4.40.0 (37 bytes, extended):
	//     [msgid:32][chunkid:64][inode:32][chunkindx:32][length:64][chunkopflags:8][offset:32][size:32]
	//
	// We declare ourselves as mfsClientVersion 4.58.4 (>= 4.40.0), so the master
	// expects the extended 37-byte format.
	//
	// length  : new total file size after this write (NOT chunk size).
	// offset  : byte offset within the chunk where the write started.
	// size    : number of bytes written in this chunk.
	// NO version field. NO lockid field.
	endReq := PutUint32(nil, 0)               // msgid
	endReq = PutUint64(endReq, info.ChunkID)  // chunkid:64
	endReq = PutUint32(endReq, nodeID)         // inode:32
	endReq = PutUint32(endReq, index)          // chunkindx:32 — chunk index within file
	endReq = PutUint64(endReq, newLength)      // length:64 — new total file size
	endReq = PutUint8(endReq, 0)              // chunkopflags:8 = 0
	endReq = PutUint32(endReq, chunkOffset)   // offset:32 — write start within chunk (>= 4.40.0)
	endReq = PutUint32(endReq, uint32(len(data))) // size:32 — bytes written (>= 4.40.0)

	endAns, err := c.roundtrip(CltomFuseWriteChunkEnd, MatoclFuseWriteChunkEnd, endReq)
	if err != nil {
		return fmt.Errorf("mfsclient: Write(%d, off=%d): WRITE_CHUNK_END: %w", nodeID, offset, err)
	}
	logger.Debug("[mfsclient] Write: WRITE_CHUNK_END resp len=%d bytes=%x", len(endAns), endAns)
	if len(endAns) < 5 {
		return fmt.Errorf("mfsclient: Write(%d, off=%d): WRITE_CHUNK_END response too short (%d)", nodeID, offset, len(endAns))
	}
	if endAns[4] != StatusOK {
		return fmt.Errorf("mfsclient: Write(%d, off=%d): WRITE_CHUNK_END status 0x%02x", nodeID, offset, endAns[4])
	}
	logger.Debug("[mfsclient] Write: WRITE_CHUNK_END acked status=OK")
	return nil
}

// Read reads up to size bytes from file node nodeID starting at offset using
// the real MooseFS chunk-server protocol.
//
// Steps:
//  1. CLTOMA_FUSE_READ_CHUNK (432) → master returns ChunkInfo (CS location).
//     c.mu is held only during this master roundtrip.
//  2. DialCS + ReadChunk: data is fetched from the chunk server (no lock held —
//     CS I/O does not touch c.conn).
//
// Returns an empty (nil) slice when the requested offset is past the end of
// the file (EOF).
func (c *Client) Read(nodeID uint32, offset uint64, size uint32) ([]byte, error) {
	index := uint32(offset / ChunkSize)
	chunkOffset := uint32(offset % ChunkSize)

	// Phase 1: roundtrip master sous mutex — localiser le chunk.
	info, err := func() (*ChunkInfo, error) {
		c.mu.Lock()
		defer c.mu.Unlock()

		req := PutUint32(nil, 0) // msgid
		req = PutUint32(req, nodeID)
		req = PutUint32(req, index)

		ans, err := c.roundtrip(CltomFuseReadChunk, MatoclFuseReadChunk, req)
		if err != nil {
			return nil, fmt.Errorf("mfsclient: Read(%d, off=%d): READ_CHUNK: %w", nodeID, offset, err)
		}

		// A 5-byte response is an error or EOF: [msgid:32][status:8].
		if len(ans) == 5 {
			status := ans[4]
			switch status {
			case StatusOK:
				return nil, nil // empty chunk (EOF) — info==nil, err==nil
			case StatusENOENT:
				return nil, fmt.Errorf("mfsclient: Read(%d, off=%d): %w", nodeID, offset, plugins.ErrFileNotFound)
			default:
				return nil, fmt.Errorf("mfsclient: Read(%d, off=%d): status 0x%02x", nodeID, offset, status)
			}
		}

		info, err := parseChunkInfo(ans)
		if err != nil {
			return nil, fmt.Errorf("mfsclient: Read(%d, off=%d): parse chunk info: %w", nodeID, offset, err)
		}
		if len(info.Servers) == 0 {
			return nil, fmt.Errorf("mfsclient: Read(%d, off=%d): no chunk servers available", nodeID, offset)
		}
		return info, nil
	}()
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil // EOF
	}

	// Phase 2: I/O chunk server hors mutex — c.conn n'est pas utilisé ici.
	srv := info.Servers[0]
	cs, err := DialCS(srv.IP, srv.Port)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: Read(%d, off=%d): dial CS: %w", nodeID, offset, err)
	}
	defer cs.Close()

	return ReadChunk(cs, info.ChunkID, info.Version, chunkOffset, size)
}

// ─── Internal: chunk info parsing ────────────────────────────────────────────

// parseChunkInfo decodes the master response for READ_CHUNK (433) and
// WRITE_CHUNK (435). Three protocol variants are supported, detected from the
// byte at offset 4 (immediately after msgid):
//
//	Proto 2 (MooseFS >= 3.0.10, incl. all 4.x) — protocolid byte = 2:
//	  [msgid:32][protocolid:8=2][length:64][chunkid:64][version:32]
//	  N × [ip:32 port:16 cs_ver:32 labelmask:32]  (14 bytes/entry, N implicit)
//	  [lockid:32]  (optional trailing field — present when server uses chunk locking)
//
//	Proto 1 (MooseFS >= 1.7.32, < 3.0.10) — protocolid byte = 1:
//	  [msgid:32][protocolid:8=1][length:64][chunkid:64][version:32]
//	  N × [ip:32 port:16 cs_ver:32]  (10 bytes/entry, N implicit)
//	  [lockid:32]  (optional trailing field)
//
//	Proto 0 (MooseFS < 1.7.32) — no protocolid byte:
//	  [msgid:32][length:64][chunkid:64][version:32]
//	  N × [ip:32 port:16]  (6 bytes/entry, N implicit)
//
// N is never transmitted explicitly; it is derived from the remaining payload
// bytes divided by the per-entry size.  Any trailing 4 bytes that do not form
// a complete CS entry are interpreted as a lockid token that must be echoed
// back in the WRITE_CHUNK_END request.
func parseChunkInfo(ans []byte) (*ChunkInfo, error) {
	if len(ans) < 5 {
		return nil, fmt.Errorf("response too short (%d bytes)", len(ans))
	}

	off := 4 // skip msgid

	// Detect protocol version from the byte immediately following msgid.
	// Proto 1/2: this byte is explicitly protocolid (1 or 2).
	// Proto 0: this byte is the MSB of the file length (always 0 for files < 72 PiB).
	protocolID := ans[off]

	var (
		err       error
		chunkID   uint64
		version   uint32
		entrySize int
	)

	switch protocolID {
	case 1, 2:
		// Proto 1/2: consume protocolid, then [length:64][chunkid:64][version:32].
		off++ // consume protocolid byte
		_, off, err = ReadUint64(ans, off) // file length — not needed in ChunkInfo
		if err != nil {
			return nil, fmt.Errorf("length: %w", err)
		}
		chunkID, off, err = ReadUint64(ans, off)
		if err != nil {
			return nil, fmt.Errorf("chunkID: %w", err)
		}
		version, off, err = ReadUint32(ans, off)
		if err != nil {
			return nil, fmt.Errorf("version: %w", err)
		}
		switch protocolID {
		case 1:
			entrySize = 10 // ip:32 + port:16 + cs_ver:32
		case 2:
			entrySize = 14 // ip:32 + port:16 + cs_ver:32 + labelmask:32
		}

	case 0:
		// Proto 0: no protocolid byte; off is still at 4 (start of [length:64]).
		// The byte at offset 4 is the MSB of the 64-bit file length (always 0
		// for files < 72 PiB), not a separate protocol identifier.
		_, off, err = ReadUint64(ans, off) // file length
		if err != nil {
			return nil, fmt.Errorf("length (proto0): %w", err)
		}
		chunkID, off, err = ReadUint64(ans, off)
		if err != nil {
			return nil, fmt.Errorf("chunkID (proto0): %w", err)
		}
		version, off, err = ReadUint32(ans, off)
		if err != nil {
			return nil, fmt.Errorf("version (proto0): %w", err)
		}
		entrySize = 6 // ip:32 + port:16 (no cs_ver in proto 0)

	default:
		// Unknown protocolid — likely a newer MooseFS protocol version (>= proto 3)
		// that GhostDrive does not yet support.  Falling back to proto 0 would
		// silently misparse the response and produce incorrect chunk server lists.
		// Return an explicit error so the caller can surface a diagnostic.
		limit := len(ans)
		if limit > 32 {
			limit = 32
		}
		logger.Warn("mfsclient: parseChunkInfo: unknown protocolid=%d — MooseFS may have introduced a new chunk-info format. Raw (first %d bytes): %x", protocolID, limit, ans[:limit])
		return nil, fmt.Errorf("parseChunkInfo: unknown protocolid %d (raw: %x) — upgrade GhostDrive or report this to the project", protocolID, ans[:limit])
	}

	remaining := len(ans) - off
	n := 0
	if entrySize > 0 && remaining > 0 {
		n = remaining / entrySize
	}

	info := &ChunkInfo{
		ChunkID: chunkID,
		Version: version,
		Servers: make([]ChunkServer, 0, n),
	}

	for i := 0; i < n; i++ {
		var srv ChunkServer
		srv.IP, off, err = ReadUint32(ans, off)
		if err != nil {
			return nil, fmt.Errorf("server[%d] IP: %w", i, err)
		}
		srv.Port, off, err = ReadUint16(ans, off)
		if err != nil {
			return nil, fmt.Errorf("server[%d] port: %w", i, err)
		}
		if entrySize >= 10 { // proto 1 or 2: cs_ver field present
			srv.Version, off, err = ReadUint32(ans, off)
			if err != nil {
				return nil, fmt.Errorf("server[%d] cs_ver: %w", i, err)
			}
		}
		if entrySize == 14 { // proto 2 only: labelmask field present
			_, off, err = ReadUint32(ans, off) // labelmask — not used by GhostDrive
			if err != nil {
				return nil, fmt.Errorf("server[%d] labelmask: %w", i, err)
			}
		}
		info.Servers = append(info.Servers, srv)
	}

	// MooseFS 4.x may append a lockid:32 token after the CS entries.
	// Any trailing 4 bytes that don't form a complete CS entry are the lockid;
	// it must be echoed verbatim in the subsequent WRITE_CHUNK_END request.
	// Diagnostic: trailing=4 → lockid present; trailing=0 → omitted by master.
	logger.Debug("mfsclient: parseChunkInfo: len=%d off=%d trailing=%d proto=%d nCS=%d",
		len(ans), off, len(ans)-off, protocolID, len(info.Servers))
	if len(ans)-off == 4 {
		info.LockID, _, _ = ReadUint32(ans, off)
	}

	return info, nil
}
