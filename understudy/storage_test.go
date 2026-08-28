package understudy

import (
	"context"
	"errors"
	"testing"
)

// The one rule storage relies on: capacity comes from the window. These are the
// sizes a live 26.1.2 server reports, and none of them needs a case in the code.
func TestStorageCapacityComesFromTheWindow(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind WindowType
		own  int
	}{
		{"single chest", WindowGeneric9x3, 27},
		{"double chest", WindowGeneric9x3, 54},
		{"shulker box", WindowShulkerBox, 27},
		{"hopper", WindowHopper, 5},
		{"dispenser", WindowGeneric3x3, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := openWindowWith(t, tc.kind, tc.own, nil)
			if got := c.ContainerOwnSlots(); got != tc.own {
				t.Errorf("ContainerOwnSlots() = %d, want %d", got, tc.own)
			}
		})
	}
}

// CountInContainer sees the player's rows too, so a bot holding twenty diamonds
// while looking into an empty chest would read as twenty. The distinction is
// the whole reason CountInContainerOnly exists.
func TestCountInContainerOnlyExcludesThePlayer(t *testing.T) {
	c := openWindowWith(t, WindowGeneric9x3, 27, map[int]ItemStack{
		0:  {Name: "minecraft:diamond", Count: 3},  // in the chest
		30: {Name: "minecraft:diamond", Count: 20}, // in the player's rows
	})

	if got := c.CountInContainerOnly("diamond"); got != 3 {
		t.Errorf("CountInContainerOnly = %d, want 3 — the player's diamonds leaked in", got)
	}
	if got := c.CountInContainer("diamond"); got != 23 {
		t.Errorf("CountInContainer = %d, want 23 (both sides)", got)
	}
	contents := c.ContainerContents()
	if len(contents) != 1 || contents[0].Slot != 0 {
		t.Errorf("ContainerContents() = %+v, want only the chest's own slot 0", contents)
	}
}

func TestStorageRefusesWithoutAWindow(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	if _, err := c.Deposit(ctx, "diamond", 1); !errors.Is(err, ErrNoContainer) {
		t.Errorf("Deposit error = %v, want ErrNoContainer", err)
	}
	if _, err := c.Withdraw(ctx, "diamond", 1); !errors.Is(err, ErrNoContainer) {
		t.Errorf("Withdraw error = %v, want ErrNoContainer", err)
	}
	if _, err := c.DepositAll(ctx); !errors.Is(err, ErrNoContainer) {
		t.Errorf("DepositAll error = %v, want ErrNoContainer", err)
	}
}

// A workstation is not storage. Depositing into a furnace would put items
// somewhere arbitrary, so it is refused by name.
func TestStorageRefusesAWindowWithNoStorageSlots(t *testing.T) {
	c := openWindowWith(t, WindowLectern, 0, nil)
	_, err := c.Deposit(context.Background(), "book", 1)
	if err == nil {
		t.Fatal("depositing into a lectern should be refused")
	}
	if !contains(err.Error(), "lectern") || !contains(err.Error(), "no storage slots") {
		t.Errorf("error %q should name the window and the reason", err)
	}
}

// freeContainerSlot decides where a partial stack lands: alongside the same
// item if possible, so a chest does not fragment into single-item slots.
func TestFreeContainerSlotPrefersMatchingStacks(t *testing.T) {
	c := openWindowWith(t, WindowGeneric9x3, 27, map[int]ItemStack{
		0: {Name: "minecraft:cobblestone", Count: 10},
		1: {Name: "minecraft:diamond", Count: 4},
	})

	slot, ok := c.freeContainerSlot("diamond", 27)
	if !ok || slot != 1 {
		t.Errorf("freeContainerSlot(diamond) = %d, %v; want slot 1 alongside the diamonds", slot, ok)
	}
	// Nothing matching: the first empty slot.
	slot, ok = c.freeContainerSlot("emerald", 27)
	if !ok || slot != 2 {
		t.Errorf("freeContainerSlot(emerald) = %d, %v; want the first empty slot 2", slot, ok)
	}
}

func TestFreeContainerSlotReportsAFullContainer(t *testing.T) {
	full := map[int]ItemStack{}
	for i := range 5 {
		full[i] = ItemStack{Name: "minecraft:stone", Count: 64}
	}
	c := openWindowWith(t, WindowHopper, 5, full)

	if _, ok := c.freeContainerSlot("diamond", 5); ok {
		t.Error("a full hopper should report no free slot for a different item")
	}
	// The same item can still stack in.
	if _, ok := c.freeContainerSlot("stone", 5); !ok {
		t.Error("a matching item should be able to join an existing stack")
	}
}

func TestFindInContainerSlotsIgnoresThePlayersRows(t *testing.T) {
	c := openWindowWith(t, WindowGeneric9x3, 27, map[int]ItemStack{
		29: {Name: "minecraft:emerald", Count: 5}, // player row only
	})
	if _, ok := c.findInContainerSlots("emerald", 27); ok {
		t.Error("findInContainerSlots matched an item that is in the player's rows")
	}
}
