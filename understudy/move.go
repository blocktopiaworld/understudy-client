package understudy

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/blocktopiaworld/understudy-client/internal/geom"
	"github.com/blocktopiaworld/understudy-client/protocol"
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

	best, stalled := math.Inf(1), 0
	for {
		pos := c.Position()
		dx, dy, dz := x-pos.X, y-pos.Y, z-pos.Z
		dist := geom.Length(dx, dy, dz)
		if dist <= step {
			if err := c.MoveTo(x, y, z); err != nil {
				return err
			}
			return c.settleAfterWalk(ctx)
		}
		// Walking into a wall is not an error the server reports: it corrects
		// the position back and the loop asks again, forever. Left to itself
		// that burns the caller's whole timeout and then blames the context,
		// which says nothing about a wall. So notice the lack of progress and
		// name it.
		if dist < best-walkProgressEpsilon {
			best, stalled = dist, 0
		} else if stalled++; stalled >= walkStallTicks {
			return fmt.Errorf(
				"understudy: walking to %.1f,%.1f,%.1f made no progress for %v at "+
					"%.1f,%.1f,%.1f, %.1f blocks short — something is in the way "+
					"(this is dead reckoning, not pathfinding)",
				x, y, z, time.Duration(walkStallTicks)*TickRate,
				pos.X, pos.Y, pos.Z, dist)
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

// settleAfterWalk drops the bot if the walk finished over a hole.
//
// WalkTo is dead reckoning at a constant height: it interpolates toward the
// target and gravity is not part of that. So a walk that steps off a ledge ends
// with the bot standing in mid-air, still telling the server it is on the
// ground — and vanilla kicks a floating player after about four seconds with
// "multiplayer.disconnect.flying".
//
// Auto-fall does not cover this. It runs on a server teleport, which is the
// other way a bot ends up airborne, and walking off an edge never triggers it.
// That gap is why the flying kick kept coming back after each fix to the
// teleport path: they are two different paths and only one of them was fixed.
//
// A real player who walks off a ledge falls, so this does too — with real
// gravity and real fall damage, which is also what a test walking off a ledge
// to check a totem is asking for.
func (c *Client) settleAfterWalk(ctx context.Context) error {
	support := c.GroundBelow()
	if !support.Known || !support.Found || c.Position().Y-support.GroundY <= gravityEpsilon {
		// On the ground, or over a column that has not been sent. Falling into
		// terrain the client has not received is the mistake that made a bot
		// hover in the first place, so unknown means leave it alone.
		return nil
	}
	if _, err := c.Fall(ctx); err != nil {
		return fmt.Errorf("understudy: walked off an edge and could not settle: %w", err)
	}
	return nil
}

// walkStallTicks is how long WalkTo tolerates getting no closer before it
// gives up, and walkProgressEpsilon is what counts as closer. One second is
// well beyond the jitter of a server correcting a legitimate step, and far
// below the action timeout it used to consume in full.
const (
	walkStallTicks      = 20
	walkProgressEpsilon = 1e-3

	// gravityEpsilon is how far above the floor still counts as standing on
	// it. Position arithmetic lands fractionally off, and falling 0.0001 of a
	// block would be a round trip for nothing.
	gravityEpsilon = 0.05
)

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
