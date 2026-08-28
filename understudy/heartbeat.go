package understudy

import (
	"context"
	"time"

	"github.com/block-topia/understudy-client/protocol"
)

// markPositionSent stamps when a movement packet last went out.
func (c *Client) markPositionSent() { c.lastPositionSent.Store(time.Now().UnixNano()) }

// sincePositionSent reports how long ago a movement packet last went out.
func (c *Client) sincePositionSent() time.Duration {
	last := c.lastPositionSent.Load()
	if last == 0 {
		return timeForever
	}
	return time.Since(time.Unix(0, last))
}

// timeForever stands in for "never happened" without special-casing at every
// comparison. Any real elapsed time compares smaller.
const timeForever = time.Duration(1) << 62

// startPositionLoop begins reporting the bot's position every tick, once.
//
// A real client sends a movement packet roughly twenty times a second whether
// or not the player moved — vanilla's own loop emits one every tick and forces
// a full position at least every twentieth. A bot that only speaks when it has
// somewhere to be is silent for most of a session, and that silence is visible
// to the server, in ways that matter:
//
//   - the server's movement and idle bookkeeping never advances, so statistics
//     derived from a player simply *being* somewhere do not accrue;
//   - anti-cheat and AFK plugins score a player who never reports position
//     differently from one who does;
//   - it does not look like a player, which is the whole point of driving a
//     real connection instead of issuing commands.
//
// The loop deliberately yields to the action verbs: if a walk, a fall or a
// teleport echo has already sent a position this tick, it stays quiet rather
// than interleaving a stale position into the middle of a descent. That check
// is what makes it safe to run alongside everything else.
//
// It starts on the first teleport rather than on entering play, because before
// that there is no position to report and 0,0,0 is a lie the server would have
// to correct.
func (c *Client) startPositionLoop(ctx context.Context) {
	if c.opts.DisableIdlePosition {
		return
	}
	if !c.positionLoop.CompareAndSwap(false, true) {
		return
	}
	c.background(func() { c.runPositionLoop(ctx) })
}

func (c *Client) runPositionLoop(ctx context.Context) {
	ticker := time.NewTicker(TickRate)
	defer ticker.Stop()

	c.log.Debug("idle position loop started", "rate", TickRate)
	for {
		if err := sleep(ctx, ticker.C); err != nil {
			c.log.Debug("idle position loop stopped")
			return
		}
		// A dead player is not positioned by the server, and one that has left
		// play has no position packet to send.
		if c.State() != protocol.StatePlay || c.Dead() {
			continue
		}
		// Something else already spoke for us this tick.
		if c.sincePositionSent() < TickRate {
			continue
		}
		pos := c.Position()
		if err := c.writePosition(pos.X, pos.Y, pos.Z, c.OnGround()); err != nil {
			// The socket is gone or going. Run will report the real reason, so
			// this is logged at debug and the loop simply stops.
			c.log.Debug("idle position loop ended", "err", err)
			return
		}
	}
}
