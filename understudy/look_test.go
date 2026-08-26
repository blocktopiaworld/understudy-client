package understudy

import (
	"slices"
	"testing"
)

func TestDirectionNamesAreSorted(t *testing.T) {
	names := DirectionNames()
	if len(names) == 0 {
		t.Fatal("DirectionNames() is empty")
	}
	if !slices.IsSorted(names) {
		t.Errorf("DirectionNames() is not sorted: %v", names)
	}
	for _, name := range names {
		if _, ok := LookupDirection(name); !ok {
			t.Errorf("DirectionNames() lists %q but LookupDirection does not resolve it", name)
		}
	}
}

// Yaw 0 faces SOUTH and increases clockwise, so west is +90, north is 180 and
// east is -90. This is the table that would otherwise be guessed backwards.
func TestDirectionYawConvention(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaw  float32
	}{
		{"south", 0}, {"west", 90}, {"north", 180}, {"east", -90},
		{"southwest", 45}, {"northwest", 135}, {"northeast", -135}, {"southeast", -45},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, ok := LookupDirection(tc.name)
			if !ok {
				t.Fatalf("LookupDirection(%q) not found", tc.name)
			}
			yaw, sets := dir.Yaw()
			if !sets {
				t.Fatalf("direction %q names no yaw", tc.name)
			}
			if yaw != tc.yaw {
				t.Errorf("%q yaw = %g, want %g", tc.name, yaw, tc.yaw)
			}
		})
	}
}

// up and down tilt without throwing away the current heading, which is what
// makes them usable mid-task.
func TestVerticalDirectionsLeaveYawAlone(t *testing.T) {
	for _, name := range []string{"up", "down"} {
		dir, ok := LookupDirection(name)
		if !ok {
			t.Fatalf("LookupDirection(%q) not found", name)
		}
		if yaw, sets := dir.Yaw(); sets {
			t.Errorf("%q sets yaw to %g, want it left alone", name, yaw)
		}
		if _, sets := dir.Pitch(); !sets {
			t.Fatalf("%q names no pitch", name)
		}
		if gotYaw, _ := dir.Apply(123, 0); gotYaw != 123 {
			t.Errorf("%q.Apply changed the yaw to %g, want the original 123", name, gotYaw)
		}
	}
	if up, _ := LookupDirection("up"); func() float32 { p, _ := up.Pitch(); return p }() != -90 {
		t.Error("up pitch is not -90 (negative looks up)")
	}
	if down, _ := LookupDirection("down"); func() float32 { p, _ := down.Pitch(); return p }() != 90 {
		t.Error("down pitch is not 90")
	}
}

func TestDirectionApplyReplacesBothAxes(t *testing.T) {
	north, _ := LookupDirection("north")
	yaw, pitch := north.Apply(45, -30)
	if yaw != 180 || pitch != 0 {
		t.Errorf("north.Apply(45,-30) = %g,%g; want 180,0 — a cardinal direction levels off", yaw, pitch)
	}
}

func TestLookupDirectionNormalisesInput(t *testing.T) {
	for _, in := range []string{"NORTH", " north ", "North", "\tnorth\n"} {
		if _, ok := LookupDirection(in); !ok {
			t.Errorf("LookupDirection(%q) not found; input should be trimmed and lowercased", in)
		}
	}
	if _, ok := LookupDirection("sideways"); ok {
		t.Error("LookupDirection(sideways) reported found, want not found")
	}
}

func TestLookDirectionRejectsUnknownNames(t *testing.T) {
	c := newTestClient(t)
	// The error has to list the accepted names, or the caller has no next step.
	wantErrContaining(t, c.LookDirection("nowhere"), "LookDirection(nowhere)", "north", "nowhere")
}

// The table is unexported behind a lookup because a package-level map is
// mutable by every caller: one caller could redefine "north" process-wide.
// Returning a value rather than pointers is what makes that safe.
func TestDirectionsTableIsNotExposedForMutation(t *testing.T) {
	first, ok := LookupDirection("north")
	if !ok {
		t.Fatal("north missing")
	}
	first.yaw = 42 // a caller can only ever mutate its own copy

	second, _ := LookupDirection("north")
	if yaw, _ := second.Yaw(); yaw != 180 {
		t.Errorf("north yaw is now %g after a caller mutated its copy, want 180", yaw)
	}
}
