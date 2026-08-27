package understudy

import "fmt"

// Component names, for diagnostics only.
//
// A component that cannot be decoded stops a window scan, and "data component
// 27" is a poor thing to hand someone at that moment. With a name it says which
// item property is in the way, and the fix is a capture plus a case.
//
// # Where these came from
//
// From the server itself. Running the jar with --reports writes a registries
// report, and minecraft:data_component_type in it lists every type with its
// protocol id. That is the same number the wire carries, so this is the
// mapping rather than an inference about it.
//
// An earlier version of this file read the order out of the jar's constant
// pool instead, and got the first eighty-one right before running off the end
// of the class into unrelated strings — "set", "this", "bootstrap",
// "metafactory". Nothing caught it, because nothing decoded that far. The
// twenty-nine real entries it was hiding are the entity variants below, and
// those are not hypothetical: a bucket of tropical fish carries three of them.
//
// Nothing branches on these names. They decorate an error and nothing more, so
// a rename in a future version is a confusing message, never a wrong decode.
var componentNames = map[int32]string{
	0:   "custom_data",
	1:   "max_stack_size",
	2:   "max_damage",
	3:   "damage",
	4:   "unbreakable",
	5:   "use_effects",
	6:   "custom_name",
	7:   "minimum_attack_charge",
	8:   "damage_type",
	9:   "item_name",
	10:  "item_model",
	11:  "lore",
	12:  "rarity",
	13:  "enchantments",
	14:  "can_place_on",
	15:  "can_break",
	16:  "attribute_modifiers",
	17:  "custom_model_data",
	18:  "tooltip_display",
	19:  "repair_cost",
	20:  "creative_slot_lock",
	21:  "enchantment_glint_override",
	22:  "intangible_projectile",
	23:  "food",
	24:  "consumable",
	25:  "use_remainder",
	26:  "use_cooldown",
	27:  "damage_resistant",
	28:  "tool",
	29:  "weapon",
	30:  "attack_range",
	31:  "enchantable",
	32:  "equippable",
	33:  "repairable",
	34:  "glider",
	35:  "tooltip_style",
	36:  "death_protection",
	37:  "blocks_attacks",
	38:  "piercing_weapon",
	39:  "kinetic_weapon",
	40:  "swing_animation",
	41:  "additional_trade_cost",
	42:  "stored_enchantments",
	43:  "dye",
	44:  "dyed_color",
	45:  "map_color",
	46:  "map_id",
	47:  "map_decorations",
	48:  "map_post_processing",
	49:  "charged_projectiles",
	50:  "bundle_contents",
	51:  "potion_contents",
	52:  "potion_duration_scale",
	53:  "suspicious_stew_effects",
	54:  "writable_book_content",
	55:  "written_book_content",
	56:  "trim",
	57:  "debug_stick_state",
	58:  "entity_data",
	59:  "bucket_entity_data",
	60:  "block_entity_data",
	61:  "instrument",
	62:  "provides_trim_material",
	63:  "ominous_bottle_amplifier",
	64:  "jukebox_playable",
	65:  "provides_banner_patterns",
	66:  "recipes",
	67:  "lodestone_tracker",
	68:  "firework_explosion",
	69:  "fireworks",
	70:  "profile",
	71:  "note_block_sound",
	72:  "banner_patterns",
	73:  "base_color",
	74:  "pot_decorations",
	75:  "container",
	76:  "block_state",
	77:  "bees",
	78:  "lock",
	79:  "container_loot",
	80:  "break_sound",
	81:  "villager/variant",
	82:  "wolf/variant",
	83:  "wolf/sound_variant",
	84:  "wolf/collar",
	85:  "fox/variant",
	86:  "salmon/size",
	87:  "parrot/variant",
	88:  "tropical_fish/pattern",
	89:  "tropical_fish/base_color",
	90:  "tropical_fish/pattern_color",
	91:  "mooshroom/variant",
	92:  "rabbit/variant",
	93:  "pig/variant",
	94:  "pig/sound_variant",
	95:  "cow/variant",
	96:  "cow/sound_variant",
	97:  "chicken/variant",
	98:  "chicken/sound_variant",
	99:  "zombie_nautilus/variant",
	100: "frog/variant",
	101: "horse/variant",
	102: "painting/variant",
	103: "llama/variant",
	104: "axolotl/variant",
	105: "cat/variant",
	106: "cat/sound_variant",
	107: "cat/collar",
	108: "sheep/color",
	109: "shulker/color",
}

// componentName renders a component type for an error message.
func componentName(kind int32) string {
	if name, ok := componentNames[kind]; ok {
		return fmt.Sprintf("%s (%d)", name, kind)
	}
	return fmt.Sprintf("%d, which is not a type this server version registers", kind)
}
