package nbt

import "testing"

// The walker steps through NBT sent by a server, and a server can send
// anything. It must answer "truncated" or a byte count for every input and
// never panic, because a panic here takes the whole client down from one
// malformed packet.
//
// Seeded with the shapes that actually turn up: an empty compound, a string, a
// nested list, and the arrays whose lengths are attacker-controlled.
func FuzzSkipTag(f *testing.F) {
	f.Add([]byte{0x00})                                                // TAG_End
	f.Add([]byte{0x0a, 0x00})                                          // empty compound
	f.Add([]byte{0x08, 0x00, 0x02, 'h', 'i'})                          // string
	f.Add([]byte{0x09, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 'x'}) // list of one string
	f.Add([]byte{0x07, 0x7f, 0xff, 0xff, 0xff})                        // byte array claiming 2 billion
	f.Add([]byte{0x0b, 0xff, 0xff, 0xff, 0xff})                        // int array with a negative length
	f.Add([]byte{0x0c, 0x7f, 0xff, 0xff, 0xff})                        // long array claiming 2 billion
	f.Add([]byte{0x0a, 0x0a, 0x0a, 0x0a, 0x0a, 0x0a})                  // compounds all the way down

	f.Fuzz(func(t *testing.T, data []byte) {
		n, err := SkipTag(data)
		if err != nil {
			return
		}
		// A successful walk must land inside the buffer. Reporting a length
		// past the end would send the caller reading someone else's bytes.
		if n < 0 || n > len(data) {
			t.Fatalf("SkipTag consumed %d of %d bytes", n, len(data))
		}
	})
}
