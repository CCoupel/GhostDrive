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
//	                                        CS may interleave ANTOAN_NOP (0) keepalives
//	                                        at any point before READ_STATUS — observed
//	                                        while the CS is flushing a slow disk read
//	                                        (issue #160). ReadChunk skips them silently,
//	                                        bounded by maxConsecutiveNOPs so a CS emitting
//	                                        nothing but keepalives cannot hang the caller.
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
	"sync"
	"syscall"
	"time"

	"github.com/CCoupel/GhostDrive/internal/backoff"
	"github.com/CCoupel/GhostDrive/internal/logger"
)

// ── CS Connection Pool ────────────────────────────────────────────────────────
//
// On Windows, closing a TCP socket does not free the local port immediately —
// it enters TIME_WAIT for ~4 minutes (default TcpTimedWaitDelay).  With
// chunkSize = 64 KiB in Download(), reading a 100 MB file opens and closes
// ~1 600 connections to the same CS, exhausting the ephemeral port range
// (49 152–65 535) and triggering WSAEADDRINUSE (error 10048).
//
// csPool reuses idle connections to each chunk server so that each active
// download/upload session typically holds only one TCP connection.
// A connection is returned to the pool only after a successful operation;
// on error the caller closes it directly so broken sockets are never pooled.

// maxIdleCSConns is the maximum number of idle connections kept per CS address.
// 4 matches the default upload pipeline concurrency (uploadConcurrency).
const maxIdleCSConns = 4

// csPool is a thread-safe pool of idle TCP connections to chunk servers.
type csPool struct {
	mu     sync.Mutex
	idle   map[string][]net.Conn // key: "a.b.c.d:port"
	max    int
	maxAge time.Duration // 0 → use maxIdleConnAge; overridable via newCSPoolWithMaxAge (tests)
}

func newCSPool() *csPool {
	return &csPool{
		idle: make(map[string][]net.Conn),
		max:  maxIdleCSConns,
	}
}

// newCSPoolWithMaxAge returns a csPool identical to newCSPool() except that
// Get() evicts idle connections older than maxAge instead of the default
// maxIdleConnAge. Exists so tests can exercise the max-idle-age eviction path
// (CA8) without waiting maxIdleConnAge (60s) in real time; newCSPool() and
// all its callers are unaffected.
func newCSPoolWithMaxAge(maxAge time.Duration) *csPool {
	return &csPool{
		idle:   make(map[string][]net.Conn),
		max:    maxIdleCSConns,
		maxAge: maxAge,
	}
}

// csAddr formats ip (uint32, big-endian) and port as "a.b.c.d:port".
func csAddr(ip uint32, port uint16) string {
	b := [4]byte{byte(ip >> 24), byte(ip >> 16), byte(ip >> 8), byte(ip)}
	return fmt.Sprintf("%d.%d.%d.%d:%d", b[0], b[1], b[2], b[3], port)
}

// agedConn wraps a connection returned to the pool with the time it was put
// back, so Get() can detect and discard connections that have been idle long
// enough that the chunk server has likely already closed them server-side
// (#160 D5) — a MooseFS CS enforces its own idle timeout on client sockets,
// and without TCP keepalive (see DialCS) a connection idle past that timeout
// is stale by construction even though it still looks fine locally.
//
// Get() always hands back the unwrapped inner net.Conn — only Put()/Get()
// know about ages; callers see a plain net.Conn exactly as before.
type agedConn struct {
	net.Conn
	putAt time.Time
}

// maxIdleConnAge is the maximum time a connection may sit idle in the pool
// before Get() discards it instead of serving it. MooseFS chunk server idle
// timeout configuration is not available to this project (read-only cluster
// access, see CLAUDE.md), so this value is a conservative default that
// favours an extra redial over silently serving a stale socket. Tune upward
// if production logs show an unwarranted increase in dial rate.
const maxIdleConnAge = 60 * time.Second

