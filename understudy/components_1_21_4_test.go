package understudy

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// Component payloads captured from a vanilla 1.21.4 server.
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
// The ids here are 1.21.4's own wire ids, not the canonical ones, because that
// is what a 1.21.4 server sends and what the translation has to handle.
func TestComponentPayloadsDecodeWholeOn1_21_4(t *testing.T) {
	v, err := protocol.ByProtocol(769)
	if err != nil {
		t.Skipf("1.21.4 is not registered in this build: %v", err)
	}
	for _, tc := range []struct {
		wire    int32
		name    string
		samples []string
	}{
		{59, "banner_patterns", []string{"01270e", "02270e060b"}},
		{60, "base_color", []string{"0e"}},
		{62, "container", []string{"0105010000", "04050100000000091c0000"}},
		{57, "profile", []string{"01054e6f74636801069a79f444e94726a5befca90e38aaf501087465787475726573980365776f6749434a306157316c633352686258416949446f674d5463344e7a677a4e7a49354d546b794d69774b4943416963484a765a6d6c735a556c6b49694136494349774e6a6c684e7a6c6d4e4451305a546b304e7a4932595456695a575a6a59546b775a544d345957466d4e53497343694167496e427962325a706247564f5957316c4969413649434a4f6233526a6143497343694167496e4e705a323568644856795a564a6c63585670636d566b49694136494852796457557343694167496e526c65485231636d567a496941364948734b4943416749434a5453306c4f496941364948734b4943416749434167496e5679624349674f6941696148523063446f764c33526c65485231636d567a4c6d3170626d566a636d466d644335755a58517664475634644856795a5338794f5449774d446c684e446b794e5749314f4759774d6d4d334e3252685a474d7a5a574e6c5a6a41335a574530597a63304e7a4a6d4e6a526c4d475a6b597a4d79593255314e5449794e4467354d7a59794e6a677749676f674943416766516f674948304b66513d3d01ac057672335367714c6b5765594245464a2b3446495a6a6c7174374b4d496375595657477570324c6251483758324b57795552346e3558505364683151662f71736c786b376854433868426f6264372b68684e3978744c724c41303277564476523373564e6b444c6f7048305834462f746665674b734b45543571546a697042774950544544687972586564747270612f49314162545a474776415731463732554b6d4b37755967426e6e6b5151564379426f454464414e64796978516732746a67344c386e61413363437a657364622f53577735326a4d76457475394a706c524138522b7430465057644749705261454774335735554155493363685534414f6e743842486738756772774739412f627042474b2b5a7a77502f41684a3774434d3138666734414d4b2b6c354356394a78667753506a734c67532b4448556674584338374c353072487a68624b5637465149454d54754353636432586677417a7030703262682f327a4d6636504458615a6f73586a70506f51447a7946526c516e514f2f504e5a4e664d76695632656b6e4c7a5648723531685a71706c79714d2f4332597775614d634454736f657a756f75677755594b336e6a353277515532546e69665257724f544f5a6555534145352f48543462564170546177627232424e4e39716d6b4547725274765942574b76317841336f633071413261677a4c6f35713246333674326149752f545537763647543436686c6d5071534a494f5275456753542b3055737a374e43373861313876723239424d73525550544a736373367a38316c41747642684636594a3052795368376c50706b4a72354c3848315a5a4e4f617844365136534b6877466a6b44337456456536716e75304f71552f586c69532f367a4a4c74617747535541424a32726547456f34334478526d345a68616c556731795a5550494c343d"}},
		{56, "fireworks", []string{"0200"}},
		{39, "charged_projectiles", []string{"0101c1060000", "0201c1060000018b090000"}},
		{50, "instrument", []string{"05"}},
		{19, "enchantment_glint_override", []string{"01"}},
		{54, "lodestone_tracker", []string{"0001"}},
		{13, "attribute_modifiers", []string{"01000e6d696e6563726166743a746573744000000000000000000601"}},
		{45, "trim", []string{"050201"}},
		{40, "bundle_contents", []string{"0103010000", "0203010000071c0000"}},
		{43, "writable_book_content", []string{"010568656c6c6f00"}},
		{44, "written_book_content", []string{"015400026d65000108000270310000"}},
		{63, "block_state", []string{"0106666163696e67056e6f727468"}},
		{61, "pot_decorations", []string{"04ba07bd0aba07ba07"}},
		{51, "ominous_bottle_amplifier", []string{"03"}},
		{58, "note_block_sound", []string{"1f6d696e6563726166743a626c6f636b2e6e6f74655f626c6f636b2e62656c6c"}},
		{7, "item_model", []string{"116d696e6563726166743a6469616d6f6e64"}},
		{11, "can_place_on", []string{"01010209000001"}},
		{12, "can_break", []string{"0101030901000001"}},
		{21, "food", []string{"043f99999a01"}},
		{22, "consumable", []string{"3fcccccd01da040100"}},
		{23, "use_remainder", []string{"01be060000"}},
		{24, "use_cooldown", []string{"4020000000"}},
		{27, "enchantable", []string{"0f"}},
		{29, "repairable", []string{"02c406"}},
		{25, "damage_resistant", []string{"116d696e6563726166743a69735f66697265"}},
		{32, "death_protection", []string{"00"}},
		{28, "equippable", []string{"02820b000000010101"}},
		{35, "map_color", []string{"0012d687"}},
		{37, "map_decorations", []string{"0a0a000161050008726f746174696f6e42b40000060001783ff00000000000000600017a40000000000000000800047479706500106d696e6563726166743a706c617965720000"}},
		{64, "bees", []string{"010a0800026964000d6d696e6563726166743a626565000a64"}},
		{53, "recipes", []string{"090800000001000f6d696e6563726166743a737469636b"}},
		{48, "bucket_entity_data", []string{"0a0100044e6f41490100"}},
		{47, "entity_data", []string{"0a0800026964000d6d696e6563726166743a7069670100044e6f41490100"}},
		{49, "block_entity_data", []string{"0a0800026964000f6d696e6563726166743a636865737400"}},
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
