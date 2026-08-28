package understudy

import "github.com/blocktopiaworld/understudy-client/internal/geom"

// Player and world geometry, re-exported from internal/geom so callers of this
// package do not have to reach into an internal one for a constant.
const (
	// EyeHeight is how far a standing player's eyes sit above their feet.
	// Every interaction the server range-checks is measured from here.
	EyeHeight = geom.EyeHeight

	// ArrowEyeHeight is where a bow releases its arrow — slightly below the
	// eyes, which matters over distance.
	ArrowEyeHeight = geom.ArrowEyeHeight

	// MobAimHeight is a rough mid-body offset for aiming at a mob rather than
	// at the ground it stands on.
	MobAimHeight = geom.MobAimHeight

	// BlockCentreOffset moves a coordinate from a block's corner to its centre.
	BlockCentreOffset = geom.BlockCentreOffset
)

// LookDirection converts a yaw/pitch in degrees to a unit direction vector, in
// Minecraft's convention: yaw 0 faces +Z, and a negative pitch looks up.
func LookDirection(yaw, pitch float32) (dx, dy, dz float64) {
	return geom.LookDirection(yaw, pitch)
}

// eyes returns the bot's eye position, which is the origin of every aim,
// reach measurement and ray trace.
func (c *Client) eyes() (x, y, z float64) {
	pos := c.Position()
	return pos.X, pos.Y + EyeHeight, pos.Z
}
