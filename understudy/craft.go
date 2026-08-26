package understudy

import (
	"context"
	"fmt"
	"slices"
)

// The player's own 2x2 crafting grid lives in window 0, so using it needs no
// container to be opened and no window bookkeeping — which makes it the
// cheapest way to craft anything with a 2x2 recipe.
const (
	SlotCraftGrid2x2Start = 1 // slots 1..4
	SlotCraftGrid2x2End   = 4
)

// clickDelay paces container clicks.
//
// The server applies each click in order and echoes the resulting slots back.
// Firing them back to back means later clicks are computed against an
// inventory the client has not seen yet, and a mis-sequenced craft silently
// produces nothing.
const clickDelay = 3 * TickRate

// CraftIn2x2 crafts using the player's own 2x2 grid.
//
// layout maps a grid slot (1..4) to the item that belongs there, so a caller
// expresses a recipe positionally rather than this package carrying a recipe
// table. Shapeless recipes simply use whichever slots are convenient.
//
// The crafted stack is pulled out with a quick-move, which is what makes the
// crafted/* statistic tick — taking the result is the act that counts, not
// assembling the ingredients.
func (c *Client) CraftIn2x2(ctx context.Context, layout map[int]string) (ItemStack, error) {
	if err := c.requireAlive("craft"); err != nil {
		return ItemStack{}, err
	}
	for slot := range layout {
		if slot < SlotCraftGrid2x2Start || slot > SlotCraftGrid2x2End {
			return ItemStack{}, fmt.Errorf("understudy: grid slot %d is outside the 2x2 grid (1..4)", slot)
		}
	}
	return c.craftUsingGrid(ctx, layout)
}

// craftUsingGrid performs the click sequence common to any crafting grid.
func (c *Client) craftUsingGrid(ctx context.Context, layout map[int]string) (ItemStack, error) {
	pause := func() error { return wait(ctx, clickDelay) }

	// Start from an empty grid. Leftovers change what the recipe resolves to,
	// and a stale output slot would otherwise be mistaken for this craft's
	// result — which is how an impossible layout reports success.
	if err := c.ClearCraftingGrid(ctx); err != nil {
		return ItemStack{}, err
	}
	if err := pause(); err != nil {
		return ItemStack{}, err
	}

	// Deterministic order, so a failure is reproducible.
	slots := make([]int, 0, len(layout))
	for slot := range layout {
		slots = append(slots, slot)
	}
	slices.Sort(slots)

	for _, gridSlot := range slots {
		itemName := layout[gridSlot]
		src, ok := c.inv.FindInStorage(itemName)
		if !ok {
			return ItemStack{}, fmt.Errorf("understudy: no %q in inventory to craft with", itemName)
		}
		// Pick the stack up, drop one into the grid, then put the rest back.
		// Right-click (button 1) places a single item, which is what keeps a
		// recipe needing one log from consuming the whole stack.
		if err := c.clickSlot(src.Slot, 0, ClickModeNormal); err != nil {
			return ItemStack{}, err
		}
		if err := pause(); err != nil {
			return ItemStack{}, err
		}
		if err := c.clickSlot(gridSlot, 1, ClickModeNormal); err != nil {
			return ItemStack{}, err
		}
		if err := pause(); err != nil {
			return ItemStack{}, err
		}
		if err := c.clickSlot(src.Slot, 0, ClickModeNormal); err != nil {
			return ItemStack{}, err
		}
		if err := pause(); err != nil {
			return ItemStack{}, err
		}
	}

	// Let the server settle the grid and compute the result before reaching
	// for the output slot, which is empty until it has.
	if err := pause(); err != nil {
		return ItemStack{}, err
	}
	// Confirm every requested slot actually received its ingredient before
	// trusting the output; a partially filled grid can still resolve to some
	// other valid recipe.
	for _, gridSlot := range slots {
		if _, filled := c.SlotAt(gridSlot); !filled {
			return ItemStack{}, fmt.Errorf(
				"understudy: grid slot %d never received %q — the craft would have made "+
					"whatever the partial layout resolves to", gridSlot, layout[gridSlot])
		}
	}

	result, hasResult := c.SlotAt(SlotCraftOutput)
	if !hasResult {
		return ItemStack{}, fmt.Errorf(
			"understudy: nothing in the crafting output — the layout %v is not a valid recipe, "+
				"or the ingredients did not reach the grid", layout)
	}

	// Quick-move the output into the inventory. This is the click that credits
	// the crafted statistic.
	if err := c.clickSlot(SlotCraftOutput, 0, ClickModeQuickMove); err != nil {
		return result, err
	}
	if err := pause(); err != nil {
		return result, err
	}
	return result, nil
}

// ClearCraftingGrid returns anything left in the 2x2 grid to the inventory.
//
// Worth calling between crafts: leftovers in the grid change what the next
// recipe resolves to, and they are dropped on the floor when the inventory
// closes — where the bot may then walk over and collect them, changing an
// inventory something else is measuring.
func (c *Client) ClearCraftingGrid(ctx context.Context) error {
	for slot := SlotCraftGrid2x2Start; slot <= SlotCraftGrid2x2End; slot++ {
		if _, occupied := c.SlotAt(slot); !occupied {
			continue
		}
		if err := c.clickSlot(slot, 0, ClickModeQuickMove); err != nil {
			return err
		}
		if err := wait(ctx, clickDelay); err != nil {
			return err
		}
	}
	return nil
}
