package understudy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blocktopia/understudy-client/protocol"
)

// nextSequence numbers this bot's block actions. Since 1.19 every dig and
// place carries a monotonically increasing sequence so the server can
// acknowledge the client's predicted change.
//
// Per-client, not package-level: the counter is meaningful only within one
// connection, and a shared one would couple every bot in a fleet to every
// other. Harmless in practice, but a library that leaks state between
// instances is one nobody can safely run two of.
func (c *Client) nextSequence() int32 { return c.blockSequence.Add(1) }

// digPollInterval is how often the world model is checked for the block going
// away. Well under a tick: the point is to notice the break as soon as the
// server's block-change lands, not on some slower cadence of our own.
const digPollInterval = 5 * time.Millisecond

// blockChangeTimeout is how long to wait for the world model to agree that a
// block changed before calling it a failure.
const blockChangeTimeout = 2 * time.Second

// swingInterval keeps the arm moving while breaking. Some server-side listeners
// key off the animation rather than the dig packets, and once every two ticks
// is what a real client looks like.
const swingInterval = 2 * TickRate

// Swing plays the arm animation. Purely cosmetic on its own — it does not
// hit anything. Attack and Dig are the packets that do work.
func (c *Client) Swing() error {
	if err := c.requireAlive("swing"); err != nil {
		return err
	}
	return c.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.SBPlayArmAnimation).VarInt(protocol.MainHand).Bytes())
}

// blockDig sends a block_dig packet with the given status. Breaking, dropping
// and releasing a held use all ride on this one packet.
//
// It is the single choke point for player_action, which is one of the three
// packet types the server drops while a teleport is settling — so the gate goes
// here rather than at each of the four verbs that reach it.
func (c *Client) blockDig(ctx context.Context, status, x, y, z, face int32) error {
	if err := c.awaitTeleportSettle(ctx); err != nil {
		return err
	}
	w := protocol.NewWriter(c.v.Packets.SBPlayBlockDig).
		VarInt(status).
		BlockPos(x, y, z).
		I8(int8(face)).
		VarInt(c.nextSequence())
	return c.conn.WritePacket(w.Bytes())
}

// StartDig begins breaking a block.
func (c *Client) StartDig(ctx context.Context, x, y, z, face int32) error {
	if err := c.requireAlive("dig"); err != nil {
		return err
	}
	return c.blockDig(ctx, protocol.DigStart, x, y, z, face)
}

// FinishDig completes breaking a block.
func (c *Client) FinishDig(ctx context.Context, x, y, z, face int32) error {
	if err := c.requireAlive("dig"); err != nil {
		return err
	}
	return c.blockDig(ctx, protocol.DigFinish, x, y, z, face)
}

// DigBlock aims at a block and breaks it, holding for the given duration.
//
// The duration has to cover the server's expected break time for that block
// with the currently held tool. Too short and the server rejects the finish
// and leaves the block standing, silently. Callers that care about speed
// should measure per block type rather than guess a single global value.
func (c *Client) DigBlock(ctx context.Context, x, y, z, face int32, hold time.Duration) error {
	if err := c.requireReach("break", x, y, z); err != nil {
		return err
	}
	if err := c.LookAtBlock(x, y, z); err != nil {
		return err
	}
	// Aiming is not the same as being able to hit it. Confirm the crosshair ray
	// actually lands on this block rather than something in front of it, and
	// take the face from the ray — a guessed face is the wrong one whenever the
	// block is approached from any other side.
	hit, s := c.LineOfSightTo(x, y, z)
	if err := s.err("break", x, y, z, hit); err != nil {
		return err
	}
	if s == sightClear {
		face = hit.Face
	}
	if err := c.StartDig(ctx, x, y, z, face); err != nil {
		return err
	}
	return c.awaitBreak(ctx, x, y, z, face, hold)
}

