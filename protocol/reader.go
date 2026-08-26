package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// MaxStringLen bounds a decoded string.
//
// The protocol's own limit is 32767 characters, which is at most four bytes
// each. Anything longer is a desynced stream rather than a real field, and
// without a cap the length prefix — which is whatever bytes happened to land
// at that offset — decides how much memory this client allocates.
const MaxStringLen = 32767 * 4

// zeroPad backs every failed read. It is sized for the widest fixed-width
// field (a 16-byte UUID) so the accessors can index it unconditionally.
//
// Sharing one buffer is the point: n comes off the wire, so allocating n bytes
// to satisfy a read that has already failed turns a single corrupt length
// prefix into an out-of-memory kill.
var zeroPad [16]byte

// Reader decodes a packet payload. Every read is bounds-checked and the first
// failure is sticky: once Err is set, later reads return zero values instead
// of panicking. That lets a decoder read a whole packet and check the error
// once at the end rather than after every field.
//
// A Reader is not safe for concurrent use.
type Reader struct {
	buf []byte
	pos int
	err error
}

// NewReader returns a Reader over buf. The buffer is not copied, so it must
// not be modified while the Reader is in use.
func NewReader(buf []byte) *Reader { return &Reader{buf: buf} }

// Err returns the first error encountered, if any.
func (r *Reader) Err() error { return r.err }

// Remaining returns the undecoded tail of the payload, or nil once a read has
// failed. It aliases the underlying buffer.
func (r *Reader) Remaining() []byte {
	if r.err != nil || r.pos > len(r.buf) {
		return nil
	}
	return r.buf[r.pos:]
}

func (r *Reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// take returns the next n bytes, advancing the cursor.
//
// On any failure it returns a slice of shared zeroes rather than a fresh
// allocation — see zeroPad. The result may therefore be shorter than n, which
// is safe because every caller either indexes within the fixed-width prefix or
// discards the value once Err is set.
func (r *Reader) take(n int) []byte {
	if r.err == nil {
		switch {
		case n < 0:
			r.fail(fmt.Errorf("protocol: negative read length %d", n))
		case n > len(r.buf)-r.pos:
			r.fail(fmt.Errorf("protocol: short read: want %d bytes at offset %d, have %d",
				n, r.pos, len(r.buf)-r.pos))
		default:
			b := r.buf[r.pos : r.pos+n]
			r.pos += n
			return b
		}
	}
	if n > len(zeroPad) || n < 0 {
		n = len(zeroPad)
	}
	return zeroPad[:n]
}

// ReadByte satisfies io.ByteReader so the VarInt helpers can read from here.
func (r *Reader) ReadByte() (byte, error) {
	if r.err != nil {
		return 0, r.err
	}
	if r.pos >= len(r.buf) {
		r.fail(errors.New("protocol: short read: no bytes remaining"))
		return 0, r.err
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

// VarInt reads a variable-length 32-bit integer.
func (r *Reader) VarInt() int32 {
	v, err := ReadVarInt(r)
	if err != nil {
		r.fail(err)
		return 0
	}
	return v
}

// VarLong reads a variable-length 64-bit integer.
func (r *Reader) VarLong() int64 {
	v, err := ReadVarLong(r)
	if err != nil {
		r.fail(err)
		return 0
	}
	return v
}

// Bool reads a single byte as a boolean.
func (r *Reader) Bool() bool { return r.take(1)[0] != 0 }

// U8 reads an unsigned byte.
func (r *Reader) U8() uint8 { return r.take(1)[0] }

// I8 reads a signed byte.
func (r *Reader) I8() int8 { return int8(r.take(1)[0]) }

// I16 reads a big-endian signed 16-bit integer.
func (r *Reader) I16() int16 { return int16(binary.BigEndian.Uint16(r.take(2))) }

// I32 reads a big-endian signed 32-bit integer.
func (r *Reader) I32() int32 { return int32(binary.BigEndian.Uint32(r.take(4))) }

// I64 reads a big-endian signed 64-bit integer.
func (r *Reader) I64() int64 { return int64(binary.BigEndian.Uint64(r.take(8))) }

// F32 reads a big-endian IEEE-754 single.
func (r *Reader) F32() float32 { return math.Float32frombits(binary.BigEndian.Uint32(r.take(4))) }

// F64 reads a big-endian IEEE-754 double.
func (r *Reader) F64() float64 { return math.Float64frombits(binary.BigEndian.Uint64(r.take(8))) }

// String reads a length-prefixed UTF-8 string, rejecting implausible lengths
// rather than allocating whatever the prefix claims. See MaxStringLen.
func (r *Reader) String() string {
	n := r.VarInt()
	if r.err != nil {
		return ""
	}
	if n < 0 || n > MaxStringLen {
		r.fail(fmt.Errorf("protocol: string length %d out of range (max %d)", n, MaxStringLen))
		return ""
	}
	b := r.take(int(n))
	if r.err != nil {
		return ""
	}
	return string(b)
}

// UUID reads a 128-bit UUID in wire order.
func (r *Reader) UUID() UUID {
	var u UUID
	copy(u[:], r.take(16))
	return u
}
