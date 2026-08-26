package understudy

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/blocktopia/understudy-client/internal/entities"
	"github.com/blocktopia/understudy-client/internal/geom"
	"github.com/blocktopia/understudy-client/protocol"
)

// Entity is a tracked entity in the bot's view of the world.
//
// An alias rather than a wrapper: the tracker owns the type, and callers of
// this package should not have to import an internal one to name it.
type Entity = entities.Entity

// byDistance sorts entities nearest-first from a position, in place.
func byDistance(list []Entity, from Position) {
	slices.SortFunc(list, func(a, b Entity) int {
		return cmp.Compare(distance(from, a), distance(from, b))
	})
}

// Entities returns every tracked entity, nearest first.
func (c *Client) Entities() []Entity {
	list := c.entities.All()
	byDistance(list, c.Position())
	return list
}

// EntitiesOfType returns tracked entities whose type name matches, nearest
// first. The match accepts a bare name ("zombie") as well as a namespaced one
// ("minecraft:zombie").
func (c *Client) EntitiesOfType(typeName string) []Entity {
	list := c.entities.Matching(typeName)
	byDistance(list, c.Position())
	return list
}

// ErrNoSuchEntity reports that nothing of the requested type is being tracked.
//
// It is a sentinel because "there is none" and "there is one but you cannot
// reach it" are different answers that callers act on differently — killing
// the last of something is a success, being out of range is not. See
// AttackTimes, which relies on telling them apart.
var ErrNoSuchEntity = errors.New("understudy: no tracked entity")

// NearestEntity returns the closest entity of the given type. An empty
// typeName matches any type. It returns an error wrapping ErrNoSuchEntity if
// nothing of that type is tracked.
func (c *Client) NearestEntity(typeName string) (Entity, error) {
	candidates := c.entities.Matching(typeName)
	if len(candidates) == 0 {
		return Entity{}, fmt.Errorf("%w of type %q", ErrNoSuchEntity, typeName)
	}
	// Only the minimum is needed, so pick it in one pass rather than sorting
	// the whole view distance to read element zero.
	pos := c.Position()
	nearest := candidates[0]
	for _, e := range candidates[1:] {
		if distance(pos, e) < distance(pos, nearest) {
			nearest = e
		}
	}
	return nearest, nil
}

// Attack hits an entity once, at full charge.
//
// Discrete attacks matter: holding the button down fires every tick, and every
// hit after the first is an uncharged swing doing a fraction of the damage.
// Space repeated calls by the weapon's cooldown (~600ms for a sword) to land
// full-power hits.
func (c *Client) Attack(entityID int32) error {
	if err := c.requireAlive("attack"); err != nil {
		return err
	}
	// Before 26.1 attacking was folded into use_entity with a mode field, a
	// shape this client does not implement. Say so rather than sending a
	// packet the server will ignore in silence.
	if !c.v.SupportsAttackPacket() {
		return fmt.Errorf("understudy: attacking is not implemented for %s "+
			"(it predates the dedicated attack packet)", c.v.Name)
	}
	if err := c.conn.WritePacket(
		protocol.NewWriter(c.v.Packets.SBPlayAttack).VarInt(entityID).Bytes()); err != nil {
		return err
	}
	return c.Swing()
}

// AttackNearest finds the closest entity of a type, faces it, and hits it once.
//
// It fails rather than swinging if the nearest candidate is out of reach:
// "nearest" is not the same as "reachable", and mobs wander.
func (c *Client) AttackNearest(typeName string) (Entity, error) {
	target, err := c.aimAtNearest("attack", typeName)
	if err != nil {
		return target, err
	}
	return target, c.Attack(target.ID)
}

// InteractEntity right-clicks an entity — taming, breeding, trading, leashing,
// shearing.
//
// In 26.1 this is interact only: attacking moved to its own packet. The hit
// location is sent as a zero vector, which the variable-length encoding
// expresses in a single zero byte, since interacting with an entity does not
// depend on where on its body it was clicked.
func (c *Client) InteractEntity(entityID int32) error {
	if err := c.requireAlive("interact"); err != nil {
		return err
	}
	if c.v.Packets.SBPlayUseEntity == protocol.Absent {
		return fmt.Errorf("understudy: %s has no use_entity packet", c.v.Name)
	}
	w := protocol.NewWriter(c.v.Packets.SBPlayUseEntity).
		VarInt(entityID).
		VarInt(protocol.MainHand).
		U8(0). // lpVec3 zero vector: a leading zero byte encodes {0,0,0}
		Bool(false)
	if err := c.conn.WritePacket(w.Bytes()); err != nil {
		return err
	}
	return c.Swing()
}

// InteractNearest right-clicks the closest entity of a type.
func (c *Client) InteractNearest(typeName string) (Entity, error) {
	target, err := c.aimAtNearest("interact", typeName)
	if err != nil {
		return target, err
	}
	return target, c.InteractEntity(target.ID)
}

// aimAtNearest finds the closest entity of a type, checks it is in reach and
// faces it — the preamble every entity verb shares.
//
// The facing is not required for the server to register a hit, but an unfacing
// attacker is a giveaway that this is not a player, and some plugins do check
// the look vector.
func (c *Client) aimAtNearest(action, typeName string) (Entity, error) {
	target, err := c.NearestEntity(typeName)
	if err != nil {
		return Entity{}, err
	}
	if err := c.requireEntityReach(action, target); err != nil {
		return target, err
	}
	return target, c.LookAtEntity(target)
}

