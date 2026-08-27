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
// Only the components that actually turn up in testing are here — seventy of
// the eighty-one that exist. The right answer for the other eleven is still to
// stop and say so, now with the name attached: see component_names.go.
//
// Those eleven are use_effects, creative_slot_lock, piercing_weapon,
// kinetic_weapon, swing_animation, additional_trade_cost, dye,
// map_post_processing, debug_stick_state, lock and container_loot. Two of them
// — creative_slot_lock and map_post_processing — this server rejects outright,
// so they may not exist on 26.1 at all; the rest simply never turned up on an
// item a command could build. Each will announce itself by name the first time
// one does.

// Component type ids, as observed on 26.1.
const (
	componentCustomData             = 0
	componentMaxStackSize           = 1
	componentMaxDamage              = 2
	componentDamage                 = 3
	componentUnbreakable            = 4
	componentCustomName             = 6
	componentMinimumAttackCharge    = 7
	componentDamageType             = 8
	componentItemName               = 9
	componentItemModel              = 10
	componentLore                   = 11
	componentRarity                 = 12
	componentEnchantments           = 13
	componentCanPlaceOn             = 14
	componentCanBreak               = 15
	componentAttributeModifiers     = 16
	componentCustomModelData        = 17
	componentTooltipDisplay         = 18
	componentRepairCost             = 19
	componentGlintOverride          = 21
	componentIntangibleProjectile   = 22
	componentFood                   = 23
	componentConsumable             = 24
	componentUseRemainder           = 25
	componentUseCooldown            = 26
	componentDamageResistant        = 27
	componentTool                   = 28
	componentWeapon                 = 29
	componentAttackRange            = 30
	componentEnchantable            = 31
	componentEquippable             = 32
	componentRepairable             = 33
	componentGlider                 = 34
	componentTooltipStyle           = 35
	componentDeathProtection        = 36
	componentBlocksAttacks          = 37
	componentStoredEnchantments     = 42
	componentDyedColor              = 44
	componentMapColor               = 45
	componentMapID                  = 46
	componentMapDecorations         = 47
	componentChargedProjectiles     = 49
	componentBundleContents         = 50
	componentPotionContents         = 51
	componentPotionDurationScale    = 52
	componentStewEffects            = 53
	componentWritableBook           = 54
	componentWrittenBook            = 55
	componentTrim                   = 56
	componentEntityData             = 58
	componentBucketEntityData       = 59
	componentBlockEntityData        = 60
	componentInstrument             = 61
	componentProvidesTrimMaterial   = 62
	componentOminousAmplifier       = 63
	componentJukeboxPlayable        = 64
	componentProvidesBannerPatterns = 65
	componentRecipes                = 66
	componentLodestoneTracker       = 67
	componentFireworkExplosion      = 68
	componentFireworks              = 69
	componentProfile                = 70
	componentNoteBlockSound         = 71
	componentBannerPatterns         = 72
	componentBaseColor              = 73
	componentPotDecorations         = 74
	componentContainer              = 75
	componentBlockState             = 76
	componentBees                   = 77
	componentBreakSound             = 80
)

