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
// count is how many to move; 0 or less moves everything the player has. The
// number actually moved is returned, which can be short when the container
// fills up — the server accepts the click and silently drops the remainder
// back, so the count is worth checking rather than assuming.
func (c *Client) Deposit(ctx context.Context, name string, count int32) (moved int32, err error) {
	if !c.window.IsOpen() {
		return 0, ErrNoContainer
	}
	own := c.ContainerOwnSlots()
	if own == 0 {
		return 0, fmt.Errorf("understudy: this %s has no storage slots", c.ContainerType())
	}
	before := c.window.CountFrom(name, own) // what the player holds
	target := before
	if count > 0 && count < before {
		target = before - count
	} else {
		target = 0
	}

	// Shift-click whole stacks: one click per stack, and the server decides
	// where each lands.
	for range maxStackMoves {
		held := c.window.CountFrom(name, own)
		if held <= target {
			break
		}
		src, ok := c.window.FindFrom(name, own)
		if !ok {
			break
		}
		if err := c.TakeFromContainer(src.Slot); err != nil {
			return before - c.window.CountFrom(name, own), err
		}
		if err := wait(ctx, clickDelay); err != nil {
			return before - c.window.CountFrom(name, own), err
		}
		// Nothing moved: the container is full, and clicking again would spin.
		if c.window.CountFrom(name, own) == held {
			c.log.Debug("deposit stalled; the container is probably full",
				"item", name, "still_held", held)
			break
		}
	}
	return before - c.window.CountFrom(name, own), nil
}

// Withdraw moves a named item out of the container into the player's inventory.
//
// Same shape as Deposit in reverse, and the same caveat: a full inventory means
// fewer items move than asked for, silently.
func (c *Client) Withdraw(ctx context.Context, name string, count int32) (moved int32, err error) {
	if !c.window.IsOpen() {
		return 0, ErrNoContainer
	}
	own := c.ContainerOwnSlots()
	if own == 0 {
		return 0, fmt.Errorf("understudy: this %s has no storage slots", c.ContainerType())
	}
	inContainer := func() int32 {
		var total int32
		for slot := range own {
			if item, ok := c.window.Slot(slot); ok {
				if exact, fuzzy := matchesName(item, name); exact || fuzzy {
					total += item.Count
				}
			}
		}
		return total
	}
	before := inContainer()
	target := int32(0)
	if count > 0 && count < before {
		target = before - count
	}

	for range maxStackMoves {
		held := inContainer()
		if held <= target {
			break
		}
		src, ok := c.findInContainerSlots(name, own)
		if !ok {
			break
		}
		if err := c.TakeFromContainer(src); err != nil {
			return before - inContainer(), err
		}
		if err := wait(ctx, clickDelay); err != nil {
			return before - inContainer(), err
		}
		if inContainer() == held {
			c.log.Debug("withdraw stalled; the inventory is probably full",
				"item", name, "still_in_container", held)
			break
		}
	}
	return before - inContainer(), nil
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

// findInContainerSlots returns the first container slot holding an item.
func (c *Client) findInContainerSlots(name string, own int) (int, bool) {
	for slot := range own {
		item, ok := c.window.Slot(slot)
		if !ok || item.Empty() {
			continue
		}
		if exact, fuzzy := matchesName(item, name); exact || fuzzy {
			return slot, true
		}
	}
	return 0, false
}

// maxStackMoves bounds the shift-click loops. A double chest is 54 slots and a
// player has 36, so nothing legitimate needs more than this — it exists so a
// container that refuses to accept items cannot spin forever.
const maxStackMoves = 128
