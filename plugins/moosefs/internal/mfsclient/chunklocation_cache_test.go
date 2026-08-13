// Package mfsclient — tests for the chunk-location cache (issue #163, B1).
//
// Bugfix #163 (_work/reports/plan-20260812-155238.md): Client.Read calls
// locateChunk (a CLTOMA_FUSE_READ_CHUNK round-trip to the master) before
// EVERY read, even though a 64 MiB MooseFS chunk is read in many small
// blocks — the same location is redundantly requested up to 1024 times.
// Phase 1 adds a location cache indexed by (nodeID, chunkIndex), modelled on
// the official client's chunksdatacache. These tests exercise it entirely
// black-box through Client.Read(), counting CLTOMA_FUSE_READ_CHUNK requests
// against a fake master — independent of whatever internal cache type/field
// names Phase 1 lands with (a lesson from #160: internal signatures churned
// several times while tests were being written in parallel).
package mfsclient

import (
	"encoding/binary"
	"hash/crc32"
	"net"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── countingMaster ────────────────────────────────────────────────────────────

// countingMasterLocation is one possible answer to a CLTOMA_FUSE_READ_CHUNK
// request: a single chunk server plus the chunk version the master reports
// for it.
type countingMasterLocation struct {
	ip      uint32
	port    uint16
	version uint32
}

// countingMasterConfig configures serveCountingMaster: a fixed chunkID/
// fileLen (proto=2, single normal chunk — simplest vehicle for the location
// cache itself, which sits above the EC4/non-EC split) and a sequence of
// locations to answer successive READ_CHUNK requests with. The Nth request
// gets locations[N-1]; once the slice is exhausted, the last entry repeats.
type countingMasterConfig struct {
	chunkID uint64
	fileLen uint64

	locations []countingMasterLocation
}

// serveCountingMaster serves REGISTER + READ_CHUNK (proto=2) on conn,
// incrementing callCount on every READ_CHUNK request — the master-roundtrip
// counter these tests assert on (CA1).
func serveCountingMaster(conn net.Conn, cfg *countingMasterConfig, callCount *atomic.Int64) {
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
			resp = PutUint32(resp, 0xC0FFEE01) // sessionID (fixed — no client-visible meaning here)
			resp = PutUint32(resp, 0)          // maxopenfiles (unused)
			_ = WriteFrame(conn, MatoclFuseRegister, resp)

		case CltomFuseReadChunk:
			if len(payload) < 12 {
				return
			}
			msgid, _, _ := ReadUint32(payload, 0)

			n := callCount.Add(1)
			idx := int(n) - 1
			if idx >= len(cfg.locations) {
				idx = len(cfg.locations) - 1
			}
			loc := cfg.locations[idx]

			var resp []byte
			resp = PutUint32(resp, msgid)
			resp = PutUint8(resp, 2) // protocolid = 2 (normal replicated chunk)
			resp = PutUint64(resp, cfg.fileLen)
			resp = PutUint64(resp, cfg.chunkID)
			resp = PutUint32(resp, loc.version)
			resp = PutUint32(resp, loc.ip)
			resp = PutUint16(resp, loc.port)
			resp = PutUint32(resp, 0) // cs_ver
			resp = PutUint32(resp, 0) // labelmask
			_ = WriteFrame(conn, MatoclFuseReadChunk, resp)

		default:
			return
		}
	}
}

// startCountingMaster starts a fake master serving cfg and returns a
// connected, registered Client plus the shared READ_CHUNK call counter.
func startCountingMaster(t *testing.T, cfg *countingMasterConfig) (*Client, *atomic.Int64) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var callCount atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go serveCountingMaster(conn, cfg, &callCount)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	addr := ln.Addr().(*net.TCPAddr)
	c, err := Dial("127.0.0.1", addr.Port)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())
	return c, &callCount
}

// ─── TestChunkLocationCache_HitAvoidsMasterRoundtrip (task 11, CA1) ──────────

