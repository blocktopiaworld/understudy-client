package protocol

import (
	"bytes"
	"testing"
)

func TestNewWriterStartsWithPacketID(t *testing.T) {
	for _, id := range []int32{0, 1, 0x7f, 0x80, 300} {
		got := NewWriter(id).Bytes()
		want := AppendVarInt(nil, id)
		if !bytes.Equal(got, want) {
			t.Errorf("NewWriter(%d).Bytes() = %v, want %v", id, got, want)
		}
	}
}

// The protocol is big-endian throughout. A byte-swapped field is a real value
// somewhere else entirely rather than an error.
func TestWriterBigEndian(t *testing.T) {
	tests := []struct {
		name  string
		build func(*Writer) *Writer
		want  []byte
	}{
		{"I16", func(w *Writer) *Writer { return w.I16(0x0102) }, []byte{0x01, 0x02}},
		{"I32", func(w *Writer) *Writer { return w.I32(0x01020304) }, []byte{0x01, 0x02, 0x03, 0x04}},
		{"I64", func(w *Writer) *Writer { return w.I64(0x0102030405060708) },
			[]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}},
		{"Bool true", func(w *Writer) *Writer { return w.Bool(true) }, []byte{1}},
		{"Bool false", func(w *Writer) *Writer { return w.Bool(false) }, []byte{0}},
		{"U8", func(w *Writer) *Writer { return w.U8(0xab) }, []byte{0xab}},
		{"I8 negative", func(w *Writer) *Writer { return w.I8(-1) }, []byte{0xff}},
		{"String", func(w *Writer) *Writer { return w.String("hi") }, []byte{2, 'h', 'i'}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// NewWriter(0) contributes a single 0x00 packet-id byte.
			got := tc.build(NewWriter(0)).Bytes()[1:]
			if !bytes.Equal(got, tc.want) {
				t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// BlockPos packs x:26, z:26, y:12 — with z in the middle, which is the usual
// place to get it wrong. A wrong packing addresses a real block somewhere else
// rather than erroring, so the round trip is the only thing that catches it.
func TestBlockPosRoundTrip(t *testing.T) {
	tests := []struct{ x, y, z int32 }{
		{0, 0, 0},
		{1, 2, 3},
		{-1, -1, -1},
		{-300, 76, -310},
		{100, -64, -100},
		{33554431, 2047, 33554431},    // maximum positive in each field
		{-33554432, -2048, -33554432}, // minimum negative in each field
	}
	for _, tc := range tests {
		packed := NewReader(NewWriter(0).BlockPos(tc.x, tc.y, tc.z).Bytes()[1:]).I64()
		x, y, z := DecodeBlockPos(packed)
		if x != tc.x || y != tc.y || z != tc.z {
			t.Errorf("BlockPos(%d,%d,%d) -> DecodeBlockPos = (%d,%d,%d)",
				tc.x, tc.y, tc.z, x, y, z)
		}
	}
}

// Hand-packed, so the field ordering is pinned independently of the encoder.
func TestDecodeBlockPosKnownValue(t *testing.T) {
	const packed = int64(1)<<38 | int64(2)<<12 | 3 // x=1, z=2, y=3
	x, y, z := DecodeBlockPos(packed)
	if x != 1 || y != 3 || z != 2 {
		t.Errorf("DecodeBlockPos(%#x) = (%d,%d,%d), want (1,3,2)", packed, x, y, z)
	}
}

func TestWriterChainsInFieldOrder(t *testing.T) {
	got := NewWriter(0x2a).VarInt(1).String("ab").I16(7).Bytes()
	want := []byte{0x2a, 0x01, 0x02, 'a', 'b', 0x00, 0x07}
	if !bytes.Equal(got, want) {
		t.Errorf("chained writer = %v, want %v", got, want)
	}
}
