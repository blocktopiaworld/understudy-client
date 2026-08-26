package understudy

import (
	"context"
	"sync"
	"time"

	"github.com/blocktopia/understudy-client/protocol"
)

// chunkKey identifies a chunk column.
type chunkKey struct{ X, Z int32 }

// world is the bot's view of loaded terrain.
//
// It exists to answer one question cheaply and exactly: what block is at this
// coordinate? Everything the bot could not otherwise do for itself — find the
// floor, notice it is underwater, confirm a block actually broke — reduces to
// that.
//
// The mutex guards the columns as well as the map holding them. That is not
// belt-and-braces: a block update rewrites a section in place, and expanding a
// uniform section to direct encoding replaces its backing arrays outright — so
// a reader that only synchronised on the map lookup would walk a slice being
// swapped underneath it. Terrain updates arrive on the read loop while a
// control API traces rays from its own goroutines, which is exactly that race.
//
// All access therefore holds the lock for the whole operation, not just long
// enough to find the column.
type world struct {
	mu     sync.RWMutex
	chunks map[chunkKey]*protocol.ChunkColumn
}

func newWorld() *world {
	return &world{chunks: make(map[chunkKey]*protocol.ChunkColumn)}
}

func (w *world) store(c *protocol.ChunkColumn) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.chunks[chunkKey{c.X, c.Z}] = c
}

func (w *world) drop(x, z int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.chunks, chunkKey{x, z})
}

func (w *world) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	clear(w.chunks)
}

func (w *world) loaded() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.chunks)
}

// blockState returns the state at world coordinates, or air if the chunk is
// not loaded.
//
// Arithmetic shift, not division: dividing a negative coordinate by 16 rounds
// towards zero and lands on the wrong chunk for every negative coordinate.
func (w *world) blockState(x, y, z int32) int32 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	// BlockState is nil-safe, so a miss reads as air without a branch here.
	return w.chunks[chunkKey{x >> 4, z >> 4}].BlockState(x, y, z)
}

// setBlockState applies a single block update. It takes the write lock because
// it mutates the column — see the note on world.
func (w *world) setBlockState(x, y, z, state int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.chunks[chunkKey{x >> 4, z >> 4}].SetBlockState(x, y, z, state)
}

// scan runs fn with the read lock held once, handing it a block lookup.
//
// A ground scan or a ray trace reads hundreds of blocks along a line. Doing
// that a locked call at a time is hundreds of round trips through the mutex
// while the read loop is trying to store chunks, and — worse — lets terrain
// change halfway through, so the scan answers about a world that never existed.
//
// fn must not call back into the world, and must not retain the lookup.
func (w *world) scan(fn func(at func(x, y, z int32) int32)) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	fn(func(x, y, z int32) int32 {
		return w.chunks[chunkKey{x >> 4, z >> 4}].BlockState(x, y, z)
	})
}

// hasChunk reports whether the column covering a coordinate is loaded. Callers
// must check this before trusting an "air" answer, since an unloaded chunk is
// indistinguishable from empty space.
func (w *world) hasChunk(x, z int32) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.chunks[chunkKey{x >> 4, z >> 4}]
	return ok
}

// BlockAt returns the block state at world coordinates.
func (c *Client) BlockAt(x, y, z int32) int32 { return c.world.blockState(x, y, z) }

// ChunkLoaded reports whether terrain covering a coordinate is known.
func (c *Client) ChunkLoaded(x, z int32) bool { return c.world.hasChunk(x, z) }

// LoadedChunks returns how many chunk columns are currently held.
func (c *Client) LoadedChunks() int { return c.world.loaded() }

// IsSolidAt reports whether the block at a coordinate blocks movement.
func (c *Client) IsSolidAt(x, y, z int32) bool {
	return c.v.IsSolid(c.world.blockState(x, y, z))
}

// IsTargetableAt reports whether the crosshair would stop on this block —
// true for cobweb, crops and torches, which IsSolidAt reports as empty.
func (c *Client) IsTargetableAt(x, y, z int32) bool {
	return c.v.IsTargetable(c.world.blockState(x, y, z))
}

