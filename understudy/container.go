package understudy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/blocktopiaworld/understudy-client/internal/inventory"
	"github.com/blocktopiaworld/understudy-client/internal/nbt"
	"github.com/blocktopiaworld/understudy-client/protocol"
)

// Container slot layouts.
//
// Slot numbering is per window type and there is no way to derive it from the
// packets — the server sends a type ID and a flat array. Getting it wrong is
// silent: a click lands on a different slot and the recipe simply does not
// craft, with no rejection anywhere. So the layouts a caller is likely to want
// are named here rather than left as literals at each call site.
const (
	// CraftingResultSlot is the output of a crafting table. Slots 1..9 are the
	// 3x3 grid, row-major from the top-left.
	CraftingResultSlot = 0
	// CraftingGridSlot is the first of the nine grid slots.
	CraftingGridSlot = 1
	// CraftingGridSize is how many slots the 3x3 grid covers.
	CraftingGridSize = 9

	// SmithingTemplateSlot, SmithingBaseSlot and SmithingAdditionSlot are the
	// three inputs of a smithing table; SmithingResultSlot is its output.
	SmithingTemplateSlot = 0
	SmithingBaseSlot     = 1
	SmithingAdditionSlot = 2
	SmithingResultSlot   = 3

	// StonecutterInputSlot and StonecutterResultSlot are a stonecutter's two
	// slots. The recipe is chosen with ClickContainerButton, not by clicking.
	StonecutterInputSlot  = 0
	StonecutterResultSlot = 1

	// MerchantResultSlot is where a villager's selected trade delivers.
	MerchantInputSlot1 = 0
	MerchantInputSlot2 = 1
	MerchantResultSlot = 2
)

// ErrNoContainer reports that an operation needed an open container window and
// there was not one.
//
// A sentinel because the alternative is worse than an error: a click with no
// window open is addressed to the player's own inventory, which the server
// accepts and applies somewhere unintended.
var ErrNoContainer = errors.New("understudy: no container window is open")

// ErrNotConnected reports that something tried to put a packet on a wire that
// is not there. Only reachable before Connect or after Close; it exists so
// those paths return an error rather than panicking on a nil connection.
var ErrNotConnected = errors.New("understudy: not connected")

// requireConn guards the write paths that a caller can reach without a session.
func (c *Client) requireConn() error {
	if c.conn == nil {
		return ErrNotConnected
	}
	return nil
}

// containerOpenTimeout bounds the wait for a window after using a block. The
// server opens it within a tick or two; anything longer means the block was
// not a container, or the interaction never landed.
const containerOpenTimeout = 5 * time.Second

// containerOpenRetry is how often the interaction is repeated while waiting.
//
// Three seconds was not enough for an entity that had only just spawned, and
// the extra time alone would not have helped: the server answers a single
// early interact with silence, so the ask has to be made again rather than
// waited on longer.
const containerOpenRetry = 1200 * time.Millisecond

// tradeResultTimeout bounds the wait for a trade's output. The server answers
// within a tick or two; anything longer means it did not accept the trade.
const tradeResultTimeout = 1500 * time.Millisecond

// ContainerOpen reports whether a block or entity UI is currently open.
func (c *Client) ContainerOpen() bool { return c.window.IsOpen() }

// ContainerID returns the server-assigned window ID, or inventory.NoWindow.
func (c *Client) ContainerID() int32 { return c.window.ID() }

// ContainerKind returns the window type ID the server reported.
func (c *Client) ContainerKind() int32 { return c.window.Kind() }

// ContainerTitle returns the window's title as plain text.
func (c *Client) ContainerTitle() string { return c.window.Title() }

// ContainerSlots returns the open window's contents.
//
// The array covers the container's own slots *and* the player's inventory
// appended after them, which is how the server addresses a click — so slot 9
// of a crafting table window is the player's first storage slot, not a tenth
// grid cell.
func (c *Client) ContainerSlots() []ItemStack { return c.window.Slots() }

// ContainerSlot returns one slot of the open window.
func (c *Client) ContainerSlot(slot int) (ItemStack, bool) { return c.window.Slot(slot) }

