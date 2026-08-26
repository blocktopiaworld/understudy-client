package protocol

import (
	"errors"
	"math"
	"runtime"
	"strings"
	"testing"
)

// Every fixed-width accessor, round-tripped through the writer. A field read
// at the wrong width shifts everything after it, which surfaces as a short
// read somewhere unrelated rather than here.
func TestReaderFixedWidth(t *testing.T) {
	want := UUID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	w := NewWriter(0).
		Bool(true).Bool(false).
		U8(0xfe).I8(-2).
		I16(-30000).I32(-2000000000).I64(-9000000000000000000).
		F32(1.5).F64(-2.25).
		String("hello").
		UUID(want)

	r := NewReader(w.Bytes())
	if id := r.VarInt(); id != 0 {
		t.Errorf("packet id = %d, want 0", id)
	}
	if got := r.Bool(); !got {
		t.Errorf("Bool() = %v, want true", got)
	}
	if got := r.Bool(); got {
		t.Errorf("Bool() = %v, want false", got)
	}
	if got := r.U8(); got != 0xfe {
		t.Errorf("U8() = %#x, want 0xfe", got)
	}
	if got := r.I8(); got != -2 {
		t.Errorf("I8() = %d, want -2", got)
	}
	if got := r.I16(); got != -30000 {
		t.Errorf("I16() = %d, want -30000", got)
	}
	if got := r.I32(); got != -2000000000 {
		t.Errorf("I32() = %d, want -2000000000", got)
	}
	if got := r.I64(); got != -9000000000000000000 {
		t.Errorf("I64() = %d, want -9000000000000000000", got)
	}
	if got := r.F32(); got != 1.5 {
		t.Errorf("F32() = %v, want 1.5", got)
	}
	if got := r.F64(); got != -2.25 {
		t.Errorf("F64() = %v, want -2.25", got)
	}
	if got := r.String(); got != "hello" {
		t.Errorf("String() = %q, want %q", got, "hello")
	}
	if got := r.UUID(); got != want {
		t.Errorf("UUID() = %v, want %v", got, want)
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	if rest := r.Remaining(); len(rest) != 0 {
		t.Errorf("Remaining() = %d bytes, want 0", len(rest))
	}
}

// The first failure is sticky so a decoder can read a whole packet and check
// once at the end. A later error overwriting the first would point at the
// wrong field.
func TestReaderShortReadIsSticky(t *testing.T) {
	r := NewReader([]byte{0x01, 0x02})
	if got := r.U8(); got != 1 {
		t.Fatalf("U8() = %d, want 1", got)
	}
	if got := r.I32(); got != 0 {
		t.Errorf("I32() past the end = %d, want the zero value", got)
	}
	first := r.Err()
	if first == nil {
		t.Fatal("Err() = nil after a short read, want an error")
	}
	_ = r.I64()
	if !errors.Is(r.Err(), first) {
		t.Errorf("Err() changed after a second failed read: %v, want the original %v", r.Err(), first)
	}
	if rest := r.Remaining(); rest != nil {
		t.Errorf("Remaining() = %v after an error, want nil", rest)
	}
}

// A desynced stream puts arbitrary bytes where a length prefix should be. The
// reader must refuse to size an allocation from one.
//
// Regression: take() used to return make([]byte, n) on the failure path, so a
// bogus VarInt of 2^30 allocated a gigabyte *and* returned it as a string,
// from a single corrupt packet.
func TestReaderDoesNotAllocateOnBogusLength(t *testing.T) {
	const bogus = 1 << 30

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	r := NewReader(AppendVarInt(nil, bogus))
	got := r.String()

	runtime.ReadMemStats(&after)

	if got != "" {
		t.Errorf("String() returned %d bytes for a truncated %d-byte string, want %q",
			len(got), bogus, "")
	}
	if r.Err() == nil {
		t.Error("Err() = nil for an out-of-range string length, want an error")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Errorf("decoding a bogus length allocated %d bytes, want well under 1 MiB", grew)
	}
}

func TestReaderStringLengthBounds(t *testing.T) {
	tests := []struct {
		name    string
		length  int32
		wantErr bool
	}{
		{name: "empty", length: 0},
		{name: "negative", length: -1, wantErr: true},
		{name: "at the cap with no body", length: MaxStringLen, wantErr: true},
		{name: "over the cap", length: MaxStringLen + 1, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReader(AppendVarInt(nil, tc.length))
			got := r.String()
			if gotErr := r.Err() != nil; gotErr != tc.wantErr {
				t.Errorf("String() with length %d: err = %v, want error: %v",
					tc.length, r.Err(), tc.wantErr)
			}
			if tc.wantErr && got != "" {
				t.Errorf("String() = %q on failure, want %q", got, "")
			}
		})
	}
}

func TestReaderStringRoundTrip(t *testing.T) {
	for _, s := range []string{"", "a", "minecraft:diamond_pickaxe", strings.Repeat("x", 4096), "héllo ☃"} {
		r := NewReader(NewWriter(0).String(s).Bytes())
		r.VarInt() // packet id
		if got := r.String(); got != s {
			t.Errorf("round trip of %d-byte string = %q, want %q", len(s), got, s)
		}
		if err := r.Err(); err != nil {
			t.Errorf("round trip of %q: Err() = %v", s, err)
		}
	}
}

func TestReaderByteReader(t *testing.T) {
	r := NewReader([]byte{0x7f})
	b, err := r.ReadByte()
	if err != nil || b != 0x7f {
		t.Fatalf("ReadByte() = %#x, %v; want 0x7f, nil", b, err)
	}
	if _, err := r.ReadByte(); err == nil {
		t.Error("ReadByte() past the end = nil error, want an error")
	}
}

func TestReaderFloatsSurviveSpecialValues(t *testing.T) {
	w := NewWriter(0).F32(float32(math.Inf(-1))).F64(math.Inf(1))
	r := NewReader(w.Bytes())
	r.VarInt()
	if got := r.F32(); !math.IsInf(float64(got), -1) {
		t.Errorf("F32() = %v, want -Inf", got)
	}
	if got := r.F64(); !math.IsInf(got, 1) {
		t.Errorf("F64() = %v, want +Inf", got)
	}
}

func TestReaderNeverAllocatesOnTheFailurePath(t *testing.T) {
	r := NewReader([]byte{0x01})
	r.fail(errors.New("seeded"))
	if got := r.take(1 << 30); len(got) > len(zeroPad) {
		t.Errorf("take(%d) after an error returned %d bytes, want at most %d",
			1<<30, len(got), len(zeroPad))
	}
}

// Remaining aliases the buffer, so a decoder that keeps it must not be handed
// something that moves underneath it.
func TestReaderRemainingIsTheUndecodedTail(t *testing.T) {
	r := NewReader([]byte{1, 2, 3, 4, 5})
	r.U8()
	r.U8()
	got := r.Remaining()
	if len(got) != 3 || got[0] != 3 {
		t.Errorf("Remaining() = %v, want [3 4 5]", got)
	}
}