// Support describes what a bot is standing in or on.
type Support struct {
	// GroundY is the Y coordinate the bot's feet rest at.
	GroundY float64
	// Found is false when no floor was located within the search depth.
	Found bool
	// InWater is true when the landing point is water rather than solid ground.
	InWater bool
	// InLava is true when the landing point is lava.
	InLava bool
}

// maxGroundSearch bounds the downward scan. Deeper than the tallest possible
// drop from the build limit to the void floor.
const maxGroundSearch = 512

// FindGround scans downward for the first thing that would stop a fall.
//
// Water counts as a stop and is reported separately, because it stops a fall
// *differently*: it cancels fall damage entirely, and then drowns anything that
// stays under. A bot that treats water as empty space falls through it like a
// stone and dies at the bottom.
//
// Returns Found == false if the terrain is not loaded or nothing was hit,
// which callers must not confuse with "the void": an unloaded chunk reads as
// air everywhere.
func (c *Client) FindGround(x, y, z int32) Support {
	if !c.world.hasChunk(x, z) {
		return Support{}
	}
	var support Support
	c.world.scan(func(at func(x, y, z int32) int32) {
		for probe := y; probe > y-maxGroundSearch; probe-- {
			state := at(x, probe, z)
			switch {
			case c.v.IsWater(state):
				// Feet go *inside* the top water block, not on top of it. Water
				// only cancels fall damage once the player is actually in it, so
				// stopping one block high lands the bot on "air above water" and
				// the server charges full fall damage — which is exactly the bug
				// this looks like it is avoiding.
				support = Support{GroundY: float64(probe), Found: true, InWater: true}
				return
			case c.v.IsLava(state):
				support = Support{GroundY: float64(probe + 1), Found: true, InLava: true}
				return
			case c.v.IsSolid(state):
				support = Support{GroundY: float64(probe + 1), Found: true}
				return
			}
		}
	})
	return support
}

// GroundBelow finds what the bot is currently standing over.
func (c *Client) GroundBelow() Support {
	pos := c.Position()
	x, y, z := blockPos(pos.X, pos.Y, pos.Z)
	// Start the scan one below the feet — the block the bot occupies is the
	// one it is standing *in*, not standing on.
	return c.FindGround(x, y-1, z)
}

// Submerged reports whether the bot's head is inside water, which is the
// condition that drowns it.
func (c *Client) Submerged() bool {
	pos := c.Position()
	// The head block sits one above the feet.
	x, head, z := blockPos(pos.X, pos.Y+1.0, pos.Z)
	return c.v.IsWater(c.world.blockState(x, head, z))
}

// WaterSurfaceAbove finds the Y at which the bot's head would clear the water,
// scanning up from its current position. Returns false if no surface is found.
func (c *Client) WaterSurfaceAbove() (surfaceY float64, found bool) {
	pos := c.Position()
	x, start, z := blockPos(pos.X, pos.Y, pos.Z)
	c.world.scan(func(at func(x, y, z int32) int32) {
		for probe := start; probe < start+maxGroundSearch; probe++ {
			if !c.v.IsWater(at(x, probe+1, z)) {
				surfaceY, found = float64(probe), true
				return
			}
		}
	})
	return surfaceY, found
}

// maxChunksPerTick is the absorption rate reported to the server. Vanilla
// clamps this server-side, so asking high simply means "send them as fast as
// you are willing to".
const maxChunksPerTick = 64.0

// chunkWaitTimeout bounds how long to wait for terrain after a teleport.
// Generous, because the alternative to waiting is acting on a world the bot
// cannot see; if it expires the caller falls back to the imprecise path rather
// than failing.
const chunkWaitTimeout = 5 * time.Second

// chunkPollInterval is how often waitForChunk re-checks. Well under a tick, so
// terrain is picked up in the same tick it arrives.
const chunkPollInterval = 25 * time.Millisecond

