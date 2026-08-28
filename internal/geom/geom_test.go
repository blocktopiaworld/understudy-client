package geom

import (
	"math"
	"testing"
)

func closeEnough(a, b, tol float64) bool { return math.Abs(a-b) < tol }

// sameHeading compares two yaws as directions, so -180 and 180 — which point
// the same way — are equal.
func sameHeading(a, b float64) bool {
	diff := math.Mod(math.Abs(a-b), 360)
	return diff < 1e-4 || math.Abs(diff-360) < 1e-4
}

// Truncation towards zero would put x = -0.5 in block 0 rather than block -1,
// so every query west or north of the origin would be off by one.
func TestBlockPosFloors(t *testing.T) {
	tests := []struct {
		x, y, z             float64
		wantX, wantY, wantZ int32
	}{
		{0, 0, 0, 0, 0, 0},
		{0.9, 0.9, 0.9, 0, 0, 0},
		{1.0, 1.0, 1.0, 1, 1, 1},
		{-0.5, -0.5, -0.5, -1, -1, -1},
		{-0.001, -0.001, -0.001, -1, -1, -1},
		{-1.0, -1.0, -1.0, -1, -1, -1},
		{-1.5, 64.2, -310.7, -2, 64, -311},
	}
	for _, tc := range tests {
		x, y, z := BlockPos(tc.x, tc.y, tc.z)
		if x != tc.wantX || y != tc.wantY || z != tc.wantZ {
			t.Errorf("BlockPos(%g,%g,%g) = (%d,%d,%d), want (%d,%d,%d)",
				tc.x, tc.y, tc.z, x, y, z, tc.wantX, tc.wantY, tc.wantZ)
		}
	}
}

func TestBlockCentre(t *testing.T) {
	x, y, z := BlockCentre(10, 64, -5)
	if x != 10.5 || y != 64.5 || z != -4.5 {
		t.Errorf("BlockCentre(10,64,-5) = (%g,%g,%g), want (10.5,64.5,-4.5)", x, y, z)
	}
}

func TestLength(t *testing.T) {
	for _, tc := range []struct{ dx, dy, dz, want float64 }{
		{0, 0, 0, 0}, {3, 4, 0, 5}, {1, 2, 2, 3}, {-3, -4, 0, 5},
	} {
		if got := Length(tc.dx, tc.dy, tc.dz); !closeEnough(got, tc.want, 1e-9) {
			t.Errorf("Length(%g,%g,%g) = %g, want %g", tc.dx, tc.dy, tc.dz, got, tc.want)
		}
	}
}

func TestClamp(t *testing.T) {
	for _, tc := range []struct{ v, lo, hi, want float64 }{
		{5, 0, 10, 5}, {-1, 0, 10, 0}, {11, 0, 10, 10}, {0, 0, 0, 0},
	} {
		if got := Clamp(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Errorf("Clamp(%g,%g,%g) = %g, want %g", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}

// Reach is measured eye-to-nearest-face, not eye-to-centre, so a bot can work
// a little further away than centre-distance suggests.
func TestBlockDistanceMeasuresToTheNearestFace(t *testing.T) {
	// Eyes directly above the block at 0,0,0: the nearest point of its box is
	// its top face, straight down.
	if got := BlockDistance(0.5, 3, 0.5, 0, 0, 0); !closeEnough(got, 2, 1e-9) {
		t.Errorf("BlockDistance to the block below = %g, want 2 (to the face, not the centre)", got)
	}
	// Inside the block, the distance is zero.
	if got := BlockDistance(0.5, 0.5, 0.5, 0, 0, 0); got != 0 {
		t.Errorf("BlockDistance from inside the block = %g, want 0", got)
	}
	// Four blocks east: the nearest face is at x=4.
	if got := BlockDistance(0.5, 0.5, 0.5, 4, 0, 0); !closeEnough(got, 3.5, 1e-9) {
		t.Errorf("BlockDistance = %g, want 3.5 (centre-distance would be ~4)", got)
	}
}

// Yaw 0 faces SOUTH (+Z) and increases clockwise, so west is +90, north is 180
// and east is -90. Guessing that 0 means north puts every aim backwards.
func TestYawPitchTowardsCardinalDirections(t *testing.T) {
	tests := []struct {
		name      string
		x, y, z   float64
		wantYaw   float64
		wantPitch float64
		vertical  bool
	}{
		{name: "south is +Z, yaw 0", z: 10},
		{name: "west is -X, yaw +90", x: -10, wantYaw: 90},
		{name: "north is -Z, yaw 180", z: -10, wantYaw: 180},
		{name: "east is +X, yaw -90", x: 10, wantYaw: -90},
		{name: "straight up is pitch -90", y: 10, wantPitch: -90, vertical: true},
		{name: "straight down is pitch +90", y: -10, wantPitch: 90, vertical: true},
		{name: "45 degrees down", y: -10, z: 10, wantPitch: 45},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yaw, pitch := YawPitchTowards(0, 0, 0, tc.x, tc.y, tc.z)
			if !tc.vertical && !sameHeading(float64(yaw), tc.wantYaw) {
				t.Errorf("yaw = %g, want %g", yaw, tc.wantYaw)
			}
			if !closeEnough(float64(pitch), tc.wantPitch, 1e-4) {
				t.Errorf("pitch = %g, want %g", pitch, tc.wantPitch)
			}
		})
	}
}

// The rotation towards a point and the direction vector for that rotation must
// be inverses, or aiming and ray-tracing disagree about where the crosshair
// is — which is the failure that makes a mining loop stall one block short.
func TestYawPitchAndLookDirectionAreInverses(t *testing.T) {
	for _, target := range [][3]float64{
		{5, 0, 5}, {-5, 3, 2}, {0, -4, 9}, {12, 7, -3}, {-1, -1, -1},
	} {
		yaw, pitch := YawPitchTowards(0, 0, 0, target[0], target[1], target[2])
		dx, dy, dz := LookDirection(yaw, pitch)

		norm := Length(target[0], target[1], target[2])
		wantX, wantY, wantZ := target[0]/norm, target[1]/norm, target[2]/norm
		if !closeEnough(dx, wantX, 1e-5) || !closeEnough(dy, wantY, 1e-5) || !closeEnough(dz, wantZ, 1e-5) {
			t.Errorf("aiming at %v gave direction (%g,%g,%g), want (%g,%g,%g)",
				target, dx, dy, dz, wantX, wantY, wantZ)
		}
	}
}

func TestLookDirectionIsUnitLength(t *testing.T) {
	for yaw := float32(-180); yaw <= 180; yaw += 37 {
		for pitch := float32(-90); pitch <= 90; pitch += 31 {
			dx, dy, dz := LookDirection(yaw, pitch)
			if got := Length(dx, dy, dz); !closeEnough(got, 1, 1e-9) {
				t.Errorf("LookDirection(%g,%g) has length %g, want 1", yaw, pitch, got)
			}
		}
	}
}