// awaitBreak waits for a block to actually break, sending the explicit finish
// only if it has not gone by itself.
//
// A fixed wait is wrong in both directions. With a good tool the server breaks
// the block the moment it sees START — no finish is needed at all — so any
// fixed hold is pure latency. Meanwhile a slow block needs *longer* than the
// caller guessed, and cutting it short leaves the block standing.
//
// Watching the world model handles both: return the instant it breaks,
// otherwise escalate to the finish packet and keep watching.
func (c *Client) awaitBreak(ctx context.Context, x, y, z, face int32, hold time.Duration) error {
	// Without terrain there is nothing to observe, so fall back to the old
	// fixed sequence rather than spin.
	if !c.ChunkLoaded(x, z) {
		if err := wait(ctx, hold); err != nil {
			return err
		}
		return c.FinishDig(ctx, x, y, z, face)
	}

	start := time.Now()
	// Allow well beyond the caller's hold before giving up: the hold is a hint
	// about tool speed, not a hard limit.
	deadline := start.Add(hold + blockChangeTimeout)
	finished := false
	var lastSwing time.Time

	for {
		if !c.IsTargetableAt(x, y, z) {
			return nil // gone — no finish packet was ever required
		}
		now := time.Now()
		if now.Sub(lastSwing) >= swingInterval {
			if err := c.Swing(); err != nil {
				return err
			}
			lastSwing = now
		}
		if !finished && now.Sub(start) >= hold {
			if err := c.FinishDig(ctx, x, y, z, face); err != nil {
				return err
			}
			finished = true
		}
		if now.After(deadline) {
			return fmt.Errorf("understudy: failed to break block at %d,%d,%d — still solid after %v "+
				"(wrong tool, or the server rejected the break)", x, y, z, now.Sub(start).Round(time.Millisecond))
		}
		if err := wait(ctx, digPollInterval); err != nil {
			return err
		}
	}
}

// confirmBlockBecame waits for the world model to agree that a block is now
// present (or gone), and reports if it never does.
//
// Presence is tested with IsTargetable rather than IsSolid. A cobweb or a wheat
// crop is not solid *while it is still there*, so a collision-based check
// reports the break as complete the instant it is asked — a false pass on
// exactly the blocks most likely to need a retry.
//
// The server ignores a dig it considers invalid — out of reach, wrong tool,
// too fast — without any reply, and ignores a placement into an occupied space
// the same way. Both look identical to success from the client's side, so the
// only honest confirmation is the block actually changing.
func (c *Client) confirmBlockBecame(ctx context.Context, x, y, z int32, wantSolid bool) error {
	if !c.ChunkLoaded(x, z) {
		return nil // no world data here; nothing to verify against
	}
	deadline := time.Now().Add(blockChangeTimeout)
	for {
		if c.IsTargetableAt(x, y, z) == wantSolid {
			return nil
		}
		if time.Now().After(deadline) {
			verb, state := "break", "still solid"
			if wantSolid {
				verb, state = "place", "still empty"
			}
			return fmt.Errorf("understudy: failed to %s block at %d,%d,%d — %s after %v "+
				"(out of reach, wrong tool, or the space was occupied)",
				verb, x, y, z, state, blockChangeTimeout)
		}
		if err := wait(ctx, digPollInterval); err != nil {
			return err
		}
	}
}

// DigBlocks breaks several blocks from where the bot is standing.
//
// The point is not to move: everything inside BlockReach can be worked from a
// single position, so a field is cleared in one pass instead of a teleport per
// block. Anything out of range is reported rather than swung at, and the
// remaining blocks are still attempted — one unreachable corner should not
// abandon the rest of the field.
func (c *Client) DigBlocks(ctx context.Context, blocks [][3]int32, face int32, hold time.Duration) (dug int, err error) {
	var unreachable []string
	for _, b := range blocks {
		if err := ctx.Err(); err != nil {
			return dug, err
		}
		if !c.CanReachBlock(b[0], b[1], b[2]) {
			unreachable = append(unreachable,
				fmt.Sprintf("%d,%d,%d (%.1f away)", b[0], b[1], b[2], c.BlockDistance(b[0], b[1], b[2])))
			continue
		}
		if err := c.DigBlock(ctx, b[0], b[1], b[2], face, hold); err != nil {
			return dug, err
		}
		dug++
	}
	if len(unreachable) > 0 {
		return dug, fmt.Errorf("understudy: dug %d of %d; out of reach from here: %s",
			dug, len(blocks), strings.Join(unreachable, "; "))
	}
	return dug, nil
}

// DropHeld drops the held item. With all true it throws the whole stack,
// otherwise a single item.
//
// Dropping rides on the block_dig packet with a status that means "drop"
// rather than "break" — the position and face are ignored, which is why they
// are sent as zeroes.
func (c *Client) DropHeld(ctx context.Context, all bool) error {
	if err := c.requireAlive("drop"); err != nil {
		return err
	}
	status := protocol.DigDropItem
	if all {
		status = protocol.DigDropStack
	}
	return c.blockDig(ctx, status, 0, 0, 0, 0)
}

// releaseUse ends a held right-click — the packet that actually commits eating,
// drinking and loosing a bow.
func (c *Client) releaseUse(ctx context.Context) error {
	return c.blockDig(ctx, protocol.DigReleaseUse, 0, 0, 0, 0)
}
