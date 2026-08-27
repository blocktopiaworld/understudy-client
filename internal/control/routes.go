package control

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/blocktopia/understudy-client/protocol"
	"github.com/blocktopia/understudy-client/understudy"
)

// Defaults for the optional fields the JSON API exposes.
const (
	defaultDigHold  = 400 * time.Millisecond
	defaultSneakMS  = 1000
	defaultDigFace  = protocol.FaceTop
	defaultAttempts = 1
)

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Read-only.
	mux.HandleFunc("GET /state", s.handleState)
	mux.HandleFunc("GET /inventory", s.handleInventory)
	mux.HandleFunc("GET /block", s.handleBlock)
	mux.HandleFunc("GET /ground", s.handleGround)
	mux.HandleFunc("GET /reach", s.handleReach)
	mux.HandleFunc("GET /lookingat", s.handleLookingAt)
	mux.HandleFunc("GET /entities", s.handleEntities)

	// Actions.
	mux.Handle("POST /look", handle(s, s.look))
	mux.Handle("POST /lookat", handle(s, s.lookAt))
	mux.Handle("POST /move", handle(s, s.move))
	mux.Handle("POST /walk", handle(s, s.walk))
	mux.Handle("POST /fall", handle(s, s.fall))
	mux.Handle("POST /slot", handle(s, s.slot))
	mux.Handle("POST /hold", handle(s, s.hold))
	mux.Handle("POST /drop", handle(s, s.drop))
	mux.Handle("POST /sneak", handle(s, s.sneak))
	mux.Handle("POST /equip", handle(s, s.equip))
	mux.Handle("POST /interact", handle(s, s.interact))
	mux.Handle("POST /interactat", handle(s, s.interactAt))
	mux.Handle("POST /consume", handle(s, s.consume))
	mux.Handle("POST /shoot", handle(s, s.shoot))
	mux.Handle("POST /craft", handle(s, s.craft))
	mux.Handle("POST /swing", handle(s, s.swing))
	mux.Handle("POST /diglook", handle(s, s.digLookingAt))
	mux.Handle("POST /attack", handle(s, s.attack))
	mux.Handle("POST /dig", handle(s, s.dig))
	mux.Handle("POST /place", handle(s, s.place))
	mux.Handle("POST /use", handle(s, s.use))

	// Container UIs: crafting tables, smithing tables, stonecutters, villagers.
	mux.HandleFunc("GET /container", s.handleContainer)
	mux.HandleFunc("GET /trades", s.handleTrades)
	mux.HandleFunc("GET /recipes", s.handleRecipes)
	mux.Handle("POST /container/open", handle(s, s.containerOpen))
	mux.Handle("POST /container/close", handle(s, s.containerClose))
	mux.Handle("POST /container/click", handle(s, s.containerClick))
	mux.Handle("POST /container/take", handle(s, s.containerTake))
	mux.Handle("POST /container/button", handle(s, s.containerButton))
	mux.Handle("POST /container/craft", handle(s, s.containerCraft))
	mux.Handle("POST /container/grid", handle(s, s.containerGrid))
	mux.Handle("POST /container/put", handle(s, s.containerPut))
	mux.Handle("POST /container/clear", handle(s, s.containerClear))
	mux.Handle("POST /container/deposit", handle(s, s.containerDeposit))
	mux.Handle("POST /container/withdraw", handle(s, s.containerWithdraw))

	// Workstations. Each is its window's layout, so the caller names items
	// rather than slot numbers.
	mux.Handle("POST /smelt", handle(s, s.smelt))
	mux.Handle("POST /rename", handle(s, s.rename))
	mux.Handle("POST /anvil", handle(s, s.anvilCombine))
	mux.Handle("POST /loom", handle(s, s.loom))
	mux.Handle("POST /grindstone", handle(s, s.grindstone))
	mux.Handle("POST /smith", handle(s, s.smith))
	mux.Handle("POST /enchant", handle(s, s.enchant))
	mux.Handle("POST /brew", handle(s, s.brew))
	mux.Handle("POST /beacon", handle(s, s.beacon))
	mux.Handle("POST /cartography", handle(s, s.cartography))
	mux.Handle("POST /container/trade", handle(s, s.containerTrade))
	return mux
}

