package understudy

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Minecraft's player gravity, applied per tick: velocity picks up 0.08 blocks
// of downward acceleration and is then scaled by drag. Terminal velocity works
// out at about 3.92 blocks per tick.
const (
	gravityPerTick = 0.08
	fallDrag       = 0.98
)

// MaxFallBlocks bounds a self-detecting fall. Without a floor beneath it a bot
// would otherwise descend into the void forever, so an unbounded search is
// reported as an error instead of a very slow death.
const MaxFallBlocks = 512

// swimSpeed is how fast the bot rises through water. Swimming upward is slower
// than falling; this is a plausible rate the server will not reject as flying.
const swimSpeed = 0.12

// autoFallDelay lets a teleport settle before the bot starts falling. Chunks
// around the destination still have to load, and descending into terrain the
// server has not finished sending invites a spurious correction.
const autoFallDelay = 250 * time.Millisecond

// Fall drops the bot until it lands, detecting the ground by itself.
//
// The client decodes chunks, so it usually knows where the floor is — but it
// does not need to, because the *server* is the authority on solid ground. A
// move that would put the player inside terrain is rejected and the server
// snaps them back with a position correction, so a correction arriving
// mid-descent means "you have landed, and here is exactly where".
//
// This is why the fall is simulated tick by tick at real gravity rather than
// jumped in one packet: a plausible descent lands on the floor, whereas a
// single huge step is corrected as "moved too quickly" and tells you nothing
// about where the ground was.
func (c *Client) Fall(ctx context.Context) (blocks float64, err error) {
	if err := c.requireAlive("fall"); err != nil {
		return 0, err
	}
	start := c.Position()

	// With terrain loaded the floor is a lookup, so the descent can stop on the
	// exact block instead of overshooting into it and being corrected back to
	// wherever the last valid position happened to be.
	support := c.GroundBelow()
	if !support.Known {
		// The column has not arrived. "I have not been sent the terrain" is not
		// "there is no terrain", and descending on the second reading is how a
		// bot teleported onto a ledge fourteen thousand blocks away ends up
		// kicked: it pushes into ground the server can see and the client
		// cannot, the server refuses every move, and after four seconds of a
		// player claiming to be airborne at a constant height it disconnects
		// them for floating.
		//
		// Staying put is the safe reading. If there really is nothing under the
		// bot the server will start it falling, which is its job — the client
		// simulates a fall to land precisely, not to discover gravity.
		//
		// Claiming to be standing is the safe half of that. The server is the
		// authority on solid ground: if the bot really is in mid-air it will
		// say so and start the fall itself, whereas a client that volunteers
		// "airborne" over terrain it cannot see is counted toward the floating
		// threshold for as long as it keeps saying it.
		c.mu.Lock()
		c.onGround = true
		c.mu.Unlock()
		c.log.Debug("not falling: the column below has not been sent",
			"x", start.X, "y", start.Y, "z", start.Z)
		return 0, nil
	}
	if !support.Found {
		return c.descend(ctx, descent{start: start, blind: true})
	}
	// Already standing on it. That is auto-fall's most common outcome and it is
	// a success, not a failure — reporting it as an error made every ordinary
	// teleport log a warning, which is how real warnings stop being read.
	if start.Y <= support.GroundY {
		return 0, nil
	}
	if support.InLava {
		return 0, fmt.Errorf("understudy: refusing to fall into lava at y=%.0f", support.GroundY)
	}
	if support.InWater {
		// Water cancels fall damage completely and then drowns anything that
		// stays under, so entering it is where the fall ends. onGround stays
		// false: a player floating in water is not standing on anything, and
		// claiming otherwise is what makes the server treat entry as an impact.
		fell, err := c.descend(ctx, descent{start: start, target: &support.GroundY, landOnGround: false})
		if err != nil {
			return fell, err
		}
		return fell, c.surface(ctx)
	}
	return c.descend(ctx, descent{start: start, target: &support.GroundY, landOnGround: true})
}

// FallTo drops the bot to a known ground height.
//
// It still stops early if the server corrects the descent, so a groundY that is
// too low lands correctly rather than leaving the bot hovering. Prefer Fall
// unless the floor height is genuinely known and the extra precision matters.
func (c *Client) FallTo(ctx context.Context, groundY float64) (blocks float64, err error) {
	if err := c.requireAlive("fall"); err != nil {
		return 0, err
	}
	start := c.Position()
	if start.Y <= groundY {
		return 0, fmt.Errorf("understudy: already at or below ground level (y=%.2f, target=%.2f)",
			start.Y, groundY)
	}
	return c.descend(ctx, descent{start: start, target: &groundY, landOnGround: true})
}

// EnsureGrounded makes the bot fall if it is hovering, and settles almost
// immediately if it is already standing on something.
//
// Worth calling after anything that can leave a bot in mid-air — a teleport to
// an unverified spot, or mining the block it was standing on. Vanilla kicks a
// floating player after about four seconds ("floating too long"), and that kick
// lands with no warning.
func (c *Client) EnsureGrounded(ctx context.Context) (fell float64, err error) {
	return c.Fall(ctx)
}

