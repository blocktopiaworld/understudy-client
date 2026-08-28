package understudy

import (
	"testing"

	"github.com/block-topia/understudy-client/protocol"
)

// Reach is measured from the eyes to the nearest point of the block's box —
// not to its centre — so a bot can legitimately work a little further than
// centre-distance suggests.
func TestBlockDistanceMeasuresToTheNearestFace(t *testing.T) {
	c := newTestClient(t)
	setPosition(c, 0.5, 0, 0.5) // eyes at y = EyeHeight

	if got := c.BlockDistance(0, 0, 0); !closeEnough(got, EyeHeight-1, 1e-9) {
		t.Errorf("BlockDistance to the block underfoot = %g, want %g", got, EyeHeight-1)
	}
	// A block four east: its nearest face is at x=4, the eyes at x=0.5.
	if got := c.BlockDistance(4, 1, 0); got >= 4 {
		t.Errorf("BlockDistance = %g; measuring to the centre would give ~4.1, "+
			"but the nearest face is 3.5 away", got)
	}
}

func TestCanReachBlock(t *testing.T) {
	c := newTestClient(t)
	setPosition(c, 0.5, 0, 0.5)

	for _, tc := range []struct {
		name    string
		x, y, z int32
		want    bool
	}{
		{"underfoot", 0, 0, 0, true},
		{"just inside reach", 4, 1, 0, true},
		{"well beyond reach", 20, 1, 0, false},
		{"straight up beyond reach", 0, 10, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.CanReachBlock(tc.x, tc.y, tc.z); got != tc.want {
				t.Errorf("CanReachBlock(%d,%d,%d) = %v (distance %.2f), want %v",
					tc.x, tc.y, tc.z, got, c.BlockDistance(tc.x, tc.y, tc.z), tc.want)
			}
		})
	}
}

// Out-of-range digs and placements get no reply at all, so an unchecked action
// looks exactly like one working normally.
func TestRequireReachExplainsTheDistance(t *testing.T) {
	c := newTestClient(t)
	setPosition(c, 0.5, 0, 0.5)

	if err := c.requireReach("break", 1, 0, 0); err != nil {
		t.Errorf("requireReach on an adjacent block = %v, want nil", err)
	}
	wantErrContaining(t, c.requireReach("break", 50, 0, 0),
		"requireReach on a distant block", "break", "50", "reach")
}

// The server ignores actions from a dead player without complaint, so without
// this check a caller carries on issuing commands into the void and fails much
// later, somewhere unrelated.
func TestRequireAlive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(*Client)
		wantErr string
	}{
		{
			name:    "before entering play",
			setup:   func(c *Client) { c.setState(protocol.StateConfiguration) },
			wantErr: "before entering play state",
		},
		{
			name: "while dead",
			setup: func(c *Client) {
				c.mu.Lock()
				c.dead = true
				c.mu.Unlock()
			},
			wantErr: "while dead",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			tc.setup(c)
			wantErrContaining(t, c.requireAlive("dig"), "requireAlive", tc.wantErr, "dig")
		})
	}
}

func TestRequireAlivePassesWhenPlayingAndAlive(t *testing.T) {
	c := newTestClient(t)
	if err := c.requireAlive("dig"); err != nil {
		t.Errorf("requireAlive on a live client = %v, want nil", err)
	}
}

func TestRequireEntityReach(t *testing.T) {
	c := newTestClient(t)
	setPosition(c, 0, 0, 0)

	near := Entity{ID: 1, TypeName: "minecraft:zombie", X: 1}
	far := Entity{ID: 2, TypeName: "minecraft:zombie", X: AttackReach + 2}

	if err := c.requireEntityReach("attack", near); err != nil {
		t.Errorf("requireEntityReach on a near entity = %v, want nil", err)
	}
	wantErrContaining(t, c.requireEntityReach("attack", far),
		"requireEntityReach on a far entity", "minecraft:zombie", "beyond")
}

func TestBlockOffsetByFace(t *testing.T) {
	for _, tc := range []struct {
		face int32
		want [3]int32
	}{
		{protocol.FaceBottom, [3]int32{5, 4, 5}},
		{protocol.FaceTop, [3]int32{5, 6, 5}},
		{protocol.FaceNorth, [3]int32{5, 5, 4}},
		{protocol.FaceSouth, [3]int32{5, 5, 6}},
		{protocol.FaceWest, [3]int32{4, 5, 5}},
		{protocol.FaceEast, [3]int32{6, 5, 5}},
	} {
		if got := BlockOffsetByFace(5, 5, 5, tc.face); got != tc.want {
			t.Errorf("BlockOffsetByFace(5,5,5, face %d) = %v, want %v", tc.face, got, tc.want)
		}
	}
}