// --- read-only ---------------------------------------------------------------

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	health, food := s.bot.Health()
	out := s.position()
	out["username"] = s.bot.Username()
	out["uuid"] = s.bot.UUID().String()
	out["state"] = s.bot.State().String()
	out["entity_id"] = s.bot.EntityID()
	out["joined"] = s.bot.Joined()
	out["dead"] = s.bot.Dead()
	out["deaths"] = s.bot.Deaths()
	out["health"] = health
	out["food"] = food
	out["held_slot"] = s.bot.HeldSlot()
	// The state that explains why damage did or did not land, and why a server
	// might call a bot a flyer. All three were invisible from here, which is
	// how a working totem of undying was reported as broken: the player was in
	// creative, and nothing said so.
	out["on_ground"] = s.bot.OnGround()
	out["game_mode"] = s.bot.GameMode().String()
	out["effects"] = s.bot.Effects()
	if err := s.bot.WhyNotDamageable(); err != nil {
		out["damageable"] = false
		out["not_damageable_because"] = err.Error()
	} else {
		out["damageable"] = true
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleInventory lists the slots the bot knows about.
//
// ?count=dirt reports both totals. They differ whenever the item sits in the
// offhand or armour — see CountItemStorage for why both are worth having.
func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	if name := r.URL.Query().Get("count"); name != "" {
		s.inventoryCount(w, r, name)
		return
	}
	held, _ := s.bot.HeldItem()
	total, pickups := s.bot.PickupsSeen()
	s.writeJSON(w, http.StatusOK, body{
		"items":     s.bot.Inventory(),
		"held_slot": s.bot.HeldSlot(),
		"held_item": held.Name,
		// Surfaced rather than hidden: a partial view is why a "missing" item
		// might not actually be missing.
		"truncated": s.bot.InventoryTruncated(),
		"pickups":   pickups,
		"picked_up": total,
	})
}

func (s *Server) inventoryCount(w http.ResponseWriter, r *http.Request, name string) {
	// ?want= asks whether a quantity would physically fit. Previously this
	// reported `fits_in_36` from SlotsNeeded(name, 1), which is trivially true
	// for every item in the game and answered nothing.
	want := int32(1)
	if raw := r.URL.Query().Get("want"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || v < 0 {
			s.badRequest(w, invalidf("bad want %q: expected a non-negative count", raw))
			return
		}
		want = int32(v)
	}
	slots, fits := s.bot.SlotsNeeded(name, want)
	s.writeJSON(w, http.StatusOK, body{
		"item":         name,
		"total":        s.bot.CountItem(name),
		"storage_only": s.bot.CountItemStorage(name),
		"free_slots":   s.bot.FreeStorageSlots(),
		"stack_size":   s.bot.Version().StackSizeOf(name),
		"want":         want,
		"slots_needed": slots,
		"fits":         fits,
	})
}

// handleBlock reports the block the bot believes is at a coordinate:
// GET /block?x=-300&y=76&z=-310
//
// `loaded` matters as much as the state does. An unloaded chunk reads as air
// everywhere, so "air" is only meaningful when the terrain is actually known.
func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	x, y, z, err := blockCoords(r)
	if err != nil {
		s.badRequest(w, err)
		return
	}
	state := s.bot.BlockAt(x, y, z)
	v := s.bot.Version()
	s.writeJSON(w, http.StatusOK, body{
		"x": x, "y": y, "z": z,
		"state":      state,
		"loaded":     s.bot.ChunkLoaded(x, z),
		"solid":      v.IsSolid(state),
		"water":      v.IsWater(state),
		"lava":       v.IsLava(state),
		"air":        v.IsAir(state),
		"targetable": v.IsTargetable(state),
	})
}

// handleGround reports what the bot is standing over.
func (s *Server) handleGround(w http.ResponseWriter, _ *http.Request) {
	support := s.bot.GroundBelow()
	pos := s.bot.Position()
	s.writeJSON(w, http.StatusOK, body{
		// known distinguishes "the column is not loaded" from "loaded, and
		// nothing is under the bot". Reporting only `found` made those one
		// answer, which is the reading that gets a bot kicked for floating.
		"known": support.Known,
		"found": support.Found, "ground_y": support.GroundY,
		"in_water": support.InWater, "in_lava": support.InLava,
		"submerged": s.bot.Submerged(),
		"y":         pos.Y,
		"gap":       pos.Y - support.GroundY,
		"chunks":    s.bot.LoadedChunks(),
	})
}