// ContainerSize returns how many slots the open window covers.
func (c *Client) ContainerSize() int { return c.window.Size() }

// ContainerTruncated reports whether the window's contents are incomplete
// because an item carried data components that could not be skipped.
func (c *Client) ContainerTruncated() bool { return c.window.Trimmed() }

// FindInContainer returns the slot holding an item in the open window.
func (c *Client) FindInContainer(name string) (ItemStack, bool) { return c.window.Find(name) }

// CountInContainer totals a named item across the open window.
func (c *Client) CountInContainer(name string) int32 { return c.window.Count(name) }

// OpenContainer right-clicks a block and waits for the server to open its UI.
//
// This is a real interaction, not a command: the block has to be in reach and
// the bot has to be able to see it, exactly as for placing. A block that is not
// a container simply never opens one, which is why this reports a timeout
// rather than waiting forever.
func (c *Client) OpenContainer(ctx context.Context, x, y, z, face int32) error {
	before := c.window.Sequence()
	if err := c.UseOnBlock(ctx, x, y, z, face); err != nil {
		return err
	}
	return c.awaitContainer(ctx, before, func() error {
		return c.UseOnBlock(ctx, x, y, z, face)
	})
}

// OpenContainerOnEntity right-clicks an entity and waits for its UI — a
// villager's trades, a horse's inventory.
func (c *Client) OpenContainerOnEntity(ctx context.Context, entityID int32) error {
	before := c.window.Sequence()
	if err := c.InteractEntity(entityID); err != nil {
		return err
	}
	return c.awaitContainer(ctx, before, func() error {
		return c.InteractEntity(entityID)
	})
}

// OpenContainerOnNearest right-clicks the closest entity of a type and waits
// for its UI.
func (c *Client) OpenContainerOnNearest(ctx context.Context, typeName string) (Entity, error) {
	before := c.window.Sequence()
	target, err := c.InteractNearest(typeName)
	if err != nil {
		return target, err
	}
	return target, c.awaitContainer(ctx, before, func() error {
		_, err := c.InteractNearest(typeName)
		return err
	})
}

// awaitContainer waits for a window newer than the one seen before the
// interaction, *and* for its contents to arrive.
//
// Comparing the sequence rather than "is one open" is what stops a window left
// open from an earlier step being mistaken for this one.
//
// Waiting for the contents matters just as much. open_window and window_items
// are separate packets, so a caller that acts the moment the window exists is
// reading an empty container — "no white_wool in the window to craft with"
// when the wool is sitting in slot 37, a beat later. Same shape as the
// teleport race: confirm the state you are about to act on rather than assume
// the packet that carries it has landed.
func (c *Client) awaitContainer(ctx context.Context, before int, again func() error) error {
	ticker := time.NewTicker(chunkPollInterval)
	defer ticker.Stop()
	deadline := time.After(containerOpenTimeout)
	retry := time.NewTicker(containerOpenRetry)
	defer retry.Stop()
	for {
		if c.window.Sequence() > before && c.window.IsOpen() && c.window.Size() > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-retry.C:
			// Ask again. An entity that has only just spawned is not ready to
			// trade for a second or so, and a single interact sent inside that
			// window is answered with nothing at all — which arrives here as
			// "the target may not have a UI", a confident and wrong diagnosis
			// of "not yet". A freshly summoned wandering trader failed to open
			// four times in six on the first ask and every time on the second.
			//
			// Same shape as waiting for a block to become targetable before
			// refusing to dig it: not ready is not the same as never.
			if again != nil {
				if err := again(); err != nil {
					return err
				}
			}
		case <-deadline:
			if c.window.Sequence() > before && c.window.IsOpen() {
				// The window opened but never sent its contents. Usable for a
				// button press, so this is worth reporting rather than failing.
				c.log.Warn("container opened but sent no contents",
					"window", c.window.ID(), "within", containerOpenTimeout)
				return nil
			}
			return fmt.Errorf(
				"understudy: no container opened within %v — the target may not have a UI",
				containerOpenTimeout)
		case <-ticker.C:
		}
	}
}

