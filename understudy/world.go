package understudy

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/blocktopia/understudy-client/internal/geom"
	"github.com/blocktopia/understudy-client/internal/nbt"
	"github.com/blocktopia/understudy-client/protocol"
)

// BlockAt returns the block state at world coordinates.
func (c *Client) BlockAt(x, y, z int32) int32 { return c.world.BlockState(x, y, z) }

// ChunkLoaded reports whether terrain covering a coordinate is known.
func (c *Client) ChunkLoaded(x, z int32) bool { return c.world.HasChunk(x, z) }

// LoadedChunks returns how many chunk columns are currently held.
func (c *Client) LoadedChunks() int { return c.world.Loaded() }

// IsSolidAt reports whether the block at a coordinate blocks movement.
func (c *Client) IsSolidAt(x, y, z int32) bool {
	return c.v.IsSolid(c.world.BlockState(x, y, z))
}

// IsTargetableAt reports whether the crosshair would stop on this block —
// true for cobweb, crops and torches, which IsSolidAt reports as empty.
func (c *Client) IsTargetableAt(x, y, z int32) bool {
	return c.v.IsTargetable(c.world.BlockState(x, y, z))
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
	// Known is false when the column holding the search is not loaded, which
	// is a different answer from "searched and found nothing" and must not be
	// collapsed into it.
	//
	// The two were the same value for a long time, and the comment on
	// FindGround told callers not to confuse them without giving them any way
	// to tell. Fall then read the shared "not found" as "no floor here" and
	// descended into terrain it simply had not been sent yet, which a server
	// answers by refusing every move and then kicking for floating.
	Known bool
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
// Sets Known == false when the terrain is not loaded, and Found == false when
// it is loaded and nothing was hit. Callers must not treat the first as the
// second: an unloaded chunk reads as air everywhere, so "no data" would
// otherwise mean "the void".
func (c *Client) FindGround(x, y, z int32) Support {
	if !c.world.HasChunk(x, z) {
		return Support{Known: false}
	}
	support := Support{Known: true}
	c.world.Scan(func(at func(x, y, z int32) int32) {
		for probe := y; probe > y-maxGroundSearch; probe-- {
			state := at(x, probe, z)
			switch {
			case c.v.IsWater(state):
				// Feet go *inside* the top water block, not on top of it. Water
				// only cancels fall damage once the player is actually in it, so
				// stopping one block high lands the bot on "air above water" and
				// the server charges full fall damage — which is exactly the bug
				// this looks like it is avoiding.
				support = Support{GroundY: float64(probe), Found: true, InWater: true, Known: true}
				return
			case c.v.IsLava(state):
				support = Support{GroundY: float64(probe + 1), Found: true, InLava: true, Known: true}
				return
			case c.v.IsSolid(state):
				support = Support{GroundY: float64(probe + 1), Found: true, Known: true}
				return
			}
		}
	})
	return support
}

// GroundBelow finds what the bot is currently standing over.
func (c *Client) GroundBelow() Support {
	pos := c.Position()
	x, y, z := geom.BlockPos(pos.X, pos.Y, pos.Z)
	// Start the scan one below the feet — the block the bot occupies is the
	// one it is standing *in*, not standing on.
	return c.FindGround(x, y-1, z)
}

// Submerged reports whether the bot's head is inside water, which is the
// condition that drowns it.
func (c *Client) Submerged() bool {
	pos := c.Position()
	// The head block sits one above the feet.
	x, head, z := geom.BlockPos(pos.X, pos.Y+1.0, pos.Z)
	return c.v.IsWater(c.world.BlockState(x, head, z))
}

// WaterSurfaceAbove finds the Y at which the bot's head would clear the water,
// scanning up from its current position. Returns false if no surface is found.
func (c *Client) WaterSurfaceAbove() (surfaceY float64, found bool) {
	pos := c.Position()
	x, start, z := geom.BlockPos(pos.X, pos.Y, pos.Z)
	c.world.Scan(func(at func(x, y, z int32) int32) {
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
		x, _, z := geom.BlockPos(pos.X, pos.Y, pos.Z)
		return c.world.HasChunk(x, z)
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
		return true, c.handleMapChunk(p)

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
		c.world.Drop(x, z)
		return true, nil

	case c.v.Packets.CBPlayBlockChange:
		r := p.Reader()
		packed := r.I64()
		state := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		x, y, z := protocol.DecodeBlockPos(packed)
		c.world.SetBlockState(x, y, z, state)
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
			c.world.SetBlockState(
				sectionX*16+dx,
				sectionY*16+dy,
				sectionZ*16+dz,
				state)
		}
		return true, nil
	}
	return false, nil
}

// handleMapChunk decodes a chunk column and stores it.
//
// A chunk that fails to parse is dropped, not fatal: the bot keeps playing with
// a hole in its map rather than dying on one packet.
func (c *Client) handleMapChunk(p protocol.Packet) error {
	r := p.Reader()
	x, z := r.I32(), r.I32()
	if err := c.skipHeightmaps(r, x, z); err != nil {
		return err
	}
	size := r.VarInt()
	if err := r.Err(); err != nil {
		return err
	}
	if size < 0 || int(size) > len(r.Remaining()) {
		return nil // malformed or truncated; drop rather than guess
	}
	blob := r.Remaining()[:size]
	dumpChunkBlob(blob)
	column, err := protocol.ParseChunkData(c.v, x, z, blob)
	if err != nil {
		c.log.Warn("chunk parse failed", "x", x, "z", z, "err", err)
		return nil
	}
	c.world.Store(column)
	return nil
}

// skipHeightmaps steps over the heightmaps between the chunk coordinates and
// the chunk data.
//
// Nothing here reads them, but they must be walked *exactly* or the data blob
// starts at the wrong offset — and that surfaces as a short read deep inside a
// later section, nowhere near the real mistake.
//
// Their shape changed in 1.21.5. Before that they are a single nameless NBT
// compound; from 1.21.5 they are a prefixed array of {type, long[]}. Reading
// the new shape off the old one takes a VarInt out of the middle of NBT and
// walks a garbage array.
func (c *Client) skipHeightmaps(r *protocol.Reader, x, z int32) error {
	if c.v.Chunk.NBTHeightmaps {
		n, err := nbt.SkipTag(r.Remaining())
		if err != nil {
			return fmt.Errorf("understudy: chunk %d,%d heightmaps: %w", x, z, err)
		}
		r.Skip(n)
		return nil
	}
	for range r.VarInt() {
		r.VarInt() // type
		for range r.VarInt() {
			r.I64()
		}
	}
	return r.Err()
}

// dumpChunkBlob writes the first raw chunkData payload to the path in
// UNDERSTUDY_DUMP_CHUNK, if set.
//
// Chunk framing is the one part of this protocol that cannot be reasoned out
// from a field list: a mistake shows up as a short read many sections later,
// nowhere near the actual error. Having the real bytes to measure against
// turns that into arithmetic.
var dumpOnce sync.Once

func dumpChunkBlob(blob []byte) {
	path := os.Getenv("UNDERSTUDY_DUMP_CHUNK")
	if path == "" {
		return
	}
	dumpOnce.Do(func() {
		// The path comes from UNDERSTUDY_DUMP_CHUNK, set by whoever is running
		// the bot; there is no untrusted input here.
		_ = os.WriteFile(path, blob, 0o644) //nolint:gosec // G703: operator-supplied debug path
	})
}
