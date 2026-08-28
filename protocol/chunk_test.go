package protocol

import "testing"

// testVersion is a minimal protocol table: air 0, stone 1, water 2, lava 3.
func testVersion(format ChunkFormat) *Version {
	return NewVersion(VersionSpec{
		Name:     "test",
		Protocol: 1,
		Chunk:    format,
		Air:      [][2]int32{{0, 0}},
		Solid:    [][2]int32{{1, 1}},
		Water:    [][2]int32{{2, 2}},
		Lava:     [][2]int32{{3, 3}},
	})
}

// uniformSection encodes a bitsPerEntry-0 container: one state for the whole
// cube, no data array.
func uniformSection(w *Writer, state int32, format ChunkFormat) *Writer {
	w.U8(0).VarInt(state)
	if format.HasSizePrefix {
		w.U8(0)
	}
	return w
}

// indirectSection encodes a 4-bit indirect container whose every entry is
// palette index 1.
func indirectSection(w *Writer, palette []int32, capacity int, format ChunkFormat) *Writer {
	const bits = 4
	w.U8(bits).VarInt(int32(len(palette)))
	for _, s := range palette {
		w.VarInt(s)
	}
	entriesPerLong := 64 / bits
	longs := (capacity + entriesPerLong - 1) / entriesPerLong
	if format.HasSizePrefix {
		w.VarInt(int32(longs))
	}
	var packed int64
	for i := range entriesPerLong {
		packed |= int64(1) << (i * bits)
	}
	for range longs {
		w.I64(packed)
	}
	return w
}

// section writes one full section: counts, block states, biomes.
func section(w *Writer, write func(*Writer) *Writer, format ChunkFormat) *Writer {
	w.I16(4096) // solid block count
	if format.HasFluidCount {
		w.I16(0)
	}
	write(w)
	uniformSection(w, 0, format) // biomes
	return w
}

// Both format flags are invisible until wrong, and then surface as a short
// read several sections downstream — so every combination is exercised.
func TestParseChunkDataAcrossFormats(t *testing.T) {
	for _, format := range []ChunkFormat{
		{},
		{HasFluidCount: true},
		{HasSizePrefix: true},
		{HasFluidCount: true, HasSizePrefix: true},
	} {
		v := testVersion(format)
		w := NewWriter(0)
		const sections = 24
		for range sections {
			section(w, func(w *Writer) *Writer { return uniformSection(w, 1, format) }, format)
		}

		col, err := ParseChunkData(v, 0, 0, w.Bytes()[1:])
		if err != nil {
			t.Fatalf("format %+v: ParseChunkData: %v", format, err)
		}
		if len(col.Sections) != sections {
			t.Errorf("format %+v: got %d sections, want %d", format, len(col.Sections), sections)
		}
		if col.MinY != OverworldMinY {
			t.Errorf("format %+v: MinY = %d, want %d", format, col.MinY, OverworldMinY)
		}
		if got := col.BlockState(5, 0, 5); got != 1 {
			t.Errorf("format %+v: BlockState(5,0,5) = %d, want 1", format, got)
		}
	}
}

// The floor is not in the packet; it is inferred from the section count rather
// than by decoding the dimension registry for a single constant.
func TestParseChunkDataInfersDimensionFloor(t *testing.T) {
	format := ChunkFormat{HasFluidCount: true}
	v := testVersion(format)
	for _, tc := range []struct {
		name     string
		sections int
		wantMinY int32
	}{
		{"nether or end", 16, NetherMinY},
		{"overworld", 24, OverworldMinY},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWriter(0)
			for range tc.sections {
				section(w, func(w *Writer) *Writer { return uniformSection(w, 1, format) }, format)
			}
			col, err := ParseChunkData(v, 0, 0, w.Bytes()[1:])
			if err != nil {
				t.Fatalf("ParseChunkData: %v", err)
			}
			if col.MinY != tc.wantMinY {
				t.Errorf("MinY = %d, want %d", col.MinY, tc.wantMinY)
			}
		})
	}
}

