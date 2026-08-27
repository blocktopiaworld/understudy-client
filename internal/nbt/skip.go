package nbt

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Tag types, in wire order.
const (
	TagEnd = iota
	TagByte
	TagShort
	TagInt
	TagLong
	TagFloat
	TagDouble
	TagByteArray
	TagString
	TagList
	TagCompound
	TagIntArray
	TagLongArray
)

// ErrTruncated reports that a tag ran off the end of the buffer.
var ErrTruncated = errors.New("nbt: truncated")

// maxDepth bounds nesting. NBT is self-describing and arrives from the wire,
// so a crafted payload of nothing but list-of-list headers would otherwise
// recurse until the stack gave out.
const maxDepth = 64

// SkipTag walks one complete NBT value and reports how many bytes it spanned.
//
// It reads a *nameless* root, which is what the network format has used since
// 1.20.2: a type byte followed straight by the payload, with no name string.
// A file-format tag would carry a name there and this would mis-parse it.
//
// Skipping rather than decoding is deliberate. The only reason this exists is
// that pre-1.21.5 chunk packets put an NBT compound of heightmaps between the
// coordinates and the chunk data, and nothing in this client reads heightmaps
// — but the bytes still have to be stepped over exactly, or the data blob
// starts at the wrong offset and the failure surfaces as a short read many
// sections later.
func SkipTag(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("%w: empty tag", ErrTruncated)
	}
	tag := data[0]
	if tag == TagEnd {
		return 1, nil
	}
	n, err := skipPayload(data[1:], tag, 0)
	if err != nil {
		return 0, err
	}
	return n + 1, nil
}

// skipPayload steps over the payload of a tag whose type is already known.
func skipPayload(data []byte, tag byte, depth int) (int, error) {
	if depth > maxDepth {
		return 0, fmt.Errorf("nbt: nested deeper than %d", maxDepth)
	}
	switch tag {
	case TagByte:
		return fixed(data, 1)
	case TagShort:
		return fixed(data, 2)
	case TagInt, TagFloat:
		return fixed(data, 4)
	case TagLong, TagDouble:
		return fixed(data, 8)
	case TagString:
		// Length is unsigned: a 40000-byte string is legal and a signed read
		// would take it as negative.
		if len(data) < 2 {
			return 0, fmt.Errorf("%w: string length", ErrTruncated)
		}
		return fixed(data, 2+int(binary.BigEndian.Uint16(data)))
	case TagByteArray:
		return array(data, 1)
	case TagIntArray:
		return array(data, 4)
	case TagLongArray:
		return array(data, 8)

	case TagList:
		if len(data) < 5 {
			return 0, fmt.Errorf("%w: list header", ErrTruncated)
		}
		elem := data[0]
		count := int32(binary.BigEndian.Uint32(data[1:]))
		off := 5
		// A negative or zero count means an empty list, and the element type is
		// then meaningless — vanilla writes TAG_End for it.
		if count <= 0 || elem == TagEnd {
			return off, nil
		}
		for range count {
			n, err := skipPayload(data[off:], elem, depth+1)
			if err != nil {
				return 0, err
			}
			off += n
		}
		return off, nil

	case TagCompound:
		off := 0
		for {
			if off >= len(data) {
				return 0, fmt.Errorf("%w: compound without an end tag", ErrTruncated)
			}
			inner := data[off]
			off++
			if inner == TagEnd {
				return off, nil
			}
			// Named entries inside a compound always carry their name, even
			// when the root does not.
			if off+2 > len(data) {
				return 0, fmt.Errorf("%w: entry name length", ErrTruncated)
			}
			nameLen := int(binary.BigEndian.Uint16(data[off:]))
			off += 2
			if off+nameLen > len(data) {
				return 0, fmt.Errorf("%w: entry name", ErrTruncated)
			}
			off += nameLen

			n, err := skipPayload(data[off:], inner, depth+1)
			if err != nil {
				return 0, err
			}
			off += n
		}

	default:
		return 0, fmt.Errorf("nbt: unknown tag type %d", tag)
	}
}

func fixed(data []byte, n int) (int, error) {
	if n < 0 || len(data) < n {
		return 0, fmt.Errorf("%w: want %d bytes, have %d", ErrTruncated, n, len(data))
	}
	return n, nil
}

// array steps over an int32-prefixed array of fixed-width elements. The count
// is bounded against the buffer before it is multiplied, so a crafted length
// cannot overflow into a small positive number.
func array(data []byte, width int) (int, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("%w: array length", ErrTruncated)
	}
	count := int32(binary.BigEndian.Uint32(data))
	if count < 0 {
		return 0, fmt.Errorf("nbt: negative array length %d", count)
	}
	if int(count) > (len(data)-4)/width {
		return 0, fmt.Errorf("%w: array of %d x %d bytes, have %d",
			ErrTruncated, count, width, len(data)-4)
	}
	return 4 + int(count)*width, nil
}
