package understudy

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/blocktopia/understudy-client/protocol"
)

// Player inventory slot layout for window 0. These indices are what
// window_click addresses, and they are not the same as the hotbar numbers a
// player sees.
const (
	SlotCraftOutput = 0
	SlotCraftGridA  = 1 // 1..4
	SlotArmorHead   = 5 // 5..8
	SlotMainStart   = 9 // 9..35, the three upper rows
	SlotMainEnd     = 35
	SlotHotbarStart = 36 // 36..44, shown to the player as 1..9
	SlotHotbarEnd   = 44
	SlotOffhand     = 45
	PlayerWindowID  = 0

	hotbarSlotCount = 9
)

// StorageSlots is a player's carrying capacity: the 27 main slots plus the 9
// hotbar slots. Armour and the offhand are deliberately excluded — see
// CountItemStorage.
const StorageSlots = 36

// Container click modes. Mode 2 is the useful one here: it swaps a slot
// directly with a hotbar slot, which moves an item into the hand without ever
// picking it up onto the cursor.
const (
	ClickModeNormal     int32 = 0
	ClickModeQuickMove  int32 = 1 // shift-click
	ClickModeHotbarSwap int32 = 2
	ClickModeDrop       int32 = 4
)

// ItemStack is one inventory slot's contents.
type ItemStack struct {
	Slot  int    `json:"slot"`
	ID    int32  `json:"id"`
	Name  string `json:"name"`
	Count int32  `json:"count"`
}

// Empty reports whether the slot holds nothing.
func (i ItemStack) Empty() bool { return i.Count <= 0 }

// isStorage reports whether a slot is one of the 36 storage slots, as opposed
// to armour, the offhand or the crafting grid.
func (i ItemStack) isStorage() bool {
	return i.Slot >= SlotMainStart && i.Slot <= SlotHotbarEnd
}

// inventory is the bot's view of its own slots.
//
// Contents arrive as whole-window snapshots (window_items) and single-slot
// updates (set_slot). Both are tracked, because a bot can be handed items at
// any moment and a stale view would pick the wrong tool.
//
// Every field is reached through a method on this type, so the locking
// discipline lives in one place.
type inventory struct {
	mu      sync.RWMutex
	slots   map[int]ItemStack
	stateID int32
	// truncated records that a window_items packet could not be fully decoded
	// because an item carried data components. The slots before that point are
	// still valid; the ones after are unknown.
	truncated bool
	// pickups tallies items collected off the ground, by item name.
	pickups map[string]int32
}

func newInventory() *inventory {
	return &inventory{
		slots:   make(map[int]ItemStack),
		pickups: make(map[string]int32),
	}
}

func (inv *inventory) setSlot(slot int, item ItemStack) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if item.Empty() {
		delete(inv.slots, slot)
		return
	}
	inv.slots[slot] = item
}

func (inv *inventory) replaceAll(items []ItemStack, truncated bool) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	clear(inv.slots)
	for _, it := range items {
		if !it.Empty() {
			inv.slots[it.Slot] = it
		}
	}
	inv.truncated = truncated
}

func (inv *inventory) setStateID(id int32) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.stateID = id
}

func (inv *inventory) getStateID() int32 {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	return inv.stateID
}

func (inv *inventory) isTruncated() bool {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	return inv.truncated
}

// slot returns one slot's contents directly, without materialising the whole
// inventory. The crafting code asks about individual slots in a loop; doing
// that through the sorted snapshot allocated and sorted the entire inventory
// once per question.
func (inv *inventory) slot(i int) (ItemStack, bool) {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	it, ok := inv.slots[i]
	return it, ok
}

// sorted returns every non-empty slot, ordered by slot index.
func (inv *inventory) sorted() []ItemStack {
	inv.mu.RLock()
	out := make([]ItemStack, 0, len(inv.slots))
	for _, it := range inv.slots {
		out = append(out, it)
	}
	inv.mu.RUnlock()
	// Stable order makes any output and any test assertion reproducible.
	slices.SortFunc(out, func(a, b ItemStack) int { return cmp.Compare(a.Slot, b.Slot) })
	return out
}

func (inv *inventory) recordPickup(item string, count int32) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.pickups[item] += count
}

func (inv *inventory) pickupTally() (total int32, byItem map[string]int32) {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	out := make(map[string]int32, len(inv.pickups))
	for k, v := range inv.pickups {
		out[k] = v
		total += v
	}
	return total, out
}

func (inv *inventory) resetPickups() {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	clear(inv.pickups)
}

// Inventory returns every non-empty slot the bot knows about, ordered by slot.
func (c *Client) Inventory() []ItemStack { return c.inv.sorted() }

// SlotAt returns the contents of a specific slot.
func (c *Client) SlotAt(slot int) (ItemStack, bool) { return c.inv.slot(slot) }