// PlayerEntity finds a tracked player by name.
//
// The spawn packet carries a UUID but no name, and names only arrive in the
// player_info packet — a bitmask-driven structure with a lot of decoding
// surface. This client requires online-mode=false anyway (it implements no
// encryption), and on such a server a player's UUID is *derived* from their
// name, so the name can be resolved without decoding player_info at all.
//
// That equivalence is exactly what makes this shortcut safe here and unsafe
// in general: against an online-mode server these UUIDs come from Mojang and
// this lookup would never match.
func (c *Client) PlayerEntity(name string) (Entity, error) {
	want := protocol.OfflineUUID(name)
	tracked := c.entities.All()
	for _, e := range tracked {
		if e.UUID == want {
			return e, nil
		}
	}
	return Entity{}, fmt.Errorf("understudy: player %q is not in range (tracked entities: %d)",
		name, len(tracked))
}

func distance(p Position, e Entity) float64 {
	return geom.Length(e.X-p.X, e.Y-p.Y, e.Z-p.Z)
}

// DistanceTo returns how far the bot is from an entity, in blocks.
func (c *Client) DistanceTo(e Entity) float64 { return distance(c.Position(), e) }

// AttackCooldown is how long a sword takes to recharge. Attacking faster lands
// uncharged hits that do a fraction of the damage, so a fight never resolves.
const AttackCooldown = 600 * time.Millisecond

// AttackTimes hits the nearest entity of a type repeatedly, pausing for the
// weapon cooldown between swings.
//
// The target is re-selected every swing: the previous one may have died, and
// hitting a corpse's stale ID does nothing.
//
// It returns the number of hits that actually landed, which can be fewer than
// asked for. Running out of targets is not an error once something has been
// hit — a diamond pickaxe one-shots a chicken, so "attack it three times"
// legitimately lands one hit and finds nothing left to swing at. Reporting
// that as a failure blames the caller for succeeding. Finding nothing on the
// *first* swing is still an error, because then nothing was attacked at all.
func (c *Client) AttackTimes(ctx context.Context, typeName string, times int) (Entity, int, error) {
	var target Entity
	hits := 0
	for i := range times {
		if i > 0 {
			if err := wait(ctx, AttackCooldown); err != nil {
				return target, hits, err
			}
		}
		next, err := c.AttackNearest(typeName)
		if err != nil {
			if hits > 0 && errors.Is(err, ErrNoSuchEntity) {
				// Nothing left of that type: the previous hits killed it.
				c.log.Debug("attack ran out of targets", "type", typeName, "hits", hits)
				return target, hits, nil
			}
			return target, hits, err
		}
		target = next
		hits++
	}
	return target, hits, nil
}

// maxEntitiesPerPacket bounds a destroy list, so a corrupt count cannot make
// the client preallocate an arbitrary slice.
const maxEntitiesPerPacket = 1 << 16

// handleEntityPacket decodes the entity-tracking packets. It returns false if
// the packet was not one of them, so the caller can fall through.
func (c *Client) handleEntityPacket(p protocol.Packet) (bool, error) {
	switch p.ID {
	case c.v.Packets.CBPlaySpawnEntity:
		r := p.Reader()
		id := r.VarInt()
		uuid := r.UUID()
		typeID := r.VarInt()
		x, y, z := r.F64(), r.F64(), r.F64()
		// Everything after this point (velocity, rotation, object data) is
		// deliberately left undecoded — the trailing velocity field has an
		// ambiguous width, and none of it is needed to address the entity.
		if err := r.Err(); err != nil {
			return true, err
		}
		c.entities.Spawn(&Entity{
			ID: id, Type: typeID, TypeName: c.v.EntityTypeName(typeID),
			UUID: uuid, X: x, Y: y, Z: z,
		})
		return true, nil

	case c.v.Packets.CBPlayEntityDestroy:
		r := p.Reader()
		n := r.VarInt()
		if err := r.Err(); err != nil {
			return true, err
		}
		if n < 0 || int(n) > maxEntitiesPerPacket {
			return true, fmt.Errorf("understudy: implausible entity destroy count %d", n)
		}
		ids := make([]int32, 0, n)
		for range n {
			ids = append(ids, r.VarInt())
		}
		if err := r.Err(); err != nil {
			return true, err
		}
		c.entities.Remove(ids)
		return true, nil

	case c.v.Packets.CBPlayRelEntityMove, c.v.Packets.CBPlayEntityMoveLook:
		r := p.Reader()
		id := r.VarInt()
		dx, dy, dz := r.I16(), r.I16(), r.I16()
		if err := r.Err(); err != nil {
			return true, err
		}
		c.entities.MoveRelative(id,
			float64(dx)*protocol.RelativeMoveUnit,
			float64(dy)*protocol.RelativeMoveUnit,
			float64(dz)*protocol.RelativeMoveUnit)
		return true, nil

	case c.v.Packets.CBPlayEntityTeleport:
		r := p.Reader()
		id := r.VarInt()
		x, y, z := r.F64(), r.F64(), r.F64()
		if err := r.Err(); err != nil {
			return true, err
		}
		c.entities.Teleport(id, x, y, z)
		return true, nil
	}
	return false, nil
}
