package versions

import (
	"testing"

	"github.com/block-topia/understudy-client/protocol"
)

// The point of carrying real shapes is the cases boundingBox cannot express.
//
// minecraft-data calls every one of stone, oak_slab, oak_fence, white_carpet,
// glass_pane and cobblestone_wall a "block", so the old boolean treated a
// carpet the same as a wall. Movement cares enormously: 0.0625 is walked over
// without noticing, 0.5 is a free step up, 1.0 needs a jump and 1.5 cannot be
// climbed at all.
//
// State IDs are 26.1's and will move between versions — that is why they are
// asserted here, against the version they came from, rather than assumed
// anywhere in the client.
func TestCollisionHeightsFor26_1(t *testing.T) {
	v, err := protocol.ByName("26.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		state int32
		want  float64
	}{
		{"air", 0, 0},
		{"stone", 1, 1.0},
		{"torch", 3370, 0},
		{"oak_stairs (bottom half)", 3918, 0.5},
		{"snow, one layer", 6919, 0},
		{"snow, two layers", 6920, 0.125},
		{"oak_fence", 6996, 1.5},
		{"white_carpet", 12896, 0.0625},
		{"oak_slab (bottom)", 13333, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := v.CollisionHeight(tc.state); got != tc.want {
				t.Errorf("CollisionHeight(%d) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// A fence is 1.5 blocks tall, so it is a wall rather than a step. This is the
// single most useful thing the shapes buy: IsSolid says "solid" for both a
// fence and a slab, and a bot that steps onto the fence walks into it and
// stalls with no error to explain why.
func TestFenceIsTallerThanAFullBlock(t *testing.T) {
	v, err := protocol.ByName("26.1")
	if err != nil {
		t.Fatal(err)
	}
	const fence, stone, slab = 6996, 1, 13333

	// The coarse classification cannot tell them apart...
	if !v.IsSolid(fence) || !v.IsSolid(stone) || !v.IsSolid(slab) {
		t.Fatal("IsSolid should call all three solid — if not, this test is checking the wrong thing")
	}
	// ...but the shapes can.
	if !(v.CollisionHeight(slab) < v.CollisionHeight(stone) &&
		v.CollisionHeight(stone) < v.CollisionHeight(fence)) {
		t.Errorf("heights are slab=%v stone=%v fence=%v, want strictly increasing",
			v.CollisionHeight(slab), v.CollisionHeight(stone), v.CollisionHeight(fence))
	}
	if v.IsFullCube(fence) || v.IsFullCube(slab) {
		t.Error("a fence and a slab are not full cubes")
	}
	if !v.IsFullCube(stone) {
		t.Error("stone is a full cube")
	}
}

// A state the tables do not describe must not read as empty air. Movement code
// treating unknown as clear walks a bot confidently into it.
func TestUnknownStateIsNotEmptyAir(t *testing.T) {
	v, err := protocol.ByName("26.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []int32{-1, 1 << 30} {
		if v.HasCollisionData(state) {
			t.Errorf("HasCollisionData(%d) = true, want false", state)
		}
		if v.CollisionShape(state) != nil {
			t.Errorf("CollisionShape(%d) returned boxes for an unmapped state", state)
		}
	}
	// Air *is* described, and is genuinely empty — the two must not look alike.
	if !v.HasCollisionData(0) {
		t.Error("air should be described by the tables")
	}
	if v.BlocksMovement(0) {
		t.Error("air should not block movement")
	}
}

// Every version must describe a contiguous block of states starting at air.
//
// A hole would mean some real block silently reads as "unknown", and since
// movement has to treat unknown as an obstacle, that shows up as a bot
// refusing to walk through a perfectly ordinary doorway.
func TestEveryStateIsDescribed(t *testing.T) {
	for _, name := range protocol.Names() {
		v, err := protocol.ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		// Find the top of the table, then assert there are no gaps below it.
		var top int32
		for state := int32(0); state < 1<<17; state++ {
			if v.HasCollisionData(state) {
				top = state
			}
		}
		if top < 20_000 {
			t.Errorf("%s: highest described state is %d, want a full block table", name, top)
		}
		for state := int32(0); state <= top; state++ {
			if !v.HasCollisionData(state) {
				t.Fatalf("%s: state %d is a hole in the table (top is %d)", name, state, top)
			}
		}
	}
}
