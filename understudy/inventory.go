package understudy

import (
	"errors"
	"fmt"

	"github.com/blocktopia/understudy-client/internal/inventory"
	"github.com/blocktopia/understudy-client/protocol"
)

// ItemStack is one inventory slot's contents.
//
// An alias rather than a wrapper: the slot store owns the type, and callers of
// this package should not have to import an internal one to name it.
type ItemStack = inventory.ItemStack

// Player inventory slot layout for window 0, re-exported so callers never
// reach into an internal package for a constant.
const (
	SlotCraftOutput = inventory.SlotCraftOutput
	SlotCraftGridA  = inventory.SlotCraftGridA
	SlotArmorHead   = inventory.SlotArmorHead
	SlotMainStart   = inventory.SlotMainStart
	SlotMainEnd     = inventory.SlotMainEnd
	SlotHotbarStart = inventory.SlotHotbarStart
	SlotHotbarEnd   = inventory.SlotHotbarEnd
	SlotOffhand     = inventory.SlotOffhand
	PlayerWindowID  = inventory.PlayerWindowID

	// StorageSlots is a player's carrying capacity: the 27 main slots plus the
	// 9 hotbar slots, excluding armour and the offhand.
	StorageSlots = inventory.StorageSlots

	hotbarSlotCount = inventory.HotbarSlots
)

// Container click modes. Mode 2 is the useful one here: it swaps a slot
// directly with a hotbar slot, which moves an item into the hand without ever
// picking it up onto the cursor.
const (
	ClickModeNormal     int32 = 0
	ClickModeQuickMove  int32 = 1 // shift-click
	ClickModeHotbarSwap int32 = 2
	ClickModeDrop       int32 = 4
)

// Inventory returns every non-empty slot the bot knows about, ordered by slot.
func (c *Client) Inventory() []ItemStack { return c.inv.Sorted() }

// SlotAt returns the contents of a specific slot.
func (c *Client) SlotAt(slot int) (ItemStack, bool) { return c.inv.Slot(slot) }

// InventoryTruncated reports whether the last full-window snapshot could not be
// decoded to the end. See the comment on inventory.truncated.
func (c *Client) InventoryTruncated() bool { return c.inv.Truncated() }

// FindItem returns the slot holding an item, preferring an exact name match
// and otherwise the lowest-numbered fuzzy one.
func (c *Client) FindItem(name string) (ItemStack, bool) { return c.inv.FindItem(name) }

// CountItem totals an item across every slot the bot knows about, including
// the offhand and armour.
func (c *Client) CountItem(name string) int32 { return c.inv.CountAll(name) }

// CountItemStorage totals an item across the 36 *storage* slots only. See
// inventory.CountStorage for why both numbers are worth having.
func (c *Client) CountItemStorage(name string) int32 { return c.inv.CountStorage(name) }

// FreeStorageSlots reports how many of the 36 storage slots are empty.
func (c *Client) FreeStorageSlots() int { return c.inv.FreeStorageSlots() }

// HoldItem puts a named item into the bot's hand, wherever it currently is.
//
// If the item is already on the hotbar this is just a slot selection. If it is
// in the main inventory it is swapped onto the hotbar first — which is the
// whole point, since a bot given a tool by `/give` has no control over where it
// lands.
func (c *Client) HoldItem(name string) (ItemStack, error) {
	if err := c.requireAlive("hold item"); err != nil {
		return ItemStack{}, err
	}
	item, ok := c.FindItem(name)
	if !ok {
		if c.InventoryTruncated() {
			return ItemStack{}, fmt.Errorf(
				"understudy: %q not found, and the inventory view is incomplete "+
					"(an item with data components stopped the scan)", name)
		}
		return ItemStack{}, fmt.Errorf("understudy: no %q in inventory (%d slots known)",
			name, len(c.Inventory()))
	}

	if item.Slot >= SlotHotbarStart && item.Slot <= SlotHotbarEnd {
		return item, c.SetHeldSlot(item.Slot - SlotHotbarStart)
	}

	// Swap it onto whichever hotbar slot is currently selected.
	hotbar := c.HeldSlot()
	if err := c.clickSlot(item.Slot, int8(hotbar), ClickModeHotbarSwap); err != nil {
		return item, err
	}
	// The server will echo the moved slots back; update optimistically so an
	// immediately following action sees the right thing.
	c.inv.SetSlot(SlotHotbarStart+hotbar, ItemStack{
		Slot: SlotHotbarStart + hotbar, ID: item.ID, Name: item.Name, Count: item.Count,
	})
	c.inv.SetSlot(item.Slot, ItemStack{Slot: item.Slot})
	return item, c.SetHeldSlot(hotbar)
}

