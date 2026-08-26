package geom

import "math"

// Face numbering, as the protocol uses it for dig and place.
//
// Duplicated from the protocol package rather than imported, so this package
// stays free of wire concerns — the values are Mojang-stable and the traversal
// has to name a face somehow.
const (
	FaceBottom int32 = 0 // -Y
	FaceTop    int32 = 1 // +Y
	FaceNorth  int32 = 2 // -Z
	FaceSouth  int32 = 3 // +Z
	FaceWest   int32 = 4 // -X
	FaceEast   int32 = 5 // +X
)

// Hit is a voxel a ray entered, and the face it entered through.
type Hit struct {
	X, Y, Z  int32
	Face     int32
	Distance float64
}

// Raycast walks the voxel grid from an origin along a direction, calling stop
// for each voxel it enters, and returns the first one stop accepts.
//
// This is how the game itself decides what you are pointing at, and modelling
// it matters more than it looks: a straight-line distance check says a block
// four metres away is reachable, but if anything is in between then the
// crosshair is on *that* block, and what a player would actually mine is not
// the one the caller named.
//
// Grid traversal, not fixed-size sampling: stepping by a fraction of a block
// can tunnel through a corner and miss a face, whereas advancing to the next
// boundary visits every voxel the ray truly crosses.
//
// The caller supplies stop, so this package needs to know nothing about blocks,
// terrain or what "solid" means.
func Raycast(ox, oy, oz, dx, dy, dz, maxDist float64, stop func(x, y, z int32) bool) (Hit, bool) {
	x, y, z := BlockPos(ox, oy, oz)
	stepX, stepY, stepZ := axisStep(dx), axisStep(dy), axisStep(dz)

	// Distance along the ray to the next boundary on each axis, and the
	// distance between successive boundaries.
	tMaxX := safeDiv(boundary(ox, x, stepX), dx)
	tMaxY := safeDiv(boundary(oy, y, stepY), dy)
	tMaxZ := safeDiv(boundary(oz, z, stepZ), dz)
	tDeltaX, tDeltaY, tDeltaZ := safeDiv(1, dx), safeDiv(1, dy), safeDiv(1, dz)

	dist := 0.0
	face := FaceTop
	for dist <= maxDist {
		if stop(x, y, z) {
			return Hit{X: x, Y: y, Z: z, Face: face, Distance: dist}, true
		}
		// Advance along whichever axis reaches its next boundary first, and
		// remember which face we entered the new voxel through.
		switch {
		case tMaxX <= tMaxY && tMaxX <= tMaxZ:
			dist, x, tMaxX = tMaxX, x+stepX, tMaxX+tDeltaX
			face = faceForStep(stepX, FaceEast, FaceWest)
		case tMaxY <= tMaxZ:
			dist, y, tMaxY = tMaxY, y+stepY, tMaxY+tDeltaY
			face = faceForStep(stepY, FaceTop, FaceBottom)
		default:
			dist, z, tMaxZ = tMaxZ, z+stepZ, tMaxZ+tDeltaZ
			face = faceForStep(stepZ, FaceSouth, FaceNorth)
		}
	}
	return Hit{}, false
}

// axisStep is the direction of travel along one axis. A zero component steps
// negative, which is harmless: safeDiv gives that axis an infinite boundary
// distance, so it is never the axis chosen to advance.
func axisStep(d float64) int32 {
	if d > 0 {
		return 1
	}
	return -1
}

// boundary is the distance from origin to the next voxel edge along an axis.
func boundary(origin float64, voxel, step int32) float64 {
	if step > 0 {
		return float64(voxel+1) - origin
	}
	return origin - float64(voxel)
}

// safeDiv divides by an axis component's magnitude, treating a zero component
// as infinitely far — a ray parallel to an axis never crosses its boundaries.
func safeDiv(n, d float64) float64 {
	if d == 0 {
		return math.Inf(1)
	}
	return n / math.Abs(d)
}

// faceForStep picks the face a ray entered through: moving in +X enters the
// west face, so the sides are swapped relative to the direction of travel.
func faceForStep(step, positiveSide, negativeSide int32) int32 {
	if step > 0 {
		return negativeSide
	}
	return positiveSide
}
