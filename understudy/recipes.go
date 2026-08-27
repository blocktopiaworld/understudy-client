package understudy

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/blocktopia/understudy-client/protocol"
)

// The server's recipe book.
//
// craft_recipe_request takes a numeric recipe id and nothing else, so without
// this a caller has no way to name what it wants to craft. The ids are the
// server's own and change between versions and even between worlds, which is
// why they are learned from the connection rather than written down.
//
// # How the encoding was established
//
// minecraft-data describes recipe_book_add down to SlotDisplay, but leaves
// IDSet and the data components undefined — and one entry in the vanilla book
// (suspicious stew) carries a component, which is enough to lose alignment for
// every entry after it.
//
// So it was read off a captured packet rather than guessed. The check that it
// is right is that all 1498 entries decode and land exactly one byte from the
// end, on the trailing "replace" flag: a single wrong field width anywhere
// would leave the remainder as nonsense long before then.
//
// What that established:
//
//   - IDSet is the vanilla holder set: a VarInt n, where 0 means a tag name
//     string follows and otherwise n-1 ids do.
//   - SlotDisplay's item_stack is *id first*, unlike the inventory Slot which
//     is count first. Confirmed against a furnace recipe that reads as
//     mutton -> cooked_mutton with 0.35 experience in the smoker category.
//   - The one component that appears, type 53, is a list of pairs of VarInts.

// RecipeID is the server-assigned id that craft_recipe_request takes.
type RecipeID int32

// maxRecipeEntries bounds the book. Vanilla 26.1 sends about 1500; the cap
// stops a corrupt count preallocating an arbitrary slice.
const maxRecipeEntries = 1 << 14

// slotDisplayDepth bounds SlotDisplay recursion, which nests through
// composite, with_remainder and dyed variants.
const slotDisplayDepth = 32

// Recipe display kinds.
const (
	recipeShapeless = 0
	recipeShaped    = 1
	recipeFurnace   = 2
	recipeStonecutt = 3
	recipeSmithing  = 4
)

// RecipeFor returns the recipe id that produces a named item.
//
// The lookup is by what the recipe *makes*, which is the question a caller
// actually has. Names match the same way they do elsewhere: namespaced or bare.
func (c *Client) RecipeFor(name string) (RecipeID, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if id, ok := c.recipes[protocol.Namespaced(name)]; ok {
		return id, true
	}
	// Fall back to the loose suffix match the rest of the client accepts.
	for item, id := range c.recipes {
		if _, fuzzy := matchesName(ItemStack{Name: item}, name); fuzzy {
			return id, true
		}
	}
	return 0, false
}

// KnownRecipes returns how many recipes were learned from the server.
//
// Worth checking before relying on RecipeFor: a server that sends its book late,
// or a decode that stopped early on an unknown component, leaves this short.
func (c *Client) KnownRecipes() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.recipes)
}

// CraftRecipeFor asks the server to craft a named item in the open window.
//
// This is the cheap path: the server lays the grid out from its own recipe
// book, so a caller neither encodes the recipe nor places ingredients slot by
// slot, and `all` repeats until the ingredients run out. CraftInGrid does the
// same job by clicking, which works without the recipe book but costs about
// twenty clicks a craft.
//
// The result still has to be collected — see TakeFromContainer with
// CraftingResultSlot.
func (c *Client) CraftRecipeFor(ctx context.Context, name string, all bool) error {
	id, ok := c.RecipeFor(name)
	if !ok {
		return fmt.Errorf(
			"understudy: no recipe known for %q (%d recipes learned from the server) — "+
				"CraftInGrid places ingredients by hand and needs no recipe book",
			name, c.KnownRecipes())
	}
	return c.CraftRecipe(int32(id), all)
}

