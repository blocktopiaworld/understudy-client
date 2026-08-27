package protocol

import (
	"strconv"
	"sync"
)

// PacketIDs holds every packet ID this client uses, for one protocol version.
//
// IDs are dense indices that shift whenever Mojang inserts a packet, so they
// cannot be constants. A field of -1 means the packet does not exist in that
// version; since real IDs are non-negative, an absent packet simply never
// matches a dispatch and never gets sent.
type PacketIDs struct {
	SBHandshake int32

	SBLoginStart           int32
	SBLoginAcknowledged    int32
	CBLoginDisconnect      int32
	CBLoginEncryptionBegin int32
	CBLoginSuccess         int32
	CBLoginCompress        int32

	SBConfigSettings            int32
	SBConfigFinishConfiguration int32
	SBConfigKeepAlive           int32
	SBConfigPong                int32
	SBConfigSelectKnownPacks    int32
	SBConfigAcceptCodeOfConduct int32
	CBConfigDisconnect          int32
	CBConfigFinishConfiguration int32
	CBConfigKeepAlive           int32
	CBConfigPing                int32
	CBConfigSelectKnownPacks    int32
	CBConfigCodeOfConduct       int32

	SBPlayTeleportConfirm    int32
	SBPlayAttack             int32
	SBPlayChatMessage        int32
	SBPlayClientCommand      int32
	SBPlayUseEntity          int32
	SBPlayUseItem            int32
	SBPlayKeepAlive          int32
	SBPlayPosition           int32
	SBPlayPositionLook       int32
	SBPlayLook               int32
	SBPlayBlockDig           int32
	SBPlayHeldItemSlot       int32
	SBPlayArmAnimation       int32
	SBPlayBlockPlace         int32
	SBPlayWindowClick        int32
	SBPlayCloseWindow        int32
	SBPlaySelectTrade        int32
	SBPlayCraftRecipeRequest int32
	SBPlayContainerButton    int32
	SBPlayNameItem           int32
	SBPlaySetBeaconEffect    int32
	SBPlaySetCreativeSlot    int32
	SBPlayPlayerInput        int32
	SBPlayEntityAction       int32
	SBPlayChunkBatchReceived int32
	SBPlayPlayerLoaded       int32

	CBPlaySpawnEntity        int32
	CBPlayBlockChange        int32
	CBPlayKickDisconnect     int32
	CBPlayUnloadChunk        int32
	CBPlayKeepAlive          int32
	CBPlayMapChunk           int32
	CBPlayLogin              int32
	CBPlayRelEntityMove      int32
	CBPlayEntityMoveLook     int32
	CBPlayEntityTeleport     int32
	CBPlayDeathCombatEvent   int32
	CBPlayPosition           int32
	CBPlayEntityDestroy      int32
	CBPlayMultiBlockChange   int32
	CBPlayRespawn            int32
	CBPlayUpdateHealth       int32
	CBPlayWindowItems        int32
	CBPlaySetSlot            int32
	CBPlayOpenWindow         int32
	CBPlayCloseWindow        int32
	CBPlayTradeList          int32
	CBPlayRecipeBookAdd      int32
	CBPlayHeldItemSlot       int32
	CBPlayCollect            int32
	CBPlayChunkBatchStart    int32
	CBPlayChunkBatchFinished int32
}

// Absent is the ID of a packet a version does not have.
const Absent int32 = -1

