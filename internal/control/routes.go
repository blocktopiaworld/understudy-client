package control

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/blocktopiaworld/understudy-client/protocol"
	"github.com/blocktopiaworld/understudy-client/understudy"
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
	out := body{
		"item":         name,
		"total":        s.bot.CountItem(name),
		"storage_only": s.bot.CountItemStorage(name),
		"free_slots":   s.bot.FreeStorageSlots(),
		"stack_size":   s.bot.Version().StackSizeOf(name),
		"want":         want,
		"slots_needed": slots,
		"fits":         fits,
	}
	// A block state or a component list is not something this client matches
	// on, so it answers for the id and says which part it ignored. Counting
	// every potion when the caller asked for a water bottle is a defensible
	// answer; giving it without saying so is not.
	if q := protocol.Qualifier(name); q != "" {
		out["matched_as"] = protocol.BaseID(name)
		out["ignored_qualifier"] = q
	}
	s.writeJSON(w, http.StatusOK, out)
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

type blockRef struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
	Z int32 `json:"z"`
}

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
	Direction  string    `json:"direction" openapi:"enum=north|south|east|west|up|down"`
	Yaw        *float32  `json:"yaw"`
	Pitch      *float32  `json:"pitch" openapi:"min=-90,max=90"`
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

type pointRequest struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

func (s *Server) lookAt(_ context.Context, in pointRequest) (body, error) {
	return nil, s.bot.LookAt(in.X, in.Y, in.Z)
}

func (s *Server) move(_ context.Context, in pointRequest) (body, error) {
	return nil, s.bot.MoveTo(in.X, in.Y, in.Z)
}

// walk goes at walking speed, or sprinting speed with {"sprint": true}.
//
// A flag rather than a /sprint endpoint: it is the same journey to the same
// place, and a caller that wants to compare the two should not have to change
// which URL it posts to.
func (s *Server) walk(ctx context.Context, in struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Z      float64 `json:"z"`
	Sprint bool    `json:"sprint"`
}) (body, error) {
	if in.Sprint {
		if err := s.bot.SprintTo(ctx, in.X, in.Y, in.Z); err != nil {
			return nil, err
		}
		return body{"sprinted": true, "speed": understudy.SprintSpeed}, nil
	}
	if err := s.bot.WalkTo(ctx, in.X, in.Y, in.Z); err != nil {
		return nil, err
	}
	return body{"sprinted": false, "speed": understudy.WalkSpeed}, nil
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
	Slot int `json:"slot" openapi:"required,min=0,max=8"`
}) (body, error) {
	return nil, s.bot.SetHeldSlot(in.Slot)
}

