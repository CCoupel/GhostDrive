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
	addr      string  // "host:port"
	sessionID uint32  // assigned by master after Register
	pool      *csPool // idle CS connection pool — prevents TIME_WAIT exhaustion (Windows #111)
}

// Dial opens a TCP connection to host:port and returns a ready Client.
// Returns an error if the connection cannot be established.
func Dial(host string, port int) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("mfsclient: dial %s: %w", addr, err)
	}
	return &Client{conn: conn, addr: addr, pool: newCSPool()}, nil
}

// Close closes the underlying TCP connection and all pooled CS connections.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.pool.CloseAll()
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

// writeStrategy describes a single attempt in the Write() cascade.
// csIdx selects the target CS from ChunkInfo.Servers; syncChain controls whether
// the remaining servers are passed as a replication chain.
type writeStrategy struct {
	csIdx     int  // index into info.Servers to select the target CS
	syncChain bool // pass info.Servers[csIdx+1:] as chain (sync); false = nil chain (async)
}

// defaultWriteStrategies defines the ordered fallback cascade for Write().
// Each strategy is attempted in order; the first to succeed wins.
//
//  1. Sync chain CS1 — write to Servers[0] with Servers[1:] as replication chain.
//     Preferred path when the cluster topology is fully healthy.
//  2. Sync chain CS2 — write to Servers[1] with Servers[2:] as chain.
//     Used when CS1 is unreachable but CS2 is available.
//  3. Async CS1 — write to Servers[0] with nil chain; master replicates post-commit.
//     Used when the sync chain breaks (CANTCONNECT on chain peer) but CS1 is up.
//  4. Async CS2 — write to Servers[1] with nil chain. Last resort.
//
// Strategies with csIdx >= len(info.Servers) are silently skipped.
// DO NOT mutate in tests — construct a local []writeStrategy for custom cascades.
var defaultWriteStrategies = []writeStrategy{
	{csIdx: 0, syncChain: true},  // 1. sync chain CS1
	{csIdx: 1, syncChain: true},  // 2. sync chain CS2
	{csIdx: 0, syncChain: false}, // 3. async CS1
	{csIdx: 1, syncChain: false}, // 4. async CS2
}

// writeChunkLock acquires a write lock on the chunk at (nodeID, index) by
// sending CLTOMA_FUSE_WRITE_CHUNK to the master. It holds c.mu only during the
// master roundtrip and returns the ChunkInfo (server list + LockID + file length).
//
// This is Phase 1 of the write protocol, extracted so it can be called once per
// fallback attempt (each attempt needs a fresh LockID).
func (c *Client) writeChunkLock(nodeID uint32, index uint32) (*ChunkInfo, error) {
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
		return nil, fmt.Errorf("mfsclient: writeChunkLock(node=%d, idx=%d): WRITE_CHUNK: %w", nodeID, index, err)
	}
	// A 5-byte response is an error: [msgid:32][status:8].
	if len(ans) == 5 {
		return nil, fmt.Errorf("mfsclient: writeChunkLock(node=%d, idx=%d): WRITE_CHUNK status 0x%02x", nodeID, index, ans[4])
	}
	info, err := parseChunkInfo(ans)
	if err != nil {
		limit := len(ans)
		if limit > 64 {
			limit = 64
		}
		return nil, fmt.Errorf("mfsclient: writeChunkLock(node=%d, idx=%d): parse chunk info: %w [raw %d bytes: % x]",
			nodeID, index, err, len(ans), ans[:limit])
	}
	if len(info.Servers) == 0 {
		return nil, fmt.Errorf("mfsclient: writeChunkLock(node=%d, idx=%d): no chunk servers available", nodeID, index)
	}
	logger.Debug("[mfsclient] writeChunkLock: chunk %d — %d servers, lockid=%d, fileLen=%d",
		index, len(info.Servers), info.LockID, info.Length)
	return info, nil
}

