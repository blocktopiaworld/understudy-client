package understudy

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/blocktopia/understudy-client/internal/ballistics"
	"github.com/blocktopia/understudy-client/internal/geom"
)

// BowFullDraw is how long the bow must be held for maximum power.
const BowFullDraw = ballistics.FullDraw

// bowAimHeight is mid-body, so an arrow does not pass under a mob.
const bowAimHeight = 0.9

// BowPower converts a draw duration into vanilla's launch power, 0..1.
//
// The curve is not linear: half a second of draw gives roughly 0.4 power, not
// 0.5, so a caller tuning for range needs the real curve rather than the
// intuition. See internal/ballistics for the arithmetic.
func BowPower(draw time.Duration) float64 { return ballistics.Power(draw) }

// AimBow points the bot so an arrow at the given draw strength will land on a
// world coordinate, accounting for gravity and drag.
func (c *Client) AimBow(x, y, z float64, draw time.Duration) error {
	pos := c.Position()
	eyeY := pos.Y + ArrowEyeHeight

	dx, dz := x-pos.X, z-pos.Z
	horizontal := math.Hypot(dx, dz)
	vertical := y - eyeY

	pitch, ok := ballistics.SolvePitch(horizontal, vertical, BowPower(draw))
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
	if draw < ballistics.MinDraw {
		return fmt.Errorf("understudy: draw of %v is below the ~%v minimum; the bow will not fire",
			draw, ballistics.MinDraw)
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
	cx, cy, cz := geom.BlockCentre(x, y, z)
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
