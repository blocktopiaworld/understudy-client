package understudy

import (
	"encoding/hex"
	"testing"

	"github.com/blocktopia/understudy-client/protocol"
)

// Every payload here was cut out of a real window_items packet from a 26.1
// server, holding an item built with a known /item replace. The assertion is
// the one that matters for a format with no length prefix: the reader must
// land on the payload's last byte. One byte short or long and every slot after
// it in the packet decodes as nonsense, which is exactly the failure this
// whole file exists to prevent.
//
// Where a component has two samples they differ in width, because a single
// sample cannot tell a real count from a fixed width that happened to fit.
func TestComponentPayloadsDecodeWhole(t *testing.T) {
	v := testVersion(t)
	for _, tc := range []struct {
		kind    int32
		name    string
		note    string
		samples []string
	}{
		{16, "attribute_modifiers",
			"a chestplate with one +2 armour modifier; the trailing 00 is the display mode",
			[]string{"01000e6d696e6563726166743a746573744000000000000000000600"}},
		{21, "enchantment_glint_override",
			"forced on",
			[]string{"01"}},
		{49, "charged_projectiles",
			"a crossbow with one arrow, and one with an arrow and a rocket",
			[]string{"018007010000", "028007010000db09010000"}},
		{50, "bundle_contents",
			"a bundle of stone, and one of stone and dirt",
			[]string{"0101030000", "02010300001c070000"}},
		{52, "potion_duration_scale",
			"0.5 as a float32",
			[]string{"3f000000"}},
		{54, "writable_book_content",
			"one page reading hello, with no filtered version",
			[]string{"010568656c6c6f00"}},
		{55, "written_book_content",
			"title T, author me, one page p1 as a text component",
			[]string{"015400026d65000108000270310000"}},
		{56, "trim",
			"gold and coast, holders 5 and 2 for registry indices 4 and 1",
			[]string{"0502"}},
		{61, "instrument",
			"sing_goat_horn, holder 7 for index 6",
			[]string{"07"}},
		{63, "ominous_bottle_amplifier",
			"amplifier 3",
			[]string{"03"}},
		{64, "jukebox_playable",
			"pigstep",
			[]string{"0e"}},
		{67, "lodestone_tracker",
			"no target; and 9,9,9 in the nether, tracked",
			[]string{"0001", "01136d696e6563726166743a6f766572776f726c64000000400000300200", "01146d696e6563726166743a7468655f6e6574686572000002400000900901"}},
		{68, "firework_explosion",
			"a burst that fades from red to blue, twinkling",
			[]string{"0401000000ff010000ff000001"}},
		{69, "fireworks",
			"flight 2 with no bursts; flight 3 with one",
			[]string{"0200", "0301000100ff000001000000ff0100"}},
		{70, "profile",
			"named heads, one with a uuid, and properties with and without a signature",
			[]string{"000103426f62000000000000", "0001054e6f746368000000000000", "0100000001000000020000000300000004054e6f7463680000000000", "0001054e6f7463680001087465787475726573036162630000000000", "0001054e6f746368000108746578747572657303616263010373696700000000"}},
		{71, "note_block_sound",
			"block.note_block.bell",
			[]string{"1f6d696e6563726166743a626c6f636b2e6e6f74655f626c6f636b2e62656c6c"}},
		{72, "banner_patterns",
			"one stripe_top in red; and that plus a cross in blue",
			[]string{"01270e", "02270e060b"}},
		{73, "base_color",
			"red",
			[]string{"0e"}},
		{74, "pot_decorations",
			"brick, angler sherd, brick, brick",
			[]string{"048208a60b82088208"}},
		{75, "container",
			"a shulker of stone in slot 0; and one with dirt in slot 3, sent as four dense slots",
			[]string{"010101050000", "0401010500000000011c090000"}},
		{76, "block_state",
			"facing=north",
			[]string{"0106666163696e67056e6f727468"}},
		{7, "minimum_attack_charge",
			"0.9",
			[]string{"3f666666"}},
		{8, "damage_type",
			"minecraft:generic as a holder",
			[]string{"12"}},
		{10, "item_model",
			"points at the diamond model",
			[]string{"116d696e6563726166743a6469616d6f6e64"}},
		{14, "can_place_on",
			"placeable on dirt",
			[]string{"0101020900000000"}},
		{15, "can_break",
			"breaks dirt and stone, two blocks in one holder set",
			[]string{"010103090100000000"}},
		{18, "tooltip_display",
			"shown, with damage hidden",
			[]string{"000103"}},
		{22, "intangible_projectile",
			"an empty map, which is a nameless empty compound",
			[]string{"0a00"}},
		{23, "food",
			"nutrition 4, saturation 1.2, always edible",
			[]string{"043f99999a01"}},
		{24, "consumable",
			"1.6 seconds, the eat animation, a sound, particles, no effects",
			[]string{"3fcccccd01b9050100"}},
		{25, "use_remainder",
			"leaves a bowl behind",
			[]string{"fd06010000"}},
		{26, "use_cooldown",
			"2.5 seconds with no shared group",
			[]string{"4020000000"}},
		{27, "damage_resistant",
			"resistant to the #minecraft:is_fire tag",
			[]string{"00116d696e6563726166743a69735f66697265"}},
		{28, "tool",
			"no rules; and one rule for dirt at speed 5.0 counting for drops",
			[]string{"003f8000000101", "0102090140a0000001013fc000000201"}},
		{29, "weapon",
			"2 damage per attack, blocking disabled for 1.0s",
			[]string{"023f800000"}},
		{30, "attack_range",
			"all six defaults",
			[]string{"00000000404000000000000040a000003e99999a3f800000"}},
		{31, "enchantable",
			"enchantability 15",
			[]string{"0f"}},
		{32, "equippable",
			"slot head with everything absent; a chest slot naming its asset, overlay and allowed entities",
			[]string{"04470000000101010000a00b", "02c10c0000000101010000a00b", "034701116d696e6563726166743a6c656174686572011a6d696e6563726166743a6d6973632f70756d706b696e626c75720102640000000100a00b"}},
		{33, "repairable",
			"repaired with diamond",
			[]string{"028307"}},
		{34, "glider",
			"present, and that is the whole meaning",
			[]string{""}},
		{35, "tooltip_style",
			"minecraft:fancy",
			[]string{"0f6d696e6563726166743a66616e6379"}},
		{36, "death_protection",
			"no effects on being saved",
			[]string{"00"}},
		{37, "blocks_attacks",
			"0.25 delay, 1.5 disable scale, no reductions, three absent sounds",
			[]string{"3e8000003fc00000003f800000000000003f800000000000"}},
		{45, "map_color",
			"tinted 1234567",
			[]string{"0012d687"}},
		{47, "map_decorations",
			"one player marker",
			[]string{"0a0a000161050008726f746174696f6e42b40000060001783ff00000000000000600017a40000000000000000800047479706500106d696e6563726166743a706c617965720000"}},
		{58, "entity_data",
			"a pig spawn egg: type 100, then NoAI",
			[]string{"640a0100044e6f41490100"}},
		{59, "bucket_entity_data",
			"a bucket of cod carrying NoAI, with no type prefix",
			[]string{"0a0100044e6f41490100"}},
		{60, "block_entity_data",
			"a chest, type 1, with nothing else",
			[]string{"010a00"}},
		{62, "provides_trim_material",
			"gold",
			[]string{"05"}},
		{65, "provides_banner_patterns",
			"the #minecraft:pattern_item/globe tag",
			[]string{"001c6d696e6563726166743a7061747465726e5f6974656d2f676c6f6265"}},
		{66, "recipes",
			"a knowledge book holding minecraft:stick",
			[]string{"090800000001000f6d696e6563726166743a737469636b"}},
		{77, "bees",
			"one bee, type 11, 10 ticks in the hive of a minimum 100",
			[]string{"010b0a000a64"}},
		{80, "break_sound",
			"a sound holder",
			[]string{"b90c"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, s := range tc.samples {
				payload, err := hex.DecodeString(s)
				if err != nil {
					t.Fatalf("bad sample: %v", err)
				}
				r := protocol.NewReader(payload)
				if err := skipComponent(v, r, tc.kind, nil); err != nil {
					t.Errorf("%s (%s): %v", tc.name, tc.note, err)
					continue
				}
				if left := len(r.Remaining()); left != 0 {
					t.Errorf("%s (%s): %d of %d byte(s) left unread — "+
						"the payload must be consumed exactly",
						tc.name, tc.note, left, len(payload))
				}
			}
		})
	}
}

// The two places this decoder refuses rather than guesses. Both are reachable
// from a real server — a datapack that defines a banner pattern inline, or a
// head carrying whatever those four trailing bytes are — and both would
// desynchronise the rest of the packet if they were skipped by a fixed width.
func TestComponentsRefuseWhatTheyCannotSkip(t *testing.T) {
	v := testVersion(t)
	for _, tc := range []struct {
		name    string
		kind    int32
		payload string
		want    string
	}{
		{
			"a banner pattern defined inline rather than named",
			componentBannerPatterns, "0100",
			"defined inline",
		},
		{
			"a profile with one of its trailing fields set",
			componentProfile, "000103426f620000000001",
			"never been seen set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := hex.DecodeString(tc.payload)
			if err != nil {
				t.Fatalf("bad sample: %v", err)
			}
			err = skipComponent(v, protocol.NewReader(payload), tc.kind, nil)
			if err == nil {
				t.Fatal("decoded something it has no encoding for, which " +
					"silently corrupts every slot after it")
			}
			if !contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q", err, tc.want)
			}
		})
	}
}
