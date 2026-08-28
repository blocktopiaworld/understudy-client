package ballistics

import (
	"math"
	"testing"
	"time"
)

func closeEnough(a, b, tol float64) bool { return math.Abs(a-b) < tol }

// The power curve is (t²+2t)/3 for t = draw/full, which is emphatically not
// linear: half a second of draw gives roughly 40% power, not 50%. A caller
// tuning for range needs the real curve rather than the intuition.
func TestPower(t *testing.T) {
	tests := []struct {
		name string
		draw time.Duration
		want float64
	}{
		{"no draw", 0, 0},
		{"negative", -time.Second, 0},
		{"full draw", FullDraw, 1},
		{"beyond full draw is clamped", 3 * FullDraw, 1},
		{"half a second is ~0.4, not 0.5", 500 * time.Millisecond, (0.25 + 1.0) / 3},
		{"quarter draw", FullDraw / 4, (0.0625 + 0.5) / 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Power(tc.draw); !closeEnough(got, tc.want, 1e-9) {
				t.Errorf("Power(%v) = %g, want %g", tc.draw, got, tc.want)
			}
		})
	}
}

func TestPowerIsMonotonicAndBounded(t *testing.T) {
	prev := -1.0
	for ms := 0; ms <= 1500; ms += 25 {
		got := Power(time.Duration(ms) * time.Millisecond)
		if got < prev {
			t.Fatalf("Power fell from %g to %g at %dms", prev, got, ms)
		}
		if got < 0 || got > 1 {
			t.Fatalf("Power(%dms) = %g, outside 0..1", ms, got)
		}
		prev = got
	}
}

// Order matters as much as the values: move, then drag scales the whole
// velocity, then gravity is subtracted from Y. Applying gravity before drag
// produces a subtly flatter arc that misses at range.
func TestSimulateDrops(t *testing.T) {
	height, reached := Simulate(0, 1, 20)
	if !reached {
		t.Fatal("a full-power level shot did not reach 20 blocks")
	}
	if height >= 0 {
		t.Errorf("height after 20 blocks = %g, want a drop below the launch height", height)
	}
}

func TestSimulateRisesWhenAimedUp(t *testing.T) {
	up, _ := Simulate(30*math.Pi/180, 1, 10)
	level, _ := Simulate(0, 1, 10)
	if up <= level {
		t.Errorf("aiming 30° up gave height %g, not more than level's %g", up, level)
	}
}

// Drag means a shot does not travel indefinitely; a weak one falls short.
func TestSimulateOutOfRange(t *testing.T) {
	if _, reached := Simulate(0, 0.05, 500); reached {
		t.Error("a 5% power shot reached 500 blocks, want it to fall short")
	}
}

func TestSimulateDragIsAppliedBeforeGravity(t *testing.T) {
	// One tick at full power, level: x advances by the launch speed exactly,
	// and y is still 0 because gravity applies to the *next* tick's velocity.
	h, reached := Simulate(0, 1, speedPerPower-0.0001)
	if !reached {
		t.Fatal("did not reach the first tick's distance")
	}
	if !closeEnough(h, 0, 1e-9) {
		t.Errorf("height after one tick = %g, want 0 — gravity must apply after the move", h)
	}
}

func TestSolvePitch(t *testing.T) {
	tests := []struct {
		name                 string
		horizontal, vertical float64
		power                float64
		wantOK               bool
	}{
		{"level short shot", 10, 0, 1, true},
		{"level medium shot", 25, 0, 1, true},
		{"uphill", 15, 5, 1, true},
		{"downhill", 15, -5, 1, true},
		{"straight up", 0, 10, 1, true},
		{"straight down", 0, -10, 1, true},
		{"far beyond range at low power", 400, 0, 0.1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pitch, ok := SolvePitch(tc.horizontal, tc.vertical, tc.power)
			if ok != tc.wantOK {
				t.Fatalf("SolvePitch(%g,%g,%g) ok = %v, want %v",
					tc.horizontal, tc.vertical, tc.power, ok, tc.wantOK)
			}
			if ok && (pitch < -90 || pitch > 90) {
				t.Errorf("pitch = %g, outside the valid -90..90 range", pitch)
			}
		})
	}
}

// Protocol pitch is inverted relative to the maths convention: negative looks
// up. A sign error here aims every shot into the ground.
func TestSolvePitchSignConvention(t *testing.T) {
	up, ok := SolvePitch(20, 8, 1)
	if !ok {
		t.Fatal("no solution for an uphill shot")
	}
	down, ok := SolvePitch(20, -8, 1)
	if !ok {
		t.Fatal("no solution for a downhill shot")
	}
	if up >= down {
		t.Errorf("uphill pitch %g is not more negative than downhill %g; the sign is inverted", up, down)
	}
	if straightUp, _ := SolvePitch(0, 10, 1); straightUp != -90 {
		t.Errorf("straight up = %g, want -90", straightUp)
	}
	if straightDown, _ := SolvePitch(0, -10, 1); straightDown != 90 {
		t.Errorf("straight down = %g, want 90", straightDown)
	}
}

// The solver picks the flat trajectory — the shallower of the two arcs — which
// is what a player would use: faster to the target and far less sensitive to
// small aiming errors.
func TestSolvePitchPicksTheFlatArc(t *testing.T) {
	pitch, ok := SolvePitch(20, 0, 1)
	if !ok {
		t.Fatal("no solution for a 20-block level shot")
	}
	if pitch < -25 {
		t.Errorf("pitch = %g, want the shallow arc (nearer level than -25°)", pitch)
	}
}

// A solved pitch must actually land the arrow where it was aimed — the round
// trip through Simulate is the real check.
func TestSolvedPitchLandsOnTarget(t *testing.T) {
	for _, tc := range []struct{ horizontal, vertical float64 }{
		{10, 0}, {20, 0}, {15, 4}, {15, -4}, {30, -2},
	} {
		pitch, ok := SolvePitch(tc.horizontal, tc.vertical, 1)
		if !ok {
			t.Errorf("no solution for %g away, %g up", tc.horizontal, tc.vertical)
			continue
		}
		// Undo the protocol's inverted pitch before simulating.
		h, reached := Simulate(-pitch*math.Pi/180, 1, tc.horizontal)
		if !reached {
			t.Errorf("solved arc for %g away never got there", tc.horizontal)
			continue
		}
		if !closeEnough(h, tc.vertical, solveTolerance) {
			t.Errorf("aimed at %g up over %g, arc arrived at %g", tc.vertical, tc.horizontal, h)
		}
	}
}
