package understudy

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Bow draw timing. A bow reaches full power after 20 ticks of holding; below
// about 3 ticks the shot is not released at all.
const (
	BowFullDraw    = 20 * TickRate
	bowMinDrawTick = 3
)

// Arrow flight constants, matching vanilla's AbstractArrow.tick.
//
// Order matters as much as the values: the arrow moves, then drag scales the
// whole velocity, then gravity is subtracted from Y. Applying gravity before
// drag produces a subtly flatter arc that misses at range.
const (
	arrowSpeedPerPower = 3.0  // blocks/tick at full draw
	arrowDrag          = 0.99 // per tick, applied to every component
	arrowGravity       = 0.05 // blocks/tick², after drag
	bowAimHeight       = 0.9  // mid-body, so an arrow does not pass under a mob
)

// BowPower converts a draw duration into vanilla's launch power, 0..1.
//
// The curve is not linear: (t² + 2t)/3 for t = draw/full. Half a second of
// draw gives roughly 0.4 power, not 0.5, so a caller tuning for range needs
// the real curve rather than the intuition.
func BowPower(draw time.Duration) float64 {
	if draw <= 0 {
		return 0
	}
	// t is the draw in units of a full draw. It was previously written as
	// `seconds * 20 / 20` — ticks, then ticks-per-full-draw — which cancels to
	// exactly this and only looked like it was doing something.
	t := draw.Seconds() / BowFullDraw.Seconds()
	return math.Min((t*t+2*t)/3, 1)
}

// simulateArrow flies an arrow launched at a pitch and reports the height it
// has reached once it has covered horizontal distance d.
//
// Simulation rather than a closed-form solution because drag makes the exact
// trajectory awkward to invert, and a tick-accurate loop is both shorter and
// harder to get subtly wrong.
func simulateArrow(pitchRad, power, d float64) (height float64, reached bool) {
	speed := power * arrowSpeedPerPower
	vx := math.Cos(pitchRad) * speed
	vy := math.Sin(pitchRad) * speed

	x, y := 0.0, 0.0
	for range 600 {
		x += vx
		y += vy
		vx *= arrowDrag
		vy = vy*arrowDrag - arrowGravity

		if x >= d {
			return y, true
		}
		// Falling and already well past the target's level: it will not climb
		// back, so stop rather than simulate it into the ground.
		if vy < 0 && y < -256 {
			break
		}
	}
	return y, false
}

// solveBowPitch finds the launch pitch that drops an arrow onto a target.
//
// It searches the flat-trajectory solution (the shallower of the two arcs),
// which is what a player would use: faster to reach the target and far less
// sensitive to small aiming errors than the lobbed one.
func solveBowPitch(horizontal, vertical, power float64) (pitchDeg float64, ok bool) {
	if horizontal <= 0.01 {
		// Straight up or down; no ballistic solution needed.
		if vertical >= 0 {
			return -90, true
		}
		return 90, true
	}

	best, bestErr := 0.0, math.MaxFloat64
	// Sweep from steeply down to steeply up in fine steps. 0.25° is well
	// inside the accuracy a target block needs at practical range.
	for deg := -89.0; deg <= 89.0; deg += 0.25 {
		h, reached := simulateArrow(deg*math.Pi/180, power, horizontal)
		if !reached {
			continue
		}
		if e := math.Abs(h - vertical); e < bestErr {
			best, bestErr = deg, e
		}
	}
	if bestErr > 1.5 {
		return 0, false // no arc gets within 1.5 blocks; out of range
	}
	// Protocol pitch is inverted relative to the maths convention: negative
	// looks up.
	return -best, true
}

// AimBow points the bot so an arrow at the given draw strength will land on a
// world coordinate, accounting for gravity and drag.
func (c *Client) AimBow(x, y, z float64, draw time.Duration) error {
	pos := c.Position()
	eyeY := pos.Y + ArrowEyeHeight

	dx, dz := x-pos.X, z-pos.Z
	horizontal := math.Hypot(dx, dz)
	vertical := y - eyeY

	pitch, ok := solveBowPitch(horizontal, vertical, BowPower(draw))
	if !ok {
		return fmt.Errorf("understudy: no bow trajectory reaches %.1f blocks away and %.1f up at %.0f%% draw",
			horizontal, vertical, BowPower(draw)*100)
	}
	yaw := float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	return c.Look(yaw, float32(pitch))
}

// DrawBow holds the bow for a duration and looses the arrow.
//
// Like eating, this is a held action: the use has to be started, held, and
// then explicitly released. Sending only the start leaves the bot standing
// there at full draw forever, having fired nothing.
func (c *Client) DrawBow(ctx context.Context, draw time.Duration) error {
	if err := c.requireAlive("draw bow"); err != nil {
		return err
	}
	if draw < bowMinDrawTick*TickRate {
		return fmt.Errorf("understudy: draw of %v is below the ~%v minimum; the bow will not fire",
			draw, bowMinDrawTick*TickRate)
	}
	if err := c.UseItem(ctx); err != nil {
		return err
	}
	if err := wait(ctx, draw); err != nil {
		return err
	}
	return c.releaseUse(ctx)
}

// ShootAt aims at a point and looses an arrow at the given draw strength.
//
// The bow must already be in hand and arrows in the inventory; neither is
// checked here because the server's refusal is silent and a caller wanting
// certainty should verify the outcome (a target_hit stat, a dead mob) rather
// than trust the shot.
func (c *Client) ShootAt(ctx context.Context, x, y, z float64, draw time.Duration) error {
	if err := c.AimBow(x, y, z, draw); err != nil {
		return err
	}
	// Let the rotation land before the draw starts; the server uses the look
	// direction at the moment of *release*, but aiming first keeps the whole
	// action visible and ordered.
	if err := wait(ctx, 2*TickRate); err != nil {
		return err
	}
	return c.DrawBow(ctx, draw)
}

// ShootBlock aims at the centre of a block face and shoots it.
func (c *Client) ShootBlock(ctx context.Context, x, y, z int32, draw time.Duration) error {
	cx, cy, cz := blockCentre(x, y, z)
	return c.ShootAt(ctx, cx, cy, cz, draw)
}

// ShootNearest shoots the closest entity of a type, aiming at its body.
func (c *Client) ShootNearest(ctx context.Context, typeName string, draw time.Duration) (Entity, error) {
	target, err := c.NearestEntity(typeName)
	if err != nil {
		return Entity{}, err
	}
	// Aim at mid-body rather than the feet: an arrow that lands a block low
	// passes under most mobs.
	return target, c.ShootAt(ctx, target.X, target.Y+bowAimHeight, target.Z, draw)
}
