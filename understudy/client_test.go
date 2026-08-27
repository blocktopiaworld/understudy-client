package understudy

import (
	"testing"

	"github.com/blocktopia/understudy-client/protocol"
)

// The teleport packet's flags field is the last one, and reading it is what
// pins the layout: if the fields before it were wrong the reader would not land
// on the end.
//
// Worth pinning because the relative path cannot be tested against a real
// server — vanilla resolves /tp to absolute coordinates before sending, so the
// flags are always zero on the wire. Whole-packet consumption is the only
// evidence available that the field is where this thinks it is.
func TestTeleportPacketConsumesExactly(t *testing.T) {
	v := testVersion(t)
	w := protocol.NewWriter(v.Packets.CBPlayPosition).
		VarInt(7).            // teleport id
		F64(1).F64(2).F64(3). // position
		F64(0).F64(0).F64(0). // velocity
		F32(90).F32(45).      // yaw, pitch
		I32(0)                // flags
	// Strip the packet id the writer puts in front, the way the client's
	// dispatch does before handing a payload to a handler.
	r := protocol.NewReader(protocol.Packet{Data: w.Bytes()}.Reader().Remaining())
	r.VarInt()
	r.VarInt()
	for range 6 {
		r.F64()
	}
	r.F32()
	r.F32()
	r.I32()
	if err := r.Err(); err != nil {
		t.Fatalf("reading through the flags field failed: %v", err)
	}
	if left := len(r.Remaining()); left != 0 {
		t.Errorf("%d byte(s) left after the flags — the field order is wrong, "+
			"and a relative teleport would resolve against garbage", left)
	}
}

// The resolution itself: a delta must be added to where the bot already is,
// and an absolute must not be.
func TestRelativeTeleportResolvesAgainstThePresentPosition(t *testing.T) {
	at := Position{X: 100, Y: 64, Z: -50}
	for _, tc := range []struct {
		name       string
		flags      int32
		x, y, z    float64
		wx, wy, wz float64
	}{
		{"nothing relative", 0, 5, 6, 7, 5, 6, 7},
		{"x only", teleportRelativeX, 5, 6, 7, 105, 6, 7},
		{"all three", teleportRelativeX | teleportRelativeY | teleportRelativeZ,
			5, 6, 7, 105, 70, -43},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, y, z := relativeTo(tc.flags, tc.x, tc.y, tc.z, at)
			if x != tc.wx || y != tc.wy || z != tc.wz {
				t.Errorf("got %v,%v,%v want %v,%v,%v", x, y, z, tc.wx, tc.wy, tc.wz)
			}
		})
	}
}
