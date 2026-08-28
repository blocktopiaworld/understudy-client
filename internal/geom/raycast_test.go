package geom

import "testing"

// solidAt returns a stop predicate over an explicit set of voxels, so a test
// can describe terrain without any notion of blocks or a server.
func solidAt(voxels ...[3]int32) func(x, y, z int32) bool {
	set := make(map[[3]int32]bool, len(voxels))
	for _, v := range voxels {
		set[v] = true
	}
	return func(x, y, z int32) bool { return set[[3]int32{x, y, z}] }
}

func TestRaycastHitsTheNearestVoxel(t *testing.T) {
	hit, ok := Raycast(0.5, 0.5, 0.5, 1, 0, 0, 10,
		solidAt([3]int32{3, 0, 0}, [3]int32{5, 0, 0}))
	if !ok {
		t.Fatal("Raycast found nothing, want the voxel at x=3")
	}
	if hit.X != 3 || hit.Y != 0 || hit.Z != 0 {
		t.Errorf("hit = (%d,%d,%d), want (3,0,0) — the nearer voxel", hit.X, hit.Y, hit.Z)
	}
	if !closeEnough(hit.Distance, 2.5, 1e-9) {
		t.Errorf("Distance = %g, want 2.5", hit.Distance)
	}
}

// Moving in +X enters the west face, so the sides are swapped relative to the
// direction of travel. A wrong face places a block against the wrong side.
func TestRaycastFaces(t *testing.T) {
	tests := []struct {
		name       string
		dx, dy, dz float64
		target     [3]int32
		wantFace   int32
	}{
		{"travelling +X enters the west face", 1, 0, 0, [3]int32{3, 0, 0}, FaceWest},
		{"travelling -X enters the east face", -1, 0, 0, [3]int32{-3, 0, 0}, FaceEast},
		{"travelling +Y enters the bottom face", 0, 1, 0, [3]int32{0, 3, 0}, FaceBottom},
		{"travelling -Y enters the top face", 0, -1, 0, [3]int32{0, -3, 0}, FaceTop},
		{"travelling +Z enters the north face", 0, 0, 1, [3]int32{0, 0, 3}, FaceNorth},
		{"travelling -Z enters the south face", 0, 0, -1, [3]int32{0, 0, -3}, FaceSouth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hit, ok := Raycast(0.5, 0.5, 0.5, tc.dx, tc.dy, tc.dz, 10, solidAt(tc.target))
			if !ok {
				t.Fatalf("Raycast found nothing, want %v", tc.target)
			}
			if hit.Face != tc.wantFace {
				t.Errorf("Face = %d, want %d", hit.Face, tc.wantFace)
			}
		})
	}
}

func TestRaycastRespectsMaxDistance(t *testing.T) {
	far := solidAt([3]int32{8, 0, 0})
	if _, ok := Raycast(0.5, 0.5, 0.5, 1, 0, 0, 3, far); ok {
		t.Error("Raycast hit a voxel 7.5 away with maxDist 3, want no hit")
	}
	if _, ok := Raycast(0.5, 0.5, 0.5, 1, 0, 0, 10, far); !ok {
		t.Error("Raycast found nothing with maxDist 10, want the voxel")
	}
}

// A zero direction vector must terminate rather than spin: every axis gets an
// infinite boundary distance, so the first step ends the walk.
func TestRaycastZeroDirectionTerminates(t *testing.T) {
	if _, ok := Raycast(0.5, 0.5, 0.5, 0, 0, 0, 10, solidAt([3]int32{5, 5, 5})); ok {
		t.Error("Raycast with a zero direction reported a hit, want none")
	}
}

func TestRaycastStartingInsideAVoxel(t *testing.T) {
	hit, ok := Raycast(0.5, 0.5, 0.5, 1, 0, 0, 10, solidAt([3]int32{0, 0, 0}))
	if !ok {
		t.Fatal("Raycast found nothing when starting inside a solid voxel")
	}
	if hit.Distance != 0 {
		t.Errorf("Distance = %g, want 0 when the origin voxel already stops the ray", hit.Distance)
	}
}

// Grid traversal, not fixed-step sampling: a diagonal ray must visit every
// voxel it truly crosses rather than tunnelling through a corner.
func TestRaycastDiagonalDoesNotTunnel(t *testing.T) {
	// A wall one voxel thick, crossed at 45°. Fixed-step sampling can step
	// straight past this; boundary traversal cannot.
	wall := solidAt(
		[3]int32{3, 3, 0}, [3]int32{3, 2, 0}, [3]int32{2, 3, 0},
	)
	if _, ok := Raycast(0.5, 0.5, 0.5, 1, 1, 0, 20, wall); !ok {
		t.Error("a 45° ray passed through a one-voxel wall — the traversal is tunnelling")
	}
}

func TestRaycastNothingInRange(t *testing.T) {
	if _, ok := Raycast(0.5, 0.5, 0.5, 1, 0, 0, 10, func(int32, int32, int32) bool { return false }); ok {
		t.Error("Raycast reported a hit with nothing solid anywhere")
	}
}

// The predicate is the only thing that decides what stops a ray, so this
// package needs no notion of blocks at all.
func TestRaycastPredicateDecidesEverything(t *testing.T) {
	calls := 0
	_, _ = Raycast(0.5, 0.5, 0.5, 1, 0, 0, 5, func(int32, int32, int32) bool {
		calls++
		return false
	})
	if calls < 5 {
		t.Errorf("predicate called %d times over 5 blocks, want at least one per voxel", calls)
	}
}
