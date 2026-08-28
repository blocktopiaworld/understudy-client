package protocol

import (
	"encoding/binary"
	"math"
)

// Writer encodes a packet payload. The methods chain, so a packet reads as a
// single expression in field order — which is how it is checked against the
// protocol description.
//
// A Writer is not safe for concurrent use.
type Writer struct{ buf []byte }

// NewWriter returns a Writer with the given packet ID already encoded, which
// is where every outbound payload starts.
func NewWriter(packetID int32) *Writer {
	return &Writer{buf: AppendVarInt(nil, packetID)}
}

// Bytes returns the encoded payload. It aliases the Writer's buffer, so it
// must not be retained across further writes.
func (w *Writer) Bytes() []byte { return w.buf }

// VarInt appends a variable-length 32-bit integer.
func (w *Writer) VarInt(v int32) *Writer { w.buf = AppendVarInt(w.buf, v); return w }

// VarLong appends a variable-length 64-bit integer.
func (w *Writer) VarLong(v int64) *Writer { w.buf = AppendVarLong(w.buf, v); return w }

// U8 appends an unsigned byte.
func (w *Writer) U8(v uint8) *Writer { w.buf = append(w.buf, v); return w }

// I8 appends a signed byte.
func (w *Writer) I8(v int8) *Writer { w.buf = append(w.buf, byte(v)); return w }

// Bool appends a boolean as a single byte.
func (w *Writer) Bool(v bool) *Writer {
	var b byte
	if v {
		b = 1
	}
	w.buf = append(w.buf, b)
	return w
}

// I16 appends a big-endian signed 16-bit integer.
func (w *Writer) I16(v int16) *Writer {
	w.buf = binary.BigEndian.AppendUint16(w.buf, uint16(v))
	return w
}

// I32 appends a big-endian signed 32-bit integer.
func (w *Writer) I32(v int32) *Writer {
	w.buf = binary.BigEndian.AppendUint32(w.buf, uint32(v))
	return w
}

// I64 appends a big-endian signed 64-bit integer.
func (w *Writer) I64(v int64) *Writer {
	w.buf = binary.BigEndian.AppendUint64(w.buf, uint64(v))
	return w
}

// F32 appends a big-endian IEEE-754 single.
func (w *Writer) F32(v float32) *Writer {
	w.buf = binary.BigEndian.AppendUint32(w.buf, math.Float32bits(v))
	return w
}

// F64 appends a big-endian IEEE-754 double.
func (w *Writer) F64(v float64) *Writer {
	w.buf = binary.BigEndian.AppendUint64(w.buf, math.Float64bits(v))
	return w
}

// String appends a length-prefixed UTF-8 string.
func (w *Writer) String(v string) *Writer {
	w.buf = AppendVarInt(w.buf, int32(len(v)))
	w.buf = append(w.buf, v...)
	return w
}

// UUID appends a 128-bit UUID in wire order.
func (w *Writer) UUID(v UUID) *Writer {
	w.buf = append(w.buf, v[:]...)
	return w
}

// BlockPos writes a block coordinate in the packed form the protocol uses:
// a single 64-bit word of x:26, z:26, y:12, in that order. Note the ordering —
// z sits between x and y, which is the usual place to get this wrong, and a
// wrong packing addresses a real block somewhere else entirely rather than
// erroring.
func (w *Writer) BlockPos(x, y, z int32) *Writer {
	v := (int64(x&0x3FFFFFF) << 38) | (int64(z&0x3FFFFFF) << 12) | int64(y&0xFFF)
	return w.I64(v)
}

// DecodeBlockPos unpacks the 64-bit block position form written by
// Writer.BlockPos: x:26, z:26, y:12, each signed. The shifts sign-extend by
// shifting left then arithmetic-right, which is why they are written as a pair
// rather than a mask.
func DecodeBlockPos(v int64) (x, y, z int32) {
	x = int32(v >> 38)
	z = int32(v << 26 >> 38)
	y = int32(v << 52 >> 52)
	return x, y, z
}

// F16 writes an IEEE-754 half-precision float.
//
// Minecraft uses these where a full float would be wasteful and the precision
// does not matter — the "lpVec3" (low-precision vec3) carrying the point on an
// entity that an interaction hit, which only has to be good enough to tell one
// part of a boat from another.
//
// Getting the width wrong here is not subtle: the server reports the packet as
// longer or shorter than it expected and drops the connection, which is at
// least loud.
func (w *Writer) F16(v float32) *Writer {
	return w.U16(float16bits(v))
}

// U16 writes a big-endian unsigned 16-bit value.
func (w *Writer) U16(v uint16) *Writer {
	w.buf = binary.BigEndian.AppendUint16(w.buf, v)
	return w
}

// float16bits converts a float32 to half-precision, rounding to nearest even.
func float16bits(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16((b >> 16) & 0x8000)
	exp := int32((b>>23)&0xff) - 127 + 15
	mantissa := b & 0x7fffff

	switch {
	case exp >= 0x1f: // overflow, or inf/NaN: saturate
		if (b>>23)&0xff == 0xff && mantissa != 0 {
			return sign | 0x7e00 // NaN
		}
		return sign | 0x7c00 // infinity
	case exp <= 0: // subnormal or underflow to zero
		if exp < -10 {
			return sign
		}
		mantissa |= 0x800000
		shift := uint32(14 - exp)
		half := uint16(mantissa >> shift)
		// Round to nearest even.
		if mantissa&(1<<(shift-1)) != 0 {
			half++
		}
		return sign | half
	default:
		half := sign | uint16(exp<<10) | uint16(mantissa>>13)
		if mantissa&0x1000 != 0 { // round to nearest even
			half++
		}
		return half
	}
}
