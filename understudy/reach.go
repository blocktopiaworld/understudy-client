package understudy

import (
	"fmt"

	"github.com/blocktopiaworld/understudy-client/internal/geom"
	"github.com/blocktopiaworld/understudy-client/protocol"
)

// BlockReach is how far a survival player can reach a block, in blocks.
//
// This is the `block_interaction_range` attribute, and it is measured from the
// eyes to the nearest point of the block's box — not to its centre — so a bot
// can legitimately work a little further than centre-distance suggests.
// Creative raises it to 5.0; this client assumes survival.
const BlockReach = 4.5

// AttackReach is how far a player can hit an entity, in blocks.
//
// The server enforces this and simply *ignores* an attack on anything further
// away — no error, no feedback. A bot swinging at a target that has wandered
// two blocks too far looks identical to one landing every hit, so this is
// checked client-side to turn a silent miss into a real error.
const AttackReach = 3.0

// BlockDistance returns the distance from the bot's eyes to the nearest point
// of a block, which is the measure the server actually enforces.
func (c *Client) BlockDistance(x, y, z int32) float64 {
	eyeX, eyeY, eyeZ := c.eyes()
	return geom.BlockDistance(eyeX, eyeY, eyeZ, x, y, z)
}

// CanReachBlock reports whether a block is within interaction range.
func (c *Client) CanReachBlock(x, y, z int32) bool {
	return c.BlockDistance(x, y, z) <= BlockReach
}

// requireAlive rejects actions while the bot is on the death screen.
//
// The server ignores actions from a dead player without complaint, so without
// this check a caller would carry on issuing commands into the void and only
// fail much later, somewhere unrelated.
func (c *Client) requireAlive(action string) error {
	if c.State() != protocol.StatePlay {
		return fmt.Errorf("understudy: cannot %s before entering play state", action)
	}
	if c.Dead() {
		// Retryable: a bot respawns, by itself unless told not to.
		return refuse(ReasonDead, true,
			fmt.Errorf("understudy: cannot %s while dead", action))
	}
	return nil
}

// requireReach rejects a block action the server would silently drop.
//
// Out-of-range digs and placements get no reply at all, so without this a bot
// swinging at something four metres too far looks exactly like one working
// normally — the same failure mode that made attacks miss silently.
func (c *Client) requireReach(action string, x, y, z int32) error {
	if d := c.BlockDistance(x, y, z); d > BlockReach {
		return refuse(ReasonOutOfReach, false,
			fmt.Errorf("understudy: cannot %s block at %d,%d,%d — %.2f blocks away, beyond the %.1f block reach",
				action, x, y, z, d, BlockReach))
	}
	return nil
}

// requireEntityReach rejects an attack or interaction the server would ignore.
func (c *Client) requireEntityReach(action string, e Entity) error {
	if d := c.DistanceTo(e); d > AttackReach {
		return refuse(ReasonOutOfReach, false,
			fmt.Errorf("understudy: nearest %s (entity %d) is %.2f blocks away, beyond the %.1f block %s reach",
				e.TypeName, e.ID, d, AttackReach, action))
	}
	return nil
}
