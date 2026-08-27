package control

import (
	"context"
	"time"

	"github.com/blocktopia/understudy-client/protocol"
	"github.com/blocktopia/understudy-client/understudy"
)

// Bot is the slice of a client the control API drives.
//
// An interface rather than *understudy.Client so the handlers can be exercised
// against a stub. That is not a theoretical benefit: these handlers hold the
// argument parsing, the defaulting and the response shape for every verb, and
// none of it was reachable by a test while the only way to obtain the
// dependency was to connect to a Minecraft server.
//
// It is deliberately wide — one method per verb, mirroring the client — rather
// than an abstraction over what a bot "is". Narrowing it would mean inventing a
// vocabulary that has to be kept in step with the real one.
type Bot interface {
	// identity and state
	Username() string
	UUID() protocol.UUID
	State() protocol.State
	EntityID() int32
	Joined() bool
	Dead() bool
	Deaths() int
	// OnGround, GameMode, Effects and WhyNotDamageable answer "can this player
	// be hurt, and is it standing on anything" — the questions a scenario has
	// to be able to assert before it trusts what damage did.
	OnGround() bool
	GameMode() understudy.GameMode
	Effects() []understudy.Effect
	WhyNotDamageable() error
	Health() (health float32, food int32)
	Position() understudy.Position
	Version() *protocol.Version

	// looking
	Look(yaw, pitch float32) error
	LookDirection(name string) error
	LookYawPitch(yaw, pitch *float32) error
	LookAt(x, y, z float64) error
	LookAtBlock(x, y, z int32) error
	LookAtNearest(typeName string) (understudy.Entity, error)
	LookAtPlayer(name string) (understudy.Entity, error)
	LookingAt() (understudy.RayHit, bool)

	// moving
	MoveTo(x, y, z float64) error
	WalkTo(ctx context.Context, x, y, z float64) error
	Fall(ctx context.Context) (float64, error)
	FallTo(ctx context.Context, groundY float64) (float64, error)
	Sneak(ctx context.Context, d time.Duration) error

	// inventory
	Inventory() []understudy.ItemStack
	HeldItem() (understudy.ItemStack, bool)
	HeldSlot() int
	SetHeldSlot(slot int) error
	HoldItem(name string) (understudy.ItemStack, error)
	DropHeld(ctx context.Context, all bool) error
	EquipArmour(name string) (understudy.ItemStack, error)
	InventoryTruncated() bool
	CountItem(name string) int32
	CountItemStorage(name string) int32
	FreeStorageSlots() int
	SlotsNeeded(name string, count int32) (int, bool)
	PickupsSeen() (int32, map[string]int32)

	// world
	BlockAt(x, y, z int32) int32
	ChunkLoaded(x, z int32) bool
	LoadedChunks() int
	GroundBelow() understudy.Support
	Submerged() bool
	BlockDistance(x, y, z int32) float64
	CanReachBlock(x, y, z int32) bool

	// entities
	Entities() []understudy.Entity
	EntitiesOfType(typeName string) []understudy.Entity
	DistanceTo(e understudy.Entity) float64
	Attack(entityID int32) error
	AttackTimes(ctx context.Context, typeName string, times int) (understudy.Entity, int, error)
	InteractEntity(entityID int32) error
	InteractNearest(typeName string) (understudy.Entity, error)
	InteractAt(entityID int32, dx, dy, dz float64) error
	NearestEntity(typeName string) (understudy.Entity, error)

	// acting on the world
	Swing() error
	DigBlock(ctx context.Context, x, y, z, face int32, hold time.Duration) error
	DigBlocks(ctx context.Context, blocks [][3]int32, face int32, hold time.Duration) (int, error)
	DigLookingAt(ctx context.Context, hold time.Duration) (understudy.RayHit, error)
	PlaceBlock(ctx context.Context, x, y, z, face int32) error
	PlaceBlockVerified(ctx context.Context, x, y, z, face int32) error
	UseItem(ctx context.Context) error
	Consume(ctx context.Context) error
	ConsumeItem(ctx context.Context, name string) (understudy.ItemStack, error)
	CraftIn2x2(ctx context.Context, layout map[int]string) (understudy.ItemStack, error)

	// containers: crafting tables, smithing tables, stonecutters, villagers
	OpenContainer(ctx context.Context, x, y, z, face int32) error
	OpenContainerOnNearest(ctx context.Context, typeName string) (understudy.Entity, error)
	CloseContainer() error
	ContainerOpen() bool
	ContainerID() int32
	ContainerKind() int32
	ContainerTitle() string
	ContainerSlots() []understudy.ItemStack
	ContainerTruncated() bool
	ClickContainerSlot(slot int, button int8, mode int32) error
	TakeFromContainer(slot int) error
	ClickContainerButton(button int32) error
	CraftRecipe(recipeID int32, all bool) error
	CraftInGrid(ctx context.Context, layout map[int]string, repeat int) (understudy.ItemStack, error)
	CraftRecipeFor(ctx context.Context, name string, all bool) error
	RecipeFor(name string) (understudy.RecipeID, bool)
	KnownRecipes() int
	MissingRecipes() int
	SelectTrade(index int32) error
	Trade(ctx context.Context, index int32) (understudy.ItemStack, error)
	TradeAndTake(ctx context.Context, index int32, times int) (int, error)
	Trades() []understudy.TradeOffer
	TradeForItem(ctx context.Context, output string, times int) (int, error)

	// window shape and slot moves
	ContainerType() understudy.WindowType
	ContainerOwnSlots() int
	ContainerContents() []understudy.ItemStack
	CountInContainerOnly(name string) int32
	PutIntoSlot(ctx context.Context, name string, slot int) (understudy.ItemStack, error)
	PutOneIntoSlot(ctx context.Context, name string, slot int) (understudy.ItemStack, error)
	TakeSlot(ctx context.Context, slot int, timeout time.Duration) (understudy.ItemStack, error)
	ClearContainerInputs(ctx context.Context) error

	// storage
	Deposit(ctx context.Context, name string, count int32) (int32, error)
	Withdraw(ctx context.Context, name string, count int32) (int32, error)
	DepositAll(ctx context.Context) (int, error)

	// workstations
	Smelt(ctx context.Context, input, fuel string, count int) (understudy.ItemStack, error)
	RenameItem(ctx context.Context, item, newName string) (understudy.ItemStack, error)
	CombineInAnvil(ctx context.Context, first, second string) (understudy.ItemStack, error)
	ApplyBannerPattern(ctx context.Context, banner, dye, patternItem string, pattern int32) (understudy.ItemStack, error)
	Disenchant(ctx context.Context, item string) (understudy.ItemStack, error)
	UpgradeInSmithingTable(ctx context.Context, template, base, addition string) (understudy.ItemStack, error)
	Enchant(ctx context.Context, item string, level int32) (understudy.ItemStack, error)
	Brew(ctx context.Context, bottle, ingredient, fuel string, count int) error
	ActivateBeacon(ctx context.Context, payment string, primary, secondary int32) error
	ApplyToMap(ctx context.Context, mapItem, applied string) (understudy.ItemStack, error)
	ShootAt(ctx context.Context, x, y, z float64, draw time.Duration) error
	ShootBlock(ctx context.Context, x, y, z int32, draw time.Duration) error
	ShootNearest(ctx context.Context, typeName string, draw time.Duration) (understudy.Entity, error)
}

// A *understudy.Client must satisfy Bot. Checked at compile time so a signature
// change in the client surfaces here rather than at the one call site in main.
var _ Bot = (*understudy.Client)(nil)
