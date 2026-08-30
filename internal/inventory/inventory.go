// Package inventory is the client's view of its own slots.
//
// Contents arrive as whole-window snapshots and single-slot updates. Both are
// tracked, because a bot can be handed items at any moment and a stale view
// would pick the wrong tool.
//
// Free of any notion of the connection: it stores slots and answers questions
// about them. Sending a container click is the caller's job.
package inventory

import (
	"cmp"
	"slices"
	"strings"
	"sync"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// Player inventory slot layout for window 0. These indices are what a
// container click addresses, and they are not the same as the hotbar numbers a
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

	// HotbarSlots is how many hotbar slots a player has.
	HotbarSlots = 9
)

// StorageSlots is a player's carrying capacity: the 27 main slots plus the 9
// hotbar slots. Armour and the offhand are deliberately excluded — see
// CountStorage.
const StorageSlots = 36

// ItemStack is one slot's contents.
type ItemStack struct {
	Slot  int    `json:"slot"`
	ID    int32  `json:"id"`
	Name  string `json:"name"`
	Count int32  `json:"count"`

	// Potion is the potion-type id carried by a potion's contents component,
	// or -1 for an item that has none.
	//
	// It is here because the item *name* cannot tell potions apart: a water
	// bottle, an awkward potion and a potion of strength are all
	// "minecraft:potion", and only the component differs. Anything watching a
	// brewing stand for progress has nothing else to compare.
	Potion int32 `json:"potion,omitempty"`
}

// NoPotion marks an item that carries no potion contents.
const NoPotion = -1

// Empty reports whether the slot holds nothing.
func (i ItemStack) Empty() bool { return i.Count <= 0 }

// IsStorage reports whether a slot is one of the 36 storage slots, as opposed
// to armour, the offhand or the crafting grid.
func (i ItemStack) IsStorage() bool {
	return i.Slot >= SlotMainStart && i.Slot <= SlotHotbarEnd
}

// Inventory holds the slots. The zero value is not usable; call New.
type Inventory struct {
	mu      sync.RWMutex
	slots   map[int]ItemStack
	stateID int32
	// truncated records that a whole-window packet could not be fully decoded
	// because an item carried data components. The slots before that point are
	// still valid; the ones after are unknown.
	truncated bool
	// pickups tallies items collected off the ground, by key.
	pickups map[string]int32
}

// New returns an empty Inventory.
func New() *Inventory {
	return &Inventory{
		slots:   make(map[int]ItemStack),
		pickups: make(map[string]int32),
	}
}

// SetSlot records one slot's contents. An empty stack removes the slot rather
// than leaving a phantom entry behind.
func (inv *Inventory) SetSlot(slot int, item ItemStack) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if item.Empty() {
		delete(inv.slots, slot)
		return
	}
	inv.slots[slot] = item
}

// ReplaceAll swaps in a whole-window snapshot, recording whether the decode
// ran out before the end.
func (inv *Inventory) ReplaceAll(items []ItemStack, truncated bool) {
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

// SetStateID records the window state ID a container click must echo back.
func (inv *Inventory) SetStateID(id int32) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.stateID = id
}

// StateID returns the last window state ID seen.
func (inv *Inventory) StateID() int32 {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	return inv.stateID
}

// Truncated reports whether the last whole-window snapshot could not be
// decoded to the end. A partial view is why a "missing" item might not
// actually be missing.
func (inv *Inventory) Truncated() bool {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	return inv.truncated
}

// Slot returns one slot's contents without materialising the whole inventory.
//
// The crafting code asks about individual slots in a loop; doing that through
// the sorted snapshot allocated and sorted everything once per question.
func (inv *Inventory) Slot(i int) (ItemStack, bool) {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	it, ok := inv.slots[i]
	return it, ok
}

// Sorted returns every non-empty slot, ordered by slot index. The stable order
// makes any output and any test assertion reproducible.
func (inv *Inventory) Sorted() []ItemStack {
	inv.mu.RLock()
	out := make([]ItemStack, 0, len(inv.slots))
	for _, it := range inv.slots {
		out = append(out, it)
	}
	inv.mu.RUnlock()
	slices.SortFunc(out, func(a, b ItemStack) int { return cmp.Compare(a.Slot, b.Slot) })
	return out
}

// RecordPickup adds to the tally of items collected off the ground.
func (inv *Inventory) RecordPickup(key string, count int32) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.pickups[key] += count
}

