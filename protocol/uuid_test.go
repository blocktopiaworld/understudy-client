package protocol

import "testing"

// On an offline-mode server the UUID is the player's identity, derived from
// the name by RFC-4122 v3 (MD5) over "OfflinePlayer:"+name. Anything else
// addresses a player who does not exist, so these vectors are load-bearing.
//
// They were produced by an independent implementation of Java's
// UUID.nameUUIDFromBytes rather than copied from this code — a vector taken
// from the thing it is testing proves nothing. Beware: a search for a
// well-known player's UUID returns their *online-mode* Mojang one, which is a
// different value entirely.
func TestOfflineUUIDMatchesVanilla(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Notch", "b50ad385-829d-3141-a216-7e7d7539ba7f"},
		{"jeb_", "a762f560-4fce-3236-812a-b80efff0b62b"},
		{"Dinnerbone", "4d258a81-2358-3084-8166-05b9faccad80"},
		{"Understudy", "e2c3b064-2872-38a0-b245-2b1fd4c1d0fe"},
		{"", "fc5bc365-aedf-30a8-8b89-04e462e29bde"},
	}
	for _, tc := range tests {
		if got := OfflineUUID(tc.name).String(); got != tc.want {
			t.Errorf("OfflineUUID(%q) = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestOfflineUUIDSetsVersionAndVariant(t *testing.T) {
	u := OfflineUUID("AnyName")
	if v := u[6] >> 4; v != 3 {
		t.Errorf("version nibble = %d, want 3 (RFC-4122 v3)", v)
	}
	if variant := u[8] >> 6; variant != 0b10 {
		t.Errorf("variant bits = %#b, want 0b10 (RFC-4122)", variant)
	}
}

func TestOfflineUUIDIsDeterministicAndDistinct(t *testing.T) {
	if OfflineUUID("Same") != OfflineUUID("Same") {
		t.Error("OfflineUUID is not deterministic")
	}
	if OfflineUUID("One") == OfflineUUID("Two") {
		t.Error("different names produced the same UUID")
	}
}

func TestUUIDStringFormat(t *testing.T) {
	u := UUID{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10}
	want := "01234567-89ab-cdef-fedc-ba9876543210"
	if got := u.String(); got != want {
		t.Errorf("UUID.String() = %q, want %q", got, want)
	}
}

func TestUUIDSurvivesTheWire(t *testing.T) {
	want := OfflineUUID("RoundTrip")
	r := NewReader(NewWriter(0).UUID(want).Bytes())
	r.VarInt() // packet id
	if got := r.UUID(); got != want {
		t.Errorf("UUID round trip = %s, want %s", got, want)
	}
}
