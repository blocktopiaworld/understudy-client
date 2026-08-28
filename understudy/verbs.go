package understudy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/block-topia/understudy-client/protocol"
)

// SetInput sets the player's movement-input bits (sneak, sprint, jump…).
//
// These live in player_input as of 26.1. Older clients put sneaking in
// entity_action, which in 26.1 no longer has the action at all — so code
// written against the old shape sneaks silently never.
func (c *Client) SetInput(flags uint8) error {
	if err := c.requireAlive("set input"); err != nil {
		return err
	}
	if c.v.Packets.SBPlayPlayerInput == protocol.Absent {
		return fmt.Errorf("understudy: %s has no player_input packet", c.v.Name)
	}
	c.mu.Lock()
	c.input = flags
	c.mu.Unlock()
	return c.conn.WritePacket(protocol.NewWriter(c.v.Packets.SBPlayPlayerInput).U8(flags).Bytes())
}

// setInputBit turns one input flag on or off, leaving the others alone.
func (c *Client) setInputBit(bit uint8, on bool) error {
	flags := c.Input()
	if on {
		flags |= bit
	} else {
		flags &^= bit
	}
	return c.SetInput(flags)
}

// SetSneaking starts or stops sneaking, leaving the other input bits alone.
func (c *Client) SetSneaking(on bool) error { return c.setInputBit(protocol.InputSneak, on) }

// SetSprinting starts or stops sprinting, leaving the other input bits alone.
func (c *Client) SetSprinting(on bool) error { return c.setInputBit(protocol.InputSprint, on) }

// Sneak holds sneak for a duration, then releases it. Useful for the
// sneak_time statistic, which accrues only while actually sneaking.
//
// The release is unconditional: a cancelled context must still stand the bot
// back up, or it stays crouched for the rest of the session.
func (c *Client) Sneak(ctx context.Context, d time.Duration) error {
	if err := c.SetSneaking(true); err != nil {
		return err
	}
	waitErr := wait(ctx, d)
	if err := c.SetSneaking(false); err != nil {
		return err
	}
	return waitErr
}

// EquipArmour moves a wearable item onto the armour slot it belongs in.
//
// It leans on the server's own shift-click behaviour: quick-moving a helmet
// from the inventory puts it on the head, because that is where the server
// decides such an item goes. Doing it by hand would mean replicating those
// placement rules client-side.
func (c *Client) EquipArmour(name string) (ItemStack, error) {
	if err := c.requireAlive("equip"); err != nil {
		return ItemStack{}, err
	}
	item, ok := c.FindItem(name)
	if !ok {
		return ItemStack{}, fmt.Errorf("understudy: no %q in inventory", name)
	}
	if item.Slot >= SlotArmorHead && item.Slot < SlotMainStart {
		return item, nil // already worn
	}
	return item, c.clickSlot(item.Slot, 0, ClickModeQuickMove)
}

// ConsumeDuration is how long a normal eat or drink takes: 32 ticks.
const ConsumeDuration = 32 * TickRate

// consumeMargin is added to ConsumeDuration so a slightly slow server still
// sees the full use time before the release arrives.
const consumeMargin = 4 * TickRate

// consumeSettle lets the resulting slot update arrive before the outcome is
// judged. Without it a successful eat looks like a refused one.
const consumeSettle = 8 * TickRate

// Consume eats or drinks the held item.
//
// This is a *held* action, not an instant one. Sending use_item alone starts
// the animation and nothing else; the server only applies the effect once the
// item has been held for its full use time and the use is then released. A
// client that skips the wait appears to eat and never actually does.
func (c *Client) Consume(ctx context.Context) error {
	if err := c.requireAlive("consume"); err != nil {
		return err
	}
	// Snapshot the stack first. The server silently refuses to eat ordinary
	// food at full hunger — no error, no feedback — so without a before/after
	// comparison a refused eat is indistinguishable from a successful one, and
	// the caller happily moves on believing it fed the bot.
	before, hadItem := c.HeldItem()

	if err := c.UseItem(ctx); err != nil {
		return err
	}
	if err := wait(ctx, ConsumeDuration+consumeMargin); err != nil {
		return err
	}
	// Release the use, which is what commits it.
	if err := c.releaseUse(ctx); err != nil {
		return err
	}
	if !hadItem {
		return errors.New("understudy: nothing in hand to consume")
	}
	if err := wait(ctx, consumeSettle); err != nil {
		return err
	}

	// The stack shrinking is the reliable signal, and it is the right one even
	// for items that *can* be eaten at full hunger (golden apples) or that
	// change into something else (a milk bucket becoming an empty one).
	after, stillHeld := c.HeldItem()
	consumed := !stillHeld || after.Name != before.Name || after.Count < before.Count
	if !consumed {
		health, food := c.Health()
		return fmt.Errorf(
			"understudy: %s was not consumed (still %d in hand; food %d/20, health %.0f) — "+
				"ordinary food is refused at full hunger",
			before.Name, after.Count, food, health)
	}
	return nil
}

// ConsumeItem holds a named item and consumes it.
func (c *Client) ConsumeItem(ctx context.Context, name string) (ItemStack, error) {
	item, err := c.HoldItem(name)
	if err != nil {
		return ItemStack{}, err
	}
	// Give the server a moment to register the slot change before the use
	// starts, or the wrong item gets eaten.
	if err := wait(ctx, 4*TickRate); err != nil {
		return item, err
	}
	return item, c.Consume(ctx)
}
