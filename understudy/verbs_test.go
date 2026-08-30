package understudy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// The verbs are defined by the packets they put on the wire, so they are
// exercised against the fake server rather than mocked out.

func TestLookSendsRotation(t *testing.T) {
	c, s := settled(t)
	if err := c.Look(90, -20); err != nil {
		t.Fatalf("Look: %v", err)
	}
	waitFor(t, time.Second, "the look packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayLook) > 0
	})

	if pos := c.Position(); pos.Yaw != 90 || pos.Pitch != -20 {
		t.Errorf("Position() = yaw %g pitch %g, want 90/-20", pos.Yaw, pos.Pitch)
	}
	r := s.first(t, c.v.Packets.SBPlayLook, "look").Reader()
	if yaw, pitch := r.F32(), r.F32(); yaw != 90 || pitch != -20 {
		t.Errorf("look packet carried %g/%g, want 90/-20", yaw, pitch)
	}
}

// A nil component leaves that axis alone, so a caller can pan without
// re-deriving the current tilt.
func TestLookYawPitchLeavesNilAxisAlone(t *testing.T) {
	c, _ := settled(t)
	if err := c.Look(45, -30); err != nil {
		t.Fatalf("Look: %v", err)
	}
	newYaw := float32(90)
	if err := c.LookYawPitch(&newYaw, nil); err != nil {
		t.Fatalf("LookYawPitch: %v", err)
	}
	pos := c.Position()
	if pos.Yaw != 90 {
		t.Errorf("yaw = %g, want 90", pos.Yaw)
	}
	if pos.Pitch != -30 {
		t.Errorf("pitch = %g, want the original -30", pos.Pitch)
	}
}

func TestLookDirectionAimsCorrectly(t *testing.T) {
	c, _ := settled(t)
	if err := c.LookDirection("north"); err != nil {
		t.Fatalf("LookDirection: %v", err)
	}
	if got := c.Position().Yaw; got != 180 {
		t.Errorf("yaw after looking north = %g, want 180", got)
	}
	// up tilts without changing the heading.
	if err := c.LookDirection("up"); err != nil {
		t.Fatalf("LookDirection(up): %v", err)
	}
	if pos := c.Position(); pos.Yaw != 180 || pos.Pitch != -90 {
		t.Errorf("after looking up = yaw %g pitch %g, want 180/-90", pos.Yaw, pos.Pitch)
	}
}

// Block coordinates name a corner, so aiming at the raw integer targets the
// seam between four blocks.
func TestLookAtBlockAimsAtTheCentre(t *testing.T) {
	c, _ := settled(t)
	setPosition(c, 0.5, 64, 0.5)
	if err := c.LookAtBlock(0, 63, 0); err != nil {
		t.Fatalf("LookAtBlock: %v", err)
	}
	if got := c.Position().Pitch; got < 45 {
		t.Errorf("pitch aiming at the block underfoot = %g, want steeply down", got)
	}
}

func TestMoveToUpdatesPositionAndSends(t *testing.T) {
	c, s := settled(t)
	if err := c.MoveTo(10, 64, -20); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if pos := c.Position(); pos.X != 10 || pos.Y != 64 || pos.Z != -20 {
		t.Errorf("Position() = %+v, want (10,64,-20)", pos)
	}
	waitFor(t, time.Second, "the position packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayPositionLook) > 0
	})
}

