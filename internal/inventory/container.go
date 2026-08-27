package inventory

import "sync"

// NoWindow is the window ID when nothing is open. The player's own inventory
// is window 0 and is always available, so "no container" needs a distinct
// value rather than a zero.
const NoWindow = -1

// Container is a block or entity UI the server has opened: a crafting table,
// a smithing table, a chest, a villager's trades.
//
// It is deliberately separate from Inventory. The player's inventory is always
// there and is addressed by fixed slot constants; a container appears with a
// server-assigned ID, has a layout that depends on its type, and vanishes. The
// two also disagree about what a slot number means — slot 0 is the crafting
// *result* in a crafting table and the first storage slot in a chest — so
// merging them would make every slot index ambiguous.
type Container struct {
	mu       sync.RWMutex
	open     bool
	id       int32
	kind     int32
	title    string
	slots    []ItemStack
	declared int
	trimmed  bool
	stateID  int32
	sequence int
}

// NewContainer returns a closed container.
func NewContainer() *Container { return &Container{id: NoWindow} }

// Open records a newly opened window, discarding anything from a previous one.
func (c *Container) Open(id, kind int32, title string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.open, c.id, c.kind, c.title = true, id, kind, title
	c.slots, c.declared, c.trimmed = nil, 0, false
	c.sequence++
}

// Close marks the window shut. The contents are kept: a caller that opened a
// container, took something and closed it still wants to know what was there.
func (c *Container) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.open = false
	c.id = NoWindow
}

// IsOpen reports whether a container window is currently open.
func (c *Container) IsOpen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.open
}

// ID returns the server-assigned window ID, or NoWindow when nothing is open.
//
// Clicks must carry this rather than the player's window ID: a click sent to
// window 0 while a container is open is applied to the player's inventory
// instead, which is a silent wrong answer rather than an error.
func (c *Container) ID() int32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.id
}

// Kind returns the window type ID the server reported.
func (c *Container) Kind() int32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.kind
}

// Title returns the window title as plain text, best-effort.
func (c *Container) Title() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.title
}

// Sequence counts how many windows have been opened on this connection. It
// lets a caller tell "the window I opened" from "a window that was already
// open", without racing the packet that carries the ID.
func (c *Container) Sequence() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sequence
}

// Matches reports whether an incoming window ID belongs to the open container.
func (c *Container) Matches(windowID int32) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.open && windowID == c.id
}

// ReplaceAll sets the contents. declared is the slot count the server stated,
// which is not always how many items decoded: an item carrying data components
// stops the scan, and trimmed records that.
//
// The two are kept apart because the window's *shape* must survive a partial
// decode. Sizing the layout from the decoded items instead made a 41-slot
// brewing stand look like 32, so "the container's own slots" came out as
// 41-36=5 one moment and 32-36 clamped to 0 the next — and every slot lookup
// then searched from the wrong floor, silently.
func (c *Container) ReplaceAll(slots []ItemStack, declared int, trimmed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slots, c.declared, c.trimmed = slots, declared, trimmed
	if declared > len(slots) {
		// Pad so Slot lookups inside the window answer "empty" rather than
		// "out of range" for the slots that could not be decoded.
		for i := len(slots); i < declared; i++ {
			c.slots = append(c.slots, ItemStack{Slot: i})
		}
	}
}

// SetSlot updates one slot, growing the view if the server addresses past the
// end — which happens when set_slot arrives before the full window_items.
func (c *Container) SetSlot(slot int, item ItemStack) {
	if slot < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.slots) <= slot {
		c.slots = append(c.slots, ItemStack{Slot: len(c.slots)})
	}
	item.Slot = slot
	c.slots[slot] = item
}

// SetStateID records the server's state counter, which every click must echo.
func (c *Container) SetStateID(id int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stateID = id
}

// StateID returns the last state counter the server sent.
func (c *Container) StateID() int32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stateID
}

// Slots returns a copy of the container's contents.
func (c *Container) Slots() []ItemStack {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ItemStack, len(c.slots))
	copy(out, c.slots)
	return out
}

// Trimmed reports whether the contents are incomplete because a slot could not
// be decoded.
func (c *Container) Trimmed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.trimmed
}

// Slot returns one slot's contents and whether it is within the window.
func (c *Container) Slot(slot int) (ItemStack, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if slot < 0 || slot >= len(c.slots) {
		return ItemStack{}, false
	}
	return c.slots[slot], true
}

// Size returns how many slots the window covers, including the player's own
// inventory rows, which the server appends to every container.
//
// This is the count the server declared, not the number of items successfully
// decoded — see ReplaceAll for why that distinction is load-bearing.
func (c *Container) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.declared > len(c.slots) {
		return c.declared
	}
	return len(c.slots)
}

// Find returns the slot holding a named item, using the same exact-then-fuzzy
// rules as the player inventory so callers do not have to learn two.
func (c *Container) Find(name string) (ItemStack, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Find(c.slots, name)
}

// Count totals a named item across the window.
func (c *Container) Count(name string) int32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var total int32
	for _, item := range c.slots {
		if exact, fuzzy := Matches(item, name); exact || fuzzy {
			total += item.Count
		}
	}
	return total
}

// FindFrom returns the lowest slot at or above floor holding an item.
//
// The floor matters mid-craft. A container window puts the crafting grid at
// low slot numbers and the player's own rows after it, and Find returns the
// lowest match — so once an ingredient has been placed into the grid, the next
// lookup finds *that* one and the loop picks its own work back up, assembling
// a single-item recipe forever. Same reason Inventory has FindInStorage.
func (c *Container) FindFrom(name string, floor int) (ItemStack, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if floor <= 0 {
		return Find(c.slots, name)
	}
	if floor >= len(c.slots) {
		return ItemStack{}, false
	}
	return Find(c.slots[floor:], name)
}

// CountFrom totals a named item at or above a slot floor.
//
// For a merchant window that means "how many has the player actually got",
// counting the player's own rows and not the offer's result slot — which is
// the difference between a trade that happened and one the server merely
// re-offered.
func (c *Container) CountFrom(name string, floor int) int32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var total int32
	for _, item := range c.slots {
		if item.Slot < floor {
			continue
		}
		if exact, fuzzy := Matches(item, name); exact || fuzzy {
			total += item.Count
		}
	}
	return total
}
