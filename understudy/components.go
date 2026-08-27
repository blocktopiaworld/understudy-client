package understudy

import (
	"fmt"

	"github.com/blocktopia/understudy-client/internal/nbt"
	"github.com/blocktopia/understudy-client/protocol"
)

// Data components.
//
// An item can carry components — a potion's contents, a stew's effects, a
// custom name — and there is no published encoding for any of them.
// minecraft-data defines none. Worse, they are not length-prefixed, so a
// component that cannot be decoded cannot be *skipped* either: the reader has
// no way to know where the next field begins.
//
// That single fact shapes everything here. An item with an unreadable component
// stops the scan, and every slot after it in that packet is unknown rather than
// empty — which is how a bot holding three water bottles became blind to the
// blaze powder in its own hotbar, with no error anywhere.
//
// # How these were established
//
// Not from documentation, which does not exist. Each was read off a captured
// packet whose contents were chosen: put a known item in a known slot, dump the
// window, and check the bytes account for exactly what went in. The check that
// a reading is right is that the whole packet then decodes and lands on its
// final byte — a wrong width anywhere leaves the remainder as nonsense.
//
// Only the components that actually turn up in testing are here. The right
// answer for anything else is still to stop and say so.

// Component type ids, as observed on 26.1.
const (
	componentDamage             = 3
	componentCustomName         = 6
	componentEnchantments       = 13
	componentStoredEnchantments = 42
	componentMapID              = 46
	componentPotionContents     = 51
	componentStewEffects        = 53
)

// skipComponent steps over one data component's payload.
//
// Returns an error naming the type when it cannot, which the caller turns into
// a partial window rather than a desynchronised one.
func skipComponent(v *protocol.Version, r *protocol.Reader, kind int32, into *ItemStack) error {
	switch kind {
	case componentPotionContents:
		return skipPotionContents(r, into)
	case componentDamage:
		// A single VarInt. Confirmed with two picks damaged 37 and 1000, which
		// encode one byte and two — the case a single sample cannot show.
		//
		// This is the component that matters most: any tool that has been used
		// carries it, so before this was handled a bot went blind to its own
		// inventory the moment it mined anything.
		r.VarInt()
		return r.Err()
	case componentCustomName:
		// A nameless NBT tag — the same text component a window title carries,
		// so the existing walker steps over it.
		n, err := nbt.SkipTag(r.Remaining())
		if err != nil {
			return fmt.Errorf("custom name: %w", err)
		}
		r.Skip(n)
		return r.Err()
	case componentMapID:
		// A single VarInt. Confirmed with two maps whose ids were chosen: they
		// decoded as 7 and 9 and sat exactly seven bytes apart.
		r.VarInt()
		return r.Err()
	case componentEnchantments, componentStoredEnchantments, componentStewEffects:
		// All three are the same shape: a count, then that many pairs of
		// VarInts. Enchantments are id and level, a stew's effects are id and
		// duration.
		//
		// Confirmed with a sword carrying one enchantment and another carrying
		// three, which is what shows the count is real rather than a fixed
		// width that happened to fit.
		return skipVarIntPairs(r)
	default:
		return fmt.Errorf("data component %d has no known encoding", kind)
	}
}

// skipPotionContents steps over a potion's contents.
//
// The payload is five bytes for a plain water bottle, `01 00 00 00 00`:
//
//	optional potion id      bool, then a VarInt
//	optional custom colour  bool, then an int32
//	custom effects          a VarInt count
//	effects to apply        a VarInt count
//
// Two captures pin it. The same bottle with a custom colour came out exactly
// four bytes longer and nothing else moved, which fixes the colour as an int32
// and everything around it.
//
// # The off-by-one that a single capture hid
//
// Read from one bottle alone, this looks like six bytes with a trailing
// optional string. It is not: window_items ends with the *carried* slot — what
// the cursor is holding — and with one item in the window that spare byte sits
// immediately after the potion, where it reads exactly like another empty
// optional.
//
// With three bottles the mistake is obvious, because the potions are eleven
// bytes apart rather than twelve, and reading six desynchronises the second one
// onwards. A capture with one of a thing cannot tell you its width; two of them
// can.
//
// The effect lists are counts this cannot walk into — a mob-effect instance is
// another undocumented structure — but they are zero for every ordinary potion,
// including every stage of brewing.
func skipPotionContents(r *protocol.Reader, into *ItemStack) error {
	if r.Bool() {
		id := r.VarInt()
		// Kept rather than discarded: it is the only thing that distinguishes
		// one potion from another, since they all share a name.
		if into != nil {
			into.Potion = id
		}
	}
	if r.Bool() {
		r.I32() // packed custom colour
	}
	if n := r.VarInt(); n != 0 {
		return fmt.Errorf("potion carries %d custom effects, which cannot be skipped", n)
	}
	if n := r.VarInt(); n != 0 {
		return fmt.Errorf("potion carries %d effects to apply, which cannot be skipped", n)
	}
	return r.Err()
}

// skipVarIntPairs steps over a count followed by that many pairs of VarInts.
//
// Shared because three separate components turn out to use it — enchantments,
// an enchanted book's stored enchantments, and a suspicious stew's effects.
func skipVarIntPairs(r *protocol.Reader) error {
	for range r.VarInt() {
		r.VarInt()
		r.VarInt()
	}
	return r.Err()
}
