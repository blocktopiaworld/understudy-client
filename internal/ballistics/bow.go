// Package ballistics solves bow trajectories.
//
// Pure arithmetic over vanilla's arrow physics: no connection, no world, no
// blocks. That makes it exhaustively testable on its own, which matters
// because the constants and their *order* are the whole correctness argument
// and a subtly wrong arc simply misses at range rather than erroring.
package ballistics

import (
	"math"
	"time"
)

// Bow draw timing. A bow reaches full power after 20 ticks of holding; below
// about 3 ticks the shot is not released at all.
const (
	// TickRate is the server tick. Duplicated rather than imported so this
	// package depends on nothing.
	TickRate = 50 * time.Millisecond

	// FullDraw is how long the bow must be held for maximum power.
	FullDraw = 20 * TickRate

	// MinDrawTicks is the shortest draw that fires at all.
	MinDrawTicks = 3

	// MinDraw is MinDrawTicks as a duration.
	MinDraw = MinDrawTicks * TickRate
)

// Arrow flight constants, matching vanilla's AbstractArrow.tick.
//
// Order matters as much as the values: the arrow moves, then drag scales the
// whole velocity, then gravity is subtracted from Y. Applying gravity before
// drag produces a subtly flatter arc that misses at range.
const (
	speedPerPower = 3.0  // blocks/tick at full draw
	drag          = 0.99 // per tick, applied to every component
	gravity       = 0.05 // blocks/tick², after drag

	// maxFlightTicks bounds the simulation. Well beyond any shot that could
	// still be going anywhere useful.
	maxFlightTicks = 600

	// abandonBelow stops simulating an arrow that is falling and already far
	// past the target's level; it will not climb back.
	abandonBelow = -256

	// solveStepDeg is the sweep resolution, well inside the accuracy a target
	// block needs at practical range.
	solveStepDeg = 0.25

	// solveTolerance is how close the arc must come, in blocks, to count.
	solveTolerance = 1.5
)

// Power converts a draw duration into vanilla's launch power, 0..1.
//
// The curve is not linear: (t² + 2t)/3 for t = draw/full. Half a second of
// draw gives roughly 0.4 power, not 0.5, so a caller tuning for range needs
// the real curve rather than the intuition.
func Power(draw time.Duration) float64 {
	if draw <= 0 {
		return 0
	}
	t := draw.Seconds() / FullDraw.Seconds()
	return math.Min((t*t+2*t)/3, 1)
}

// Simulate flies an arrow launched at a pitch and reports the height it has
// reached once it has covered horizontal distance d.
//
// Simulation rather than a closed-form solution because drag makes the exact
// trajectory awkward to invert, and a tick-accurate loop is both shorter and
// harder to get subtly wrong.
func Simulate(pitchRad, power, d float64) (height float64, reached bool) {
	speed := power * speedPerPower
	vx := math.Cos(pitchRad) * speed
	vy := math.Sin(pitchRad) * speed

	x, y := 0.0, 0.0
	for range maxFlightTicks {
		x += vx
		y += vy
		vx *= drag
		vy = vy*drag - gravity

		if x >= d {
			return y, true
		}
		if vy < 0 && y < abandonBelow {
			break
		}
	}
	return y, false
}

// SolvePitch finds the launch pitch that drops an arrow onto a target at a
// given horizontal and vertical offset.
//
// It searches the flat-trajectory solution (the shallower of the two arcs),
// which is what a player would use: faster to reach the target and far less
// sensitive to small aiming errors than the lobbed one.
//
// The returned pitch is in the *protocol's* convention, which is inverted
// relative to the maths one: negative looks up.
func SolvePitch(horizontal, vertical, power float64) (pitchDeg float64, ok bool) {
	if horizontal <= 0.01 {
		// Straight up or down; no ballistic solution needed.
		if vertical >= 0 {
			return -90, true
		}
		return 90, true
	}

	best, bestErr := 0.0, math.MaxFloat64
	for deg := -89.0; deg <= 89.0; deg += solveStepDeg {
		h, reached := Simulate(deg*math.Pi/180, power, horizontal)
		if !reached {
			continue
		}
		if e := math.Abs(h - vertical); e < bestErr {
			best, bestErr = deg, e
		}
	}
	if bestErr > solveTolerance {
		return 0, false // no arc gets close enough; out of range
	}
	return -best, true
}
