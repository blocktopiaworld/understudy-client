package protocol

import (
	"fmt"
)

// SectionHeight is the edge length of a chunk section: 16×16×16 blocks.
const SectionHeight = 16

// Container capacities: block states are a full 16³ grid, biomes a coarser 4³.
const (
	blocksPerSection = SectionHeight * SectionHeight * SectionHeight
	biomesPerSection = 4 * 4 * 4
)

// MaxSections bounds how tall a decoded column may be. The overworld is 24
// sections and no vanilla dimension exceeds that; the cap stops a malformed
// blob from being read as an unbounded run of sections.
const MaxSections = 64

// OverworldMinY and NetherMinY are the two dimension floors this client sees.
// The floor is not carried in the chunk packet, so it is inferred from the
// section count rather than by decoding the dimension registry.
const (
	OverworldMinY int32 = -64
	NetherMinY    int32 = 0

	// overworldSections is the section count that distinguishes an overworld
	// column (24) from a Nether or End one (16).
	overworldSections = 16
)

// MaxBitsPerEntry caps the entry width of a paletted container.
//
// Direct encoding needs ceil(log2(stateCount)) bits, which is 15 for every
// version this client speaks. The cap exists because bitsPerEntry arrives from
// the wire and is used as a divisor: a corrupt or hostile value above 64 makes
// 64/bits zero, and the next division panics the whole process. Rejecting it
// here keeps a malformed chunk a dropped packet rather than a crash.
const MaxBitsPerEntry uint8 = 32

// ChunkSection is one 16³ cube of block states.
//
// The states are kept in the wire's own paletted form rather than expanded to
// 4096 int32s per section. A loaded view distance is hundreds of sections, and
// expanding them all would cost tens of megabytes to answer questions about a
// handful of blocks.
//
// A ChunkSection is not safe for concurrent use; callers hold the lock.
type ChunkSection struct {
	// bitsPerEntry is 0 for a section that is entirely one block state, and
	// never exceeds MaxBitsPerEntry — readPalettedContainer rejects anything
	// wider, which is what lets the indexing arithmetic divide by it safely.
	bitsPerEntry uint8
	// palette maps packed values to block states. Empty for direct encoding,
	// where the packed value *is* the state.
	palette []int32
	// single is the state of a uniform section (bitsPerEntry == 0).
	single int32
	// data is the packed block state array.
	data []int64
}

// BlockState returns the state at a local coordinate, each 0..15.
func (s *ChunkSection) BlockState(x, y, z int32) int32 {
	if s == nil {
		return AirState
	}
	if s.bitsPerEntry == 0 {
		return s.single
	}
	// Defence in depth: readPalettedContainer already rejects a wider entry,
	// but this arithmetic divides by bitsPerEntry and a zero divisor would take
	// the process down rather than lose one block.
	if s.bitsPerEntry > MaxBitsPerEntry {
		return AirState
	}
	// Index order is y, then z, then x — the order the section is written in.
	// Getting this wrong reads a real block from the wrong place, which is far
	// harder to notice than a crash.
	index := (y*SectionHeight+z)*SectionHeight + x

	entriesPerLong := int32(64 / int(s.bitsPerEntry))
	longIndex := index / entriesPerLong
	if longIndex < 0 || int(longIndex) >= len(s.data) {
		return AirState
	}
	// Entries never straddle a long boundary (since 1.16), so the offset is a
	// plain multiple of the entry width.
	offset := (index % entriesPerLong) * int32(s.bitsPerEntry)
	mask := int64(1)<<s.bitsPerEntry - 1
	value := int32((s.data[longIndex] >> uint(offset)) & mask)

	if len(s.palette) == 0 {
		return value // direct: the packed value is the state
	}
	if int(value) >= len(s.palette) {
		return AirState
	}
	return s.palette[value]
}

// ChunkColumn is a 16×N×16 column of sections at a chunk coordinate.
type ChunkColumn struct {
	X, Z     int32
	MinY     int32
	Sections []*ChunkSection
}

