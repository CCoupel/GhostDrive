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

// readEC4At reads size bytes at chunkOffset within an EC4+1 chunk in shard-
// granular mode.  Only the single data shard that contains the requested
// bytes is contacted.
//
// Parameters:
//
//	info        — ChunkInfo with ECParts==4 and Servers[0..3] (DF0..DF3)
//	chunkIndex  — index of the chunk within the file (fileOffset / ChunkSize)
//	chunkOffset — byte offset within the chunk (0 .. ChunkSize-1)
//	size        — number of bytes to read (typically 65536 — one MooseFS block)
//
// The physical chunk ID for each shard is derived via ECPhysicalChunkID.
// Connection pooling and the retry/backoff/fresh-dial policy (doCSRead,
// csclient.go) are shared with Client.Read for normal chunks — see #160.
//
// Precondition: the read must not cross a shard boundary.
// Callers must ensure chunkOffset is aligned to mfsBlockSize and
// size ≤ mfsBlockSize ≤ shardSize.
func (c *Client) readEC4At(
	info *ChunkInfo,
	chunkIndex uint32,
	chunkOffset uint32,
	size uint32,
) ([]byte, error) {
	// ── 1. Compute shard geometry ─────────────────────────────────────────────

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
		return nil, nil // EOF — nothing to read
	}

	// shardSize: size of each data shard, aligned to a MooseFS block (65536 B).
	// Each of the 4 data shards covers exactly shardSize bytes of the chunk.
	const mfsBlockSize uint32 = 65536
	shardSize := alignToBlock(divCeil(chunkDataSize, 4), mfsBlockSize)

	// ── 2. Determine which shard covers chunkOffset ───────────────────────────

	shardIdx := chunkOffset / shardSize
	offsetInShard := chunkOffset % shardSize

	if int(shardIdx) >= len(info.Servers) {
		return nil, fmt.Errorf(
			"mfsclient: readEC4At chunkID=%d chunkIndex=%d: shardIdx %d out of range (nServers=%d, shardSize=%d, chunkOffset=%d)",
			info.ChunkID, chunkIndex, shardIdx, len(info.Servers), shardSize, chunkOffset,
		)
	}

	// ── 3. Derive physical chunk ID and contact the target CS ─────────────────

	srv := info.Servers[shardIdx]
	physicalID := ECPhysicalChunkID(info.ChunkID, int(shardIdx))

	logger.Debug("[mfsclient] readEC4At chunkID=%d chunkIndex=%d shardIdx=%d physicalID=0x%x offsetInShard=%d size=%d",
		info.ChunkID, chunkIndex, shardIdx, physicalID, offsetInShard, size)

	opDesc := fmt.Sprintf("readEC4At chunkID=%d shard=%d", info.ChunkID, shardIdx)
	return doCSRead(c.pool, srv.IP, srv.Port, opDesc, func(cs net.Conn) ([]byte, error) {
		return ReadChunk(cs, physicalID, info.Version, offsetInShard, size)
	})
}