// handleReach reports whether a block is workable from where the bot stands:
// GET /reach?x=..&y=..&z=..
func (s *Server) handleReach(w http.ResponseWriter, r *http.Request) {
	x, y, z, err := blockCoords(r)
	if err != nil {
		s.badRequest(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, body{
		"distance": s.bot.BlockDistance(x, y, z),
		"reach":    understudy.BlockReach,
		"can":      s.bot.CanReachBlock(x, y, z),
	})
}

// handleLookingAt reports the block under the crosshair — the block the game
// itself would target, found by ray-tracing rather than named by coordinate.
func (s *Server) handleLookingAt(w http.ResponseWriter, _ *http.Request) {
	hit, ok := s.bot.LookingAt()
	if !ok {
		s.writeJSON(w, http.StatusOK, body{"hit": false})
		return
	}
	s.writeJSON(w, http.StatusOK, body{
		"hit": true, "x": hit.X, "y": hit.Y, "z": hit.Z,
		"face": hit.Face, "distance": hit.Distance, "state": hit.State,
	})
}

// handleEntities lists tracked entities, nearest first. Optional ?type= filters
// by name and ?radius= by distance.
func (s *Server) handleEntities(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list := s.bot.Entities()
	if t := q.Get("type"); t != "" {
		list = s.bot.EntitiesOfType(t)
	}
	if raw := q.Get("radius"); raw != "" {
		radius, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			s.badRequest(w, invalidf("bad radius %q", raw))
			return
		}
		kept := make([]understudy.Entity, 0, len(list))
		for _, e := range list {
			if s.bot.DistanceTo(e) <= radius {
				kept = append(kept, e)
			}
		}
		list = kept
	}

	out := make([]body, 0, len(list))
	for _, e := range list {
		out = append(out, body{
			"id": e.ID, "type_name": e.TypeName,
			"x": e.X, "y": e.Y, "z": e.Z,
			"distance": s.bot.DistanceTo(e),
		})
	}
	s.writeJSON(w, http.StatusOK, body{"count": len(out), "entities": out})
}

// --- actions -----------------------------------------------------------------

type blockRef struct{ X, Y, Z int32 }

// lookRequest is the one place aiming is controlled, and it accepts every form
// a caller might reasonably have to hand — mirroring Carpet's
// `/player <name> look`:
//
//	{"direction":"north"}              named direction
//	{"yaw":90}                         either axis alone; the other is kept
//	{"yaw":90,"pitch":-20}             absolute rotation
//	{"x":10,"y":64,"z":-5}             an exact point
//	{"block":{"x":10,"y":64,"z":-5}}   a block, aimed at its centre
//	{"entity_type":"chicken"}          the nearest entity of a type
//	{"player":"Someone"}               a named player, aimed at eye height
type lookRequest struct {
	Direction  string    `json:"direction"`
	Yaw        *float32  `json:"yaw"`
	Pitch      *float32  `json:"pitch"`
	X          *float64  `json:"x"`
	Y          *float64  `json:"y"`
	Z          *float64  `json:"z"`
	Block      *blockRef `json:"block"`
	EntityType string    `json:"entity_type"`
	Player     string    `json:"player"`
}

// look checks the forms most-specific first, so a body carrying several does
// something predictable rather than depending on field order.
func (s *Server) look(_ context.Context, in lookRequest) (body, error) {
	targeted := func(e understudy.Entity, err error) (body, error) {
		if err != nil {
			return nil, err
		}
		return body{"target_id": e.ID, "target_type": e.TypeName}, nil
	}
	switch {
	case in.Direction != "":
		return nil, s.bot.LookDirection(in.Direction)
	case in.Player != "":
		return targeted(s.bot.LookAtPlayer(in.Player))
	case in.EntityType != "":
		return targeted(s.bot.LookAtNearest(in.EntityType))
	case in.Block != nil:
		return nil, s.bot.LookAtBlock(in.Block.X, in.Block.Y, in.Block.Z)
	case in.X != nil && in.Y != nil && in.Z != nil:
		return nil, s.bot.LookAt(*in.X, *in.Y, *in.Z)
	case in.Yaw != nil || in.Pitch != nil:
		return nil, s.bot.LookYawPitch(in.Yaw, in.Pitch)
	}
	return nil, invalidf(
		"look: need one of direction, yaw/pitch, x/y/z, block, entity_type or player (directions: %s)",
		strings.Join(understudy.DirectionNames(), ", "))
}

type pointRequest struct{ X, Y, Z float64 }

func (s *Server) lookAt(_ context.Context, in pointRequest) (body, error) {
	return nil, s.bot.LookAt(in.X, in.Y, in.Z)
}

func (s *Server) move(_ context.Context, in pointRequest) (body, error) {
	return nil, s.bot.MoveTo(in.X, in.Y, in.Z)
}

func (s *Server) walk(ctx context.Context, in pointRequest) (body, error) {
	return nil, s.bot.WalkTo(ctx, in.X, in.Y, in.Z)
}

// fall drops the bot to a known floor height, taking real fall damage.
//
//	{}             fall until the bot lands, finding the ground itself
//	{"to_y": 77}   stop at a known height instead
//
// The no-argument form is the safe one: the ground is detected from the
// server's own position corrections, so it cannot leave the bot hovering.
func (s *Server) fall(ctx context.Context, in struct {
	ToY *float64 `json:"to_y"`
}) (body, error) {
	before, _ := s.bot.Health()
	deathsBefore := s.bot.Deaths()

	var blocks float64
	var err error
	if in.ToY != nil {
		blocks, err = s.bot.FallTo(ctx, *in.ToY)
	} else {
		blocks, err = s.bot.Fall(ctx)
	}
	if err != nil {
		return nil, err
	}
	// Damage lands a tick or two after the landing packet, so give the server a
	// moment before reporting health — otherwise every fall looks harmless.
	if err := settle(ctx); err != nil {
		return nil, err
	}
	after, _ := s.bot.Health()

	out := body{
		"fell_blocks":   blocks,
		"health_before": before,
		"health_after":  after,
		"deaths":        s.bot.Deaths(),
	}
	// A fatal fall respawns the bot at full health, so comparing health across
	// it reports a negative "damage". Report the kill instead — the subtraction
	// is only meaningful when the same life survived it.
	if s.bot.Deaths() > deathsBefore {
		out["fatal"] = true
	} else {
		out["fatal"] = false
		out["damage"] = before - after
	}
	return out, nil
}

