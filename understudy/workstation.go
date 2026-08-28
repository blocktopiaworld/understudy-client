package understudy

import (
	"context"
	"fmt"
	"time"

	"github.com/block-topia/understudy-client/protocol"
)

// The workstations.
//
// Each is the same shape: put the inputs in the slots that window uses, wait
// for the result, take it. What differs is only the layout and how long the
// server takes — so these are thin, and the interesting part of each is the
// comment saying what silently does nothing if you get it wrong.

// smeltTimeout bounds a smelt. A furnace takes ten seconds an item, a blast
// furnace or smoker half that, and the first item also has to wait for the fuel
// to catch — so this is deliberately generous.
const smeltTimeout = 30 * time.Second

// Smelt puts an input and a fuel into an open furnace, blast furnace or smoker
// and collects what comes out.
//
// It returns the smelted stack. count asks for more than one, which works
// because the furnace keeps smelting while both slots hold something — so a
// stack of 8 iron and a stack of coal yields eight ingots from one call.
//
// A furnace given something it cannot smelt does not complain: the input sits
// there and no result ever appears. That is what the timeout reports.
func (c *Client) Smelt(ctx context.Context, input, fuel string, count int) (ItemStack, error) {
	if err := c.requireWindow(WindowFurnace, WindowBlastFurnace, WindowSmoker); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutIntoSlot(ctx, fuel, FurnaceFuelSlot); err != nil {
		return ItemStack{}, fmt.Errorf("understudy: loading fuel: %w", err)
	}
	if _, err := c.PutIntoSlot(ctx, input, FurnaceInputSlot); err != nil {
		return ItemStack{}, fmt.Errorf("understudy: loading input: %w", err)
	}
	if count < 1 {
		count = 1
	}

	// Wait for the output to reach the requested count rather than taking the
	// first ingot: smelting is one item at a time, and a caller asking for
	// eight wants eight.
	deadline := time.After(smeltTimeout)
	ticker := time.NewTicker(chunkPollInterval)
	defer ticker.Stop()
	for {
		item, ok := c.window.Slot(FurnaceResultSlot)
		if ok && int(item.Count) >= count {
			break
		}
		select {
		case <-ctx.Done():
			return ItemStack{}, ctx.Err()
		case <-deadline:
			if ok && !item.Empty() {
				// Partial: report what was actually smelted rather than failing
				// outright, since the count may simply have been optimistic.
				c.log.Debug("smelt fell short", "want", count, "got", item.Count)
				return c.TakeSlot(ctx, FurnaceResultSlot, time.Second)
			}
			return ItemStack{}, fmt.Errorf(
				"understudy: nothing smelted within %v — %q may not be smeltable, or %q "+
					"is not fuel. Neither is reported by the server", smeltTimeout, input, fuel)
		case <-ticker.C:
		}
	}
	return c.TakeSlot(ctx, FurnaceResultSlot, time.Second)
}

// RenameItem renames the item in an anvil and takes the result.
//
// The rename itself is a separate packet: putting an item in an anvil and
// clicking the output without sending a name gives back an unchanged item, so
// the name has to go first.
//
// Anvils cost levels. In survival with no experience the result slot fills but
// the take is refused, which looks exactly like a rename that did not happen —
// so a caller testing this needs the bot to have levels.
func (c *Client) RenameItem(ctx context.Context, item, newName string) (ItemStack, error) {
	if err := c.requireWindow(WindowAnvil); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutIntoSlot(ctx, item, AnvilFirstSlot); err != nil {
		return ItemStack{}, err
	}
	if err := c.SetItemName(newName); err != nil {
		return ItemStack{}, err
	}
	if err := wait(ctx, clickDelay); err != nil {
		return ItemStack{}, err
	}
	return c.TakeSlot(ctx, AnvilResultSlot, 3*time.Second)
}

// CombineInAnvil puts two items in an anvil — repairing, or applying a book —
// and takes the result.
func (c *Client) CombineInAnvil(ctx context.Context, first, second string) (ItemStack, error) {
	if err := c.requireWindow(WindowAnvil); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutIntoSlot(ctx, first, AnvilFirstSlot); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutIntoSlot(ctx, second, AnvilSecondSlot); err != nil {
		return ItemStack{}, err
	}
	return c.TakeSlot(ctx, AnvilResultSlot, 3*time.Second)
}

