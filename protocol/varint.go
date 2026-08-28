package protocol

import (
	"errors"
	"io"
)

// Minecraft's VarInt/VarLong: 7 payload bits per byte, high bit means
// "another byte follows". A VarInt is capped at 5 bytes and a VarLong at 10 —
// a longer run is a malformed stream (or a desync), and it has to be an error
// rather than a silent truncation, because a desynced length prefix would
// otherwise be read as a plausible packet boundary and corrupt everything
// after it.
const (
	maxVarIntBytes  = 5
	maxVarLongBytes = 10
)

var errVarIntTooLong = errors.New("protocol: VarInt exceeds 5 bytes")
var errVarLongTooLong = errors.New("protocol: VarLong exceeds 10 bytes")

// ReadVarInt reads a VarInt from r.
func ReadVarInt(r io.ByteReader) (int32, error) {
	var value uint32
	for i := range maxVarIntBytes {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(value), nil
		}
	}
	return 0, errVarIntTooLong
}

// ReadVarLong reads a VarLong from r.
func ReadVarLong(r io.ByteReader) (int64, error) {
	var value uint64
	for i := range maxVarLongBytes {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= uint64(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int64(value), nil
		}
	}
	return 0, errVarLongTooLong
}

// AppendVarInt appends the VarInt encoding of v to dst.
func AppendVarInt(dst []byte, v int32) []byte {
	u := uint32(v)
	for {
		if u&^0x7f == 0 {
			return append(dst, byte(u))
		}
		dst = append(dst, byte(u&0x7f)|0x80)
		u >>= 7
	}
}

// AppendVarLong appends the VarLong encoding of v to dst.
func AppendVarLong(dst []byte, v int64) []byte {
	u := uint64(v)
	for {
		if u&^0x7f == 0 {
			return append(dst, byte(u))
		}
		dst = append(dst, byte(u&0x7f)|0x80)
		u >>= 7
	}
}

// VarIntLen reports how many bytes AppendVarInt would write, for sizing a
// buffer before encoding into it.
func VarIntLen(v int32) int {
	u := uint32(v)
	n := 1
	for u&^0x7f != 0 {
		u >>= 7
		n++
	}
	return n
}