func (s *Server) slot(_ context.Context, in struct {
	Slot int `json:"slot"`
}) (body, error) {
	return nil, s.bot.SetHeldSlot(in.Slot)
}

// hold puts a named item into the bot's hand, from anywhere in the inventory.
func (s *Server) hold(_ context.Context, in struct {
	Item string `json:"item"`
}) (body, error) {
	if in.Item == "" {
		return nil, invalidf("hold: item is required")
	}
	item, err := s.bot.HoldItem(in.Item)
	if err != nil {
		return nil, err
	}
	held, _ := s.bot.HeldItem()
	return body{
		"found_in_slot": item.Slot,
		"held_slot":     s.bot.HeldSlot(),
		"held_item":     held.Name,
	}, nil
}

func (s *Server) drop(ctx context.Context, in struct {
	All bool `json:"all"`
}) (body, error) {
	return nil, s.bot.DropHeld(ctx, in.All)
}

// sneak holds sneak for a duration, since sneak_time only accrues while
// actually sneaking.
func (s *Server) sneak(ctx context.Context, in struct {
	MS int `json:"ms"`
}) (body, error) {
	return nil, s.bot.Sneak(ctx, millis(orDefault(in.MS, defaultSneakMS), 0))
}

func (s *Server) equip(_ context.Context, in struct {
	Item string `json:"item"`
}) (body, error) {
	if in.Item == "" {
		return nil, invalidf("equip: item is required")
	}
	item, err := s.bot.EquipArmour(in.Item)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "from_slot": item.Slot}, nil
}

// interact right-clicks an entity — taming, breeding, trading.
func (s *Server) interact(_ context.Context, in struct {
	Type     string `json:"type"`
	EntityID int32  `json:"entity_id"`
}) (body, error) {
	if in.EntityID != 0 {
		return nil, s.bot.InteractEntity(in.EntityID)
	}
	if in.Type == "" {
		return nil, invalidf("interact: need entity_id or type")
	}
	target, err := s.bot.InteractNearest(in.Type)
	if err != nil {
		return nil, err
	}
	return body{"target_id": target.ID, "target_type": target.TypeName}, nil
}

// consume eats or drinks, optionally switching to the item first.
func (s *Server) consume(ctx context.Context, in struct {
	Item string `json:"item"`
}) (body, error) {
	beforeHP, beforeFood := s.bot.Health()
	var err error
	if in.Item != "" {
		_, err = s.bot.ConsumeItem(ctx, in.Item)
	} else {
		err = s.bot.Consume(ctx)
	}
	if err != nil {
		return nil, err
	}
	if err := settle(ctx); err != nil {
		return nil, err
	}
	afterHP, afterFood := s.bot.Health()
	return body{
		"health": afterHP, "food": afterFood,
		"health_gained": afterHP - beforeHP,
		"food_gained":   afterFood - beforeFood,
	}, nil
}

// shoot draws a bow and looses an arrow at a target:
//
//	{"block":{"x":..,"y":..,"z":..}}   shoot a block, aimed at its centre
//	{"x":..,"y":..,"z":..}             shoot an exact point
//	{"type":"zombie"}                  shoot the nearest entity of a type
//	{"draw_ms":1000}                   draw time; 1000ms is full power
//
// Draw time is the power control: the curve is (t²+2t)/3, so half a second
// gives roughly 40% power, not 50%.
func (s *Server) shoot(ctx context.Context, in struct {
	X, Y, Z *float64
	Block   *blockRef `json:"block"`
	Type    string    `json:"type"`
	DrawMS  int       `json:"draw_ms"`
}) (body, error) {
	draw := millis(in.DrawMS, understudy.BowFullDraw)
	out := body{"draw_ms": draw.Milliseconds(), "power": understudy.BowPower(draw)}

	switch {
	case in.Type != "":
		target, err := s.bot.ShootNearest(ctx, in.Type, draw)
		if err != nil {
			return nil, err
		}
		out["target_id"] = target.ID
		out["target_type"] = target.TypeName
		return out, nil
	case in.Block != nil:
		return out, s.bot.ShootBlock(ctx, in.Block.X, in.Block.Y, in.Block.Z, draw)
	case in.X != nil && in.Y != nil && in.Z != nil:
		return out, s.bot.ShootAt(ctx, *in.X, *in.Y, *in.Z, draw)
	}
	return nil, invalidf("shoot: need block, x/y/z or type")
}

