package understudy

import (
	"context"
	"time"
)

// TeleportSettle is how long after answering a teleport the server needs
// before it will act on a block interaction.
//
// A teleport is not finished when the client agrees it moved. The server keeps
// the player in an "awaiting position from client" state and, while that lasts,
// silently ignores use_item, use_item_on and player_action — the three packets
// behind eating, placing and digging. There is no rejection and no feedback:
// the action simply does not happen.
//
// Mining never showed the problem because awaitBreak keeps swinging and
// re-sends the finish packet, so it retries through the window by accident. A
// one-shot place has no such luck and just vanishes.
//
// Reported measurement, taken on fresh sessions: 2/4 placements succeeded with
// no pause, 4/4 with a 300ms pause, and 4/4 if any real block interaction had
// gone first. A bare arm swing did *not* help, which is what ruled out "the
// session is not warm yet".
//
// # Status: retained, but NOT reproduced here
//
// A direct A/B against a local Fabric 26.1.2 server could not reproduce the
// failure: 6/6 fresh-session placements succeeded both with this gate and with
// it set to zero. So on that server this wait is buying nothing measurable.
//
// It is kept because it is close to free — the window is measured from the
// last teleport echo, so a bot that has been standing still or mining has
// already waited it out, and only the first interaction after a teleport pays
// anything at all — and because the report came from a different server under a
// real workload, which that bench did not replicate.
//
// What the local run *did* find is a different failure with the same symptom,
// and it is worth ruling out first: a block action issued before the client has
// processed the teleport is rejected for being out of reach, because the
// client is still measuring from where it used to be. On a fresh session the
// read loop is busy absorbing a flood of chunk batches, so that window is
// hundreds of milliseconds. Waiting for the client's position to reflect the
// teleport is deterministic, and faster than any fixed sleep.
//
// Before tuning this constant, confirm which of the two is actually happening:
// the reach rejection is loud and returns an error, this one is silent.
const TeleportSettle = 350 * time.Millisecond

// markTeleportEcho stamps when the reply to a server teleport went out.
func (c *Client) markTeleportEcho() { c.lastTeleportEcho.Store(time.Now().UnixNano()) }

// teleportSettleRemaining reports how much of the settle window is left.
func (c *Client) teleportSettleRemaining() time.Duration {
	last := c.lastTeleportEcho.Load()
	if last == 0 {
		return 0 // no teleport this session; nothing to wait for
	}
	remaining := TeleportSettle - time.Since(time.Unix(0, last))
	if remaining < 0 {
		return 0
	}
	return remaining
}

// awaitTeleportSettle blocks until the server will accept a block interaction.
//
// It costs nothing in the common case — the window is measured from the last
// teleport, so a bot that has been standing still, or mining in a loop, has
// already waited it out and this returns immediately. Only the first
// interaction after a teleport pays anything, and it pays only the remainder.
//
// This lives in the client rather than in whatever drives it because it is a
// property of the protocol, not of any one caller. A driver that sleeps after
// every teleport pays the cost even when it is about to do nothing; a driver
// that forgets loses actions silently. Gating the packets that are actually
// affected is both cheaper and impossible to forget.
func (c *Client) awaitTeleportSettle(ctx context.Context) error {
	remaining := c.teleportSettleRemaining()
	if remaining <= 0 {
		return nil
	}
	c.log.Debug("waiting out the teleport settle window", "remaining", remaining)
	return wait(ctx, remaining)
}