// hold puts a named item into the bot's hand, from anywhere in the inventory.
func (s *Server) hold(_ context.Context, in struct {
	Item string `json:"item" openapi:"required"`
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
	// Read the hand before, because after the drop it may be empty. This
	// answered nothing at all until a conformance test found that the client's
	// own count did not move after a drop — a silence that hid the bug for as
	// long as nobody asked the server.
	before, _ := s.bot.HeldItem()
	if err := s.bot.DropHeld(ctx, in.All); err != nil {
		return nil, err
	}
	dropped := int32(1)
	if in.All {
		dropped = before.Count
	}
	if before.Empty() {
		dropped = 0
	}
	return body{"item": before.Name, "dropped": dropped, "all": in.All}, nil
}

// sneak holds sneak for a duration, since sneak_time only accrues while
// actually sneaking.
func (s *Server) sneak(ctx context.Context, in struct {
	MS int `json:"ms" openapi:"min=0"`
}) (body, error) {
	return nil, s.bot.Sneak(ctx, millis(orDefault(in.MS, defaultSneakMS), 0))
}

func (s *Server) equip(_ context.Context, in struct {
	Item string `json:"item" openapi:"required"`
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

// openapi:anyOf entity_id
// openapi:anyOf type
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

// openapi:anyOf block
// openapi:anyOf x, y, z
// openapi:anyOf type
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
	X      *float64  `json:"x"`
	Y      *float64  `json:"y"`
	Z      *float64  `json:"z"`
	Block  *blockRef `json:"block"`
	Type   string    `json:"type"`
	DrawMS int       `json:"draw_ms" openapi:"min=0"`
	// The bow to draw. Optional, and held first when given.
	Item string `json:"item"`
}) (body, error) {
	if in.Item != "" {
		if _, err := s.bot.HoldItem(in.Item); err != nil {
			return nil, err
		}
		if err := settle(ctx); err != nil {
			return nil, err
		}
	}
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

func (s *Server) use(ctx context.Context, in struct {
	// How long to hold the right-click before releasing it, which is what a
	// bow, a shield or a spyglass needs. Zero is a tap.
	HoldMS int `json:"hold_ms" openapi:"min=0"`
}) (body, error) {
	return nil, s.bot.UseItemFor(ctx, time.Duration(in.HoldMS)*time.Millisecond)
}

// digLookingAt mines whatever the crosshair is on.
func (s *Server) digLookingAt(ctx context.Context, in struct {
	HoldMS int `json:"hold_ms" openapi:"min=0"`
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

// openapi:anyOf entity_id
// openapi:anyOf type
// attack hits either an explicit entity ID or the nearest entity of a type.
// Targeting by type is the useful form: entity IDs are assigned by the server
// and a caller has no way to predict one.
func (s *Server) attack(ctx context.Context, in struct {
	EntityID int32  `json:"entity_id"`
	Type     string `json:"type"`
	Times    int    `json:"times" openapi:"min=1"`
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

// openapi:anyOf x, y, z
// openapi:anyOf blocks
func (s *Server) dig(ctx context.Context, in struct {
	X      int32      `json:"x"`
	Y      int32      `json:"y"`
	Z      int32      `json:"z"`
	Blocks []blockRef `json:"blocks"`
	Face   *int32     `json:"face"`
	HoldMS int        `json:"hold_ms" openapi:"min=0"`
}) (body, error) {
	face, err := blockFace(in.Face)
	if err != nil {
		return nil, err
	}
	hold := millis(in.HoldMS, defaultDigHold)

	if len(in.Blocks) == 0 {
		// Reported the same way as a batch of one, so a caller reading "dug"
		// does not have to know which form it sent. It used to answer with the
		// envelope alone here and a count for the array form, which meant a
		// test that wanted one number had to send a one-element array.
		if err := s.bot.DigBlock(ctx, in.X, in.Y, in.Z, face, hold); err != nil {
			return body{"dug": 0}, err
		}
		return body{"dug": 1}, nil
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
	X    int32  `json:"x"`
	Y    int32  `json:"y"`
	Z    int32  `json:"z"`
	Face *int32 `json:"face" openapi:"min=0,max=5"`
	// Opt-in: confirm a block actually appeared, and re-send if it didn't.
	// Off by default because /place doubles as "right-click this block" for
	// opening UIs and using items, where nothing is expected to appear and
	// verifying would turn every such call into a two-second failure.
	Verify bool `json:"verify"`
	// What to place. Optional, and when given it is held first — /consume has
	// always taken its item this way, and there was no reason for placing to
	// be the one verb that made the caller arrange its own hand.
	Item string `json:"item"`
}) (body, error) {
	face, err := blockFace(in.Face)
	if err != nil {
		return nil, err
	}
	if in.Item != "" {
		if _, err := s.bot.HoldItem(in.Item); err != nil {
			return nil, err
		}
		// The slot change has to reach the server before the placement, or the
		// wrong thing gets placed.
		if err := settle(ctx); err != nil {
			return nil, err
		}
	}
	if in.Verify {
		if err := s.bot.PlaceBlockVerified(ctx, in.X, in.Y, in.Z, face); err != nil {
			return nil, err
		}
	} else if err := s.bot.PlaceBlock(ctx, in.X, in.Y, in.Z, face); err != nil {
		return nil, err
	}
	held, _ := s.bot.HeldItem()
	return body{
		"placed": held.Name, "face": face, "verified": in.Verify,
		"against": body{"x": in.X, "y": in.Y, "z": in.Z},
	}, nil
}

// errNoMerchantWindow is the answer to "what is this merchant selling" when
// there is no merchant.
var errNoMerchantWindow = errors.New(
	"understudy: no container is open, so there are no offers to report — open a merchant" +
		" first; an empty list would not have told you which of the two you had")

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
// station is the block or entity a verb should work at, for callers who would
// otherwise have to open it themselves.
//
// Twenty of these endpoints refuse until the right window is open, and the
// client already knows which window each one needs — it just would not go and
// get one. So every one of them now accepts the position of the block to use,
// opens it, and does the job: three calls become one, and nothing that already
// worked changes, because with no position given the behaviour is exactly what
// it was.
//
// It cannot find the block for you. The version tables carry item names and
// block *classification*, not block names, so "the nearest furnace" is not a
// question this client can answer. Naming the position is the caller's part.
//
// The coordinates are pointers because zero is a real coordinate: "not given"
// has to be distinguishable from "at the origin".
type station struct {
	// The block to work at. Give all three or none.
	X *int32 `json:"x"`
	Y *int32 `json:"y"`
	Z *int32 `json:"z"`
	// Which face of it to click, 0-5. Defaults to the one facing the bot.
	Face *int32 `json:"face" openapi:"min=0,max=5"`

	// A merchant to open instead of a block: the nearest entity of this type.
	At string `json:"at"`
	// A merchant by exact entity id, from GET /entities.
	EntityID int32 `json:"at_entity_id"`
}

// asked reports whether the caller named something to open.
//
// Any one coordinate counts, so that half a position reaches the check below
// and is told what is missing. Treating it as "nothing given" would silently
// run the verb against whatever window happened to be open, which is the wrong
// answer arrived at quietly.
func (w station) asked() bool {
	return w.X != nil || w.Y != nil || w.Z != nil || w.At != "" || w.EntityID != 0
}

// open opens what the caller named, and does nothing when they named nothing.
//
// The window is left open afterwards. A verb that closed it would break the
// callers who open once and act several times, and closing is one more call
// for the callers who do not care.
func (s *Server) open(ctx context.Context, w station) error {
	if !w.asked() {
		return nil
	}
	if w.EntityID != 0 {
		return s.bot.OpenContainerOnEntity(ctx, w.EntityID)
	}
	if w.At != "" {
		_, err := s.bot.OpenContainerOnNearest(ctx, w.At)
		return err
	}
	if w.X == nil || w.Y == nil || w.Z == nil {
		return invalidf("give all three of X, Y and Z, or none of them")
	}
	face, err := blockFace(w.Face)
	if err != nil {
		return err
	}
	return s.bot.OpenContainer(ctx, *w.X, *w.Y, *w.Z, face)
}

func (s *Server) containerOpen(ctx context.Context, in struct {
	X    int32  `json:"x"`
	Y    int32  `json:"y"`
	Z    int32  `json:"z"`
	Face *int32 `json:"face" openapi:"min=0,max=5"`
	Type string `json:"type"`
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

func (s *Server) containerClick(ctx context.Context, in struct {
	station
	Slot   int   `json:"slot"`
	Button int8  `json:"button" openapi:"min=0,max=1"`
	Mode   int32 `json:"mode" openapi:"min=0,max=6"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	return nil, s.bot.ClickContainerSlot(in.Slot, in.Button, in.Mode)
}

// containerTake shift-clicks a slot, which is what empties a crafting result
// including every repeat the ingredients allowed.
func (s *Server) containerTake(ctx context.Context, in struct {
	station
	Slot int `json:"slot"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	return nil, s.bot.TakeFromContainer(in.Slot)
}

// containerButton presses a numbered button — how a stonecutter or loom picks
// a recipe.
func (s *Server) containerButton(ctx context.Context, in struct {
	station
	Button int32 `json:"button"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	return nil, s.bot.ClickContainerButton(in.Button)
}

// containerCraft asks the server to lay out a recipe from its own recipe book,
// rather than the caller placing ingredients slot by slot. all:true repeats
// until the ingredients run out.
func (s *Server) containerCraft(ctx context.Context, in struct {
	station
	Recipe int32  `json:"recipe"`
	Item   string `json:"item"`
	All    bool   `json:"all"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
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

// openapi:anyOf item
// openapi:anyOf index
func (s *Server) containerTrade(ctx context.Context, in struct {
	station
	Index int32  `json:"index"`
	Item  string `json:"item"`
	Times int    `json:"times" openapi:"min=1"`
	// Raw skips the confirmation, for a caller that wants to select a trade
	// and inspect the window itself.
	Raw bool `json:"raw"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	if in.Raw {
		return nil, s.bot.SelectTrade(in.Index)
	}
	// Clamp here rather than relying on the callee to do it, so the number
	// asked for and the number reported come from the same place.
	times := max(in.Times, 1)
	// Selecting by what the trade produces survives a villager whose offers are
	// in a different order, which selecting by index does not.
	if in.Item != "" {
		done, err := s.bot.TradeForItem(ctx, in.Item, times)
		if err != nil {
			return nil, err
		}
		return body{"traded": done, "requested": times, "item": in.Item}, nil
	}
	// Every count goes through TradeAndTake, including one. Trade alone stops a
	// step short: it selects the offer and waits for the result to appear, but
	// the server does not count a trade until the result is *taken*. A caller
	// that stopped there saw a result stack, reported success, and left the
	// villager holding it — no traded_with_villager, no trade event, nothing
	// downstream. Selecting is not trading.
	var output understudy.ItemStack
	for _, offer := range s.bot.Trades() {
		if offer.Index == in.Index {
			output = offer.Output
			break
		}
	}
	// done < times means the villager ran out; done > times means vanilla
	// batched the take. Either way it is measured from the stock gained, so
	// report it rather than echoing what was asked for.
	done, err := s.bot.TradeAndTake(ctx, in.Index, times)
	if err != nil {
		return nil, err
	}
	out := body{"traded": done, "requested": times}
	if output.Name != "" {
		out["item"] = output.Name
		out["count"] = output.Count
	}
	return out, nil
}

// containerGrid lays a recipe out in the open crafting table by slot, and
// takes the result. Preferred over /container/craft for hand-written tests:
// a layout is readable, a numeric recipe id is not.
func (s *Server) containerGrid(ctx context.Context, in struct {
	station
	Layout map[string]string `json:"layout" openapi:"required"`
	Repeat int               `json:"repeat" openapi:"min=1"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
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
	station
	Item string `json:"item"`
	Slot int    `json:"slot"`
	One  bool   `json:"one"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
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

func (s *Server) containerClear(ctx context.Context, in struct {
	station
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	return nil, s.bot.ClearContainerInputs(ctx)
}

// containerDeposit reports what actually moved, not what was asked for: a full
// container accepts the click and silently keeps the remainder.
func (s *Server) containerDeposit(ctx context.Context, in struct {
	station
	Item  string `json:"item"`
	Count int32  `json:"count"`
	All   bool   `json:"all"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
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
	station
	Item  string `json:"item"`
	Count int32  `json:"count"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	moved, err := s.bot.Withdraw(ctx, in.Item, in.Count)
	if err != nil {
		return nil, err
	}
	return body{"moved": moved, "requested": in.Count, "left_in_container": s.bot.CountInContainerOnly(in.Item)}, nil
}

// --- workstations -----------------------------------------------------------

func (s *Server) smelt(ctx context.Context, in struct {
	station
	Input string `json:"input"`
	Fuel  string `json:"fuel"`
	Count int    `json:"count"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
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
	station
	Item string `json:"item"`
	Name string `json:"name"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	item, err := s.bot.RenameItem(ctx, in.Item, in.Name)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count, "renamed_to": in.Name}, nil
}

func (s *Server) anvilCombine(ctx context.Context, in struct {
	station
	First  string `json:"first"`
	Second string `json:"second"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	item, err := s.bot.CombineInAnvil(ctx, in.First, in.Second)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) loom(ctx context.Context, in struct {
	station
	Banner  string `json:"banner"`
	Dye     string `json:"dye"`
	Pattern string `json:"pattern_item"`
	Index   int32  `json:"index"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	item, err := s.bot.ApplyBannerPattern(ctx, in.Banner, in.Dye, in.Pattern, in.Index)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) grindstone(ctx context.Context, in struct {
	station
	Item string `json:"item"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	item, err := s.bot.Disenchant(ctx, in.Item)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) smith(ctx context.Context, in struct {
	station
	Template string `json:"template"`
	Base     string `json:"base"`
	Addition string `json:"addition"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	item, err := s.bot.UpgradeInSmithingTable(ctx, in.Template, in.Base, in.Addition)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) enchant(ctx context.Context, in struct {
	station
	Item  string `json:"item"`
	Level int32  `json:"level"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	item, err := s.bot.Enchant(ctx, in.Item, in.Level)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}

func (s *Server) brew(ctx context.Context, in struct {
	station
	Bottle     string `json:"bottle"`
	Ingredient string `json:"ingredient"`
	Fuel       string `json:"fuel"`
	Count      int    `json:"count"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	if in.Bottle == "" {
		in.Bottle = "minecraft:potion"
	}
	if in.Fuel == "" {
		in.Fuel = "minecraft:blaze_powder"
	}
	return nil, s.bot.Brew(ctx, in.Bottle, in.Ingredient, in.Fuel, in.Count)
}

// openapi:anyOf entity_id
// openapi:anyOf type
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
// handleTrades reports a merchant's offers, and says which kind of "none" it
// has when there are none.
//
// An empty list used to mean any of three things: no window open, a window that
// is not a merchant, or a merchant with nothing to sell. They need different
// fixes, and collapsing them left callers polling in a loop to find out which
// they had — the caller doing the client's job.
func (s *Server) handleTrades(w http.ResponseWriter, _ *http.Request) {
	if !s.bot.ContainerOpen() {
		s.failed(w, errNoMerchantWindow, body{"open": false, "count": 0, "trades": []body{}})
		return
	}
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
	s.writeJSON(w, http.StatusOK, body{
		"open": true, "kind": s.bot.ContainerKind(),
		"count": len(out), "trades": out,
	})
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
	station
	Payment   string `json:"payment"`
	Primary   int32  `json:"primary"`
	Secondary int32  `json:"secondary"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	return nil, s.bot.ActivateBeacon(ctx, in.Payment, in.Primary, in.Secondary)
}

func (s *Server) cartography(ctx context.Context, in struct {
	station
	Map     string `json:"map"`
	Applied string `json:"applied"`
}) (body, error) {
	if err := s.open(ctx, in.station); err != nil {
		return nil, err
	}
	item, err := s.bot.ApplyToMap(ctx, in.Map, in.Applied)
	if err != nil {
		return nil, err
	}
	return body{"item": item.Name, "count": item.Count}, nil
}
