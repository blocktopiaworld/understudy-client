package understudy

import (
	"context"
	"fmt"
)

// Storage containers: chests of every wood and copper, barrels, shulker boxes,
// ender chests, hoppers, dispensers, droppers, and the entity-backed ones —
// chest and hopper minecarts.
//
// None of them are special-cased. A container's capacity comes from the window
// the server opened, so a double chest is 54 slots where a single is 27, a
// hopper is 5, and a chest minecart is a chest that happens to be an entity.
// The only thing that would need code per variant is a layout that is not
// "[the container][the player's 36]", and no vanilla storage does that.

// Deposit moves a named item from the player's inventory into the container.
//
// count is how many to move; 0 or less moves everything. The number actually
// moved is returned, which can be short when the container fills up — the
// server accepts the click and silently keeps the remainder.
func (c *Client) Deposit(ctx context.Context, name string, count int32) (moved int32, err error) {
	own, err := c.storageSlots()
	if err != nil {
		return 0, err
	}
	// The player's rows are everything from own to the end of the window.
	return c.transfer(ctx, name, count, own, c.window.Size(), 0, own)
}

// Withdraw moves a named item out of the container into the player's inventory.
//
// Same caveat in reverse: a full inventory means fewer items move than asked
// for, and the server does not say so.
func (c *Client) Withdraw(ctx context.Context, name string, count int32) (moved int32, err error) {
	own, err := c.storageSlots()
	if err != nil {
		return 0, err
	}
	return c.transfer(ctx, name, count, 0, own, own, c.window.Size())
}

// transfer moves count of an item from one slot range to another.
//
// Deposit and Withdraw are the same operation with the ranges swapped, and both
// hit the same subtlety, so they share this rather than each growing their own
// copy of it.
//
// # Why exact counts cost clicks
//
// Shift-click moves a *whole stack* and nothing finer. So a request for 5 out
// of a stack of 25 cannot be one click: the remainder has to be placed one item
// at a time with right-clicks, which is exactly what the game makes a person
// do. Whole stacks still move wholesale, so moving 64 of 64 is one click and
// only the ragged edge is slow.
func (c *Client) transfer(ctx context.Context, name string, count int32,
	srcFrom, srcTo, dstFrom, dstTo int,
) (int32, error) {
	inSource := func() int32 { return c.countInRange(name, srcFrom, srcTo) }
	before := inSource()
	remaining := before
	if count > 0 && count < before {
		remaining = count
	}

	for range maxStackMoves {
		if remaining <= 0 {
			break
		}
		src, ok := c.findInRange(name, srcFrom, srcTo)
		if !ok {
			break
		}
		if src.Count <= remaining {
			was := inSource()
			if err := c.TakeFromContainer(src.Slot); err != nil {
				return before - inSource(), err
			}
			if err := wait(ctx, clickDelay); err != nil {
				return before - inSource(), err
			}
			gone := was - inSource()
			if gone <= 0 {
				// Nothing moved: the far side is full, and clicking again
				// would spin.
				c.log.Debug("transfer stalled; the destination is probably full",
					"item", name, "still_in_source", was)
				break
			}
			remaining -= gone
			continue
		}
		// Only part of this stack is wanted: place it item by item.
		dst, ok := c.freeSlotInRange(name, dstFrom, dstTo)
		if !ok {
			c.log.Debug("transfer stalled; no room at the destination", "item", name)
			break
		}
		if err := c.placeOneByOne(ctx, src.Slot, dst, remaining); err != nil {
			return before - inSource(), err
		}
		remaining = 0
	}
	return before - inSource(), nil
}

// storageSlots returns the container's own slot count, refusing windows that
// have none — a lectern, or a workstation whose slots are not storage.
func (c *Client) storageSlots() (int, error) {
	if !c.window.IsOpen() {
		return 0, ErrNoContainer
	}
	own := c.ContainerOwnSlots()
	if own == 0 {
		return 0, fmt.Errorf("understudy: this %s has no storage slots", c.ContainerType())
	}
	return own, nil
}

// countInRange totals a named item across a half-open slot range.
func (c *Client) countInRange(name string, from, to int) int32 {
	var total int32
	for slot := from; slot < to; slot++ {
		item, ok := c.window.Slot(slot)
		if !ok || item.Empty() {
			continue
		}
		if exact, fuzzy := matchesName(item, name); exact || fuzzy {
			total += item.Count
		}
	}
	return total
}

