package understudy

import (
	"context"
	"fmt"
	"time"
)

// Moving items around an open window.
//
// Every workstation flow — smelting, renaming, enchanting, brewing — is the
// same three steps: put something in a slot, wait for the server to produce a
// result, take it. These are those steps, so each workstation verb is a short
// description of its layout rather than another copy of the click dance.

// slotWaitTimeout bounds how long to wait for a slot to change. Generous
// enough for a furnace to smelt, which is the slowest thing here.
const slotWaitTimeout = 15 * time.Second

// PutIntoSlot moves a named item from the player's rows into a container slot.
//
// The whole stack goes: pick it up, put it down. For a single item use
// PutOneIntoSlot — the difference matters for a furnace, where the fuel slot
// taking a whole stack of coal is fine, and for an anvil, where it is not.
//
// The item is only ever taken from the player's own rows. Searching the whole
// window would find whatever is already in the container — including the item
// this call just placed — which is how a loop ends up shuffling one item back
// and forth forever.
func (c *Client) PutIntoSlot(ctx context.Context, name string, slot int) (ItemStack, error) {
	return c.moveIntoSlot(ctx, name, slot, false)
}

// PutOneIntoSlot moves a single item from the player's rows into a slot,
// leaving the rest of the stack where it was.
func (c *Client) PutOneIntoSlot(ctx context.Context, name string, slot int) (ItemStack, error) {
	return c.moveIntoSlot(ctx, name, slot, true)
}

func (c *Client) moveIntoSlot(ctx context.Context, name string, slot int, single bool) (ItemStack, error) {
	if !c.window.IsOpen() {
		return ItemStack{}, ErrNoContainer
	}
	floor := c.PlayerSlotsStart()
	src, ok := c.window.FindFrom(name, floor)
	if !ok {
		return ItemStack{}, fmt.Errorf(
			"understudy: no %q in the player's rows of this %s window (searched from slot %d)",
			name, c.ContainerType(), floor)
	}
	// Pick up the stack, then place. Right-click (button 1) drops a single
	// item; left-click puts the lot down.
	button := int8(0)
	if single {
		button = 1
	}
	for _, step := range []struct {
		slot   int
		button int8
	}{{src.Slot, 0}, {slot, button}, {src.Slot, 0}} {
		if err := c.ClickContainerSlot(step.slot, step.button, ClickModeNormal); err != nil {
			return ItemStack{}, err
		}
		if err := wait(ctx, clickDelay); err != nil {
			return ItemStack{}, err
		}
	}
	placed, _ := c.window.Slot(slot)
	return placed, nil
}

// AwaitSlot waits for a slot to hold something, and returns it.
//
// Workstations produce their output a tick or many seconds later — a furnace
// takes ten seconds a piece — so this polls rather than sleeping a guess. A
// timeout means the operation did not happen, which for most workstations is
// silent otherwise: an invalid combination simply leaves the result slot empty.
func (c *Client) AwaitSlot(ctx context.Context, slot int, timeout time.Duration) (ItemStack, error) {
	if timeout <= 0 {
		timeout = slotWaitTimeout
	}
	deadline := time.After(timeout)
	ticker := time.NewTicker(chunkPollInterval)
	defer ticker.Stop()
	for {
		if item, ok := c.window.Slot(slot); ok && !item.Empty() {
			return item, nil
		}
		select {
		case <-ctx.Done():
			return ItemStack{}, ctx.Err()
		case <-deadline:
			return ItemStack{}, fmt.Errorf(
				"understudy: slot %d of this %s was still empty after %v — the inputs may be "+
					"wrong for it, which the server does not report",
				slot, c.ContainerType(), timeout)
		case <-ticker.C:
		}
	}
}

// TakeSlot waits for a slot to fill and then shift-clicks it into the player's
// inventory, returning what was taken.
//
// Shift-clicking rather than pick-up-and-place because that is the click that
// credits crafting and smelting statistics, and because for a result slot it
// takes every repeat the inputs allow rather than one.
func (c *Client) TakeSlot(ctx context.Context, slot int, timeout time.Duration) (ItemStack, error) {
	item, err := c.AwaitSlot(ctx, slot, timeout)
	if err != nil {
		return ItemStack{}, err
	}
	if err := c.TakeFromContainer(slot); err != nil {
		return item, err
	}
	return item, wait(ctx, clickDelay)
}

// EmptySlot moves whatever is in a slot back to the player's inventory, if
// anything is. Used between operations so a leftover input does not change what
// the next one produces.
func (c *Client) EmptySlot(ctx context.Context, slot int) error {
	item, ok := c.window.Slot(slot)
	if !ok || item.Empty() {
		return nil
	}
	if err := c.TakeFromContainer(slot); err != nil {
		return err
	}
	return wait(ctx, clickDelay)
}

// ClearContainerInputs empties every slot the container owns, returning the
// contents to the player.
//
// Worth doing between operations at a shared workstation: an ingredient left in
// a brewing stand or a loom changes what the next attempt makes, and the server
// reports that as a perfectly ordinary result rather than as a mistake.
func (c *Client) ClearContainerInputs(ctx context.Context) error {
	for slot := range c.ContainerOwnSlots() {
		if err := c.EmptySlot(ctx, slot); err != nil {
			return fmt.Errorf("understudy: clearing slot %d: %w", slot, err)
		}
	}
	return nil
}