// writeChunkEnd commits the write to the master by sending CLTOMA_FUSE_WRITE_CHUNK_END
// with the new file length (offset + dataLen). It holds c.mu only during the master
// roundtrip.
//
// This is Phase 3 of the write protocol, extracted for reuse across fallback attempts.
func (c *Client) writeChunkEnd(info *ChunkInfo, nodeID, index uint32, offset uint64, dataLen int) error {
	newLength := offset + uint64(dataLen)
	chunkOffset := uint32(offset % ChunkSize)

	c.mu.Lock()
	defer c.mu.Unlock()

	logger.Debug("[mfsclient] writeChunkEnd: WRITE_CHUNK_END newLength=%d chunkOffset=%d dataLen=%d",
		newLength, chunkOffset, dataLen)

	// CLTOMA_FUSE_WRITE_CHUNK_END (436) — extended format (>= 4.40.0, 37 bytes):
	//   [msgid:32][chunkid:64][inode:32][chunkindx:32][length:64][chunkopflags:8][offset:32][size:32]
	//
	// length  : new total file size after this write.
	// offset  : byte offset within the chunk where the write started.
	// size    : number of bytes written in this chunk.
	endReq := PutUint32(nil, 0)                       // msgid
	endReq = PutUint64(endReq, info.ChunkID)           // chunkid:64
	endReq = PutUint32(endReq, nodeID)                  // inode:32
	endReq = PutUint32(endReq, index)                   // chunkindx:32
	endReq = PutUint64(endReq, newLength)               // length:64 — new total file size
	endReq = PutUint8(endReq, 0)                       // chunkopflags:8 = 0
	endReq = PutUint32(endReq, chunkOffset)             // offset:32 — write start within chunk
	endReq = PutUint32(endReq, uint32(dataLen))         // size:32 — bytes written

	endAns, err := c.roundtrip(CltomFuseWriteChunkEnd, MatoclFuseWriteChunkEnd, endReq)
	if err != nil {
		return fmt.Errorf("mfsclient: writeChunkEnd(%d, idx=%d): WRITE_CHUNK_END: %w", nodeID, index, err)
	}
	logger.Debug("[mfsclient] writeChunkEnd: resp len=%d bytes=%x", len(endAns), endAns)
	if len(endAns) < 5 {
		return fmt.Errorf("mfsclient: writeChunkEnd(%d, idx=%d): WRITE_CHUNK_END response too short (%d)",
			nodeID, index, len(endAns))
	}
	if endAns[4] != StatusOK {
		return fmt.Errorf("mfsclient: writeChunkEnd(%d, idx=%d): WRITE_CHUNK_END status 0x%02x",
			nodeID, index, endAns[4])
	}
	logger.Debug("[mfsclient] writeChunkEnd: WRITE_CHUNK_END acked status=OK")
	return nil
}

// writeChunkRelease releases the master write lock for a chunk WITHOUT modifying
// the file size. It sends CLTOMA_FUSE_WRITE_CHUNK_END with length = currentLength
// (the file length BEFORE the failed write attempt) and size = 0, signalling to the
// master that no data was committed.
//
// This must be called when CS I/O fails after writeChunkLock() to avoid leaving
// the master holding the lock for ~60 seconds until timeout.
func (c *Client) writeChunkRelease(info *ChunkInfo, nodeID, index uint32, currentLength uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	logger.Debug("[mfsclient] writeChunkRelease: releasing lock chunk %d, fileLen=%d (no write committed)",
		index, currentLength)

	// WRITE_CHUNK_END with length = currentLength (unchanged) and size = 0:
	// the master sees no new data → does not update the file size.
	endReq := PutUint32(nil, 0)                  // msgid
	endReq = PutUint64(endReq, info.ChunkID)      // chunkid:64
	endReq = PutUint32(endReq, nodeID)             // inode:32
	endReq = PutUint32(endReq, index)              // chunkindx:32
	endReq = PutUint64(endReq, currentLength)      // length:64 — UNCHANGED (release only)
	endReq = PutUint8(endReq, 0)                  // chunkopflags:8 = 0
	endReq = PutUint32(endReq, 0)                 // offset:32 = 0 (no data written)
	endReq = PutUint32(endReq, 0)                 // size:32 = 0 (no data written)

	endAns, err := c.roundtrip(CltomFuseWriteChunkEnd, MatoclFuseWriteChunkEnd, endReq)
	if err != nil {
		return fmt.Errorf("mfsclient: writeChunkRelease(%d, idx=%d): WRITE_CHUNK_END: %w", nodeID, index, err)
	}
	if len(endAns) < 5 {
		return fmt.Errorf("mfsclient: writeChunkRelease(%d, idx=%d): response too short (%d)",
			nodeID, index, len(endAns))
	}
	if endAns[4] != StatusOK {
		return fmt.Errorf("mfsclient: writeChunkRelease(%d, idx=%d): status 0x%02x",
			nodeID, index, endAns[4])
	}
	return nil
}