// craft crafts using the player's 2x2 grid:
//
//	{"layout": {"1": "oak_log"}}       one log -> four planks
//	{"layout": {"1":"oak_planks","2":"oak_planks","3":"oak_planks","4":"oak_planks"}}
func (s *Server) craft(ctx context.Context, in struct {
	Layout map[string]string `json:"layout"`
}) (body, error) {
	layout, err := slotLayout(in.Layout)
	if err != nil {
		return nil, err
	}
	result, err := s.bot.CraftIn2x2(ctx, layout)
	if err != nil {
		return nil, err
	}
	return body{"crafted": result.Name, "count": result.Count}, nil
}

func (s *Server) swing(_ context.Context, _ struct{}) (body, error) {
	return nil, s.bot.Swing()
}

func (s *Server) use(ctx context.Context, _ struct{}) (body, error) {
	return nil, s.bot.UseItem(ctx)
}

// digLookingAt mines whatever the crosshair is on.
func (s *Server) digLookingAt(ctx context.Context, in struct {
	HoldMS int `json:"hold_ms"`
}) (body, error) {
	hit, err := s.bot.DigLookingAt(ctx, millis(in.HoldMS, defaultDigHold))
	if err != nil {
		return nil, err
	}
	return body{
		"x": hit.X, "y": hit.Y, "z": hit.Z,
		"face": hit.Face, "distance": hit.Distance,
	}, nil
}

// attack hits either an explicit entity ID or the nearest entity of a type.
// Targeting by type is the useful form: entity IDs are assigned by the server
// and a caller has no way to predict one.
func (s *Server) attack(ctx context.Context, in struct {
	EntityID int32  `json:"entity_id"`
	Type     string `json:"type"`
	Times    int    `json:"times"`
}) (body, error) {
	times := orDefault(in.Times, defaultAttempts)

	if in.EntityID != 0 {
		for i := range times {
			if i > 0 {
				if err := sleepCtx(ctx, understudy.AttackCooldown); err != nil {
					return nil, err
				}
			}
			if err := s.bot.Attack(in.EntityID); err != nil {
				return nil, err
			}
		}
		return body{"hits": times, "target_id": in.EntityID}, nil
	}
	if in.Type == "" {
		return nil, invalidf("attack: need entity_id or type")
	}
	// hits is what landed, not what was asked for: the target can die partway
	// through, and a caller checking its own assertion needs the real number.
	target, hits, err := s.bot.AttackTimes(ctx, in.Type, times)
	if err != nil {
		return nil, err
	}
	return body{
		"hits":        hits,
		"requested":   times,
		"target_id":   target.ID,
		"target_type": target.TypeName,
	}, nil
}

func (s *Server) dig(ctx context.Context, in struct {
	X, Y, Z int32
	Blocks  []blockRef `json:"blocks"`
	Face    *int32     `json:"face"`
	HoldMS  int        `json:"hold_ms"`
}) (body, error) {
	face, err := blockFace(in.Face)
	if err != nil {
		return nil, err
	}
	hold := millis(in.HoldMS, defaultDigHold)

	if len(in.Blocks) == 0 {
		return nil, s.bot.DigBlock(ctx, in.X, in.Y, in.Z, face, hold)
	}
	coords := make([][3]int32, 0, len(in.Blocks))
	for _, b := range in.Blocks {
		coords = append(coords, [3]int32{b.X, b.Y, b.Z})
	}
	dug, err := s.bot.DigBlocks(ctx, coords, face, hold)
	if err != nil {
		// The count is part of the answer even on failure: "dug 6 of 9" tells
		// the caller far more than the error alone.
		return body{"dug": dug}, err
	}
	return body{"dug": dug}, nil
}

func (s *Server) place(ctx context.Context, in struct {
	X, Y, Z int32
	Face    *int32 `json:"face"`
	// Opt-in: confirm a block actually appeared, and re-send if it didn't.
	// Off by default because /place doubles as "right-click this block" for
	// opening UIs and using items, where nothing is expected to appear and
	// verifying would turn every such call into a two-second failure.
	Verify bool `json:"verify"`
}) (body, error) {
	face, err := blockFace(in.Face)
	if err != nil {
		return nil, err
	}
	if in.Verify {
		return nil, s.bot.PlaceBlockVerified(ctx, in.X, in.Y, in.Z, face)
	}
	return nil, s.bot.PlaceBlock(ctx, in.X, in.Y, in.Z, face)
}

// blockFace resolves an optional face field, rejecting anything that is not a
// real block side.
//
// The wire encodes the face as one signed byte, so an unchecked value is
// truncated rather than refused: face 260 arrives as face 4 and the block is
// worked from a side the caller never asked for.
func blockFace(face *int32) (int32, error) {
	v := deref(face, defaultDigFace)
	if !protocol.ValidFace(v) {
		return 0, invalidf("face %d is not a block face (want 0..%d)", v, protocol.FaceCount-1)
	}
	return v, nil
}