// ChunkFormat captures the parts of the chunk encoding that changed between
// versions. These are the three that actually bite:
//
//   - HasSizePrefix: before 1.21.5 each paletted container carried a VarInt
//     count of longs. From 1.21.5 it is computed instead, saving a byte.
//   - HasFluidCount: from 26.1 each section carries a second int16 after the
//     solid block count.
//   - NBTHeightmaps: before 1.21.5 the heightmaps in a chunk packet are a
//     single (nameless) NBT compound. From 1.21.5 they are a prefixed array of
//     {VarInt type, prefixed array of long}. Nothing here reads heightmaps,
//     but they sit between the coordinates and the chunk data, so walking them
//     with the wrong shape puts the data blob at the wrong offset.
//
// All three are invisible until they are wrong, and then they surface as a
// short read several sections downstream — nowhere near the actual mistake.
// The 1.21.5 chunk rework moved two of them at once, which is why
// HasSizePrefix and NBTHeightmaps share a threshold.
type ChunkFormat struct {
	HasFluidCount bool
	HasSizePrefix bool
	NBTHeightmaps bool
}

// Version is everything this client needs to know that varies between
// Minecraft versions.
//
// A Version is immutable once registered and is shared by every Client
// speaking that version, so all its methods are safe for concurrent use.
type Version struct {
	Name     string
	Protocol int32
	Chunk    ChunkFormat
	Packets  PacketIDs

	// componentIDs maps this version's data component wire ids to the canonical
	// ids the decoder switches on. Nil means the version's ids have never been
	// established.
	componentIDs map[int32]int32

	// components describes how this version encodes component payloads, which
	// is a separate question from which id is which. Knowing the ids is not
	// enough: 1.21.11 writes an item nested in a component count-first where
	// 26.1 writes it id-first, and so on. Zero value means unknown, and an
	// unknown encoding cannot be read at all.
	components      ComponentEncoding
	knowsComponents bool

	entityNames []string
	itemNames   []string
	itemStacks  []int32
	shapes      [][]Box
	shapeRuns   [][3]int32
	solidStates [][2]int32
	waterStates [][2]int32
	lavaStates  [][2]int32
	airStates   [][2]int32

	// itemIDs indexes itemNames in reverse, built on first use. The tables run
	// to a few thousand entries and StackSizeOf is called per item per
	// scenario; scanning the slice each time made a name lookup O(items).
	itemIDsOnce sync.Once
	itemIDs     map[string]int32
}

// VersionSpec describes a protocol table to build.
//
// It exists because a Version's tables are unexported — which is right, they
// are an implementation detail of the lookups — but that also meant only the
// generated files, being in this package, could construct one. Anything
// wanting a small synthetic version (a test, a tool) had no way to make it.
type VersionSpec struct {
	Name     string
	Protocol int32
	Chunk    ChunkFormat
	Packets  PacketIDs

	// Components describes how this version encodes component payloads. Leave
	// nil for a version whose encodings have not been checked; setting it
	// wrongly desynchronises windows silently.
	Components *ComponentEncoding

	// ComponentIDs maps this version's data component wire ids to canonical
	// ids. Generated by internal/gen/gencomponents.mjs from the server's own
	// registries report; leave nil for a version whose ids are unknown.
	//
	// They are dense registry indices, like packet ids, and shift whenever
	// Mojang inserts a component — between 1.21.4 and 26.1 only five of
	// sixty-seven kept their number. Unlike packet ids they are in no published
	// dataset, which is why they come from the server rather than from
	// minecraft-data.
	ComponentIDs map[int32]int32

	// EntityNames and ItemNames are indexed by wire ID; an empty string means
	// the ID is unused in this version.
	EntityNames []string
	ItemNames   []string
	// ItemStacks is indexed by wire ID. A non-positive entry means the default.
	ItemStacks []int32

	// Shapes holds the distinct collision shapes, indexed by ShapeRuns.
	Shapes [][]Box
	// ShapeRuns maps block-state ranges to a shape, as sorted {lo, hi, shape}.
	ShapeRuns [][3]int32

	// Block-state classification, as sorted inclusive [lo, hi] ranges.
	Solid [][2]int32
	Water [][2]int32
	Lava  [][2]int32
	Air   [][2]int32
}

