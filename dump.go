package understudy

import (
	"os"
	"sync"
)

// dumpChunkBlob writes the first raw chunkData payload to the path in
// UNDERSTUDY_DUMP_CHUNK, if set.
//
// Chunk framing is the one part of this protocol that cannot be reasoned out
// from a field list: a mistake shows up as a short read many sections later,
// nowhere near the actual error. Having the real bytes to measure against
// turns that into arithmetic.
var dumpOnce sync.Once

func dumpChunkBlob(blob []byte) {
	path := os.Getenv("UNDERSTUDY_DUMP_CHUNK")
	if path == "" {
		return
	}
	dumpOnce.Do(func() {
		// The path comes from UNDERSTUDY_DUMP_CHUNK, set by whoever is running
		// the bot; there is no untrusted input here.
		_ = os.WriteFile(path, blob, 0o644) //nolint:gosec // G703: operator-supplied debug path
	})
}
