package protocol

import "testing"

// shapeVersion builds a version whose only interesting content is geometry:
// state 1 a full cube, 2 a slab, 3 a fence (taller than its own block), 4 a
// carpet, 5 explicitly empty, and a gap at 6 to stand for an unmapped state.
func shapeVersion() *Version {
	const u = ShapeUnit
	return NewVersion(VersionSpec{
		Name:     "shapes",
		Protocol: 9999,
		Shapes: [][]Box{
			0: nil,                                  // no collision
			1: {FullCube},                           // full block
			2: {{0, 0, 0, u, u / 2, u}},             // slab
			3: {{6, 0, 6, u - 6, u * 3 / 2, u - 6}}, // fence: 1.5 tall
			4: {{0, 0, 0, u, u / 16, u}},            // carpet
			5: {{0, 0, 0, 0, 0, 0}},                 // degenerate: present but encloses nothing
		},
		ShapeRuns: [][3]int32{
			{0, 0, 0},
			{1, 1, 1},
			{2, 2, 2},
			{3, 3, 3},
			{4, 4, 4},
			{5, 5, 5},
			// 6 deliberately absent
			{7, 9, 1},
		},
	})
}

func TestCollisionHeight(t *testing.T) {
	v := shapeVersion()
	for _, tc := range []struct {
		name  string
		state int32
		want  float64
	}{
		{"nothing", 0, 0},
		{"full cube", 1, 1},
		{"slab", 2, 0.5},
		{"fence stands above its own block", 3, 1.5},
		{"carpet", 4, 0.0625},
		{"degenerate box", 5, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := v.CollisionHeight(tc.state); got != tc.want {
				t.Errorf("CollisionHeight(%d) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// A run covering several states must answer for all of them, which is what
// keeps the table from needing one row per state.
func TestShapeRunsCoverTheirWholeRange(t *testing.T) {
	v := shapeVersion()
	for state := int32(7); state <= 9; state++ {
		if !v.IsFullCube(state) {
			t.Errorf("state %d is inside the {7,9} run and should be a full cube", state)
		}
	}
}

// An unmapped state is not empty air. Reading it as clear is how a bot walks
// into something it cannot see.
func TestUnmappedStateReportsNoData(t *testing.T) {
	v := shapeVersion()
	for _, state := range []int32{-5, 6, 10, 1 << 20} {
		if v.HasCollisionData(state) {
			t.Errorf("HasCollisionData(%d) = true, want false", state)
		}
		if got := v.CollisionShape(state); got != nil {
			t.Errorf("CollisionShape(%d) = %v, want nil", state, got)
		}
		if v.BlocksMovement(state) {
			t.Errorf("BlocksMovement(%d) = true; unmapped is not the same as solid", state)
		}
	}
	// The distinction that matters: state 0 IS described and IS empty.
	if !v.HasCollisionData(0) || v.BlocksMovement(0) {
		t.Error("state 0 should be described and non-blocking")
	}
}

func TestBlocksMovementIgnoresDegenerateBoxes(t *testing.T) {
	v := shapeVersion()
	if v.BlocksMovement(5) {
		t.Error("a box enclosing nothing does not block movement")
	}
	if !v.BlocksMovement(4) {
		t.Error("a carpet is thin but does collide")
	}
}

func TestBoxAccessorsAndEmpty(t *testing.T) {
	b := Box{1, 2, 3, 4, 6, 8}
	if b.MinX() != 1 || b.MinY() != 2 || b.MinZ() != 3 ||
		b.MaxX() != 4 || b.MaxY() != 6 || b.MaxZ() != 8 {
		t.Errorf("accessors returned the wrong fields for %v", b)
	}
	if b.Empty() {
		t.Error("a box with extent is not empty")
	}
	if got, want := b.Height(), 4.0/ShapeUnit; got != want {
		t.Errorf("Height() = %v, want %v", got, want)
	}
	for _, degenerate := range []Box{{}, {0, 0, 0, 0, 5, 5}, {5, 0, 0, 1, 5, 5}} {
		if !degenerate.Empty() {
			t.Errorf("%v encloses nothing and should be Empty", degenerate)
		}
	}
}

// The binary search must land on the right run regardless of where the state
// sits, so walk every boundary rather than trusting a couple of samples.
func TestShapeLookupAcrossEveryBoundary(t *testing.T) {
	v := shapeVersion()
	want := map[int32]int{0: 0, 1: 1, 2: 1, 3: 1, 4: 1, 5: 1, 7: 1, 8: 1, 9: 1}
	for state, boxes := range want {
		if got := len(v.CollisionShape(state)); got != boxes {
			t.Errorf("CollisionShape(%d) has %d boxes, want %d", state, got, boxes)
		}
	}
}
