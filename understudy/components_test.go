package understudy

import (
	"testing"

	"github.com/blocktopia/understudy-client/protocol"
)

// component builds a one-item window_items payload carrying a single component,
// which is the shape every case below needs.
func componentStack(t *testing.T, itemID int32, kind int32, payload func(*protocol.Writer)) ItemStack {
	t.Helper()
	c := newTestClient(t)
	w := protocol.NewWriter(c.v.Packets.CBPlayWindowItems).
		VarInt(PlayerWindowID).VarInt(1).VarInt(1).
		VarInt(1).VarInt(itemID). // one item
		VarInt(1).VarInt(0).      // one component added, none removed
		VarInt(kind)
	payload(w)
	p := protocol.Packet{ID: c.v.Packets.CBPlayWindowItems, Data: w.Bytes()[1:]}
	if _, err := c.handleInventoryPacket(p); err != nil {
		t.Fatalf("handleInventoryPacket: %v", err)
	}
	items := c.Inventory()
	if len(items) == 0 {
		t.Fatalf("component %d made the item undecodable", kind)
	}
	return items[0]
}

// Every one of these is an item a test will hand a bot within minutes. Before
// they were handled, any of them stopped the window scan — and the slots after
// it read as empty rather than unknown, so a bot went blind to its own
// inventory with nothing reported.
func TestComponentsThatTestsActuallyMeet(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    int32
		payload func(*protocol.Writer)
	}{
		{"damage, one byte", componentDamage, func(w *protocol.Writer) { w.VarInt(37) }},
		{"damage, two bytes", componentDamage, func(w *protocol.Writer) { w.VarInt(1000) }},
		{"one enchantment", componentEnchantments, func(w *protocol.Writer) {
			w.VarInt(1).VarInt(33).VarInt(3)
		}},
		{"three enchantments", componentEnchantments, func(w *protocol.Writer) {
			w.VarInt(3).VarInt(23).VarInt(1).VarInt(40).VarInt(2).VarInt(33).VarInt(5)
		}},
		{"stored enchantments", componentStoredEnchantments, func(w *protocol.Writer) {
			w.VarInt(1).VarInt(13).VarInt(2)
		}},
		{"stew effects", componentStewEffects, func(w *protocol.Writer) {
			w.VarInt(1).VarInt(22).VarInt(7)
		}},
		{"map id", componentMapID, func(w *protocol.Writer) { w.VarInt(7) }},
		{"custom name", componentCustomName, func(w *protocol.Writer) {
			nbtString(w, `"Rock"`)
		}},
		{"water bottle", componentPotionContents, func(w *protocol.Writer) {
			w.Bool(true).VarInt(0).Bool(false).VarInt(0).VarInt(0)
		}},
		{"coloured potion", componentPotionContents, func(w *protocol.Writer) {
			w.Bool(true).VarInt(0).Bool(true).I32(0x00ff0000).VarInt(0).VarInt(0)
		}},

		// The plugin-shaped ones. A server plugin's items routinely carry
		// custom_data and lore, and repair_cost turns up on anything that has
		// been through an anvil — including this client's own rename verb.
		{"repair cost, one byte", componentRepairCost, func(w *protocol.Writer) { w.VarInt(3) }},
		{"repair cost, two bytes", componentRepairCost, func(w *protocol.Writer) { w.VarInt(300) }},
		{"max stack size", componentMaxStackSize, func(w *protocol.Writer) { w.VarInt(17) }},
		{"max damage", componentMaxDamage, func(w *protocol.Writer) { w.VarInt(999) }},
		{"rarity", componentRarity, func(w *protocol.Writer) { w.VarInt(3) }},
		{"unbreakable, no payload", componentUnbreakable, func(w *protocol.Writer) {}},
		{"dyed colour", componentDyedColor, func(w *protocol.Writer) { w.I32(10511680) }},
		{"custom data", componentCustomData, func(w *protocol.Writer) {
			// {a: 1} as a nameless compound.
			w.U8(10).U8(3).U8(0).U8(1).U8('a').I32(1).U8(0)
		}},
		{"one line of lore", componentLore, func(w *protocol.Writer) {
			w.VarInt(1)
			nbtString(w, `"one"`)
		}},
		{"three lines of lore", componentLore, func(w *protocol.Writer) {
			w.VarInt(3)
			for _, line := range []string{`"one"`, `"two"`, `"three"`} {
				nbtString(w, line)
			}
		}},
		{"item name", componentItemName, func(w *protocol.Writer) { nbtString(w, `"Named"`) }},
		{"custom model data", componentCustomModelData, func(w *protocol.Writer) {
			w.VarInt(1).F32(1.0). // one float
						VarInt(0). // no flags
						VarInt(0). // no strings
						VarInt(0)  // no colours
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := componentStack(t, 1, tc.kind, tc.payload)
			if item.Count != 1 {
				t.Errorf("item = %+v, want a single item decoded", item)
			}
		})
	}
}

// nbtString writes a nameless NBT string, which is how every text component
// in an item's data arrives.
func nbtString(w *protocol.Writer, s string) {
	w.U8(8).U8(byte(len(s) >> 8)).U8(byte(len(s)))
	for _, b := range []byte(s) {
		w.U8(b)
	}
}

// The potion id is the only thing separating one potion from another, since
// they all share a name. Brew depends on it.
func TestPotionIdentityIsKept(t *testing.T) {
	item := componentStack(t, 1, componentPotionContents, func(w *protocol.Writer) {
		w.Bool(true).VarInt(5).Bool(false).VarInt(0).VarInt(0)
	})
	if item.Potion != 5 {
		t.Errorf("Potion = %d, want 5", item.Potion)
	}

	// An item with no potion contents must not read as potion 0 — water *is*
	// potion 0, so the two have to be distinguishable.
	plain := componentStack(t, 1, componentDamage, func(w *protocol.Writer) { w.VarInt(1) })
	if plain.Potion == 0 {
		t.Error("an item with no potion contents reads as potion 0, which is water")
	}
}

// An unknown component still stops, and says which one — the alternative is
// guessing a length and filling the rest of the window with nonsense.
func TestUnknownComponentIsReportedByNumber(t *testing.T) {
	c := newTestClient(t)
	w := protocol.NewWriter(c.v.Packets.CBPlayWindowItems).
		VarInt(PlayerWindowID).VarInt(1).VarInt(1).
		VarInt(1).VarInt(1).VarInt(1).VarInt(0).
		VarInt(9999). // no such component
		VarInt(0)
	p := protocol.Packet{ID: c.v.Packets.CBPlayWindowItems, Data: w.Bytes()[1:]}
	if _, err := c.handleInventoryPacket(p); err != nil {
		t.Fatalf("handleInventoryPacket: %v", err)
	}
	if !c.InventoryTruncated() {
		t.Error("an unreadable component should mark the inventory truncated")
	}
}
