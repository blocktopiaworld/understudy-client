package understudy

import (
	"testing"

	"github.com/blocktopia/understudy-client/protocol"
)

// Components nest through items: a container holds item stacks, an item stack
// holds components, and one of those components is a container again. Nothing
// bounded that until a fuzzer went looking.
//
// An eight-MiB packet — the largest this client accepts — buys about 1.2
// million layers. That does not quite overflow the stack, but it grows it to
// 1.2 million frames and spends a second there, and a little deeper is
// `fatal error: stack overflow`, which no recover catches. Real items nest one
// or two deep.
func TestNestedComponentsAreBounded(t *testing.T) {
	layer := []byte{0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x4b} // container holding an item holding a container
	var payload []byte
	for range componentDepth + 5 {
		payload = append(payload, layer...)
	}
	payload = append(payload, 0x00)

	err := skipComponent(testVersion(t), protocol.NewReader(payload), componentContainer, nil)
	if err == nil {
		t.Fatal("decoded components nested past the limit instead of refusing")
	}
	if !contains(err.Error(), "nested deeper than") {
		t.Errorf("error %q should say the nesting is what stopped it", err)
	}
}

// And the ordinary depth — a shulker box holding items — must still decode.
func TestOrdinaryNestingStillDecodes(t *testing.T) {
	// container: one slot, present, stone, count 5, no components.
	payload := []byte{0x01, 0x01, 0x01, 0x05, 0x00, 0x00}
	r := protocol.NewReader(payload)
	if err := skipComponent(testVersion(t), r, componentContainer, nil); err != nil {
		t.Fatalf("a shulker box with one item should decode: %v", err)
	}
	if left := len(r.Remaining()); left != 0 {
		t.Errorf("%d byte(s) left unread", left)
	}
}
