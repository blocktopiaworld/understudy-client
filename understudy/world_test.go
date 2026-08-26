package understudy

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWorldStoreDropReset(t *testing.T) {
	w := newWorld()
	if w.loaded() != 0 {
		t.Fatalf("a fresh world has %d chunks, want 0", w.loaded())
	}
	w.store(emptyColumn(0, 0))
	w.store(emptyColumn(1, 0))
	if got := w.loaded(); got != 2 {
		t.Errorf("loaded() = %d, want 2", got)
	}
	if !w.hasChunk(5, 5) {
		t.Error("hasChunk(5,5) = false; world coordinates 5,5 are inside chunk 0,0")
	}
	if !w.hasChunk(16, 0) {
		t.Error("hasChunk(16,0) = false; world coordinate 16 is inside chunk 1")
	}
	if w.hasChunk(-1, 0) {
		t.Error("hasChunk(-1,0) = true; chunk -1 was never stored")
	}

	w.drop(1, 0)
	if got := w.loaded(); got != 1 {
		t.Errorf("loaded() after drop = %d, want 1", got)
	}
	w.reset()
	if got := w.loaded(); got != 0 {
		t.Errorf("loaded() after reset = %d, want 0", got)
	}
}

// Arithmetic shift, not division: dividing a negative coordinate by 16 rounds
// towards zero and lands on the wrong chunk for every negative coordinate.
func TestWorldNegativeCoordinatesFindTheRightChunk(t *testing.T) {
	w := newWorld()
	w.store(emptyColumn(-1, -1)) // covers world -16..-1 on both axes

	for _, p := range [][2]int32{{-1, -1}, {-16, -16}, {-8, -3}} {
		if !w.hasChunk(p[0], p[1]) {
			t.Errorf("hasChunk(%d,%d) = false, want true", p[0], p[1])
		}
	}
	if w.hasChunk(0, 0) {
		t.Error("hasChunk(0,0) = true; only chunk -1,-1 was stored")
	}

	w.setBlockState(-5, 64, -5, stateStone)
	if got := w.blockState(-5, 64, -5); got != stateStone {
		t.Errorf("blockState(-5,64,-5) = %d, want %d", got, stateStone)
	}
}

func TestWorldUnloadedChunkReadsAsAir(t *testing.T) {
	w := newWorld()
	if got := w.blockState(100, 64, 100); got != stateAir {
		t.Errorf("blockState in an unloaded chunk = %d, want air (%d)", got, stateAir)
	}
	w.setBlockState(100, 64, 100, stateStone) // writing into nothing is a no-op
}

// CONFIRMED RACE. Terrain updates arrive on the read loop while a control API
// traces rays from its own goroutines. setBlockState used to mutate the column
// under a *read* lock, and blockState read it holding no lock at all — and
// expandToDirect replaces the section's backing arrays outright.
func TestWorldIsSafeForConcurrentUse(t *testing.T) {
	w := newWorld()
	w.store(emptyColumn(0, 0))

	var wg sync.WaitGroup
	const goroutines, iterations = 4, 500
	for g := range goroutines {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for i := range iterations {
				w.setBlockState(int32(i%16), 5, int32(g), int32(i%50)+1)
			}
		}()
		go func() {
			defer wg.Done()
			for i := range iterations {
				_ = w.blockState(int32(i%16), 5, int32(g))
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				w.scan(func(at func(x, y, z int32) int32) {
					for y := range int32(20) {
						_ = at(0, y, 0)
					}
				})
			}
		}()
	}
	wg.Wait()
}

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
			place:     func(c *Client) { c.world.setBlockState(0, 60, 0, stateStone) },
			from:      70,
			want:      Support{GroundY: 61, Found: true},
			wantFound: true,
		},
		{
			// Feet go *inside* the top water block, not on top of it: water only
			// cancels fall damage once the player is actually in it, and
			// stopping one block high lands on "air above water" for full damage.
			name:      "water surface",
			place:     func(c *Client) { c.world.setBlockState(0, 60, 0, stateWater) },
			from:      70,
			want:      Support{GroundY: 60, Found: true, InWater: true},
			wantFound: true,
		},
		{
			name:      "lava",
			place:     func(c *Client) { c.world.setBlockState(0, 60, 0, stateLava) },
			from:      70,
			want:      Support{GroundY: 61, Found: true, InLava: true},
			wantFound: true,
		},
		{name: "nothing below", place: func(c *Client) {}, from: 70},
		{
			name: "stops at the first thing hit",
			place: func(c *Client) {
				c.world.setBlockState(0, 65, 0, stateStone)
				c.world.setBlockState(0, 60, 0, stateStone)
			},
			from:      70,
			want:      Support{GroundY: 66, Found: true},
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
	c.world.setBlockState(0, 63, 0, stateStone)
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
	c.world.setBlockState(0, 65, 0, stateWater)
	if !c.Submerged() {
		t.Error("Submerged() = false with water at head height, want true")
	}
	c.world.setBlockState(0, 65, 0, stateAir)
	c.world.setBlockState(0, 64, 0, stateWater)
	if c.Submerged() {
		t.Error("Submerged() = true with water only at the feet, want false")
	}
}

func TestWaterSurfaceAbove(t *testing.T) {
	c := newTestClient(t)
	loadChunk(t, c, 0, 0)
	for y := int32(64); y <= 67; y++ {
		c.world.setBlockState(0, y, 0, stateWater)
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
	c.world.setBlockState(0, 64, 0, stateStone)
	c.world.setBlockState(0, 65, 0, stateWater)
	c.world.setBlockState(0, 66, 0, stateWeb)

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
			c.world.store(emptyColumn(0, 0))
		}()
		if !c.waitForChunk(context.Background(), 2*time.Second) {
			t.Error("waitForChunk = false, want true once the chunk arrived")
		}
	})
}
