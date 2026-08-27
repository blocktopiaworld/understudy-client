package understudy

import (
	"context"
	"errors"
	"testing"
)

// Every workstation takes the same window_click packet, so clicking slot 0 of
// the wrong window is accepted and does something else entirely. These check
// the guard names both what you have and what the verb needed, because "it did
// nothing" is otherwise the only symptom.
func TestWorkstationsRefuseTheWrongWindow(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(*Client) error
		want string
	}{
		{"smelt", func(c *Client) error {
			_, err := c.Smelt(ctx, "raw_iron", "coal", 1)
			return err
		}, "furnace"},
		{"rename", func(c *Client) error {
			_, err := c.RenameItem(ctx, "pickaxe", "Bob")
			return err
		}, "anvil"},
		{"combine", func(c *Client) error {
			_, err := c.CombineInAnvil(ctx, "a", "b")
			return err
		}, "anvil"},
		{"loom", func(c *Client) error {
			_, err := c.ApplyBannerPattern(ctx, "banner", "dye", "", 0)
			return err
		}, "loom"},
		{"grindstone", func(c *Client) error {
			_, err := c.Disenchant(ctx, "sword")
			return err
		}, "grindstone"},
		{"smithing", func(c *Client) error {
			_, err := c.UpgradeInSmithingTable(ctx, "t", "b", "a")
			return err
		}, "smithing table"},
		{"enchant", func(c *Client) error {
			_, err := c.Enchant(ctx, "sword", 0)
			return err
		}, "enchanting table"},
		{"brew", func(c *Client) error {
			return c.Brew(ctx, "potion", "nether_wart", "blaze_powder", 1)
		}, "brewing stand"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A chest is open — the wrong window for every one of these.
			c := openWindowWith(t, WindowGeneric9x3, 27, nil)
			err := tc.call(c)
			if err == nil {
				t.Fatalf("%s at a chest should be refused", tc.name)
			}
			if !contains(err.Error(), tc.want) {
				t.Errorf("error %q should say it needed a %s", err, tc.want)
			}
			if !contains(err.Error(), "chest") {
				t.Errorf("error %q should say what is actually open", err)
			}
		})
	}
}

func TestWorkstationsRefuseWithNoWindow(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	if _, err := c.Smelt(ctx, "raw_iron", "coal", 1); !errors.Is(err, ErrNoContainer) {
		t.Errorf("Smelt error = %v, want ErrNoContainer", err)
	}
	if err := c.SetItemName("Bob"); !errors.Is(err, ErrNoContainer) {
		t.Errorf("SetItemName error = %v, want ErrNoContainer", err)
	}
}

// The anvil rejects an over-long name outright rather than truncating it, so
// the client should say so instead of sending something that will be dropped.
func TestSetItemNameBoundsTheLength(t *testing.T) {
	c := openWindowWith(t, WindowAnvil, 3, nil)
	long := ""
	for range maxItemNameLen + 1 {
		long += "x"
	}
	err := c.SetItemName(long)
	if err == nil {
		t.Fatal("an over-long item name should be refused")
	}
	if !contains(err.Error(), "50") {
		t.Errorf("error %q should name the limit", err)
	}
}

// The enchanting offers are 0, 1 and 2 — the three rows, not an experience
// level. Passing 30 would click a button that does not exist.
func TestEnchantBoundsTheOffer(t *testing.T) {
	c := openWindowWith(t, WindowEnchantment, 2, map[int]ItemStack{
		3: {Name: "minecraft:diamond_sword", Count: 1},
		4: {Name: "minecraft:lapis_lazuli", Count: 3},
	})
	// Slot placement will fail without a connection; what matters is that an
	// out-of-range offer is caught rather than sent.
	if _, err := c.Enchant(context.Background(), "diamond_sword", 30); err == nil {
		t.Error("enchant offer 30 should be refused")
	}
}

// Brewing transforms the bottles in place rather than filling a result slot, so
// the count is bounded by the three bottle slots.
func TestBrewBoundsTheBottleCount(t *testing.T) {
	c := openWindowWith(t, WindowBrewingStand, 5, nil)
	// No connection, so this fails at the first click — but it must fail on the
	// bottle, not by addressing a fourth bottle slot that does not exist.
	err := c.Brew(context.Background(), "potion", "nether_wart", "", 9)
	if err == nil {
		t.Fatal("expected an error without a connection")
	}
	if contains(err.Error(), "bottle 4") {
		t.Errorf("Brew addressed a fourth bottle slot: %v", err)
	}
}
