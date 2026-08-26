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
// # Status: redundant given the idle position loop, kept as a backstop
//
// A direct A/B against a live Fabric 26.1.2 server measured 6/6 fresh-session
// placements both with this gate and with it set to zero — no difference.
//
// That is not because the failure was imaginary. Both arms of that test were
// running startPositionLoop, and an idle position ticker is the *actual* fix
// for this: the server is waiting for a position from the client, and a client
// that sends one every tick answers within a tick. The measurement above was
// taken against a client that sent none at all, so it was silent for as long
// as the server cared to wait.
//
// So the gate is a backstop for the case where the heartbeat is off
// (Options.DisableIdlePosition) or has not started yet — it begins on the first
// teleport, and a caller can act before that. It is close to free either way:
// the window is measured from the last teleport echo, so a bot that has been
// standing still or mining has already waited it out.
//
// There is also a *different* failure with the same symptom, worth ruling out
// first because it is far easier to hit: a block action issued before the
// client has processed the teleport is rejected for being out of reach, because
// the client is still measuring from where it used to be. On a fresh session
// the read loop is busy absorbing a flood of chunk batches, so that window is
// hundreds of milliseconds. That one is loud — it returns an error naming the
// distance. This one is silent.
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
