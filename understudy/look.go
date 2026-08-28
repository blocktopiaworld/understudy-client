package understudy

import (
	"fmt"
	"slices"
	"strings"

	"github.com/block-topia/understudy-client/internal/geom"
	"github.com/block-topia/understudy-client/protocol"
)

// Direction is a named heading, as a rotation the bot can be pointed along.
//
// A direction need not name both axes: "up" and "down" tilt without throwing
// away the current heading, which is what makes them usable mid-task. Apply is
// where that "leave this axis alone" rule lives, so no caller re-implements it.
//
// Values, not a pair of *float32. The pointer form meant every entry in the
// table below allocated, callers could reach through and mutate a shared
// float, and "unset" and "zero" looked identical at a glance — for a type
// whose whole job is to distinguish them.
type Direction struct {
	yaw, pitch         float32
	setsYaw, setsPitch bool
}

// Yaw returns the heading this direction names, and whether it names one.
func (d Direction) Yaw() (float32, bool) { return d.yaw, d.setsYaw }

// Pitch returns the tilt this direction names, and whether it names one.
func (d Direction) Pitch() (float32, bool) { return d.pitch, d.setsPitch }

// Apply returns the rotation this direction produces from a current one,
// leaving untouched whichever axis it does not name.
func (d Direction) Apply(yaw, pitch float32) (float32, float32) {
	if d.setsYaw {
		yaw = d.yaw
	}
	if d.setsPitch {
		pitch = d.pitch
	}
	return yaw, pitch
}

// facing is a direction that turns the bot and levels it off.
func facing(yaw float32) Direction {
	return Direction{yaw: yaw, setsYaw: true, setsPitch: true}
}

// tilting is a direction that changes only the pitch.
func tilting(pitch float32) Direction {
	return Direction{pitch: pitch, setsPitch: true}
}

// directions maps Minecraft's named directions to a rotation, matching the
// vocabulary of Carpet's `/player <name> look <direction>`.
//
// Unexported with a lookup function rather than an exported map: a package
// variable holding a map is mutable by every caller, so one caller could
// redefine "north" for the whole process.
//
// See yawPitchTowards for the yaw convention, which is not the intuitive one.
var directions = map[string]Direction{
	"south":     facing(0),
	"west":      facing(90),
	"north":     facing(180),
	"east":      facing(-90),
	"southwest": facing(45),
	"northwest": facing(135),
	"northeast": facing(-135),
	"southeast": facing(-45),
	"up":        tilting(-90),
	"down":      tilting(90),
}

// LookupDirection resolves a direction name, case- and space-insensitively.
func LookupDirection(name string) (Direction, bool) {
	d, ok := directions[strings.ToLower(strings.TrimSpace(name))]
	return d, ok
}

// DirectionNames lists the accepted direction names, sorted, for error
// messages and help text.
func DirectionNames() []string {
	names := make([]string, 0, len(directions))
	for name := range directions {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Look points the bot at an absolute yaw/pitch, in degrees.
func (c *Client) Look(yaw, pitch float32) error {
	if err := c.requireAlive("look"); err != nil {
		return err
	}
	c.mu.Lock()
	c.pos.Yaw, c.pos.Pitch = yaw, pitch
	onGround := c.onGround
	c.mu.Unlock()

	w := protocol.NewWriter(c.v.Packets.SBPlayLook).
		F32(yaw).F32(pitch).U8(movementFlags(onGround))
	return c.conn.WritePacket(w.Bytes())
}

// LookDirection points the bot along a named direction.
func (c *Client) LookDirection(name string) error {
	dir, ok := LookupDirection(name)
	if !ok {
		return fmt.Errorf("understudy: unknown direction %q (want one of %s)",
			name, strings.Join(DirectionNames(), ", "))
	}
	pos := c.Position()
	return c.Look(dir.Apply(pos.Yaw, pos.Pitch))
}

// LookYawPitch updates either axis independently. A nil component is left
// unchanged, so a caller can pan without re-deriving the current tilt.
func (c *Client) LookYawPitch(yaw, pitch *float32) error {
	pos := c.Position()
	newYaw, newPitch := pos.Yaw, pos.Pitch
	if yaw != nil {
		newYaw = *yaw
	}
	if pitch != nil {
		newPitch = *pitch
	}
	return c.Look(newYaw, newPitch)
}

// LookAt points the bot at an exact world coordinate.
func (c *Client) LookAt(x, y, z float64) error {
	eyeX, eyeY, eyeZ := c.eyes()
	yaw, pitch := geom.YawPitchTowards(eyeX, eyeY, eyeZ, x, y, z)
	return c.Look(yaw, pitch)
}

// LookAtBlock points the bot at the centre of a block.
//
// Block coordinates name a corner, so aiming at the raw integer targets the
// seam between four blocks and the ray can land on a neighbour. The classic
// symptom is a mining loop that stalls one block short of its target.
func (c *Client) LookAtBlock(x, y, z int32) error {
	cx, cy, cz := geom.BlockCentre(x, y, z)
	return c.LookAt(cx, cy, cz)
}

// LookAtEntity faces a tracked entity, aiming at roughly body height rather
// than at its feet.
func (c *Client) LookAtEntity(e Entity) error {
	return c.LookAt(e.X, e.Y+MobAimHeight, e.Z)
}

// LookAtNearest faces the closest tracked entity of a type.
func (c *Client) LookAtNearest(typeName string) (Entity, error) {
	target, err := c.NearestEntity(typeName)
	if err != nil {
		return Entity{}, err
	}
	return target, c.LookAtEntity(target)
}

// LookAtPlayer faces a named player, aiming at their head rather than feet.
func (c *Client) LookAtPlayer(name string) (Entity, error) {
	target, err := c.PlayerEntity(name)
	if err != nil {
		return Entity{}, err
	}
	// Players are 1.8 tall with eyes at EyeHeight; aiming there reads as eye
	// contact rather than staring at their shoes.
	return target, c.LookAt(target.X, target.Y+EyeHeight, target.Z)
}