// locate maps a world Y to the section holding it and the local Y within it.
//
// Both halves have to agree about negative coordinates, and they did not.
// Go's division truncates towards zero, so for a Y in the sixteen blocks
// *below* the column floor, (y-MinY)/16 is 0 rather than -1 — the coordinate
// passed the "is it above the floor?" check and was then read out of the
// bottom section, at a local Y the mask had correctly wrapped to 15. The
// symptom is a downward ground scan finding solid rock under the world floor.
//
// Rejecting a negative offset outright is both the fix and the intent: below
// the floor is outside the column.
func (c *ChunkColumn) locate(y int32) (section *ChunkSection, localY int32, ok bool) {
	rel := y - c.MinY
	if rel < 0 {
		return nil, 0, false
	}
	index := rel / SectionHeight
	if int(index) >= len(c.Sections) {
		return nil, 0, false
	}
	// The mask is the correct non-negative remainder for a power-of-two size.
	return c.Sections[index], rel & (SectionHeight - 1), true
}

// BlockState returns the state at absolute world coordinates, or air if the
// coordinate falls outside the column.
func (c *ChunkColumn) BlockState(x, y, z int32) int32 {
	if c == nil {
		return AirState
	}
	section, localY, ok := c.locate(y)
	if !ok {
		return AirState
	}
	return section.BlockState(x&15, localY, z&15)
}

// SetBlockState overwrites a single block, for the block-update packets.
//
// A uniform (bitsPerEntry 0) section cannot represent two different states, so
// it is expanded to a direct-encoded section first. Sections are almost always
// uniform air, and a single placed block is exactly the case that breaks that
// assumption.
func (c *ChunkColumn) SetBlockState(x, y, z, state int32) {
	if c == nil {
		return
	}
	s, localY, ok := c.locate(y)
	if !ok {
		return
	}
	if s == nil {
		s = &ChunkSection{}
		c.Sections[(y-c.MinY)/SectionHeight] = s
	}
	if s.bitsPerEntry == 0 || s.bitsPerEntry > MaxBitsPerEntry {
		s.expandToDirect()
	}

	index := (localY*SectionHeight+(z&15))*SectionHeight + (x & 15)

	value := state
	if len(s.palette) > 0 {
		// Find or append a palette entry; if the palette cannot grow within
		// the current entry width, fall back to direct encoding.
		found := int32(-1)
		for i, p := range s.palette {
			if p == state {
				found = int32(i)
				break
			}
		}
		if found < 0 {
			if len(s.palette) < 1<<s.bitsPerEntry {
				s.palette = append(s.palette, state)
				found = int32(len(s.palette) - 1)
			} else {
				s.expandToDirect()
				found = state
			}
		}
		value = found
	}

	entriesPerLong := int32(64 / int(s.bitsPerEntry))
	longIndex := index / entriesPerLong
	if longIndex < 0 || int(longIndex) >= len(s.data) {
		return
	}
	offset := uint((index % entriesPerLong) * int32(s.bitsPerEntry))
	mask := int64(1)<<s.bitsPerEntry - 1
	s.data[longIndex] = (s.data[longIndex] &^ (mask << offset)) | (int64(value)&mask)<<offset
}

// directBits is the entry width used when a section has to hold arbitrary
// states. 15 bits covers every block state in 26.1 (max 29872 < 32768).
const directBits = 15

// expandToDirect rewrites a section as a direct-encoded one holding its
// current contents.
func (s *ChunkSection) expandToDirect() {
	const blocks = SectionHeight * SectionHeight * SectionHeight
	old := make([]int32, blocks)
	for i := range blocks {
		x := int32(i % SectionHeight)
		z := int32((i / SectionHeight) % SectionHeight)
		y := int32(i / (SectionHeight * SectionHeight))
		old[i] = s.BlockState(x, y, z)
	}

	entriesPerLong := 64 / directBits
	s.bitsPerEntry = directBits
	s.palette = nil
	s.data = make([]int64, (blocks+entriesPerLong-1)/entriesPerLong)
	mask := int64(1)<<directBits - 1
	for i, state := range old {
		longIndex := i / entriesPerLong
		offset := uint((i % entriesPerLong) * directBits)
		s.data[longIndex] |= (int64(state) & mask) << offset
	}
}

