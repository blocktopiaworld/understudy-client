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
	if !c.teleportSettleSatisfied() {
		t.Error("teleportSettleSatisfied() = false on a fresh client, want true")
	}
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

// The heartbeat is what actually clears the server's "awaiting position" state,
// so a position sent after the echo satisfies the gate outright. This is the
// case that matters: a bot standing still or mining has the position loop
// reporting all along, so it never pays anything.
func TestTeleportSettleSatisfiedByAPositionAfterTheEcho(t *testing.T) {
	c := newTestClient(t)
	c.markTeleportEcho()
	if c.teleportSettleSatisfied() {
		t.Fatal("teleportSettleSatisfied() = true straight after a teleport, want false")
	}

	c.markPositionSent()
	if !c.teleportSettleSatisfied() {
		t.Error("teleportSettleSatisfied() = false after a position went out, want true")
	}

	start := time.Now()
	if err := c.awaitTeleportSettle(context.Background()); err != nil {
		t.Fatalf("awaitTeleportSettle: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("awaitTeleportSettle blocked for %v once answered, want ~0", elapsed)
	}
}

// A position sent *before* the echo proves nothing — the server is waiting for
// one that came after it. Getting this backwards would make the gate a no-op.
func TestTeleportSettleIgnoresAPositionBeforeTheEcho(t *testing.T) {
	c := newTestClient(t)
	c.markPositionSent()
	time.Sleep(2 * time.Millisecond)
	c.markTeleportEcho()

	if c.teleportSettleSatisfied() {
		t.Error("teleportSettleSatisfied() = true with the position predating the echo, want false")
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

// Once the window has passed the gate is free again even if no position was
// ever sent, so a bot with the heartbeat disabled still makes progress.
func TestTeleportSettleExpires(t *testing.T) {
	c := newTestClient(t)
	c.lastTeleportEcho.Store(time.Now().Add(-2 * TeleportSettle).UnixNano())

	if !c.teleportSettleSatisfied() {
		t.Error("teleportSettleSatisfied() = false after the window elapsed, want true")
	}
	if got := c.teleportSettleRemaining(); got != 0 {
		t.Errorf("teleportSettleRemaining() after the window = %v, want 0", got)
	}
}

// With no connection to answer on, the gate falls back to waiting out whatever
// is left of the window rather than the whole of it.
func TestAwaitTeleportSettleFallsBackToTheRemainder(t *testing.T) {
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
	if !c.teleportSettleSatisfied() {
		t.Error("teleportSettleSatisfied() = false after waiting the window out, want true")
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