// HeldItem returns what is currently in the bot's hand.
func (c *Client) HeldItem() (ItemStack, bool) {
	return c.inv.Slot(SlotHotbarStart + c.HeldSlot())
}

// SetHeldSlot selects a hotbar slot, 0-8.
func (c *Client) SetHeldSlot(slot int) error {
	if slot < 0 || slot >= hotbarSlotCount {
		return fmt.Errorf("understudy: hotbar slot %d out of range 0-%d", slot, hotbarSlotCount-1)
	}
	if err := c.requireAlive("select slot"); err != nil {
		return err
	}
	c.setHeldSlotLocal(slot)
	return c.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.SBPlayHeldItemSlot).I16(int16(slot)).Bytes())
}

// clickSlot sends a container click.
//
// changedSlots is deliberately sent empty and the cursor as absent. Those
// fields are the client's *prediction* of the outcome; the server verifies
// them against its own state and, on a mismatch, simply resends the authoritative
// contents. That resync is not a problem here — it is the desired behaviour,
// and it avoids having to compute the component hashes a populated prediction
// would require.
func (c *Client) clickSlot(slot int, button int8, mode int32) error {
	if c.v.Packets.SBPlayWindowClick == protocol.Absent {
		return errors.New("understudy: this version has no window_click packet")
	}
	w := protocol.NewWriter(c.v.Packets.SBPlayWindowClick).
		VarInt(PlayerWindowID).
		VarInt(c.inv.StateID()).
		I16(int16(slot)).
		I8(button).
		VarInt(mode).
		VarInt(0). // changedSlots: empty, let the server resync
		Bool(false)
	return c.conn.WritePacket(w.Bytes())
}

// SlotsNeeded reports how many slots a quantity of an item would occupy, and
// whether it can fit in a player's storage at all.
//
// Worth asking before setting something up: "hold 2304 dirt" needs all 36
// slots, which leaves no room for the tool the bot might otherwise be carrying.
func (c *Client) SlotsNeeded(name string, count int32) (slots int, fits bool) {
	if count <= 0 {
		return 0, true
	}
	stack := c.v.StackSizeOf(name)
	if stack <= 0 {
		stack = protocol.DefaultStackSize
	}
	slots = int((count + stack - 1) / stack)
	return slots, slots <= StorageSlots
}

// PickupsSeen returns how many items the bot has collected off the ground,
// and the per-item tally.
//
// Worth watching in both directions. Sometimes collecting things is the point;
// but a bot that wanders over its own mining drops also inflates counts nobody
// meant it to touch, and quietly changes the inventory something else is
// measuring. Either way it is better observed than inferred afterwards.
func (c *Client) PickupsSeen() (total int32, byItem map[string]int32) {
	return c.inv.Pickups()
}

// ResetPickups clears the pickup tally, so a caller can measure a window
// rather than a session.
func (c *Client) ResetPickups() { c.inv.ResetPickups() }

// pickupItemKey is the key PickupsSeen tallies under.
//
// The collect packet identifies the item *entity*, not what is inside it —
// every dropped stack is a "minecraft:item". Naming the contents would mean
// decoding entity metadata, which this client does not do, so the tally is
// deliberately by entity and the count is the useful part: it answers "did the
// bot pick anything up?", which is the question that matters when a stray drop
// would corrupt a count. What was picked up is visible in the inventory delta,
// and the authoritative per-item numbers are the server's own picked_up
// statistics.
const pickupItemKey = "item"

// readSlotFinal decodes an item stack that is the LAST field of its packet.
//
// This is the case that matters in practice. Components cannot be *skipped*
// without knowing ~100 wire shapes, but they can be ignored outright when
// nothing follows them — so a single-slot update decodes an enchanted item
// perfectly well, while only a whole-inventory snapshot has to give up.
//
// That asymmetry is worth exploiting: a bot typically joins with an empty
// inventory and receives its gear afterwards, one set_slot at a time.
func readSlotFinal(v *protocol.Version, r *protocol.Reader) (ItemStack, error) {
	count := r.VarInt()
	if err := r.Err(); err != nil {
		return ItemStack{}, err
	}
	if count <= 0 {
		return ItemStack{}, nil
	}
	id := r.VarInt()
	if err := r.Err(); err != nil {
		return ItemStack{}, err
	}
	// Whatever components follow are the rest of the packet; drop them.
	return ItemStack{ID: id, Name: v.ItemName(id), Count: count}, nil
}