// handleRecipeBook decodes recipe_book_add into a result-name to id map.
//
// Only the result matters here. The ingredients are decoded because they must
// be stepped over exactly, not because anything reads them: the server lays the
// grid out itself, so knowing what a recipe *needs* would be redundant.
func (c *Client) handleRecipeBook(p protocol.Packet) error {
	dumpRecipeBook(p.Data)
	r := p.Reader()
	count := r.VarInt()
	if err := r.Err(); err != nil {
		return err
	}
	if count < 0 || count > maxRecipeEntries {
		return fmt.Errorf("understudy: implausible recipe count %d", count)
	}

	learned := make(map[string]RecipeID, count)
	decoded := 0
	for range count {
		id := RecipeID(r.VarInt())
		result, err := c.readRecipeDisplay(r)
		if err != nil {
			c.log.Debug("stopped decoding the recipe book", "after", decoded, "err", err)
			break
		}
		r.VarInt() // group (optional varint; absent is zero)
		r.VarInt() // category
		if r.Bool() {
			for range r.VarInt() {
				readIDSet(r)
			}
		}
		r.U8() // notification/highlight flags
		if err := r.Err(); err != nil {
			c.log.Debug("recipe entry ran short", "after", decoded, "err", err)
			break
		}
		decoded++
		// First id wins: a later duplicate is an alternative recipe for the
		// same item, and either will craft it.
		if result != "" {
			if _, seen := learned[result]; !seen {
				learned[result] = id
			}
		}
	}

	c.mu.Lock()
	// recipe_book_add is additive unless the server says to replace, and the
	// first one on a session is empty — so merge rather than overwrite, or the
	// book is lost the moment anything unlocks a single recipe.
	if c.recipes == nil {
		c.recipes = make(map[string]RecipeID, len(learned))
	}
	for name, id := range learned {
		c.recipes[name] = id
	}
	total := len(c.recipes)
	c.mu.Unlock()

	c.log.Debug("recipe book", "entries", count, "decoded", decoded, "known", total)
	return nil
}

// readRecipeDisplay steps over one recipe's display and returns what it makes.
//
// Every kind is the same shape — some slot displays, of which one is the result
// — so each is described as "how many come before the result, and how many
// after", rather than as five near-identical blocks of the same three lines.
func (c *Client) readRecipeDisplay(r *protocol.Reader) (string, error) {
	kind := r.VarInt()
	switch kind {
	case recipeShapeless:
		// A count, then that many ingredients, then result and station.
		return c.readDisplaySlots(r, int(r.VarInt()), 1)
	case recipeShaped:
		r.VarInt() // width
		r.VarInt() // height
		return c.readDisplaySlots(r, int(r.VarInt()), 1)
	case recipeFurnace:
		// ingredient, fuel, [result], station, then duration and experience.
		result, err := c.readDisplaySlots(r, 2, 1)
		if err != nil {
			return "", err
		}
		r.VarInt() // duration
		r.F32()    // experience
		return result, r.Err()
	case recipeStonecutt:
		return c.readDisplaySlots(r, 1, 1)
	case recipeSmithing:
		// template, base, addition, [result], station.
		return c.readDisplaySlots(r, 3, 1)
	default:
		return "", fmt.Errorf("unknown recipe display kind %d", kind)
	}
}

// readDisplaySlots steps over `before` slot displays, then the result, then
// `after` more, and returns what the result names.
func (c *Client) readDisplaySlots(r *protocol.Reader, before, after int) (string, error) {
	for range before {
		if _, err := c.readSlotDisplay(r, 0); err != nil {
			return "", err
		}
	}
	result, err := c.readSlotDisplay(r, 0)
	if err != nil {
		return "", err
	}
	for range after {
		if _, err := c.readSlotDisplay(r, 0); err != nil {
			return "", err
		}
	}
	return result, nil
}