// TestChunkLocationCache_HitAvoidsMasterRoundtrip verifies that a second
// Read() at a different offset WITHIN THE SAME CHUNK reuses the cached
// location instead of re-querying the master — the core of B1: today,
// Client.Read calls locateChunk on every single block.
func TestChunkLocationCache_HitAvoidsMasterRoundtrip(t *testing.T) {
	const nodeID = uint32(1)
	const block = uint32(4096)
	const chunkID = uint64(0x111)

	content := makeShardBytes(1, int(block)*3)

	cs := newFakeCSServer()
	ip, port := cs.Start()
	t.Cleanup(cs.Stop)
	cs.SetChunkData(chunkID, content)

	cfg := &countingMasterConfig{
		chunkID:   chunkID,
		fileLen:   uint64(len(content)),
		locations: []countingMasterLocation{{ip: ip, port: port, version: 1}},
	}
	c, callCount := startCountingMaster(t, cfg)

	got1, err := c.Read(nodeID, 0, block)
	require.NoError(t, err)
	assert.Equal(t, content[:block], got1)

	got2, err := c.Read(nodeID, uint64(block), block)
	require.NoError(t, err)
	assert.Equal(t, content[block:2*block], got2)

	got3, err := c.Read(nodeID, uint64(2*block), block)
	require.NoError(t, err)
	assert.Equal(t, content[2*block:3*block], got3)

	assert.Equal(t, int64(1), callCount.Load(),
		"3 reads within the same chunk must issue exactly ONE CLTOMA_FUSE_READ_CHUNK request — the 2nd and 3rd must hit the location cache (CA1)")
}

// ─── TestChunkLocationCache_InvalidatedOnReadError (task 12, CA2) ────────────

// TestChunkLocationCache_InvalidatedOnReadError verifies that when a read
// against a cached location fails, the cache entry is invalidated with the
// FRESH location the #160 retry-relocate path obtains (ecclient.go
// locate()/c.locateChunk, D3bis) — not left pointing at the dead server. A
// later, independent Read() on the same chunk must reuse that fresh entry
// (no extra master round-trip) and must NOT need to retry again.
func TestChunkLocationCache_InvalidatedOnReadError(t *testing.T) {
	const nodeID = uint32(1)
	const block = uint32(4096)
	const chunkID = uint64(0x222)
	content := makeShardBytes(2, int(block))

	// Bad CS: accepts then immediately closes — a retryable EOF (#160).
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

	goodCS := newFakeCSServer()
	goodIP, goodPort := goodCS.Start()
	t.Cleanup(goodCS.Stop)
	goodCS.SetChunkData(chunkID, content)

	cfg := &countingMasterConfig{
		chunkID: chunkID,
		fileLen: uint64(len(content)),
		locations: []countingMasterLocation{
			{ip: badIP, port: badPort, version: 1},
			{ip: goodIP, port: goodPort, version: 1},
		},
	}
	c, callCount := startCountingMaster(t, cfg)

	got1, err := c.Read(nodeID, 0, block)
	require.NoError(t, err, "first Read must succeed via #160's retry-relocate path")
	assert.Equal(t, content, got1)
	require.Equal(t, int64(2), callCount.Load(),
		"first Read must re-query the master once after the initially-cached (bad) location failed")

	got2, err := c.Read(nodeID, 0, block)
	require.NoError(t, err, "second Read must succeed directly, without needing its own retry")
	assert.Equal(t, content, got2)
	assert.Equal(t, int64(2), callCount.Load(),
		"a read error must invalidate the cache entry with the retry's fresh location — a later Read must NOT re-query the master again (CA2)")
}

// ─── TestChunkLocationCache_VersionMismatchRefetches (task 13, CA3) ──────────

