package understudy

import "strings"

// readableText salvages the human-readable parts of an NBT text component.
//
// Disconnect reasons arrive as NBT, and decoding NBT properly would drag in a
// whole type system for the sake of one error string. But a kick message is
// the most important thing this client ever reports — "Flying is not enabled
// on this server" is the difference between a five-minute fix and an hour of
// confusion — so it cannot be allowed to print as binary noise.
//
// NBT stores its strings as plain length-prefixed UTF-8 with no escaping, so
// the readable content survives intact between the tag bytes. Pulling out the
// printable runs recovers things like "translate" and
// "multiplayer.disconnect.flying" without any structural parsing.
func readableText(data []byte) string {
	const minRun = 4

	var parts []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() >= minRun {
			parts = append(parts, cur.String())
		}
		cur.Reset()
	}
	for _, b := range data {
		// Printable ASCII only. Text components are effectively ASCII in
		// practice, and admitting high bytes would let NBT tag headers back in
		// as mojibake.
		if b >= 0x20 && b < 0x7f {
			cur.WriteByte(b)
			continue
		}
		flush()
	}
	flush()

	if len(parts) == 0 {
		return "(unreadable reason)"
	}
	return strings.Join(parts, " ")
}