// ApplyBannerPattern dyes a pattern onto a banner at a loom.
//
// pattern selects from the loom's list by index, the same numbering the buttons
// use. An optional patternItem is a banner-pattern item for the designs that
// need one; pass "" for the built-in patterns.
func (c *Client) ApplyBannerPattern(ctx context.Context, banner, dye, patternItem string, pattern int32) (ItemStack, error) {
	if err := c.requireWindow(WindowLoom); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutOneIntoSlot(ctx, banner, LoomBannerSlot); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutOneIntoSlot(ctx, dye, LoomDyeSlot); err != nil {
		return ItemStack{}, err
	}
	if patternItem != "" {
		if _, err := c.PutOneIntoSlot(ctx, patternItem, LoomPatternSlot); err != nil {
			return ItemStack{}, err
		}
	}
	if err := c.ClickContainerButton(pattern); err != nil {
		return ItemStack{}, err
	}
	if err := wait(ctx, clickDelay); err != nil {
		return ItemStack{}, err
	}
	return c.TakeSlot(ctx, LoomResultSlot, 3*time.Second)
}

// Disenchant strips enchantments off an item at a grindstone, returning the
// cleaned item. Also how a grindstone repairs two of the same tool.
func (c *Client) Disenchant(ctx context.Context, item string) (ItemStack, error) {
	if err := c.requireWindow(WindowGrindstone); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutIntoSlot(ctx, item, GrindstoneFirstSlot); err != nil {
		return ItemStack{}, err
	}
	return c.TakeSlot(ctx, GrindstoneResultSlot, 3*time.Second)
}

// UpgradeInSmithingTable applies a smithing template and an addition to a base
// item — the netherite upgrade, and armour trims.
//
// All three slots are required. A smithing table with two of the three filled
// produces nothing and says nothing about which one is missing.
func (c *Client) UpgradeInSmithingTable(ctx context.Context, template, base, addition string) (ItemStack, error) {
	if err := c.requireWindow(WindowSmithing); err != nil {
		return ItemStack{}, err
	}
	for _, in := range []struct {
		name string
		slot int
	}{
		{template, SmithingTemplateSlot},
		{base, SmithingBaseSlot},
		{addition, SmithingAdditionSlot},
	} {
		if _, err := c.PutOneIntoSlot(ctx, in.name, in.slot); err != nil {
			return ItemStack{}, fmt.Errorf("understudy: smithing slot %d: %w", in.slot, err)
		}
	}
	return c.TakeSlot(ctx, SmithingResultSlot, 3*time.Second)
}

// Enchant applies an enchantment at a table.
//
// level is the button index — 0, 1 or 2 for the three offers, not the
// experience level. The bot needs the levels and the lapis, and an enchanting
// table with neither simply offers nothing.
func (c *Client) Enchant(ctx context.Context, item string, level int32) (ItemStack, error) {
	if err := c.requireWindow(WindowEnchantment); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutOneIntoSlot(ctx, item, EnchantItemSlot); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutIntoSlot(ctx, "minecraft:lapis_lazuli", EnchantLapisSlot); err != nil {
		return ItemStack{}, fmt.Errorf("understudy: enchanting needs lapis: %w", err)
	}
	if err := wait(ctx, clickDelay); err != nil {
		return ItemStack{}, err
	}
	if level < 0 || level > 2 {
		return ItemStack{}, fmt.Errorf("understudy: enchant offer %d is not 0, 1 or 2", level)
	}
	if err := c.ClickContainerButton(level); err != nil {
		return ItemStack{}, err
	}
	if err := wait(ctx, clickDelay); err != nil {
		return ItemStack{}, err
	}
	// The enchanted item goes back into the *input* slot, not a result slot.
	return c.TakeSlot(ctx, EnchantItemSlot, 3*time.Second)
}

// brewTimeout bounds a brew. A full brewing cycle is twenty seconds.
const brewTimeout = 40 * time.Second

// Brew loads a brewing stand and waits for the cycle to finish.
//
// bottles go into the three lower slots, the ingredient above them, and blaze
// powder fuels it. The result replaces the bottles in place, so this waits for
// the first bottle slot to *change* rather than for a separate output.
func (c *Client) Brew(ctx context.Context, bottle, ingredient, fuel string, count int) error {
	if err := c.requireWindow(WindowBrewingStand); err != nil {
		return err
	}
	if fuel != "" {
		if _, err := c.PutIntoSlot(ctx, fuel, BrewFuelSlot); err != nil {
			return fmt.Errorf("understudy: loading blaze powder: %w", err)
		}
	}
	if count < 1 || count > 3 {
		count = 3
	}
	for i := range count {
		if _, err := c.PutOneIntoSlot(ctx, bottle, BrewBottleSlot1+i); err != nil {
			return fmt.Errorf("understudy: loading bottle %d: %w", i+1, err)
		}
	}
	before, _ := c.window.Slot(BrewBottleSlot1)
	if _, err := c.PutOneIntoSlot(ctx, ingredient, BrewIngredientSlot); err != nil {
		return err
	}

	// Brewing transforms the bottles where they stand, so "done" is the first
	// bottle becoming something other than what went in.
	//
	// Comparing *names* cannot detect that: a water bottle, an awkward potion
	// and a potion of strength are all "minecraft:potion". Only the potion id
	// inside the contents component differs, which is why readSlot keeps it.
	deadline := time.After(brewTimeout)
	ticker := time.NewTicker(chunkPollInterval)
	defer ticker.Stop()
	for {
		if now, ok := c.window.Slot(BrewBottleSlot1); ok && brewChanged(before, now) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf(
				"understudy: nothing brewed within %v — %q may not be a valid ingredient for %q, "+
					"or the stand has no blaze powder", brewTimeout, ingredient, bottle)
		case <-ticker.C:
		}
	}
}