// Pickups returns the total collected and the per-key tally. The map is a copy,
// so a caller cannot corrupt the running count.
func (inv *Inventory) Pickups() (total int32, byKey map[string]int32) {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	out := make(map[string]int32, len(inv.pickups))
	for k, v := range inv.pickups {
		out[k] = v
		total += v
	}
	return total, out
}

// ResetPickups clears the tally, so a caller can measure a window rather than
// a session.
func (inv *Inventory) ResetPickups() {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	clear(inv.pickups)
}

// --- lookups ----------------------------------------------------------------

// Matches reports whether a stack is the item the caller asked for.
//
// A bare name ("diamond_pickaxe") and a namespaced one both work, and a suffix
// match is accepted so "pickaxe" finds "minecraft:diamond_pickaxe". The suffix
// form is deliberately loose — it is a convenience — so callers that need
// certainty should pass the full name and check exact.
func Matches(stack ItemStack, name string) (exact, fuzzy bool) {
	// A block state or a component list is not something this client can match
	// on — it knows an item by its id — so the qualifier is dropped rather than
	// allowed to match nothing at all.
	name = protocol.BaseID(name)
	want := protocol.Namespaced(name)
	if stack.Name == want {
		return true, true
	}
	return false, strings.HasSuffix(stack.Name, "_"+protocol.BareName(name))
}

// Find returns the stack holding an item, preferring an exact name match and
// otherwise the lowest-numbered fuzzy one.
//
// items must be slot-ordered, which is what makes "lowest slot" meaningful.
func Find(items []ItemStack, name string) (ItemStack, bool) {
	var best ItemStack
	found, bestExact := false, false
	for _, it := range items {
		exact, fuzzy := Matches(it, name)
		if !exact && !fuzzy {
			continue
		}
		// Only an exact match may displace an earlier fuzzy one.
		if !found || (exact && !bestExact) {
			best, found, bestExact = it, true, exact
		}
	}
	return best, found
}

// FindItem locates an item anywhere in the inventory.
func (inv *Inventory) FindItem(name string) (ItemStack, bool) {
	return Find(inv.Sorted(), name)
}

// FindInStorage locates an item in the storage slots only.
//
// Deliberately not FindItem: that searches every tracked slot, including the
// crafting grid. Mid-craft that means the "ingredient" it finds can be the one
// just placed into the grid, so the loop picks its own work back up and only
// ever assembles a single-item recipe.
func (inv *Inventory) FindInStorage(name string) (ItemStack, bool) {
	all := inv.Sorted()
	storage := make([]ItemStack, 0, StorageSlots)
	for _, it := range all {
		if it.IsStorage() {
			storage = append(storage, it)
		}
	}
	return Find(storage, name)
}

// Count totals exact name matches across the slots a filter admits.
//
// Counting is deliberately exact-match only: a fuzzy total would silently add
// dark_oak_planks to an oak_planks count.
func (inv *Inventory) Count(name string, include func(ItemStack) bool) int32 {
	// Exact on the id, and the id is what a qualifier is not part of. Matches
	// drops one for the same reason; counting has to agree with it, or asking
	// "how many" and asking "hold one" answer differently about the same stack.
	want := protocol.Namespaced(protocol.BaseID(name))
	var total int32
	for _, it := range inv.Sorted() {
		if it.Name == want && include(it) {
			total += it.Count
		}
	}
	return total
}

// CountAll totals an item across every slot, including the offhand and armour.
func (inv *Inventory) CountAll(name string) int32 {
	return inv.Count(name, func(ItemStack) bool { return true })
}

// CountStorage totals an item across the 36 storage slots only.
//
// The distinction is not academic. Server implementations disagree about what
// "the inventory" means: some scan the 36 storage slots, others the whole
// container (41, including armour and the offhand). An item in the offhand
// therefore counts on one and not the other. Reporting both numbers is what
// lets a caller detect that divergence rather than silently inherit it.
func (inv *Inventory) CountStorage(name string) int32 {
	return inv.Count(name, ItemStack.IsStorage)
}

// FreeStorageSlots reports how many of the 36 storage slots are empty.
func (inv *Inventory) FreeStorageSlots() int {
	used := 0
	for _, it := range inv.Sorted() {
		if it.IsStorage() && !it.Empty() {
			used++
		}
	}
	return StorageSlots - used
}