// skipComponent steps over one data component's payload.
//
// Returns an error naming the type when it cannot, which the caller turns into
// a partial window rather than a desynchronised one.
func skipComponent(v *protocol.Version, r *protocol.Reader, kind int32, into *ItemStack) error {
	switch kind {
	case componentPotionContents:
		return skipPotionContents(r, into)
	case componentUnbreakable:
		// No payload at all — the component's presence is the whole meaning.
		return nil
	case componentDamage, componentMaxStackSize, componentMaxDamage,
		componentRarity, componentRepairCost, componentMapID, componentEnchantable:
		// Each a single VarInt. Confirmed with two samples apiece where the
		// value crosses the one-to-two byte boundary: damage 37 and 1000,
		// repair cost 3 and 300.
		//
		// damage is the one that matters most. Any tool that has been used
		// carries it, so before this was handled a bot went blind to its own
		// inventory the moment it mined anything — and repair_cost is the same
		// story for anything that has been through an anvil.
		r.VarInt()
		return r.Err()
	case componentDyedColor, componentMapColor:
		// A packed 32-bit colour. 0x00a06540 came back as the 10511680 set on a
		// dyed item, and a map tinted 1234567 as 0x0012d687.
		r.I32()
		return r.Err()
	case componentCustomData, componentCustomName, componentItemName,
		componentIntangibleProjectile, componentMapDecorations,
		componentBucketEntityData, componentRecipes:
		// A nameless NBT tag. custom_data is a whole compound, the two names
		// are text components, a knowledge book's recipes are a list of strings
		// — all step with the same walker.
		//
		// intangible_projectile is here rather than beside unbreakable, which
		// is the surprise: it reads like a marker but is not one. Set to an
		// empty map it sends `0a 00`, a nameless empty compound, where a true
		// marker like glider sends nothing at all.
		return skipNBT(r)
	case componentGlintOverride:
		// One bool. Whether the item shimmers, forced either way.
		r.Bool()
		return r.Err()
	case componentPotionDurationScale, componentMinimumAttackCharge:
		r.F32()
		return r.Err()
	case componentNoteBlockSound, componentItemModel, componentTooltipStyle:
		// Plain strings. A note block's sound is a name rather than a registry
		// id because it can name a sound that has none, and the other two point
		// at client-side resources the server never resolves.
		_ = r.String()
		return r.Err()
	case componentOminousAmplifier, componentBaseColor:
		// Plain VarInts rather than holders — an amplifier is a number and a
		// base colour is one of the sixteen dyes, neither of which can be
		// defined inline.
		r.VarInt()
		return r.Err()
	case componentInstrument, componentJukeboxPlayable, componentDamageType,
		componentProvidesTrimMaterial, componentBreakSound:
		return skipHolder(r, componentName(kind))
	case componentDamageResistant, componentRepairable,
		componentProvidesBannerPatterns:
		// A holder set: a count where zero means a tag name follows instead.
		// `#minecraft:is_fire` arrived as exactly that.
		readIDSet(r)
		return r.Err()
	case componentEntityData, componentBlockEntityData:
		// A type id and then the rest as NBT. The id is hoisted out of the
		// compound rather than left in it — a pig spawn egg carrying
		// {id:"minecraft:pig",NoAI:1b} sends 100 followed by a compound holding
		// only NoAI. That hoisting is what makes bees decodable too.
		r.VarInt()
		return skipNBT(r)
	default:
		return skipShapedComponent(v, r, kind)
	}
}

