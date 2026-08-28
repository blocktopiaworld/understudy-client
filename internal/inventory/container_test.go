package inventory

import "testing"

// fill sets a container's contents where the declared size and the decoded
// count agree, which is the ordinary case.
func fill(c *Container, trimmed bool, items ...ItemStack) {
	c.ReplaceAll(items, len(items), trimmed)
}

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

	fill(c, false, ItemStack{Slot: 0, Name: "minecraft:oak_planks", Count: 4})
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
	fill(c, true, ItemStack{Slot: 0, Name: "minecraft:diamond", Count: 9})

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
	fill(c, false, ItemStack{Slot: 0, Name: "minecraft:dirt", Count: 1})
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
	fill(c, false,
		ItemStack{Slot: 0, Name: "minecraft:oak_planks", Count: 3},
		ItemStack{Slot: 1, Name: "minecraft:stick", Count: 2},
		ItemStack{Slot: 2, Name: "minecraft:oak_planks", Count: 5},
	)

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
	fill(c, false, ItemStack{Slot: 0, Name: "minecraft:dirt", Count: 1})

	got := c.Slots()
	got[0].Count = 99
	if again := c.Slots(); again[0].Count != 1 {
		t.Error("Slots() handed out the internal slice — a caller mutated the container")
	}
}

// A window's shape has to survive a partial decode.
//
// This is a real failure, found live: the bot held potions, potions carry data
// components, and readSlot cannot skip those — so the scan stopped at slot 32
// of a 41-slot brewing stand. Sizing the layout from the decoded items made the
// container's own slot count come out as 41-36=5 one moment and 32-36 clamped
// to 0 the next, and every slot lookup then searched from the wrong floor with
// nothing reported.
func TestSizeSurvivesATruncatedDecode(t *testing.T) {
	c := NewContainer()
	c.Open(1, 11, "Brewing Stand")

	// The server declared 41 slots; decoding stopped after 32.
	decoded := make([]ItemStack, 32)
	for i := range decoded {
		decoded[i] = ItemStack{Slot: i}
	}
	c.ReplaceAll(decoded, 41, true)

	if got := c.Size(); got != 41 {
		t.Errorf("Size() = %d, want the declared 41 — the layout must not shrink "+
			"because an item could not be decoded", got)
	}
	if !c.Trimmed() {
		t.Error("Trimmed() should report that the contents are incomplete")
	}
	// The slots that could not be decoded read as empty rather than out of range,
	// so a caller addressing them gets "nothing there" instead of a false
	// "outside the window".
	for _, slot := range []int{32, 40} {
		if _, ok := c.Slot(slot); !ok {
			t.Errorf("Slot(%d) is inside a 41-slot window and should be addressable", slot)
		}
	}
	if _, ok := c.Slot(41); ok {
		t.Error("Slot(41) is outside a 41-slot window")
	}
}

// FindFrom used to slice the slot list by array position, which is the same as
// the slot number only while the list is dense from zero. ReplaceAll pads to
// the declared size by appending to the end, so a single stack recorded for
// slot 37 sits at index 0 — and slicing from 10 stepped straight over it.
//
// The symptom was a crafting table refusing the planks it was plainly holding:
// CountFrom filtered on the slot number and said eight, FindFrom sliced by
// index and said none, reading the same list.
func TestFindFromUsesTheSlotNumberNotThePosition(t *testing.T) {
	planks := ItemStack{Slot: 37, Name: "minecraft:oak_planks", Count: 8}

	for _, tc := range []struct {
		name  string
		slots []ItemStack
	}{
		{"dense, as a full window_items delivers", denseWith(46, planks)},
		{"one stack padded to the declared size", []ItemStack{planks}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Container{}
			c.ReplaceAll(tc.slots, 46, false)

			if got := c.CountFrom("oak_planks", 10); got != 8 {
				t.Errorf("CountFrom = %d, want 8", got)
			}
			got, ok := c.FindFrom("oak_planks", 10)
			if !ok {
				t.Fatal("FindFrom did not find the planks CountFrom can see")
			}
			if got.Slot != 37 {
				t.Errorf("found slot %d, want 37", got.Slot)
			}
			// And the floor still does its job: the grid is below it.
			if _, ok := c.FindFrom("oak_planks", 38); ok {
				t.Error("FindFrom above the stack found it anyway")
			}
		})
	}
}

func denseWith(size int, items ...ItemStack) []ItemStack {
	out := make([]ItemStack, size)
	for i := range out {
		out[i] = ItemStack{Slot: i}
	}
	for _, item := range items {
		out[item.Slot] = item
	}
	return out
}
