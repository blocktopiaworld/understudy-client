// Package geom holds the spatial arithmetic the client needs: where a player's
// eyes are, which block a coordinate falls in, and which way to face.
//
// It is separate because none of it needs a connection. These are pure
// functions over numbers, so they can be tested exhaustively without a server,
// and the constants they carry stop being bare literals scattered through the
// packet-writing code — which is how a ray came to be cast from the feet
// instead of the eyes, hitting a block and a half below what the caller meant.
package geom

import "math"

// Player and world geometry, in blocks.
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

// BlockPos returns the block coordinate containing a world position.
//
// Floor, not truncation: Go's int32 conversion rounds towards zero, so a
// position at x = -0.5 would land in block 0 rather than block -1 and every
// query west or north of the origin would be off by one.
func BlockPos(x, y, z float64) (int32, int32, int32) {
	return int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
}

// BlockCentre returns the world coordinate at the middle of a block.
func BlockCentre(x, y, z int32) (float64, float64, float64) {
	return float64(x) + BlockCentreOffset,
		float64(y) + BlockCentreOffset,
		float64(z) + BlockCentreOffset
}

// Length returns the magnitude of a vector.
func Length(dx, dy, dz float64) float64 { return math.Sqrt(dx*dx + dy*dy + dz*dz) }

// Clamp confines v to [lo, hi].
func Clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(v, hi)) }

// BlockDistance returns the distance from a point to the nearest point of a
// block's box — which is the measure a server enforces for reach, rather than
// the distance to the block's centre. A bot can therefore legitimately work a
// little further away than centre-distance suggests.
func BlockDistance(fromX, fromY, fromZ float64, x, y, z int32) float64 {
	return Length(
		fromX-Clamp(fromX, float64(x), float64(x)+1),
		fromY-Clamp(fromY, float64(y), float64(y)+1),
		fromZ-Clamp(fromZ, float64(z), float64(z)+1),
	)
}

// YawPitchTowards returns the rotation that points from an eye position at a
// world coordinate.
//
// The yaw convention is the confusing part and is worth stating outright: yaw 0
// faces SOUTH (+Z) and increases clockwise, so west is +90, north is 180 and
// east is -90. Guessing that 0 means north — the intuitive reading — puts
// every aim exactly backwards.
func YawPitchTowards(eyeX, eyeY, eyeZ, x, y, z float64) (yaw, pitch float32) {
	dx, dy, dz := x-eyeX, y-eyeY, z-eyeZ
	horizontal := math.Hypot(dx, dz)
	yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	pitch = float32(-math.Atan2(dy, horizontal) * 180 / math.Pi)
	return yaw, pitch
}

// LookDirection converts a yaw/pitch in degrees to a unit direction vector, in
// Minecraft's convention: yaw 0 faces +Z, and a negative pitch looks up. It is
// the inverse of YawPitchTowards.
func LookDirection(yaw, pitch float32) (dx, dy, dz float64) {
	y := float64(yaw) * math.Pi / 180
	p := float64(pitch) * math.Pi / 180
	return -math.Sin(y) * math.Cos(p), -math.Sin(p), math.Cos(y) * math.Cos(p)
}
