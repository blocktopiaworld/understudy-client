package inventory

import (
	"testing"

	"github.com/blocktopia/understudy-client/protocol"
)

func stack(slot int, name string, count int32) ItemStack {
	return ItemStack{Slot: slot, Name: protocol.Namespaced(name), Count: count}
}

func TestSortedBySlot(t *testing.T) {
	inv := New()
	for _, s := range []int{40, 9, 36, 0, 45} {
		inv.SetSlot(s, stack(s, "dirt", 1))
	}
	got := inv.Sorted()
	want := []int{0, 9, 36, 40, 45}
	if len(got) != len(want) {
		t.Fatalf("Sorted() returned %d slots, want %d", len(got), len(want))
	}
	for i, slot := range want {
		if got[i].Slot != slot {
			t.Errorf("Sorted()[%d].Slot = %d, want %d", i, got[i].Slot, slot)
		}
	}
}

// A zero count means the slot is now empty; it must be removed rather than
// linger as a phantom stack.
func TestEmptySlotDeletes(t *testing.T) {
	inv := New()
	inv.SetSlot(9, stack(9, "dirt", 5))
	if _, ok := inv.Slot(9); !ok {
		t.Fatal("slot 9 missing after SetSlot")
	}
	inv.SetSlot(9, ItemStack{Slot: 9})
	if _, ok := inv.Slot(9); ok {
		t.Error("slot 9 still present after being set empty")
	}
}

func TestReplaceAll(t *testing.T) {
	inv := New()
	inv.SetSlot(1, stack(1, "dirt", 1))
	inv.ReplaceAll([]ItemStack{stack(9, "oak_log", 3)}, true)

	if _, ok := inv.Slot(1); ok {
		t.Error("slot 1 survived ReplaceAll, want the whole window replaced")
	}
	if it, ok := inv.Slot(9); !ok || it.Count != 3 {
		t.Errorf("slot 9 = %+v, %v; want oak_log x3", it, ok)
	}
	if !inv.Truncated() {
		t.Error("Truncated() = false after a truncated snapshot")
	}
}

// The fuzzy suffix match is a convenience, but it must not shadow an exact
// one: "oak_planks" also suffix-matches "dark_oak_planks", and picking that
// crafts the wrong recipe.
func TestFindPrefersExactMatch(t *testing.T) {
	items := []ItemStack{
		stack(9, "dark_oak_planks", 10),
		stack(20, "oak_planks", 5),
	}
	got, ok := Find(items, "oak_planks")
	if !ok {
		t.Fatal("Find(oak_planks) found nothing")
	}
	if got.Slot != 20 {
		t.Errorf("Find(oak_planks) = slot %d (%s), want slot 20 (the exact match)",
			got.Slot, got.Name)
	}
}

func TestFind(t *testing.T) {
	items := []ItemStack{
		stack(9, "diamond_pickaxe", 1),
		stack(12, "dirt", 64),
		stack(36, "dirt", 32),
	}
	for _, tc := range []struct {
		name     string
		query    string
		wantSlot int
		wantOK   bool
	}{
		{"exact bare name", "dirt", 12, true},
		{"exact namespaced name", "minecraft:dirt", 12, true},
		{"fuzzy suffix", "pickaxe", 9, true},
		{"lowest slot wins among equals", "dirt", 12, true},
		{"not present", "emerald", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Find(items, tc.query)
			if ok != tc.wantOK {
				t.Fatalf("Find(%q) found = %v, want %v", tc.query, ok, tc.wantOK)
			}
			if ok && got.Slot != tc.wantSlot {
				t.Errorf("Find(%q) = slot %d, want %d", tc.query, got.Slot, tc.wantSlot)
			}
		})
	}
}

// Mid-craft, searching every slot finds the ingredient just placed into the
// grid, so the loop picks its own work back up — which produced an oak_button
// from four planks.
func TestFindInStorageIgnoresTheCraftingGrid(t *testing.T) {
	inv := New()
	inv.SetSlot(SlotCraftGridA, stack(SlotCraftGridA, "oak_planks", 1))
	inv.SetSlot(20, stack(20, "oak_planks", 4))

	got, ok := inv.FindInStorage("oak_planks")
	if !ok {
		t.Fatal("FindInStorage found nothing")
	}
	if got.Slot != 20 {
		t.Errorf("FindInStorage = slot %d, want 20 — the grid must not be searched", got.Slot)
	}
	// FindItem, by contrast, does see the grid.
	if got, _ := inv.FindItem("oak_planks"); got.Slot != SlotCraftGridA {
		t.Errorf("FindItem = slot %d, want the grid slot %d", got.Slot, SlotCraftGridA)
	}
}

