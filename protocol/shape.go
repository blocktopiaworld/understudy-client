package protocol

// ShapeUnit is how many parts of a block a Box coordinate counts in.
//
// Vanilla geometry is built from sixteenths, with a handful of thirty-seconds
// (the amethyst buds), so 1/32 represents every shape exactly. Storing integer
// units rather than floats keeps the tables four times smaller and, more
// usefully, makes comparisons exact: a slab is 16 units tall, never 0.49999.
const ShapeUnit = 32

// Box is one axis-aligned collision box in units of 1/ShapeUnit of a block,
// relative to the block's own corner.
//
// Coordinates can fall outside 0..ShapeUnit: fences and walls stand 1.5 blocks
// tall so their collision reaches 48, and a few shapes start slightly negative.
// Anything treating a box as clamped to its own block will get those wrong.
type Box [6]int8

// The accessors below name the fields, because [6]int8 indices are exactly the
// kind of thing that gets transposed silently.

// MinX returns the box's lower X bound, in units of 1/ShapeUnit.
func (b Box) MinX() int8 { return b[0] }

// MinY returns the box's lower Y bound, in units of 1/ShapeUnit.
func (b Box) MinY() int8 { return b[1] }

// MinZ returns the box's lower Z bound, in units of 1/ShapeUnit.
func (b Box) MinZ() int8 { return b[2] }

// MaxX returns the box's upper X bound, in units of 1/ShapeUnit.
func (b Box) MaxX() int8 { return b[3] }

// MaxY returns the box's upper Y bound, in units of 1/ShapeUnit.
func (b Box) MaxY() int8 { return b[4] }

// MaxZ returns the box's upper Z bound, in units of 1/ShapeUnit.
func (b Box) MaxZ() int8 { return b[5] }

// Empty reports whether the box encloses nothing.
func (b Box) Empty() bool {
	return b.MaxX() <= b.MinX() || b.MaxY() <= b.MinY() || b.MaxZ() <= b.MinZ()
}

// Height returns the box's vertical extent in blocks.
func (b Box) Height() float64 { return float64(b.MaxY()-b.MinY()) / ShapeUnit }

// FullCube is the shape of an ordinary block. Named so comparisons against it
// read as an assertion rather than a magic literal.
var FullCube = Box{0, 0, 0, ShapeUnit, ShapeUnit, ShapeUnit}

// CollisionShape returns the boxes a block state collides with.
//
// An empty result means the state has no collision at all — air, but also
// grass, torches and carpet-thin decoration. Callers must not read "no boxes"
// as "unknown": an unknown state also returns none, which is why
// HasCollisionData exists to tell the two apart.
func (v *Version) CollisionShape(state int32) []Box {
	idx, ok := v.shapeIndexOf(state)
	if !ok {
		return nil
	}
	return v.shapes[idx]
}

// HasCollisionData reports whether the version's tables describe this state.
//
// A state outside the table is not "empty", it is unmapped — a version
// mismatch, or a modded block. Movement code has to treat that as an obstacle
// rather than as clear air, because walking confidently into an unknown block
// is a stall with no error attached.
func (v *Version) HasCollisionData(state int32) bool {
	_, ok := v.shapeIndexOf(state)
	return ok
}

// shapeIndexOf binary-searches the run table.
func (v *Version) shapeIndexOf(state int32) (int, bool) {
	lo, hi := 0, len(v.shapeRuns)-1
	for lo <= hi {
		mid := int(uint(lo+hi) >> 1)
		run := v.shapeRuns[mid]
		switch {
		case state < run[0]:
			hi = mid - 1
		case state > run[1]:
			lo = mid + 1
		default:
			idx := int(run[2])
			if idx < 0 || idx >= len(v.shapes) {
				return 0, false
			}
			return idx, true
		}
	}
	return 0, false
}

// IsFullCube reports whether a state fills its block exactly.
//
// This is the case the old boolean got right, and it is worth keeping cheap:
// most movement questions have a fast answer when the block is a plain cube.
func (v *Version) IsFullCube(state int32) bool {
	shape := v.CollisionShape(state)
	return len(shape) == 1 && shape[0] == FullCube
}

// CollisionHeight returns how far up a block a player would be lifted by
// standing on it, in blocks, measured from the block's own floor.
//
// This is what a boolean cannot express and what movement actually needs: a
// slab is 0.5, a full block 1.0, a fence 1.5 — so a fence is not a step, it is
// a wall, and treating it as solid-and-therefore-steppable walks a bot into it
// and stalls with no error.
//
// Boxes that do not start at the block floor are ignored: standing on the top
// half of a vertical slab is not something walking gets you to.
func (v *Version) CollisionHeight(state int32) float64 {
	var top int8
	for _, box := range v.CollisionShape(state) {
		if box.Empty() || box.MinY() > 0 {
			continue
		}
		if box.MaxY() > top {
			top = box.MaxY()
		}
	}
	return float64(top) / ShapeUnit
}

// BlocksMovement reports whether a state has any collision at all.
//
// Deliberately distinct from IsSolid, which answers from the coarse
// boundingBox ranges. Where they disagree the shape is right — IsSolid calls a
// fence solid and a slab solid without distinguishing them, and calls a
// pressure plate solid when it has no collision worth the name.
func (v *Version) BlocksMovement(state int32) bool {
	for _, box := range v.CollisionShape(state) {
		if !box.Empty() {
			return true
		}
	}
	return false
}