// NewVersion builds a Version from a spec. It does not register it; call
// Register for that.
func NewVersion(spec VersionSpec) *Version {
	return &Version{
		Name:            spec.Name,
		Protocol:        spec.Protocol,
		Chunk:           spec.Chunk,
		Packets:         spec.Packets,
		componentIDs:    spec.ComponentIDs,
		knowsComponents: spec.Components != nil,
		components: func() ComponentEncoding {
			if spec.Components == nil {
				return ComponentEncoding{}
			}
			return *spec.Components
		}(),
		entityNames: spec.EntityNames,
		itemNames:   spec.ItemNames,
		itemStacks:  spec.ItemStacks,
		shapes:      spec.Shapes,
		shapeRuns:   spec.ShapeRuns,
		solidStates: spec.Solid,
		waterStates: spec.Water,
		lavaStates:  spec.Lava,
		airStates:   spec.Air,
	}
}

// SupportsAttackPacket reports whether the version has a dedicated attack
// packet. Versions before 26.1 fold attacking into use_entity with a mode
// field, which this client does not implement — so attacking is unavailable
// there rather than silently doing nothing.
func (v *Version) SupportsAttackPacket() bool { return v.Packets.SBPlayAttack != Absent }

// EntityTypeName resolves a wire entity type ID to its namespaced name.
func (v *Version) EntityTypeName(id int32) string {
	if id < 0 || int(id) >= len(v.entityNames) || v.entityNames[id] == "" {
		return unknownName(id)
	}
	return v.entityNames[id]
}

// ItemName resolves a wire item ID to its namespaced name.
func (v *Version) ItemName(id int32) string {
	if id < 0 || int(id) >= len(v.itemNames) || v.itemNames[id] == "" {
		return unknownName(id)
	}
	return v.itemNames[id]
}

// StackSize returns how many of an item fit in one slot.
//
// This is not cosmetic for goal feasibility: totems stack to 1, so "hold 5
// totems" needs five whole slots, while "hold 2304 dirt" needs exactly 36 —
// every storage slot a player has.
func (v *Version) StackSize(id int32) int32 {
	if id < 0 || int(id) >= len(v.itemStacks) || v.itemStacks[id] <= 0 {
		return DefaultStackSize
	}
	return v.itemStacks[id]
}

// ItemID resolves a namespaced item name to its wire ID. A bare name is
// assumed to be in the minecraft namespace.
func (v *Version) ItemID(name string) (int32, bool) {
	v.itemIDsOnce.Do(func() {
		v.itemIDs = make(map[string]int32, len(v.itemNames))
		for id, n := range v.itemNames {
			if n != "" {
				v.itemIDs[n] = int32(id)
			}
		}
	})
	id, ok := v.itemIDs[Namespaced(name)]
	return id, ok
}

// StackSizeOf returns the stack size for an item name, defaulting to 64 for
// anything unrecognised.
func (v *Version) StackSizeOf(name string) int32 {
	id, ok := v.ItemID(name)
	if !ok {
		return DefaultStackSize
	}
	return v.StackSize(id)
}

// IsSolid reports whether a block state blocks movement.
func (v *Version) IsSolid(state int32) bool { return inRanges(v.solidStates, state) }

// IsWater reports whether a block state is water or a bubble column.
//
// Water is emphatically not just "non-solid air": it cancels fall damage
// completely and drowns anything that stays submerged. Treating it as empty is
// how a bot falls into a lake like a stone and dies at the bottom.
func (v *Version) IsWater(state int32) bool { return inRanges(v.waterStates, state) }

// IsLava reports whether a block state is lava.
func (v *Version) IsLava(state int32) bool { return inRanges(v.lavaStates, state) }

// IsFluid reports whether a block state is a liquid.
func (v *Version) IsFluid(state int32) bool { return v.IsWater(state) || v.IsLava(state) }

// IsAir reports whether a block state is any kind of air.
//
// Not just state 0: cave_air and void_air are distinct states with their own
// IDs, and underground they are the overwhelming majority. Treating only
// state 0 as air makes a crosshair ray stop dead on the first cave air block
// it meets.
func (v *Version) IsAir(state int32) bool { return inRanges(v.airStates, state) }

