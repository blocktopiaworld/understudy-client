package understudy

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/blocktopia/understudy-client/protocol"
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

// LookDirection converts a yaw/pitch in degrees to a unit direction vector,
// in Minecraft's convention: yaw 0 faces +Z, and a negative pitch looks up.
func LookDirection(yaw, pitch float32) (dx, dy, dz float64) {
	y := float64(yaw) * math.Pi / 180
	p := float64(pitch) * math.Pi / 180
	return -math.Sin(y) * math.Cos(p), -math.Sin(p), math.Cos(y) * math.Cos(p)
}

// RayTrace walks the voxel grid from a point along a direction and returns the
// first targetable block within maxDist.
//
// This is how the game itself decides what you are pointing at, and modelling
// it matters more than it looks: a straight-line distance check says a block
// four metres away is reachable, but if anything is in between then the
// crosshair is on *that* block, and what a player would actually mine is not
// the one the caller named.
//
// Uses grid traversal rather than fixed-size sampling steps: stepping by a
// fraction of a block can tunnel through a corner and miss a face, whereas
// advancing to the next boundary visits every voxel the ray truly crosses.
//
// The whole walk runs under a single terrain lock, so it cannot observe a
// half-applied block update partway along the ray.
func (c *Client) RayTrace(ox, oy, oz, dx, dy, dz, maxDist float64) (RayHit, bool) {
	// Current voxel.
	x, y, z := blockPos(ox, oy, oz)

	stepX, stepY, stepZ := axisStep(dx), axisStep(dy), axisStep(dz)

	// Distance along the ray to the next boundary on each axis, and the
	// distance between successive boundaries.
	tMaxX := safeDiv(boundary(ox, x, stepX), dx)
	tMaxY := safeDiv(boundary(oy, y, stepY), dy)
	tMaxZ := safeDiv(boundary(oz, z, stepZ), dz)
	tDeltaX, tDeltaY, tDeltaZ := safeDiv(1, dx), safeDiv(1, dy), safeDiv(1, dz)

	var hit RayHit
	found := false
	c.world.scan(func(at func(x, y, z int32) int32) {
		dist := 0.0
		face := protocol.FaceTop
		for dist <= maxDist {
			if state := at(x, y, z); c.v.IsTargetable(state) {
				hit = RayHit{X: x, Y: y, Z: z, Face: face, Distance: dist, State: state}
				found = true
				return
			}
			// Advance along whichever axis reaches its next boundary first, and
			// remember which face we entered the new voxel through.
			switch {
			case tMaxX <= tMaxY && tMaxX <= tMaxZ:
				dist, x, tMaxX = tMaxX, x+stepX, tMaxX+tDeltaX
				face = faceForStep(stepX, protocol.FaceEast, protocol.FaceWest)
			case tMaxY <= tMaxZ:
				dist, y, tMaxY = tMaxY, y+stepY, tMaxY+tDeltaY
				face = faceForStep(stepY, protocol.FaceTop, protocol.FaceBottom)
			default:
				dist, z, tMaxZ = tMaxZ, z+stepZ, tMaxZ+tDeltaZ
				face = faceForStep(stepZ, protocol.FaceSouth, protocol.FaceNorth)
			}
		}
	})
	return hit, found
}

// axisStep is the direction of travel along one axis. A zero component steps
// negative, which is harmless: safeDiv gives that axis an infinite boundary
// distance, so it is never the axis chosen to advance.
func axisStep(d float64) int32 {
	if d > 0 {
		return 1
	}
	return -1
}

// boundary is the distance from origin to the next voxel edge along an axis.
func boundary(origin float64, voxel, step int32) float64 {
	if step > 0 {
		return float64(voxel+1) - origin
	}
	return origin - float64(voxel)
}

// safeDiv divides by an axis component's magnitude, treating a zero component
// as infinitely far — a ray parallel to an axis never crosses its boundaries.
func safeDiv(n, d float64) float64 {
	if d == 0 {
		return math.Inf(1)
	}
	return n / math.Abs(d)
}

// faceForStep picks the face a ray entered through: moving in +X enters the
// west face, so the sides are swapped relative to the direction of travel.
func faceForStep(step, positiveSide, negativeSide int32) int32 {
	if step > 0 {
		return negativeSide
	}
	return positiveSide
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
	tx, ty, tz := blockCentre(x, y, z)
	dx, dy, dz := tx-eyeX, ty-eyeY, tz-eyeZ
	dist := length(dx, dy, dz)
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
