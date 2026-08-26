package understudy

import "math"

// Player and world geometry, in blocks.
//
// These were previously written as bare literals at each use site — 1.62
// appeared in aiming, reach checking and two ray-trace origins. That is the
// kind of duplication that goes wrong silently: a ray cast from the feet still
// hits *something*, just a block and a half below what the caller meant, and
// the symptom is a mining loop that stalls one block short of its target.
const (
	// EyeHeight is how far a standing player's eyes sit above their feet.
	// Every interaction the server range-checks is measured from here.
	EyeHeight = 1.62

	// ArrowEyeHeight is where a bow releases its arrow — slightly below the
	// eyes, which matters over distance even though it looks like a rounding
	// difference up close.
	ArrowEyeHeight = 1.52

	// MobAimHeight is a rough mid-body offset for aiming at a mob rather than
	// at the ground it stands on.
	MobAimHeight = 1.0

	// BlockCentreOffset moves a coordinate from a block's corner to its centre.
	// Block coordinates name the corner, so aiming at the raw integer targets
	// the seam between four blocks and the ray can land on a neighbour.
	BlockCentreOffset = 0.5
)

// blockPos returns the block coordinate containing a world position.
//
// Floor, not truncation: Go's int32 conversion rounds towards zero, so a
// position at x = -0.5 would land in block 0 rather than block -1 and every
// query west or north of the origin would be off by one.
func blockPos(x, y, z float64) (int32, int32, int32) {
	return int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
}

// blockCentre returns the world coordinate at the middle of a block.
func blockCentre(x, y, z int32) (float64, float64, float64) {
	return float64(x) + BlockCentreOffset,
		float64(y) + BlockCentreOffset,
		float64(z) + BlockCentreOffset
}

// eyes returns the bot's eye position, which is the origin of every aim,
// reach measurement and ray trace.
func (c *Client) eyes() (x, y, z float64) {
	pos := c.Position()
	return pos.X, pos.Y + EyeHeight, pos.Z
}

// length returns the magnitude of a vector.
func length(dx, dy, dz float64) float64 { return math.Sqrt(dx*dx + dy*dy + dz*dz) }

// yawPitchTowards returns the rotation that points from an eye position at a
// world coordinate.
//
// The yaw convention is the confusing part and is worth stating outright: yaw 0
// faces SOUTH (+Z) and increases clockwise, so west is +90, north is 180 and
// east is -90. Guessing that 0 means north — the intuitive reading — puts
// every aim exactly backwards.
func yawPitchTowards(eyeX, eyeY, eyeZ, x, y, z float64) (yaw, pitch float32) {
	dx, dy, dz := x-eyeX, y-eyeY, z-eyeZ
	horizontal := math.Hypot(dx, dz)
	yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	pitch = float32(-math.Atan2(dy, horizontal) * 180 / math.Pi)
	return yaw, pitch
}