func TestWalkToArrives(t *testing.T) {
	c, _ := settled(t)
	c.opts.DisableIdlePosition = true
	setPosition(c, 0, 64, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.WalkTo(ctx, 1, 64, 0); err != nil {
		t.Fatalf("WalkTo: %v", err)
	}
	pos := c.Position()
	if !closeEnough(pos.X, 1, 1e-6) || !closeEnough(pos.Z, 0, 1e-6) {
		t.Errorf("Position() after walking = %+v, want (1,64,0)", pos)
	}
}

func TestWalkToHonoursContext(t *testing.T) {
	c, _ := settled(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.WalkTo(ctx, 1000, 64, 1000); err == nil {
		t.Error("WalkTo with a cancelled context = nil error, want ctx.Err()")
	}
}

func TestSwingSendsArmAnimation(t *testing.T) {
	c, s := settled(t)
	if err := c.Swing(); err != nil {
		t.Fatalf("Swing: %v", err)
	}
	waitFor(t, time.Second, "the arm animation", func() bool {
		return s.countOf(c.v.Packets.SBPlayArmAnimation) > 0
	})
}

func TestSetHeldSlotSends(t *testing.T) {
	c, s := settled(t)
	if err := c.SetHeldSlot(5); err != nil {
		t.Fatalf("SetHeldSlot: %v", err)
	}
	if got := c.HeldSlot(); got != 5 {
		t.Errorf("HeldSlot() = %d, want 5", got)
	}
	waitFor(t, time.Second, "the held-item packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayHeldItemSlot) > 0
	})
}

// Dropping rides on block_dig with a status meaning "drop" rather than
// "break", so the status byte is the whole distinction.
func TestDropHeldUsesTheRightStatus(t *testing.T) {
	for _, tc := range []struct {
		name       string
		all        bool
		wantStatus int32
	}{
		{"a single item", false, protocol.DigDropItem},
		{"the whole stack", true, protocol.DigDropStack},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := settled(t)
			if err := c.DropHeld(context.Background(), tc.all); err != nil {
				t.Fatalf("DropHeld: %v", err)
			}
			waitFor(t, time.Second, "the drop packet", func() bool {
				return s.countOf(c.v.Packets.SBPlayBlockDig) > 0
			})
			got := s.first(t, c.v.Packets.SBPlayBlockDig, "block_dig").Reader().VarInt()
			if got != tc.wantStatus {
				t.Errorf("block_dig status = %d, want %d", got, tc.wantStatus)
			}
		})
	}
}

// Since 1.19 every dig and place carries a monotonically increasing sequence,
// and it is per-connection so two bots do not share a counter.
func TestBlockSequenceIsPerClientAndIncreasing(t *testing.T) {
	a, _ := settled(t)
	b, _ := settled(t)

	if got := a.nextSequence(); got != 1 {
		t.Errorf("first sequence = %d, want 1", got)
	}
	if got := a.nextSequence(); got != 2 {
		t.Errorf("second sequence = %d, want 2", got)
	}
	if got := b.nextSequence(); got != 1 {
		t.Errorf("a second client's first sequence = %d, want 1 — the counter leaked", got)
	}
}

func TestStartAndFinishDigSendTheRightStatuses(t *testing.T) {
	c, s := settled(t)
	ctx := context.Background()
	if err := c.StartDig(ctx, 1, 2, 3, protocol.FaceTop); err != nil {
		t.Fatalf("StartDig: %v", err)
	}
	if err := c.FinishDig(ctx, 1, 2, 3, protocol.FaceTop); err != nil {
		t.Fatalf("FinishDig: %v", err)
	}
	waitFor(t, time.Second, "both dig packets", func() bool {
		return s.countOf(c.v.Packets.SBPlayBlockDig) >= 2
	})

	var statuses []int32
	for _, p := range s.received() {
		if p.ID != c.v.Packets.SBPlayBlockDig {
			continue
		}
		r := p.Reader()
		statuses = append(statuses, r.VarInt())
		x, y, z := protocol.DecodeBlockPos(r.I64())
		if x != 1 || y != 2 || z != 3 {
			t.Errorf("dig addressed %d,%d,%d, want 1,2,3", x, y, z)
		}
	}
	if len(statuses) < 2 || statuses[0] != protocol.DigStart || statuses[1] != protocol.DigFinish {
		t.Errorf("dig statuses = %v, want [%d %d]", statuses, protocol.DigStart, protocol.DigFinish)
	}
}

