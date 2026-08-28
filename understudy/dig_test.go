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