// IsTargetable reports whether the crosshair would stop on a block.
//
// This is deliberately NOT IsSolid. Vanilla raycasts the block's *outline*
// shape, not its collision shape, and the two differ for exactly the blocks a
// test suite cares about: cobweb, crops, torches and flowers are all
// walk-through, so a collision-based ray passes straight through them and
// reports an empty line of sight to something standing right in front of the
// crosshair. Fluids are excluded because a block can be targeted through
// water.
func (v *Version) IsTargetable(state int32) bool {
	return !v.IsAir(state) && !v.IsFluid(state)
}

// AirState is the block state ID for air, which is 0 in every version.
const AirState int32 = 0

// inRanges reports whether state falls within any of the sorted, merged ranges.
//
// The generated tables hold a few hundred ranges rather than tens of thousands
// of per-state entries, and a binary search over them is fast enough to call
// for every block scanned during a fall.
func inRanges(ranges [][2]int32, state int32) bool {
	lo, hi := 0, len(ranges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case state < ranges[mid][0]:
			hi = mid - 1
		case state > ranges[mid][1]:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// unknownName labels a wire ID the generated table does not cover, which
// happens on a modded server or a version whose table is a release behind.
// It is deliberately still a valid namespaced name so it flows through the
// same matching as any other.
func unknownName(id int32) string {
	return DefaultNamespace + ":unknown/" + strconv.FormatInt(int64(id), 10)
}

// ComponentKind translates a data component's wire id into the canonical id the
// decoder recognises, reporting whether this version has an id for it.
//
// The translation matters because the ids are per-version registry indices. On
// 1.21.11 an attribute_modifiers component arrives as 13 and on 26.1 as 16, and
// nothing in the payload says which was meant — so reading one as the other
// does not fail, it consumes the wrong number of bytes and desynchronises
// everything after it.
func (v *Version) ComponentKind(wire int32) (int32, bool) {
	kind, ok := v.componentIDs[wire]
	return kind, ok
}

// HasComponentIDs reports whether this version's component ids are known at all.
func (v *Version) HasComponentIDs() bool { return len(v.componentIDs) > 0 }

// ComponentEncoding describes the ways component payloads differ between
// versions, as measured rather than as guessed.
//
// Every field is false for 26.1 and true for 1.21.11 and 1.21.4. They were
// found by putting the same items on servers of each version and comparing the
// bytes: of sixty-seven components sampled on both, fifty-one are byte for byte
// identical and the rest fall into these three groups.
type ComponentEncoding struct {
	// NestedStacksCountFirst: an item stack held inside a component leads with
	// its count rather than its id, and an empty container slot is a zero count
	// rather than an absent optional. Affects container, charged_projectiles,
	// bundle_contents and use_remainder.
	NestedStacksCountFirst bool

	// TagsAreBareStrings: a component whose value is a tag writes the tag name
	// on its own, where 26.1 wraps it in a holder set — a count of zero
	// followed by the name. Affects damage_resistant and
	// provides_banner_patterns.
	TagsAreBareStrings bool

	// RegistryRefsHaveLeadingFlag: six components write a byte before their
	// registry reference, which is 1 in every sample seen. Affects damage_type,
	// instrument, jukebox_playable, provides_trim_material, chicken/variant and
	// zombie_nautilus/variant — and no other reference, so break_sound, trim
	// and banner_patterns are untouched.
	RegistryRefsHaveLeadingFlag bool
}

// ComponentEncoding reports how this version encodes component payloads, and
// whether that is known at all.
//
// Separate from HasComponentIDs because the two are independent. 1.21.11's ids
// are known — generated from its own registries report — and its payloads still
// differ from 26.1's, so knowing which id is which buys nothing on its own.
func (v *Version) ComponentEncoding() (ComponentEncoding, bool) {
	return v.components, v.knowsComponents
}
