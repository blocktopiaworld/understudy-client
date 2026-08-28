// Package world is the bot's view of loaded terrain.
//
// It exists to answer one question cheaply and exactly: what block is at this
// coordinate? Finding the floor, noticing you are underwater and confirming a
// block actually broke all reduce to that.
//
// Deliberately free of any notion of what a block *means*: it stores states
// and hands them back. Whether a state is solid, water or air is a per-version
// question that belongs with the version tables, not here.
package world

import (
	"sync"

	"github.com/block-topia/understudy-client/protocol"
)

// key identifies a chunk column.
type key struct{ X, Z int32 }

// World holds the chunk columns a server has sent.
//
// The mutex guards the columns as well as the map holding them. That is not
// belt-and-braces: a block update rewrites a section in place, and expanding a
// uniform section to direct encoding replaces its backing arrays outright — so
// a reader that only synchronised on the map lookup would walk a slice being
// swapped underneath it. Terrain updates arrive on a read loop while callers
// trace rays from their own goroutines, which is exactly that race.
//
// All access therefore holds the lock for the whole operation, not just long
// enough to find the column.
//
// The zero value is not usable; call New.
type World struct {
	mu     sync.RWMutex
	chunks map[key]*protocol.ChunkColumn
}

// New returns an empty World.
func New() *World {
	return &World{chunks: make(map[key]*protocol.ChunkColumn)}
}

// Store adds or replaces a chunk column.
func (w *World) Store(c *protocol.ChunkColumn) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.chunks[key{c.X, c.Z}] = c
}

// Drop forgets a chunk column, by chunk coordinate.
func (w *World) Drop(chunkX, chunkZ int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.chunks, key{chunkX, chunkZ})
}

// Reset forgets every column. A respawn can cross dimensions, and the same
// chunk coordinate means different blocks on the other side of a portal.
func (w *World) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	clear(w.chunks)
}

// Loaded reports how many chunk columns are currently held.
func (w *World) Loaded() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.chunks)
}

// BlockState returns the state at world coordinates, or air if the chunk is
// not loaded.
//
// Arithmetic shift, not division: dividing a negative coordinate by 16 rounds
// towards zero and lands on the wrong chunk for every negative coordinate.
func (w *World) BlockState(x, y, z int32) int32 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	// ChunkColumn.BlockState is nil-safe, so a miss reads as air without a
	// branch here.
	return w.chunks[key{x >> 4, z >> 4}].BlockState(x, y, z)
}

// SetBlockState applies a single block update. It takes the write lock because
// it mutates the column — see the note on World.
func (w *World) SetBlockState(x, y, z, state int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.chunks[key{x >> 4, z >> 4}].SetBlockState(x, y, z, state)
}

// Scan runs fn with the read lock held once, handing it a block lookup.
//
// A ground scan or a ray trace reads hundreds of blocks along a line. Doing
// that a locked call at a time is hundreds of round trips through the mutex
// while a read loop is trying to store chunks, and — worse — lets terrain
// change halfway through, so the scan answers about a world that never existed.
//
// fn must not call back into the World, and must not retain the lookup.
func (w *World) Scan(fn func(at func(x, y, z int32) int32)) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	fn(func(x, y, z int32) int32 {
		return w.chunks[key{x >> 4, z >> 4}].BlockState(x, y, z)
	})
}

// HasChunk reports whether the column covering a coordinate is loaded.
//
// Callers must check this before trusting an "air" answer, since an unloaded
// chunk is indistinguishable from empty space — and chunks trail a teleport by
// a noticeable margin.
func (w *World) HasChunk(x, z int32) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.chunks[key{x >> 4, z >> 4}]
	return ok
}