// Get returns an idle connection from the pool, or dials a new one if none
// is available or the only idle candidates are older than maxIdleConnAge.
func (p *csPool) Get(ip uint32, port uint16) (net.Conn, error) {
	key := csAddr(ip, port)
	for {
		p.mu.Lock()
		conns := p.idle[key]
		if len(conns) == 0 {
			p.mu.Unlock()
			// Dial outside the lock — establishing a TCP connection can be slow.
			return DialCS(ip, port)
		}
		conn := conns[len(conns)-1]
		p.idle[key] = conns[:len(conns)-1]
		p.mu.Unlock()

		ac, wrapped := conn.(*agedConn)
		if !wrapped {
			// No age information (e.g. injected directly, as some tests do) —
			// treat as fresh.
			return conn, nil
		}
		age := p.maxAge
		if age <= 0 {
			age = maxIdleConnAge
		}
		if time.Since(ac.putAt) > age {
			_ = ac.Conn.Close() // too old — discard and try the next idle connection (or dial fresh)
			continue
		}
		return ac.Conn, nil
	}
}

// Put returns a healthy connection to the pool, timestamped for maxIdleConnAge.
// If the pool for this address is already full, the connection is closed.
// Callers MUST NOT call Put after an error — close the connection directly.
func (p *csPool) Put(conn net.Conn, ip uint32, port uint16) {
	key := csAddr(ip, port)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.idle[key]) < p.max {
		p.idle[key] = append(p.idle[key], &agedConn{Conn: conn, putAt: time.Now()})
	} else {
		_ = conn.Close() // pool full — discard
	}
}

// CloseAll closes every idle connection and empties the pool.
// Called when the owning Client is closed.
func (p *csPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conns := range p.idle {
		for _, c := range conns {
			_ = c.Close()
		}
	}
	p.idle = make(map[string][]net.Conn)
}

// errUnexpectedCmd is the sentinel wrapped by ReadChunk when the chunk server
// responds with a command opcode that is neither a data/status frame nor the
// ANTOAN_NOP (0) keepalive — i.e. a genuine protocol desync or an opcode this
// client does not know how to handle in a CS read sequence.
//
// ANTOAN_NOP is NOT wrapped in this sentinel: it is a legitimate keepalive,
// silently skipped by ReadChunk (bounded by maxConsecutiveNOPs). A truly
// unexpected opcode is a fatal, non-retryable condition — dialling a fresh
// connection would not change the CS's behaviour, since the CS itself is not
// confused, the client's frame parsing is.
//
// Historical note (#160): an earlier version of this comment claimed that
// cmd=0 indicated a half-closed socket leaving zeros in the OS TCP buffer.
// That diagnosis was physically impossible — ReadFrame reads the header via
// io.ReadFull, and a half-closed socket produces io.EOF / ErrUnexpectedEOF,
// never 8 zero bytes — and led isStaleConnErr to treat every legitimate NOP
// keepalive as a "stale connection", masking the real bug (a missing NOP
// skip) behind a retry that could never converge. Kept as a sentinel (rather
// than a string match) so callers can use errors.Is regardless of wrapping.
var errUnexpectedCmd = errors.New("unexpected response cmd")

// errServerStatus is the sentinel wrapped by ReadChunk when the chunk server
// answers CSTOCL_READ_STATUS with a non-OK status.
//
// This is deliberately NOT matched by isStaleConnErr: most non-OK statuses
// are genuine application-level failures (CRC mismatch reported by the
// server, permission/data errors) that a retry — with or without a fresh
// dial — cannot fix, so they must fail fast (CA7/CA8).
//
// But since #163 introduced a chunk-location cache (chunklocationcache.go),
// one specific cause of a non-OK status changed meaning: the chunk server
// legitimately no longer having the chunk this Client asked for, because its
// location was cached up to chunkLocationCacheTTL ago and has since moved
// (rebalance, rewrite, another client). doCSRead grants errServerStatus
// exactly ONE forced chunk-location refresh (never the full retry/backoff
// budget isStaleConnErr-classified errors get) before treating it as
// terminal — see doCSRead's dedicated branch. A persistent, non-location
// status rejection still fails fast on the second occurrence.
var errServerStatus = errors.New("chunk server rejected request")

