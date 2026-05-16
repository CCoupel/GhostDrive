// Package mfsclient — unit tests for EC4+1 chunk read (ecclient.go, issue #114).
//
// All tests use in-memory fake chunk servers (fakeCSServer from csclient_test.go)
// and never connect to a real MooseFS cluster.
//
// Test inventory:
//
//	TestECPhysicalChunkID            — formula correct for parts 0..4
//	TestDivCeilAlignToBlock          — helper function correctness
//	TestReadEC4Basic                 — 4 shards × 8 KiB, one read per shard
//	TestReadEC4FullChunk             — 4 shards × 1 MiB, 64 sequential 64-KiB reads
//	TestReadEC4PartialLastShard      — chunk length not a multiple of 4 blocks
//	TestReadEC4InsufficientServers   — fewer servers than shardIdx → error
//	TestReadEC4ShardReadError        — CS closes connection → error propagated
//	TestReadEC4StaleConnection       — stale pooled conn → retry → success
//	TestReadEC4Via_ClientRead        — Client.Read() path with proto=3 fake master
package mfsclient

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── ECPhysicalChunkID ────────────────────────────────────────────────────────

func TestECPhysicalChunkID(t *testing.T) {
	tests := []struct {
		name      string
		logicalID uint64
		partIdx   int
		want      uint64
	}{
		{"logical=0 part=0 (DF0)", 0, 0, 0x1000000000000000},
		{"logical=0 part=1 (DF1)", 0, 1, 0x1100000000000000},
		{"logical=0 part=2 (DF2)", 0, 2, 0x1200000000000000},
		{"logical=0 part=3 (DF3)", 0, 3, 0x1300000000000000},
		{"logical=0 part=4 (CF0)", 0, 4, 0x1400000000000000},
		// Non-zero logical ID: verified against MooseFS hddspacemgr.c
		{"logical=0xaebb part=0", 0xaebb, 0, 0x1000000000000000 + 0xaebb},
		{"logical=0xaebb part=2", 0xaebb, 2, 0x1200000000000000 + 0xaebb},
		{"logical=0xaebb part=3", 0xaebb, 3, 0x1300000000000000 + 0xaebb},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ECPhysicalChunkID(tt.logicalID, tt.partIdx)
			assert.Equal(t, tt.want, got,
				"ECPhysicalChunkID(0x%x, %d)", tt.logicalID, tt.partIdx)
		})
	}
}

// ─── Helper functions ─────────────────────────────────────────────────────────

