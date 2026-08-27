// Package entities tracks the entities a server has told the client about.
//
// This is the minimum needed to *address* an entity: attacking takes a numeric
// entity ID, and the only place an ID ever appears is the spawn packet.
// Without tracking, the attack verb is unusable — there is no other way to
// learn one.
//
// It deliberately stores position only. Health, equipment and metadata all
// arrive as separate packets with far more decoding surface, and none of it is
// needed to pick a target and hit it.
package entities

import (
	"sync"

	"github.com/blocktopia/understudy-client/protocol"
)

// Entity is a tracked entity in the client's view of the world.
type Entity struct {
	ID       int32         `json:"id"`
	Type     int32         `json:"type"`
	TypeName string        `json:"type_name"`
	UUID     protocol.UUID `json:"-"`
	X        float64       `json:"x"`
	Y        float64       `json:"y"`
	Z        float64       `json:"z"`
}

// Tracker maintains the set of live entities. The zero value is not usable;
// call New.
type Tracker struct {
	mu       sync.RWMutex
	entities map[int32]*Entity
}

// New returns an empty Tracker.
func New() *Tracker {
	return &Tracker{entities: make(map[int32]*Entity)}
}

// Spawn records an entity, replacing any existing one with the same ID.
func (t *Tracker) Spawn(e *Entity) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entities[e.ID] = e
}

// Remove forgets the given entity IDs.
func (t *Tracker) Remove(ids []int32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range ids {
		delete(t.entities, id)
	}
}

// MaxViewBlocks is the furthest an entity can be and still be in view.
//
// A server's view distance tops out at 32 chunks, so 512 blocks is beyond any
// setting. An entry further away than this is not "far", it is stale: the
// server has stopped addressing it and simply has not said so yet.
const MaxViewBlocks = 512

// DropBeyond forgets entities further than dist from a point, returning how
// many went.
//
// This exists because a teleport does not clear the tracker. The server sends
// remove_entities for the old surroundings as their chunks unload, which is
// correct but not immediate — and in the window before it arrives the tracker
// holds a whole previous location and reports it as current. Measured: a bot
// teleported 6750 blocks still listed all 117 entities from where it had been,
// every one of them further away than any view distance allows, until the
// server caught up about half a second later.
//
// Half a second is a long time for a caller that teleports into an arena and
// immediately asks for the nearest mob. It gets one from the last arena.
func (t *Tracker) DropBeyond(x, y, z, dist float64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	limit := dist * dist
	dropped := 0
	for id, e := range t.entities {
		dx, dy, dz := e.X-x, e.Y-y, e.Z-z
		if dx*dx+dy*dy+dz*dz > limit {
			delete(t.entities, id)
			dropped++
		}
	}
	return dropped
}

// MoveRelative applies a positional delta.
//
// Updates for an entity that was never seen to spawn are dropped: without a
// spawn packet there is no type, and a typeless entity cannot be selected for
// anything useful.
func (t *Tracker) MoveRelative(id int32, dx, dy, dz float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.entities[id]; ok {
		e.X += dx
		e.Y += dy
		e.Z += dz
	}
}

// Teleport moves an entity to an absolute position.
func (t *Tracker) Teleport(id int32, x, y, z float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.entities[id]; ok {
		e.X, e.Y, e.Z = x, y, z
	}
}

// All returns a snapshot.
//
// The entities are copied out rather than shared, because the tracker keeps
// mutating the originals as movement packets arrive — a shared pointer would
// let a caller's snapshot change underneath it.
func (t *Tracker) All() []Entity {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Entity, 0, len(t.entities))
	for _, e := range t.entities {
		out = append(out, *e)
	}
	return out
}

// Matching returns a snapshot filtered by type name, or everything when
// typeName is empty. A bare name ("zombie") and a namespaced one both work.
//
// Filtering inside the lock avoids copying out an entire view distance of
// entities to keep two of them.
func (t *Tracker) Matching(typeName string) []Entity {
	if typeName == "" {
		return t.All()
	}
	want := protocol.Namespaced(typeName)
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []Entity
	for _, e := range t.entities {
		if e.TypeName == want {
			out = append(out, *e)
		}
	}
	return out
}

// Reset forgets every entity. Entity IDs do not survive a dimension change, and
// keeping stale entries would let a caller attack an ID that now means
// something else, or nothing.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	clear(t.entities)
}
