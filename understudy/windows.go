package understudy

// WindowType identifies the kind of UI the server opened. The file also carries
// the slot layouts each of those windows uses.
//
// # Where these numbers come from
//
// They are not in minecraft-data — there is no windows.json — so every value
// here was read off a live 26.1.2 server by opening the block and recording
// what it reported. They are documentation of an observation, not a guess, and
// the layout tests re-derive the sizes from the same source.
//
// # Do not branch on WindowType
//
// It is here so a caller can *report* what it opened, not so the client can
// decide behaviour from it. Sizes and layouts are read from the window itself:
// a double chest is 54 own-slots where a single is 27, a copper chest is the
// same type ID as an oak one, and a chest minecart is a chest that happens to
// be an entity. Deriving from the window covers all of that for free, and
// hard-coding per type would need a new case for each variant Mojang adds.
//
// # Blocks that look like containers and are not
//
// fletching_table, composter and an *empty* lectern open nothing at all — they
// are decorative or interacted with directly — so OpenContainer times out on
// them. That is the correct answer, not a bug. A lectern with a book opens
// type 17 with zero slots, being a reader rather than a container.
//
// # The layout rule
//
// Every container window is [the container's own slots][the player's 36].
// So the player's inventory always begins at Size()-PlayerWindowSlots, and a
// container's own slot count is Size()-PlayerWindowSlots. Nothing else about a
// window's shape is knowable from the protocol.
type WindowType int32

// Window type IDs, as reported by 26.1.2.
const (
	WindowGeneric9x1   WindowType = 0
	WindowGeneric9x3   WindowType = 2 // chest, barrel, ender chest, copper chest, chest minecart
	WindowGeneric3x3   WindowType = 6 // dispenser, dropper
	WindowCrafter      WindowType = 7 // the 3x3 auto-crafter
	WindowAnvil        WindowType = 8 // "container.repair"
	WindowBeacon       WindowType = 9
	WindowBlastFurnace WindowType = 10
	WindowBrewingStand WindowType = 11
	WindowCrafting     WindowType = 12 // crafting table
	WindowEnchantment  WindowType = 13
	WindowFurnace      WindowType = 14
	WindowGrindstone   WindowType = 15
	WindowHopper       WindowType = 16 // hopper, hopper minecart
	WindowLectern      WindowType = 17 // a lectern holding a book; carries no slots
	WindowLoom         WindowType = 18
	WindowMerchant     WindowType = 19 // villager, wandering trader
	WindowShulkerBox   WindowType = 20
	WindowSmithing     WindowType = 21 // "container.upgrade"
	WindowSmoker       WindowType = 22
	WindowCartography  WindowType = 23
	WindowStonecutter  WindowType = 24
)

// PlayerWindowSlots is how many of the player's own slots the server appends to
// every container window: 27 storage plus the 9 hotbar.
//
// Armour and the offhand are *not* included, which is why a container window is
// 36 larger than its own contents rather than 41.
const PlayerWindowSlots = 36

// Slot layouts, by window.
//
// Slot numbering restarts per window type and cannot be derived from the
// packets — the server sends a flat array. Getting one wrong is silent: the
// click lands somewhere else and the operation simply does not happen.
const (
	// Furnace, blast furnace and smoker share a layout.
	FurnaceInputSlot  = 0
	FurnaceFuelSlot   = 1
	FurnaceResultSlot = 2

	// Anvil: two inputs and the result. Renaming needs only the first.
	AnvilFirstSlot  = 0
	AnvilSecondSlot = 1
	AnvilResultSlot = 2

	// Loom: the banner, the dye, an optional banner-pattern item, and the
	// result. The pattern itself is chosen with ClickContainerButton.
	LoomBannerSlot  = 0
	LoomDyeSlot     = 1
	LoomPatternSlot = 2
	LoomResultSlot  = 3

	// Grindstone: two inputs, one result. Disenchanting uses the first only.
	GrindstoneFirstSlot  = 0
	GrindstoneSecondSlot = 1
	GrindstoneResultSlot = 2

	// Cartography: a map and the paper/glass/compass applied to it.
	CartographyMapSlot    = 0
	CartographyPaperSlot  = 1
	CartographyResultSlot = 2

	// Enchanting: the item and the lapis. The level is a container button.
	EnchantItemSlot  = 0
	EnchantLapisSlot = 1

	// Brewing: three bottle slots, the ingredient above them, and the fuel.
	BrewBottleSlot1    = 0
	BrewBottleSlot2    = 1
	BrewBottleSlot3    = 2
	BrewIngredientSlot = 3
	BrewFuelSlot       = 4

	// Beacon has a single payment slot.
	BeaconPaymentSlot = 0
)

// String names the window type for logs and errors.
func (w WindowType) String() string {
	switch w {
	case WindowGeneric9x1:
		return "generic 9x1"
	case WindowGeneric9x3:
		return "chest"
	case WindowGeneric3x3:
		return "dispenser"
	case WindowCrafter:
		return "crafter"
	case WindowAnvil:
		return "anvil"
	case WindowBeacon:
		return "beacon"
	case WindowBlastFurnace:
		return "blast furnace"
	case WindowBrewingStand:
		return "brewing stand"
	case WindowCrafting:
		return "crafting table"
	case WindowEnchantment:
		return "enchanting table"
	case WindowFurnace:
		return "furnace"
	case WindowGrindstone:
		return "grindstone"
	case WindowHopper:
		return "hopper"
	case WindowLectern:
		return "lectern"
	case WindowLoom:
		return "loom"
	case WindowMerchant:
		return "merchant"
	case WindowShulkerBox:
		return "shulker box"
	case WindowSmithing:
		return "smithing table"
	case WindowSmoker:
		return "smoker"
	case WindowCartography:
		return "cartography table"
	case WindowStonecutter:
		return "stonecutter"
	}
	return "window type " + itoa(int32(w))
}

// ContainerType returns the open window's type.
func (c *Client) ContainerType() WindowType { return WindowType(c.window.Kind()) }

// ContainerOwnSlots returns how many slots belong to the container itself,
// excluding the player's inventory the server appends.
//
// This is the number to trust rather than any constant: it is 27 for a single
// chest and 54 for a double, 5 for a hopper, 27 for a chest minecart. Derived
// from the window, so every variant works without a case for it.
func (c *Client) ContainerOwnSlots() int {
	size := c.window.Size()
	if size <= PlayerWindowSlots {
		return 0
	}
	return size - PlayerWindowSlots
}

// PlayerSlotsStart returns the first slot of the player's own inventory within
// the open window. Ingredients must be taken from at or above this.
func (c *Client) PlayerSlotsStart() int { return c.ContainerOwnSlots() }

// itoa avoids pulling strconv in for one error path.
func itoa(v int32) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
