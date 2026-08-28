package entities

import (
	"sync"
	"testing"

	"github.com/block-topia/understudy-client/protocol"
)

func ids(list []Entity) []int32 {
	out := make([]int32, 0, len(list))
	for _, e := range list {
		out = append(out, e.ID)
	}
	return out
}

func TestTrackerLifecycle(t *testing.T) {
	tr := New()
	tr.Spawn(&Entity{ID: 1, TypeName: "minecraft:pig", X: 1, Y: 2, Z: 3})
	tr.Spawn(&Entity{ID: 2, TypeName: "minecraft:zombie"})

	if got := len(tr.All()); got != 2 {
		t.Fatalf("All() = %d entities, want 2", got)
	}

	tr.MoveRelative(1, 0.5, 0.5, 0.5)
	tr.Teleport(2, 10, 20, 30)

	byID := map[int32]Entity{}
	for _, e := range tr.All() {
		byID[e.ID] = e
	}
	if e := byID[1]; e.X != 1.5 || e.Y != 2.5 || e.Z != 3.5 {
		t.Errorf("entity 1 after MoveRelative = (%g,%g,%g), want (1.5,2.5,3.5)", e.X, e.Y, e.Z)
	}
	if e := byID[2]; e.X != 10 || e.Y != 20 || e.Z != 30 {
		t.Errorf("entity 2 after Teleport = (%g,%g,%g), want (10,20,30)", e.X, e.Y, e.Z)
	}

	tr.Remove([]int32{1})
	if got := ids(tr.All()); len(got) != 1 || got[0] != 2 {
		t.Errorf("All() after Remove = %v, want just entity 2", got)
	}
	tr.Reset()
	if got := len(tr.All()); got != 0 {
		t.Errorf("All() after Reset = %d, want 0", got)
	}
}

// Without a spawn packet there is no type, and a typeless entity cannot be
// selected for anything useful.
func TestTrackerIgnoresUnknownIDs(t *testing.T) {
	tr := New()
	tr.MoveRelative(99, 1, 1, 1)
	tr.Teleport(99, 1, 1, 1)
	if got := len(tr.All()); got != 0 {
		t.Errorf("All() = %d after updating an unspawned entity, want 0", got)
	}
}

// The tracker keeps mutating the originals as movement packets arrive, so a
// shared pointer would let a caller's snapshot change under it.
func TestTrackerSnapshotIsACopy(t *testing.T) {
	tr := New()
	tr.Spawn(&Entity{ID: 1, X: 0})

	snapshot := tr.All()
	tr.Teleport(1, 100, 0, 0)

	if snapshot[0].X != 0 {
		t.Errorf("snapshot changed to X=%g after a later teleport; All() must copy", snapshot[0].X)
	}
	// Matching must copy too.
	filtered := tr.Matching("")
	tr.Teleport(1, 200, 0, 0)
	if filtered[0].X == 200 {
		t.Error("Matching() returned a shared pointer, want a copy")
	}
}

func TestTrackerMatching(t *testing.T) {
	tr := New()
	tr.Spawn(&Entity{ID: 1, TypeName: "minecraft:pig"})
	tr.Spawn(&Entity{ID: 2, TypeName: "minecraft:zombie"})
	tr.Spawn(&Entity{ID: 3, TypeName: "minecraft:pig"})

	for _, tc := range []struct {
		name  string
		query string
		want  int
	}{
		{"bare name", "pig", 2},
		{"namespaced name", "minecraft:pig", 2},
		{"empty matches everything", "", 3},
		{"no matches", "creeper", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(tr.Matching(tc.query)); got != tc.want {
				t.Errorf("Matching(%q) = %d entities, want %d", tc.query, got, tc.want)
			}
		})
	}
}

func TestTrackerSpawnReplacesByID(t *testing.T) {
	tr := New()
	tr.Spawn(&Entity{ID: 1, TypeName: "minecraft:pig"})
	tr.Spawn(&Entity{ID: 1, TypeName: "minecraft:zombie"})
	all := tr.All()
	if len(all) != 1 || all[0].TypeName != "minecraft:zombie" {
		t.Errorf("All() = %+v, want one entity, the later zombie", all)
	}
}

func TestEntityCarriesItsUUID(t *testing.T) {
	tr := New()
	want := protocol.OfflineUUID("Someone")
	tr.Spawn(&Entity{ID: 1, UUID: want})
	if got := tr.All()[0].UUID; got != want {
		t.Errorf("UUID = %s, want %s", got, want)
	}
}

func TestTrackerIsSafeForConcurrentUse(t *testing.T) {
	tr := New()
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(4)
		go func() {
			defer wg.Done()
			for i := range 200 {
				tr.Spawn(&Entity{ID: int32(g*1000 + i), TypeName: "minecraft:pig"})
			}
		}()
		go func() {
			defer wg.Done()
			for i := range 200 {
				tr.MoveRelative(int32(g*1000+i), 1, 1, 1)
			}
		}()
		go func() {
			defer wg.Done()
			for range 200 {
				_ = tr.Matching("pig")
			}
		}()
		go func() {
			defer wg.Done()
			for i := range 200 {
				tr.Remove([]int32{int32(g*1000 + i)})
			}
		}()
	}
	wg.Wait()
}

// A teleport leaves the tracker holding the place the bot just left.
//
// The server does send remove_entities for those, but a tick or more later, and
// until it does they are listed as current. Measured against a real server: a
// bot teleported 6750 blocks still listed all 117 entities from its previous
// location, every one further away than any view distance permits, for about
// half a second. That is long enough for a caller that teleports into an arena
// and asks for the nearest mob to get one from the last arena — which is
// exactly the symptom that went unexplained for so long.
func TestDropBeyondForgetsWhatATeleportLeftBehind(t *testing.T) {
	tr := New()
	tr.Spawn(&Entity{ID: 1, TypeName: "minecraft:zombie", X: 0, Y: 64, Z: 0})
	tr.Spawn(&Entity{ID: 2, TypeName: "minecraft:zombie", X: 30, Y: 64, Z: 0})
	// Where the bot was before the teleport.
	tr.Spawn(&Entity{ID: 3, TypeName: "minecraft:zombie", X: 6750, Y: 64, Z: 6750})

	dropped := tr.DropBeyond(0, 64, 0, MaxViewBlocks)
	if dropped != 1 {
		t.Errorf("dropped %d, want only the one left behind", dropped)
	}
	if got := len(tr.All()); got != 2 {
		t.Errorf("%d entities left, want the two still in range", got)
	}
	for _, e := range tr.All() {
		if e.ID == 3 {
			t.Error("kept an entity 9500 blocks away, which no view distance reaches")
		}
	}
}

// The other half: an entity that is merely far must survive, or a bot loses
// track of things it can still legitimately see.
func TestDropBeyondKeepsWhatIsStillInView(t *testing.T) {
	tr := New()
	for i, dist := range []float64{0, 100, 300, 500} {
		tr.Spawn(&Entity{ID: int32(i + 1), TypeName: "minecraft:cow", X: dist, Y: 64, Z: 0})
	}
	if dropped := tr.DropBeyond(0, 64, 0, MaxViewBlocks); dropped != 0 {
		t.Errorf("dropped %d entities inside the maximum view distance", dropped)
	}
}
