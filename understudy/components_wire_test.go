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
