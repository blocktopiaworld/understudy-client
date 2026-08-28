package nbt

import (
	"encoding/binary"
	"errors"
	"testing"
)

// b builds a byte slice from mixed literals, so fixtures read like the wire
// format rather than like Go.
func b(parts ...any) []byte {
	var out []byte
	for _, p := range parts {
		switch v := p.(type) {
		case byte:
			out = append(out, v)
		case int:
			out = append(out, byte(v))
		case rune:
			// So a fixture can write 'a' for a literal byte.
			out = append(out, byte(v))
		case []byte:
			out = append(out, v...)
		case string:
			out = binary.BigEndian.AppendUint16(out, uint16(len(v)))
			out = append(out, v...)
		default:
			panic("unsupported fixture type")
		}
	}
	return out
}

func u32(n uint32) []byte { return binary.BigEndian.AppendUint32(nil, n) }

// Every payload width has to be exact: one byte out and the caller's next read
// lands mid-field, which is the whole failure mode this exists to prevent.
func TestSkipTagWidths(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want int
	}{
		{"end", b(TagEnd), 1},
		{"byte", b(TagByte, 0x7f), 2},
		{"short", b(TagShort, 0, 1), 3},
		{"int", b(TagInt, u32(1)), 5},
		{"float", b(TagFloat, u32(1)), 5},
		{"long", b(TagLong, u32(0), u32(1)), 9},
		{"double", b(TagDouble, u32(0), u32(1)), 9},
		{"string", b(TagString, "hello"), 1 + 2 + 5},
		{"empty string", b(TagString, ""), 3},
		{"byte array", b(TagByteArray, u32(3), 1, 2, 3), 1 + 4 + 3},
		{"int array", b(TagIntArray, u32(2), u32(7), u32(8)), 1 + 4 + 8},
		{"long array", b(TagLongArray, u32(1), u32(0), u32(9)), 1 + 4 + 8},
		{"empty compound", b(TagCompound, TagEnd), 2},
		{"empty list", b(TagList, TagEnd, u32(0)), 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SkipTag(tc.data)
			if err != nil {
				t.Fatalf("SkipTag: %v", err)
			}
			if got != tc.want {
				t.Errorf("SkipTag spanned %d bytes, want %d", got, tc.want)
			}
			if got != len(tc.data) {
				t.Errorf("SkipTag spanned %d of %d bytes — it must consume exactly one tag",
					got, len(tc.data))
			}
		})
	}
}

// The heightmaps case: a compound of named long arrays, exactly the shape a
// pre-1.21.5 server puts between the chunk coordinates and the chunk data.
func TestSkipTagHeightmapsShape(t *testing.T) {
	longs := func(n int) []byte {
		out := u32(uint32(n))
		for range n {
			out = append(out, u32(0)...)
			out = append(out, u32(0)...)
		}
		return out
	}
	data := b(
		TagCompound,
		TagLongArray, "MOTION_BLOCKING", longs(37),
		TagLongArray, "WORLD_SURFACE", longs(37),
		TagEnd,
	)
	// Something must follow, or a decoder that overruns still looks correct.
	withTail := append(append([]byte{}, data...), 0xAB, 0xCD)

	n, err := SkipTag(withTail)
	if err != nil {
		t.Fatalf("SkipTag: %v", err)
	}
	if n != len(data) {
		t.Fatalf("SkipTag spanned %d bytes, want %d", n, len(data))
	}
	if got := withTail[n:]; got[0] != 0xAB || got[1] != 0xCD {
		t.Errorf("the bytes after the tag are %x, want abcd — the walk was off", got)
	}
}

func TestSkipTagNestedListsAndCompounds(t *testing.T) {
	// A list of two compounds, each holding a string and an int.
	entry := b(
		TagString, "id", "minecraft:stone",
		TagInt, "count", u32(3),
		TagEnd,
	)
	data := b(TagList, TagCompound, u32(2), entry, entry)

	n, err := SkipTag(data)
	if err != nil {
		t.Fatalf("SkipTag: %v", err)
	}
	if n != len(data) {
		t.Errorf("SkipTag spanned %d bytes, want %d", n, len(data))
	}
}

// Everything here arrives from a socket, so a truncated or hostile tag must
// return an error rather than read past the buffer or spin.
func TestSkipTagRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"truncated long", b(TagLong, 0, 0)},
		{"truncated string", b(TagString, 0, 10, 'a')},
		{"compound with no end", b(TagCompound, TagByte, "x", 1)},
		{"negative array length", b(TagByteArray, u32(0xFFFFFFFF))},
		{"array length past the buffer", b(TagLongArray, u32(1_000_000), 1, 2, 3)},
		{"unknown tag type", b(99, 1, 2, 3)},
		{"truncated entry name", b(TagCompound, TagByte, 0, 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if n, err := SkipTag(tc.data); err == nil {
				t.Errorf("SkipTag(%s) = %d, nil; want an error", tc.name, n)
			}
		})
	}
}

// A payload of nothing but list headers would recurse until the stack gave out.
func TestSkipTagBoundsNesting(t *testing.T) {
	data := []byte{TagList}
	for range maxDepth + 10 {
		data = append(data, TagList)
		data = append(data, u32(1)...)
	}
	_, err := SkipTag(data)
	if err == nil {
		t.Fatal("SkipTag on deeply nested lists = nil error, want a depth error")
	}
	if errors.Is(err, ErrTruncated) {
		t.Errorf("error = %v, want a nesting error rather than truncation", err)
	}
}