// CloseContainer shuts the open window.
//
// Worth doing rather than walking away: a crafting grid left dirty drops its
// contents on the floor when it eventually closes, and those drops then change
// any nearby pickup or hold count.
func (c *Client) CloseContainer() error {
	id := c.window.ID()
	if id == inventory.NoWindow {
		return nil
	}
	if c.v.Packets.SBPlayCloseWindow == protocol.Absent {
		return fmt.Errorf("understudy: %s has no close_window packet", c.v.Name)
	}
	// Mark it closed first: the server does not acknowledge, so waiting for a
	// reply that never comes would hang.
	c.window.Close()
	return c.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.SBPlayCloseWindow).VarInt(id).Bytes())
}

// ClickContainerSlot clicks a slot in the open window.
//
// button and mode are the raw protocol values — see the ClickMode constants in
// inventory.go. The common cases have their own verbs below.
func (c *Client) ClickContainerSlot(slot int, button int8, mode int32) error {
	if err := c.requireAlive("click"); err != nil {
		return err
	}
	id := c.window.ID()
	if id == inventory.NoWindow {
		return ErrNoContainer
	}
	if err := c.requireConn(); err != nil {
		return err
	}
	if c.v.Packets.SBPlayWindowClick == protocol.Absent {
		return fmt.Errorf("understudy: %s has no window_click packet", c.v.Name)
	}
	if size := c.window.Size(); size > 0 && (slot < 0 || slot >= size) {
		return fmt.Errorf("understudy: slot %d is outside the %d-slot window", slot, size)
	}
	w := protocol.NewWriter(c.v.Packets.SBPlayWindowClick).
		VarInt(id).
		VarInt(c.window.StateID()).
		I16(int16(slot)).
		I8(button).
		VarInt(mode).
		VarInt(0). // changedSlots: empty, let the server resync
		Bool(false)
	return c.conn.WritePacket(w.Bytes())
}

// TakeFromContainer shift-clicks a slot, moving its whole stack to the other
// half of the window.
//
// Shift-clicking rather than pick-up-then-put-down is deliberate: it is one
// packet, the server decides where the items land, and it is what actually
// empties a crafting result including every repeat the ingredients allow.
func (c *Client) TakeFromContainer(slot int) error {
	return c.ClickContainerSlot(slot, 0, ClickModeQuickMove)
}

// ClickContainerButton presses a numbered button in the open window.
//
// This is how a stonecutter or loom selects a recipe, and how an enchanting
// table picks a level — despite the packet being named enchant_item, it is the
// generic container-button message.
func (c *Client) ClickContainerButton(button int32) error {
	id := c.window.ID()
	if id == inventory.NoWindow {
		return ErrNoContainer
	}
	if c.v.Packets.SBPlayContainerButton == protocol.Absent {
		return fmt.Errorf("understudy: %s has no container-button packet", c.v.Name)
	}
	return c.conn.WritePacket(protocol.NewWriter(c.v.Packets.SBPlayContainerButton).
		VarInt(id).VarInt(button).Bytes())
}

// SelectTrade chooses a villager's trade by its index in the offer list.
//
// The offers are decoded, so a caller can pick by index or read Trades() to
// choose one. See TradeFor and TradeForItem to select by what a trade produces
// rather than by its position, which survives a villager whose offer order
// differs.
func (c *Client) SelectTrade(index int32) error {
	if !c.window.IsOpen() {
		return ErrNoContainer
	}
	if c.v.Packets.SBPlaySelectTrade == protocol.Absent {
		return fmt.Errorf("understudy: %s has no select_trade packet", c.v.Name)
	}
	if index < 0 {
		return fmt.Errorf("understudy: trade index %d is negative", index)
	}
	return c.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.SBPlaySelectTrade).VarInt(index).Bytes())
}