// skipShapedComponent steps over the components whose payload is a structure
// rather than a single value.
//
// Split from skipComponent only for length. The line between the two is that
// everything above reads one thing — a number, a string, a tag, a registry
// reference — and everything here reads several, or a list of several.
func skipShapedComponent(v *protocol.Version, r *protocol.Reader, kind int32) error {
	switch kind {
	case componentCanPlaceOn, componentCanBreak:
		return skipBlockPredicates(r)
	case componentTooltipDisplay:
		// Whether to hide the tooltip entirely, then the components to leave
		// out of it — by their own type ids, so `damage` arrives as a 3.
		r.Bool()
		return skipVarIntList(r)
	case componentFood:
		// Nutrition, saturation, and whether it can always be eaten. Read back
		// as the 4, 1.2 and true that went in.
		r.VarInt()
		r.F32()
		r.Bool()
		return r.Err()
	case componentConsumable:
		return skipConsumable(r)
	case componentUseRemainder:
		// What is left behind, as a nested stack — a bowl after the soup.
		_, err := skipNestedStack(v, r)
		return err
	case componentUseCooldown:
		// How long, and an optional group so several items can share one timer.
		r.F32()
		if r.Bool() {
			_ = r.String()
		}
		return r.Err()
	case componentTool:
		return skipTool(r)
	case componentWeapon:
		// Damage per attack, and how long a hit disables blocking.
		r.VarInt()
		r.F32()
		return r.Err()
	case componentAttackRange:
		// Six floats. Nothing here could be set through /item replace, so all
		// six came back as defaults — 0, 3, 0, 5, 0.3, 1 — and the reading
		// rests on the whole packet landing rather than on a value being
		// recognised. Every one of them parses as a sensible float, and no
		// other grouping of twenty-four bytes does.
		r.Skip(6 * 4)
		return r.Err()
	case componentEquippable:
		return skipEquippable(r)
	case componentDeathProtection, componentGlider:
		return skipDeathProtection(r, kind)
	case componentBlocksAttacks:
		return skipBlocksAttacks(r)
	case componentBees:
		return skipBees(r)
	case componentTrim:
		return skipTrim(r)
	case componentContainer:
		return skipContainerContents(v, r)
	case componentFireworkExplosion:
		return skipFireworkExplosion(r)
	case componentAttributeModifiers:
		return skipAttributeModifiers(r)
	case componentLodestoneTracker:
		return skipLodestoneTracker(r)
	case componentProfile:
		return skipProfile(r)
	case componentWrittenBook:
		return skipWrittenBook(r)
	case componentLore:
		// A count, then that many NBT text components. Confirmed with one line
		// and with three.
		return skipNBTList(r)
	case componentCustomModelData:
		return skipCustomModelData(r)
	case componentPotDecorations:
		// Four sherd item ids, one per side.
		return skipVarIntList(r)
	case componentBlockState:
		// The properties a block item carries, as name/value string pairs —
		// `facing` and `north` for a stone placed facing north.
		return skipStringPairs(r)
	case componentBannerPatterns:
		return skipBannerPatterns(r)
	case componentChargedProjectiles, componentBundleContents:
		return skipNestedStackList(v, r)
	case componentFireworks:
		return skipFireworks(r)
	case componentWritableBook:
		return skipWritableBook(r)
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
		return fmt.Errorf("data component %s has no known encoding", componentName(kind))
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

// skipBlockPredicates steps over the blocks an item may be placed on or may
// break.
//
// Each predicate is an optional holder set of blocks, an optional state match,
// optional nbt, and then the pair of counts that match components — the same
// pair a villager's trade carries. A pickaxe restricted to dirt and stone sent
// its two blocks as one holder set and left everything after it absent.
//
// The state and component matchers are structures this cannot walk into, but
// they are absent on anything a command or a plugin sets in the ordinary way.
func skipBlockPredicates(r *protocol.Reader) error {
	for range r.VarInt() {
		if r.Bool() {
			readIDSet(r)
		}
		if r.Bool() {
			return fmt.Errorf("block predicate matches block state properties, " +
				"which cannot be skipped")
		}
		if r.Bool() {
			if err := skipNBT(r); err != nil {
				return err
			}
		}
		if exact, partial := r.VarInt(), r.VarInt(); exact != 0 || partial != 0 {
			return fmt.Errorf("block predicate matches %d exact and %d partial "+
				"components, which cannot be skipped", exact, partial)
		}
	}
	return r.Err()
}

// skipConsumable steps over how an item is eaten or drunk: how long it takes,
// the animation, the sound, whether it shows particles, and what it does.
//
// The effects are a list this cannot walk into. They are empty for anything
// vanilla — a potion's effects live in potion_contents, not here.
func skipConsumable(r *protocol.Reader) error {
	r.F32()    // consume seconds
	r.VarInt() // animation
	if err := skipHolder(r, "consume sound"); err != nil {
		return err
	}
	r.Bool() // particles
	if n := r.VarInt(); n != 0 {
		return fmt.Errorf("consumable carries %d effects, which cannot be skipped", n)
	}
	return r.Err()
}

// skipDeathProtection steps over what happens when a totem saves its holder.
//
// glider shares this only because it has no payload at all: its whole meaning
// is that it is present. death_protection has a count, which is zero on a
// vanilla totem, and its effects are a structure this cannot walk into.
func skipDeathProtection(r *protocol.Reader, kind int32) error {
	if kind == componentGlider {
		return nil
	}
	if n := r.VarInt(); n != 0 {
		return fmt.Errorf("death protection carries %d effects, which cannot be skipped", n)
	}
	return r.Err()
}

// skipTool steps over what an item mines: a rule per block set, then the
// defaults.
//
// A rule is the blocks it applies to, an optional speed and an optional
// "counts as the right tool". Read back as the dirt/5.0/true that went in,
// with 1.5 and 2 for the defaults after it.
func skipTool(r *protocol.Reader) error {
	for range r.VarInt() {
		readIDSet(r)
		if r.Bool() {
			r.F32() // speed
		}
		if r.Bool() {
			r.Bool() // correct for drops
		}
	}
	r.F32()    // default mining speed
	r.VarInt() // damage per block
	r.Bool()   // can destroy blocks in creative
	return r.Err()
}

// skipEquippable steps over where an item is worn.
//
// Two samples: a bare {slot:"head"}, where every optional is absent, and one
// setting the asset, the camera overlay and the entities allowed to wear it,
// which is what shows those three are optionals rather than fixed fields.
func skipEquippable(r *protocol.Reader) error {
	r.VarInt() // slot — head came back as 4
	if err := skipHolder(r, "equip sound"); err != nil {
		return err
	}
	if r.Bool() {
		_ = r.String() // equipment asset
	}
	if r.Bool() {
		_ = r.String() // camera overlay
	}
	if r.Bool() {
		readIDSet(r) // the entities that may wear it
	}
	r.Skip(5) // dispensable, swappable, damage on hurt, equip on interact, shearable
	return skipHolder(r, "shearing sound")
}

// skipBlocksAttacks steps over a shield's blocking.
//
// The delay before it engages, how long a disabling hit lasts, the damage it
// reduces, how much the item itself takes, and three optional sounds. A shield
// built with a 0.25 delay and a 1.5 scale came back as exactly those.
//
// The reductions are a structure this cannot walk into and are empty on a
// vanilla shield, whose blocking is described entirely by the numbers here.
func skipBlocksAttacks(r *protocol.Reader) error {
	r.F32() // block delay seconds
	r.F32() // disable cooldown scale
	if n := r.VarInt(); n != 0 {
		return fmt.Errorf("blocks_attacks carries %d damage reductions, "+
			"which cannot be skipped", n)
	}
	r.F32() // item damage threshold
	r.F32() // base
	r.F32() // factor
	if r.Bool() {
		_ = r.String() // the damage type tag that bypasses blocking
	}
	for _, what := range []string{"block sound", "disable sound"} {
		if r.Bool() {
			if err := skipHolder(r, what); err != nil {
				return err
			}
		}
	}
	return r.Err()
}

// skipBees steps over what a hive holds.
//
// Each bee is its entity type, what is left of its data once the type is taken
// out, and the two tick counts. The leading type is the same hoisting
// entity_data does, which is what made this readable: without it the 11 in
// front of an empty compound looks like nothing at all.
func skipBees(r *protocol.Reader) error {
	for range r.VarInt() {
		r.VarInt() // entity type
		if err := skipNBT(r); err != nil {
			return err
		}
		r.VarInt() // ticks in hive
		r.VarInt() // minimum ticks in hive
	}
	return r.Err()
}

// skipTrim steps over an armour trim: two holders, the material and the
// pattern. Gold and coast came back as 5 and 2 against registry indices 4 and
// 1. Nothing else follows — the flag that used to show the trim in the tooltip
// moved out into tooltip_display.
func skipTrim(r *protocol.Reader) error {
	if err := skipHolder(r, "trim material"); err != nil {
		return err
	}
	return skipHolder(r, "trim pattern")
}

// skipNBTList steps over a count and that many nameless NBT tags.
func skipNBTList(r *protocol.Reader) error {
	for range r.VarInt() {
		if err := skipNBT(r); err != nil {
			return err
		}
	}
	return r.Err()
}

// skipVarIntList steps over a count and that many VarInts.
func skipVarIntList(r *protocol.Reader) error {
	for range r.VarInt() {
		r.VarInt()
	}
	return r.Err()
}

// skipStringPairs steps over a count and that many pairs of strings.
func skipStringPairs(r *protocol.Reader) error {
	for range r.VarInt() {
		_ = r.String()
		_ = r.String()
	}
	return r.Err()
}

// skipCustomModelData steps over four lists: floats, flags, strings and
// colours.
func skipCustomModelData(r *protocol.Reader) error {
	for range r.VarInt() {
		r.F32()
	}
	for range r.VarInt() {
		r.Bool()
	}
	for range r.VarInt() {
		_ = r.String()
	}
	for range r.VarInt() {
		r.I32()
	}
	return r.Err()
}

// skipBannerPatterns steps over a banner's layers: a holder and a dye colour
// apiece. Confirmed with a one-layer banner and a two-layer one.
func skipBannerPatterns(r *protocol.Reader) error {
	for range r.VarInt() {
		if err := skipHolder(r, "banner pattern"); err != nil {
			return err
		}
		r.VarInt() // dye colour
	}
	return r.Err()
}

// skipNestedStackList steps over a count and that many item stacks. Confirmed
// with a crossbow holding one arrow and another holding an arrow and a rocket.
func skipNestedStackList(v *protocol.Version, r *protocol.Reader) error {
	for range r.VarInt() {
		if _, err := skipNestedStack(v, r); err != nil {
			return err
		}
	}
	return r.Err()
}

// skipFireworks steps over a rocket: how long it flies, and the bursts.
func skipFireworks(r *protocol.Reader) error {
	r.VarInt() // flight duration
	for range r.VarInt() {
		if err := skipFireworkExplosion(r); err != nil {
			return err
		}
	}
	return r.Err()
}

// skipWritableBook steps over a book still being written: pages are raw
// strings, each with an optional filtered version beside it.
func skipWritableBook(r *protocol.Reader) error {
	for range r.VarInt() {
		_ = r.String()
		if r.Bool() {
			_ = r.String()
		}
	}
	return r.Err()
}

// skipHolder steps over a reference to a registry entry.
//
// The vanilla convention is an id one greater than the registry index, with
// zero reserved to mean "the definition follows inline". Three independent
// registries confirm it: banner pattern `cross` arrived as 6 and `stripe_top`
// as 39 against alphabetical indices 5 and 38, a gold/coast armour trim as 5
// and 2 against 4 and 1, and `sing_goat_horn` as 7 against 6.
//
// The inline form is a whole definition whose shape differs per registry, so it
// stops here rather than guessing. Nothing a server sends for an ordinary item
// uses it — only a datapack that defines an entry the client has never seen.
func skipHolder(r *protocol.Reader, what string) error {
	if r.VarInt() == 0 {
		return fmt.Errorf("%s is defined inline, which cannot be skipped", what)
	}
	return r.Err()
}

// skipNestedStack steps over an item stack held inside a component, returning
// its id.
//
// Not the same encoding as an item in a packet: those lead with the count and
// use a zero to mean empty, while one nested in a component leads with the id
// and is never empty. The optional-ness, where there is any, sits outside.
func skipNestedStack(v *protocol.Version, r *protocol.Reader) (int32, error) {
	id := r.VarInt()
	r.VarInt() // count
	added := r.VarInt()
	removed := r.VarInt()
	if err := r.Err(); err != nil {
		return id, err
	}
	for range added {
		kind := r.VarInt()
		if err := skipComponent(v, r, kind, nil); err != nil {
			return id, fmt.Errorf("item %s: %w", v.ItemName(id), err)
		}
	}
	for range removed {
		r.VarInt()
	}
	return id, r.Err()
}

// skipContainerContents steps over what a shulker box or chest item holds.
//
// The list is dense rather than sparse: a box with something in slots 0 and 3
// sends four entries, the two in between being a bare zero. That is why the
// count is a slot count and not an item count.
func skipContainerContents(v *protocol.Version, r *protocol.Reader) error {
	for range r.VarInt() {
		if !r.Bool() {
			continue // an empty slot, one byte
		}
		if _, err := skipNestedStack(v, r); err != nil {
			return err
		}
	}
	return r.Err()
}

// skipFireworkExplosion steps over one burst: its shape, the colours it starts
// and fades to, and the two flags.
//
// The colours are packed int32s. A rocket built with 0xFF0000 fading to 0xFF
// came back as exactly those two words, which is what fixes them as int32s
// rather than anything shorter.
func skipFireworkExplosion(r *protocol.Reader) error {
	r.VarInt() // shape
	for range r.VarInt() {
		r.I32() // colour
	}
	for range r.VarInt() {
		r.I32() // fade colour
	}
	r.Bool() // trail
	r.Bool() // twinkle
	return r.Err()
}

// skipAttributeModifiers steps over the modifiers an item applies.
//
// Each is an attribute id, the modifier's own name, the amount as a float64,
// how it combines, and which slot it applies in — then a display mode, which is
// the field that made a diamond chestplate read one byte short until it was
// found. Only mode 2 carries anything: a text component overriding the line
// shown in the tooltip.
func skipAttributeModifiers(r *protocol.Reader) error {
	for range r.VarInt() {
		r.VarInt()     // attribute
		_ = r.String() // the modifier's id
		r.F64()        // amount
		r.VarInt()     // operation
		r.VarInt()     // slot group
		if r.VarInt() == 2 {
			if err := skipNBT(r); err != nil {
				return err
			}
		}
	}
	return r.Err()
}

// skipLodestoneTracker steps over a compass's lodestone.
//
// An optional target — a dimension name and a packed block position — then
// whether the compass still tracks it. A compass pointed at 1,2,3 in the
// overworld came back as the name followed by 0x4000003002, which is that
// position packed the vanilla way.
func skipLodestoneTracker(r *protocol.Reader) error {
	if r.Bool() {
		_ = r.String() // dimension
		r.I64()        // packed position
	}
	r.Bool() // tracked
	return r.Err()
}

// skipProfile steps over a player head's profile.
//
// It leads with a discriminator, which is the part a single sample cannot show.
// Zero is the partial form, where the name and the uuid are each optional —
// that is what a head named after someone the server has not resolved looks
// like. One is the resolved form, where both are present and neither carries a
// flag. Five heads pin it: two named, one with a uuid, and two with properties
// with and without a signature.
//
// The four bytes at the end are zero on every head seen. They are read as four
// absent optionals, which is the reading that fails loudly rather than
// silently: a head that sets one stops the scan instead of desynchronising it.
func skipProfile(r *protocol.Reader) error {
	if r.VarInt() == 0 {
		if r.Bool() {
			_ = r.String() // name
		}
		if r.Bool() {
			r.Skip(16) // uuid
		}
	} else {
		r.Skip(16)
		_ = r.String()
	}
	for range r.VarInt() {
		_ = r.String() // property name
		_ = r.String() // value
		if r.Bool() {
			_ = r.String() // signature
		}
	}
	for i := range 4 {
		if r.Bool() {
			return fmt.Errorf("profile carries field %d after its properties, "+
				"which has never been seen set and cannot be skipped", i)
		}
	}
	return r.Err()
}

// skipWrittenBook steps over a signed book.
//
// Unlike a writable one, its pages are text components rather than raw strings,
// so they step with the NBT walker.
func skipWrittenBook(r *protocol.Reader) error {
	_ = r.String() // title
	if r.Bool() {
		_ = r.String() // the filtered title
	}
	_ = r.String() // author
	r.VarInt()     // generation
	for range r.VarInt() {
		if err := skipNBT(r); err != nil {
			return err
		}
		if r.Bool() {
			if err := skipNBT(r); err != nil {
				return err
			}
		}
	}
	r.Bool() // resolved
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

// skipNBT steps over one nameless NBT tag in the reader.
func skipNBT(r *protocol.Reader) error {
	n, err := nbt.SkipTag(r.Remaining())
	if err != nil {
		return fmt.Errorf("nbt payload: %w", err)
	}
	r.Skip(n)
	return r.Err()
}
