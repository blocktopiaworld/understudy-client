package protocol

import (
	"bytes"
	"testing"
)

// The canonical VarInt vectors. A desynced length prefix would otherwise be
// read as a plausible packet boundary and corrupt everything after it.
func TestVarIntKnownVectors(t *testing.T) {
	tests := []struct {
		value int32
		want  []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{2, []byte{0x02}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{255, []byte{0xff, 0x01}},
		{25565, []byte{0xdd, 0xc7, 0x01}},
		{2097151, []byte{0xff, 0xff, 0x7f}},
		{2147483647, []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
		{-1, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
		{-2147483648, []byte{0x80, 0x80, 0x80, 0x80, 0x08}},
	}
	for _, tc := range tests {
		got := AppendVarInt(nil, tc.value)
		if !bytes.Equal(got, tc.want) {
			t.Errorf("AppendVarInt(%d) = %v, want %v", tc.value, got, tc.want)
		}
		if n := VarIntLen(tc.value); n != len(tc.want) {
			t.Errorf("VarIntLen(%d) = %d, want %d", tc.value, n, len(tc.want))
		}
		back, err := ReadVarInt(bytes.NewReader(got))
		if err != nil {
			t.Errorf("ReadVarInt(%v): %v", got, err)
			continue
		}
		if back != tc.value {
			t.Errorf("round trip of %d gave %d", tc.value, back)
		}
	}
}

func TestVarLongRoundTrip(t *testing.T) {
	for _, v := range []int64{0, 1, 127, 128, 2147483647, -1, 9223372036854775807, -9223372036854775808} {
		encoded := AppendVarLong(nil, v)
		got, err := ReadVarLong(bytes.NewReader(encoded))
		if err != nil {
			t.Errorf("ReadVarLong(%d): %v", v, err)
			continue
		}
		if got != v {
			t.Errorf("round trip of %d gave %d", v, got)
		}
	}
}

// A run longer than the cap is a malformed stream, and it has to be an error
// rather than a silent truncation.
func TestVarIntRejectsOverlongEncoding(t *testing.T) {
	overlong := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	if _, err := ReadVarInt(bytes.NewReader(overlong)); err == nil {
		t.Error("ReadVarInt of a 6-byte run = nil error, want an error")
	}
	tooLong := bytes.Repeat([]byte{0xff}, 11)
	if _, err := ReadVarLong(bytes.NewReader(tooLong)); err == nil {
		t.Error("ReadVarLong of an 11-byte run = nil error, want an error")
	}
}

func TestVarIntTruncated(t *testing.T) {
	// A continuation bit with nothing following it.
	if _, err := ReadVarInt(bytes.NewReader([]byte{0x80})); err == nil {
		t.Error("ReadVarInt of a truncated encoding = nil error, want an error")
	}
	if _, err := ReadVarInt(bytes.NewReader(nil)); err == nil {
		t.Error("ReadVarInt of no bytes = nil error, want an error")
	}
}