// TestChunkLocationCache_VersionMismatchRefetches pins down the invariant
// behind CA3 without assuming a specific wire-level trigger: no dedicated
// "wrong version" chunk-server status exists in protocol.go today, so
// exactly how Phase 1 detects a stale cached Version is an implementation
// choice. What must hold regardless of that choice: once the cache is
// refreshed via the #160 retry-relocate path (reusing the same mechanics as
// TestChunkLocationCache_InvalidatedOnReadError), the chunk server must be
// contacted with the FRESH version the master just reported — never a stale
// version left over from the old cache entry (which a real CS would reject).
func TestChunkLocationCache_VersionMismatchRefetches(t *testing.T) {
	const nodeID = uint32(1)
	const block = uint32(4096)
	const chunkID = uint64(0x333)
	const staleVersion = uint32(1)
	const currentVersion = uint32(2)
	content := makeShardBytes(3, int(block))

	// recordingCS records the version field of every CLTOCS_READ it receives
	// and always serves the real data — this test only needs to observe
	// WHICH version was requested, not simulate CS-side rejection.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var lastVersionSeen atomic.Int64
	var acceptCount atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			acceptCount.Add(1)
			go func(c net.Conn) {
				defer c.Close()
				cmd, payload, readErr := ReadFrame(c)
				if readErr != nil || cmd != CltocsFuseRead || len(payload) < 20 {
					return
				}
				gotChunkID, off, _ := ReadUint64(payload, 0)
				gotVersion, _, _ := ReadUint32(payload, off)
				lastVersionSeen.Store(int64(gotVersion))

				checksum := crc32.ChecksumIEEE(content)
				var dataResp []byte
				dataResp = PutUint64(dataResp, gotChunkID)
				dataResp = PutUint16(dataResp, 0)
				dataResp = PutUint16(dataResp, 0)
				dataResp = PutUint32(dataResp, uint32(len(content)))
				dataResp = PutUint32(dataResp, checksum)
				dataResp = append(dataResp, content...)
				if writeErr := WriteFrame(c, CstoclFuseReadData, dataResp); writeErr != nil {
					return
				}
				var status []byte
				status = PutUint64(status, gotChunkID)
				status = PutUint8(status, StatusOK)
				_ = WriteFrame(c, CstoclFuseReadStatus, status)
			}(conn)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	addr := ln.Addr().(*net.TCPAddr)
	ip := binary.BigEndian.Uint32(addr.IP.To4())
	port := uint16(addr.Port)

	// First master answer is deliberately stale (version 1); a second,
	// independent chunk write elsewhere bumped it to version 2 — the master
	// always tells the truth on a fresh query, only a CACHED entry can lag.
	// Forcing a relocate (via the same bad→good CS pattern as task 12) is
	// the only currently-existing mechanism that refreshes the cache; this
	// test uses it as the vehicle to observe the version that then reaches
	// the chunk server.
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

	cfg := &countingMasterConfig{
		chunkID: chunkID,
		fileLen: uint64(len(content)),
		locations: []countingMasterLocation{
			{ip: badIP, port: badPort, version: staleVersion},
			{ip: ip, port: port, version: currentVersion},
		},
	}
	c, _ := startCountingMaster(t, cfg)

	got, err := c.Read(nodeID, 0, block)
	require.NoError(t, err, "Read must succeed once relocated to the chunk server reporting the current version")
	assert.Equal(t, content, got)
	assert.Equal(t, int64(currentVersion), lastVersionSeen.Load(),
		"the chunk server must be contacted with the FRESH version from the re-query, never a stale cached version (CA3)")
}

// ─── TestChunkLocationCache_ServerStatusRejection* (code review fast-follow, CA3) ──

// startRejectingCSServer starts a fake chunk server that answers every
// CLTOCS_READ with a non-OK CSTOCL_READ_STATUS (StatusERROR) and no
// CSTOCL_READ_DATA frame at all — the real shape of a chunk server refusing
// a request for a chunk/version it no longer has, which is exactly what a
// STALE chunk-location-cache entry produces in production (rebalance,
// rewrite, another client) — as opposed to a connection-level failure
// (closed/refused socket), which TestChunkLocationCache_VersionMismatchRefetches
// already covers via a different mechanism. Returns a counter of how many
// CLTOCS_READ requests this CS received.
func startRejectingCSServer(t *testing.T) (ip uint32, port uint16, requestCount *atomic.Int64) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var count atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				cmd, payload, readErr := ReadFrame(c)
				if readErr != nil || cmd != CltocsFuseRead || len(payload) < 8 {
					return
				}
				count.Add(1)
				chunkID, _, _ := ReadUint64(payload, 0)
				var status []byte
				status = PutUint64(status, chunkID)
				status = PutUint8(status, StatusERROR) // non-OK, no READ_DATA — a real rejection
				_ = WriteFrame(c, CstoclFuseReadStatus, status)
			}(conn)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	addr := ln.Addr().(*net.TCPAddr)
	return binary.BigEndian.Uint32(addr.IP.To4()), uint16(addr.Port), &count
}

