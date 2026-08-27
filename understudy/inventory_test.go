package understudy

import (
	"testing"

	"github.com/blocktopia/understudy-client/protocol"
)

func stack(slot int, name string, count int32) ItemStack {
	return ItemStack{Slot: slot, Name: protocol.Namespaced(name), Count: count}
}

// Totems stack to 1, so "hold 5 totems" needs five whole slots, while
// "hold 2304 dirt" needs exactly 36 — every storage slot a player has.
func TestSlotsNeeded(t *testing.T) {
	c := newTestClient(t)
	for _, tc := range []struct {
		name      string
		item      string
		count     int32
		wantSlots int
		wantFits  bool
	}{
		{"nothing", "dirt", 0, 0, true},
		{"one stack", "dirt", 64, 1, true},
		{"one over a stack", "dirt", 65, 2, true},
		{"exactly the inventory", "dirt", 2304, 36, true},
		{"one over the inventory", "dirt", 2305, 37, false},
		{"unstackable", "totem_of_undying", 5, 5, true},
		{"unknown item defaults to 64", "mystery_item", 64, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slots, fits := c.SlotsNeeded(tc.item, tc.count)
			if slots != tc.wantSlots || fits != tc.wantFits {
				t.Errorf("SlotsNeeded(%q, %d) = %d, %v; want %d, %v",
					tc.item, tc.count, slots, fits, tc.wantSlots, tc.wantFits)
			}
		})
	}
}

func TestHeldItemTracksTheSelectedSlot(t *testing.T) {
	c := newTestClient(t)
	c.inv.SetSlot(SlotHotbarStart+3, stack(SlotHotbarStart+3, "diamond_pickaxe", 1))

	if _, ok := c.HeldItem(); ok {
		t.Error("HeldItem() found something in slot 0, want nothing")
	}
	c.setHeldSlotLocal(3)
	got, ok := c.HeldItem()
	if !ok || got.Name != "minecraft:diamond_pickaxe" {
		t.Errorf("HeldItem() = %+v, %v; want the pickaxe", got, ok)
	}
}

func TestSetHeldSlotRange(t *testing.T) {
	c := newTestClient(t)
	for _, slot := range []int{-1, 9, 100} {
		if err := c.SetHeldSlot(slot); err == nil {
			t.Errorf("SetHeldSlot(%d) = nil error, want an out-of-range error", slot)
		}
	}
}

func TestPickupTally(t *testing.T) {
	c := newTestClient(t)
	total, byItem := c.PickupsSeen()
	if total != 0 || len(byItem) != 0 {
		t.Errorf("a fresh client has picked up %d items, want 0", total)
	}

	c.inv.RecordPickup(pickupItemKey, 3)
	c.inv.RecordPickup(pickupItemKey, 2)
	total, byItem = c.PickupsSeen()
	if total != 5 || byItem[pickupItemKey] != 5 {
		t.Errorf("PickupsSeen() = %d, %v; want 5", total, byItem)
	}

	// The returned map must be a copy, or a caller could corrupt the tally.
	byItem[pickupItemKey] = 999
	if total, _ = c.PickupsSeen(); total != 5 {
		t.Errorf("PickupsSeen() = %d after mutating the returned map, want 5", total)
	}

	c.ResetPickups()
	if total, _ = c.PickupsSeen(); total != 0 {
		t.Errorf("PickupsSeen() = %d after reset, want 0", total)
	}
}

// --- slot decoding -----------------------------------------------------------

func TestReadSlotEmpty(t *testing.T) {
	v := testVersion(t)
	r := protocol.NewReader(protocol.NewWriter(0).VarInt(0).Bytes()[1:])
	got, err := readSlot(v, r)
	if err != nil {
		t.Fatalf("readSlot: %v", err)
	}
	if !got.Empty() {
		t.Errorf("readSlot of a zero count = %+v, want empty", got)
	}
}

// Removed components are just a list of type IDs, which *can* be skipped.
func TestReadSlotWithRemovedComponents(t *testing.T) {
	v := testVersion(t)
	w := protocol.NewWriter(0).VarInt(5).VarInt(1).VarInt(0).VarInt(2).VarInt(7).VarInt(8)
	got, err := readSlot(v, protocol.NewReader(w.Bytes()[1:]))
	if err != nil {
		t.Fatalf("readSlot: %v", err)
	}
	if got.Count != 5 || got.Name != "minecraft:dirt" {
		t.Errorf("readSlot = %+v, want 5 dirt", got)
	}
}

