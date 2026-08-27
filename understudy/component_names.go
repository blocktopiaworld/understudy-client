package understudy

import "fmt"

// Component names, for diagnostics only.
//
// A component that cannot be decoded stops a window scan, and "data component
// 27" is a poor thing to hand someone at that moment. With a name it says
// which item property is in the way, and the fix is a capture plus a case.
//
// # Where these came from, and why they are only used for messages
//
// There is no published list. minecraft-data carries none, and the server does
// not enumerate them on a bad command. These were read out of the server jar:
// net/minecraft/core/component/DataComponents registers them in order, and the
// class constant pool preserves that order, so the index *is* the wire id.
//
// Checked against the seven established from live captures — damage 3,
// custom_name 6, enchantments 13, stored_enchantments 42, map_id 46,
// potion_contents 51, suspicious_stew_effects 53 — all of which land exactly.
//
// That is good evidence, not a guarantee, and the order will shift whenever
// Mojang inserts a component. So nothing branches on these: they decorate an
// error and nothing more. A wrong name in a future version is a confusing
// message, never a wrong decode.
var componentNames = map[int32]string{
	0:  "custom_data",
	1:  "max_stack_size",
	2:  "max_damage",
	3:  "damage",
	4:  "unbreakable",
	5:  "use_effects",
	6:  "custom_name",
	7:  "minimum_attack_charge",
	8:  "damage_type",
	9:  "item_name",
	10: "item_model",
	11: "lore",
	12: "rarity",
	13: "enchantments",
	14: "can_place_on",
	15: "can_break",
	16: "attribute_modifiers",
	17: "custom_model_data",
	18: "tooltip_display",
	19: "repair_cost",
	20: "creative_slot_lock",
	21: "enchantment_glint_override",
	22: "intangible_projectile",
	23: "food",
	24: "consumable",
	25: "use_remainder",
	26: "use_cooldown",
	27: "damage_resistant",
	28: "tool",
	29: "weapon",
	30: "attack_range",
	31: "enchantable",
	32: "equippable",
	33: "repairable",
	34: "glider",
	35: "tooltip_style",
	36: "death_protection",
	37: "blocks_attacks",
	38: "piercing_weapon",
	39: "kinetic_weapon",
	40: "swing_animation",
	41: "additional_trade_cost",
	42: "stored_enchantments",
	43: "dye",
	44: "dyed_color",
	45: "map_color",
	46: "map_id",
	47: "map_decorations",
	48: "map_post_processing",
	49: "charged_projectiles",
	50: "bundle_contents",
	51: "potion_contents",
	52: "potion_duration_scale",
	53: "suspicious_stew_effects",
	54: "writable_book_content",
	55: "written_book_content",
	56: "trim",
	57: "debug_stick_state",
	58: "entity_data",
	59: "bucket_entity_data",
	60: "block_entity_data",
	61: "instrument",
	62: "provides_trim_material",
	63: "ominous_bottle_amplifier",
	64: "jukebox_playable",
	65: "provides_banner_patterns",
	66: "recipes",
	67: "lodestone_tracker",
	68: "firework_explosion",
	69: "fireworks",
	70: "profile",
	71: "note_block_sound",
	72: "banner_patterns",
	73: "base_color",
	74: "pot_decorations",
	75: "container",
	76: "block_state",
	77: "bees",
	78: "lock",
	79: "container_loot",
	80: "break_sound",
}

// componentName describes a component id for an error message.
func componentName(kind int32) string {
	if n, ok := componentNames[kind]; ok {
		return fmt.Sprintf("%d (%s)", kind, n)
	}
	return fmt.Sprintf("%d", kind)
}
