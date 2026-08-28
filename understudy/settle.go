package understudy

import (
	"context"
	"time"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// TeleportSettle bounds how long a block interaction will wait for a teleport
// to finish settling.
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
// Reported measurement, taken on fresh sessions against a client that sent no
// idle position packets at all: 2/4 placements succeeded with no pause, 4/4
// with a 300ms pause, and 4/4 if any real block interaction had gone first. A
// bare arm swing did *not* help, which is what ruled out "the session is not
// warm yet" and pointed at the teleport.
//
// # This is a ceiling, not a cost
//
// The name of the state says what clears it: the server wants a position from
// the client. So awaitTeleportSettle sends one, rather than sleeping until the
// heartbeat happens to come round, and then waits a single tick for the server
// to act on it. The usual cost is therefore one TickRate, and this constant
// only bounds the fallback path where the position could not be sent.
//
// It used to sleep the whole window unconditionally, which was measured at 346ms
// mean across 11 waits — and mineField repositions about eleven times per field,
// through tp directly rather than through the driver's stand(), so nothing else
// was absorbing it. That came to 3.8s per field of pure sleeping, and it is the
// bulk of a 98s -> 154s regression across a full run.
//
// # A different failure with the same symptom
//
// Worth ruling out first, because it is far easier to hit: a block action
// issued before the client has *processed* the teleport is rejected for being
// out of reach, because the client is still measuring from where it used to
// be. On a fresh session the read loop is busy absorbing a flood of chunk
// batches, so that window is hundreds of milliseconds. That one is loud — it
// returns an error naming the distance. This one is silent.
const TeleportSettle = 350 * time.Millisecond

// markTeleportEcho stamps when the reply to a server teleport went out.
//
// The echo writes its position packet directly rather than through
// writePosition, so it deliberately does not count as a position sent. That is
// what makes "lastPositionSent is newer than lastTeleportEcho" mean something:
// a movement packet has gone out since the teleport, which is exactly what the
// server was waiting for.
func (c *Client) markTeleportEcho() { c.lastTeleportEcho.Store(time.Now().UnixNano()) }

// teleportSettleSatisfied reports whether the server already has what it needs.
func (c *Client) teleportSettleSatisfied() bool {
	last := c.lastTeleportEcho.Load()
	if last == 0 {
		return true // no teleport this session; nothing to wait for
	}
	if c.lastPositionSent.Load() > last {
		return true // a position has gone out since the echo
	}
	return time.Since(time.Unix(0, last)) >= TeleportSettle
}

// teleportSettleRemaining reports how much of the fallback window is left.
func (c *Client) teleportSettleRemaining() time.Duration {
	last := c.lastTeleportEcho.Load()
	if last == 0 {
		return 0
	}
	remaining := TeleportSettle - time.Since(time.Unix(0, last))
	if remaining < 0 {
		return 0
	}
	return remaining
}

// awaitTeleportSettle blocks until the server will accept a block interaction.
//
// It costs nothing in the common case: a bot that has been standing still or
// mining has had the heartbeat reporting its position all along, so the check
// passes on entry and this returns immediately. Only the first interaction
// after a teleport pays anything, and what it pays is a tick.
//
// This lives in the client rather than in whatever drives it because it is a
// property of the protocol, not of any one caller. A driver that sleeps after
// every teleport pays the cost even when it is about to do nothing; a driver
// that forgets loses actions silently. Gating the packets that are actually
// affected is both cheaper and impossible to forget.
func (c *Client) awaitTeleportSettle(ctx context.Context) error {
	if c.teleportSettleSatisfied() {
		return nil
	}
	// With no connection, or outside play, there is no position packet to send,
	// so there is nothing to do but wait the window out.
	if c.conn == nil || c.State() != protocol.StatePlay {
		return wait(ctx, c.teleportSettleRemaining())
	}
	// The server is waiting for a position from the client, so send one. This
	// is the whole point: sleeping and hoping is strictly worse than answering
	// the question that is being asked.
	pos := c.Position()
	if err := c.writePosition(pos.X, pos.Y, pos.Z, c.OnGround()); err != nil {
		// Cannot speak — the socket is going, or we are not in play. Fall back
		// to waiting the window out and let the caller's own write report why.
		c.log.Debug("could not answer the teleport settle, waiting it out", "err", err)
		return wait(ctx, c.teleportSettleRemaining())
	}
	// Give the server a tick to act on it before the interaction lands.
	return wait(ctx, TickRate)
}
