package control

import (
	"context"
	"errors"
	"time"

	"github.com/blocktopia/understudy-client/protocol"
	understudy "github.com/blocktopia/understudy-client/understudy"
)

// stubBot is a Bot that records what it was asked to do and returns whatever
// the test tells it to.
//
// Named for behaviour rather than mechanism: the interesting thing about it is
// that it answers without a server, which is what makes the argument parsing,
// the defaulting and the response shapes testable at all.
type stubBot struct {
	// what the handlers see
	pos      understudy.Position
	health   float32
	food     int32
	dead     bool
	deaths   int
	heldSlot int
	items    []understudy.ItemStack
	entities []understudy.Entity
	support  understudy.Support
	hit      understudy.RayHit
	hitOK    bool
	// hitsLanded, when set below the requested count, models a target that
	// died partway through an attack run.
	hitsLanded int
	version    *protocol.Version

	// what the handlers get back
	err error

	// what the handlers did
	calls    []string
	lastDig  [3]int32
	lastFace int32
	lastHold time.Duration
	lastDraw time.Duration
	attacks  int
	layout   map[int]string
	dropAll  bool
	sneakFor time.Duration
}

func newStubBot() *stubBot {
	return &stubBot{
		pos:     understudy.Position{X: 1, Y: 64, Z: -2, Yaw: 90, Pitch: -10},
		health:  20,
		food:    18,
		version: stubVersion(),
	}
}

func (b *stubBot) record(name string) { b.calls = append(b.calls, name) }

func (b *stubBot) called(name string) bool {
	for _, c := range b.calls {
		if c == name {
			return true
		}
	}
	return false
}

// stubVersion is a synthetic table; the handlers only use it to classify block
// states and look up stack sizes.
func stubVersion() *protocol.Version {
	return protocol.NewVersion(protocol.VersionSpec{
		Name:       "test",
		Protocol:   9999,
		ItemNames:  []string{0: "minecraft:dirt", 1: "minecraft:totem_of_undying"},
		ItemStacks: []int32{0: 64, 1: 1},
		Air:        [][2]int32{{0, 0}},
		Solid:      [][2]int32{{1, 1}},
		Water:      [][2]int32{{2, 2}},
		Lava:       [][2]int32{{3, 3}},
	})
}

// errRefused stands in for the world saying no — dead, out of reach, not in
// play yet — as opposed to the caller sending something malformed.
var errRefused = errors.New("understudy: cannot dig while dead")

// --- identity and state ---

func (b *stubBot) Username() string              { return "StubBot" }
func (b *stubBot) UUID() protocol.UUID           { return protocol.OfflineUUID("StubBot") }
func (b *stubBot) State() protocol.State         { return protocol.StatePlay }
func (b *stubBot) EntityID() int32               { return 42 }
func (b *stubBot) Joined() bool                  { return true }
func (b *stubBot) Dead() bool                    { return b.dead }
func (b *stubBot) Deaths() int                   { return b.deaths }
func (b *stubBot) Health() (float32, int32)      { return b.health, b.food }
func (b *stubBot) Position() understudy.Position { return b.pos }
func (b *stubBot) Version() *protocol.Version    { return b.version }

// --- looking ---

func (b *stubBot) Look(yaw, pitch float32) error { b.record("Look"); return b.err }
func (b *stubBot) LookDirection(name string) error {
	b.record("LookDirection:" + name)
	return b.err
}
func (b *stubBot) LookYawPitch(yaw, pitch *float32) error { b.record("LookYawPitch"); return b.err }
func (b *stubBot) LookAt(x, y, z float64) error           { b.record("LookAt"); return b.err }
func (b *stubBot) LookAtBlock(x, y, z int32) error        { b.record("LookAtBlock"); return b.err }
func (b *stubBot) LookAtNearest(typeName string) (understudy.Entity, error) {
	b.record("LookAtNearest:" + typeName)
	if b.err != nil {
		return understudy.Entity{}, b.err
	}
	return understudy.Entity{ID: 7, TypeName: protocol.Namespaced(typeName)}, nil
}
func (b *stubBot) LookAtPlayer(name string) (understudy.Entity, error) {
	b.record("LookAtPlayer:" + name)
	if b.err != nil {
		return understudy.Entity{}, b.err
	}
	return understudy.Entity{ID: 9, TypeName: "minecraft:player"}, nil
}
func (b *stubBot) LookingAt() (understudy.RayHit, bool) { return b.hit, b.hitOK }

