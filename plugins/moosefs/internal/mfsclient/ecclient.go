// Package mfsclient — EC4+1 chunk read implementation (issue #114).
//
// MooseFS Pro 4.x supports erasure coding (EC4+1: 4 data shards + 1 parity).
// Each shard is stored on a distinct chunk server and is accessed via the normal
// CLTOCS_READ (opcode 200) protocol using a physical chunk ID derived from the
// logical chunk ID returned by the master:
//
//	physical[part] = logical + 0x1000000000000000 + part × 0x0100000000000000
//
// This file implements shard-granular reads: for a given file offset, only the
// single shard containing the requested bytes is contacted.  This avoids the
// overhead of downloading all 4 shards for every 64-KiB read block.
package mfsclient

import (
	"errors"
	"fmt"
	"net"

	"github.com/CCoupel/GhostDrive/internal/logger"
)

// divCeil returns ⌈a/b⌉ (ceiling division), both arguments uint32.
func divCeil(a, b uint32) uint32 {
	return (a + b - 1) / b
}

// alignToBlock rounds n up to the nearest multiple of blockSize.
// blockSize must be a power of 2 (typically 65536 = MooseFS block size).
func alignToBlock(n, blockSize uint32) uint32 {
	if n%blockSize == 0 {
		return n
	}
	return (n/blockSize + 1) * blockSize
}

// errEC4EOF is a sentinel returned by locateEC4Shard when chunkOffset falls
// at or past the end of the chunk's actual data — i.e. nothing to read.
var errEC4EOF = errors.New("mfsclient: readEC4At: chunk offset past EOF")

// ec4ShardLocation is the resolved shard target for one readEC4At attempt:
// which data shard covers chunkOffset, and where to reach it.
type ec4ShardLocation struct {
	shardIdx      uint32
	offsetInShard uint32
	physicalID    uint64
	srv           ChunkServer
}

// locateEC4Shard computes which of the 4 data shards of an EC4+1 chunk
// (described by info) covers chunkOffset, and the physical chunk ID / server
// to contact for it. Returns errEC4EOF (wrapped) if chunkOffset is past the
// chunk's actual data.
//
// Pure function of (info, chunkIndex, chunkOffset) — no I/O, no Client state
// — so it can be re-run cheaply against a freshly re-queried ChunkInfo on
// retry (#160 D3bis) without duplicating the geometry math.
func locateEC4Shard(info *ChunkInfo, chunkIndex uint32, chunkOffset uint32) (ec4ShardLocation, error) {
	// chunkDataSize: how many bytes of actual data are in this chunk.
	// For the last chunk of the file, this may be less than ChunkSize.
	var chunkDataSize uint32
	chunkStart := uint64(chunkIndex) * ChunkSize
	if info.Length > chunkStart {
		remaining := info.Length - chunkStart
		if remaining >= ChunkSize {
			chunkDataSize = uint32(ChunkSize)
		} else {
			chunkDataSize = uint32(remaining)
		}
	}
	if chunkDataSize == 0 {
		return ec4ShardLocation{}, errEC4EOF
	}

	// shardSize: size of each data shard, aligned to a MooseFS block (65536 B).
	// Each of the 4 data shards covers exactly shardSize bytes of the chunk.
	const mfsBlockSize uint32 = 65536
	shardSize := alignToBlock(divCeil(chunkDataSize, 4), mfsBlockSize)

	shardIdx := chunkOffset / shardSize
	offsetInShard := chunkOffset % shardSize

	if int(shardIdx) >= len(info.Servers) {
		return ec4ShardLocation{}, fmt.Errorf(
			"mfsclient: readEC4At chunkID=%d chunkIndex=%d: shardIdx %d out of range (nServers=%d, shardSize=%d, chunkOffset=%d)",
			info.ChunkID, chunkIndex, shardIdx, len(info.Servers), shardSize, chunkOffset,
		)
	}

	return ec4ShardLocation{
		shardIdx:      shardIdx,
		offsetInShard: offsetInShard,
		physicalID:    ECPhysicalChunkID(info.ChunkID, int(shardIdx)),
		srv:           info.Servers[shardIdx],
	}, nil
}

// readEC4At reads size bytes at chunkOffset within an EC4+1 chunk in shard-
// granular mode.  Only the single data shard that contains the requested
// bytes is contacted.
//
// Parameters:
//
//	nodeID      — file inode, needed to re-query the master on retry (D3bis)
//	info        — ChunkInfo with ECParts==4 and Servers[0..3] (DF0..DF3),
//	              already resolved by the caller (Client.Read) for attempt 0
//	chunkIndex  — index of the chunk within the file (fileOffset / ChunkSize)
//	chunkOffset — byte offset within the chunk (0 .. ChunkSize-1)
//	size        — number of bytes to read (typically 65536 — one MooseFS block)
//
// Connection pooling and the retry/backoff/fresh-dial/re-locate policy
// (doCSRead, csclient.go) are shared with Client.Read for normal chunks —
// see #160. A retry re-queries the master via c.locateChunk instead of
// reusing the info passed in, mirroring chunksdatacache_invalidate in the
// official client (readdata.c:1989).
//
// Precondition: the read must not cross a shard boundary.
// Callers must ensure chunkOffset is aligned to mfsBlockSize and
// size ≤ mfsBlockSize ≤ shardSize.
func (c *Client) readEC4At(
	nodeID uint32,
	info *ChunkInfo,
	chunkIndex uint32,
	chunkOffset uint32,
	size uint32,
) ([]byte, error) {
	loc0, err := locateEC4Shard(info, chunkIndex, chunkOffset)
	if err != nil {
		if errors.Is(err, errEC4EOF) {
			return nil, nil
		}
		return nil, err
	}

	logger.Debug("[mfsclient] readEC4At chunkID=%d chunkIndex=%d shardIdx=%d physicalID=0x%x offsetInShard=%d size=%d",
		info.ChunkID, chunkIndex, loc0.shardIdx, loc0.physicalID, loc0.offsetInShard, size)

	opDesc := fmt.Sprintf("readEC4At chunkID=%d shard=%d", info.ChunkID, loc0.shardIdx)

	locate := func(attempt int) (csLocateResult, error) {
		curInfo := info
		loc := loc0
		if attempt > 0 {
			// D3bis: invalidate the cached chunk location and re-query the
			// master instead of re-attacking the shard server that just
			// failed — it may itself be the reason the attempt failed.
			fresh, lErr := c.locateChunk(nodeID, chunkIndex)
			if lErr != nil {
				return csLocateResult{}, lErr
			}
			if fresh == nil {
				return csLocateResult{eof: true}, nil
			}
			curInfo = fresh

			freshLoc, lErr := locateEC4Shard(curInfo, chunkIndex, chunkOffset)
			if lErr != nil {
				if errors.Is(lErr, errEC4EOF) {
					return csLocateResult{eof: true}, nil
				}
				return csLocateResult{}, lErr
			}
			loc = freshLoc
		}

		physicalID := loc.physicalID
		version := curInfo.Version
		offsetInShard := loc.offsetInShard
		return csLocateResult{
			ip:   loc.srv.IP,
			port: loc.srv.Port,
			read: func(cs net.Conn) ([]byte, error) {
				return ReadChunk(cs, physicalID, version, offsetInShard, size)
			},
		}, nil
	}

	return doCSRead(c.pool, opDesc, locate)
}