// An item carrying components cannot be skipped without decoding ~100 wire
// shapes, so the decoder must report that it stopped rather than guess a
// length and desynchronise the rest of the packet.
func TestReadSlotRefusesAddedComponents(t *testing.T) {
	v := testVersion(t)
	w := protocol.NewWriter(0).VarInt(1).VarInt(5).VarInt(3).VarInt(0)
	if _, err := readSlot(v, protocol.NewReader(w.Bytes()[1:])); err == nil {
		t.Error("readSlot of an item with added components = nil error, want an error")
	}
}

// A single-slot update puts the item last, so components can be ignored
// outright — which is why an enchanted tool decodes here but not in a whole
// window snapshot.
func TestReadSlotFinalIgnoresComponents(t *testing.T) {
	v := testVersion(t)
	w := protocol.NewWriter(0).VarInt(1).VarInt(5).
		VarInt(99).VarInt(1).VarInt(2).VarInt(3) // trailing component junk
	got, err := readSlotFinal(v, protocol.NewReader(w.Bytes()[1:]))
	if err != nil {
		t.Fatalf("readSlotFinal: %v", err)
	}
	if got.Count != 1 || got.Name != "minecraft:diamond_pickaxe" {
		t.Errorf("readSlotFinal = %+v, want 1 diamond_pickaxe", got)
	}
}

// --- packet handling ---------------------------------------------------------

func TestHandleWindowItems(t *testing.T) {
	c := newTestClient(t)
	p := packet(c.v.Packets.CBPlayWindowItems, func(w *protocol.Writer) {
		w.VarInt(PlayerWindowID).VarInt(7).VarInt(3)
		w.VarInt(0)                               // slot 0: empty
		w.VarInt(2).VarInt(1).VarInt(0).VarInt(0) // slot 1: 2 dirt
		w.VarInt(1).VarInt(2).VarInt(0).VarInt(0) // slot 2: 1 oak_log
	})
	handled, err := c.handleInventoryPacket(p)
	if !handled || err != nil {
		t.Fatalf("handleInventoryPacket = %v, %v", handled, err)
	}
	if got := c.inv.StateID(); got != 7 {
		t.Errorf("state id = %d, want 7", got)
	}
	if it, ok := c.SlotAt(1); !ok || it.Count != 2 || it.Name != "minecraft:dirt" {
		t.Errorf("slot 1 = %+v, %v; want 2 dirt", it, ok)
	}
	if _, ok := c.SlotAt(0); ok {
		t.Error("slot 0 is present, want it dropped as empty")
	}
	if c.InventoryTruncated() {
		t.Error("InventoryTruncated() = true for a fully decoded window")
	}
}

// A window that hits an undecodable item keeps what it read and says so; the
// stream stays in sync because packets are length-framed.
func TestHandleWindowItemsTruncates(t *testing.T) {
	c := newTestClient(t)
	p := packet(c.v.Packets.CBPlayWindowItems, func(w *protocol.Writer) {
		w.VarInt(PlayerWindowID).VarInt(1).VarInt(2)
		w.VarInt(2).VarInt(1).VarInt(0).VarInt(0) // slot 0: 2 dirt
		w.VarInt(1).VarInt(5).VarInt(4).VarInt(0) // slot 1: has components
	})
	if _, err := c.handleInventoryPacket(p); err != nil {
		t.Fatalf("handleInventoryPacket: %v", err)
	}
	if !c.InventoryTruncated() {
		t.Error("InventoryTruncated() = false, want true after an undecodable item")
	}
	if it, ok := c.SlotAt(0); !ok || it.Count != 2 {
		t.Errorf("slot 0 = %+v, %v; want the slots before the failure kept", it, ok)
	}
}

func TestHandleWindowItemsIgnoresOtherWindows(t *testing.T) {
	c := newTestClient(t)
	c.inv.SetSlot(9, stack(9, "dirt", 1))
	p := packet(c.v.Packets.CBPlayWindowItems, func(w *protocol.Writer) {
		w.VarInt(3).VarInt(1).VarInt(0) // a chest, not the player window
	})
	if _, err := c.handleInventoryPacket(p); err != nil {
		t.Fatalf("handleInventoryPacket: %v", err)
	}
	if _, ok := c.SlotAt(9); !ok {
		t.Error("a chest's contents cleared the player inventory")
	}
}