// waitForChunk blocks until the terrain under the bot is loaded, or the
// timeout expires. Reports whether the chunk arrived.
//
// Chunks trail a teleport by a noticeable margin, and an unloaded chunk reads
// as air everywhere — so acting immediately after a teleport means acting on a
// world that looks empty.
func (c *Client) waitForChunk(ctx context.Context, timeout time.Duration) bool {
	// Re-read the position each time: the bot may still be being moved.
	loaded := func() bool {
		pos := c.Position()
		x, _, z := blockPos(pos.X, pos.Y, pos.Z)
		return c.world.hasChunk(x, z)
	}
	if loaded() {
		return true
	}

	ticker := time.NewTicker(chunkPollInterval)
	defer ticker.Stop()
	deadline := time.After(timeout)
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			return false
		case <-ticker.C:
			if loaded() {
				return true
			}
		}
	}
}

// handleWorldPacket decodes the terrain packets. Returns false if the packet
// was not one of them.
func (c *Client) handleWorldPacket(p protocol.Packet) (bool, error) {
	switch p.ID {
	case c.v.Packets.CBPlayMapChunk:
		r := p.Reader()
		x, z := r.I32(), r.I32()
		// Heightmaps: an array of {type, data[]}. Skipped, but it has to be
		// walked exactly or chunkData starts at the wrong offset.
		heightmaps := r.VarInt()
		for range heightmaps {
			r.VarInt() // type
			n := r.VarInt()
			for range n {
				r.I64()
			}
		}
		size := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		if size < 0 || int(size) > len(r.Remaining()) {
			return true, nil // malformed or truncated; drop rather than guess
		}
		blob := r.Remaining()[:size]
		dumpChunkBlob(blob)
		column, err := protocol.ParseChunkData(c.v, x, z, blob)
		if err != nil {
			// A chunk that fails to parse is dropped, not fatal: the bot keeps
			// playing with a hole in its map rather than dying on one packet.
			c.log.Warn("chunk parse failed", "x", x, "z", z, "err", err)
			return true, nil
		}
		c.world.store(column)
		return true, nil

	case c.v.Packets.CBPlayChunkBatchFinished:
		// The server sends chunks in batches and waits for the client to report
		// how fast it can absorb them. Without this reply the sender throttles
		// to a standstill after the first batch — terrain arrives at join and
		// never again, so the bot spends the rest of its life believing that
		// anywhere it teleports to is an infinite void.
		//
		// A headless client has no rendering to keep up with, so it asks for
		// the maximum rather than measuring itself; the server clamps it.
		r := p.Reader()
		batchSize := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		if c.v.Packets.SBPlayChunkBatchReceived == protocol.Absent {
			return true, nil
		}
		w := protocol.NewWriter(c.v.Packets.SBPlayChunkBatchReceived).F32(maxChunksPerTick)
		if err := c.conn.WritePacket(w.Bytes()); err != nil {
			return true, err
		}
		c.log.Debug("chunk batch acknowledged", "chunks", batchSize)
		return true, nil

	case c.v.Packets.CBPlayChunkBatchStart:
		return true, nil // nothing to do; the reply happens on finish

	case c.v.Packets.CBPlayUnloadChunk:
		r := p.Reader()
		// Note the order: Z comes first in this packet.
		z, x := r.I32(), r.I32()
		if err := r.Err(); err != nil {
			return true, err
		}
		c.world.drop(x, z)
		return true, nil

	case c.v.Packets.CBPlayBlockChange:
		r := p.Reader()
		packed := r.I64()
		state := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		x, y, z := protocol.DecodeBlockPos(packed)
		c.world.setBlockState(x, y, z, state)
		return true, nil

	case c.v.Packets.CBPlayMultiBlockChange:
		r := p.Reader()
		packed := r.I64()
		n := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		// Section coordinates: x:22, z:22, y:20.
		sectionX := int32(packed >> 42)
		sectionZ := int32(packed << 22 >> 42)
		sectionY := int32(packed << 44 >> 44)
		for range n {
			record := r.VarLong()
			if err := r.Err(); err != nil {
				return true, err
			}
			state := int32(record >> 12)
			local := record & 0xfff
			dx := int32(local>>8) & 0xf
			dz := int32(local>>4) & 0xf
			dy := int32(local) & 0xf
			c.world.setBlockState(
				sectionX*16+dx,
				sectionY*16+dy,
				sectionZ*16+dz,
				state)
		}
		return true, nil
	}
	return false, nil
}