func TestParseChunkDataIndirectPalette(t *testing.T) {
	format := ChunkFormat{HasFluidCount: true}
	v := testVersion(format)
	w := NewWriter(0)
	for range 24 {
		section(w, func(w *Writer) *Writer {
			return indirectSection(w, []int32{0, 7}, blocksPerSection, format)
		}, format)
	}
	col, err := ParseChunkData(v, 0, 0, w.Bytes()[1:])
	if err != nil {
		t.Fatalf("ParseChunkData: %v", err)
	}
	for _, p := range [][3]int32{{0, -64, 0}, {15, -50, 15}, {7, 0, 3}} {
		if got := col.BlockState(p[0], p[1], p[2]); got != 7 {
			t.Errorf("BlockState%v = %d, want 7", p, got)
		}
	}
}

// CONFIRMED CRASH. bitsPerEntry is a byte off the wire used as a divisor:
// anything above 64 makes 64/bits zero and the next division panics, taking
// the process down from one malformed chunk packet.
func TestReadPalettedContainerRejectsImplausibleBits(t *testing.T) {
	for _, bits := range []uint8{MaxBitsPerEntry + 1, 64, 65, 200, 255} {
		t.Run("", func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("bitsPerEntry %d panicked: %v", bits, r)
				}
			}()
			r := NewReader([]byte{bits})
			if _, err := readPalettedContainer(r, 8, blocksPerSection, ChunkFormat{}); err == nil {
				t.Errorf("bitsPerEntry %d: err = nil, want an error", bits)
			}
		})
	}
}

// The same divisor reaches BlockState and SetBlockState on already-decoded
// data, so a section that got through once must not panic later.
func TestChunkSectionSurvivesImplausibleBits(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	s := &ChunkSection{bitsPerEntry: 100, data: []int64{0}}
	if got := s.BlockState(0, 0, 0); got != AirState {
		t.Errorf("BlockState with a corrupt entry width = %d, want %d", got, AirState)
	}

	col := &ChunkColumn{MinY: 0, Sections: []*ChunkSection{s}}
	col.SetBlockState(0, 0, 0, 5)
	if got := col.BlockState(0, 0, 0); got != 5 {
		t.Errorf("after SetBlockState, BlockState = %d, want 5", got)
	}
}

func TestParseChunkDataRejectsTooManySections(t *testing.T) {
	format := ChunkFormat{}
	v := testVersion(format)
	w := NewWriter(0)
	for range MaxSections + 2 {
		section(w, func(w *Writer) *Writer { return uniformSection(w, 0, format) }, format)
	}
	if _, err := ParseChunkData(v, 0, 0, w.Bytes()[1:]); err == nil {
		t.Error("ParseChunkData with too many sections: err = nil, want an error")
	}
}

// Cutting the blob anywhere inside a section must error, not panic and not
// silently return a half-decoded column.
func TestParseChunkDataTruncatedAtEveryOffset(t *testing.T) {
	format := ChunkFormat{HasFluidCount: true}
	v := testVersion(format)
	w := NewWriter(0)
	section(w, func(w *Writer) *Writer { return uniformSection(w, 1, format) }, format)
	full := w.Bytes()[1:]

	for cut := 1; cut < len(full); cut++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("truncating to %d bytes panicked: %v", cut, r)
				}
			}()
			_, _ = ParseChunkData(v, 0, 0, full[:cut])
		}()
	}
}

// A uniform section cannot represent two states, so it has to expand to direct
// encoding first. Sections are almost always uniform air, and a single placed
// block is exactly the case that breaks that assumption.
func TestSetBlockStateExpandsUniformSection(t *testing.T) {
	col := &ChunkColumn{MinY: 0, Sections: []*ChunkSection{{bitsPerEntry: 0, single: 1}}}

	col.SetBlockState(3, 4, 5, 42)
	if got := col.BlockState(3, 4, 5); got != 42 {
		t.Errorf("BlockState(3,4,5) = %d, want 42", got)
	}
	for _, p := range [][3]int32{{0, 0, 0}, {15, 15, 15}, {3, 4, 6}, {2, 4, 5}} {
		if got := col.BlockState(p[0], p[1], p[2]); got != 1 {
			t.Errorf("BlockState%v = %d after expansion, want the original 1", p, got)
		}
	}
}