// findInRange returns the first stack of an item within a slot range.
func (c *Client) findInRange(name string, from, to int) (ItemStack, bool) {
	for slot := from; slot < to; slot++ {
		item, ok := c.window.Slot(slot)
		if !ok || item.Empty() {
			continue
		}
		if exact, fuzzy := matchesName(item, name); exact || fuzzy {
			return item, true
		}
	}
	return ItemStack{}, false
}

// freeSlotInRange picks where a partial stack should land: alongside the same
// item if there is one, else the first empty slot — so a chest does not
// fragment into single-item slots.
func (c *Client) freeSlotInRange(name string, from, to int) (int, bool) {
	empty := -1
	for slot := from; slot < to; slot++ {
		item, ok := c.window.Slot(slot)
		if !ok {
			continue
		}
		if item.Empty() {
			if empty < 0 {
				empty = slot
			}
			continue
		}
		if exact, fuzzy := matchesName(item, name); exact || fuzzy {
			return slot, true
		}
	}
	if empty >= 0 {
		return empty, true
	}
	return 0, false
}

// placeOneByOne picks a stack up, right-clicks n single items into a
// destination slot, and puts whatever is left back where it came from.
//
// Right-click with a full cursor places exactly one item — the only way the
// protocol offers to move a precise number.
func (c *Client) placeOneByOne(ctx context.Context, src, dst int, n int32) error {
	if err := c.ClickContainerSlot(src, 0, ClickModeNormal); err != nil {
		return err
	}
	if err := wait(ctx, clickDelay); err != nil {
		return err
	}
	for range n {
		if err := c.ClickContainerSlot(dst, 1, ClickModeNormal); err != nil {
			return err
		}
		if err := wait(ctx, clickDelay); err != nil {
			return err
		}
	}
	// Put the remainder back, or the server drops it on the floor when the
	// window closes — where the bot may then walk over and collect it.
	if err := c.ClickContainerSlot(src, 0, ClickModeNormal); err != nil {
		return err
	}
	return wait(ctx, clickDelay)
}

// freeContainerSlot picks a destination within the container's own slots.
func (c *Client) freeContainerSlot(name string, own int) (int, bool) {
	return c.freeSlotInRange(name, 0, own)
}

// findInContainerSlots returns the first container slot holding an item.
func (c *Client) findInContainerSlots(name string, own int) (int, bool) {
	item, ok := c.findInRange(name, 0, own)
	return item.Slot, ok
}

// DepositAll empties the player's storage rows into the container, and reports
// how many stacks moved.
//
// Useful for setting a scenario up: fill a chest, or clear a bot before
// measuring what it picks up.
func (c *Client) DepositAll(ctx context.Context) (stacks int, err error) {
	if !c.window.IsOpen() {
		return 0, ErrNoContainer
	}
	own := c.ContainerOwnSlots()
	if own == 0 {
		return 0, fmt.Errorf("understudy: this %s has no storage slots", c.ContainerType())
	}
	for range maxStackMoves {
		moved := false
		for slot := own; slot < c.window.Size(); slot++ {
			item, ok := c.window.Slot(slot)
			if !ok || item.Empty() {
				continue
			}
			if err := c.TakeFromContainer(slot); err != nil {
				return stacks, err
			}
			if err := wait(ctx, clickDelay); err != nil {
				return stacks, err
			}
			if now, _ := c.window.Slot(slot); now.Empty() {
				stacks++
				moved = true
			} else {
				// It did not move: the container is full.
				return stacks, nil
			}
			break
		}
		if !moved {
			return stacks, nil
		}
	}
	return stacks, nil
}

// ContainerContents lists what the container itself holds, excluding the
// player's rows — which is what "what is in this chest" means.
func (c *Client) ContainerContents() []ItemStack {
	own := c.ContainerOwnSlots()
	out := make([]ItemStack, 0, own)
	for slot := range own {
		if item, ok := c.window.Slot(slot); ok && !item.Empty() {
			out = append(out, item)
		}
	}
	return out
}

// CountInContainerOnly totals a named item in the container's own slots,
// ignoring the player's inventory the window also covers.
//
// The distinction matters: CountInContainer sees both, so a bot holding twenty
// diamonds while looking into an empty chest would read as twenty.
func (c *Client) CountInContainerOnly(name string) int32 {
	var total int32
	for _, item := range c.ContainerContents() {
		if exact, fuzzy := matchesName(item, name); exact || fuzzy {
			total += item.Count
		}
	}
	return total
}

// maxStackMoves bounds the shift-click loops. A double chest is 54 slots and a
// player has 36, so nothing legitimate needs more than this — it exists so a
// container that refuses to accept items cannot spin forever.
const maxStackMoves = 128