// Write writes data to file node nodeID at offset using the real MooseFS
// chunk-server protocol with a 4-strategy cascade fallback.
//
// The cascade iterates over defaultWriteStrategies in order:
//  1. Sync chain CS1 — preferred; CS1 replicates synchronously to CS2+.
//  2. Sync chain CS2 — CS1 unreachable; CS2 is the replication head.
//  3. Async CS1 — sync chain broken (CANTCONNECT); CS1 writes locally, master replicates.
//  4. Async CS2 — last resort; CS1 down, async write to CS2.
//
// For each strategy:
//  1. Acquire a fresh write lock via writeChunkLock() (new LockID per attempt).
//  2. DialCS → WriteChunk with the appropriate chain (nil = async, Servers[csIdx+1:] = sync).
//  3. On success: writeChunkEnd() to commit the new file length → return nil.
//  4. On failure: writeChunkRelease() to free the master lock, try the next strategy.
//
// Returns an error only if all strategies are exhausted.
func (c *Client) Write(nodeID uint32, offset uint64, data []byte) error {
	index := uint32(offset / ChunkSize)
	chunkOffset := uint32(offset % ChunkSize)

	// Phase 1: acquire the initial write lock to get the server list.
	info, err := c.writeChunkLock(nodeID, index)
	if err != nil {
		return err
	}

	var lastErr error
	firstActive := true // the initial lock (info) has not been consumed yet

	for si, strat := range defaultWriteStrategies {
		// Skip strategies that require a server index beyond what the master returned.
		if strat.csIdx >= len(info.Servers) {
			continue
		}

		// Re-acquire the lock for every strategy after the first applicable one.
		// Each attempt needs a fresh LockID from the master.
		if !firstActive {
			info, err = c.writeChunkLock(nodeID, index)
			if err != nil {
				return fmt.Errorf("mfsclient: Write(%d, off=%d): re-lock strategy %d: %w",
					nodeID, offset, si+1, err)
			}
			// Guard: master may return fewer servers on a subsequent lock.
			// Release the fresh lock before skipping to avoid a ~60 s master timeout.
			if strat.csIdx >= len(info.Servers) {
				if releaseErr := c.writeChunkRelease(info, nodeID, index, info.Length); releaseErr != nil {
					logger.Warn("[mfsclient] Write: writeChunkRelease after shrink guard (chunk %d, strategy %d): %v",
						index, si+1, releaseErr)
				}
				continue
			}
		}
		firstActive = false

		srv := info.Servers[strat.csIdx]
		csIP := net.IP([]byte{byte(srv.IP >> 24), byte(srv.IP >> 16), byte(srv.IP >> 8), byte(srv.IP)})
		logger.Debug("[mfsclient] Write: strategy %d/%d: dialing CS %s:%d (csIdx=%d syncChain=%v)",
			si+1, len(defaultWriteStrategies), csIP, srv.Port, strat.csIdx, strat.syncChain)

		// Phase 2: CS I/O — no master lock held here.
		// Pool.Get() returns an idle connection or dials a new one.
		cs, dialErr := c.pool.Get(srv.IP, srv.Port)
		if dialErr != nil {
			lastErr = dialErr
			logger.Warn("[mfsclient] Write: strategy %d/%d CS %s:%d dial failed, trying next: %v",
				si+1, len(defaultWriteStrategies), csIP, srv.Port, dialErr)
			if releaseErr := c.writeChunkRelease(info, nodeID, index, info.Length); releaseErr != nil {
				logger.Warn("[mfsclient] Write: writeChunkRelease after dial failure (chunk %d, strategy %d): %v",
					index, si+1, releaseErr)
			}
			continue
		}

		// Build the replication chain for this strategy.
		// syncChain=true and more servers available → pass remaining servers as peers.
		// syncChain=false or no more servers         → nil chain (async: master replicates).
		var chain []ChunkServer
		if strat.syncChain && strat.csIdx+1 < len(info.Servers) {
			chain = info.Servers[strat.csIdx+1:]
		}

		writeErr := WriteChunk(cs, info.ChunkID, info.Version, chunkOffset, data, chain)
		if writeErr != nil {
			cs.Close() // don't pool a broken connection
			lastErr = writeErr
			logger.Warn("[mfsclient] Write: strategy %d/%d CS %s:%d write failed, trying next: %v",
				si+1, len(defaultWriteStrategies), csIP, srv.Port, writeErr)
			if releaseErr := c.writeChunkRelease(info, nodeID, index, info.Length); releaseErr != nil {
				logger.Warn("[mfsclient] Write: writeChunkRelease after write failure (chunk %d, strategy %d): %v",
					index, si+1, releaseErr)
			}
			continue
		}
		c.pool.Put(cs, srv.IP, srv.Port)

		// Phase 3: CS write succeeded — commit the new file length to the master.
		return c.writeChunkEnd(info, nodeID, index, offset, len(data))
	}

	if lastErr != nil {
		return fmt.Errorf("mfsclient: Write(%d, off=%d): all write strategies failed, last error: %w",
			nodeID, offset, lastErr)
	}
	return fmt.Errorf("mfsclient: Write(%d, off=%d): no applicable write strategies (no servers available)",
		nodeID, offset)
}

