package entities

import (
	"sync"
	"testing"

	"github.com/blocktopia/understudy-client/protocol"
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