// isStaleConnErr reports whether err indicates a stale TCP connection —
// one that was pooled successfully but later closed or timed out on the
// remote side (server-side idle timeout, OS keepalive expiry, network
// interruption, or a deadline set by this client — see maxIdleConnAge and
// the CS I/O deadlines in ReadChunk/WriteChunk).
//
// Deliberately does NOT match errUnexpectedCmd (#160 — see its comment) nor
// errServerStatus (#163 — see its comment; that error gets its own bounded
// one-shot handling in doCSRead, not the general connection-retry budget)
// nor any other application/protocol-level error (CRC mismatch): those are
// not connection problems and retrying them changes nothing, so they must
// fail fast (CA7) instead of consuming retry budget.
//
// Detection is typed, not string-matched: matching on error text (as this
// function used to) is fragile and silently absorbs errors it was never
// meant to, such as the earlier "read frame header" / "write frame header"
// substrings, which matched non-retryable failures too.
func isStaleConnErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) {
		// syscall.ECONNABORTED is Go's portable alias for WSAECONNABORTED on
		// Windows (both syscall packages define matching Errno values), so
		// this single check covers the WSAECONNABORTED 10053 seen in #160
		// production logs without needing a windows-only build tag.
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// readerGracePeriod is the maximum time the WriteChunk sender goroutine waits
// for the reader goroutine to surface a protocol-level error (e.g. DISCONNECTED
// STATUS) when a transport-level write failure (e.g. "connection reset by peer")
// occurs first.  This yields a more actionable error message to callers.
const readerGracePeriod = 50 * time.Millisecond

// ── CS I/O deadlines (#160 D4) ─────────────────────────────────────────────
//
// No net.Dial timeout, read/write deadline, or TCP keepalive existed anywhere
// in this plugin before #160: a CS that stopped responding blocked the caller
// indefinitely (the "blocage complet" reported in the issue). These constants
// bound every CS I/O path without needing per-call tuning.

const (
	// csDialTimeout bounds how long DialCS waits for the TCP handshake.
	csDialTimeout = 5 * time.Second
	// csKeepAlive is the OS-level TCP keepalive period on CS connections, so
	// idle pooled connections are detected (and the OS informed) before a
	// silent server-side close leaves a half-open socket in the pool.
	csKeepAlive = 30 * time.Second
	// csInactivityTimeout bounds the *gap* between consecutive frames on a CS
	// connection (reset before every read/write, both in ReadChunk and
	// WriteChunk), NOT the total call duration. This mirrors the official
	// MooseFS client's model — CHUNKSERVER_ACTIVITY_TIMEOUT 5.0
	// (readdata.c:78, evaluated continuously at :1395) — deliberately chosen
	// over a single deadline covering the whole call: a slow read or a large
	// upload that keeps *progressing* must never be killed, only a CS that
	// goes silent should be. A single static value is shared by both
	// directions; ReadChunk and WriteChunk each reset it independently, so
	// tuning stays simple without risking the read-side value starving a
	// legitimately long write (the risk flagged in the bugfix #160 plan for
	// the WriteChunk reader goroutine, csclient.go).
	csInactivityTimeout = 5 * time.Second
)

// ── Retry policy (#160 D3, D3bis, D6) ──────────────────────────────────────
//
// Shared by doCSRead (Client.Read, readEC4At). Previously each caller
// implemented its own "retry-once, reuse the pool, same cached location"
// loop: no guaranteed fresh dial (D3), no invalidation of the chunk location
// before retrying (D3bis — GhostDrive kept re-attacking the very CS that had
// just failed instead of asking the master for a fresh placement), and no
// bound on cumulative wait (D6).

// maxCSAttempts is the maximum number of CS I/O attempts per doCSRead call
// (1 initial + up to maxCSAttempts-1 retries). Kept well below the 30-attempt
// threshold at which the MooseFS backoff schedule (internal/backoff) would
// hit its own cap, since each retry here also costs a master round-trip
// (locateFunc re-query) on top of the backoff sleep.
const maxCSAttempts = 6

// maxRetryBudget bounds the *cumulative* backoff wait across all attempts of
// a single doCSRead call, on top of the internal/backoff.MooseFS schedule's
// own per-step values. Deliberately kept far below the interval at which
// Windows Cloud Filter API / WinFsp reissues a failed placeholder Open()
// (observed at roughly a second or more in #160 production logs) — if the
// low-level retry here took as long as that, it would stack with the
// higher-level retry instead of transparently absorbing a transient failure,
// multiplying perceived latency rather than hiding it (see plan risk table,
// bugfix #160).
const maxRetryBudget = 2 * time.Second

// csLocateResult is what a locateFunc resolves for one doCSRead attempt.
type csLocateResult struct {
	ip   uint32
	port uint16
	// read performs the actual chunk-server operation once connected.
	read func(cs net.Conn) ([]byte, error)
	// eof, when true, tells doCSRead to return (nil, nil) immediately without
	// attempting any I/O — used when a retry's re-query discovers the file
	// has shrunk past the requested offset in the meantime.
	eof bool
}

// locateFunc resolves the chunk-server address (and the operation to run
// against it) for one doCSRead attempt. attempt is 0 for the first try and
// increases by one per retry. Implementations MUST re-resolve the location
// from the master — rather than reusing a cached ChunkInfo — whenever
// attempt > 0 (#160 D3bis / CA7): retrying against the same cached server
// that just failed is exactly what let #160 degrade into a storm of
// failures against one bad CS instead of failing over.
type locateFunc func(attempt int) (csLocateResult, error)

// doCSRead executes a chunk-server read with the retry policy shared by
// Client.Read (normal chunks) and readEC4At (EC4+1 shard reads), implementing
// invariants 5-11 of docs/diagrams/moosefs-ec4-read-statemachine.md:
//
//   - attempt 0 may reuse a pooled connection; every later attempt forces a
//     freshly dialled connection, bypassing the pool entirely (CA6) — a
//     pooled connection that just failed is never handed out again blindly.
//   - every attempt after the first re-resolves the chunk location via
//     locate (CA7) instead of re-attacking the same cached server.
//   - non-retryable errors (isStaleConnErr == false, e.g. CRC mismatch or a
//     truly unexpected opcode) fail immediately without consuming any retry
//     budget (CA8).
//   - errServerStatus (a non-OK CSTOCL_READ_STATUS) gets exactly ONE forced
//     chunk-location refresh — never the full retry/backoff budget — since
//     #163's chunk-location cache means this specific error can legitimately
//     be a stale cached placement, not just a data/permission failure. See
//     errServerStatus's comment.
//   - retryable (stale-connection) errors back off per the official MooseFS
//     schedule (internal/backoff.MooseFS), with a hard ceiling on the
//     cumulative wait for the whole call (CA8).
//
// opDesc identifies the caller for error messages (e.g. "Read(node=5, off=0)"
// or "readEC4At chunkID=42 shard=1").
func doCSRead(pool *csPool, opDesc string, locate locateFunc) ([]byte, error) {
	var lastErr error
	var waited time.Duration
	attemptsMade := 0
	// #163 code-review MAJOR fix: bounds the errServerStatus special case
	// (below) to exactly one forced chunk-location refresh, never a loop —
	// a persistent, non-location status rejection must still fail fast on
	// its second occurrence rather than burning through the retry budget.
	locationRefreshUsed := false

	for i := 0; i < maxCSAttempts; i++ {
		if i > 0 {
			d := backoff.MooseFS.Delay(i)
			if waited+d > maxRetryBudget {
				break // budget exhausted — stop retrying, report lastErr below
			}
			time.Sleep(d)
			waited += d
		}
		attemptsMade++

		loc, locErr := locate(i)
		if locErr != nil {
			lastErr = fmt.Errorf("locate: %w", locErr)
			continue
		}
		if loc.eof {
			return nil, nil
		}

		var cs net.Conn
		var dialErr error
		if i == 0 {
			cs, dialErr = pool.Get(loc.ip, loc.port) // may legitimately reuse a pooled connection
		} else {
			cs, dialErr = DialCS(loc.ip, loc.port) // CA6 — never re-serve the pool on retry
		}
		if dialErr != nil {
			lastErr = fmt.Errorf("dial CS: %w", dialErr)
			continue
		}

		result, readErr := loc.read(cs)
		if readErr == nil {
			pool.Put(cs, loc.ip, loc.port)
			return result, nil
		}
		cs.Close() // never pool a broken connection

		if errors.Is(readErr, errServerStatus) {
			// A non-OK status can mean this call's cached chunk location
			// (#163 chunklocationcache.go) is stale — grant exactly one
			// forced relocate (the next locate(attempt) call, attempt>0,
			// always forces a fresh master query) before giving up. This is
			// deliberately NOT the same as isStaleConnErr's full retry
			// budget: a status rejection that is NOT about location (e.g. a
			// server-side data/permission error) must still fail fast once
			// the one relocate attempt has been given.
			if !locationRefreshUsed {
				locationRefreshUsed = true
				lastErr = readErr
				continue
			}
			return nil, fmt.Errorf("mfsclient: %s: %w", opDesc, readErr)
		}

		if !isStaleConnErr(readErr) {
			return nil, fmt.Errorf("mfsclient: %s: %w", opDesc, readErr) // CA8 — fail fast
		}
		lastErr = readErr
	}

	return nil, fmt.Errorf("mfsclient: %s: CS I/O failed after %d attempt(s), retry budget %v: %w",
		opDesc, attemptsMade, waited, lastErr)
}

// DialCS opens a TCP connection to the MooseFS chunk server at the given IP
// address (uint32 big-endian network byte order) and port, bounded by
// csDialTimeout and with OS-level TCP keepalive enabled (csKeepAlive).
// Returns a raw net.Conn ready for ReadChunk or WriteChunk.
func DialCS(ip uint32, port uint16) (net.Conn, error) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, ip)
	addr := fmt.Sprintf("%d.%d.%d.%d:%d", b[0], b[1], b[2], b[3], port)
	dialer := net.Dialer{Timeout: csDialTimeout, KeepAlive: csKeepAlive}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("csclient: dial %s: %w", addr, err)
	}
	return conn, nil
}