// settle waits for the server to apply a consequence before it is reported.
func settle(ctx context.Context) error { return sleepCtx(ctx, settleDelay) }

// sleepCtx waits for d, or until ctx is cancelled. Handlers must never block
// on a bare timer: a client that hangs up should stop the work, not leave a
// goroutine counting down.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// --- containers -------------------------------------------------------------

// handleContainer reports the open window, if any.
func (s *Server) handleContainer(w http.ResponseWriter, _ *http.Request) {
	if !s.bot.ContainerOpen() {
		s.writeJSON(w, http.StatusOK, body{"open": false})
		return
	}
	slots := s.bot.ContainerSlots()
	out := make([]body, 0, len(slots))
	for _, it := range slots {
		if it.Empty() {
			continue
		}
		out = append(out, body{"slot": it.Slot, "item": it.Name, "count": it.Count})
	}
	s.writeJSON(w, http.StatusOK, body{
		"open":      true,
		"window_id": s.bot.ContainerID(),
		"type":      s.bot.ContainerKind(),
		"title":     s.bot.ContainerTitle(),
		"size":      len(slots),
		"own_slots": s.bot.ContainerOwnSlots(),
		"kind":      s.bot.ContainerType().String(),
		"items":     out,
		"truncated": s.bot.ContainerTruncated(),
	})
}

// containerOpen right-clicks a block, or the nearest entity of a type, and
// waits for its UI. A block that is not a container never opens one, so this
// reports a timeout rather than hanging.
func (s *Server) containerOpen(ctx context.Context, in struct {
	X, Y, Z int32
	Face    *int32 `json:"face"`
	Type    string `json:"type"`
}) (body, error) {
	if in.Type != "" {
		target, err := s.bot.OpenContainerOnNearest(ctx, in.Type)
		if err != nil {
			return nil, err
		}
		return body{
			"window_id": s.bot.ContainerID(), "type": s.bot.ContainerKind(),
			"title": s.bot.ContainerTitle(), "size": len(s.bot.ContainerSlots()),
			"target_id": target.ID, "target_type": target.TypeName,
		}, nil
	}
	face, err := blockFace(in.Face)
	if err != nil {
		return nil, err
	}
	if err := s.bot.OpenContainer(ctx, in.X, in.Y, in.Z, face); err != nil {
		return nil, err
	}
	return body{
		"window_id": s.bot.ContainerID(), "type": s.bot.ContainerKind(),
		"title": s.bot.ContainerTitle(), "size": len(s.bot.ContainerSlots()),
	}, nil
}

func (s *Server) containerClose(_ context.Context, _ struct{}) (body, error) {
	return nil, s.bot.CloseContainer()
}

func (s *Server) containerClick(_ context.Context, in struct {
	Slot   int   `json:"slot"`
	Button int8  `json:"button"`
	Mode   int32 `json:"mode"`
}) (body, error) {
	return nil, s.bot.ClickContainerSlot(in.Slot, in.Button, in.Mode)
}

// containerTake shift-clicks a slot, which is what empties a crafting result
// including every repeat the ingredients allowed.
func (s *Server) containerTake(_ context.Context, in struct {
	Slot int `json:"slot"`
}) (body, error) {
	return nil, s.bot.TakeFromContainer(in.Slot)
}

// containerButton presses a numbered button — how a stonecutter or loom picks
// a recipe.
func (s *Server) containerButton(_ context.Context, in struct {
	Button int32 `json:"button"`
}) (body, error) {
	return nil, s.bot.ClickContainerButton(in.Button)
}

// containerCraft asks the server to lay out a recipe from its own recipe book,
// rather than the caller placing ingredients slot by slot. all:true repeats
// until the ingredients run out.
func (s *Server) containerCraft(ctx context.Context, in struct {
	Recipe int32  `json:"recipe"`
	Item   string `json:"item"`
	All    bool   `json:"all"`
}) (body, error) {
	// By name is the useful form: the recipe ids are the server's own and
	// change between versions, so nothing outside this session can know them.
	if in.Item != "" {
		if err := s.bot.CraftRecipeFor(ctx, in.Item, in.All); err != nil {
			return nil, err
		}
		id, _ := s.bot.RecipeFor(in.Item)
		return body{"item": in.Item, "recipe": id, "all": in.All}, nil
	}
	return nil, s.bot.CraftRecipe(in.Recipe, in.All)
}

