package understudy

import "testing"

// The layout rule is the only thing the client assumes about a window's shape:
// [the container's own slots][the player's 36]. Everything that works without a
// special case — double chests, copper chests, chest minecarts — works because
// of it, so it is worth pinning.
//
// The sizes here were read off a live 26.1.2 server; see windows.go.
func TestContainerOwnSlotsFollowsTheLayoutRule(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int
		wantOwn int
	}{
		{"single chest", 63, 27},
		{"double chest", 90, 54},
		{"hopper", 41, 5},
		{"furnace", 39, 3},
		{"crafting table", 46, 10},
		{"smithing table", 40, 4},
		{"stonecutter", 38, 2},
		{"brewing stand", 41, 5},
		{"dispenser", 45, 9},
		{"beacon", 37, 1},
		{"lectern, which has no slots at all", 0, 0},
		// A window smaller than the player's own rows cannot be right, but it
		// must not produce a negative count either.
		{"impossibly small", 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			c.window.Open(1, 0, tc.name)
			slots := make([]ItemStack, tc.size)
			for i := range slots {
				slots[i] = ItemStack{Slot: i}
			}
			c.window.ReplaceAll(slots, len(slots), false)

			if got := c.ContainerOwnSlots(); got != tc.wantOwn {
				t.Errorf("ContainerOwnSlots() = %d for a %d-slot window, want %d",
					got, tc.size, tc.wantOwn)
			}
			// The player's rows begin exactly where the container's end.
			if got := c.PlayerSlotsStart(); got != tc.wantOwn {
				t.Errorf("PlayerSlotsStart() = %d, want %d", got, tc.wantOwn)
			}
		})
	}
}

// The type is for reporting, not for branching — but an unnamed one should
// still say something useful rather than a bare number with no context.
func TestWindowTypeNames(t *testing.T) {
	for _, tc := range []struct {
		w    WindowType
		want string
	}{
		{WindowGeneric9x3, "chest"},
		{WindowFurnace, "furnace"},
		{WindowCrafting, "crafting table"},
		{WindowSmithing, "smithing table"},
		{WindowMerchant, "merchant"},
		{WindowLectern, "lectern"},
		{WindowType(99), "window type 99"},
		{WindowType(-3), "window type -3"},
	} {
		if got := tc.w.String(); got != tc.want {
			t.Errorf("WindowType(%d).String() = %q, want %q", tc.w, got, tc.want)
		}
	}
}

func TestContainerTypeReportsWhatWasOpened(t *testing.T) {
	c := newTestClient(t)
	c.window.Open(3, int32(WindowBlastFurnace), "")
	if got := c.ContainerType(); got != WindowBlastFurnace {
		t.Errorf("ContainerType() = %v, want a blast furnace", got)
	}
	if got := c.ContainerType().String(); got != "blast furnace" {
		t.Errorf("name = %q, want %q", got, "blast furnace")
	}
}

// Clicking slot 0 of the wrong window is accepted by the server and does
// something else entirely, so the mismatch has to be an error with a name in it.
func TestRequireWindowNamesBothSides(t *testing.T) {
	c := newTestClient(t)
	c.window.Open(1, int32(WindowGeneric9x3), "Chest")

	err := c.requireWindow(WindowFurnace, WindowBlastFurnace, WindowSmoker)
	if err == nil {
		t.Fatal("smelting at a chest should be refused")
	}
	for _, want := range []string{"chest", "furnace", "blast furnace", "smoker"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if err := c.requireWindow(WindowGeneric9x3); err != nil {
		t.Errorf("a chest should satisfy requireWindow(chest): %v", err)
	}
}

func TestRequireWindowNeedsAnOpenWindow(t *testing.T) {
	c := newTestClient(t)
	if err := c.requireWindow(WindowFurnace); err == nil {
		t.Error("requireWindow with nothing open = nil, want ErrNoContainer")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// The same failure from the client's side: a truncated decode must not change
// where the player's rows begin.
func TestOwnSlotsSurviveATruncatedDecode(t *testing.T) {
	c := newTestClient(t)
	c.window.Open(1, int32(WindowBrewingStand), "Brewing Stand")

	decoded := make([]ItemStack, 32) // stopped early on a potion's components
	for i := range decoded {
		decoded[i] = ItemStack{Slot: i}
	}
	c.window.ReplaceAll(decoded, 41, true)

	if got := c.ContainerOwnSlots(); got != 5 {
		t.Errorf("ContainerOwnSlots() = %d, want 5 — sizing from the decoded items "+
			"instead of the declared count made this 0, and every slot lookup "+
			"then searched from the wrong floor", got)
	}
	if got := c.PlayerSlotsStart(); got != 5 {
		t.Errorf("PlayerSlotsStart() = %d, want 5", got)
	}
	if !c.ContainerTruncated() {
		t.Error("ContainerTruncated() should surface that the contents are partial")
	}
}
