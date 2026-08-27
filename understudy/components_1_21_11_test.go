package understudy

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/blocktopia/understudy-client/protocol"
)

// Component payloads captured from a vanilla 1.21.11 server.
//
// The same discipline as components_wire_test.go and for the same reason: with
// no length prefix, the only check that a reading is right is that it consumes
// the payload exactly. What is different here is how the payloads were
// delimited. Replaying items and watching for a truncated window finds that
// something is wrong but not where, so each probe item was placed with a
// bedrock sentinel behind it — a header that cannot be mistaken for anything
// else — which bounds every payload on both sides instead of inferring it.
//
// That is what turned "nine components differ" into three encoding rules. Of
// the sixty-seven components put on both versions, fifty-one are byte for byte
// identical; the rest differ only in nested stack order, in whether a tag is
// wrapped, and in a leading flag on six registry references.
//
// The ids here are 1.21.11's own wire ids, not the canonical ones, because that
// is what a 1.21.11 server sends and what the translation has to handle.
func TestComponentPayloadsDecodeWholeOn1_21_11(t *testing.T) {
	v, err := protocol.ByProtocol(774)
	if err != nil {
		t.Skipf("1.21.11 is not registered in this build: %v", err)
	}
	for _, tc := range []struct {
		wire    int32
		name    string
		samples []string
	}{
		{70, "banner_patterns", []string{"01270e", "02270e060b"}},
		{71, "base_color", []string{"0e"}},
		{73, "container", []string{"0105010000", "04050100000000091c0000"}},
		{68, "profile", []string{"0001054e6f746368000000000000"}},
		{67, "fireworks", []string{"0200"}},
		{47, "charged_projectiles", []string{"0101ff060000", "0201ff06000001da090000"}},
		{59, "instrument", []string{"0105"}},
		{62, "jukebox_playable", []string{"0105"}},
		{21, "enchantment_glint_override", []string{"01"}},
		{65, "lodestone_tracker", []string{"0001"}},
		{16, "attribute_modifiers", []string{"01000e6d696e6563726166743a746573744000000000000000000600"}},
		{54, "trim", []string{"0502"}},
		{48, "bundle_contents", []string{"0103010000", "0203010000071c0000"}},
		{52, "writable_book_content", []string{"010568656c6c6f00"}},
		{53, "written_book_content", []string{"015400026d65000108000270310000"}},
		{74, "block_state", []string{"0106666163696e67056e6f727468"}},
		{72, "pot_decorations", []string{"048108a50b81088108"}},
		{61, "ominous_bottle_amplifier", []string{"03"}},
		{69, "note_block_sound", []string{"1f6d696e6563726166743a626c6f636b2e6e6f74655f626c6f636b2e62656c6c"}},
		{50, "potion_duration_scale", []string{"3f000000"}},
		{10, "item_model", []string{"116d696e6563726166743a6469616d6f6e64"}},
		{18, "tooltip_display", []string{"000103"}},
		{23, "food", []string{"043f99999a01"}},
		{24, "consumable", []string{"3fcccccd019c050100"}},
		{25, "use_remainder", []string{"01fc060000"}},
		{26, "use_cooldown", []string{"4020000000"}},
		{31, "enchantable", []string{"0f"}},
		{33, "repairable", []string{"028207"}},
		{27, "damage_resistant", []string{"116d696e6563726166743a69735f66697265"}},
		{36, "death_protection", []string{"00"}},
		{14, "can_place_on", []string{"0101020900000000"}},
		{15, "can_break", []string{"010103090100000000"}},
		{28, "tool", []string{"003f8000000101", "0102090140a0000001013fc000000201"}},
		{29, "weapon", []string{"023f800000"}},
		{32, "equippable", []string{"04470000000101010000e70a", "02880c0000000101010000e70a"}},
		{22, "intangible_projectile", []string{"0a00"}},
		{35, "tooltip_style", []string{"0f6d696e6563726166743a66616e6379"}},
		{78, "break_sound", []string{"800c"}},
		{63, "provides_banner_patterns", []string{"1c6d696e6563726166743a7061747465726e5f6974656d2f676c6f6265"}},
		{60, "provides_trim_material", []string{"0105"}},
		{7, "minimum_attack_charge", []string{"3f666666"}},
		{30, "attack_range", []string{"00000000404000000000000040a000003e99999a3f800000"}},
		{37, "blocks_attacks", []string{"3e8000003fc00000003f800000000000003f800000000000"}},
		{43, "map_color", []string{"0012d687"}},
		{45, "map_decorations", []string{"0a0a000161050008726f746174696f6e42b40000060001783ff00000000000000600017a40000000000000000800047479706500106d696e6563726166743a706c617965720000"}},
		{75, "bees", []string{"010b0a000a64"}},
		{64, "recipes", []string{"090800000001000f6d696e6563726166743a737469636b"}},
		{57, "bucket_entity_data", []string{"0a0100044e6f41490100"}},
		{56, "entity_data", []string{"640a0100044e6f41490100"}},
		{58, "block_entity_data", []string{"010a00"}},
		{8, "damage_type", []string{"0112"}},
		{39, "kinetic_weapon", []string{"0a00000000000000003f8000000001bb0b", "0a0c0132413000000000000001870140a333330000000001e10100000000409333333ec28f5c3f73333301ba0b01bb0b"}},
		{79, "villager/variant", []string{"02"}},
		{80, "wolf/variant", []string{"03"}},
		{81, "wolf/sound_variant", []string{"02"}},
		{82, "wolf/collar", []string{"0e"}},
		{83, "fox/variant", []string{"00"}},
		{84, "salmon/size", []string{"00"}},
		{85, "parrot/variant", []string{"00"}},
		{86, "tropical_fish/pattern", []string{"00"}},
		{87, "tropical_fish/base_color", []string{"0e", "00"}},
		{88, "tropical_fish/pattern_color", []string{"0b", "01"}},
		{89, "mooshroom/variant", []string{"00", "01"}},
		{94, "zombie_nautilus/variant", []string{"0101"}},
		{90, "rabbit/variant", []string{"63"}},
		{91, "pig/variant", []string{"00"}},
		{92, "cow/variant", []string{"00"}},
		{93, "chicken/variant", []string{"0100"}},
		{95, "frog/variant", []string{"00"}},
	} {
		t.Run(fmt.Sprintf("%d_%s", tc.wire, tc.name), func(t *testing.T) {
			for _, s := range tc.samples {
				payload, err := hex.DecodeString(s)
				if err != nil {
					t.Fatalf("bad sample: %v", err)
				}
				r := protocol.NewReader(payload)
				if err := skipComponent(v, r, tc.wire, nil); err != nil {
					t.Errorf("%s: %v", tc.name, err)
					continue
				}
				if left := len(r.Remaining()); left != 0 {
					t.Errorf("%s: %d of %d byte(s) left unread", tc.name, left, len(payload))
				}
			}
		})
	}
}
