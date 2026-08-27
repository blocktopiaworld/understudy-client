package understudy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blocktopia/understudy-client/internal/inventory"
	"github.com/blocktopia/understudy-client/protocol"
)

// openWindowPacket builds a clientbound open_window: window id, type, then a
// nameless NBT text component for the title.
func openWindowPacket(v *protocol.Version, id, kind int32, title string) []byte {
	w := protocol.NewWriter(v.Packets.CBPlayOpenWindow).VarInt(id).VarInt(kind)
	out := w.Bytes()
	// TAG_String, nameless, holding the title — enough for ReadableText to find.
	out = append(out, 8, byte(len(title)>>8), byte(len(title)))
	return append(out, title...)
}

// windowItemsPacket builds a clientbound window_items for a window id.
func windowItemsPacket(v *protocol.Version, windowID, stateID int32, items []ItemStack) []byte {
	w := protocol.NewWriter(v.Packets.CBPlayWindowItems).
		VarInt(windowID).VarInt(stateID).VarInt(int32(len(items)))
	for _, it := range items {
		if it.Count <= 0 {
			w.VarInt(0)
			continue
		}
		w.VarInt(it.Count).VarInt(it.ID).VarInt(0).VarInt(0)
	}
	return w.Bytes()
}

// openContainer drives a session to the point where a window is open.
//
// It waits for the open *counter* to advance rather than for "a window is
// open", because with one already open the latter is true before the new
// packet has been read — so the next assertion sees the previous window's
// contents. That is the same trap awaitContainer avoids in the client, and it
// showed up here as a test that passed alone and failed under load.
func openContainer(t *testing.T, c *Client, s *fakeServer, id, kind int32) {
	t.Helper()
	before := c.window.Sequence()
	if err := s.conn.WritePacket(openWindowPacket(c.v, id, kind, "Crafting Table")); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the window to open", func() bool {
		return c.window.Sequence() > before && c.ContainerOpen()
	})
}

func TestOpenWindowIsTracked(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()

	if c.ContainerOpen() {
		t.Fatal("no container should be open before the server opens one")
	}
	openContainer(t, c, s, 7, 3)

	if got := c.ContainerID(); got != 7 {
		t.Errorf("ContainerID() = %d, want 7", got)
	}
	if got := c.ContainerKind(); got != 3 {
		t.Errorf("ContainerKind() = %d, want 3", got)
	}
	if got := c.ContainerTitle(); got != "Crafting Table" {
		t.Errorf("ContainerTitle() = %q, want the title from the packet", got)
	}
}

// A container's contents must land in the container, not the player's
// inventory. Before this, container windows were dropped entirely — the ID was
// in the packet table and nothing read it.
func TestContainerItemsDoNotLandInThePlayerInventory(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()
	openContainer(t, c, s, 7, 3)

	items := []ItemStack{
		{ID: 1, Count: 3}, // slot 0
		{ID: 2, Count: 5}, // slot 1
	}
	if err := s.conn.WritePacket(windowItemsPacket(c.v, 7, 99, items)); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the container contents", func() bool {
		return c.ContainerSize() == 2
	})

	if got := len(c.Inventory()); got != 0 {
		t.Errorf("the player inventory has %d items; the container's contents leaked into it", got)
	}
	slot, ok := c.ContainerSlot(1)
	if !ok || slot.Count != 5 {
		t.Errorf("ContainerSlot(1) = %+v, %v; want a count of 5", slot, ok)
	}
}

// Every click echoes the window's own state counter. Sending the player
// inventory's instead makes the server resync silently rather than act.
func TestContainerClickCarriesTheWindowAndState(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()
	openContainer(t, c, s, 7, 3)

	if err := s.conn.WritePacket(windowItemsPacket(c.v, 7, 4242,
		[]ItemStack{{ID: 1, Count: 1}, {ID: 1, Count: 1}})); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the contents", func() bool { return c.ContainerSize() == 2 })

	if err := c.TakeFromContainer(CraftingResultSlot); err != nil {
		t.Fatalf("TakeFromContainer: %v", err)
	}
	waitFor(t, 2*time.Second, "the click", func() bool {
		return s.countOf(c.v.Packets.SBPlayWindowClick) > 0
	})

	r := s.first(t, c.v.Packets.SBPlayWindowClick, "window_click").Reader()
	if got := r.VarInt(); got != 7 {
		t.Errorf("click went to window %d, want the container's 7", got)
	}
	if got := r.VarInt(); got != 4242 {
		t.Errorf("click carried state %d, want the container's 4242", got)
	}
	if got := r.I16(); got != CraftingResultSlot {
		t.Errorf("click addressed slot %d, want %d", got, CraftingResultSlot)
	}
	r.I8() // button
	if got := r.VarInt(); got != ClickModeQuickMove {
		t.Errorf("click mode = %d, want quick-move (%d)", got, ClickModeQuickMove)
	}
}

// With nothing open, a click would be addressed to the player's own inventory
// and silently applied somewhere unintended — so it has to refuse.
func TestContainerActionsRefuseWithNoWindow(t *testing.T) {
	c, _ := newSession(t)
	cancel := run(t, c)
	defer cancel()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"click", func() error { return c.ClickContainerSlot(0, 0, ClickModeNormal) }},
		{"take", func() error { return c.TakeFromContainer(0) }},
		{"button", func() error { return c.ClickContainerButton(0) }},
		{"craft", func() error { return c.CraftRecipe(0, true) }},
		{"trade", func() error { return c.SelectTrade(0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected a refusal with no container open")
			}
			if !errors.Is(err, ErrNoContainer) {
				t.Errorf("error = %v, want it to wrap ErrNoContainer", err)
			}
		})
	}
}

