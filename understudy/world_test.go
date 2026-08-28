package understudy

import (
	"context"
	"testing"
	"time"
)

func TestFindGround(t *testing.T) {
	for _, tc := range []struct {
		name      string
		place     func(c *Client)
		from      int32
		want      Support
		wantFound bool
	}{
		{
			name:      "solid floor",
			place:     func(c *Client) { c.world.SetBlockState(0, 60, 0, stateStone) },
			from:      70,
			want:      Support{GroundY: 61, Found: true, Known: true},
			wantFound: true,
		},
		{
			// Feet go *inside* the top water block, not on top of it: water only
			// cancels fall damage once the player is actually in it, and
			// stopping one block high lands on "air above water" for full damage.
			name:      "water surface",
			place:     func(c *Client) { c.world.SetBlockState(0, 60, 0, stateWater) },
			from:      70,
			want:      Support{GroundY: 60, Found: true, Known: true, InWater: true},
			wantFound: true,
		},
		{
			name:      "lava",
			place:     func(c *Client) { c.world.SetBlockState(0, 60, 0, stateLava) },
			from:      70,
			want:      Support{GroundY: 61, Found: true, Known: true, InLava: true},
			wantFound: true,
		},
		// Loaded and empty: Known stays true, because the client has the
		// column and there genuinely is nothing in it.
		{name: "nothing below", place: func(c *Client) {}, from: 70, want: Support{Known: true}},
		{
			name: "stops at the first thing hit",
			place: func(c *Client) {
				c.world.SetBlockState(0, 65, 0, stateStone)
				c.world.SetBlockState(0, 60, 0, stateStone)
			},
			from:      70,
			want:      Support{GroundY: 66, Found: true, Known: true},
			wantFound: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			loadChunk(t, c, 0, 0)
			tc.place(c)

			got := c.FindGround(0, tc.from, 0)
			if got.Found != tc.wantFound {
				t.Fatalf("Found = %v, want %v", got.Found, tc.wantFound)
			}
			if tc.wantFound && got != tc.want {
				t.Errorf("FindGround = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// An unloaded chunk reads as air everywhere, which callers must not confuse
// with "the void".
func TestFindGroundRequiresLoadedTerrain(t *testing.T) {
	c := newTestClient(t)
	if got := c.FindGround(0, 70, 0); got.Found {
		t.Errorf("FindGround in an unloaded chunk = %+v, want Found false", got)
	}
}

func TestGroundBelowStartsUnderTheFeet(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(0, 63, 0, stateStone)
	setPosition(c, 0.5, 64, 0.5)

	if got := c.GroundBelow(); !got.Found || got.GroundY != 64 {
		t.Errorf("GroundBelow() = %+v, want GroundY 64", got)
	}
}

func TestSubmerged(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	setPosition(c, 0.5, 64, 0.5)

	if c.Submerged() {
		t.Error("Submerged() = true in air, want false")
	}
	// The head block sits one above the feet; that is what drowns.
	c.world.SetBlockState(0, 65, 0, stateWater)
	if !c.Submerged() {
		t.Error("Submerged() = false with water at head height, want true")
	}
	c.world.SetBlockState(0, 65, 0, stateAir)
	c.world.SetBlockState(0, 64, 0, stateWater)
	if c.Submerged() {
		t.Error("Submerged() = true with water only at the feet, want false")
	}
}

func TestWaterSurfaceAbove(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	for y := int32(64); y <= 67; y++ {
		c.world.SetBlockState(0, y, 0, stateWater)
	}
	setPosition(c, 0.5, 64, 0.5)

	got, ok := c.WaterSurfaceAbove()
	if !ok {
		t.Fatal("WaterSurfaceAbove() reported no surface, want one")
	}
	if got != 67 {
		t.Errorf("WaterSurfaceAbove() = %g, want 67 (the Y whose block above is not water)", got)
	}
}

// Targetable is deliberately not solid: the crosshair stops on cobweb and
// crops, and passes through water.
func TestIsSolidAtAndIsTargetableAt(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(0, 64, 0, stateStone)
	c.world.SetBlockState(0, 65, 0, stateWater)
	c.world.SetBlockState(0, 66, 0, stateWeb)

	for _, tc := range []struct {
		y                 int32
		solid, targetable bool
	}{
		{64, true, true},
		{65, false, false}, // water: a block can be targeted *through* it
		{66, false, true},  // cobweb: walk-through, but the crosshair stops
		{67, false, false}, // air
	} {
		if got := c.IsSolidAt(0, tc.y, 0); got != tc.solid {
			t.Errorf("IsSolidAt(0,%d,0) = %v, want %v", tc.y, got, tc.solid)
		}
		if got := c.IsTargetableAt(0, tc.y, 0); got != tc.targetable {
			t.Errorf("IsTargetableAt(0,%d,0) = %v, want %v", tc.y, got, tc.targetable)
		}
	}
}

func TestWaitForChunk(t *testing.T) {
	t.Run("returns immediately when loaded", func(t *testing.T) {
		c := newTestClient(t)
		loadChunk(t, c, 0, 0)
		setPosition(c, 5, 64, 5)

		start := time.Now()
		if !c.waitForChunk(context.Background(), time.Second) {
			t.Fatal("waitForChunk = false with the chunk already loaded")
		}
		if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
			t.Errorf("waitForChunk took %v with the chunk already loaded, want ~0", elapsed)
		}
	})

	t.Run("times out with no terrain", func(t *testing.T) {
		c := newTestClient(t)
		setPosition(c, 5, 64, 5)
		if c.waitForChunk(context.Background(), 50*time.Millisecond) {
			t.Error("waitForChunk = true with no terrain, want false")
		}
	})

	t.Run("honours the context", func(t *testing.T) {
		c := newTestClient(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if c.waitForChunk(ctx, time.Minute) {
			t.Error("waitForChunk = true with a cancelled context, want false")
		}
	})

	// Chunks trail a teleport, which is the whole reason this exists.
	t.Run("notices a late arrival", func(t *testing.T) {
		c := newTestClient(t)
		setPosition(c, 5, 64, 5)
		go func() {
			time.Sleep(40 * time.Millisecond)
			c.world.Store(emptyColumn(0, 0))
		}()
		if !c.waitForChunk(context.Background(), 2*time.Second) {
			t.Error("waitForChunk = false, want true once the chunk arrived")
		}
	})
}