func TestDivCeilAlignToBlock(t *testing.T) {
	t.Run("divCeil", func(t *testing.T) {
		assert.Equal(t, uint32(1), divCeil(1, 4))
		assert.Equal(t, uint32(1), divCeil(4, 4))
		assert.Equal(t, uint32(2), divCeil(5, 4))
		assert.Equal(t, uint32(4), divCeil(16, 4))
		assert.Equal(t, uint32(4), divCeil(13, 4))
	})
	t.Run("alignToBlock", func(t *testing.T) {
		const block = uint32(65536)
		assert.Equal(t, block, alignToBlock(1, block))
		assert.Equal(t, block, alignToBlock(block, block))
		assert.Equal(t, 2*block, alignToBlock(block+1, block))
		assert.Equal(t, 2*block, alignToBlock(2*block, block))
	})
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// makeECClient creates a bare Client with only a CS pool — no master connection.
// Used for tests that call readEC4At directly without a master roundtrip.
func makeECClient() *Client {
	return &Client{pool: newCSPool()}
}

// makeShardBytes returns a deterministic byte slice of size n for shard i.
// Used to fill fake CS stores and verify that the correct shard is returned.
func makeShardBytes(shardIdx int, size int) []byte {
	b := make([]byte, size)
	for j := range b {
		b[j] = byte((shardIdx*7 + j*3) & 0xFF)
	}
	return b
}

// startFourCSServers starts 4 fakeCSServer instances and returns them along
// with a ChunkInfo populated with their addresses.
// Each server is seeded with shardData[i] at physicalID = ECPhysicalChunkID(logicalID, i).
func startFourCSServers(t *testing.T, logicalID uint64, shardData [4][]byte) (*ChunkInfo, [4]*fakeCSServer) {
	t.Helper()
	var servers [4]*fakeCSServer
	info := &ChunkInfo{
		ChunkID: logicalID,
		Version: 1,
		ECParts: 4,
		Servers: make([]ChunkServer, 4),
	}

	totalLen := uint64(0)
	for i := range shardData {
		totalLen += uint64(len(shardData[i]))
	}
	info.Length = totalLen

	for i := 0; i < 4; i++ {
		s := newFakeCSServer()
		ip, port := s.Start()
		t.Cleanup(s.Stop)

		physID := ECPhysicalChunkID(logicalID, i)
		s.SetChunkData(physID, shardData[i])

		servers[i] = s
		info.Servers[i] = ChunkServer{IP: ip, Port: port}
	}
	return info, servers
}

// ─── TestReadEC4Basic ─────────────────────────────────────────────────────────

// TestReadEC4Basic verifies shard-granular EC4+1 reads for the minimum valid
// EC chunk size.
//
// Setup: 4 × 65536 B shards → chunk = 256 KiB (one MooseFS block per shard).
// Each call to readEC4At must contact exactly the expected CS and return the
// correct bytes.
//
// Note: shardSize must be ≥ 65536 (MooseFS block size) because
// alignToBlock(divCeil(n,4), 65536) rounds up.  A 256 KiB chunk gives
// shardSize = alignToBlock(65536, 65536) = 65536 exactly.
func TestReadEC4Basic(t *testing.T) {
	const shardSize = 65536 // 64 KiB — minimum MooseFS block size
	const logicalID = uint64(0xaebb)

	var shardData [4][]byte
	for i := range shardData {
		shardData[i] = makeShardBytes(i, shardSize)
	}

	info, servers := startFourCSServers(t, logicalID, shardData)

	c := makeECClient()

	for i := 0; i < 4; i++ {
		chunkOffset := uint32(i) * shardSize
		got, err := c.readEC4At(info, 0, chunkOffset, shardSize)
		require.NoErrorf(t, err, "shard %d read failed", i)
		assert.Equalf(t, shardData[i], got, "shard %d data mismatch", i)
		// Verify that only the target server was contacted.
		for j, s := range servers {
			if j == i {
				assert.GreaterOrEqualf(t, s.connCount.Load(), int64(1),
					"server %d must have been contacted for shard %d", j, i)
			}
		}
		_ = servers[i] // silence unused warning
	}
}

// ─── TestReadEC4FullChunk ─────────────────────────────────────────────────────

// TestReadEC4FullChunk verifies sequential 64-KiB reads across a 4 MiB chunk
// (4 × 1 MiB shards).  This simulates the Download() inner loop.
func TestReadEC4FullChunk(t *testing.T) {
	const block = uint32(65536)          // 64 KiB — MooseFS block size
	const shardSz = uint32(1024 * 1024)  // 1 MiB per shard
	const chunkLen = uint64(4 * 1024 * 1024) // 4 MiB total
	const logicalID = uint64(0x1234)

	var shardData [4][]byte
	for i := range shardData {
		shardData[i] = makeShardBytes(i, int(shardSz))
	}

	info, _ := startFourCSServers(t, logicalID, shardData)
	info.Length = chunkLen

	c := makeECClient()

	// Read the entire chunk in 64-KiB blocks.
	nReads := int(chunkLen / uint64(block))
	for r := 0; r < nReads; r++ {
		chunkOffset := uint32(r) * block
		shardIdx := chunkOffset / shardSz
		offsetInShard := chunkOffset % shardSz

		got, err := c.readEC4At(info, 0, chunkOffset, block)
		require.NoErrorf(t, err, "read %d (offset=%d) failed", r, chunkOffset)

		want := shardData[shardIdx][offsetInShard : offsetInShard+block]
		assert.Equalf(t, want, got, "read %d (shard %d, offsetInShard %d) data mismatch",
			r, shardIdx, offsetInShard)
	}
}

// ─── TestReadEC4PartialLastShard ──────────────────────────────────────────────

// TestReadEC4PartialLastShard verifies that a chunk whose size is not a
// multiple of 4 MooseFS blocks is handled correctly.
//
// Chunk = 3 × 65536 + 1 byte = 196609 bytes.
// shardSize = alignToBlock(divCeil(196609, 4), 65536) = alignToBlock(49153, 65536) = 65536.
// DF0..DF2 hold 65536 bytes; DF3 holds 1 byte (last shard is shorter).
func TestReadEC4PartialLastShard(t *testing.T) {
	const block = uint32(65536)
	const logicalID = uint64(0xbeef)

	// Chunk length: 3 full blocks + 1 byte.
	chunkLen := uint64(3*block + 1)

	var shardData [4][]byte
	shardData[0] = makeShardBytes(0, int(block))
	shardData[1] = makeShardBytes(1, int(block))
	shardData[2] = makeShardBytes(2, int(block))
	shardData[3] = makeShardBytes(3, 1) // only 1 byte in last shard

	info, _ := startFourCSServers(t, logicalID, shardData)
	info.Length = chunkLen

	c := makeECClient()

	// Read shard 0 (offset 0).
	got0, err := c.readEC4At(info, 0, 0, block)
	require.NoError(t, err, "shard 0 read")
	assert.Equal(t, shardData[0], got0, "shard 0 data")

	// Read shard 3 (offset 3*block) — only 1 byte available.
	got3, err := c.readEC4At(info, 0, 3*block, block)
	require.NoError(t, err, "shard 3 read (partial)")
	// fakeCSServer returns only available data; 1 byte expected.
	assert.Equal(t, shardData[3], got3, "shard 3 partial data")
}

// ─── TestReadEC4InsufficientServers ───────────────────────────────────────────

// TestReadEC4InsufficientServers verifies that readEC4At returns an error when
// the requested shard index is out of range (fewer servers than shards).
func TestReadEC4InsufficientServers(t *testing.T) {
	const block = uint32(65536)
	const logicalID = uint64(0xdead)

	// ChunkInfo with only 3 servers for a 4-shard chunk.
	// Attempting to read shard 3 (offset = 3×block) must fail.
	var shardData [4][]byte
	for i := range shardData {
		shardData[i] = makeShardBytes(i, int(block))
	}
	info, _ := startFourCSServers(t, logicalID, shardData)
	// Remove the last server to simulate missing shard DF3.
	info.Servers = info.Servers[:3]

	c := makeECClient()

	// Shard 3 offset is 3 × block.
	_, err := c.readEC4At(info, 0, 3*block, block)
	require.Error(t, err, "must fail when shardIdx >= len(Servers)")
	assert.Contains(t, err.Error(), "shardIdx", "error must mention shardIdx")
}

// ─── TestReadEC4ShardReadError ────────────────────────────────────────────────

// TestReadEC4ShardReadError verifies that a ReadChunk error from the target
// CS is propagated to the caller.
//
// CS for shard 1 immediately closes the connection after accept, causing
// ReadChunk to fail with EOF.
func TestReadEC4ShardReadError(t *testing.T) {
	const block = uint32(65536)
	const logicalID = uint64(0xcafe)

	// Start a "bad" CS that accepts and immediately closes.
	badLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	badDone := make(chan struct{})
	go func() {
		defer close(badDone)
		for {
			conn, err := badLn.Accept()
			if err != nil {
				return
			}
			conn.Close() // immediately close — simulates dead process
		}
	}()
	t.Cleanup(func() {
		_ = badLn.Close()
		<-badDone
	})

	badAddr := badLn.Addr().(*net.TCPAddr)
	badIPBytes := badAddr.IP.To4()
	badIP := binary.BigEndian.Uint32(badIPBytes)
	badPort := uint16(badAddr.Port)

	// Good shards for 0, 2, 3 — only shard 1 is bad.
	var shardData [4][]byte
	for i := range shardData {
		shardData[i] = makeShardBytes(i, int(block))
	}
	info, _ := startFourCSServers(t, logicalID, shardData)
	// Replace shard 1's server with the bad one.
	info.Servers[1] = ChunkServer{IP: badIP, Port: badPort}

	c := makeECClient()

	// Reading shard 0 should succeed.
	got0, err0 := c.readEC4At(info, 0, 0, block)
	require.NoError(t, err0, "shard 0 must succeed")
	assert.Equal(t, shardData[0], got0, "shard 0 data")

	// Reading shard 1 must fail (bad CS).
	_, err1 := c.readEC4At(info, 0, block, block)
	require.Error(t, err1, "shard 1 must return error (CS closes connection)")
}

// ─── TestReadEC4StaleConnection ───────────────────────────────────────────────

// TestReadEC4StaleConnection verifies the retry-once policy for stale pooled
// connections: when the first connection from the pool returns a stale-conn
// error (EOF), readEC4At discards it, dials a fresh connection, and succeeds.
func TestReadEC4StaleConnection(t *testing.T) {
	const block = uint32(65536)
	const logicalID = uint64(0x5a1e)

	shardData := [4][]byte{
		makeShardBytes(0, int(block)),
		makeShardBytes(1, int(block)),
		makeShardBytes(2, int(block)),
		makeShardBytes(3, int(block)),
	}
	info, _ := startFourCSServers(t, logicalID, shardData)

	c := makeECClient()
	srv0 := info.Servers[0]

	// Inject a stale (pre-closed) connection into the pool for shard 0.
	staleConn, dialErr := c.pool.Get(srv0.IP, srv0.Port)
	require.NoError(t, dialErr)
	staleConn.Close() // closed from client side — server will see EOF on next read
	c.pool.Put(staleConn, srv0.IP, srv0.Port)

	// readEC4At must transparently retry with a fresh connection and succeed.
	got, err := c.readEC4At(info, 0, 0, block)
	require.NoError(t, err, "must succeed after transparent stale-conn retry")
	assert.Equal(t, shardData[0], got, "data must match shard 0")
}

// ─── TestReadEC4Via_ClientRead ────────────────────────────────────────────────

// TestReadEC4Via_ClientRead exercises the full Client.Read() path with a fake
// MooseFS master that returns a proto=3 (EC4+1) READ_CHUNK response.
//
// Verifies that Client.Read() correctly routes through readEC4At and returns
// the expected bytes.
func TestReadEC4Via_ClientRead(t *testing.T) {
	const block = uint32(65536)
	const logicalID = uint64(0xec4)
	const fileLen = uint64(4 * block) // 4 shards × 64 KiB = 256 KiB

	// Prepare shard data and start CS servers.
	var shardData [4][]byte
	for i := range shardData {
		shardData[i] = makeShardBytes(i, int(block))
	}

	var csIP [4]uint32
	var csPort [4]uint16
	var csServers [4]*fakeCSServer
	for i := 0; i < 4; i++ {
		s := newFakeCSServer()
		ip, port := s.Start()
		t.Cleanup(s.Stop)
		physID := ECPhysicalChunkID(logicalID, i)
		s.SetChunkData(physID, shardData[i])
		csIP[i] = ip
		csPort[i] = port
		csServers[i] = s
	}

	// Minimal fake master: handles REGISTER + READ_CHUNK (proto=3).
	masterLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	masterDone := make(chan struct{})
	go func() {
		defer close(masterDone)
		for {
			conn, err := masterLn.Accept()
			if err != nil {
				return
			}
			go serveEC4Master(conn, logicalID, fileLen, csIP, csPort)
		}
	}()
	t.Cleanup(func() {
		_ = masterLn.Close()
		<-masterDone
	})

	masterAddr := masterLn.Addr().(*net.TCPAddr)
	c, err := Dial("127.0.0.1", masterAddr.Port)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	// Read each 64-KiB block; verify it matches the corresponding shard.
	for i := 0; i < 4; i++ {
		offset := uint64(i) * uint64(block)
		got, err := c.Read(1, offset, block)
		require.NoErrorf(t, err, "Read shard %d failed", i)
		require.NotNil(t, got)
		assert.Equalf(t, shardData[i], got, "shard %d data mismatch", i)
	}
}

// serveEC4Master serves a minimal fake MooseFS master for EC4+1 tests.
// It handles REGISTER (responds OK) and READ_CHUNK (responds proto=3).
func serveEC4Master(conn net.Conn, logicalID uint64, fileLen uint64, csIP [4]uint32, csPort [4]uint16) {
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
			resp = PutUint32(resp, rand.Uint32()) // sessionID
			resp = PutUint32(resp, 0)             // maxopenfiles (not used)
			_ = WriteFrame(conn, MatoclFuseRegister, resp)

		case CltomFuseReadChunk:
			if len(payload) < 12 {
				return
			}
			msgid, off, _ := ReadUint32(payload, 0)
			_, off, _ = ReadUint32(payload, off)    // nodeID (ignored)
			_, _, _ = ReadUint32(payload, off)      // chunkIndex (always 0 in this test)

			// Build proto=3 response with 4 CS entries.
			var resp []byte
			resp = PutUint32(resp, msgid)
			resp = PutUint8(resp, 3)            // protocolid = 3
			resp = PutUint64(resp, fileLen)     // file length
			resp = PutUint64(resp, logicalID)   // logical chunk ID
			resp = PutUint32(resp, 1)           // version
			for i := 0; i < 4; i++ {
				resp = PutUint32(resp, csIP[i])
				resp = PutUint16(resp, csPort[i])
				resp = PutUint32(resp, 0) // cs_ver
				resp = PutUint32(resp, 0) // labelmask
			}
			_ = WriteFrame(conn, MatoclFuseReadChunk, resp)

		default:
			return // unexpected command
		}
	}
}