func TestCloseContainerSendsTheWindowID(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()
	openContainer(t, c, s, 12, 0)

	if err := c.CloseContainer(); err != nil {
		t.Fatalf("CloseContainer: %v", err)
	}
	waitFor(t, 2*time.Second, "the close packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayCloseWindow) > 0
	})
	if got := s.first(t, c.v.Packets.SBPlayCloseWindow, "close_window").Reader().VarInt(); got != 12 {
		t.Errorf("closed window %d, want 12", got)
	}
	if c.ContainerOpen() {
		t.Error("the container should be closed locally too")
	}
	if got := c.ContainerID(); got != inventory.NoWindow {
		t.Errorf("ContainerID() = %d after closing, want NoWindow", got)
	}
	// Closing again is a no-op rather than an error: a caller tidying up should
	// not have to track whether it already did.
	if err := c.CloseContainer(); err != nil {
		t.Errorf("closing an already-closed container: %v", err)
	}
}

// The server closes windows too — walking away from a chest, or a villager
// losing interest.
func TestServerCanCloseTheWindow(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()
	openContainer(t, c, s, 5, 0)

	if err := s.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.CBPlayCloseWindow).VarInt(5).Bytes()); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the window to close", func() bool { return !c.ContainerOpen() })
}

// This is the one that makes "craft 50 banners" work: the server lays the grid
// out from its own recipe book rather than the caller placing seven items
// fifty times.
func TestCraftRecipeAsksTheServerToLayOutTheGrid(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()
	openContainer(t, c, s, 8, 3)

	if err := c.CraftRecipe(1234, true); err != nil {
		t.Fatalf("CraftRecipe: %v", err)
	}
	waitFor(t, 2*time.Second, "the recipe request", func() bool {
		return s.countOf(c.v.Packets.SBPlayCraftRecipeRequest) > 0
	})
	r := s.first(t, c.v.Packets.SBPlayCraftRecipeRequest, "craft_recipe_request").Reader()
	if got := r.VarInt(); got != 8 {
		t.Errorf("recipe request went to window %d, want 8", got)
	}
	if got := r.VarInt(); got != 1234 {
		t.Errorf("recipe id = %d, want 1234", got)
	}
	if !r.Bool() {
		t.Error("makeAll should be true — that is what crafts the whole batch")
	}
}

func TestContainerButtonAndTrade(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()
	openContainer(t, c, s, 6, 0)

	if err := c.ClickContainerButton(2); err != nil {
		t.Fatalf("ClickContainerButton: %v", err)
	}
	waitFor(t, 2*time.Second, "the button", func() bool {
		return s.countOf(c.v.Packets.SBPlayContainerButton) > 0
	})
	r := s.first(t, c.v.Packets.SBPlayContainerButton, "container button").Reader()
	if got := r.VarInt(); got != 6 {
		t.Errorf("button went to window %d, want 6", got)
	}
	if got := r.VarInt(); got != 2 {
		t.Errorf("button = %d, want 2", got)
	}

	if err := c.SelectTrade(1); err != nil {
		t.Fatalf("SelectTrade: %v", err)
	}
	waitFor(t, 2*time.Second, "the trade selection", func() bool {
		return s.countOf(c.v.Packets.SBPlaySelectTrade) > 0
	})
	if got := s.first(t, c.v.Packets.SBPlaySelectTrade, "select_trade").Reader().VarInt(); got != 1 {
		t.Errorf("selected trade %d, want 1", got)
	}
	if err := c.SelectTrade(-1); err == nil {
		t.Error("a negative trade index should be refused")
	}
}

// A slot outside the window is a caller mistake worth naming, because the
// server's answer to one is silence.
func TestContainerClickRejectsOutOfRangeSlots(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()
	openContainer(t, c, s, 7, 3)

	if err := s.conn.WritePacket(windowItemsPacket(c.v, 7, 1,
		[]ItemStack{{ID: 1, Count: 1}})); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the contents", func() bool { return c.ContainerSize() == 1 })

	if err := c.ClickContainerSlot(5, 0, ClickModeNormal); err == nil {
		t.Error("clicking slot 5 of a 1-slot window should be refused")
	}
}

// Opening a second container must not leave the first one's contents readable
// as if they were the new window's.
func TestReopeningClearsTheOldContents(t *testing.T) {
	c, s := newSession(t)
	cancel := run(t, c)
	defer cancel()

	openContainer(t, c, s, 1, 0)
	if err := s.conn.WritePacket(windowItemsPacket(c.v, 1, 1,
		[]ItemStack{{ID: 1, Count: 7}})); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	waitFor(t, 2*time.Second, "the first contents", func() bool { return c.ContainerSize() == 1 })

	openContainer(t, c, s, 2, 0)
	if got := c.ContainerSize(); got != 0 {
		t.Errorf("ContainerSize() = %d after reopening, want 0", got)
	}
}

func TestAwaitContainerTimesOutOnANonContainer(t *testing.T) {
	c, _ := newSession(t)
	cancel := run(t, c)
	defer cancel()

	ctx, stop := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer stop()
	// Nothing will open a window, so this must give up rather than hang.
	if err := c.awaitContainer(ctx, c.window.Sequence()); err == nil {
		t.Error("awaitContainer with nothing opening = nil error, want a timeout")
	}
}