// CraftRecipe asks the server to lay out a recipe in the open crafting window.
//
// The server populates the grid from the player's inventory using its own
// recipe book, so the caller does not have to encode recipes or place
// ingredients slot by slot — which is both fewer packets and one less thing to
// get wrong per recipe. With all true it repeats until the ingredients run
// out, which is what "craft fifty of these" wants.
//
// The result still has to be collected: see TakeFromContainer with
// CraftingResultSlot.
func (c *Client) CraftRecipe(recipeID int32, all bool) error {
	id := c.window.ID()
	if id == inventory.NoWindow {
		return ErrNoContainer
	}
	if c.v.Packets.SBPlayCraftRecipeRequest == protocol.Absent {
		return fmt.Errorf("understudy: %s has no craft_recipe_request packet", c.v.Name)
	}
	return c.conn.WritePacket(protocol.NewWriter(c.v.Packets.SBPlayCraftRecipeRequest).
		VarInt(id).VarInt(recipeID).Bool(all).Bytes())
}

// handleContainerPacket decodes the window lifecycle packets. Returns false if
// the packet was not one of them.
func (c *Client) handleContainerPacket(p protocol.Packet) (bool, error) {
	switch p.ID {
	case c.v.Packets.CBPlayOpenWindow:
		r := p.Reader()
		id := r.VarInt()
		kind := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		// The title is a nameless NBT text component. It is only ever shown to
		// a human, so a best-effort scrape is worth more than a decoder for
		// every component shape — and a failure here must not lose the window.
		title := nbt.ReadableText(r.Remaining())
		c.window.Open(id, kind, title)
		c.mu.Lock()
		c.trades = nil // a new window's offers have not arrived yet
		c.mu.Unlock()
		c.log.Info("container opened", "window", id, "type", kind, "title", title)
		return true, nil

	case c.v.Packets.CBPlayRecipeBookAdd:
		return true, c.handleRecipeBook(p)

	case c.v.Packets.CBPlayTradeList:
		return true, c.handleTradeList(p)

	case c.v.Packets.CBPlayCloseWindow:
		r := p.Reader()
		id := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		if c.window.Matches(id) {
			c.window.Close()
			c.log.Debug("container closed by the server", "window", id)
		}
		return true, nil
	}
	return false, nil
}

// CraftInGrid lays a recipe out in the open crafting table and takes the result.
//
// layout maps grid slot to item name, with CraftingGridSlot..CraftingGridSlot+8
// as the 3x3 read row-major from the top-left — so a banner is six wool in
// slots 1..6 and a stick in slot 7.
//
// This is CraftIn2x2's approach applied to a real table, and it is deliberately
// preferred over CraftRecipe for anything a caller writes by hand: a recipe
// request needs a numeric recipe ID from the server's registry, which nothing
// here decodes, whereas a layout is something a person can write down and read
// back. CraftRecipe remains the better option when the ID is known, because the
// server then repeats the craft for as long as the ingredients last.
//
// repeat crafts the recipe more than once, re-laying the grid each time —
// "craft fifty banners" without needing a recipe ID.
func (c *Client) CraftInGrid(ctx context.Context, layout map[int]string, repeat int) (ItemStack, error) {
	if !c.window.IsOpen() {
		return ItemStack{}, ErrNoContainer
	}
	if repeat < 1 {
		repeat = 1
	}
	for slot := range layout {
		if slot < CraftingGridSlot || slot >= CraftingGridSlot+CraftingGridSize {
			return ItemStack{}, fmt.Errorf(
				"understudy: grid slot %d is outside the 3x3 (%d..%d)",
				slot, CraftingGridSlot, CraftingGridSlot+CraftingGridSize-1)
		}
	}

	var last ItemStack
	for i := range repeat {
		result, err := c.craftOnceInGrid(ctx, layout)
		if err != nil {
			return last, fmt.Errorf("understudy: craft %d of %d: %w", i+1, repeat, err)
		}
		last = result
	}
	return last, nil
}