// ─── TestReadEC4MultiChunk ────────────────────────────────────────────────────

// ecChunkSpec holds the logical chunk ID and CS servers for one EC4 chunk.
// Used by serveEC4MasterMultiChunk to dispatch per-chunk READ_CHUNK responses.
type ecChunkSpec struct {
	logicalID uint64
	csIP      [4]uint32
	csPort    [4]uint16
}

// TestReadEC4MultiChunk verifies EC4+1 reads across a 2-chunk file
// (file length = ChunkSize + 1 shard, so chunk 0 is full and chunk 1 is partial).
//
// This exercises the chunkIndex routing in Client.Read(): each chunk has a
// distinct logical chunk ID and its own set of CS servers.  A fake master
// dispatches the correct CS list per chunk index.
//
// Reads:
//   - offset 0           → chunk 0 (full, 64 MiB), shard 0 → CS set A
//   - offset ChunkSize   → chunk 1 (65536 bytes), shard 0 → CS set B
func TestReadEC4MultiChunk(t *testing.T) {
	const block = uint32(65536)
	// File spans 2 chunks: chunk 0 full (64 MiB) + chunk 1 partial (1 × 64 KiB).
	const fileLen = uint64(ChunkSize) + uint64(block)

	const logicalID0 = uint64(0xEC4C0000) // logical ID for chunk 0
	const logicalID1 = uint64(0xEC4C0001) // logical ID for chunk 1

	// Prepare one block of identifying shard data per chunk.
	shardA := makeShardBytes(0xA0, int(block)) // chunk 0, shard 0
	shardB := makeShardBytes(0xB0, int(block)) // chunk 1, shard 0

	// Start CS servers for chunk 0 (4 servers — only CS0 is contacted in this test).
	var specA ecChunkSpec
	specA.logicalID = logicalID0
	for i := 0; i < 4; i++ {
		s := newFakeCSServer()
		ip, port := s.Start()
		t.Cleanup(s.Stop)
		if i == 0 {
			// Seed CS0 with shard data for the first 65536 bytes of the 16 MiB shard.
			physID := ECPhysicalChunkID(logicalID0, 0)
			s.SetChunkData(physID, shardA)
		}
		specA.csIP[i] = ip
		specA.csPort[i] = port
	}

	// Start CS servers for chunk 1 (4 servers — only CS0 is contacted in this test).
	var specB ecChunkSpec
	specB.logicalID = logicalID1
	for i := 0; i < 4; i++ {
		s := newFakeCSServer()
		ip, port := s.Start()
		t.Cleanup(s.Stop)
		if i == 0 {
			physID := ECPhysicalChunkID(logicalID1, 0)
			s.SetChunkData(physID, shardB)
		}
		specB.csIP[i] = ip
		specB.csPort[i] = port
	}

	// Build a fake master that dispatches the correct CS list per chunk index.
	chunks := []ecChunkSpec{specA, specB}

	masterLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	masterDone := make(chan struct{})
	go func() {
		defer close(masterDone)
		for {
			conn, err := masterLn.Accept()
			if err != nil {
				return
			}
			go serveEC4MasterMultiChunk(conn, fileLen, chunks)
		}
	}()
	t.Cleanup(func() {
		_ = masterLn.Close()
		<-masterDone
	})

	masterAddr := masterLn.Addr().(*net.TCPAddr)
	c, err := Dial("127.0.0.1", masterAddr.Port)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Register())

	// Read first block from chunk 0 (chunkOffset=0, shard 0 of chunk 0).
	gotA, err := c.Read(1, 0, block)
	require.NoError(t, err, "Read from chunk 0 must succeed")
	assert.Equal(t, shardA, gotA, "chunk 0 shard 0 data mismatch")

	// Read first block from chunk 1 (chunkOffset=0, shard 0 of chunk 1).
	gotB, err := c.Read(1, ChunkSize, block)
	require.NoError(t, err, "Read from chunk 1 must succeed")
	assert.Equal(t, shardB, gotB, "chunk 1 shard 0 data mismatch")
}