// InventoryTruncated reports whether the last full-window snapshot could not be
// decoded to the end. See the comment on inventory.truncated.
func (c *Client) InventoryTruncated() bool { return c.inv.isTruncated() }

// matchesItem reports whether a stack is the item the caller asked for.
//
// A bare name ("diamond_pickaxe") and a namespaced one both work, and a suffix
// match is accepted so "pickaxe" finds "minecraft:diamond_pickaxe". The suffix
// form is deliberately loose — it is a convenience — so callers that need
// certainty should pass the full name and check exact.
func matchesItem(stack ItemStack, name string) (exact, fuzzy bool) {
	want := protocol.Namespaced(name)
	if stack.Name == want {
		return true, true
	}
	return false, strings.HasSuffix(stack.Name, "_"+protocol.BareName(name))
}

// FindItem returns the slot holding an item, preferring an exact name match
// and otherwise the lowest-numbered fuzzy one.
func (c *Client) FindItem(name string) (ItemStack, bool) {
	return findItem(c.Inventory(), name)
}

// findItem is the pure half of FindItem, so the matching rules can be tested
// without a connected client.
func findItem(items []ItemStack, name string) (ItemStack, bool) {
	var best ItemStack
	found, bestExact := false, false
	for _, it := range items {
		exact, fuzzy := matchesItem(it, name)
		if !exact && !fuzzy {
			continue
		}
		// items is slot-ordered, so the first match is the lowest slot; only an
		// exact match may displace an earlier fuzzy one.
		if !found || (exact && !bestExact) {
			best, found, bestExact = it, true, exact
		}
	}
	return best, found
}

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
	c.inv.setSlot(SlotHotbarStart+hotbar, ItemStack{
		Slot: SlotHotbarStart + hotbar, ID: item.ID, Name: item.Name, Count: item.Count,
	})
	c.inv.setSlot(item.Slot, ItemStack{Slot: item.Slot})
	return item, c.SetHeldSlot(hotbar)
}

// HeldItem returns what is currently in the bot's hand.
func (c *Client) HeldItem() (ItemStack, bool) {
	return c.inv.slot(SlotHotbarStart + c.HeldSlot())
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
		VarInt(c.inv.getStateID()).
		I16(int16(slot)).
		I8(button).
		VarInt(mode).
		VarInt(0). // changedSlots: empty, let the server resync
		Bool(false)
	return c.conn.WritePacket(w.Bytes())
}

// CountItem totals an item across every slot the bot knows about, including
// the offhand and armour.
func (c *Client) CountItem(name string) int32 {
	return c.countItems(name, func(ItemStack) bool { return true })
}

// CountItemStorage totals an item across the 36 *storage* slots only.
//
// The distinction is not academic. Server implementations disagree about what
// "the inventory" means: some scan the 36 storage slots, others the whole
// container (41, including armour and the offhand). An item in the offhand
// therefore counts on one and not the other. Reporting both numbers is what
// lets a caller detect that divergence rather than silently inherit it.
func (c *Client) CountItemStorage(name string) int32 {
	return c.countItems(name, ItemStack.isStorage)
}

// countItems totals exact name matches across the slots a filter admits.
// Counting is deliberately exact-match only: a fuzzy total would silently add
// dark_oak_planks to an oak_planks count.
func (c *Client) countItems(name string, include func(ItemStack) bool) int32 {
	want := protocol.Namespaced(name)
	var total int32
	for _, it := range c.Inventory() {
		if it.Name == want && include(it) {
			total += it.Count
		}
	}
	return total
}

// FreeStorageSlots reports how many of the 36 storage slots are empty.
func (c *Client) FreeStorageSlots() int {
	used := 0
	for _, it := range c.Inventory() {
		if it.isStorage() && !it.Empty() {
			used++
		}
	}
	return StorageSlots - used
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
	return c.inv.pickupTally()
}

// ResetPickups clears the pickup tally, so a caller can measure a window
// rather than a session.
func (c *Client) ResetPickups() { c.inv.resetPickups() }

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
	c.inv.setStateID(stateID)
	if windowID != PlayerWindowID {
		return nil // a chest or similar; not tracked
	}
	if n < 0 || n > maxWindowSlots {
		return fmt.Errorf("understudy: implausible window slot count %d", n)
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
	c.inv.replaceAll(items, truncated)
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
	c.inv.setStateID(stateID)
	if windowID != PlayerWindowID {
		return nil
	}
	// The item is the final field, so components need not be skipped.
	item, err := readSlotFinal(c.v, r)
	if err != nil {
		c.log.Debug("could not decode slot update", "slot", slot, "err", err)
		return nil
	}
	item.Slot = int(slot)
	c.inv.setSlot(int(slot), item)
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
	c.inv.recordPickup(pickupItemKey, count)
	c.log.Debug("picked up", "count", count, "entity", collected)
	return nil
}
