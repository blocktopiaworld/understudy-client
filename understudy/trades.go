package understudy

import (
	"context"
	"fmt"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// TradeOffer is one entry in a villager's or wandering trader's offer list.
type TradeOffer struct {
	// Index is the offer's position in the list, which is what SelectTrade takes.
	Index int32

	// Input and Input2 are what the trade costs. Input2 is empty for the many
	// trades that take a single item.
	Input  ItemStack
	Input2 ItemStack
	// Output is what the trade produces.
	Output ItemStack

	// Disabled is the server's own "this offer is not available right now",
	// which is what a villager sets once it has run out of uses and needs to
	// restock. A wandering trader sets it and never clears it.
	Disabled bool

	// Uses and MaxUses are how many times this offer has been taken and how
	// many it allows. Uses >= MaxUses is the same condition as Disabled,
	// arriving a moment earlier.
	Uses    int32
	MaxUses int32

	// XP is the villager experience the trade grants; Demand and SpecialPrice
	// feed the price adjustment a villager makes for popular trades.
	XP           int32
	SpecialPrice int32
	Demand       int32
	PriceMult    float32
}

// Available reports whether the offer can be taken right now.
//
// Both conditions are checked because they are not quite the same moment: the
// server sets Disabled when it decides the offer is spent, and Uses reaching
// MaxUses is the arithmetic that leads there. Taking a spent trade is accepted
// and silently does nothing, which is the failure this exists to prevent.
func (t TradeOffer) Available() bool {
	return !t.Disabled && (t.MaxUses <= 0 || t.Uses < t.MaxUses)
}

// String describes the offer the way a person would read it.
func (t TradeOffer) String() string {
	s := fmt.Sprintf("%d: %d %s", t.Index, t.Input.Count, protocol.BareName(t.Input.Name))
	if !t.Input2.Empty() {
		s += fmt.Sprintf(" + %d %s", t.Input2.Count, protocol.BareName(t.Input2.Name))
	}
	s += fmt.Sprintf(" -> %d %s (%d/%d uses)",
		t.Output.Count, protocol.BareName(t.Output.Name), t.Uses, t.MaxUses)
	if !t.Available() {
		s += " [locked out]"
	}
	return s
}

// Trades returns the open merchant window's offers.
//
// Empty unless a merchant window is open — the list arrives with the window and
// is discarded when it closes.
func (c *Client) Trades() []TradeOffer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]TradeOffer, len(c.trades))
	copy(out, c.trades)
	return out
}

// TradeFor finds the first available offer producing a named item.
//
// This is what makes trading testable by intent rather than by index: "trade
// for bread" survives a villager whose offer order differs, where "trade 0"
// does not.
func (c *Client) TradeFor(output string) (TradeOffer, bool) {
	for _, t := range c.Trades() {
		if !t.Available() {
			continue
		}
		if exact, fuzzy := matchesName(t.Output, output); exact || fuzzy {
			return t, true
		}
	}
	return TradeOffer{}, false
}

// TradeForItem selects and performs the first available offer producing a named
// item, and collects the result.
//
// Returns how many trades completed. Unlike selecting by index this can say why
// nothing happened: whether the villager has no such offer at all, or has one
// that is locked out until it restocks.
func (c *Client) TradeForItem(ctx context.Context, output string, times int) (int, error) {
	offer, ok := c.TradeFor(output)
	if ok {
		return c.TradeAndTake(ctx, offer.Index, times)
	}
	// Say which of the two it is, because they need different fixes.
	for _, t := range c.Trades() {
		if exact, fuzzy := matchesName(t.Output, output); exact || fuzzy {
			return 0, fmt.Errorf(
				"understudy: the %s trade for %s is locked out (%d of %d uses spent) — "+
					"the villager has to restock, and a wandering trader never will",
				c.ContainerType(), output, t.Uses, t.MaxUses)
		}
	}
	return 0, fmt.Errorf("understudy: no trade offers %s (offers: %v)", output, c.Trades())
}

// maxTradeOffers bounds the offer list. A villager has at most ten; the cap
// stops a corrupt count preallocating an arbitrary slice.
const maxTradeOffers = 64

// handleTradeList decodes a merchant's offers.
//
// # Where this encoding came from
//
// minecraft-data describes the packet but leaves "ExactComponentMatcher"
// undefined, which blocked decoding entirely. Rather than guess a length — the
// mistake that desynchronises a packet and surfaces somewhere unrelated — the
// shape was read off the wire: summon a villager with chosen trades, dump the
// bytes, and check that every value put in comes back out at a known offset.
//
// The matcher turned out to be a VarInt count of component requirements, and it
// is zero for every vanilla trade. A non-zero one still cannot be skipped, for
// the same reason readSlot cannot skip data components, so this stops and
// reports rather than continuing into misaligned bytes.
func (c *Client) handleTradeList(p protocol.Packet) error {
	dumpTradeList(p.Data)
	r := p.Reader()
	windowID := r.VarInt()
	count := r.VarInt()
	if err := r.Err(); err != nil {
		return err
	}
	if count < 0 || count > maxTradeOffers {
		return fmt.Errorf("understudy: implausible trade count %d", count)
	}
	if !c.window.Matches(windowID) {
		c.log.Debug("trade list for a window we are not tracking", "window", windowID)
	}

	offers := make([]TradeOffer, 0, count)
	for i := range count {
		offer := TradeOffer{Index: i}
		var err error
		if offer.Input, err = readTradeItem(c.v, r); err != nil {
			c.log.Debug("stopped decoding trade offers", "offer", i, "err", err)
			break
		}
		if offer.Output, err = readSlot(c.v, r); err != nil {
			c.log.Debug("stopped decoding trade offers", "offer", i, "err", err)
			break
		}
		if r.Bool() { // inputItem2 is optional
			if offer.Input2, err = readTradeItem(c.v, r); err != nil {
				c.log.Debug("stopped decoding trade offers", "offer", i, "err", err)
				break
			}
		}
		offer.Disabled = r.Bool()
		offer.Uses = r.I32()
		offer.MaxUses = r.I32()
		offer.XP = r.I32()
		offer.SpecialPrice = r.I32()
		offer.PriceMult = r.F32()
		offer.Demand = r.I32()
		if err := r.Err(); err != nil {
			c.log.Debug("trade offer ran short", "offer", i, "err", err)
			break
		}
		offers = append(offers, offer)
	}

	c.mu.Lock()
	c.trades = offers
	c.mu.Unlock()
	c.log.Debug("merchant offers", "window", windowID, "count", len(offers))
	return nil
}

// readTradeItem decodes a trade's input: an id, a count, and the component
// matcher whose encoding is documented above.
func readTradeItem(v *protocol.Version, r *protocol.Reader) (ItemStack, error) {
	id := r.VarInt()
	count := r.VarInt()
	matchers := r.VarInt()
	if err := r.Err(); err != nil {
		return ItemStack{}, err
	}
	if matchers != 0 {
		return ItemStack{}, fmt.Errorf(
			"trade input %s requires %d exact components, which this client cannot skip",
			v.ItemName(id), matchers)
	}
	return ItemStack{ID: id, Name: v.ItemName(id), Count: count}, nil
}
