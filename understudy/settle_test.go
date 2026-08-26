package understudy

import (
	"context"
	"testing"
	"time"
)

// Before any teleport there is nothing to wait for, so the gate must be free.
// This is what keeps the mining hot path untouched.
func TestTeleportSettleIsFreeBeforeAnyTeleport(t *testing.T) {
	c := newTestClient(t)
	if got := c.teleportSettleRemaining(); got != 0 {
		t.Errorf("teleportSettleRemaining() = %v on a fresh client, want 0", got)
	}

	start := time.Now()
	if err := c.awaitTeleportSettle(context.Background()); err != nil {
		t.Fatalf("awaitTeleportSettle: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("awaitTeleportSettle blocked for %v with no teleport, want ~0", elapsed)
	}
}

func TestTeleportSettleRemainingDecays(t *testing.T) {
	c := newTestClient(t)
	c.markTeleportEcho()

	remaining := c.teleportSettleRemaining()
	if remaining <= 0 || remaining > TeleportSettle {
		t.Fatalf("teleportSettleRemaining() straight after a teleport = %v, want (0, %v]",
			remaining, TeleportSettle)
	}

	time.Sleep(30 * time.Millisecond)
	if later := c.teleportSettleRemaining(); later >= remaining {
		t.Errorf("remaining did not decay: %v then %v", remaining, later)
	}
}

// Once the window has passed the gate is free again, so a bot that has been
// standing still or mining in a loop pays nothing.
func TestTeleportSettleExpires(t *testing.T) {
	c := newTestClient(t)
	c.lastTeleportEcho.Store(time.Now().Add(-2 * TeleportSettle).UnixNano())
	if got := c.teleportSettleRemaining(); got != 0 {
		t.Errorf("teleportSettleRemaining() after the window = %v, want 0", got)
	}
}

// Only the remainder is paid, not the whole window.
func TestAwaitTeleportSettleBlocksForTheRemainder(t *testing.T) {
	c := newTestClient(t)
	const remaining = 60 * time.Millisecond
	c.lastTeleportEcho.Store(time.Now().Add(remaining - TeleportSettle).UnixNano())

	start := time.Now()
	if err := c.awaitTeleportSettle(context.Background()); err != nil {
		t.Fatalf("awaitTeleportSettle: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < remaining/2 {
		t.Errorf("awaitTeleportSettle returned after %v, want at least ~%v", elapsed, remaining)
	}
	if elapsed > TeleportSettle {
		t.Errorf("awaitTeleportSettle waited %v, want only the ~%v remainder", elapsed, remaining)
	}
	if got := c.teleportSettleRemaining(); got != 0 {
		t.Errorf("teleportSettleRemaining() after waiting = %v, want 0", got)
	}
}

func TestAwaitTeleportSettleHonoursContext(t *testing.T) {
	c := newTestClient(t)
	c.markTeleportEcho()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.awaitTeleportSettle(ctx); err == nil {
		t.Error("awaitTeleportSettle with a cancelled context = nil error, want ctx.Err()")
	}
}