// Index order is y, then z, then x. Getting it wrong reads a real block from
// the wrong place, which is far harder to notice than a crash.
func TestBlockStateIndexOrder(t *testing.T) {
	col := &ChunkColumn{MinY: 0, Sections: []*ChunkSection{{bitsPerEntry: 0, single: 0}}}
	col.SetBlockState(1, 0, 0, 10)
	col.SetBlockState(0, 1, 0, 20)
	col.SetBlockState(0, 0, 1, 30)

	for _, tc := range []struct{ x, y, z, want int32 }{
		{1, 0, 0, 10}, {0, 1, 0, 20}, {0, 0, 1, 30}, {0, 0, 0, 0},
	} {
		if got := col.BlockState(tc.x, tc.y, tc.z); got != tc.want {
			t.Errorf("BlockState(%d,%d,%d) = %d, want %d", tc.x, tc.y, tc.z, got, tc.want)
		}
	}
}

func TestBlockStateNegativeCoordinates(t *testing.T) {
	col := &ChunkColumn{MinY: OverworldMinY}
	for range 24 {
		col.Sections = append(col.Sections, &ChunkSection{})
	}
	for _, p := range [][3]int32{{-1, -64, -1}, {-16, -1, -16}, {-300, 76, -310}} {
		col.SetBlockState(p[0], p[1], p[2], 9)
		if got := col.BlockState(p[0], p[1], p[2]); got != 9 {
			t.Errorf("BlockState%v = %d, want 9", p, got)
		}
	}
}

// Go's division truncates towards zero, so for a Y in the sixteen blocks
// *below* the floor, (y-MinY)/16 was 0 rather than -1: the coordinate passed
// the "above the floor?" check and was read out of the bottom section at a
// local Y the mask had wrapped to 15. A ground scan found rock under the world.
func TestBlockStateBelowTheFloorIsAir(t *testing.T) {
	for _, minY := range []int32{0, OverworldMinY} {
		col := &ChunkColumn{MinY: minY, Sections: []*ChunkSection{{bitsPerEntry: 0, single: 1}}}
		for _, below := range []int32{minY - 1, minY - 8, minY - 15, minY - 16, minY - 100} {
			if got := col.BlockState(0, below, 0); got != AirState {
				t.Errorf("MinY %d: BlockState at y=%d (below the floor) = %d, want air",
					minY, below, got)
			}
			col.SetBlockState(0, below, 0, 7) // must not write into the bottom section
			if got := col.BlockState(0, minY+15, 0); got != 1 {
				t.Errorf("MinY %d: writing below the floor corrupted y=%d", minY, minY+15)
			}
		}
	}
}

func TestNilChunkReadsAsAir(t *testing.T) {
	var col *ChunkColumn
	if got := col.BlockState(0, 0, 0); got != AirState {
		t.Errorf("nil column BlockState = %d, want %d", got, AirState)
	}
	col.SetBlockState(0, 0, 0, 1) // must not panic

	var sec *ChunkSection
	if got := sec.BlockState(0, 0, 0); got != AirState {
		t.Errorf("nil section BlockState = %d, want %d", got, AirState)
	}
}

func TestBlockStateAboveTheColumnIsAir(t *testing.T) {
	col := &ChunkColumn{MinY: 0, Sections: []*ChunkSection{{bitsPerEntry: 0, single: 1}}}
	for _, y := range []int32{16, 1000} {
		if got := col.BlockState(0, y, 0); got != AirState {
			t.Errorf("BlockState(0,%d,0) above the column = %d, want %d", y, got, AirState)
		}
		col.SetBlockState(0, y, 0, 7) // must not panic
	}
}