// craftOnceInGrid performs a single pass: place every ingredient, confirm the
// grid took them, then quick-move the result out.
func (c *Client) craftOnceInGrid(ctx context.Context, layout map[int]string) (ItemStack, error) {
	pause := func() error { return wait(ctx, clickDelay) }

	slots := make([]int, 0, len(layout))
	for slot := range layout {
		slots = append(slots, slot)
	}
	slices.Sort(slots) // deterministic order, so a failure reproduces

	for _, gridSlot := range slots {
		name := layout[gridSlot]
		// Search only the player's rows, which sit after the grid. Searching
		// the whole window finds the ingredient just placed into the grid and
		// the loop picks its own work back up.
		src, ok := c.window.FindFrom(name, CraftingGridSlot+CraftingGridSize)
		if !ok {
			return ItemStack{}, fmt.Errorf(
				"understudy: no %q in the player's rows of the window to craft with", name)
		}
		// Pick the stack up, right-click one into the grid, put the rest back —
		// so a recipe needing one item does not consume the whole stack.
		for _, click := range []struct {
			slot   int
			button int8
		}{{src.Slot, 0}, {gridSlot, 1}, {src.Slot, 0}} {
			if err := c.ClickContainerSlot(click.slot, click.button, ClickModeNormal); err != nil {
				return ItemStack{}, err
			}
			if err := pause(); err != nil {
				return ItemStack{}, err
			}
		}
	}

	// Let the server resolve the grid before reaching for the output, which is
	// empty until it has.
	if err := pause(); err != nil {
		return ItemStack{}, err
	}
	for _, gridSlot := range slots {
		if item, ok := c.window.Slot(gridSlot); !ok || item.Empty() {
			return ItemStack{}, fmt.Errorf(
				"understudy: grid slot %d never received %q — the craft would have made "+
					"whatever the partial layout resolves to", gridSlot, layout[gridSlot])
		}
	}

	result, ok := c.window.Slot(CraftingResultSlot)
	if !ok || result.Empty() {
		return ItemStack{}, fmt.Errorf(
			"understudy: nothing in the crafting output — %v is not a valid recipe, "+
				"or the ingredients did not reach the grid", layout)
	}
	if err := c.TakeFromContainer(CraftingResultSlot); err != nil {
		return result, err
	}
	return result, pause()
}

// Trade selects a villager's trade and confirms it actually produced something.
//
// Worth doing rather than firing SelectTrade and hoping. A trade that is locked
// out — a villager sells the same offer only so many times before it needs to
// restock, and a wandering trader's stock runs out for good — is *refused
// silently*: the packet is accepted, no result appears, and nothing anywhere
// says why. So is a trade the player cannot afford. Both look identical to
// success from the sending side.
//
// The offer list itself is not decoded (its input items carry a component
// matcher this client cannot skip), so "is this trade available" cannot be read
// ahead of time. What can be observed is the result slot, which is the server's
// own answer — so this selects, waits, and reports what actually landed.
//
// This is half of a trade, and most callers want TradeAndTake instead.
// Selecting an offer makes the result stack appear, which reads as success and
// is not one: the server counts nothing, grants no traded_with_villager, and
// emits no trade event until the result is *taken*. A caller that stops here
// leaves the villager holding the goods.
//
// So: it returns the result stack, and collecting it with
// TakeFromContainer(MerchantResultSlot) is a required second step. Confirm that
// take by the player's stock going up, not by the result slot changing —
// vanilla re-offers the same trade immediately and refills the slot with an
// identical stack, so watching the slot answers "no" on a trade that worked.
// TradeAndTake does all of that.
func (c *Client) Trade(ctx context.Context, index int32) (ItemStack, error) {
	if !c.window.IsOpen() {
		return ItemStack{}, ErrNoContainer
	}
	// Check the offer first now that the list is decoded. A spent trade is
	// accepted and silently does nothing, so without this the only symptom is
	// a timeout — which is the same symptom as a dozen other things.
	for _, offer := range c.Trades() {
		if offer.Index != index {
			continue
		}
		if !offer.Available() {
			return ItemStack{}, fmt.Errorf(
				"understudy: trade %d (%s) is locked out — %d of %d uses spent, so the "+
					"villager must restock before it will trade again",
				index, offer, offer.Uses, offer.MaxUses)
		}
		break
	}
	before, _ := c.window.Slot(MerchantResultSlot)
	if err := c.SelectTrade(index); err != nil {
		return ItemStack{}, err
	}
	// The result appears a tick or two later. Poll rather than sleep a guess:
	// the answer is either there or the trade did not happen.
	deadline := time.After(tradeResultTimeout)
	ticker := time.NewTicker(chunkPollInterval)
	defer ticker.Stop()
	for {
		result, ok := c.window.Slot(MerchantResultSlot)
		if ok && !result.Empty() && result != before {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return ItemStack{}, ctx.Err()
		case <-deadline:
			return ItemStack{}, fmt.Errorf(
				"understudy: trade %d produced nothing within %v — it is locked out "+
					"until the villager restocks, out of stock for good (a wandering "+
					"trader), or the inputs are missing", index, tradeResultTimeout)
		case <-ticker.C:
		}
	}
}