// descent parameterises one gravity-driven drop.
//
// Fall, FallTo and the water-entry case were three near-identical tick loops
// that had already drifted apart — only one of them tracked sideways
// corrections, and only one reported the void as an error. Collapsing them into
// a single loop is what keeps a fix to the landing logic from having to be
// made three times.
type descent struct {
	start Position
	// target is the height to stop at, or nil to fall until something stops us.
	target *float64
	// landOnGround is the on-ground flag sent in the final packet. It is what
	// triggers the server's fall-damage calculation, so entering water (where
	// no impact happens) sets it false.
	landOnGround bool
	// blind means no floor is known, so only a server correction, a death or
	// MaxFallBlocks can end the descent.
	blind bool
}

// descend runs the tick loop, returning how far the bot actually fell.
func (c *Client) descend(ctx context.Context, d descent) (blocks float64, err error) {
	corrections := c.Corrections()
	// A fatal fall respawns the bot somewhere else entirely. The loop's own y
	// would then keep descending from the *old* column and the next packet
	// lands as an impossible jump ("moved too quickly"), so a death has to end
	// the fall rather than let it run on stale coordinates.
	deaths := c.Deaths()

	ticker := time.NewTicker(TickRate)
	defer ticker.Stop()

	x, y, z := d.start.X, d.start.Y, d.start.Z
	velocity := 0.0

	for d.start.Y-y < MaxFallBlocks {
		velocity = (velocity - gravityPerTick) * fallDrag
		y += velocity

		if d.target != nil && y <= *d.target {
			// Land exactly on the floor and flag on-ground in the same packet:
			// that transition is what triggers the damage calculation.
			return d.start.Y - *d.target, c.sendPosition(x, *d.target, z, d.landOnGround)
		}
		if err := c.sendPosition(x, y, z, false); err != nil {
			return d.start.Y - y, err
		}
		if err := sleep(ctx, ticker.C); err != nil {
			return d.start.Y - y, err
		}

		if c.Deaths() > deaths || c.Dead() {
			// The impact killed us. The respawn teleport is authoritative and
			// has already been applied; anything this loop sends now would be
			// relative to a column the bot has left.
			return d.start.Y - c.Position().Y, nil
		}
		if c.Corrections() > corrections {
			// The server refused the descent: we are standing on something. Its
			// position is authoritative, so adopt it and confirm on-ground.
			landed := c.Position()
			return d.start.Y - landed.Y, c.sendPosition(landed.X, landed.Y, landed.Z, true)
		}
		// Track sideways corrections so the column stays under the bot.
		cur := c.Position()
		x, z = cur.X, cur.Z
	}
	if d.blind {
		return d.start.Y - y, fmt.Errorf(
			"understudy: no ground found within %d blocks below y=%.2f — is the bot over the void?",
			MaxFallBlocks, d.start.Y)
	}
	return d.start.Y - y, nil
}

// maxSurfaceTicks bounds the swim up. At swimSpeed this covers ~48 blocks of
// water, far deeper than anything a caller is likely to build.
const maxSurfaceTicks = 400

// surface swims the bot up until its head clears the water.
//
// Without this a bot that lands in water simply stays under and drowns — the
// server applies suffocation damage on a timer and nothing in the client would
// notice until the death packet arrived.
func (c *Client) surface(ctx context.Context) error {
	if !c.Submerged() {
		return nil
	}
	target, ok := c.WaterSurfaceAbove()
	if !ok {
		return fmt.Errorf("understudy: submerged with no water surface found above")
	}

	ticker := time.NewTicker(TickRate)
	defer ticker.Stop()

	for range maxSurfaceTicks {
		pos := c.Position()
		if pos.Y >= target || !c.Submerged() {
			return c.sendPosition(pos.X, pos.Y, pos.Z, false)
		}
		if err := c.sendPosition(pos.X, math.Min(pos.Y+swimSpeed, target), pos.Z, false); err != nil {
			return err
		}
		if err := sleep(ctx, ticker.C); err != nil {
			return err
		}
		if c.Dead() {
			return nil
		}
	}
	return nil
}

// autoFall settles the bot onto solid ground after a teleport.
//
// It MUST run off the read loop. A fall is driven by watching for server
// position corrections, and those only arrive if Run is free to keep reading —
// doing this inline would deadlock the client against itself.
func (c *Client) autoFall(ctx context.Context) {
	if c.opts.DisableAutoFall || c.Dead() {
		return
	}
	if !c.falling.CompareAndSwap(false, true) {
		return // a fall is already in flight; its own corrections land here
	}
	c.background(func() {
		defer c.falling.Store(false)

		if err := wait(ctx, autoFallDelay); err != nil {
			return
		}
		// A teleport outruns its terrain: the destination chunk usually has not
		// arrived yet. Falling now would find no ground and drop back to the
		// imprecise server-correction path, which is how a bot ends up hovering
		// two blocks above a pool. Wait for the chunk first.
		c.waitForChunk(ctx, chunkWaitTimeout)

		fell, err := c.Fall(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			// Not fatal on its own, but a bot that cannot find ground is one
			// that is about to be kicked, so it must not fail silently.
			c.log.Warn("auto-fall did not reach ground", "err", err, "fell", fell)
		case fell > 1:
			c.log.Info("auto-fall settled the bot", "fell_blocks", fell)
		}
	})
}