// With a good tool the server breaks the block the moment it sees START, so
// any fixed hold is pure latency. Watching the world model returns the instant
// it goes.
func TestAwaitBreakReturnsAsSoonAsTheBlockGoes(t *testing.T) {
	c, _ := settled(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(1, 2, 3, stateStone)

	go func() {
		time.Sleep(40 * time.Millisecond)
		c.world.SetBlockState(1, 2, 3, stateAir)
	}()

	start := time.Now()
	if err := c.awaitBreak(context.Background(), 1, 2, 3, protocol.FaceTop, 5*time.Second); err != nil {
		t.Fatalf("awaitBreak: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("awaitBreak took %v after the block went at 40ms", elapsed)
	}
}

// The server ignores a dig it considers invalid without any reply, so a block
// that never changes has to become a real error.
func TestAwaitBreakReportsABlockThatNeverBreaks(t *testing.T) {
	c, _ := settled(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(1, 2, 3, stateStone)

	err := c.awaitBreak(context.Background(), 1, 2, 3, protocol.FaceTop, 10*time.Millisecond)
	wantErrContaining(t, err, "awaitBreak on an unbreakable block", "still solid")
}

func TestAwaitBreakHonoursContext(t *testing.T) {
	c, _ := settled(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(1, 2, 3, stateStone)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := c.awaitBreak(ctx, 1, 2, 3, protocol.FaceTop, time.Hour); err == nil {
		t.Error("awaitBreak = nil error with an expiring context, want ctx.Err()")
	}
}

// Presence is tested with IsTargetable rather than IsSolid: a cobweb is not
// solid *while it is still there*, so a collision check reports the break as
// complete the instant it is asked — a false pass on exactly the blocks most
// likely to need a retry.
func TestConfirmBlockBecameUsesTargetability(t *testing.T) {
	c, _ := settled(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(1, 2, 3, stateWeb)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := c.confirmBlockBecame(ctx, 1, 2, 3, false); err == nil {
		t.Error("confirmBlockBecame(broken) = nil for a standing cobweb, want an error")
	}
	if err := c.confirmBlockBecame(context.Background(), 1, 2, 3, true); err != nil {
		t.Errorf("confirmBlockBecame(placed) = %v for a standing cobweb, want nil", err)
	}
}

func TestConfirmBlockBecameSkipsUnloadedTerrain(t *testing.T) {
	c, _ := settled(t)
	if err := c.confirmBlockBecame(context.Background(), 1, 2, 3, true); err != nil {
		t.Errorf("confirmBlockBecame with unloaded terrain = %v, want nil", err)
	}
}

// One unreachable corner should not abandon the rest of the field.
func TestDigBlocksReportsUnreachableAndKeepsGoing(t *testing.T) {
	c, _ := settled(t)
	c.opts.DisableIdlePosition = true
	loadChunk(t, c, 0, 0)
	setPosition(c, 0.5, 64, 0.5)

	targets := [][3]int32{{0, 63, 0}, {1, 63, 0}, {60, 63, 0}}
	for _, b := range targets {
		c.world.SetBlockState(b[0], b[1], b[2], stateStone)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		c.world.SetBlockState(0, 63, 0, stateAir)
		time.Sleep(30 * time.Millisecond)
		c.world.SetBlockState(1, 63, 0, stateAir)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dug, err := c.DigBlocks(ctx, targets, protocol.FaceTop, 10*time.Millisecond)
	wantErrContaining(t, err, "DigBlocks with an unreachable block", "out of reach")
	if dug != 2 {
		t.Errorf("dug = %d, want 2 — the reachable blocks should still be worked", dug)
	}
}

func TestPlaceBlockSends(t *testing.T) {
	c, s := settled(t)
	setPosition(c, 0.5, 64, 0.5)
	if err := c.PlaceBlock(context.Background(), 0, 63, 0, protocol.FaceTop); err != nil {
		t.Fatalf("PlaceBlock: %v", err)
	}
	waitFor(t, time.Second, "the place packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayBlockPlace) > 0
	})
}

func TestPlaceBlockRefusesOutOfReach(t *testing.T) {
	c, _ := settled(t)
	setPosition(c, 0.5, 64, 0.5)
	if err := c.PlaceBlock(context.Background(), 60, 63, 0, protocol.FaceTop); err == nil {
		t.Error("PlaceBlock beyond reach = nil error, want an error")
	}
}

func TestUseItemSends(t *testing.T) {
	c, s := settled(t)
	if err := c.UseItem(context.Background()); err != nil {
		t.Fatalf("UseItem: %v", err)
	}
	waitFor(t, time.Second, "the use_item packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayUseItem) > 0
	})
}

// Before 26.1 attacking folded into use_entity with a mode field, which this
// client does not implement — so it must say so rather than send a packet the
// server ignores in silence.
func TestAttackRequiresTheDedicatedPacket(t *testing.T) {
	c, _ := settled(t)
	c.v = protocol.NewVersion(protocol.VersionSpec{
		Name: "old", Packets: protocol.PacketIDs{SBPlayAttack: protocol.Absent},
	})
	wantErrContaining(t, c.Attack(1), "Attack on a version without the packet", "not implemented")
}

func TestAttackSends(t *testing.T) {
	c, s := settled(t)
	if err := c.Attack(42); err != nil {
		t.Fatalf("Attack: %v", err)
	}
	waitFor(t, time.Second, "the attack packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayAttack) > 0
	})
	if got := s.first(t, c.v.Packets.SBPlayAttack, "attack").Reader().VarInt(); got != 42 {
		t.Errorf("attack carried entity %d, want 42", got)
	}
}

func TestInteractEntitySends(t *testing.T) {
	c, s := settled(t)
	if err := c.InteractEntity(7); err != nil {
		t.Fatalf("InteractEntity: %v", err)
	}
	waitFor(t, time.Second, "the use_entity packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayUseEntity) > 0
	})
}

func TestAttackTimesRespectsTheCooldown(t *testing.T) {
	c, _ := settled(t)
	setPosition(c, 0, 0, 0)
	c.entities.Spawn(&Entity{ID: 1, TypeName: "minecraft:zombie", X: 1})

	// Three hits at a 600ms cooldown cannot fit in 50ms, so the context must
	// cut it short rather than the cooldown being skipped.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, hits, err := c.AttackTimes(ctx, "zombie", 3)
	if err == nil {
		t.Error("AttackTimes = nil error with too short a deadline, want ctx.Err()")
	}
	// It still reports what it managed before the deadline, rather than
	// discarding the count along with the error.
	if hits != 1 {
		t.Errorf("AttackTimes landed %d hits before the deadline, want 1", hits)
	}
}

// A diamond pickaxe one-shots a chicken, so "attack it three times" lands one
// hit and then finds nothing to swing at. That is the caller succeeding, and
// used to be reported as "no tracked entity of type chicken" — an error that
// blames the caller and names the wrong problem.
func TestAttackTimesStopsWhenTheTargetDies(t *testing.T) {
	c, _ := settled(t)
	setPosition(c, 0, 0, 0)
	c.entities.Spawn(&Entity{ID: 1, TypeName: "minecraft:chicken", X: 1})

	// Killing it after the first swing is what the server would do.
	go func() {
		time.Sleep(AttackCooldown / 4)
		c.entities.Remove([]int32{1})
	}()

	target, hits, err := c.AttackTimes(context.Background(), "chicken", 3)
	if err != nil {
		t.Fatalf("AttackTimes after the target died: %v, want nil", err)
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1 — the chicken died after the first swing", hits)
	}
	if target.ID != 1 {
		t.Errorf("target.ID = %d, want the chicken it did hit (1)", target.ID)
	}
}

// Finding nothing on the *first* swing is still an error: nothing was attacked.
func TestAttackTimesFailsWithNoTargetAtAll(t *testing.T) {
	c, _ := settled(t)
	setPosition(c, 0, 0, 0)

	_, hits, err := c.AttackTimes(context.Background(), "chicken", 3)
	if err == nil {
		t.Fatal("AttackTimes with nothing to attack = nil error, want an error")
	}
	if !errors.Is(err, ErrNoSuchEntity) {
		t.Errorf("error = %v, want it to wrap ErrNoSuchEntity", err)
	}
	if hits != 0 {
		t.Errorf("hits = %d, want 0", hits)
	}
}

// Sneaking moved to player_input in 26.1; entity_action no longer carries it,
// so a client looking there sneaks silently never.
func TestSetSneakingUsesPlayerInput(t *testing.T) {
	c, s := settled(t)
	if err := c.SetSneaking(true); err != nil {
		t.Fatalf("SetSneaking: %v", err)
	}
	waitFor(t, time.Second, "the player_input packet", func() bool {
		return s.countOf(c.v.Packets.SBPlayPlayerInput) > 0
	})
	if c.Input()&protocol.InputSneak == 0 {
		t.Error("Input() does not have the sneak bit set")
	}

	// Sprinting must not clear sneaking.
	if err := c.SetSprinting(true); err != nil {
		t.Fatalf("SetSprinting: %v", err)
	}
	if got := c.Input(); got&protocol.InputSneak == 0 || got&protocol.InputSprint == 0 {
		t.Errorf("Input() = %#b, want both sneak and sprint set", got)
	}

	if err := c.SetSneaking(false); err != nil {
		t.Fatalf("SetSneaking(false): %v", err)
	}
	if got := c.Input(); got&protocol.InputSneak != 0 {
		t.Errorf("Input() = %#b, want the sneak bit cleared", got)
	} else if got&protocol.InputSprint == 0 {
		t.Error("clearing sneak also cleared sprint")
	}
}

// A cancelled context must still stand the bot back up, or it stays crouched
// for the rest of the session.
func TestSneakAlwaysReleases(t *testing.T) {
	c, _ := settled(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := c.Sneak(ctx, time.Hour); err == nil {
		t.Error("Sneak = nil error with an expiring context, want ctx.Err()")
	}
	if got := c.Input(); got&protocol.InputSneak != 0 {
		t.Errorf("Input() = %#b after a cancelled Sneak, want the sneak bit cleared", got)
	}
}

func TestHoldItemSelectsAHotbarSlotDirectly(t *testing.T) {
	c, s := settled(t)
	c.inv.SetSlot(SlotHotbarStart+4, stack(SlotHotbarStart+4, "diamond_pickaxe", 1))

	got, err := c.HoldItem("diamond_pickaxe")
	if err != nil {
		t.Fatalf("HoldItem: %v", err)
	}
	if got.Slot != SlotHotbarStart+4 {
		t.Errorf("found in slot %d, want %d", got.Slot, SlotHotbarStart+4)
	}
	if c.HeldSlot() != 4 {
		t.Errorf("HeldSlot() = %d, want 4", c.HeldSlot())
	}
	// Already on the hotbar: a selection, not a container click.
	if n := s.countOf(c.v.Packets.SBPlayWindowClick); n != 0 {
		t.Errorf("sent %d window clicks for an item already on the hotbar, want 0", n)
	}
}

// A bot given a tool by /give has no control over where it lands, so an item
// in the main inventory has to be swapped onto the hotbar.
func TestHoldItemSwapsFromTheMainInventory(t *testing.T) {
	c, s := settled(t)
	c.inv.SetSlot(20, stack(20, "diamond_pickaxe", 1))

	if _, err := c.HoldItem("diamond_pickaxe"); err != nil {
		t.Fatalf("HoldItem: %v", err)
	}
	waitFor(t, time.Second, "the window click", func() bool {
		return s.countOf(c.v.Packets.SBPlayWindowClick) > 0
	})
	// The optimistic local update must have moved it, so an immediately
	// following action sees the right thing.
	held, ok := c.HeldItem()
	if !ok || held.Name != "minecraft:diamond_pickaxe" {
		t.Errorf("HeldItem() = %+v, %v; want the pickaxe", held, ok)
	}
	if _, still := c.SlotAt(20); still {
		t.Error("slot 20 still holds the pickaxe after the swap")
	}
}

// A partial view is why a "missing" item might not actually be missing.
func TestHoldItemExplainsATruncatedView(t *testing.T) {
	c, _ := settled(t)
	c.inv.ReplaceAll(nil, true)
	wantErrContaining(t, func() error { _, err := c.HoldItem("diamond_pickaxe"); return err }(),
		"HoldItem with a truncated view", "incomplete")
}

func TestEquipArmourSkipsAlreadyWornItems(t *testing.T) {
	c, s := settled(t)
	c.inv.SetSlot(SlotArmorHead, stack(SlotArmorHead, "diamond_helmet", 1))

	if _, err := c.EquipArmour("diamond_helmet"); err != nil {
		t.Fatalf("EquipArmour: %v", err)
	}
	if n := s.countOf(c.v.Packets.SBPlayWindowClick); n != 0 {
		t.Errorf("sent %d clicks for an already-worn helmet, want 0", n)
	}
}

// It leans on the server's own shift-click placement rules rather than
// replicating them client-side.
func TestEquipArmourQuickMoves(t *testing.T) {
	c, s := settled(t)
	c.inv.SetSlot(20, stack(20, "diamond_helmet", 1))

	if _, err := c.EquipArmour("diamond_helmet"); err != nil {
		t.Fatalf("EquipArmour: %v", err)
	}
	waitFor(t, time.Second, "the quick-move click", func() bool {
		return s.countOf(c.v.Packets.SBPlayWindowClick) > 0
	})
	r := s.first(t, c.v.Packets.SBPlayWindowClick, "window click").Reader()
	r.VarInt() // window
	r.VarInt() // state id
	r.I16()    // slot
	r.I8()     // button
	if mode := r.VarInt(); mode != ClickModeQuickMove {
		t.Errorf("click mode = %d, want ClickModeQuickMove (%d)", mode, ClickModeQuickMove)
	}
}

func TestEquipArmourNeedsTheItem(t *testing.T) {
	c, _ := settled(t)
	if _, err := c.EquipArmour("diamond_helmet"); err == nil {
		t.Error("EquipArmour with an empty inventory = nil error, want an error")
	}
}

func TestCraftRejectsSlotsOutsideTheGrid(t *testing.T) {
	c, _ := settled(t)
	for _, slot := range []int{0, 5, -1, 9} {
		if _, err := c.CraftIn2x2(context.Background(), map[int]string{slot: "oak_planks"}); err == nil {
			t.Errorf("CraftIn2x2 with grid slot %d = nil error, want an error", slot)
		}
	}
}

func TestCraftReportsMissingIngredients(t *testing.T) {
	c, _ := settled(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := c.CraftIn2x2(ctx, map[int]string{1: "oak_planks"})
	wantErrContaining(t, err, "CraftIn2x2 with an empty inventory", "oak_planks")
}

func TestClearCraftingGridIsANoOpWhenEmpty(t *testing.T) {
	c, s := settled(t)
	if err := c.ClearCraftingGrid(context.Background()); err != nil {
		t.Fatalf("ClearCraftingGrid: %v", err)
	}
	if n := s.countOf(c.v.Packets.SBPlayWindowClick); n != 0 {
		t.Errorf("sent %d clicks for an empty grid, want 0", n)
	}
}

// The server ignores actions from a dead player without complaint.
func TestVerbsRefuseWhileDead(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(*Client) error
	}{
		{"look", func(c *Client) error { return c.Look(0, 0) }},
		{"move", func(c *Client) error { return c.MoveTo(0, 0, 0) }},
		{"swing", func(c *Client) error { return c.Swing() }},
		{"dig", func(c *Client) error { return c.StartDig(ctx, 0, 0, 0, 1) }},
		{"place", func(c *Client) error { return c.PlaceBlock(ctx, 0, 0, 0, 1) }},
		{"use", func(c *Client) error { return c.UseItem(ctx) }},
		{"drop", func(c *Client) error { return c.DropHeld(ctx, false) }},
		{"attack", func(c *Client) error { return c.Attack(1) }},
		{"slot", func(c *Client) error { return c.SetHeldSlot(1) }},
		{"input", func(c *Client) error { return c.SetInput(0) }},
		{"interact", func(c *Client) error { return c.InteractEntity(1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := settled(t)
			c.mu.Lock()
			c.dead = true
			c.mu.Unlock()
			wantErrContaining(t, tc.call(c), tc.name+" while dead", "while dead")
		})
	}
}

func TestFallToRejectsAHeightAlreadyPassed(t *testing.T) {
	c, _ := settled(t)
	setPosition(c, 0, 64, 0)
	if _, err := c.FallTo(context.Background(), 70); err == nil {
		t.Error("FallTo above the bot = nil error, want an error")
	}
}

// Already standing on the floor is auto-fall's most common outcome, and it is
// a success — reporting it as an error made every ordinary teleport warn.
func TestFallOnSolidGroundIsANoOp(t *testing.T) {
	c, _ := settled(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(0, 63, 0, stateStone)
	setPosition(c, 0.5, 64, 0.5)

	fell, err := c.Fall(context.Background())
	if err != nil {
		t.Fatalf("Fall while already grounded: %v", err)
	}
	if fell != 0 {
		t.Errorf("fell = %g while already on the floor, want 0", fell)
	}
}

func TestFallRefusesLava(t *testing.T) {
	c, _ := settled(t)
	loadChunk(t, c, 0, 0)
	c.world.SetBlockState(0, 60, 0, stateLava)
	setPosition(c, 0.5, 70, 0.5)

	_, err := c.Fall(context.Background())
	wantErrContaining(t, err, "Fall into lava", "lava")
}

// Water cancels fall damage completely and then drowns anything that stays
// under, so entering it is where the fall ends — and onGround stays false,
// because a player floating in water is not standing on anything.
func TestFallIntoWaterStopsAtTheSurface(t *testing.T) {
	c, _ := settled(t)
	c.opts.DisableIdlePosition = true
	loadChunk(t, c, 0, 0)
	for y := int32(58); y <= 62; y++ {
		c.world.SetBlockState(0, y, 0, stateWater)
	}
	setPosition(c, 0.5, 70, 0.5)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fell, err := c.Fall(ctx)
	if err != nil {
		t.Fatalf("Fall into water: %v", err)
	}
	if fell <= 0 {
		t.Errorf("fell = %g, want a real descent", fell)
	}
	if c.OnGround() {
		t.Error("OnGround() = true after entering water; the server would treat that as an impact")
	}
}

// Walking into a wall is not an error the server reports: it corrects the
// position back and the bot asks again. Before this, the loop ran until the
// caller's context expired — sixty seconds against a live server — and then
// blamed the context, which says nothing about a wall. It has to notice that
// it is getting no closer and say so.
func TestWalkToGivesUpWhenItStopsMakingProgress(t *testing.T) {
	c, _ := settled(t)
	c.opts.DisableIdlePosition = true
	setPosition(c, 0, 64, 0)

	// A server that pins the bot in place, which is what one does to a client
	// trying to walk through a block.
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(TickRate / 2)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				setPosition(c, 0, 64, 0)
			}
		}
	}()

	// Well under the deadline: the point is that it returns on its own.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := time.Now()
	err := c.WalkTo(ctx, 100, 64, 0)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("WalkTo through a wall = nil, want an error naming the obstruction")
	}
	if ctx.Err() != nil {
		t.Fatalf("WalkTo ran until the context expired: %v", err)
	}
	if want := time.Duration(walkStallTicks) * TickRate; elapsed > want*4 {
		t.Errorf("WalkTo gave up after %v, want roughly %v", elapsed, want)
	}
	if !strings.Contains(err.Error(), "no progress") {
		t.Errorf("WalkTo error = %q, want it to name the lack of progress", err)
	}
}

// Sprinting is walking times 1.3, with the sprint input held so the server sees
// a sprinting player rather than a walking one covering ground too fast.
func TestSprintingIsFasterAndHoldsTheInput(t *testing.T) {
	c, _ := settled(t)
	c.opts.DisableIdlePosition = true
	setPosition(c, 0, 64, 0)
	setFood(c, 20)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	began := time.Now()
	if err := c.SprintTo(ctx, 12, 64, 0); err != nil {
		t.Fatalf("SprintTo: %v", err)
	}
	sprinted := time.Since(began)

	setPosition(c, 0, 64, 0)
	began = time.Now()
	if err := c.WalkTo(ctx, 12, 64, 0); err != nil {
		t.Fatalf("WalkTo: %v", err)
	}
	walked := time.Since(began)

	if sprinted >= walked {
		t.Errorf("sprinting took %v and walking %v; sprinting should be quicker", sprinted, walked)
	}
	// The input has to be released, or the bot stays sprinting for the session.
	if c.Input()&protocol.InputSprint != 0 {
		t.Error("the sprint input was left held after arriving")
	}
}

// Vanilla refuses to sprint below seven food, and a bot that moved at sprint
// speed anyway would be telling the server it had travelled further than
// walking allows — which comes back as a correction, not an error, and reads as
// the walk simply not working.
func TestSprintingIsRefusedWhenTooHungry(t *testing.T) {
	c, _ := settled(t)
	setPosition(c, 0, 64, 0)
	setFood(c, 6)

	err := c.SprintTo(context.Background(), 5, 64, 0)
	if err == nil {
		t.Fatal("SprintTo on six food = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "food") {
		t.Errorf("error = %q, want it to name the hunger", err)
	}
}

// Eating is 32 ticks for almost everything and a honey bottle is 40, so a hold
// sized for food released the use four ticks early and the bottle was never
// drunk. Watching the hand is the same answer for every item, including
// whichever one Mojang adds next.
func TestConsumingWaitsForTheHandRatherThanAClock(t *testing.T) {
	t.Run("returns as soon as the stack moves", func(t *testing.T) {
		c, _ := settled(t)
		slot := SlotHotbarStart
		c.inv.SetSlot(slot, ItemStack{Slot: slot, Name: "minecraft:bread", Count: 4})
		before, _ := c.HeldItem()

		// The server takes the item a moment in.
		go func() {
			time.Sleep(6 * TickRate)
			c.inv.SetSlot(slot, ItemStack{Slot: slot, Name: "minecraft:bread", Count: 3})
		}()

		began := time.Now()
		if err := c.awaitConsumed(context.Background(), before, true); err != nil {
			t.Fatalf("awaitConsumed: %v", err)
		}
		if waited := time.Since(began); waited > ConsumeDuration {
			t.Errorf("waited %v for a stack that moved after %v", waited, 6*TickRate)
		}
	})

	t.Run("waits past the food duration for a slower drink", func(t *testing.T) {
		c, _ := settled(t)
		slot := SlotHotbarStart
		c.inv.SetSlot(slot, ItemStack{Slot: slot, Name: "minecraft:honey_bottle", Count: 2})
		before, _ := c.HeldItem()

		// A honey bottle is 40 ticks, past the 32 a fixed food hold allows.
		go func() {
			time.Sleep(40 * TickRate)
			c.inv.SetSlot(slot, ItemStack{Slot: slot, Name: "minecraft:honey_bottle", Count: 1})
		}()

		began := time.Now()
		if err := c.awaitConsumed(context.Background(), before, true); err != nil {
			t.Fatalf("awaitConsumed: %v", err)
		}
		waited := time.Since(began)
		if waited < 40*TickRate {
			t.Errorf("returned after %v, before the bottle could be drunk", waited)
		}
		if waited > consumeLimit {
			t.Errorf("waited %v, past the limit", waited)
		}
	})

	t.Run("gives up so a refusal is still reported", func(t *testing.T) {
		c, _ := settled(t)
		slot := SlotHotbarStart
		c.inv.SetSlot(slot, ItemStack{Slot: slot, Name: "minecraft:bread", Count: 4})
		before, _ := c.HeldItem()

		began := time.Now()
		if err := c.awaitConsumed(context.Background(), before, true); err != nil {
			t.Fatalf("awaitConsumed: %v", err)
		}
		if waited := time.Since(began); waited < consumeLimit {
			t.Errorf("gave up after %v, want it to wait out %v first", waited, consumeLimit)
		}
	})
}