// TestChunkLocationCache_ServerStatusRejectionForcesRelocate is the code
// review fast-follow for the [MAJEUR] finding in
// _work/reports/code-reviewer-20260813-093106.md: a stale cached location
// manifests as a chunk-server STATUS rejection, not a connection error —
// isStaleConnErr does not (and must not) match that, so it needs its own
// bounded relocate path in doCSRead (errServerStatus). This test exercises
// exactly that path black-box through Client.Read(): the cached location
// (from the master's first answer) is rejected by the chunk server; the
// SECOND master answer (after the forced relocate) points to a chunk server
// that actually has the data.
func TestChunkLocationCache_ServerStatusRejectionForcesRelocate(t *testing.T) {
	const nodeID = uint32(1)
	const block = uint32(4096)
	const chunkID = uint64(0x444)
	content := makeShardBytes(4, int(block))

	rejectIP, rejectPort, rejectCount := startRejectingCSServer(t)

	goodCS := newFakeCSServer()
	goodIP, goodPort := goodCS.Start()
	t.Cleanup(goodCS.Stop)
	goodCS.SetChunkData(chunkID, content)

	cfg := &countingMasterConfig{
		chunkID: chunkID,
		fileLen: uint64(len(content)),
		locations: []countingMasterLocation{
			{ip: rejectIP, port: rejectPort, version: 1}, // stale — rejected by the CS
			{ip: goodIP, port: goodPort, version: 1},     // fresh, after the forced relocate
		},
	}
	c, callCount := startCountingMaster(t, cfg)

	got, err := c.Read(nodeID, 0, block)
	require.NoError(t, err, "a status rejection must trigger exactly one forced relocate and then succeed")
	assert.Equal(t, content, got)
	assert.Equal(t, int64(2), callCount.Load(),
		"exactly 2 master roundtrips expected: the initial locateChunk (cache miss) plus the one forced relocate on status rejection")
	assert.Equal(t, int64(1), rejectCount.Load(),
		"the rejecting CS must be contacted exactly once — the relocate must move to a DIFFERENT server, not retry the same one")
}

// TestChunkLocationCache_ServerStatusRejectionIsBoundedNotLooped verifies the
// other half of the [MAJEUR] fix: a status rejection that is NOT about a
// stale location (both the cached AND the freshly re-queried location are
// rejected by their chunk servers) must still fail fast after exactly one
// forced relocate — never consume the full connection-retry budget
// (maxCSAttempts / internal/backoff.MooseFS), which would needlessly slow
// down what CA8 requires to fail fast.
func TestChunkLocationCache_ServerStatusRejectionIsBoundedNotLooped(t *testing.T) {
	const nodeID = uint32(1)
	const block = uint32(4096)
	const chunkID = uint64(0x555)

	rejectIP1, rejectPort1, rejectCount1 := startRejectingCSServer(t)
	rejectIP2, rejectPort2, rejectCount2 := startRejectingCSServer(t)

	cfg := &countingMasterConfig{
		chunkID: chunkID,
		fileLen: uint64(block),
		locations: []countingMasterLocation{
			{ip: rejectIP1, port: rejectPort1, version: 1},
			{ip: rejectIP2, port: rejectPort2, version: 1},
		},
	}
	c, callCount := startCountingMaster(t, cfg)

	_, err := c.Read(nodeID, 0, block)
	require.Error(t, err, "a persistent status rejection must ultimately fail, not hang or succeed")
	assert.ErrorIs(t, err, errServerStatus)
	assert.Equal(t, int64(2), callCount.Load(),
		"exactly 2 master roundtrips: initial lookup + the ONE forced relocate — never more")
	assert.Equal(t, int64(1), rejectCount1.Load(), "the first (cached) rejecting CS must be contacted exactly once")
	assert.Equal(t, int64(1), rejectCount2.Load(), "the second (relocated) rejecting CS must be contacted exactly once — not looped")
}