// WriteChunkData writes data for a single MooseFS chunk.  It is a thin wrapper
// around Write() that enforces the single-chunk boundary constraint: both
// offset and offset+len(data) must fall within the same 64 MiB chunk.
//
// This is the preferred entry point for the upload pipeline: callers group
// their I/O buffers by MooseFS chunk index (fileOffset / ChunkSize) and call
// WriteChunkData once per chunk, enabling parallel chunk uploads.
//
// Returns an error if the write would cross a chunk boundary.
func (c *Client) WriteChunkData(nodeID uint32, offset uint64, data []byte) error {
	if len(data) == 0 {
		return nil // no-op: nothing to write, skip master round-trip
	}
	chunkOffset := offset % ChunkSize
	if chunkOffset+uint64(len(data)) > ChunkSize {
		return fmt.Errorf("mfsclient: WriteChunkData(node=%d, off=%d, len=%d): data crosses chunk boundary (ChunkSize=%d)",
			nodeID, offset, len(data), ChunkSize)
	}
	return c.Write(nodeID, offset, data)
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
			// A MooseFS master may return a proto response with nCS=0 (rather
			// than a 5-byte StatusOK) when the requested chunk slot is at
			// exactly the file boundary — this happens when the file size is a
			// precise multiple of ChunkSize (e.g. exactly 64 MiB).  In this
			// case info.Length == offset, and the correct interpretation is EOF.
			if info.Length > 0 && offset >= info.Length {
				return nil, nil // treat as EOF — caller (Download) will stop iterating
			}
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

	// EC routing: delegate EC4+1 chunks to readEC4At (shard-granular read).
	// EC8+2 and other EC configurations are not yet supported.
	if info.ECParts == 4 {
		return c.readEC4At(info, index, chunkOffset, size)
	}
	if info.ECParts != 0 {
		return nil, fmt.Errorf("mfsclient: Read(%d): EC%d+%d not supported (only EC4+1 implemented)",
			nodeID, info.ECParts, info.ECParts/4)
	}

	// Phase 2: I/O chunk server hors mutex — c.conn n'est pas utilisé ici.
	//
	// Retry-once policy for stale pool connections:
	//   A pooled connection may have been closed server-side (CS idle timeout,
	//   network interruption) between two consecutive reads.  On the first
	//   attempt, if ReadChunk returns a stale-connection error (EOF, reset…),
	//   the bad connection is discarded and a fresh one is dialled immediately.
	//   This prevents callers from receiving EIO and retrying the Open() in a
	//   tight loop (Windows Explorer behaviour) — which manifested as a storm
	//   of "parseChunkInfo" debug log lines with no download progress (#112).
	srv := info.Servers[0]
	for attempt := 0; attempt < 2; attempt++ {
		cs, dialErr := c.pool.Get(srv.IP, srv.Port)
		if dialErr != nil {
			return nil, fmt.Errorf("mfsclient: Read(%d, off=%d): dial CS: %w", nodeID, offset, dialErr)
		}
		result, readErr := ReadChunk(cs, info.ChunkID, info.Version, chunkOffset, size)
		if readErr != nil {
			cs.Close() // don't pool a broken connection
			if attempt == 0 && isStaleConnErr(readErr) {
				// Stale pooled connection — discard and retry with a fresh dial.
				logger.Debug("[mfsclient] Read(%d, off=%d): stale CS connection on attempt 1, retrying: %v",
					nodeID, offset, readErr)
				continue
			}
			return nil, readErr
		}
		c.pool.Put(cs, srv.IP, srv.Port)
		return result, nil
	}
	// Unreachable: attempt 1 always returns (either success or non-stale error
	// that falls through to return nil, readErr above).
	return nil, fmt.Errorf("mfsclient: Read(%d, off=%d): CS I/O failed after 2 attempts", nodeID, offset)
}