// merchantPlayerRows is the first slot of the player's own inventory in a
// merchant window; slots 0..2 are the offer's two inputs and its result.
const merchantPlayerRows = 3

// awaitStockRose waits for the player's holding of an item to increase, which
// is how a completed trade is confirmed: the server's own count, rather than a
// guess about which packet means success.
func (c *Client) awaitStockRose(ctx context.Context, name string, before int32) bool {
	deadline := time.After(tradeResultTimeout)
	ticker := time.NewTicker(chunkPollInterval)
	defer ticker.Stop()
	for {
		if c.window.CountFrom(name, merchantPlayerRows) > before {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			return false
		case <-ticker.C:
		}
	}
}

// TradeAndTake performs a trade and collects the result, repeating while the
// villager keeps supplying one. It returns how many trades actually completed.
//
// Counting is done from the stock gained, not from the number of clicks.
// Shift-clicking a merchant result is a *batch* in vanilla: one click repeats
// the trade for as many uses as the villager has left and the player can
// afford. So a single take can be four trades, and counting clicks reports one
// — which is how this first read "traded 1" while the server had handed over
// twenty-four bread.
//
// It stops when a trade produces nothing rather than treating that as a
// failure, which is what tells a test the difference between "locked out after
// four" and "never worked at all".
func (c *Client) TradeAndTake(ctx context.Context, index int32, times int) (done int, err error) {
	if times < 1 {
		times = 1
	}
	for range times {
		result, err := c.Trade(ctx, index)
		if err != nil {
			if done > 0 {
				// Ran out partway: the trades that did happen are the answer.
				c.log.Debug("trading stopped early", "completed", done, "err", err)
				return done, nil
			}
			return 0, err
		}
		// Count what the player holds *before* taking, so the take can be
		// confirmed by the stock going up rather than by the result slot
		// changing. Vanilla re-offers the same trade immediately, refilling
		// the result slot with an identical stack — so "did slot 2 change?"
		// answers no even when the trade went through.
		before := c.window.CountFrom(result.Name, merchantPlayerRows)
		if err := c.TakeFromContainer(MerchantResultSlot); err != nil {
			return done, err
		}
		if !c.awaitStockRose(ctx, result.Name, before) {
			// The take did not land. Report what did happen rather than
			// claiming a trade that did not.
			c.log.Debug("trade result was not collected", "completed", done, "item", result.Name)
			return done, nil
		}
		// Convert the stock gained into trades. A batched take covers several
		// at once, so this can jump by more than one.
		gained := c.window.CountFrom(result.Name, merchantPlayerRows) - before
		per := result.Count
		if per < 1 {
			per = 1
		}
		completed := int(gained / per)
		if completed < 1 {
			completed = 1 // it rose, so at least one went through
		}
		done += completed
		if done >= times {
			return done, nil
		}
	}
	return done, nil
}

// dumpTradeList writes the raw trade_list payload out when
// UNDERSTUDY_DUMP_TRADES names a file.
//
// The offer list contains an "ExactComponentMatcher" that minecraft-data does
// not describe, so the only honest way to learn its encoding is to read bytes
// whose meaning is already known — summon a villager with a chosen trade, and
// see which bytes carry it. Same reasoning as the chunk dump: guessing a length
// desynchronises the rest of the packet and surfaces far from the mistake.
var dumpTradesOnce sync.Once

func dumpTradeList(payload []byte) {
	path := os.Getenv("UNDERSTUDY_DUMP_TRADES")
	if path == "" {
		return
	}
	dumpTradesOnce.Do(func() {
		// Operator-supplied debug path; no untrusted input here.
		_ = os.WriteFile(path, payload, 0o644) //nolint:gosec // G703
	})
}
