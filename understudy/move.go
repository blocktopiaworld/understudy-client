package understudy

import (
	"context"
	"time"

	"github.com/blocktopia/understudy-client/internal/geom"
	"github.com/blocktopia/understudy-client/protocol"
)

// TickRate is the server tick, and the natural cadence for movement updates.
// A real client sends position roughly once per tick; sending far faster is
// wasted, and sending far slower makes movement look like teleporting and
// risks a "moved too quickly" rejection.
const TickRate = 50 * time.Millisecond

// WalkSpeed is a player's normal walking speed in blocks per second.
const WalkSpeed = 4.317

// movementFlags builds the trailing flags byte. onGround matters: a bot that
// always claims to be airborne is a bot the server may treat as flying.
func movementFlags(onGround bool) uint8 {
	if onGround {
		return protocol.MovementOnGround
	}
	return 0
}

// MoveTo sets the bot's position directly in a single packet.
//
// The server validates how far a player moved in one update and rejects
// implausible jumps, so this is only safe for short hops. Use WalkTo to cover
// distance, or a server-side teleport to cross the world.
func (c *Client) MoveTo(x, y, z float64) error {
	if err := c.requireAlive("move"); err != nil {
		return err
	}
	return c.writePosition(x, y, z, c.OnGround())
}

// sendPosition writes a movement packet with an explicit on-ground flag.
//
// The flag is the whole point: the server derives fall damage from the
// *client's* reported descent and applies it on the transition back to
// on-ground. Falling is therefore something the bot performs, not something
// the server does to it.
func (c *Client) sendPosition(x, y, z float64, onGround bool) error {
	return c.writePosition(x, y, z, onGround)
}

// writePosition records the new position and sends it. It is the single place
// an outbound position packet is built, so the local state and what the server
// is told can never drift apart.
func (c *Client) writePosition(x, y, z float64, onGround bool) error {
	c.mu.Lock()
	c.pos.X, c.pos.Y, c.pos.Z = x, y, z
	yaw, pitch := c.pos.Yaw, c.pos.Pitch
	c.onGround = onGround
	c.mu.Unlock()

	w := protocol.NewWriter(c.v.Packets.SBPlayPositionLook).
		F64(x).F64(y).F64(z).
		F32(yaw).F32(pitch).
		U8(movementFlags(onGround))
	if err := c.conn.WritePacket(w.Bytes()); err != nil {
		return err
	}
	// Tell the idle loop it need not speak this tick.
	c.markPositionSent()
	return nil
}

// WalkTo moves the bot to a target at walking speed, one step per tick.
//
// This is dead reckoning, not pathfinding: it walks a straight line and does
// not know about walls, drops or water. That is deliberate rather than missing.
// Callers typically position a bot in terrain they control, so the movement
// that has to look believable is short and local.
func (c *Client) WalkTo(ctx context.Context, x, y, z float64) error {
	if err := c.requireAlive("walk"); err != nil {
		return err
	}
	step := WalkSpeed * TickRate.Seconds()
	ticker := time.NewTicker(TickRate)
	defer ticker.Stop()

	for {
		pos := c.Position()
		dx, dy, dz := x-pos.X, y-pos.Y, z-pos.Z
		dist := geom.Length(dx, dy, dz)
		if dist <= step {
			return c.MoveTo(x, y, z)
		}
		scale := step / dist
		if err := c.MoveTo(pos.X+dx*scale, pos.Y+dy*scale, pos.Z+dz*scale); err != nil {
			return err
		}
		if err := sleep(ctx, ticker.C); err != nil {
			return err
		}
	}
}

// sleep waits for a tick or for ctx to be cancelled, reporting cancellation as
// an error so a caller can propagate it with a bare check.
func sleep(ctx context.Context, tick <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tick:
		return nil
	}
}

// wait blocks for d, or until ctx is cancelled.
func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	return sleep(ctx, timer.C)
}
