package versions

import (
	"testing"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// Importing this package must be enough to make every version available. That
// is the whole contract: the tables register themselves from init, so a caller
// blank-imports this once and protocol.ByName works.
func TestEveryTableRegistersItself(t *testing.T) {
	names := protocol.Names()
	if len(names) == 0 {
		t.Fatal("protocol.Names() is empty; no generated table registered itself")
	}

	for _, name := range names {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Fatalf("ByName(%q): %v", name, err)
		}
		got, err := protocol.ByProtocol(v.Protocol)
		if err != nil {
			t.Fatalf("ByProtocol(%d): %v", v.Protocol, err)
		}
		if got != v {
			t.Errorf("ByProtocol(%d) returned %q, want %q", v.Protocol, got.Name, name)
		}
	}
}

// The versions the client is actually built to speak. Losing one to a
// generator change should fail loudly rather than show up as "unsupported
// version" at connect time.
func TestExpectedVersionsArePresent(t *testing.T) {
	for name, proto := range map[string]int32{
		"26.1":    775,
		"1.21.11": 774,
		"1.21.4":  769,
	} {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Errorf("ByName(%q): %v", name, err)
			continue
		}
		if v.Protocol != proto {
			t.Errorf("%s is protocol %d, want %d", name, v.Protocol, proto)
		}
	}
}

// A missing packet ID is Absent (-1), which never matches a dispatch and never
// gets sent — so a table that lost one fails silently at runtime unless
// something checks here.
func TestRequiredPacketsArePresent(t *testing.T) {
	required := []struct {
		name string
		id   func(*protocol.Version) int32
	}{
		{"handshake", func(v *protocol.Version) int32 { return v.Packets.SBHandshake }},
		{"login_start", func(v *protocol.Version) int32 { return v.Packets.SBLoginStart }},
		{"play login", func(v *protocol.Version) int32 { return v.Packets.CBPlayLogin }},
		{"map_chunk", func(v *protocol.Version) int32 { return v.Packets.CBPlayMapChunk }},
		{"position (clientbound)", func(v *protocol.Version) int32 { return v.Packets.CBPlayPosition }},
		{"position_look", func(v *protocol.Version) int32 { return v.Packets.SBPlayPositionLook }},
		{"block_dig", func(v *protocol.Version) int32 { return v.Packets.SBPlayBlockDig }},
		{"block_place", func(v *protocol.Version) int32 { return v.Packets.SBPlayBlockPlace }},
		{"window_click", func(v *protocol.Version) int32 { return v.Packets.SBPlayWindowClick }},
		{"keep_alive", func(v *protocol.Version) int32 { return v.Packets.CBPlayKeepAlive }},
	}
	for _, name := range protocol.Names() {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, req := range required {
			if req.id(v) == protocol.Absent {
				t.Errorf("%s: %s is Absent, so the client would silently never use it", name, req.name)
			}
		}
	}
}

// Item lookups by name, checked against values that are the same in every
// supported version. This is a stronger check than counting the table: it
// proves the names resolve *and* that the stack sizes line up with them,
// which is what inventory maths depends on.
func TestItemTablesResolveKnownItems(t *testing.T) {
	want := map[string]int32{
		"minecraft:cobblestone":     64,
		"minecraft:oak_log":         64,
		"minecraft:diamond_pickaxe": 1,
		"minecraft:cooked_beef":     64,
	}
	for _, name := range protocol.Names() {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		for item, size := range want {
			id, ok := v.ItemID(item)
			if !ok {
				t.Errorf("%s: ItemID(%q) not found", name, item)
				continue
			}
			if back := v.ItemName(id); back != item {
				t.Errorf("%s: ItemName(ItemID(%q)) = %q, want a round trip", name, item, back)
			}
			if got := v.StackSizeOf(item); got != size {
				t.Errorf("%s: StackSizeOf(%q) = %d, want %d", name, item, got, size)
			}
		}
	}
}

func TestEntityTablesResolveKnownEntities(t *testing.T) {
	for _, name := range protocol.Names() {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		// Entity type IDs shift between versions, so scan rather than hard-code:
		// the table is right if the entities the harness drives are all in it.
		seen := map[string]bool{}
		for id := int32(0); id < 300; id++ {
			seen[v.EntityTypeName(id)] = true
		}
		for _, want := range []string{"minecraft:chicken", "minecraft:zombie", "minecraft:item"} {
			if !seen[want] {
				t.Errorf("%s: entity table has no %s", name, want)
			}
		}
	}
}

// Block-state classification underpins every reach and line-of-sight check, so
// a table that lost its ranges would make the client think the world is empty.
func TestBlockStateClassification(t *testing.T) {
	for _, name := range protocol.Names() {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		if !v.IsAir(protocol.AirState) {
			t.Errorf("%s: state %d is not classified as air", name, protocol.AirState)
		}
		if v.IsSolid(protocol.AirState) {
			t.Errorf("%s: air is classified as solid", name)
		}
		var solids int
		for state := int32(1); state < 2000; state++ {
			if v.IsSolid(state) {
				solids++
			}
		}
		if solids == 0 {
			t.Errorf("%s: no solid block states in the first 2000, so nothing would ever block a ray", name)
		}
	}
}

// Both flags are protocol-number thresholds, and both are invisible until
// wrong: a chunk decoded with the wrong shape yields plausible garbage rather
// than an error. 26.1 is protocol 775; the size prefix went away in 1.21.5 (770).
func TestChunkFormatFollowsTheProtocolNumber(t *testing.T) {
	for _, name := range protocol.Names() {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Fatal(err)
		}
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

// Component support lives in fields that genversion.mjs does not generate, in
// files that say DO NOT EDIT. Regenerating one drops them, and dropping them
// does not break the build — it just turns component decoding off for that
// version, quietly. This is the alarm on that.
func TestEveryVersionCarriesComponentTables(t *testing.T) {
	for _, name := range protocol.Names() {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Fatalf("registered version %q does not resolve: %v", name, err)
		}
		if !v.HasComponentIDs() {
			t.Errorf("%s has no component id table — if that file was just "+
				"regenerated, re-run internal/gen/gencomponents.mjs", name)
		}
		if _, ok := v.SlotDisplayKind(0); !ok {
			t.Errorf("%s has no slot display table, so its recipe book will stop "+
				"at the first composite ingredient", name)
		}
	}
}

// The versions whose component encodings were measured must keep them, for the
// same reason: losing the literal silently disables decoding rather than
// failing.
func TestMeasuredVersionsKeepTheirEncodings(t *testing.T) {
	for _, name := range []string{"26.1", "1.21.11", "1.21.4"} {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Skipf("%s is not registered in this build", name)
		}
		if _, ok := v.ComponentEncoding(); !ok {
			t.Errorf("%s had its component encodings measured against a running "+
				"server; the Components literal is missing", name)
		}
	}
}