func (s *Server) containerTrade(ctx context.Context, in struct {
	Index int32  `json:"index"`
	Item  string `json:"item"`
	Times int    `json:"times"`
	// Raw skips the confirmation, for a caller that wants to select a trade
	// and inspect the window itself.
	Raw bool `json:"raw"`
}) (body, error) {
	if in.Raw {
		return nil, s.bot.SelectTrade(in.Index)
	}
	// Selecting by what the trade produces survives a villager whose offers are
	// in a different order, which selecting by index does not.
	if in.Item != "" {
		done, err := s.bot.TradeForItem(ctx, in.Item, in.Times)
		if err != nil {
			return nil, err
		}
		return body{"traded": done, "requested": max(in.Times, 1), "item": in.Item}, nil
	}
	if in.Times > 1 {
		done, err := s.bot.TradeAndTake(ctx, in.Index, in.Times)
		if err != nil {
			return nil, err
		}
		// done < times means the villager ran out; the caller needs the real
		// number, not the one it asked for.
		return body{"traded": done, "requested": in.Times}, nil
	}
	item, err := s.bot.Trade(ctx, in.Index)
	if err != nil {
		return nil, err
	}
	return body{"traded": 1, "item": item.Name, "count": item.Count}, nil
}

// containerGrid lays a recipe out in the open crafting table by slot, and
// takes the result. Preferred over /container/craft for hand-written tests:
// a layout is readable, a numeric recipe id is not.
func (s *Server) containerGrid(ctx context.Context, in struct {
	Layout map[string]string `json:"layout"`
	Repeat int               `json:"repeat"`
}) (body, error) {
	layout, err := slotLayout(in.Layout)
	if err != nil {
		return nil, err
	}
	item, err := s.bot.CraftInGrid(ctx, layout, in.Repeat)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count, "repeat": max(in.Repeat, 1)}, nil
}

// slotLayout turns the JSON {"1": "oak_planks"} form into slot indices. JSON
// object keys are always strings, so the conversion has to happen somewhere;
// doing it once means /craft and /container/grid cannot disagree about it.
func slotLayout(in map[string]string) (map[int]string, error) {
	if len(in) == 0 {
		return nil, invalidf("layout is required, mapping a grid slot to an item")
	}
	out := make(map[int]string, len(in))
	for k, v := range in {
		slot, err := strconv.Atoi(k)
		if err != nil {
			return nil, invalidf("bad grid slot %q", k)
		}
		out[slot] = v
	}
	return out, nil
}

// --- slot moves and storage -------------------------------------------------

func (s *Server) containerPut(ctx context.Context, in struct {
	Item string `json:"item"`
	Slot int    `json:"slot"`
	One  bool   `json:"one"`
}) (body, error) {
	move := s.bot.PutIntoSlot
	if in.One {
		move = s.bot.PutOneIntoSlot
	}
	item, err := move(ctx, in.Item, in.Slot)
	if err != nil {
		return nil, err
	}
	return body{"slot": in.Slot, "item": item.Name, "count": item.Count}, nil
}

func (s *Server) containerClear(ctx context.Context, _ struct{}) (body, error) {
	return nil, s.bot.ClearContainerInputs(ctx)
}

// containerDeposit reports what actually moved, not what was asked for: a full
// container accepts the click and silently keeps the remainder.
func (s *Server) containerDeposit(ctx context.Context, in struct {
	Item  string `json:"item"`
	Count int32  `json:"count"`
	All   bool   `json:"all"`
}) (body, error) {
	if in.All {
		stacks, err := s.bot.DepositAll(ctx)
		if err != nil {
			return nil, err
		}
		return body{"stacks": stacks}, nil
	}
	moved, err := s.bot.Deposit(ctx, in.Item, in.Count)
	if err != nil {
		return nil, err
	}
	return body{"moved": moved, "requested": in.Count, "in_container": s.bot.CountInContainerOnly(in.Item)}, nil
}

func (s *Server) containerWithdraw(ctx context.Context, in struct {
	Item  string `json:"item"`
	Count int32  `json:"count"`
}) (body, error) {
	moved, err := s.bot.Withdraw(ctx, in.Item, in.Count)
	if err != nil {
		return nil, err
	}
	return body{"moved": moved, "requested": in.Count, "left_in_container": s.bot.CountInContainerOnly(in.Item)}, nil
}

// --- workstations -----------------------------------------------------------

func (s *Server) smelt(ctx context.Context, in struct {
	Input string `json:"input"`
	Fuel  string `json:"fuel"`
	Count int    `json:"count"`
}) (body, error) {
	if in.Fuel == "" {
		in.Fuel = "minecraft:coal"
	}
	item, err := s.bot.Smelt(ctx, in.Input, in.Fuel, in.Count)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) rename(ctx context.Context, in struct {
	Item string `json:"item"`
	Name string `json:"name"`
}) (body, error) {
	item, err := s.bot.RenameItem(ctx, in.Item, in.Name)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count, "renamed_to": in.Name}, nil
}

