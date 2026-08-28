package understudy

import (
	"context"
	"testing"
)

// Falling must not treat an unloaded column as empty space.
//
// This is the same false-versus-unknown error the rest of the client is careful
// about, and it had a real cost: a bot teleported onto a ledge in a column it
// had not been sent read "no floor" and began a blind descent, pushing into
// ground the server could see and it could not. The server refuses every such
// move, so the player stays at one height claiming to be airborne, and after
// eighty ticks it is disconnected for floating.
//
// The distinction is the whole fix. "I have not been sent the terrain" is not
// "there is no terrain".
func TestFallWaitsRatherThanDescendIntoUnloadedTerrain(t *testing.T) {
	c := newTestClient(t)
	// No chunk loaded at all: the client knows nothing about what is below.
	c.pos = Position{X: 0.5, Y: 142, Z: 0.5}

	if got := c.GroundBelow(); got.Known {
		t.Fatal("an unloaded column must report Known == false")
	}

	fell, err := c.Fall(context.Background())
	if err != nil {
		t.Errorf("Fall on unloaded terrain should be a quiet no-op, got %v", err)
	}
	if fell != 0 {
		t.Errorf("fell %v blocks into terrain it cannot see", fell)
	}
	// The bot must still claim to be standing. Reporting airborne here is what
	// the server counts toward its floating threshold.
	if !c.OnGround() {
		t.Error("the bot reported airborne over terrain it has not been sent, " +
			"which is what gets it kicked for floating")
	}
}

// The other half: a loaded column with genuinely nothing in it is still a void
// drop, and must keep falling.
func TestFallStillDescendsThroughAKnownEmptyColumn(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	c.pos = Position{X: 0.5, Y: 142, Z: 0.5}

	got := c.GroundBelow()
	if !got.Known {
		t.Fatal("a loaded column must report Known == true")
	}
	if got.Found {
		t.Fatal("this column was left empty on purpose")
	}
}