// readPalettedContainer decodes one paletted container.
//
// Three encodings share this shape and only the palette differs:
//   - bitsPerEntry 0: the whole container is a single value, no data array
//   - 1..8 for blocks: an indirect palette of state IDs
//   - 9 and above: direct, where packed values are state IDs themselves
//
// Misreading which case applies desynchronises the rest of the chunk, so the
// branch is explicit rather than inferred from the data length.
func readPalettedContainer(r *Reader, maxIndirectBits uint8, capacity int, format ChunkFormat) (*ChunkSection, error) {
	bits := r.U8()
	if err := r.Err(); err != nil {
		return nil, err
	}
	// bitsPerEntry becomes a divisor below. Anything above 64 makes 64/bits
	// zero and the next division panics, so a corrupt byte here has to be a
	// dropped chunk rather than a dead process. See MaxBitsPerEntry.
	if bits > MaxBitsPerEntry {
		return nil, fmt.Errorf("protocol: implausible bits per entry %d (max %d)", bits, MaxBitsPerEntry)
	}

	section := &ChunkSection{bitsPerEntry: bits}
	switch {
	case bits == 0:
		section.single = r.VarInt()
	case bits <= maxIndirectBits:
		n := r.VarInt()
		if n < 0 || n > 1<<16 {
			return nil, fmt.Errorf("protocol: implausible palette length %d", n)
		}
		section.palette = make([]int32, n)
		for i := range section.palette {
			section.palette[i] = r.VarInt()
		}
	}
	if err := r.Err(); err != nil {
		return nil, err
	}

	if bits == 0 {
		// A uniform container has no data array. Before 1.21.5 it still
		// carried a (zero) length byte, which must be consumed.
		if format.HasSizePrefix {
			r.U8()
		}
		return section, r.Err()
	}

	// Before 1.21.5 the data array carried a VarInt count of longs; from
	// 1.21.5 that count is computed instead. Reading a prefix that is not
	// there consumes the first long and desynchronises every section after it,
	// so this must follow the version rather than be inferred.
	entriesPerLong := 64 / int(bits)
	longs := (capacity + entriesPerLong - 1) / entriesPerLong
	if format.HasSizePrefix {
		declared := r.VarInt()
		if err := r.Err(); err != nil {
			return nil, err
		}
		if declared < 0 || int(declared) > 4096 {
			return nil, fmt.Errorf("protocol: implausible data array length %d", declared)
		}
		longs = int(declared)
	}
	section.data = make([]int64, longs)
	for i := range section.data {
		section.data[i] = r.I64()
	}
	return section, r.Err()
}

// ParseChunkData decodes the chunkData blob of a map_chunk packet.
//
// Sections are read until the buffer is exhausted rather than from a declared
// count, because the count depends on the dimension's height and this client
// deliberately never decodes the dimension registry.
func ParseChunkData(v *Version, x, z int32, data []byte) (*ChunkColumn, error) {
	r := NewReader(data)
	column := &ChunkColumn{X: x, Z: z}

	for len(r.Remaining()) > 0 {
		if len(column.Sections) >= MaxSections {
			return nil, fmt.Errorf("protocol: chunk %d,%d has more than %d sections", x, z, MaxSections)
		}
		// Each section is: solid block count, fluid count, the block state
		// container, then the biome container.
		//
		// The fluid count is easy to miss — it is absent from older format
		// descriptions — and omitting it shifts everything after by two bytes,
		// surfacing as a short read many sections later rather than here.
		r.I16() // solid block count
		if v.Chunk.HasFluidCount {
			r.I16() // fluid count, 26.1+
		}
		if err := r.Err(); err != nil {
			return nil, err
		}
		blockStates, err := readPalettedContainer(r, 8, blocksPerSection, v.Chunk)
		if err != nil {
			return nil, fmt.Errorf("protocol: section %d block states: %w", len(column.Sections), err)
		}
		// Biomes are a 4³ grid with a narrower indirect range. This client
		// never reads them, but they must still be decoded to stay in sync.
		if _, err := readPalettedContainer(r, 3, biomesPerSection, v.Chunk); err != nil {
			return nil, fmt.Errorf("protocol: section %d biomes: %w", len(column.Sections), err)
		}
		column.Sections = append(column.Sections, blockStates)
	}

	// The dimension's floor is not in this packet. Overworld columns are 24
	// sections tall starting at -64; the Nether and End are 16 starting at 0.
	// Inferring from the section count avoids decoding the dimension registry
	// for a single constant.
	column.MinY = NetherMinY
	if len(column.Sections) > overworldSections {
		column.MinY = OverworldMinY
	}
	return column, nil
}