// --- moving ---

func (b *stubBot) MoveTo(x, y, z float64) error { b.record("MoveTo"); return b.err }
func (b *stubBot) WalkTo(ctx context.Context, x, y, z float64) error {
	b.record("WalkTo")
	return b.err
}
func (b *stubBot) Fall(ctx context.Context) (float64, error) {
	b.record("Fall")
	return 3.5, b.err
}
func (b *stubBot) FallTo(ctx context.Context, groundY float64) (float64, error) {
	b.record("FallTo")
	return 2.5, b.err
}
func (b *stubBot) Sneak(ctx context.Context, d time.Duration) error {
	b.record("Sneak")
	b.sneakFor = d
	return b.err
}

// --- inventory ---

func (b *stubBot) Inventory() []understudy.ItemStack { return b.items }
func (b *stubBot) HeldItem() (understudy.ItemStack, bool) {
	if len(b.items) == 0 {
		return understudy.ItemStack{}, false
	}
	return b.items[0], true
}
func (b *stubBot) HeldSlot() int { return b.heldSlot }
func (b *stubBot) SetHeldSlot(slot int) error {
	b.record("SetHeldSlot")
	if b.err != nil {
		return b.err
	}
	b.heldSlot = slot
	return nil
}
func (b *stubBot) HoldItem(name string) (understudy.ItemStack, error) {
	b.record("HoldItem:" + name)
	if b.err != nil {
		return understudy.ItemStack{}, b.err
	}
	return understudy.ItemStack{Slot: 12, Name: protocol.Namespaced(name), Count: 1}, nil
}
func (b *stubBot) DropHeld(ctx context.Context, all bool) error {
	b.record("DropHeld")
	b.dropAll = all
	return b.err
}
func (b *stubBot) EquipArmour(name string) (understudy.ItemStack, error) {
	b.record("EquipArmour:" + name)
	if b.err != nil {
		return understudy.ItemStack{}, b.err
	}
	return understudy.ItemStack{Slot: 14, Name: protocol.Namespaced(name), Count: 1}, nil
}
func (b *stubBot) InventoryTruncated() bool           { return false }
func (b *stubBot) CountItem(name string) int32        { return 7 }
func (b *stubBot) CountItemStorage(name string) int32 { return 5 }
func (b *stubBot) FreeStorageSlots() int              { return 30 }
func (b *stubBot) SlotsNeeded(name string, count int32) (int, bool) {
	if count <= 0 {
		return 0, true
	}
	stack := b.version.StackSizeOf(name)
	slots := int((count + stack - 1) / stack)
	return slots, slots <= 36
}
func (b *stubBot) PickupsSeen() (int32, map[string]int32) {
	return 4, map[string]int32{"item": 4}
}

// --- world ---

func (b *stubBot) BlockAt(x, y, z int32) int32         { return 1 }
func (b *stubBot) ChunkLoaded(x, z int32) bool         { return true }
func (b *stubBot) LoadedChunks() int                   { return 12 }
func (b *stubBot) GroundBelow() understudy.Support     { return b.support }
func (b *stubBot) Submerged() bool                     { return false }
func (b *stubBot) BlockDistance(x, y, z int32) float64 { return 2.5 }
func (b *stubBot) CanReachBlock(x, y, z int32) bool    { return true }

// --- entities ---

