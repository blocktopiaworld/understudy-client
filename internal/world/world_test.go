package world

import (
	"sync"
	"testing"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

const (
	stateAir   int32 = 0
	stateStone int32 = 1
)

// emptyColumn returns a 24-section overworld column of air.
func emptyColumn(chunkX, chunkZ int32) *protocol.ChunkColumn {
	col := &protocol.ChunkColumn{X: chunkX, Z: chunkZ, MinY: protocol.OverworldMinY}
	for range 24 {
		col.Sections = append(col.Sections, &protocol.ChunkSection{})
	}
	return col
}

func TestStoreDropReset(t *testing.T) {
	w := New()
	if w.Loaded() != 0 {
		t.Fatalf("a fresh World has %d chunks, want 0", w.Loaded())
	}
	w.Store(emptyColumn(0, 0))
	w.Store(emptyColumn(1, 0))
	if got := w.Loaded(); got != 2 {
		t.Errorf("Loaded() = %d, want 2", got)
	}
	if !w.HasChunk(5, 5) {
		t.Error("HasChunk(5,5) = false; world coordinates 5,5 are inside chunk 0,0")
	}
	if !w.HasChunk(16, 0) {
		t.Error("HasChunk(16,0) = false; world coordinate 16 is inside chunk 1")
	}
	if w.HasChunk(-1, 0) {
		t.Error("HasChunk(-1,0) = true; chunk -1 was never stored")
	}

	w.Drop(1, 0)
	if got := w.Loaded(); got != 1 {
		t.Errorf("Loaded() after Drop = %d, want 1", got)
	}
	w.Reset()
	if got := w.Loaded(); got != 0 {
		t.Errorf("Loaded() after Reset = %d, want 0", got)
	}
}

// Arithmetic shift, not division: dividing a negative coordinate by 16 rounds
// towards zero and lands on the wrong chunk for every negative coordinate.
func TestNegativeCoordinatesFindTheRightChunk(t *testing.T) {
	w := New()
	w.Store(emptyColumn(-1, -1)) // covers world -16..-1 on both axes

	for _, p := range [][2]int32{{-1, -1}, {-16, -16}, {-8, -3}} {
		if !w.HasChunk(p[0], p[1]) {
			t.Errorf("HasChunk(%d,%d) = false, want true", p[0], p[1])
		}
	}
	if w.HasChunk(0, 0) {
		t.Error("HasChunk(0,0) = true; only chunk -1,-1 was stored")
	}

	w.SetBlockState(-5, 64, -5, stateStone)
	if got := w.BlockState(-5, 64, -5); got != stateStone {
		t.Errorf("BlockState(-5,64,-5) = %d, want %d", got, stateStone)
	}
}

// An unloaded chunk is indistinguishable from empty space, which is why
// callers must check HasChunk before trusting an "air" answer.
func TestUnloadedChunkReadsAsAir(t *testing.T) {
	w := New()
	if got := w.BlockState(100, 64, 100); got != stateAir {
		t.Errorf("BlockState in an unloaded chunk = %d, want air (%d)", got, stateAir)
	}
	w.SetBlockState(100, 64, 100, stateStone) // writing into nothing is a no-op
	if got := w.BlockState(100, 64, 100); got != stateAir {
		t.Errorf("BlockState after writing into an unloaded chunk = %d, want air", got)
	}
}

func TestScanSeesTheSameWorldThroughout(t *testing.T) {
	w := New()
	w.Store(emptyColumn(0, 0))
	w.SetBlockState(1, 5, 1, stateStone)

	var seen int32
	w.Scan(func(at func(x, y, z int32) int32) {
		seen = at(1, 5, 1)
	})
	if seen != stateStone {
		t.Errorf("Scan saw %d at 1,5,1; want %d", seen, stateStone)
	}
}

// CONFIRMED RACE. Terrain updates arrive on a read loop while callers trace
// rays from their own goroutines. SetBlockState used to mutate the column
// under a *read* lock, and BlockState read it holding no lock at all — and
// expanding a uniform section replaces its backing arrays outright.
func TestIsSafeForConcurrentUse(t *testing.T) {
	w := New()
	w.Store(emptyColumn(0, 0))

	var wg sync.WaitGroup
	const goroutines, iterations = 4, 500
	for g := range goroutines {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for i := range iterations {
				w.SetBlockState(int32(i%16), 5, int32(g), int32(i%50)+1)
			}
		}()
		go func() {
			defer wg.Done()
			for i := range iterations {
				_ = w.BlockState(int32(i%16), 5, int32(g))
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				w.Scan(func(at func(x, y, z int32) int32) {
					for y := range int32(20) {
						_ = at(0, y, 0)
					}
				})
			}
		}()
	}
	wg.Wait()
}
