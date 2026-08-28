package understudy

import (
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/blocktopiaworld/understudy-client/internal/entities"
	"github.com/blocktopiaworld/understudy-client/internal/inventory"
	"github.com/blocktopiaworld/understudy-client/internal/world"
	"github.com/blocktopiaworld/understudy-client/protocol"
)

// Block states used throughout these tests. Small and explicit so a test can
// say "put stone here" without depending on a real version's tables.
const (
	stateAir   int32 = 0
	stateStone int32 = 1
	stateWater int32 = 2
	stateLava  int32 = 3
	// stateWeb stands in for a block the crosshair stops on but that does not
	// block movement — cobweb, crops, torches. The distinction between solid
	// and targetable is the whole reason IsTargetable exists.
	stateWeb int32 = 4
)

// testPackets gives every packet a distinct ID so a dispatch that matches the
// wrong field is visible rather than accidentally correct.
//
// The values are taken from the real 26.1 table where one exists, so a test
// that builds a packet is building the shape the wire actually carries.
func testPackets(t testing.TB) protocol.PacketIDs {
	t.Helper()
	v, err := protocol.ByName("26.1")
	if err != nil {
		t.Fatalf("26.1 table missing: %v", err)
	}
	return v.Packets
}

// testVersion is a synthetic table: real packet IDs, but a tiny block
// classification so a test can reason about states by name.
func testVersion(t testing.TB) *protocol.Version {
	t.Helper()
	return protocol.NewVersion(protocol.VersionSpec{
		Name:     "test",
		Protocol: 9999,
		Chunk:    protocol.ChunkFormat{HasFluidCount: true},
		Packets:  testPackets(t),
		// The test version stands in for 26.1, so its component ids are the
		// canonical ones and the mapping is the identity.
		ComponentIDs: identityComponentIDs(),
		Components:   &protocol.ComponentEncoding{},
		EntityNames: []string{
			0: "minecraft:pig",
			1: "minecraft:zombie",
			2: "minecraft:player",
			3: "minecraft:item",
		},
		ItemNames: []string{
			0: "minecraft:air",
			1: "minecraft:dirt",
			2: "minecraft:oak_log",
			3: "minecraft:oak_planks",
			4: "minecraft:dark_oak_planks",
			5: "minecraft:diamond_pickaxe",
			6: "minecraft:totem_of_undying",
			7: "minecraft:diamond_helmet",
		},
		ItemStacks: []int32{0: 64, 1: 64, 2: 64, 3: 64, 4: 64, 5: 1, 6: 1, 7: 1},
		Air:        [][2]int32{{stateAir, stateAir}},
		Solid:      [][2]int32{{stateStone, stateStone}},
		Water:      [][2]int32{{stateWater, stateWater}},
		Lava:       [][2]int32{{stateLava, stateLava}},
	})
}

// newTestClient builds a Client with no connection, for the logic that does
// not touch the socket. Anything that writes a packet needs the fake server in
// session_test.go instead.
// newTestClient builds a Client with no connection, for testing anything that
// does not put bytes on a wire.
//
// There are two of these — this and newSession — because one needs a socket and
// the other must not have one. They have to agree about the model fields, and
// twice now a new one (window) was added to only one, which shows up as a nil
// dereference deep inside an unrelated test. TestClientBuildersAgree keeps them
// honest.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	return &Client{
		opts:     Options{Username: "TestBot"},
		v:        testVersion(t),
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		uuid:     protocol.OfflineUUID("TestBot"),
		entities: entities.New(),
		effects:  newEffectSet(),
		gameMode: GameModeUnknown,
		world:    world.New(),
		inv:      inventory.New(),
		window:   inventory.NewContainer(),
		state:    protocol.StatePlay,
	}
}

// emptyColumn returns a 24-section overworld column of air at a chunk
// coordinate, ready to have blocks written into it.
func emptyColumn(chunkX, chunkZ int32) *protocol.ChunkColumn {
	col := &protocol.ChunkColumn{X: chunkX, Z: chunkZ, MinY: protocol.OverworldMinY}
	for range 24 {
		col.Sections = append(col.Sections, &protocol.ChunkSection{})
	}
	return col
}

// loadChunk gives the client a chunk and returns it so a test can place blocks.
func loadChunk(t *testing.T, c *Client, chunkX, chunkZ int32) *protocol.ChunkColumn {
	t.Helper()
	col := emptyColumn(chunkX, chunkZ)
	c.world.Store(col)
	return col
}

// setPosition puts the bot somewhere without going through the wire.
func setPosition(c *Client, x, y, z float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pos.X, c.pos.Y, c.pos.Z = x, y, z
}

// setLook aims the bot without going through the wire.
func setLook(c *Client, yaw, pitch float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pos.Yaw, c.pos.Pitch = yaw, pitch
}

// packet builds a decoded Packet as the connection would hand one over: the ID
// separate, and Data the payload with the ID already stripped.
func packet(id int32, build func(*protocol.Writer)) protocol.Packet {
	w := protocol.NewWriter(id)
	build(w)
	r := protocol.NewReader(w.Bytes())
	r.VarInt() // consume the id
	return protocol.Packet{ID: id, Data: r.Remaining()}
}

func closeEnough(a, b, tolerance float64) bool { return math.Abs(a-b) < tolerance }

// wantErrContaining fails unless err is non-nil and says what the caller needs
// to hear. An error that names a bound but not the offending value costs a
// debugging session.
func wantErrContaining(t *testing.T, err error, what string, substrings ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s = nil error, want an error", what)
	}
	for _, s := range substrings {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("%s error = %q, want it to mention %q", what, err, s)
		}
	}
}

func ids(list []Entity) []int32 {
	out := make([]int32, 0, len(list))
	for _, e := range list {
		out = append(out, e.ID)
	}
	return out
}

// Both test Client builders must initialise every model field. A field set in
// one and missed in the other fails as a nil pointer somewhere unrelated,
// which is a long way from the mistake.
func TestClientBuildersAgree(t *testing.T) {
	bare := newTestClient(t)
	wired, _ := newSession(t)

	for _, tc := range []struct {
		name string
		got  func(*Client) bool
	}{
		{"entities", func(c *Client) bool { return c.entities != nil }},
		{"world", func(c *Client) bool { return c.world != nil }},
		{"inv", func(c *Client) bool { return c.inv != nil }},
		{"window", func(c *Client) bool { return c.window != nil }},
		{"version", func(c *Client) bool { return c.v != nil }},
		{"log", func(c *Client) bool { return c.log != nil }},
	} {
		if !tc.got(bare) {
			t.Errorf("newTestClient left %s nil", tc.name)
		}
		if !tc.got(wired) {
			t.Errorf("newSession left %s nil", tc.name)
		}
	}
}

// identityComponentIDs gives the test version the canonical numbering, which is
// what a 26.1 server sends.
func identityComponentIDs() map[int32]int32 {
	ids := make(map[int32]int32, componentLastEntityVariant+1)
	for kind := int32(0); kind <= componentHighest; kind++ {
		ids[kind] = kind
	}
	return ids
}