func (b *stubBot) Entities() []understudy.Entity { return b.entities }
func (b *stubBot) EntitiesOfType(typeName string) []understudy.Entity {
	want := protocol.Namespaced(typeName)
	var out []understudy.Entity
	for _, e := range b.entities {
		if e.TypeName == want {
			out = append(out, e)
		}
	}
	return out
}
func (b *stubBot) DistanceTo(e understudy.Entity) float64 {
	dx, dy, dz := e.X-b.pos.X, e.Y-b.pos.Y, e.Z-b.pos.Z
	return dx*dx + dy*dy + dz*dz // squared: only ordering and thresholds matter here
}
func (b *stubBot) Attack(entityID int32) error {
	b.record("Attack")
	b.attacks++
	return b.err
}
func (b *stubBot) AttackTimes(ctx context.Context, typeName string, times int) (understudy.Entity, int, error) {
	b.record("AttackTimes:" + typeName)
	b.attacks += times
	if b.err != nil {
		return understudy.Entity{}, 0, b.err
	}
	// hitsLanded lets a test model a target that dies partway through.
	hits := times
	if b.hitsLanded > 0 && b.hitsLanded < times {
		hits = b.hitsLanded
	}
	return understudy.Entity{ID: 5, TypeName: protocol.Namespaced(typeName)}, hits, nil
}
func (b *stubBot) InteractEntity(entityID int32) error { b.record("InteractEntity"); return b.err }
func (b *stubBot) InteractNearest(typeName string) (understudy.Entity, error) {
	b.record("InteractNearest:" + typeName)
	if b.err != nil {
		return understudy.Entity{}, b.err
	}
	return understudy.Entity{ID: 3, TypeName: protocol.Namespaced(typeName)}, nil
}

// --- acting ---

func (b *stubBot) Swing() error { b.record("Swing"); return b.err }
func (b *stubBot) DigBlock(ctx context.Context, x, y, z, face int32, hold time.Duration) error {
	b.record("DigBlock")
	b.lastDig = [3]int32{x, y, z}
	b.lastFace, b.lastHold = face, hold
	return b.err
}
func (b *stubBot) DigBlocks(ctx context.Context, blocks [][3]int32, face int32, hold time.Duration) (int, error) {
	b.record("DigBlocks")
	b.lastFace, b.lastHold = face, hold
	if b.err != nil {
		return len(blocks) - 1, b.err
	}
	return len(blocks), nil
}
func (b *stubBot) DigLookingAt(ctx context.Context, hold time.Duration) (understudy.RayHit, error) {
	b.record("DigLookingAt")
	b.lastHold = hold
	return b.hit, b.err
}
func (b *stubBot) PlaceBlock(ctx context.Context, x, y, z, face int32) error {
	b.record("PlaceBlock")
	b.lastFace = face
	return b.err
}
func (b *stubBot) PlaceBlockVerified(ctx context.Context, x, y, z, face int32) error {
	b.record("PlaceBlockVerified")
	b.lastFace = face
	return b.err
}
func (b *stubBot) UseItem(ctx context.Context) error { b.record("UseItem"); return b.err }
func (b *stubBot) Consume(ctx context.Context) error { b.record("Consume"); return b.err }
func (b *stubBot) ConsumeItem(ctx context.Context, name string) (understudy.ItemStack, error) {
	b.record("ConsumeItem:" + name)
	return understudy.ItemStack{}, b.err
}
func (b *stubBot) CraftIn2x2(ctx context.Context, layout map[int]string) (understudy.ItemStack, error) {
	b.record("CraftIn2x2")
	b.layout = layout
	if b.err != nil {
		return understudy.ItemStack{}, b.err
	}
	return understudy.ItemStack{Name: "minecraft:oak_planks", Count: 4}, nil
}
func (b *stubBot) ShootAt(ctx context.Context, x, y, z float64, draw time.Duration) error {
	b.record("ShootAt")
	b.lastDraw = draw
	return b.err
}
func (b *stubBot) ShootBlock(ctx context.Context, x, y, z int32, draw time.Duration) error {
	b.record("ShootBlock")
	b.lastDraw = draw
	return b.err
}
func (b *stubBot) ShootNearest(ctx context.Context, typeName string, draw time.Duration) (understudy.Entity, error) {
	b.record("ShootNearest:" + typeName)
	b.lastDraw = draw
	if b.err != nil {
		return understudy.Entity{}, b.err
	}
	return understudy.Entity{ID: 11, TypeName: protocol.Namespaced(typeName)}, nil
}

var _ Bot = (*stubBot)(nil)
