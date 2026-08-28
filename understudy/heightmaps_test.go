package understudy

import (
	"encoding/binary"
	"testing"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// The heightmaps sit between the chunk coordinates and the chunk data, and
// their shape changed in 1.21.5. Nothing here reads them, but walking them with
// the wrong shape puts the data blob at the wrong offset — and the client then
// dies inside a *later* section with "short read: want 8 bytes at offset
// 44767, have 3", which points nowhere near the mistake.
//
// That is exactly how 1.21.4 failed against a real server: the walk assumed the
// post-1.21.5 prefixed array, read a VarInt out of the middle of an NBT
// compound, and desynchronised everything after it.

// chunkFormatVersion builds a version that differs only in chunk framing.
func chunkFormatVersion(t *testing.T, format protocol.ChunkFormat) *protocol.Version {
	t.Helper()
	return protocol.NewVersion(protocol.VersionSpec{
		Name:     "chunkfmt",
		Protocol: 9999,
		Chunk:    format,
		Packets:  testPackets(t),
		Air:      [][2]int32{{stateAir, stateAir}},
		Solid:    [][2]int32{{stateStone, stateStone}},
	})
}

// nbtHeightmaps encodes the pre-1.21.5 shape: one nameless compound of named
// long arrays, 37 longs each, as a real server sends.
func nbtHeightmaps() []byte {
	out := []byte{10} // TAG_Compound, nameless
	for _, name := range []string{"MOTION_BLOCKING", "WORLD_SURFACE"} {
		out = append(out, 12) // TAG_Long_Array
		out = binary.BigEndian.AppendUint16(out, uint16(len(name)))
		out = append(out, name...)
		out = binary.BigEndian.AppendUint32(out, 37)
		out = append(out, make([]byte, 37*8)...)
	}
	return append(out, 0) // TAG_End
}

// arrayHeightmaps encodes the 1.21.5+ shape: a prefixed array of
// {VarInt type, prefixed array of long}.
func arrayHeightmaps() []byte {
	w := protocol.NewWriter(0).VarInt(2)
	for range 2 {
		w.VarInt(1).VarInt(37)
		for range 37 {
			w.I64(0)
		}
	}
	// NewWriter prefixes a packet ID; drop it, this is a fragment.
	return w.Bytes()[1:]
}

// chunkBlob builds 24 uniform sections of one block state.
func chunkBlob(state int32, format protocol.ChunkFormat) []byte {
	w := protocol.NewWriter(0)
	for range 24 {
		w.I16(4096) // solid block count
		if format.HasFluidCount {
			w.I16(0)
		}
		// block states: uniform container
		w.U8(0).VarInt(state)
		if format.HasSizePrefix {
			w.U8(0)
		}
		// biomes: uniform container
		w.U8(0).VarInt(0)
		if format.HasSizePrefix {
			w.U8(0)
		}
	}
	return w.Bytes()[1:]
}

// mapChunkPacket assembles the whole clientbound packet.
func mapChunkPacket(v *protocol.Version, chunkX, chunkZ int32, heightmaps, blob []byte) protocol.Packet {
	w := protocol.NewWriter(v.Packets.CBPlayMapChunk).I32(chunkX).I32(chunkZ)
	body := append(append([]byte{}, w.Bytes()...), heightmaps...)
	tail := protocol.NewWriter(0).VarInt(int32(len(blob))).Bytes()[1:]
	body = append(append(body, tail...), blob...)
	// Something after the chunk data, so a decoder that overruns the blob is
	// not accidentally correct.
	body = append(body, 0xDE, 0xAD)
	return protocol.Packet{ID: v.Packets.CBPlayMapChunk, Data: body[1:]}
}

func TestMapChunkAcrossHeightmapFormats(t *testing.T) {
	for _, tc := range []struct {
		name       string
		format     protocol.ChunkFormat
		heightmaps func() []byte
	}{
		{
			// 1.21.4 / protocol 769 — the combination that failed live.
			name:       "pre-1.21.5 NBT heightmaps",
			format:     protocol.ChunkFormat{HasSizePrefix: true, NBTHeightmaps: true},
			heightmaps: nbtHeightmaps,
		},
		{
			name:       "1.21.5+ prefixed-array heightmaps",
			format:     protocol.ChunkFormat{},
			heightmaps: arrayHeightmaps,
		},
		{
			// 26.1 / protocol 775 — fluid count as well.
			name:       "26.1 with the fluid count",
			format:     protocol.ChunkFormat{HasFluidCount: true},
			heightmaps: arrayHeightmaps,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			c.v = chunkFormatVersion(t, tc.format)

			p := mapChunkPacket(c.v, 3, -2, tc.heightmaps(), chunkBlob(stateStone, tc.format))
			handled, err := c.handleWorldPacket(p)
			if err != nil {
				t.Fatalf("handleWorldPacket: %v", err)
			}
			if !handled {
				t.Fatal("handleWorldPacket did not claim the map_chunk packet")
			}

			if !c.world.HasChunk(3*16, -2*16) {
				t.Fatal("the chunk was not stored — the heightmap walk left the blob at the wrong offset")
			}
			// A wrong offset does not usually error; it decodes plausible
			// garbage. Assert on the block, not merely on "no error".
			if got := c.BlockAt(3*16+1, 0, -2*16+1); got != stateStone {
				t.Errorf("BlockAt = %d, want %d — the chunk decoded, but wrongly", got, stateStone)
			}
		})
	}
}

// The specific regression: reading the new shape off an old-format packet.
// This is what shipped, and it must not silently succeed.
func TestMapChunkWrongHeightmapShapeIsRejected(t *testing.T) {
	format := protocol.ChunkFormat{HasSizePrefix: true, NBTHeightmaps: true}
	c := newTestClient(t)
	// Deliberately mis-flagged: the packet carries NBT, the version says array.
	c.v = chunkFormatVersion(t, protocol.ChunkFormat{HasSizePrefix: true})

	p := mapChunkPacket(c.v, 5, 5, nbtHeightmaps(), chunkBlob(stateStone, format))
	if _, err := c.handleWorldPacket(p); err == nil {
		if c.BlockAt(5*16+1, 0, 5*16+1) == stateStone {
			t.Fatal("a mis-flagged packet decoded correctly, so this test proves nothing")
		}
	}
	if c.world.HasChunk(5*16, 5*16) && c.BlockAt(5*16+1, 0, 5*16+1) == stateStone {
		t.Error("the wrong heightmap shape produced a correct chunk — the flag is not load-bearing")
	}
}