// readSlot decodes one item stack.
//
// Components are the catch. An item that carries them (a named tool, custom
// damage, an enchantment) cannot be skipped without decoding every component
// type — roughly a hundred distinct structures — so this reports that it could
// not continue rather than guessing a length and desynchronising the rest of
// the packet.
func readSlot(v *protocol.Version, r *protocol.Reader) (ItemStack, error) {
	count := r.VarInt()
	if err := r.Err(); err != nil {
		return ItemStack{}, err
	}
	if count <= 0 {
		return ItemStack{}, nil
	}
	id := r.VarInt()
	added := r.VarInt()
	removed := r.VarInt()
	if err := r.Err(); err != nil {
		return ItemStack{}, err
	}
	if added > 0 {
		return ItemStack{}, fmt.Errorf(
			"item %s carries %d data components, which this client cannot skip",
			v.ItemName(id), added)
	}
	// Removed components are just a list of type IDs, which *can* be skipped.
	for range removed {
		r.VarInt()
	}
	if err := r.Err(); err != nil {
		return ItemStack{}, err
	}
	return ItemStack{ID: id, Name: v.ItemName(id), Count: count}, nil
}

// maxWindowSlots bounds a window_items count. The largest vanilla container is
// well under this; the cap stops a corrupt count preallocating a huge slice.
const maxWindowSlots = 1 << 12

// handleInventoryPacket decodes the inventory packets. Returns false if the
// packet was not one of them.
func (c *Client) handleInventoryPacket(p protocol.Packet) (bool, error) {
	switch p.ID {
	case c.v.Packets.CBPlayWindowItems:
		return true, c.handleWindowItems(p)
	case c.v.Packets.CBPlaySetSlot:
		return true, c.handleSetSlot(p)
	case c.v.Packets.CBPlayCollect:
		return true, c.handleCollect(p)
	case c.v.Packets.CBPlayHeldItemSlot:
		r := p.Reader()
		slot := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		if slot >= 0 && slot < hotbarSlotCount {
			c.setHeldSlotLocal(int(slot))
		}
		return true, nil
	}
	return false, nil
}

func (c *Client) handleWindowItems(p protocol.Packet) error {
	r := p.Reader()
	windowID := r.VarInt()
	stateID := r.VarInt()
	n := r.VarInt()
	if err := r.Err(); err != nil {
		return err
	}
	if n < 0 || n > maxWindowSlots {
		return fmt.Errorf("understudy: implausible window slot count %d", n)
	}
	// A container's contents arrive on its own window ID, and its state counter
	// is the one its clicks must echo — mixing the two up means every click
	// carries a stale state and the server silently resyncs instead of acting.
	toContainer := c.window.Matches(windowID)
	if toContainer {
		c.window.SetStateID(stateID)
	} else if windowID == PlayerWindowID {
		c.inv.SetStateID(stateID)
	} else {
		return nil // a window we are not tracking
	}

	items := make([]ItemStack, 0, n)
	truncated := false
	for i := range n {
		item, err := readSlot(c.v, r)
		if err != nil {
			// Keep what was decoded. Packets are length-framed, so an abandoned
			// parse costs this packet's tail and nothing else — the stream stays
			// in sync.
			c.log.Debug("inventory scan stopped early", "slot", i, "err", err)
			truncated = true
			break
		}
		item.Slot = int(i)
		items = append(items, item)
	}
	if toContainer {
		c.window.ReplaceAll(items, truncated)
		return nil
	}
	c.inv.ReplaceAll(items, truncated)
	return nil
}

func (c *Client) handleSetSlot(p protocol.Packet) error {
	r := p.Reader()
	windowID := r.VarInt()
	stateID := r.VarInt()
	slot := r.I16()
	if err := r.Err(); err != nil {
		return err
	}
	toContainer := c.window.Matches(windowID)
	if toContainer {
		c.window.SetStateID(stateID)
	} else if windowID == PlayerWindowID {
		c.inv.SetStateID(stateID)
	} else {
		return nil
	}
	// The item is the final field, so components need not be skipped.
	item, err := readSlotFinal(c.v, r)
	if err != nil {
		c.log.Debug("could not decode slot update", "slot", slot, "err", err)
		return nil
	}
	item.Slot = int(slot)
	if toContainer {
		c.window.SetSlot(int(slot), item)
		return nil
	}
	c.inv.SetSlot(int(slot), item)
	return nil
}

func (c *Client) handleCollect(p protocol.Packet) error {
	r := p.Reader()
	collected := r.VarInt()
	collector := r.VarInt()
	count := r.VarInt()
	if err := r.Err(); err != nil {
		return err
	}
	// Only our own pickups matter; the server broadcasts everyone's.
	if collector != c.EntityID() {
		return nil
	}
	c.inv.RecordPickup(pickupItemKey, count)
	c.log.Debug("picked up", "count", count, "entity", collected)
	return nil
}