// ─── Internal: chunk info parsing ────────────────────────────────────────────

// parseChunkInfo decodes the master response for READ_CHUNK (433) and
// WRITE_CHUNK (435). Four protocol variants are supported, detected from the
// byte at offset 4 (immediately after msgid):
//
//	Proto 3 (MooseFS >= 4.0.0) — protocolid byte = 3:
//	  [msgid:32][protocolid:8=3][length:64][chunkid:64][version:32]
//	  (4 or 8) × [ip:32 port:16 cs_ver:32 labelmask:32]  (14 bytes/entry, N implicit)
//	  Used for erasure-coded chunks split into 4 or 8 independent parts.
//	  Wire format identical to proto 2; differs only in the N constraint and
//	  the semantics (each server holds a distinct EC shard, not a full replica).
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
		err        error
		chunkID    uint64
		version    uint32
		fileLength uint64 // current file length before write — stored in ChunkInfo.Length
		entrySize  int
	)

	switch protocolID {
	case 1, 2, 3:
		// Proto 1/2/3: consume protocolid, then [length:64][chunkid:64][version:32].
		off++ // consume protocolid byte
		fileLength, off, err = ReadUint64(ans, off) // file length — stored in ChunkInfo.Length
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
		case 2, 3:
			entrySize = 14 // ip:32 + port:16 + cs_ver:32 + labelmask:32
		}

	case 0:
		// Proto 0: no protocolid byte; off is still at 4 (start of [length:64]).
		// The byte at offset 4 is the MSB of the 64-bit file length (always 0
		// for files < 72 PiB), not a separate protocol identifier.
		fileLength, off, err = ReadUint64(ans, off) // file length — stored in ChunkInfo.Length
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
		// Unknown protocolid — a future MooseFS protocol version that GhostDrive
		// does not yet support.  Falling back to proto 0 would silently misparse
		// the response and produce incorrect chunk server lists.
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
		Length:  fileLength,
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

	// Proto 3: erasure-coded chunk (4 or 8 independent shards).
	// Set ECParts so that Client.Read() can route to readEC4At.
	// N=0 is a degenerate case (no servers available) — ECParts stays 0 and
	// the existing nCS=0 guard in Read() handles it downstream.
	if protocolID == 3 && len(info.Servers) > 0 {
		info.ECParts = len(info.Servers)
		logger.Debug("mfsclient: parseChunkInfo: proto=3 EC chunk chunkID=%d ECParts=%d",
			info.ChunkID, info.ECParts)
	}

	// MooseFS 4.x may append a lockid:32 token after the CS entries.
	// Any trailing 4 bytes that don't form a complete CS entry are parsed as a
	// lockid and stored in ChunkInfo.LockID for diagnostics.
	// NOTE: this client does NOT echo the lockid back in WRITE_CHUNK_END — confirmed
	// against MooseFS 4.58.4 (all QUALIF tests pass on real cluster without it).
	// Diagnostic: trailing=4 → lockid present; trailing=0 → omitted by master.
	logger.Debug("mfsclient: parseChunkInfo: len=%d off=%d trailing=%d proto=%d nCS=%d",
		len(ans), off, len(ans)-off, protocolID, len(info.Servers))
	if len(ans)-off == 4 {
		info.LockID, _, _ = ReadUint32(ans, off)
	}

	return info, nil
}
