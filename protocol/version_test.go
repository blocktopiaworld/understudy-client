package protocol

import (
	"strings"
	"testing"
)

func classifyVersion() *Version {
	return NewVersion(VersionSpec{
		Name:        "classify",
		Protocol:    9999,
		EntityNames: []string{"minecraft:pig", "", "minecraft:zombie"},
		ItemNames:   []string{"minecraft:dirt", "minecraft:totem_of_undying", ""},
		ItemStacks:  []int32{64, 1, 0},
		Air:         [][2]int32{{0, 0}, {100, 102}},
		Solid:       [][2]int32{{1, 50}, {200, 300}},
		Water:       [][2]int32{{60, 63}},
		Lava:        [][2]int32{{70, 73}},
	})
}

// A gap in a generated table means a wire ID the version does not use. It must
// not shift the entries after it, and it must still produce a usable name.
func TestVersionNameLookups(t *testing.T) {
	v := classifyVersion()
	for _, tc := range []struct{ name, got, want string }{
		{"known entity", v.EntityTypeName(0), "minecraft:pig"},
		{"gap in the entity table", v.EntityTypeName(1), "minecraft:unknown/1"},
		{"entity past the end", v.EntityTypeName(99), "minecraft:unknown/99"},
		{"negative entity id", v.EntityTypeName(-1), "minecraft:unknown/-1"},
		{"known item", v.ItemName(1), "minecraft:totem_of_undying"},
		{"gap in the item table", v.ItemName(2), "minecraft:unknown/2"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// Totems stack to 1, so "hold 5 totems" needs five whole slots, while
// "hold 2304 dirt" needs exactly 36 — every storage slot a player has.
func TestStackSize(t *testing.T) {
	v := classifyVersion()
	for _, tc := range []struct {
		name string
		got  int32
		want int32
	}{
		{"by id", v.StackSize(1), 1},
		{"by bare name", v.StackSizeOf("totem_of_undying"), 1},
		{"by namespaced name", v.StackSizeOf("minecraft:totem_of_undying"), 1},
		{"ordinary item", v.StackSizeOf("dirt"), 64},
		{"unknown name falls back", v.StackSizeOf("not_a_real_item"), DefaultStackSize},
		{"unknown id falls back", v.StackSize(1000), DefaultStackSize},
		{"negative id falls back", v.StackSize(-1), DefaultStackSize},
		{"zero in the table falls back", v.StackSize(2), DefaultStackSize},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestItemIDIsStableAcrossCalls(t *testing.T) {
	v := classifyVersion()
	if id, ok := v.ItemID("dirt"); !ok || id != 0 {
		t.Errorf("ItemID(dirt) = %d, %v; want 0, true", id, ok)
	}
	if id, ok := v.ItemID("minecraft:dirt"); !ok || id != 0 {
		t.Errorf("ItemID(minecraft:dirt) = %d, %v; want 0, true", id, ok)
	}
	if _, ok := v.ItemID("nonsense"); ok {
		t.Error("ItemID(nonsense) reported found, want not found")
	}
	// The index is built once under a sync.Once; a second call must agree.
	if id, ok := v.ItemID("totem_of_undying"); !ok || id != 1 {
		t.Errorf("second ItemID = %d, %v; want 1, true", id, ok)
	}
}

// The classification tables are sorted ranges searched by bisection, so the
// interesting cases are the boundaries and the gaps between ranges.
func TestBlockClassification(t *testing.T) {
	v := classifyVersion()
	for _, tc := range []struct {
		state                          int32
		air, solid, water, lava, fluid bool
		targetable                     bool
	}{
		{state: 0, air: true},
		{state: 1, solid: true, targetable: true},
		{state: 50, solid: true, targetable: true},
		{state: 51, targetable: true}, // in no range: not air, not fluid
		{state: 60, water: true, fluid: true},
		{state: 63, water: true, fluid: true},
		{state: 64, targetable: true},
		{state: 70, lava: true, fluid: true},
		{state: 100, air: true},
		{state: 102, air: true},
		{state: 103, targetable: true},
		{state: 250, solid: true, targetable: true},
		{state: -1, targetable: true},
	} {
		if got := v.IsAir(tc.state); got != tc.air {
			t.Errorf("IsAir(%d) = %v, want %v", tc.state, got, tc.air)
		}
		if got := v.IsSolid(tc.state); got != tc.solid {
			t.Errorf("IsSolid(%d) = %v, want %v", tc.state, got, tc.solid)
		}
		if got := v.IsWater(tc.state); got != tc.water {
			t.Errorf("IsWater(%d) = %v, want %v", tc.state, got, tc.water)
		}
		if got := v.IsLava(tc.state); got != tc.lava {
			t.Errorf("IsLava(%d) = %v, want %v", tc.state, got, tc.lava)
		}
		if got := v.IsFluid(tc.state); got != tc.fluid {
			t.Errorf("IsFluid(%d) = %v, want %v", tc.state, got, tc.fluid)
		}
		// Targetable is deliberately NOT solid: the crosshair stops on cobweb
		// and crops, and passes through water.
		if got := v.IsTargetable(tc.state); got != tc.targetable {
			t.Errorf("IsTargetable(%d) = %v, want %v", tc.state, got, tc.targetable)
		}
	}
}

func TestInRangesEmpty(t *testing.T) {
	if inRanges(nil, 5) {
		t.Error("inRanges(nil, 5) = true, want false")
	}
}

// Before 26.1 attacking folded into use_entity with a mode field, which this
// client does not implement — so it must error rather than silently no-op.
func TestSupportsAttackPacket(t *testing.T) {
	with := NewVersion(VersionSpec{Packets: PacketIDs{SBPlayAttack: 1}})
	without := NewVersion(VersionSpec{Packets: PacketIDs{SBPlayAttack: Absent}})
	if !with.SupportsAttackPacket() {
		t.Error("SupportsAttackPacket() = false with a real packet id, want true")
	}
	if without.SupportsAttackPacket() {
		t.Error("SupportsAttackPacket() = true with Absent, want false")
	}
}

// --- the generated tables --------------------------------------------------

// Importing the package must be enough to make every version available.
func TestRegistryHasGeneratedVersions(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("Names() is empty; no generated version registered itself")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("Names() is not sorted: %q before %q", names[i-1], names[i])
		}
	}
	for _, name := range names {
		v, err := ByName(name)
		if err != nil {
			t.Fatalf("ByName(%q): %v", name, err)
		}
		got, err := ByProtocol(v.Protocol)
		if err != nil {
			t.Fatalf("ByProtocol(%d): %v", v.Protocol, err)
		}
		if got != v {
			t.Errorf("ByProtocol(%d) returned %q, want %q", v.Protocol, got.Name, name)
		}
	}
}

// Every generated table must be usable for the things the client actually
// does. A missing packet ID is -1, which never matches and never sends — so
// the failure is silent unless something checks.
func TestGeneratedVersionsAreComplete(t *testing.T) {
	required := []struct {
		name string
		id   func(*Version) int32
	}{
		{"handshake", func(v *Version) int32 { return v.Packets.SBHandshake }},
		{"login_start", func(v *Version) int32 { return v.Packets.SBLoginStart }},
		{"play login", func(v *Version) int32 { return v.Packets.CBPlayLogin }},
		{"map_chunk", func(v *Version) int32 { return v.Packets.CBPlayMapChunk }},
		{"position (clientbound)", func(v *Version) int32 { return v.Packets.CBPlayPosition }},
		{"position_look", func(v *Version) int32 { return v.Packets.SBPlayPositionLook }},
		{"block_dig", func(v *Version) int32 { return v.Packets.SBPlayBlockDig }},
		{"block_place", func(v *Version) int32 { return v.Packets.SBPlayBlockPlace }},
		{"window_click", func(v *Version) int32 { return v.Packets.SBPlayWindowClick }},
		{"keep_alive", func(v *Version) int32 { return v.Packets.CBPlayKeepAlive }},
	}
	for _, name := range Names() {
		v, err := ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, req := range required {
			if id := req.id(v); id < 0 {
				t.Errorf("%s: %s is Absent, so the client would silently never use it", name, req.name)
			}
		}
		if len(v.entityNames) < 50 {
			t.Errorf("%s: only %d entity names", name, len(v.entityNames))
		}
		if len(v.itemNames) < 500 {
			t.Errorf("%s: only %d item names", name, len(v.itemNames))
		}
		if !v.IsAir(AirState) {
			t.Errorf("%s: state 0 is not classified as air", name)
		}
		if len(v.solidStates) < 10 {
			t.Errorf("%s: only %d solid ranges", name, len(v.solidStates))
		}
		if len(v.waterStates) == 0 {
			t.Errorf("%s: no water states", name)
		}
	}
}

// Both flags are protocol-number thresholds, and both are invisible until
// wrong. 26.1 is protocol 775; the size prefix went away in 1.21.5 (770).
func TestGeneratedChunkFormatFollowsTheProtocolNumber(t *testing.T) {
	for _, name := range Names() {
		v, _ := ByName(name)
		wantSizePrefix := v.Protocol < 770
		wantFluidCount := v.Protocol >= 775
		if v.Chunk.HasSizePrefix != wantSizePrefix {
			t.Errorf("%s (protocol %d): HasSizePrefix = %v, want %v",
				name, v.Protocol, v.Chunk.HasSizePrefix, wantSizePrefix)
		}
		if v.Chunk.HasFluidCount != wantFluidCount {
			t.Errorf("%s (protocol %d): HasFluidCount = %v, want %v",
				name, v.Protocol, v.Chunk.HasFluidCount, wantFluidCount)
		}
	}
}

func TestRegistryUnknownLookups(t *testing.T) {
	_, err := ByName("not-a-version")
	if err == nil {
		t.Fatal("ByName of an unknown version = nil error, want an error")
	}
	// The error has to list what *is* available, or the operator has no next step.
	if !strings.Contains(err.Error(), Names()[0]) {
		t.Errorf("ByName error %q does not list the supported versions", err)
	}
	if _, err := ByProtocol(-12345); err == nil {
		t.Error("ByProtocol of an unknown protocol = nil error, want an error")
	}
}

// A duplicate can only be a mistake in the generated tables, and silently
// keeping the last one would make auto-detection pick at random.
func TestRegisterRejectsDuplicates(t *testing.T) {
	Register(NewVersion(VersionSpec{Name: "dup-test", Protocol: 990001}))
	t.Cleanup(func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		delete(byName, "dup-test")
		delete(byProtocol, 990001)
	})

	for _, tc := range []struct {
		name string
		dup  *Version
	}{
		{"same name", NewVersion(VersionSpec{Name: "dup-test", Protocol: 990002})},
		{"same protocol", NewVersion(VersionSpec{Name: "dup-test-other", Protocol: 990001})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Register with a duplicate %s did not panic", tc.name)
				}
			}()
			Register(tc.dup)
		})
	}
}
