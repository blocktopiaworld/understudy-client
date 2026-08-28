package understudy

import (
	"context"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// placeAttempts is how many times PlaceBlockVerified will send the packet.
//
// More than one because the FIRST block action of a session is sometimes
// dropped by the server with no rejection — the same fault the missing
// player_loaded packet used to cause for digs, which is fixed, and which this
// still reproduces for places. Digs never showed it because awaitBreak keeps
// swinging and re-sends FinishDig, so it retries without meaning to.
//
// Two attempts is enough — it is the first action that is dropped, never a
// later one.
const placeAttempts = 2

// PlaceBlock right-clicks a block face, which places the held item against it.
//
// The cursor position is the hit point within the face, in 0..1. Centre is a
// safe default; it only matters for blocks whose placement depends on where
// they were clicked, such as slabs and stairs.
func (c *Client) PlaceBlock(ctx context.Context, x, y, z, face int32) error {
	if err := c.requireAlive("place"); err != nil {
		return err
	}
	if err := c.awaitTeleportSettle(ctx); err != nil {
		return err
	}
	if err := c.requireReach("place against", x, y, z); err != nil {
		return err
	}
	if err := c.LookAtBlock(x, y, z); err != nil {
		return err
	}
	w := protocol.NewWriter(c.v.Packets.SBPlayBlockPlace).
		VarInt(protocol.MainHand).
		BlockPos(x, y, z).
		VarInt(face).
		F32(BlockCentreOffset).F32(BlockCentreOffset).F32(BlockCentreOffset). // cursor within the face
		Bool(false).                                                          // insideBlock
		Bool(false).                                                          // worldBorderHit
		VarInt(c.nextSequence())
	if err := c.conn.WritePacket(w.Bytes()); err != nil {
		return err
	}
	return c.Swing()
}

// PlaceBlockVerified places a block and confirms it actually appeared,
// re-sending once if it didn't.
//
// Use this when the placement is the point. PlaceBlock is left unverified
// because it doubles as "right-click this block" for opening UIs, where
// nothing is expected to change.
func (c *Client) PlaceBlockVerified(ctx context.Context, x, y, z, face int32) error {
	target := BlockOffsetByFace(x, y, z, face)
	var err error
	for range placeAttempts {
		if err = c.PlaceBlock(ctx, x, y, z, face); err != nil {
			return err
		}
		if err = c.confirmBlockBecame(ctx, target[0], target[1], target[2], true); err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return err
}

// BlockOffsetByFace returns the coordinate a block placed against a face
// occupies — the neighbour, not the clicked block itself.
func BlockOffsetByFace(x, y, z, face int32) [3]int32 {
	switch face {
	case protocol.FaceBottom:
		return [3]int32{x, y - 1, z}
	case protocol.FaceTop:
		return [3]int32{x, y + 1, z}
	case protocol.FaceNorth:
		return [3]int32{x, y, z - 1}
	case protocol.FaceSouth:
		return [3]int32{x, y, z + 1}
	case protocol.FaceWest:
		return [3]int32{x - 1, y, z}
	default:
		return [3]int32{x + 1, y, z}
	}
}

// UseItem right-clicks with the held item without targeting a block — eating,
// drinking, throwing, and using a bucket in mid-air.
func (c *Client) UseItem(ctx context.Context) error {
	if err := c.requireAlive("use item"); err != nil {
		return err
	}
	if err := c.awaitTeleportSettle(ctx); err != nil {
		return err
	}
	pos := c.Position()
	w := protocol.NewWriter(c.v.Packets.SBPlayUseItem).
		VarInt(protocol.MainHand).
		VarInt(c.nextSequence()).
		F32(pos.Yaw).F32(pos.Pitch)
	return c.conn.WritePacket(w.Bytes())
}

// UseOnBlock right-clicks a block without placing anything — opening a
// crafting table, anvil, furnace or chest, or using a bed.
//
// It is the same packet as PlaceBlock; what differs is intent and what is in
// hand. Several statistics (interact_with_anvil and friends) count the GUI
// opening rather than any item being used, so an empty hand is usually right.
func (c *Client) UseOnBlock(ctx context.Context, x, y, z, face int32) error {
	return c.PlaceBlock(ctx, x, y, z, face)
}
