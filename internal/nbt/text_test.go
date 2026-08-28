package nbt

import (
	"strings"
	"testing"
)

// A kick reason arrives as NBT, and this is the most important string the
// client ever reports. The point is that the readable parts survive between
// the tag bytes.
func TestReadableTextRecoversAKickReason(t *testing.T) {
	// A crude NBT-ish blob: tag bytes, a readable run, more tag bytes.
	blob := []byte{0x0a, 0x00, 0x00, 0x08}
	blob = append(blob, "translate"...)
	blob = append(blob, 0x00, 0x1d)
	blob = append(blob, "multiplayer.disconnect.flying"...)
	blob = append(blob, 0x00)

	got := ReadableText(blob)
	for _, want := range []string{"translate", "multiplayer.disconnect.flying"} {
		if !strings.Contains(got, want) {
			t.Errorf("ReadableText() = %q, want it to contain %q", got, want)
		}
	}
}

func TestReadableTextDropsBinaryNoise(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", nil, "(unreadable reason)"},
		{"all high bytes", []byte{0xff, 0xfe, 0x80, 0x90}, "(unreadable reason)"},
		{"all control bytes", []byte{0x00, 0x01, 0x02, 0x1f}, "(unreadable reason)"},
		// Runs shorter than minRun are tag fragments, not message text.
		{"runs too short", []byte{'a', 0x00, 'b', 0x00, 'c'}, "(unreadable reason)"},
		{"one long enough run", []byte{0x00, 'h', 'e', 'l', 'l', 'o', 0x00}, "hello"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReadableText(tc.in); got != tc.want {
				t.Errorf("ReadableText(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestReadableTextJoinsSeparateRuns(t *testing.T) {
	blob := append([]byte("first"), 0x00, 0x0a)
	blob = append(blob, "second"...)
	if got := ReadableText(blob); got != "first second" {
		t.Errorf("ReadableText() = %q, want %q", got, "first second")
	}
}

// High bytes must not be admitted: they would let NBT tag headers back in as
// mojibake, which is the failure this function exists to avoid.
func TestReadableTextRejectsHighBytes(t *testing.T) {
	blob := []byte("goodtext")
	blob = append(blob, 0xc3, 0xa9)
	blob = append(blob, "moretext"...)
	got := ReadableText(blob)
	if strings.ContainsRune(got, 0xc3) || strings.ContainsRune(got, 'é') {
		t.Errorf("ReadableText() = %q, want no high bytes", got)
	}
	if !strings.Contains(got, "goodtext") || !strings.Contains(got, "moretext") {
		t.Errorf("ReadableText() = %q, want both readable runs", got)
	}
}
