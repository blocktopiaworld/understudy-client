package protocol

import "testing"

// Callers hand this package names typed by a human ("dirt"), while the wire
// only ever carries the qualified form. Normalising in one place is what keeps
// a bare name from silently matching nothing.
func TestNamespaced(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"zombie", "minecraft:zombie"},
		{"minecraft:zombie", "minecraft:zombie"},
		{"mypack:widget", "mypack:widget"},
		{"", "minecraft:"},
		{"a:b:c", "a:b:c"},
	} {
		if got := Namespaced(tc.in); got != tc.want {
			t.Errorf("Namespaced(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBareName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"minecraft:oak_log", "oak_log"},
		{"oak_log", "oak_log"},
		{"mypack:thing", "thing"},
		{"", ""},
		{"a:b:c", "b:c"},
	} {
		if got := BareName(tc.in); got != tc.want {
			t.Errorf("BareName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNamespacedBareNameRoundTrip(t *testing.T) {
	for _, name := range []string{"dirt", "minecraft:dirt", "diamond_pickaxe"} {
		if got := BareName(Namespaced(name)); got != BareName(name) {
			t.Errorf("BareName(Namespaced(%q)) = %q, want %q", name, got, BareName(name))
		}
	}
}

// The packet ID namespace is scoped to the state, so 0x00 means different
// things in each; decoding without tracking state is the classic way to
// misread a stream.
func TestStateString(t *testing.T) {
	for _, tc := range []struct {
		state State
		want  string
	}{
		{StateHandshaking, "handshaking"},
		{StateLogin, "login"},
		{StateConfiguration, "configuration"},
		{StatePlay, "play"},
		{State(99), "unknown"},
	} {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}

// The wire encodes a face as one signed byte, so an out-of-range value is
// truncated rather than rejected — face 260 becomes face 4 and the block is
// worked from a side the caller never named.
func TestValidFace(t *testing.T) {
	for face := int32(0); face < FaceCount; face++ {
		if !ValidFace(face) {
			t.Errorf("ValidFace(%d) = false, want true", face)
		}
	}
	for _, face := range []int32{-1, FaceCount, 6, 260, 1 << 20} {
		if ValidFace(face) {
			t.Errorf("ValidFace(%d) = true, want false", face)
		}
	}
	// The truncation this guards against: 260 narrows to 4, a *valid* face, so
	// an unchecked value works the wrong side of the block rather than failing.
	oversized := int32(260)
	if int32(int8(oversized)) != FaceWest {
		t.Errorf("int8(260) = %d, want %d — the truncation this guards against has changed",
			int8(oversized), FaceWest)
	}
}
