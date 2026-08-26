package understudy

import (
	"context"
	"fmt"
	"time"

	"github.com/blocktopia/understudy-client/internal/geom"
)

// RayHit is a block the crosshair landed on.
type RayHit struct {
	X        int32   `json:"x"`
	Y        int32   `json:"y"`
	Z        int32   `json:"z"`
	Face     int32   `json:"face"`
	Distance float64 `json:"distance"`
	State    int32   `json:"state"`
}

// RayTrace walks the voxel grid from a point along a direction and returns the
// first targetable block within maxDist.
//
// The traversal itself is geom.Raycast; what this adds is the only thing that
// needs a client — deciding whether a voxel stops the ray, which depends on
// terrain the bot has been sent and on the version's block tables.
//
// The whole walk runs under a single terrain lock, so it cannot observe a
// half-applied block update partway along the ray.
func (c *Client) RayTrace(ox, oy, oz, dx, dy, dz, maxDist float64) (RayHit, bool) {
	var hit RayHit
	found := false
	c.world.Scan(func(at func(x, y, z int32) int32) {
		h, ok := geom.Raycast(ox, oy, oz, dx, dy, dz, maxDist,
			func(x, y, z int32) bool { return c.v.IsTargetable(at(x, y, z)) })
		if !ok {
			return
		}
		hit = RayHit{
			X: h.X, Y: h.Y, Z: h.Z,
			Face: h.Face, Distance: h.Distance,
			State: at(h.X, h.Y, h.Z),
		}
		found = true
	})
	return hit, found
}

// LookingAt returns the block the bot's crosshair is currently on, if any is
// within reach.
func (c *Client) LookingAt() (RayHit, bool) {
	pos := c.Position()
	eyeX, eyeY, eyeZ := c.eyes()
	dx, dy, dz := LookDirection(pos.Yaw, pos.Pitch)
	return c.RayTrace(eyeX, eyeY, eyeZ, dx, dy, dz, BlockReach)
}

// sight is the outcome of a line-of-sight check.
//
// Three states, not a bool: "nothing was hit" and "something else was hit" need
// different error messages, and a two-value form cannot tell them apart. The
// previous version papered over that by treating a hit at 0,0,0 as "no hit",
// which is a real coordinate — a bot working near the world origin would have
// been told the wrong thing.
type sight int

const (
	// sightClear means the ray landed on the requested block.
	sightClear sight = iota
	// sightBlocked means something else is in the way.
	sightBlocked
	// sightEmpty means the ray hit nothing at all within reach.
	sightEmpty
)

// err renders a sight as an actionable error, or nil when the path is clear.
func (s sight) err(action string, x, y, z int32, hit RayHit) error {
	switch s {
	case sightClear:
		return nil
	case sightEmpty:
		return fmt.Errorf("understudy: cannot %s block at %d,%d,%d — nothing solid along the line of sight",
			action, x, y, z)
	default:
		return fmt.Errorf("understudy: cannot %s block at %d,%d,%d — %d,%d,%d is in the way (%.2f blocks along)",
			action, x, y, z, hit.X, hit.Y, hit.Z, hit.Distance)
	}
}

// LineOfSightTo reports whether the bot could actually hit a block from where
// it stands, and what is in the way if not.
//
// The check is deliberately "aim at it and see what the ray hits first",
// because that is the question the game answers.
//
// Terrain that is not loaded reads as air everywhere, which would wrongly look
// like a clear path, so an unloaded chunk reports sightClear — the caller has
// nothing better to go on and the server will arbitrate.
func (c *Client) LineOfSightTo(x, y, z int32) (RayHit, sight) {
	if !c.ChunkLoaded(x, z) {
		return RayHit{}, sightClear
	}
	eyeX, eyeY, eyeZ := c.eyes()
	// Aim at the block's centre, as LookAtBlock would.
	tx, ty, tz := geom.BlockCentre(x, y, z)
	dx, dy, dz := tx-eyeX, ty-eyeY, tz-eyeZ
	dist := geom.Length(dx, dy, dz)
	if dist == 0 {
		return RayHit{}, sightEmpty
	}
	hit, ok := c.RayTrace(eyeX, eyeY, eyeZ, dx/dist, dy/dist, dz/dist, BlockReach)
	switch {
	case !ok:
		return RayHit{}, sightEmpty
	case hit.X == x && hit.Y == y && hit.Z == z:
		return hit, sightClear
	default:
		return hit, sightBlocked
	}
}

// HasLineOfSight reports whether the crosshair would land on a block, for
// callers that only need the yes/no answer.
func (c *Client) HasLineOfSight(x, y, z int32) bool {
	_, s := c.LineOfSightTo(x, y, z)
	return s == sightClear
}

// DigLookingAt breaks whatever the crosshair is on.
//
// This is the game's own model — aim, then mine what you are pointing at —
// and it is the safer primitive: the face comes from the ray rather than being
// guessed, and there is no way to name a block the bot cannot actually hit.
func (c *Client) DigLookingAt(ctx context.Context, hold time.Duration) (RayHit, error) {
	hit, ok := c.LookingAt()
	if !ok {
		pos := c.Position()
		return RayHit{}, fmt.Errorf(
			"understudy: nothing solid within %.1f blocks of the crosshair (yaw %.1f, pitch %.1f)",
			BlockReach, pos.Yaw, pos.Pitch)
	}
	if err := c.StartDig(ctx, hit.X, hit.Y, hit.Z, hit.Face); err != nil {
		return hit, err
	}
	// Shares awaitBreak with DigBlock rather than running its own fixed-hold
	// loop. The two had drifted: this path always waited out the full hold and
	// always sent FinishDig, so it was slower on a fast tool and gave up early
	// on a slow one — the exact pair of bugs awaitBreak exists to avoid.
	return hit, c.awaitBreak(ctx, hit.X, hit.Y, hit.Z, hit.Face, hold)
}