// brewChanged reports whether a bottle slot now holds something different from
// what went in — by potion identity, not by name.
func brewChanged(before, now ItemStack) bool {
	if now.Empty() {
		return false
	}
	if now.Name != before.Name {
		return true
	}
	return now.Potion != before.Potion
}

// requireWindow refuses when the open window is not one of the expected types.
//
// The check is worth having because the failure it prevents is silent: every
// workstation takes the same window_click packet, so clicking slot 0 of the
// wrong window is accepted and does something else entirely.
func (c *Client) requireWindow(want ...WindowType) error {
	if !c.window.IsOpen() {
		return ErrNoContainer
	}
	got := c.ContainerType()
	for _, w := range want {
		if got == w {
			return nil
		}
	}
	names := ""
	for i, w := range want {
		if i > 0 {
			names += " or "
		}
		names += w.String()
	}
	return fmt.Errorf("understudy: this is a %s window, not a %s", got, names)
}

// SetItemName sends the new name for the item in an open anvil.
//
// Separate from RenameItem because the packet is separate: the server applies
// the name to whatever is in the anvil's first slot when it arrives, so a
// caller doing something unusual can send it directly.
func (c *Client) SetItemName(name string) error {
	if !c.window.IsOpen() {
		return ErrNoContainer
	}
	if c.v.Packets.SBPlayNameItem == protocol.Absent {
		return fmt.Errorf("understudy: %s has no name_item packet", c.v.Name)
	}
	if len(name) > maxItemNameLen {
		return fmt.Errorf("understudy: item name is %d characters, the anvil accepts %d",
			len(name), maxItemNameLen)
	}
	return c.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.SBPlayNameItem).String(name).Bytes())
}

// maxItemNameLen is the anvil's own limit. A longer name is rejected outright
// by the server rather than truncated, so it is worth naming here.
const maxItemNameLen = 50

// ActivateBeacon pays a beacon and selects the effect it projects.
//
// The payment goes in the beacon's single slot; the effect is a separate packet
// rather than a container button, which is why this needs its own verb where a
// loom or a stonecutter does not.
//
// primary is a status-effect id. secondary is only accepted on a full
// five-layer pyramid and is ignored otherwise; pass 0 for none. A beacon whose
// pyramid is too small, or which cannot see the sky, takes the payment and
// projects nothing — silently, as ever.
func (c *Client) ActivateBeacon(ctx context.Context, payment string, primary, secondary int32) error {
	if err := c.requireWindow(WindowBeacon); err != nil {
		return err
	}
	if c.v.Packets.SBPlaySetBeaconEffect == protocol.Absent {
		return fmt.Errorf("understudy: %s has no set_beacon_effect packet", c.v.Name)
	}
	if payment != "" {
		if _, err := c.PutOneIntoSlot(ctx, payment, BeaconPaymentSlot); err != nil {
			return fmt.Errorf("understudy: paying the beacon: %w", err)
		}
	}
	w := protocol.NewWriter(c.v.Packets.SBPlaySetBeaconEffect)
	// Both effects are optional: a present flag, then the id.
	w.Bool(primary > 0)
	if primary > 0 {
		w.VarInt(primary)
	}
	w.Bool(secondary > 0)
	if secondary > 0 {
		w.VarInt(secondary)
	}
	if err := c.conn.WritePacket(w.Bytes()); err != nil {
		return err
	}
	return wait(ctx, clickDelay)
}

// ApplyToMap runs a map through a cartography table — paper to expand it, glass
// to lock it, an empty map to copy it — and takes the result.
//
// A plain three-slot container, so this is only naming which slot is which.
func (c *Client) ApplyToMap(ctx context.Context, mapItem, applied string) (ItemStack, error) {
	if err := c.requireWindow(WindowCartography); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutOneIntoSlot(ctx, mapItem, CartographyMapSlot); err != nil {
		return ItemStack{}, err
	}
	if _, err := c.PutOneIntoSlot(ctx, applied, CartographyPaperSlot); err != nil {
		return ItemStack{}, err
	}
	return c.TakeSlot(ctx, CartographyResultSlot, 3*time.Second)
}