// serveEC4MasterMultiChunk serves a fake MooseFS master that returns a
// proto=3 READ_CHUNK response indexed by chunk index (0-based).
// chunks[i] specifies the logical chunk ID and CS servers for chunk i.
func serveEC4MasterMultiChunk(conn net.Conn, fileLen uint64, chunks []ecChunkSpec) {
	defer conn.Close()
	for {
		cmd, payload, err := ReadFrame(conn)
		if err != nil {
			return
		}
		switch cmd {
		case CltomFuseRegister:
			if len(payload) < 8 {
				return
			}
			msgid := binary.BigEndian.Uint32(payload[:4])
			var resp []byte
			resp = PutUint32(resp, msgid)
			resp = PutUint32(resp, rand.Uint32()) // sessionID
			resp = PutUint32(resp, 0)             // padding
			_ = WriteFrame(conn, MatoclFuseRegister, resp)

		case CltomFuseReadChunk:
			if len(payload) < 12 {
				return
			}
			msgid, off, _ := ReadUint32(payload, 0)
			_, off, _ = ReadUint32(payload, off)         // nodeID
			chunkIndex, _, _ := ReadUint32(payload, off) // chunk index

			if int(chunkIndex) >= len(chunks) {
				// Past EOF: return 5-byte StatusOK.
				var eof []byte
				eof = PutUint32(eof, msgid)
				eof = PutUint8(eof, StatusOK)
				_ = WriteFrame(conn, MatoclFuseReadChunk, eof)
				continue
			}

			spec := chunks[chunkIndex]
			var resp []byte
			resp = PutUint32(resp, msgid)
			resp = PutUint8(resp, 3)              // protocolid = 3
			resp = PutUint64(resp, fileLen)        // file length
			resp = PutUint64(resp, spec.logicalID) // per-chunk logical ID
			resp = PutUint32(resp, 1)              // version
			for i := 0; i < 4; i++ {
				resp = PutUint32(resp, spec.csIP[i])
				resp = PutUint16(resp, spec.csPort[i])
				resp = PutUint32(resp, 0) // cs_ver
				resp = PutUint32(resp, 0) // labelmask
			}
			_ = WriteFrame(conn, MatoclFuseReadChunk, resp)

		default:
			return
		}
	}
}

// ─── Helper: suppress unused import ──────────────────────────────────────────

var _ = fmt.Sprintf // keep "fmt" import used (used in error messages above)
