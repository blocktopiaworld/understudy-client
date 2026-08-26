package control

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	understudy "github.com/blocktopia/understudy-client"
	"github.com/blocktopia/understudy-client/protocol"
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
	mux.Handle("POST /consume", handle(s, s.consume))
	mux.Handle("POST /shoot", handle(s, s.shoot))
	mux.Handle("POST /craft", handle(s, s.craft))
	mux.Handle("POST /swing", handle(s, s.swing))
	mux.Handle("POST /diglook", handle(s, s.digLookingAt))
	mux.Handle("POST /attack", handle(s, s.attack))
	mux.Handle("POST /dig", handle(s, s.dig))
	mux.Handle("POST /place", handle(s, s.place))
	mux.Handle("POST /use", handle(s, s.use))
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
	if len(in.Layout) == 0 {
		return nil, invalidf("craft: layout is required, mapping grid slot 1..4 to an item")
	}
	layout := make(map[int]string, len(in.Layout))
	for k, v := range in.Layout {
		slot, err := strconv.Atoi(k)
		if err != nil {
			return nil, invalidf("craft: bad grid slot %q", k)
		}
		layout[slot] = v
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
	target, err := s.bot.AttackTimes(ctx, in.Type, times)
	if err != nil {
		return nil, err
	}
	return body{"hits": times, "target_id": target.ID, "target_type": target.TypeName}, nil
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
