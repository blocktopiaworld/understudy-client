package understudy

import "testing"

// Available has to check both conditions because they are not the same moment:
// the server sets Disabled when it decides an offer is spent, and Uses reaching
// MaxUses is the arithmetic that gets it there. A trade that is spent by either
// measure is accepted by the server and silently does nothing.
func TestTradeOfferAvailability(t *testing.T) {
	for _, tc := range []struct {
		name  string
		offer TradeOffer
		want  bool
	}{
		{"fresh", TradeOffer{Uses: 0, MaxUses: 4}, true},
		{"partly used", TradeOffer{Uses: 3, MaxUses: 4}, true},
		{"spent", TradeOffer{Uses: 4, MaxUses: 4}, false},
		{"over-spent", TradeOffer{Uses: 9, MaxUses: 4}, false},
		{"flagged by the server", TradeOffer{Disabled: true, Uses: 0, MaxUses: 4}, false},
		// A wandering trader's one-shot offers report no maximum; treat that as
		// unlimited rather than as instantly spent.
		{"no stated maximum", TradeOffer{Uses: 7, MaxUses: 0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.offer.Available(); got != tc.want {
				t.Errorf("Available() = %v, want %v for %+v", got, tc.want, tc.offer)
			}
		})
	}
}

// The description ends up in an error a person reads at 3am, so it should say
// what the trade is and whether it can be taken.
func TestTradeOfferString(t *testing.T) {
	single := TradeOffer{
		Index:  0,
		Input:  ItemStack{Name: "minecraft:emerald", Count: 1},
		Output: ItemStack{Name: "minecraft:bread", Count: 6},
		Uses:   2, MaxUses: 4,
	}
	if got := single.String(); !contains(got, "1 emerald") || !contains(got, "6 bread") ||
		!contains(got, "2/4") {
		t.Errorf("String() = %q, want the inputs, outputs and uses", got)
	}
	if contains(single.String(), "locked out") {
		t.Error("an available trade should not read as locked out")
	}

	twoInput := TradeOffer{
		Index:  1,
		Input:  ItemStack{Name: "minecraft:emerald", Count: 3},
		Input2: ItemStack{Name: "minecraft:wheat", Count: 12},
		Output: ItemStack{Name: "minecraft:golden_carrot", Count: 2},
		Uses:   5, MaxUses: 5,
	}
	got := twoInput.String()
	if !contains(got, "12 wheat") {
		t.Errorf("String() = %q, want the second input named", got)
	}
	if !contains(got, "locked out") {
		t.Errorf("String() = %q, want a spent trade to say so", got)
	}
}

func TestTradeForPicksAnAvailableOffer(t *testing.T) {
	c := newTestClient(t)
	c.trades = []TradeOffer{
		{Index: 0, Output: ItemStack{Name: "minecraft:bread", Count: 6}, Uses: 4, MaxUses: 4},
		{Index: 1, Output: ItemStack{Name: "minecraft:bread", Count: 6}, Uses: 0, MaxUses: 4},
		{Index: 2, Output: ItemStack{Name: "minecraft:emerald", Count: 1}, Uses: 0, MaxUses: 9},
	}

	// The first bread offer is spent, so it must skip to the second rather than
	// selecting an index that will silently do nothing.
	offer, ok := c.TradeFor("bread")
	if !ok || offer.Index != 1 {
		t.Errorf("TradeFor(bread) = %+v, %v; want the available offer at index 1", offer, ok)
	}
	// Fuzzy naming works the same way as it does everywhere else.
	if _, ok := c.TradeFor("minecraft:emerald"); !ok {
		t.Error("TradeFor should match a fully namespaced name")
	}
	if _, ok := c.TradeFor("diamond_block"); ok {
		t.Error("TradeFor matched an item nothing offers")
	}
}

// "There is no such trade" and "the trade exists but is spent" need different
// fixes, so they must not collapse into one message.
func TestTradesAreACopy(t *testing.T) {
	c := newTestClient(t)
	c.trades = []TradeOffer{{Index: 0, Uses: 1}}

	got := c.Trades()
	got[0].Uses = 99
	if again := c.Trades(); again[0].Uses != 1 {
		t.Error("Trades() handed out the internal slice — a caller mutated the offers")
	}
}
