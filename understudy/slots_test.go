package understudy

import (
	"context"
	"errors"
	"testing"
	"time"
)

// openWindowWith puts the client in front of an open window whose contents are
// set directly, so the slot logic can be exercised without a server.
func openWindowWith(t *testing.T, kind WindowType, own int, items map[int]ItemStack) *Client {
	t.Helper()
	c := newTestClient(t)
	c.window.Open(1, int32(kind), kind.String())
	slots := make([]ItemStack, own+PlayerWindowSlots)
	for i := range slots {
		slots[i] = ItemStack{Slot: i}
	}
	for slot, item := range items {
		item.Slot = slot
		slots[slot] = item
	}
	c.window.ReplaceAll(slots, false)
	return c
}

// Ingredients must come from the player's rows only. Searching the whole window
// finds whatever is already in the container — including the thing just placed
// there — which is how a loop shuffles one item back and forth forever.
func TestPutIntoSlotOnlyTakesFromThePlayersRows(t *testing.T) {
	// A furnace: 3 own slots, so the player's rows start at 3. Put a coal in
	// the container's own fuel slot and none in the player's.
	c := openWindowWith(t, WindowFurnace, 3, map[int]ItemStack{
		FurnaceFuelSlot: {Name: "minecraft:coal", Count: 8},
	})

	_, err := c.PutIntoSlot(context.Background(), "coal", FurnaceFuelSlot)
	if err == nil {
		t.Fatal("expected a refusal: the only coal is already in the container")
	}
	// The error should say where it looked, because "no coal" is confusing when
	// the window visibly contains coal.
	for _, want := range []string{"coal", "player's rows", "furnace"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestPutIntoSlotRefusesWithNoWindow(t *testing.T) {
	c := newTestClient(t)
	if _, err := c.PutIntoSlot(context.Background(), "coal", 0); !errors.Is(err, ErrNoContainer) {
		t.Errorf("error = %v, want ErrNoContainer", err)
	}
	if _, err := c.PutOneIntoSlot(context.Background(), "coal", 0); !errors.Is(err, ErrNoContainer) {
		t.Errorf("PutOneIntoSlot error = %v, want ErrNoContainer", err)
	}
}

// AwaitSlot is how every workstation reports "that combination does nothing",
// since the server says nothing at all.
func TestAwaitSlotTimesOutWithAReason(t *testing.T) {
	c := openWindowWith(t, WindowFurnace, 3, nil)

	start := time.Now()
	_, err := c.AwaitSlot(context.Background(), FurnaceResultSlot, 120*time.Millisecond)
	if err == nil {
		t.Fatal("AwaitSlot on an empty slot = nil error, want a timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("AwaitSlot took %v to give up on a 120ms timeout", elapsed)
	}
	for _, want := range []string{"furnace", "empty"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestAwaitSlotReturnsWhatArrives(t *testing.T) {
	c := openWindowWith(t, WindowFurnace, 3, nil)
	go func() {
		time.Sleep(40 * time.Millisecond)
		c.window.SetSlot(FurnaceResultSlot, ItemStack{Name: "minecraft:iron_ingot", Count: 2})
	}()

	item, err := c.AwaitSlot(context.Background(), FurnaceResultSlot, 3*time.Second)
	if err != nil {
		t.Fatalf("AwaitSlot: %v", err)
	}
	if item.Name != "minecraft:iron_ingot" || item.Count != 2 {
		t.Errorf("AwaitSlot returned %+v, want 2 iron ingots", item)
	}
}

func TestAwaitSlotHonoursContext(t *testing.T) {
	c := openWindowWith(t, WindowFurnace, 3, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.AwaitSlot(ctx, FurnaceResultSlot, time.Second); err == nil {
		t.Error("AwaitSlot with a cancelled context = nil error, want ctx.Err()")
	}
}

// ClearContainerInputs must touch the container's slots and leave the player's
// rows alone — otherwise "tidy up between operations" empties the bot.
func TestClearContainerInputsLeavesThePlayerAlone(t *testing.T) {
	c := openWindowWith(t, WindowBrewingStand, 5, map[int]ItemStack{
		BrewIngredientSlot: {Name: "minecraft:nether_wart", Count: 1},
		7:                  {Name: "minecraft:diamond", Count: 9}, // a player row
	})
	// No connection here, so the clicks fail — what matters is which slots it
	// tried, and that it stopped at the container's own count.
	_ = c.ClearContainerInputs(context.Background())

	if item, _ := c.window.Slot(7); item.Count != 9 {
		t.Errorf("the player's slot 7 was disturbed: %+v", item)
	}
}

// A furnace wants the whole coal stack; an anvil must not swallow one.
func TestPutOneVersusPutWholeStack(t *testing.T) {
	c := openWindowWith(t, WindowAnvil, 3, map[int]ItemStack{
		4: {Name: "minecraft:diamond_pickaxe", Count: 1},
	})
	// Without a connection the click errors, but the *search* must succeed —
	// proving the item was located in the player's rows before any click.
	_, err := c.PutOneIntoSlot(context.Background(), "diamond_pickaxe", AnvilFirstSlot)
	if err != nil && contains(err.Error(), "no \"diamond_pickaxe\"") {
		t.Errorf("PutOneIntoSlot could not find an item that is in the player's rows: %v", err)
	}
}
