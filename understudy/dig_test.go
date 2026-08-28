package understudy

import (
	"context"
	"testing"
	"time"
)

// A block staged a moment ago reaches the client one packet later, and a caller
// that lays a field and mines it immediately is racing that packet.
//
// Reported against the e2e suite as BUG-6: the whole first batch of a freshly
// staged field failed while the second batch, moments later, mined fine.
// Refusing was correct — the client genuinely could not see them — but the race
// is short enough to wait out, and the caller has no way to wait for a packet
// it does not know about.
func TestDigWaitsForABlockThatHasNotArrivedYet(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	// Loaded column, nothing at the target: the BUG-6 window exactly.
	if c.v.IsTargetable(c.world.BlockState(3, 1, 0)) {
		t.Fatal("the target should start empty")
	}

	go func() {
		time.Sleep(60 * time.Millisecond)
		c.world.SetBlockState(3, 1, 0, stateStone)
	}()

	if !c.awaitTargetable(context.Background(), 3, 1, 0) {
		t.Error("gave up on a block that arrived well inside the wait")
	}
}

// And it must not wait out the clock on air that is genuinely air, or every
// mistaken coordinate costs the caller the full timeout.
func TestDigGivesUpOnGenuinelyEmptyAir(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)

	start := time.Now()
	if c.awaitTargetable(context.Background(), 3, 1, 0) {
		t.Error("reported a block where there is none")
	}
	if waited := time.Since(start); waited > 2*targetableWait {
		t.Errorf("waited %v for empty air, well past the %v bound", waited, targetableWait)
	}
}

// A vanilla client sets destroyDelay = 5 ticks when a break completes over
// time, and gates the next start on it. An instant break never sets it — which
// is why an efficient tool clears a block a tick and a bare hand cannot. A bot
// that ignored the difference would mine at a cadence no player could produce.
func TestAHeldBreakPausesBeforeTheNextButAnInstantOneDoesNot(t *testing.T) {
	t.Run("after an instant break there is no pause", func(t *testing.T) {
		c, _ := settled(t)
		began := time.Now()
		if err := c.waitForBreakCooldown(context.Background()); err != nil {
			t.Fatalf("waitForBreakCooldown: %v", err)
		}
		if waited := time.Since(began); waited > 10*time.Millisecond {
			t.Errorf("waited %v after an instant break, want none", waited)
		}
	})

	t.Run("after a held break it waits out the rest of the delay", func(t *testing.T) {
		c, _ := settled(t)
		c.mu.Lock()
		c.lastHeldBreak = time.Now()
		c.mu.Unlock()

		began := time.Now()
		if err := c.waitForBreakCooldown(context.Background()); err != nil {
			t.Fatalf("waitForBreakCooldown: %v", err)
		}
		waited := time.Since(began)
		if waited < breakCooldown-10*time.Millisecond {
			t.Errorf("waited %v, want about %v", waited, breakCooldown)
		}
	})

	t.Run("a delay already elapsed costs nothing", func(t *testing.T) {
		c, _ := settled(t)
		c.mu.Lock()
		c.lastHeldBreak = time.Now().Add(-2 * breakCooldown)
		c.mu.Unlock()

		began := time.Now()
		if err := c.waitForBreakCooldown(context.Background()); err != nil {
			t.Fatalf("waitForBreakCooldown: %v", err)
		}
		if waited := time.Since(began); waited > 10*time.Millisecond {
			t.Errorf("waited %v for a delay that had passed, want none", waited)
		}
	})
}
