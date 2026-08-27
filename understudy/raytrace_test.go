package understudy

import "testing"

// RayTrace is the world-aware wrapper: geom.Raycast walks the grid, and this
// decides what stops it. The traversal itself is tested in internal/geom.
func TestRayTraceStopsOnTargetableBlocks(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(3, 0, 0, stateStone)
	c.world.SetBlockState(5, 0, 0, stateStone)

	hit, ok := c.RayTrace(0.5, 0.5, 0.5, 1, 0, 0, 10)
	if !ok {
		t.Fatal("RayTrace found nothing, want the block at x=3")
	}
	if hit.X != 3 || hit.Y != 0 || hit.Z != 0 {
		t.Errorf("hit = (%d,%d,%d), want (3,0,0) — the nearer block", hit.X, hit.Y, hit.Z)
	}
	if hit.State != stateStone {
		t.Errorf("hit.State = %d, want %d", hit.State, stateStone)
	}
	if !closeEnough(hit.Distance, 2.5, 1e-9) {
		t.Errorf("Distance = %g, want 2.5", hit.Distance)
	}
}

// The crosshair stops on cobweb and crops, which IsSolid reports as empty. A
// collision-based ray passes straight through them and reports a clear line of
// sight to something standing right in front of it.
func TestRayTraceStopsOnTargetableNonSolidBlocks(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(2, 0, 0, stateWeb)
	c.world.SetBlockState(4, 0, 0, stateStone)

	hit, ok := c.RayTrace(0.5, 0.5, 0.5, 1, 0, 0, 10)
	if !ok {
		t.Fatal("RayTrace found nothing")
	}
	if hit.X != 2 {
		t.Errorf("hit x = %d, want 2 — the crosshair stops on a cobweb", hit.X)
	}
}

// A block can be targeted through water, so a fluid must not stop the ray.
func TestRayTracePassesThroughFluids(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(2, 0, 0, stateWater)
	c.world.SetBlockState(4, 0, 0, stateStone)

	hit, ok := c.RayTrace(0.5, 0.5, 0.5, 1, 0, 0, 10)
	if !ok {
		t.Fatal("RayTrace found nothing")
	}
	if hit.X != 4 {
		t.Errorf("hit x = %d, want 4 — water must not stop the crosshair", hit.X)
	}
}

func TestLineOfSightStates(t *testing.T) {
	setup := func(t *testing.T) *Client {
		c := newTestClient(t)
		loadChunk(t, c, 0, 0)
		setPosition(c, 0.5, 0, 0.5)
		return c
	}

	t.Run("clear", func(t *testing.T) {
		c := setup(t)
		c.world.SetBlockState(3, 1, 0, stateStone)
		hit, s := c.LineOfSightTo(3, 1, 0)
		if s != sightClear {
			t.Errorf("sight = %v, want sightClear (hit %v)", s, hit)
		}
		if !c.HasLineOfSight(3, 1, 0) {
			t.Error("HasLineOfSight = false, want true")
		}
	})

	t.Run("blocked by something in between", func(t *testing.T) {
		c := setup(t)
		c.world.SetBlockState(2, 1, 0, stateStone)
		c.world.SetBlockState(3, 1, 0, stateStone)
		hit, s := c.LineOfSightTo(3, 1, 0)
		if s != sightBlocked {
			t.Fatalf("sight = %v, want sightBlocked", s)
		}
		if hit.X != 2 {
			t.Errorf("obstruction reported at x=%d, want 2", hit.X)
		}
		// The error has to name what is in the way, or there is no next step.
		wantErrContaining(t, s.err("break", 3, 1, 0, hit), "blocked sight", "in the way")
	})

	t.Run("nothing at the target", func(t *testing.T) {
		c := setup(t)
		_, s := c.LineOfSightTo(3, 1, 0)
		// Not sightEmpty: the ray never runs. Asking to dig a block the client
		// has not been told about used to trace straight through the empty
		// target and blame whatever lay beyond — on a real server, the floor
		// two blocks past it, reported as "in the way" from further away than
		// the target itself.
		if s != sightNoTarget {
			t.Errorf("sight = %v, want sightNoTarget", s)
		}
		wantErrContaining(t, s.err("break", 3, 1, 0, RayHit{}), "absent target",
			"nothing there")
	})

	t.Run("target exists but is out of reach", func(t *testing.T) {
		c := setup(t)
		// Beyond the crosshair range but still inside the loaded chunk, or the
		// unloaded-terrain rule would answer first.
		far := int32(15)
		c.world.SetBlockState(far, 1, 0, stateStone)
		_, s := c.LineOfSightTo(far, 1, 0)
		if s != sightEmpty {
			t.Errorf("sight = %v, want sightEmpty for a real block beyond reach", s)
		}
	})

	// Unloaded terrain reads as air everywhere, which would wrongly look like a
	// clear path — the caller has nothing better to go on and the server will
	// arbitrate.
	t.Run("unloaded terrain defers to the server", func(t *testing.T) {
		c := newTestClient(t)
		setPosition(c, 0.5, 0, 0.5)
		if _, s := c.LineOfSightTo(3, 1, 0); s != sightClear {
			t.Errorf("sight = %v with unloaded terrain, want sightClear", s)
		}
	})
}

// The previous two-value form could not tell "nothing hit" from "wrong block
// hit", and papered over it by treating a hit at 0,0,0 as "no hit" — a real
// coordinate. A bot working near the world origin was told the wrong thing.
func TestLineOfSightAtTheWorldOrigin(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	setPosition(c, 0.5, 2, 3.5)
	c.world.SetBlockState(0, 0, 0, stateStone)

	hit, s := c.LineOfSightTo(0, 0, 0)
	if s != sightClear {
		t.Errorf("sight to the block at 0,0,0 = %v, want sightClear (hit %+v)", s, hit)
	}
	if err := s.err("break", 0, 0, 0, hit); err != nil {
		t.Errorf("err = %v for a clear line of sight to 0,0,0, want nil", err)
	}
}

// Aiming from the feet puts the ray a block and a half low, which is the
// difference between hitting the block you meant and the one under it.
func TestLookingAtUsesEyeHeight(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	setPosition(c, 0.5, 0, 0.5)
	setLook(c, 0, 0) // due south (+Z), level
	c.world.SetBlockState(0, 1, 3, stateStone)

	hit, ok := c.LookingAt()
	if !ok {
		t.Fatal("LookingAt found nothing")
	}
	if hit.Y != 1 {
		t.Errorf("hit y = %d, want 1 — the ray must start at eye height", hit.Y)
	}
}

func TestLookingAtRespectsReach(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	setPosition(c, 0.5, 0, 0.5)
	setLook(c, 0, 0)
	c.world.SetBlockState(0, 1, 8, stateStone) // beyond BlockReach
	if _, ok := c.LookingAt(); ok {
		t.Error("LookingAt found a block beyond the reach limit, want no hit")
	}
}