// ─── TestChunkLocationCache_Eviction (task 14, CA6) ──────────────────────────

// TestChunkLocationCache_Eviction verifies the cache does not grow without
// bound (CA6). This is a direct, white-box test of chunkLocationCache
// itself (same package) rather than a Client.Read()-driven one: inserting
// thousands of entries through a real network round-trip per entry would
// make this test slow AND still only exercise the real cache indirectly.
// Testing the cache's own LRU/capacity contract directly is both faster and
// more precise — it uses the real chunkLocationCacheMaxEntries constant, so
// it stays correct automatically if that value is retuned.
func TestChunkLocationCache_Eviction(t *testing.T) {
	cache := newChunkLocationCache(chunkLocationCacheMaxEntries, chunkLocationCacheTTL)

	// Insert one more entry than the cache's capacity — every key distinct,
	// inserted in order, so key 0 is the least recently used once the cache
	// is full.
	for i := 0; i <= chunkLocationCacheMaxEntries; i++ {
		key := chunkLocationKey{nodeID: uint32(i), chunkIndex: 0}
		cache.insert(key, &ChunkInfo{ChunkID: uint64(i)})
	}

	assert.LessOrEqualf(t, cache.len(), chunkLocationCacheMaxEntries,
		"cache must never exceed its configured capacity (%d entries) — CA6", chunkLocationCacheMaxEntries)

	firstKey := chunkLocationKey{nodeID: 0, chunkIndex: 0}
	_, found := cache.find(firstKey)
	assert.False(t, found,
		"the least-recently-used entry must be evicted once the cache is at capacity, not retained forever (CA6)")

	lastKey := chunkLocationKey{nodeID: uint32(chunkLocationCacheMaxEntries), chunkIndex: 0}
	_, found = cache.find(lastKey)
	assert.True(t, found, "the most recently inserted entry must still be cached")
}

// TestChunkLocationCache_Eviction_LRUOrderPreservedOnHit verifies that find()
// (a cache hit) counts as a "use" for LRU purposes: an entry that is
// re-accessed just before the cache fills up must survive eviction even
// though it was inserted early, while an entry that was inserted around the
// same time but never re-touched must not.
func TestChunkLocationCache_Eviction_LRUOrderPreservedOnHit(t *testing.T) {
	cache := newChunkLocationCache(chunkLocationCacheMaxEntries, chunkLocationCacheTTL)

	keptKey := chunkLocationKey{nodeID: 1, chunkIndex: 0}
	staleKey := chunkLocationKey{nodeID: 2, chunkIndex: 0}
	cache.insert(keptKey, &ChunkInfo{ChunkID: 1})
	cache.insert(staleKey, &ChunkInfo{ChunkID: 2})

	// Touch keptKey again — it becomes the most recently used of the two.
	_, ok := cache.find(keptKey)
	require.True(t, ok)

	// Fill the rest of the cache with fresh, never-touched-again entries —
	// exactly (maxEntries - 1) of them: combined with the 2 entries already
	// present, that is one more than capacity, triggering exactly ONE
	// eviction (of the single least-recently-used entry: staleKey).
	for i := 3; i <= chunkLocationCacheMaxEntries+1; i++ {
		cache.insert(chunkLocationKey{nodeID: uint32(i), chunkIndex: 0}, &ChunkInfo{ChunkID: uint64(i)})
	}

	_, staleFound := cache.find(staleKey)
	assert.False(t, staleFound, "an entry never re-accessed must be evicted before one that was")

	_, keptFound := cache.find(keptKey)
	assert.True(t, keptFound, "an entry re-accessed via find() must count as recently used and survive eviction longer")
}