// readSlotDisplay steps over a SlotDisplay and returns the item it names, if
// it names exactly one.
func (c *Client) readSlotDisplay(r *protocol.Reader, depth int) (string, error) {
	if depth > slotDisplayDepth {
		return "", fmt.Errorf("slot display nested deeper than %d", slotDisplayDepth)
	}
	// The kinds are registry indices and they move between versions: composite
	// is 10 on 26.1 and 7 on 1.21.11, so a book read with the wrong table stops
	// at the first composite ingredient — which is most of them, and is why
	// 1.21.11 learned nothing at all rather than learning most of it.
	wire := r.VarInt()
	kind, known := c.v.SlotDisplayKind(wire)
	if !known {
		return "", fmt.Errorf("slot display kind %d on %s has no counterpart in "+
			"the numbering this decoder knows", wire, c.v.Name)
	}
	switch kind {
	case 0, 1: // empty, any_fuel
		return "", nil
	case 2: // with_any_potion
		return c.readSlotDisplay(r, depth+1)
	case 3: // only_with_component
		name, err := c.readSlotDisplay(r, depth+1)
		r.VarInt() // component type
		return name, err
	case 4: // item
		return c.v.ItemName(r.VarInt()), nil
	case 5: // item_stack — id first here, unlike the inventory Slot
		return c.readDisplayStack(r)
	case 6: // tag
		_ = r.String() // the tag name; only its bytes matter here
		return "", nil
	case 7: // dyed_slot_demo
		if _, err := c.readSlotDisplay(r, depth+1); err != nil {
			return "", err
		}
		return c.readSlotDisplay(r, depth+1)
	case 8: // smithing_trim
		if _, err := c.readSlotDisplay(r, depth+1); err != nil { // base
			return "", err
		}
		name, err := c.readSlotDisplay(r, depth+1) // material
		if err != nil {
			return "", err
		}
		// The pattern is a registry holder: a VarInt id, where zero would mean
		// an inline entry follows. No vanilla trim sends one inline.
		if r.VarInt() == 0 {
			return "", fmt.Errorf("inline smithing trim pattern")
		}
		return name, nil
	case 9: // with_remainder
		name, err := c.readSlotDisplay(r, depth+1)
		if err != nil {
			return "", err
		}
		_, err = c.readSlotDisplay(r, depth+1)
		return name, err
	case 10: // composite
		var first string
		for range r.VarInt() {
			name, err := c.readSlotDisplay(r, depth+1)
			if err != nil {
				return "", err
			}
			if first == "" {
				first = name
			}
		}
		return first, nil
	default:
		return "", fmt.Errorf("unknown slot display kind %d", kind)
	}
}

// readDisplayStack reads the id-first item stack a SlotDisplay carries — the
// same encoding as an item nested inside a component.
func (c *Client) readDisplayStack(r *protocol.Reader) (string, error) {
	id, err := skipNestedStack(c.v, r)
	if err != nil {
		return "", err
	}
	return c.v.ItemName(id), nil
}

// readIDSet steps over the vanilla holder set: a count where zero means a tag
// name follows, and otherwise count-1 ids do.
func readIDSet(r *protocol.Reader) {
	n := r.VarInt()
	if n == 0 {
		_ = r.String() // a tag name stands in for the set
		return
	}
	for range n - 1 {
		r.VarInt()
	}
}

// dumpRecipeBook writes the raw recipe_book_add payload when
// UNDERSTUDY_DUMP_RECIPES names a file.
//
// It keeps the largest payload seen rather than the first: the initial book on
// a session is empty, and on a fresh world it only fills once recipes unlock.
// This is how the encoding in recipes.go was established, and it is what the
// next person will want when a version changes it.
var (
	dumpRecipesMu   sync.Mutex
	dumpRecipesBest int
)

func dumpRecipeBook(payload []byte) {
	path := os.Getenv("UNDERSTUDY_DUMP_RECIPES")
	if path == "" {
		return
	}
	dumpRecipesMu.Lock()
	defer dumpRecipesMu.Unlock()
	if len(payload) <= dumpRecipesBest {
		return
	}
	dumpRecipesBest = len(payload)
	_ = os.WriteFile(path, payload, 0o644) //nolint:gosec // G703: operator debug path
}
