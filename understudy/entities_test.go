package understudy

import (
	"testing"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

func TestEntitiesAreSortedNearestFirst(t *testing.T) {
	c := newTestClient(t)
	setPosition(c, 0, 0, 0)
	c.entities.Spawn(&Entity{ID: 1, TypeName: "minecraft:pig", X: 10})
	c.entities.Spawn(&Entity{ID: 2, TypeName: "minecraft:pig", X: 2})
	c.entities.Spawn(&Entity{ID: 3, TypeName: "minecraft:pig", X: 5})

	got := c.Entities()
	for i, id := range []int32{2, 3, 1} {
		if got[i].ID != id {
			t.Errorf("Entities()[%d].ID = %d, want %d (order: %v)", i, got[i].ID, id, ids(got))
		}
	}
}

func TestNearestEntity(t *testing.T) {
	c := newTestClient(t)
	setPosition(c, 0, 0, 0)
	c.entities.Spawn(&Entity{ID: 1, TypeName: "minecraft:pig", X: 10})
	c.entities.Spawn(&Entity{ID: 2, TypeName: "minecraft:zombie", X: 3})
	c.entities.Spawn(&Entity{ID: 3, TypeName: "minecraft:pig", X: 4})

	got, err := c.NearestEntity("pig")
	if err != nil {
		t.Fatalf("NearestEntity(pig): %v", err)
	}
	if got.ID != 3 {
		t.Errorf("NearestEntity(pig).ID = %d, want 3", got.ID)
	}
	// An empty type matches anything, so the zombie wins on distance.
	if got, err = c.NearestEntity(""); err != nil || got.ID != 2 {
		t.Errorf("NearestEntity(\"\") = %d, %v; want 2, nil", got.ID, err)
	}
	if _, err := c.NearestEntity("creeper"); err == nil {
		t.Error("NearestEntity of an untracked type = nil error, want an error")
	}
}

func TestDistanceTo(t *testing.T) {
	c := newTestClient(t)
	setPosition(c, 0, 0, 0)
	if got := c.DistanceTo(Entity{X: 3, Y: 4}); !closeEnough(got, 5, 1e-9) {
		t.Errorf("DistanceTo((3,4,0)) = %g, want 5", got)
	}
}

// The server enforces attack reach and simply *ignores* an attack on anything
// further away, so a silent miss has to become a real error client-side.
// "Nearest" is not the same as "reachable", and mobs wander.
func TestAttackNearestRefusesOutOfReach(t *testing.T) {
	c := newTestClient(t)
	setPosition(c, 0, 0, 0)
	c.entities.Spawn(&Entity{ID: 1, TypeName: "minecraft:zombie", X: AttackReach + 1})

	_, err := c.AttackNearest("zombie")
	wantErrContaining(t, err, "AttackNearest on an out-of-reach target",
		"beyond", "minecraft:zombie")
}

// On an offline-mode server a player's UUID derives from their name, so the
// name resolves without decoding player_info at all.
func TestPlayerEntityResolvesByDerivedUUID(t *testing.T) {
	c := newTestClient(t)
	const name = "SomePlayer"
	c.entities.Spawn(&Entity{ID: 7, TypeName: "minecraft:player", UUID: protocol.OfflineUUID(name)})

	got, err := c.PlayerEntity(name)
	if err != nil {
		t.Fatalf("PlayerEntity(%q): %v", name, err)
	}
	if got.ID != 7 {
		t.Errorf("PlayerEntity(%q).ID = %d, want 7", name, got.ID)
	}
	if _, err := c.PlayerEntity("SomeoneElse"); err == nil {
		t.Error("PlayerEntity of an absent player = nil error, want an error")
	}
}

// --- packet decoding ---------------------------------------------------------

func TestHandleEntityPacketSpawn(t *testing.T) {
	c := newTestClient(t)
	uuid := protocol.OfflineUUID("Someone")
	p := packet(c.v.Packets.CBPlaySpawnEntity, func(w *protocol.Writer) {
		w.VarInt(42).UUID(uuid).VarInt(1).F64(1.5).F64(64).F64(-2.5)
	})

	handled, err := c.handleEntityPacket(p)
	if !handled || err != nil {
		t.Fatalf("handleEntityPacket = %v, %v; want true, nil", handled, err)
	}
	list := c.entities.All()
	if len(list) != 1 {
		t.Fatalf("tracked %d entities, want 1", len(list))
	}
	e := list[0]
	if e.ID != 42 || e.TypeName != "minecraft:zombie" || e.UUID != uuid {
		t.Errorf("spawned entity = %+v, want id 42, minecraft:zombie, the given uuid", e)
	}
	if e.X != 1.5 || e.Y != 64 || e.Z != -2.5 {
		t.Errorf("spawned position = (%g,%g,%g), want (1.5,64,-2.5)", e.X, e.Y, e.Z)
	}
}

// Entity movement is sent in 1/4096ths of a block; treating the raw value as
// blocks puts entities thousands of blocks away.
func TestHandleEntityPacketRelativeMoveScaling(t *testing.T) {
	c := newTestClient(t)
	c.entities.Spawn(&Entity{ID: 1})

	p := packet(c.v.Packets.CBPlayRelEntityMove, func(w *protocol.Writer) {
		w.VarInt(1).I16(4096).I16(-4096).I16(2048)
	})
	if _, err := c.handleEntityPacket(p); err != nil {
		t.Fatalf("handleEntityPacket: %v", err)
	}
	e := c.entities.All()[0]
	if !closeEnough(e.X, 1, 1e-9) || !closeEnough(e.Y, -1, 1e-9) || !closeEnough(e.Z, 0.5, 1e-9) {
		t.Errorf("after a relative move = (%g,%g,%g), want (1,-1,0.5)", e.X, e.Y, e.Z)
	}
}

func TestHandleEntityPacketDestroy(t *testing.T) {
	c := newTestClient(t)
	for _, id := range []int32{1, 2, 3} {
		c.entities.Spawn(&Entity{ID: id})
	}
	p := packet(c.v.Packets.CBPlayEntityDestroy, func(w *protocol.Writer) {
		w.VarInt(2).VarInt(1).VarInt(3)
	})
	if _, err := c.handleEntityPacket(p); err != nil {
		t.Fatalf("handleEntityPacket: %v", err)
	}
	list := c.entities.All()
	if len(list) != 1 || list[0].ID != 2 {
		t.Errorf("after destroy, tracked %v, want just entity 2", ids(list))
	}
}

// A corrupt count must not make the client preallocate an arbitrary slice.
func TestHandleEntityPacketRejectsImplausibleDestroyCount(t *testing.T) {
	c := newTestClient(t)
	p := packet(c.v.Packets.CBPlayEntityDestroy, func(w *protocol.Writer) { w.VarInt(1 << 24) })
	if _, err := c.handleEntityPacket(p); err == nil {
		t.Error("an implausible destroy count = nil error, want an error")
	}
}

func TestHandleEntityPacketIgnoresOtherPackets(t *testing.T) {
	c := newTestClient(t)
	p := packet(c.v.Packets.CBPlayUpdateHealth, func(w *protocol.Writer) {})
	handled, err := c.handleEntityPacket(p)
	if handled || err != nil {
		t.Errorf("handleEntityPacket of a health packet = %v, %v; want false, nil", handled, err)
	}
}