func TestHandleWindowItemsRejectsImplausibleCount(t *testing.T) {
	c := newTestClient(t)
	p := packet(c.v.Packets.CBPlayWindowItems, func(w *protocol.Writer) {
		w.VarInt(PlayerWindowID).VarInt(1).VarInt(1 << 20)
	})
	if _, err := c.handleInventoryPacket(p); err == nil {
		t.Error("an implausible slot count = nil error, want an error")
	}
}

func TestHandleSetSlot(t *testing.T) {
	c := newTestClient(t)
	// A real set_slot carries the full item: count, id, then the added and
	// removed component counts. The fixture used to stop after the id, which
	// mirrored a reader that ignored the rest — so it kept passing while the
	// reader could not have handled an actual packet.
	p := packet(c.v.Packets.CBPlaySetSlot, func(w *protocol.Writer) {
		w.VarInt(PlayerWindowID).VarInt(2).I16(36).
			VarInt(9).VarInt(1). // 9 dirt
			VarInt(0).VarInt(0)  // no components added or removed
	})
	if _, err := c.handleInventoryPacket(p); err != nil {
		t.Fatalf("handleInventoryPacket: %v", err)
	}
	if it, ok := c.SlotAt(36); !ok || it.Count != 9 || it.Name != "minecraft:dirt" {
		t.Errorf("slot 36 = %+v, %v; want 9 dirt", it, ok)
	}
}

// A potion arrives by set_slot when brewing finishes, and every potion is
// named "minecraft:potion" — so the contents component is the only thing that
// says which one it is. Dropping it left Brew waiting out its timeout on a
// change it could not see.
func TestHandleSetSlotKeepsThePotionIdentity(t *testing.T) {
	c := newTestClient(t)
	p := packet(c.v.Packets.CBPlaySetSlot, func(w *protocol.Writer) {
		w.VarInt(PlayerWindowID).VarInt(3).I16(36).
			VarInt(1).VarInt(1).  // one item
			VarInt(1).VarInt(0).  // one component added
			VarInt(51).           // potion contents
			Bool(true).VarInt(7). // potion id 7
			Bool(false).          // no custom colour
			VarInt(0).VarInt(0)   // no effects
	})
	if _, err := c.handleInventoryPacket(p); err != nil {
		t.Fatalf("handleInventoryPacket: %v", err)
	}
	it, ok := c.SlotAt(36)
	if !ok {
		t.Fatal("slot 36 is empty; the component made the item undecodable")
	}
	if it.Potion != 7 {
		t.Errorf("Potion = %d, want 7 — without it one potion cannot be told from another", it.Potion)
	}
}

// The server broadcasts everyone's pickups; only our own count.
func TestHandleCollectOnlyCountsOurOwn(t *testing.T) {
	c := newTestClient(t)
	c.mu.Lock()
	c.entityID = 100
	c.mu.Unlock()

	for _, p := range []protocol.Packet{
		packet(c.v.Packets.CBPlayCollect, func(w *protocol.Writer) { w.VarInt(1).VarInt(100).VarInt(4) }),
		packet(c.v.Packets.CBPlayCollect, func(w *protocol.Writer) { w.VarInt(2).VarInt(999).VarInt(7) }),
	} {
		if _, err := c.handleInventoryPacket(p); err != nil {
			t.Fatalf("handleInventoryPacket: %v", err)
		}
	}
	if total, _ := c.PickupsSeen(); total != 4 {
		t.Errorf("PickupsSeen() = %d, want 4 — another player's pickup was counted", total)
	}
}

// The server restores the previously-selected slot on reconnect, so the client
// must adopt what it is told rather than assuming 0.
func TestHandleHeldItemSlot(t *testing.T) {
	c := newTestClient(t)
	p := packet(c.v.Packets.CBPlayHeldItemSlot, func(w *protocol.Writer) { w.VarInt(4) })
	if _, err := c.handleInventoryPacket(p); err != nil {
		t.Fatalf("handleInventoryPacket: %v", err)
	}
	if got := c.HeldSlot(); got != 4 {
		t.Errorf("HeldSlot() = %d, want 4", got)
	}

	// Out-of-range values from the wire must be ignored, not stored.
	bad := packet(c.v.Packets.CBPlayHeldItemSlot, func(w *protocol.Writer) { w.VarInt(99) })
	if _, err := c.handleInventoryPacket(bad); err != nil {
		t.Fatalf("handleInventoryPacket: %v", err)
	}
	if got := c.HeldSlot(); got != 4 {
		t.Errorf("HeldSlot() = %d after an out-of-range update, want it unchanged at 4", got)
	}
}