func (s *Server) anvilCombine(ctx context.Context, in struct {
	First  string `json:"first"`
	Second string `json:"second"`
}) (body, error) {
	item, err := s.bot.CombineInAnvil(ctx, in.First, in.Second)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) loom(ctx context.Context, in struct {
	Banner  string `json:"banner"`
	Dye     string `json:"dye"`
	Pattern string `json:"pattern_item"`
	Index   int32  `json:"index"`
}) (body, error) {
	item, err := s.bot.ApplyBannerPattern(ctx, in.Banner, in.Dye, in.Pattern, in.Index)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) grindstone(ctx context.Context, in struct {
	Item string `json:"item"`
}) (body, error) {
	item, err := s.bot.Disenchant(ctx, in.Item)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) smith(ctx context.Context, in struct {
	Template string `json:"template"`
	Base     string `json:"base"`
	Addition string `json:"addition"`
}) (body, error) {
	item, err := s.bot.UpgradeInSmithingTable(ctx, in.Template, in.Base, in.Addition)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) enchant(ctx context.Context, in struct {
	Item  string `json:"item"`
	Level int32  `json:"level"`
}) (body, error) {
	item, err := s.bot.Enchant(ctx, in.Item, in.Level)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) brew(ctx context.Context, in struct {
	Bottle     string `json:"bottle"`
	Ingredient string `json:"ingredient"`
	Fuel       string `json:"fuel"`
	Count      int    `json:"count"`
}) (body, error) {
	if in.Bottle == "" {
		in.Bottle = "minecraft:potion"
	}
	if in.Fuel == "" {
		in.Fuel = "minecraft:blaze_powder"
	}
	return nil, s.bot.Brew(ctx, in.Bottle, in.Ingredient, in.Fuel, in.Count)
}

// interactAt right-clicks a specific point on an entity. Where you click
// matters for multi-part entities — a chest boat's chest and seat are separate
// hitboxes, and only one of them opens the chest.
func (s *Server) interactAt(_ context.Context, in struct {
	EntityID int32   `json:"entity_id"`
	Type     string  `json:"type"`
	DX       float64 `json:"dx"`
	DY       float64 `json:"dy"`
	DZ       float64 `json:"dz"`
}) (body, error) {
	id := in.EntityID
	if in.Type != "" {
		target, err := s.bot.NearestEntity(in.Type)
		if err != nil {
			return nil, err
		}
		id = target.ID
	}
	if err := s.bot.InteractAt(id, in.DX, in.DY, in.DZ); err != nil {
		return nil, err
	}
	return body{"entity_id": id, "dx": in.DX, "dy": in.DY, "dz": in.DZ}, nil
}

// handleTrades lists a merchant's offers, including the ones that are spent —
// a caller testing lockout needs to see them, not have them filtered away.
func (s *Server) handleTrades(w http.ResponseWriter, _ *http.Request) {
	offers := s.bot.Trades()
	out := make([]body, 0, len(offers))
	for _, t := range offers {
		row := body{
			"index": t.Index, "output": t.Output.Name, "count": t.Output.Count,
			"input": t.Input.Name, "input_count": t.Input.Count,
			"uses": t.Uses, "max_uses": t.MaxUses,
			"available": t.Available(), "disabled": t.Disabled, "xp": t.XP,
		}
		if !t.Input2.Empty() {
			row["input2"] = t.Input2.Name
			row["input2_count"] = t.Input2.Count
		}
		out = append(out, row)
	}
	s.writeJSON(w, http.StatusOK, body{"count": len(out), "trades": out})
}

// handleRecipes reports what the server's recipe book taught us. ?item= looks
// one up by what it produces.
func (s *Server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	// missing is the number of entries the server sent that could not be
	// decoded. Without it a caller cannot tell "no recipe for that" from "that
	// recipe never decoded" — they read identically, and on a version whose
	// book is only partly understood the second is the common case.
	if name := r.URL.Query().Get("item"); name != "" {
		id, ok := s.bot.RecipeFor(name)
		s.writeJSON(w, http.StatusOK, body{
			"item": name, "found": ok, "recipe": id,
			"known": s.bot.KnownRecipes(), "missing": s.bot.MissingRecipes(),
		})
		return
	}
	s.writeJSON(w, http.StatusOK, body{
		"known": s.bot.KnownRecipes(), "missing": s.bot.MissingRecipes(),
	})
}

func (s *Server) beacon(ctx context.Context, in struct {
	Payment   string `json:"payment"`
	Primary   int32  `json:"primary"`
	Secondary int32  `json:"secondary"`
}) (body, error) {
	return nil, s.bot.ActivateBeacon(ctx, in.Payment, in.Primary, in.Secondary)
}

func (s *Server) cartography(ctx context.Context, in struct {
	Map     string `json:"map"`
	Applied string `json:"applied"`
}) (body, error) {
	item, err := s.bot.ApplyToMap(ctx, in.Map, in.Applied)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}