// Server implementations disagree about what "the inventory" means: 36 storage
// slots, or the whole container including armour and the offhand. Reporting
// both numbers is what lets a caller detect that divergence.
func TestCountAllVersusStorage(t *testing.T) {
	inv := New()
	inv.SetSlot(9, stack(9, "dirt", 10))  // main inventory
	inv.SetSlot(36, stack(36, "dirt", 5)) // hotbar
	inv.SetSlot(SlotOffhand, stack(SlotOffhand, "dirt", 3))
	inv.SetSlot(SlotArmorHead, stack(SlotArmorHead, "dirt", 1))

	if got := inv.CountAll("dirt"); got != 19 {
		t.Errorf("CountAll(dirt) = %d, want 19 (everything)", got)
	}
	if got := inv.CountStorage("dirt"); got != 15 {
		t.Errorf("CountStorage(dirt) = %d, want 15 (the 36 storage slots only)", got)
	}
}

// Counting is exact-match only: a fuzzy total would silently add
// dark_oak_planks to an oak_planks count.
func TestCountDoesNotMatchFuzzily(t *testing.T) {
	inv := New()
	inv.SetSlot(9, stack(9, "oak_planks", 4))
	inv.SetSlot(10, stack(10, "dark_oak_planks", 4))

	if got := inv.CountAll("oak_planks"); got != 4 {
		t.Errorf("CountAll(oak_planks) = %d, want 4 — dark_oak_planks must not count", got)
	}
}

func TestFreeStorageSlots(t *testing.T) {
	inv := New()
	if got := inv.FreeStorageSlots(); got != StorageSlots {
		t.Errorf("FreeStorageSlots() on an empty inventory = %d, want %d", got, StorageSlots)
	}
	inv.SetSlot(9, stack(9, "dirt", 1))
	inv.SetSlot(36, stack(36, "dirt", 1))
	// Armour and the offhand are outside the 36 and must not count.
	inv.SetSlot(SlotOffhand, stack(SlotOffhand, "dirt", 1))
	if got := inv.FreeStorageSlots(); got != StorageSlots-2 {
		t.Errorf("FreeStorageSlots() = %d, want %d", got, StorageSlots-2)
	}
}

func TestIsStorage(t *testing.T) {
	for _, tc := range []struct {
		slot int
		want bool
	}{
		{SlotCraftOutput, false},
		{SlotCraftGridA, false},
		{SlotArmorHead, false},
		{SlotMainStart, true},
		{SlotMainEnd, true},
		{SlotHotbarStart, true},
		{SlotHotbarEnd, true},
		{SlotOffhand, false},
	} {
		if got := (ItemStack{Slot: tc.slot}).IsStorage(); got != tc.want {
			t.Errorf("slot %d IsStorage() = %v, want %v", tc.slot, got, tc.want)
		}
	}
}

func TestPickupTally(t *testing.T) {
	inv := New()
	total, byKey := inv.Pickups()
	if total != 0 || len(byKey) != 0 {
		t.Errorf("a fresh inventory has picked up %d items, want 0", total)
	}

	inv.RecordPickup("item", 3)
	inv.RecordPickup("item", 2)
	total, byKey = inv.Pickups()
	if total != 5 || byKey["item"] != 5 {
		t.Errorf("Pickups() = %d, %v; want 5", total, byKey)
	}

	// The returned map must be a copy, or a caller could corrupt the tally.
	byKey["item"] = 999
	if total, _ = inv.Pickups(); total != 5 {
		t.Errorf("Pickups() = %d after mutating the returned map, want 5", total)
	}

	inv.ResetPickups()
	if total, _ = inv.Pickups(); total != 0 {
		t.Errorf("Pickups() = %d after reset, want 0", total)
	}
}

func TestStateIDRoundTrip(t *testing.T) {
	inv := New()
	if got := inv.StateID(); got != 0 {
		t.Errorf("StateID() on a fresh inventory = %d, want 0", got)
	}
	inv.SetStateID(42)
	if got := inv.StateID(); got != 42 {
		t.Errorf("StateID() = %d, want 42", got)
	}
}