// maxConsecutiveNOPs bounds how many ANTOAN_NOP keepalives ReadChunk tolerates
// in a row (i.e. without an intervening READ_DATA/READ_STATUS frame) before
// treating the chunk server as unresponsive. A legitimate CS sends at most a
// handful of NOPs while flushing a slow read; a CS emitting nothing else
// (protocol desync, or a server bug) must not be able to hang the caller in
// an unbounded loop — a server-controlled loop condition is a DoS vector by
// construction (invariant 2, docs/diagrams/moosefs-ec4-read-statemachine.md).
const maxConsecutiveNOPs = 64

// ReadChunk reads size bytes starting at offset within chunk chunkID/version
// from the chunk server connection cs.
//
// Internally it sends one CLTOCS_READ frame and collects all CSTOCL_READ_DATA
// frames until CSTOCL_READ_STATUS is received.  Returns the concatenated data.
// An empty (nil) result indicates that the requested offset is past EOF for
// that chunk.
//
// The CS may interleave ANTOAN_NOP (0) keepalives at any point before
// CSTOCL_READ_STATUS; these are skipped transparently (#160), bounded by
// maxConsecutiveNOPs and by csInactivityTimeout (rearmed on every frame
// received — see its comment for why this is not a single global deadline).
func ReadChunk(cs net.Conn, chunkID uint64, version uint32, offset uint32, size uint32) ([]byte, error) {
	// Clear any deadline before returning so a connection later handed back
	// to the pool (or reused directly) does not inherit a stale deadline
	// from this call.
	defer cs.SetDeadline(time.Time{})

	// Build and send CLTOCS_READ request.
	var payload []byte
	payload = PutUint64(payload, chunkID)
	payload = PutUint32(payload, version)
	payload = PutUint32(payload, offset)
	payload = PutUint32(payload, size)

	if err := cs.SetWriteDeadline(time.Now().Add(csInactivityTimeout)); err != nil {
		return nil, fmt.Errorf("csclient: ReadChunk %d: set write deadline: %w", chunkID, err)
	}
	if err := WriteFrame(cs, CltocsFuseRead, payload); err != nil {
		return nil, fmt.Errorf("csclient: ReadChunk %d: send: %w", chunkID, err)
	}

	// Collect READ_DATA frames until READ_STATUS.
	var result []byte
	nopCount := 0
	for {
		// Inactivity deadline, rearmed on every frame received (DATA or NOP)
		// — see csInactivityTimeout: a read that keeps progressing must never
		// be killed, only a connection that goes silent.
		if err := cs.SetReadDeadline(time.Now().Add(csInactivityTimeout)); err != nil {
			return nil, fmt.Errorf("csclient: ReadChunk %d: set read deadline: %w", chunkID, err)
		}
		cmd, data, err := ReadFrame(cs)
		if err != nil {
			return nil, fmt.Errorf("csclient: ReadChunk %d: recv: %w", chunkID, err)
		}

		if cmd != ANTOAN_NOP {
			nopCount = 0 // only *consecutive* NOPs count against the flood guard
		}

		switch cmd {
		case ANTOAN_NOP:
			// Legitimate keepalive (#160) — skip it and keep reading, after
			// validating it carries no payload (readdata.c:1683: a non-empty
			// NOP is a real protocol error, not a keepalive). See the package
			// comment and errUnexpectedCmd for why this used to be (wrongly)
			// treated as a fatal "stale connection" error.
			if len(data) != 0 {
				return nil, fmt.Errorf("csclient: ReadChunk %d: malformed ANTOAN_NOP (peer=%s, length=%d, want 0)",
					chunkID, cs.RemoteAddr(), len(data))
			}
			nopCount++
			if nopCount > maxConsecutiveNOPs {
				return nil, fmt.Errorf("csclient: ReadChunk %d: exceeded %d consecutive ANTOAN_NOP keepalives"+
					" (peer=%s unresponsive or protocol desync)", chunkID, maxConsecutiveNOPs, cs.RemoteAddr())
			}
			continue

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
				return nil, fmt.Errorf("csclient: ReadChunk %d: server status 0x%02x: %w", chunkID, status, errServerStatus)
			}
			return result, nil

		default:
			// A genuinely unrecognised opcode (not NOP, DATA or STATUS) means
			// the client and CS have desynchronised on the frame stream, or the
			// CS sent something this client does not implement. This is fatal
			// and non-retryable (CA4) — see errUnexpectedCmd for why it is
			// deliberately NOT treated as a stale-connection signal. The peer
			// address is logged (readdata.c:1694) to make any residual
			// desync diagnosable instead of an anonymous "cmd %d".
			return nil, fmt.Errorf("csclient: ReadChunk %d: unexpected response cmd %d (peer=%s): %w",
				chunkID, cmd, cs.RemoteAddr(), errUnexpectedCmd)
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
	// Clear any deadline before returning so a connection later handed back
	// to the pool (or reused directly) does not inherit a stale absolute
	// deadline from this call. Unlike ReadChunk (a single small block, bounded
	// by one global deadline), WriteChunk legitimately runs for as long as the
	// whole chunk takes to transfer, so every I/O below resets its own
	// deadline (csInactivityTimeout) instead of sharing one fixed absolute
	// deadline for the entire call — see the goroutines below.
	defer cs.SetDeadline(time.Time{})

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

	if err := cs.SetWriteDeadline(time.Now().Add(csInactivityTimeout)); err != nil {
		return fmt.Errorf("csclient: WriteChunk %d: set write deadline: %w", chunkID, err)
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
		// Idle deadline, reset before every frame: bounds the *gap* between
		// frames (a mute CS — invariant 9), not the total ACK wait, since a
		// chain with several peers may legitimately need a few keepalives.
		if err := cs.SetReadDeadline(time.Now().Add(csInactivityTimeout)); err != nil {
			return fmt.Errorf("csclient: WriteChunk %d: set read deadline: %w", chunkID, err)
		}
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

	// 3–5. Pipeline: sender and reader run concurrently.
	//
	// The reader goroutine continuously reads WRITE_STATUS frames from the CS
	// while the sender sends WRITE_DATA frames.  This allows early detection of
	// non-OK statuses (e.g. DISCONNECTED) sent by the CS before WRITE_END is
	// received, avoiding unnecessary data transmission.
	//
	// Channel semantics:
	//   sendDone    — closed by sender after WRITE_END is sent (sync.Once, idempotent).
	//   earlyErr    — buffered(1): reader → sender signal for a non-OK STATUS.
	//   finalResult — buffered(1): reader → caller with the final STATUS result.
	const blockSize = 65536
	total := uint32(len(data))
	var writeID uint32

	sendDone    := make(chan struct{})
	earlyErr    := make(chan error, 1)
	finalResult := make(chan error, 1)

	var closeOnce sync.Once
	closeSendDone := func() {
		closeOnce.Do(func() { close(sendDone) })
	}
	defer closeSendDone() // always close — prevents reader goroutine from blocking forever

	// Reader goroutine: collects WRITE_STATUS frames from the CS concurrently
	// with the sender.
	go func() {
		for {
			// Idle deadline, reset before every frame — see the comment at the
			// top of WriteChunk. SetReadDeadline is safe to call concurrently
			// with the sender goroutine's SetWriteDeadline calls below (both
			// only update independent read/write deadline state on the conn).
			if err := cs.SetReadDeadline(time.Now().Add(csInactivityTimeout)); err != nil {
				err = fmt.Errorf("csclient: WriteChunk %d: set read deadline: %w", chunkID, err)
				select { case earlyErr <- err: default: }
				finalResult <- err
				return
			}
			cmd, resp, err := ReadFrame(cs)
			if err != nil {
				if errors.Is(err, io.EOF) {
					err = fmt.Errorf("csclient: WriteChunk %d: CS closed without final WRITE_STATUS"+
						" (check CS logs for crash or OOM): %w", chunkID, err)
				} else {
					err = fmt.Errorf("csclient: WriteChunk %d: recv status: %w", chunkID, err)
				}
				select { case earlyErr <- err: default: }
				finalResult <- err
				return
			}
			logger.Debug("csclient: WriteChunk %d: reader cmd=%d resp_len=%d", chunkID, cmd, len(resp))
			if cmd == ANTOAN_NOP {
				continue // keepalive — skip
			}
			if cmd != CstoclFuseWriteStatus {
				err = fmt.Errorf("csclient: WriteChunk %d: expected WRITE_STATUS (cmd=%d), got cmd=%d",
					chunkID, CstoclFuseWriteStatus, cmd)
				select { case earlyErr <- err: default: }
				finalResult <- err
				return
			}
			if len(resp) < 13 {
				err = fmt.Errorf("csclient: WriteChunk %d: WRITE_STATUS too short (%d bytes)", chunkID, len(resp))
				select { case earlyErr <- err: default: }
				finalResult <- err
				return
			}
			status := resp[12]

			if status != StatusOK {
				err = fmt.Errorf("csclient: WriteChunk %d: server write status 0x%02x (%s)",
					chunkID, status, CSStatusName(status))
				select { case earlyErr <- err: default: }
				finalResult <- err
				return
			}

			// OK STATUS: wait for sendDone (WRITE_END sent by sender).
			// After sendDone is closed the protocol guarantees that the CS has
			// received WRITE_END and this STATUS is the final one — MooseFS sends
			// exactly one STATUS after WRITE_END.  On a closed channel <-sendDone
			// returns instantly.
			//
			// Note: intermediate OK STATUSes (CS acking DATA blocks early) do not
			// exist in the MooseFS write protocol.  The only STATUS frames are:
			//   1. Write-init ACK (handled in phase 2, before this goroutine starts).
			//   2. Final STATUS after WRITE_END (handled here).
			// Mid-stream errors (e.g. DISCONNECTED) are handled by the non-OK branch above.
			<-sendDone
			finalResult <- nil
			return
		}
	}()

	// Sender: send WRITE_DATA frames.
	written := uint32(0)
	for written < total {
		// Non-blocking early-error check from reader.
		select {
		case err := <-earlyErr:
			closeSendDone()
			<-finalResult // drain; reader already wrote the error
			return err
		default:
		}

		pos := offset + written
		blockNum := uint16(pos / blockSize)
		blockOff := uint16(pos % blockSize)

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
		framePayload = PutUint32(framePayload, writeID)
		framePayload = PutUint16(framePayload, blockNum)
		framePayload = PutUint16(framePayload, blockOff)
		framePayload = PutUint32(framePayload, uint32(len(block)))
		framePayload = PutUint32(framePayload, checksum)
		framePayload = append(framePayload, block...)

		// Idle deadline, reset before every frame — see the comment at the
		// top of WriteChunk.
		if err := cs.SetWriteDeadline(time.Now().Add(csInactivityTimeout)); err != nil {
			closeSendDone()
			return fmt.Errorf("csclient: WriteChunk %d: set write deadline (block %d): %w", chunkID, blockNum, err)
		}
		if err := WriteFrame(cs, CltocsFuseWriteData, framePayload); err != nil {
			closeSendDone()
			// Prefer the reader's protocol-level error (e.g. DISCONNECTED STATUS)
			// over the raw transport error — gives callers a more actionable message.
			// The reader always writes to finalResult before exiting, so a short
			// timeout is sufficient even on slow test schedulers.
			writeErr := fmt.Errorf("csclient: WriteChunk %d: send data (block %d): %w", chunkID, blockNum, err)
			timer := time.NewTimer(readerGracePeriod)
			defer timer.Stop()
			select {
			case readerErr := <-finalResult:
				if readerErr != nil {
					return readerErr
				}
				return writeErr
			case <-timer.C:
				return writeErr
			}
		}
		written = end
	}

	// Final early-error check before WRITE_END.
	select {
	case err := <-earlyErr:
		closeSendDone()
		<-finalResult
		return err
	default:
	}

	// 4. Send CLTOCS_WRITE_END.
	var endPayload []byte
	endPayload = PutUint64(endPayload, chunkID)
	endPayload = PutUint32(endPayload, version)

	if err := cs.SetWriteDeadline(time.Now().Add(csInactivityTimeout)); err != nil {
		closeSendDone()
		go func() { <-finalResult }()
		return fmt.Errorf("csclient: WriteChunk %d: set write deadline (end): %w", chunkID, err)
	}
	if err := WriteFrame(cs, CltocsFuseWriteEnd, endPayload); err != nil {
		closeSendDone()
		go func() { <-finalResult }()
		return fmt.Errorf("csclient: WriteChunk %d: send end: %w", chunkID, err)
	}

	// Signal reader that WRITE_END was sent; the next STATUS it receives is final.
	closeSendDone()

	// 5. Wait for reader's final STATUS result.
	return <-finalResult
}
