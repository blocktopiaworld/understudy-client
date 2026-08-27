package understudy

import (
	"encoding/hex"
	"testing"

	"github.com/blocktopia/understudy-client/protocol"
)

// Components are the most intricate thing the client decodes and the least
// documented, so they are the likeliest place for a malformed packet to find a
// sharp edge. Every input must produce a value or an error, never a panic:
// this runs inside the read loop, and a panic here ends the session.
//
// Seeded from real captures, so the fuzzer starts from valid shapes and mutates
// outward rather than spending its budget rediscovering what a component looks
// like.
func FuzzSkipComponent(f *testing.F) {
	seeds := []string{
		"01000e6d696e6563726166743a746573744000000000000000000600", // attribute_modifiers
		"0001054e6f746368000000000000",                             // profile
		"04050100000000091c0000",                                   // container
		"0a0800026964000d6d696e6563726166743a626565",               // entity nbt
		"0100000000", // potion contents
		"3e8000003fc00000003f800000000000003f800000000000", // blocks_attacks
		"",
	}
	for _, kind := range []int32{0, 3, 16, 51, 70, 75, 77, 109, 110} {
		for _, s := range seeds {
			b, err := hex.DecodeString(s)
			if err != nil {
				f.Fatalf("bad seed %q: %v", s, err)
			}
			f.Add(kind, b)
		}
	}

	v := testVersion(f)
	f.Fuzz(func(t *testing.T, kind int32, payload []byte) {
		r := protocol.NewReader(payload)
		// The contract is only that it returns. A wrong answer on garbage is
		// fine — the packet is garbage — but it must not take the process with
		// it, and it must not claim to have consumed more than it was given.
		if err := skipComponent(v, r, kind, nil); err != nil {
			return
		}
		if left := len(r.Remaining()); left > len(payload) {
			t.Fatalf("reader reports %d bytes left of a %d byte payload", left, len(payload))
		}
	})
}

// readSlot is the entry point every inventory packet goes through, and it walks
// a whole component list rather than one component.
func FuzzReadSlot(f *testing.F) {
	for _, s := range []string{
		"01010000",                           // one stone, no components
		"01c50401004904050100000000091c0000", // a shulker box with contents
		"0101010003250000",                   // damage
		"00",                                 // the empty slot
		"",
	} {
		b, err := hex.DecodeString(s)
		if err != nil {
			f.Fatalf("bad seed %q: %v", s, err)
		}
		f.Add(b)
	}
	v := testVersion(f)
	f.Fuzz(func(t *testing.T, payload []byte) {
		_, _ = readSlot(v, protocol.NewReader(payload))
	})
}
