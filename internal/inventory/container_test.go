package inventory

import "testing"

func TestContainerStartsClosed(t *testing.T) {
	c := NewContainer()
	if c.IsOpen() {
		t.Error("a new container should not be open")
	}
	// The distinction that matters: window 0 is the player's own inventory, so
	// "nothing open" cannot be represented by a zero.
	if got := c.ID(); got != NoWindow {
		t.Errorf("ID() = %d on a closed container, want NoWindow (%d)", got, NoWindow)
	}
	if c.Matches(PlayerWindowID) {
		t.Error("a closed container must not claim the player's own window")
	}
}

func TestContainerOpenAndClose(t *testing.T) {
	c := NewContainer()
	c.Open(7, 3, "Crafting")

	if !c.IsOpen() || c.ID() != 7 || c.Kind() != 3 || c.Title() != "Crafting" {
		t.Fatalf("after Open: open=%v id=%d kind=%d title=%q",
			c.IsOpen(), c.ID(), c.Kind(), c.Title())
	}
	if !c.Matches(7) {
		t.Error("Matches(7) should be true for the open window")
	}
	if c.Matches(8) || c.Matches(PlayerWindowID) {
		t.Error("Matches should reject other window ids")
	}

	c.ReplaceAll([]ItemStack{{Slot: 0, Name: "minecraft:oak_planks", Count: 4}}, false)
	c.Close()

	if c.IsOpen() || c.Matches(7) {
		t.Error("a closed container should not be open or match its old id")
	}
	// Contents survive: a caller that took something and closed the window
	// still wants to know what was there.
	if got := c.Count("oak_planks"); got != 4 {
		t.Errorf("Count after close = %d, want 4", got)
	}
}

// Opening a second window must not leave the first one's contents behind, or a
// caller reads stale slots as if they belonged to the new container.
func TestOpeningDiscardsThePreviousContents(t *testing.T) {
	c := NewContainer()
	c.Open(1, 0, "first")
	c.ReplaceAll([]ItemStack{{Slot: 0, Name: "minecraft:diamond", Count: 9}}, true)

	c.Open(2, 0, "second")
	if got := c.Size(); got != 0 {
		t.Errorf("Size() = %d after reopening, want 0", got)
	}
	if c.Trimmed() {
		t.Error("Trimmed should reset with the contents")
	}
	if got := c.Count("diamond"); got != 0 {
		t.Errorf("Count() = %d, want 0 — the old window's contents leaked", got)
	}
}

// Sequence is what lets a caller tell "the window I just opened" from "one that
// was already open", without racing the packet that carries the id.
func TestSequenceAdvancesPerWindow(t *testing.T) {
	c := NewContainer()
	start := c.Sequence()
	c.Open(1, 0, "")
	if c.Sequence() != start+1 {
		t.Errorf("Sequence() = %d after one open, want %d", c.Sequence(), start+1)
	}
	c.Close()
	if c.Sequence() != start+1 {
		t.Error("closing must not advance the sequence")
	}
	c.Open(2, 0, "")
	if c.Sequence() != start+2 {
		t.Errorf("Sequence() = %d after two opens, want %d", c.Sequence(), start+2)
	}
}

// set_slot can arrive before the full window_items, addressing past the end of
// what is known. Dropping those updates loses the crafting result.
func TestSetSlotGrowsTheView(t *testing.T) {
	c := NewContainer()
	c.Open(3, 0, "")
	c.SetSlot(5, ItemStack{Name: "minecraft:stick", Count: 2})

	if got := c.Size(); got != 6 {
		t.Errorf("Size() = %d, want 6", got)
	}
	item, ok := c.Slot(5)
	if !ok || item.Count != 2 || item.Slot != 5 {
		t.Errorf("Slot(5) = %+v, %v; want the stick with its slot set", item, ok)
	}
	// The slots skipped over are empty, not missing.
	if item, ok := c.Slot(2); !ok || !item.Empty() {
		t.Errorf("Slot(2) = %+v, %v; want an empty placeholder", item, ok)
	}
	c.SetSlot(-1, ItemStack{Name: "minecraft:dirt", Count: 1})
	if got := c.Size(); got != 6 {
		t.Errorf("a negative slot changed Size() to %d", got)
	}
}

func TestContainerSlotOutOfRange(t *testing.T) {
	c := NewContainer()
	c.Open(1, 0, "")
	c.ReplaceAll([]ItemStack{{Slot: 0, Name: "minecraft:dirt", Count: 1}}, false)
	for _, slot := range []int{-1, 1, 100} {
		if _, ok := c.Slot(slot); ok {
			t.Errorf("Slot(%d) reported ok for a %d-slot window", slot, c.Size())
		}
	}
}

// The state counter is per window and every click has to echo it. Tracking the
// container's separately from the player's is what stops a click carrying a
// stale value, which the server answers with a silent resync rather than the
// action the caller wanted.
func TestStateIDIsTracked(t *testing.T) {
	c := NewContainer()
	c.Open(4, 0, "")
	c.SetStateID(42)
	if got := c.StateID(); got != 42 {
		t.Errorf("StateID() = %d, want 42", got)
	}
}

func TestContainerFindAndCount(t *testing.T) {
	c := NewContainer()
	c.Open(1, 0, "")
	c.ReplaceAll([]ItemStack{
		{Slot: 0, Name: "minecraft:oak_planks", Count: 3},
		{Slot: 1, Name: "minecraft:stick", Count: 2},
		{Slot: 2, Name: "minecraft:oak_planks", Count: 5},
	}, false)

	if got := c.Count("oak_planks"); got != 8 {
		t.Errorf("Count(oak_planks) = %d, want 8", got)
	}
	// Fuzzy matching works the same way as it does for the player inventory,
	// so callers do not have to learn two sets of rules.
	if got := c.Count("planks"); got != 8 {
		t.Errorf("Count(planks) = %d, want 8 via the suffix match", got)
	}
	item, ok := c.Find("stick")
	if !ok || item.Slot != 1 {
		t.Errorf("Find(stick) = %+v, %v; want slot 1", item, ok)
	}
	if _, ok := c.Find("diamond"); ok {
		t.Error("Find(diamond) should not match anything here")
	}
}

func TestContainerSlotsIsACopy(t *testing.T) {
	c := NewContainer()
	c.Open(1, 0, "")
	c.ReplaceAll([]ItemStack{{Slot: 0, Name: "minecraft:dirt", Count: 1}}, false)

	got := c.Slots()
	got[0].Count = 99
	if again := c.Slots(); again[0].Count != 1 {
		t.Error("Slots() handed out the internal slice — a caller mutated the container")
	}
}
