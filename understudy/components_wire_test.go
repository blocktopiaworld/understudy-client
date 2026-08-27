package understudy

import (
	"encoding/hex"
	"fmt"
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
		{5, "use_effects",
			"cannot sprint, vibrates, quarter speed",
			[]string{"00013e800000"}},
		{38, "piercing_weapon",
			"empty, and with both sounds — which is what makes them optional",
			[]string{"01000000", "010001f50b01f40b"}},
		{39, "kinetic_weapon",
			"defaults; each condition block alone; each sound alone; and a whole spear",
			[]string{"0a0700000000000000400000000000", "0a070000003f000000400000000000", "0a07000000000000003f8000000000", "0a00000000000000003f8000000000", "0a00000000000000003f80000001f30b00", "0a00000000000000003f8000000001f40b", "0a00013241300000000000000000000000003f8000000000", "0a00000001e1010000000040933333000000003f8000000000", "0a000001870140a333330000000000000000003f8000000000", "0a0c0132413000000000000001870140a333330000000001e10100000000409333333ec28f5c3f73333301f30b01f40b"}},
		{40, "swing_animation",
			"a seven-tick stab",
			[]string{"0207"}},
		{43, "dye",
			"a leather helmet dyed red (14), and light blue (3)",
			[]string{"0e", "03"}},
		{57, "debug_stick_state",
			"empty, and oak_log/axis",
			[]string{"0a0800116d696e6563726166743a6f616b5f6c6f6700046178697300"}},
		{78, "lock",
			"empty, and locked to a diamond",
			[]string{"0a00", "0a0800056974656d7300116d696e6563726166743a6469616d6f6e6400"}},
		{79, "container_loot",
			"a dungeon chest, and one with a seed",
			[]string{"0a08000a6c6f6f745f7461626c65001f6d696e6563726166743a6368657374732f73696d706c655f64756e67656f6e00", "0a04000473656564000000000000002a08000a6c6f6f745f7461626c6500246d696e6563726166743a6368657374732f6162616e646f6e65645f6d696e65736861667400"}},
		{81, "villager/variant",
			"a plain registry id, not a holder",
			[]string{"00", "02"}},
		{82, "wolf/variant",
			"a plain registry id, not a holder",
			[]string{"03", "06"}},
		{83, "wolf/sound_variant",
			"a plain registry id, not a holder",
			[]string{"01", "02"}},
		{84, "wolf/collar",
			"a plain registry id, not a holder",
			[]string{"0e", "0b"}},
		{85, "fox/variant",
			"a plain registry id, not a holder",
			[]string{"00", "01"}},
		{86, "salmon/size",
			"a plain registry id, not a holder",
			[]string{"00", "02"}},
		{87, "parrot/variant",
			"a plain registry id, not a holder",
			[]string{"00", "02"}},
		{88, "tropical_fish/pattern",
			"a plain registry id, not a holder",
			[]string{"00", "8108"}},
		{89, "tropical_fish/base_color",
			"a plain registry id, not a holder",
			[]string{"0e", "00"}},
		{90, "tropical_fish/pattern_color",
			"a plain registry id, not a holder",
			[]string{"01", "0b"}},
		{91, "mooshroom/variant",
			"a plain registry id, not a holder",
			[]string{"00", "01"}},
		{92, "rabbit/variant",
			"a plain registry id, not a holder",
			[]string{"00", "63"}},
		{93, "pig/variant",
			"a plain registry id, not a holder",
			[]string{"01", "00"}},
		{94, "pig/sound_variant",
			"a plain registry id, not a holder",
			[]string{"01", "02"}},
		{95, "cow/variant",
			"a plain registry id, not a holder",
			[]string{"01", "00"}},
		{96, "cow/sound_variant",
			"a plain registry id, not a holder",
			[]string{"01", "00"}},
		{97, "chicken/variant",
			"a plain registry id, not a holder",
			[]string{"01", "00"}},
		{98, "chicken/sound_variant",
			"a plain registry id, not a holder",
			[]string{"00"}},
		{99, "zombie_nautilus/variant",
			"a plain registry id, not a holder",
			[]string{"00", "01"}},
		{100, "frog/variant",
			"a plain registry id, not a holder",
			[]string{"01", "00"}},
		{101, "horse/variant",
			"a plain registry id, not a holder",
			[]string{"00", "04"}},
		{102, "painting/variant",
			"a plain registry id, not a holder",
			[]string{"19"}},
		{103, "llama/variant",
			"a plain registry id, not a holder",
			[]string{"00"}},
		{104, "axolotl/variant",
			"a plain registry id, not a holder",
			[]string{"00"}},
		{105, "cat/variant",
			"a plain registry id, not a holder",
			[]string{"09"}},
		{106, "cat/sound_variant",
			"a plain registry id, not a holder",
			[]string{"00"}},
		{107, "cat/collar",
			"a plain registry id, not a holder",
			[]string{"0e"}},
		{108, "sheep/color",
			"a plain registry id, not a holder",
			[]string{"0e"}},
		{109, "shulker/color",
			"a plain registry id, not a holder",
			[]string{"0e"}},
	} {
		t.Run(fmt.Sprintf("%d_%s", tc.kind, tc.name), func(t *testing.T) {
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
		t.Run(fmt.Sprintf("%d_%s", tc.kind, tc.name), func(t *testing.T) {
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

// Every type the server registers must be either decoded or deliberately not.
//
// The point is the second half. It is easy to add a component and easy to
// forget one, and a forgotten one does not fail loudly here — it fails in a
// window scan months later, as a bot that cannot see its own inventory. So the
// exceptions are named, and a new id appearing outside that list fails now.
//
// The three named below hold registry ids without reaching an item: the command
// refuses to set them, and none of the 1506 items in the server's own component
// report carries one.
func TestEveryRegisteredComponentIsAccountedFor(t *testing.T) {
	v := testVersion(t)
	unhandled := map[int32]string{
		20: "creative_slot_lock",
		41: "additional_trade_cost",
		48: "map_post_processing",
	}
	for kind := int32(0); kind <= componentLastEntityVariant; kind++ {
		// An empty payload: anything with a known encoding fails trying to read
		// its first field, and anything without says so by name instead.
		err := skipComponent(v, protocol.NewReader(nil), kind, nil)
		missing := err != nil && contains(err.Error(), "has no known encoding")

		if name, expected := unhandled[kind]; expected {
			if !missing {
				t.Errorf("component %d (%s) is now decoded — remove it from the "+
					"exception list and give it a payload sample", kind, name)
			}
			continue
		}
		if missing {
			t.Errorf("component %d (%s) has no encoding and is not a named "+
				"exception: %v", kind, componentName(kind), err)
		}
	}
}

// The names are the registry's, so a gap in them means the report and the code
// have drifted apart.
func TestComponentNamesCoverTheRegistry(t *testing.T) {
	if got := len(componentNames); got != 110 {
		t.Errorf("componentNames has %d entries, want the 110 the 26.1 registry "+
			"report lists", got)
	}
	for kind := int32(0); kind <= componentLastEntityVariant; kind++ {
		if _, ok := componentNames[kind]; !ok {
			t.Errorf("component %d has no name, so an error about it would be a "+
				"bare number", kind)
		}
	}
	// The four the constant-pool reading got wrong, which nothing caught until
	// the registry report was generated.
	for kind, want := range map[int32]string{
		81: "villager/variant", 82: "wolf/variant",
		83: "wolf/sound_variant", 84: "wolf/collar",
	} {
		if got := componentNames[kind]; got != want {
			t.Errorf("component %d is named %q, want %q", kind, got, want)
		}
	}
}

// The component ids belong to one version, and using one version's on another
// is the failure this whole file cannot detect from the bytes: they are dense
// registry indices, so a payload of the wrong shape still decodes, just wrongly.
//
// A version with no table at all has to stop the scan rather than fall back on
// somebody else's numbering.
func TestComponentsRefuseAVersionWhoseIDsAreUnknown(t *testing.T) {
	unchecked := protocol.NewVersion(protocol.VersionSpec{
		Name:     "some-future-version",
		Protocol: 999,
		Packets:  testPackets(t),
	})
	// componentDamage is about as safe as a component gets — one VarInt, and
	// present on every used tool. It must still be refused.
	payload := []byte{37}
	err := skipComponent(unchecked, protocol.NewReader(payload), componentDamage, nil)
	if err == nil {
		t.Fatal("decoded a component on a version with no id table, which reads " +
			"the wrong shape and desynchronises the rest of the packet")
	}
	for _, want := range []string{"some-future-version", "not been established", "gencomponents"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// The other half: the version the table *was* built against must not be
// refused, or every item everywhere stops decoding.
func TestComponentsAcceptTheVersionTheyWereBuiltFor(t *testing.T) {
	v, err := protocol.ByProtocol(775)
	if err != nil {
		t.Skipf("26.1 is not registered in this build: %v", err)
	}
	if !v.HasComponentIDs() {
		t.Fatal("26.1 is the version the component shapes were read from and must " +
			"carry an id table")
	}
	if err := skipComponent(v, protocol.NewReader([]byte{37}), componentDamage, nil); err != nil {
		t.Errorf("26.1 refused a damage component: %v", err)
	}
}

// Knowing a version's component ids is not the same as being able to read its
// components, and this is the test that keeps the two apart.
//
// 1.21.11's ids are known — generated from its own registries report — and its
// payloads still differ from 26.1's. Measured against a vanilla 1.21.11 server
// by replaying every item the 26.1 work was built from, nine differ:
//
//	container            an item nested in a component is count-first there
//	                     and id-first on 26.1, and an empty slot is a zero
//	                     count rather than an absent optional
//	instrument           a registry reference is two bytes, not one — and the
//	jukebox_playable     same for damage_type, provides_trim_material and the
//	chicken/variant      entity variants
//	damage_resistant     a tag is a bare string, where 26.1 prefixes it as a
//	                     holder set — likewise provides_banner_patterns
//
// Every one of those reads a different number of bytes, so promoting 1.21.11 on
// the strength of its id table alone would desynchronise windows silently.
func TestKnowingIDsIsNotEnoughToDecode(t *testing.T) {
	v, err := protocol.ByProtocol(774) // 1.21.11
	if err != nil {
		t.Skipf("1.21.11 is not registered in this build: %v", err)
	}
	if !v.HasComponentIDs() {
		t.Error("1.21.11's component ids were generated from its registries " +
			"report and should be present")
	}
	if v.CanonicalComponents() {
		t.Fatal("1.21.11 is marked as sharing 26.1's component encodings, but " +
			"nine of them differ — see this test's comment")
	}
	// The wire id for damage, which 1.21.11 does have.
	wire, ok := func() (int32, bool) {
		for w := int32(0); w < 200; w++ {
			if kind, ok := v.ComponentKind(w); ok && kind == componentDamage {
				return w, true
			}
		}
		return 0, false
	}()
	if !ok {
		t.Fatal("1.21.11 has no id mapping to damage, so its table is incomplete")
	}
	err = skipComponent(v, protocol.NewReader([]byte{37}), wire, nil)
	if err == nil {
		t.Fatal("decoded a 1.21.11 component with 26.1's payload shapes")
	}
	if !contains(err.Error(), "not encoded the way") {
		t.Errorf("error %q should say the encodings differ, not that the id is "+
			"unknown — the id is known", err)
	}
}
