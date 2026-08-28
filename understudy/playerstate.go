package understudy

import (
	"fmt"
	"sync"

	"github.com/blocktopiaworld/understudy-client/protocol"
)

// The player's own game mode and status effects.
//
// # Why this is here
//
// Because without it there is no way to answer "can this player be hurt?", and
// that question turned a working feature into a bug report. A totem of undying
// was reported as not firing when a player died holding one. It fires: held in
// either hand it saves the player and is consumed. What had actually happened
// is that the damage never landed, and the two states that do that — a player
// left in creative or spectator by an earlier scenario, or one still carrying
// Resistance V — were both invisible from here.
//
// The tell is worth writing down: a totem that works is *gone*. "Alive with the
// totem intact" is not a totem that failed to fire, it is damage that never
// arrived. A harness that can see the game mode and the effects can assert the
// precondition instead of misreading the result.

// GameMode is the player's own mode, as the server last reported it.
type GameMode int32

// The vanilla modes. Unknown is the zero value rather than survival, so a
// caller cannot mistake "never told" for "told it was survival".
const (
	GameModeUnknown  GameMode = -1
	GameModeSurvival GameMode = iota - 1
	GameModeCreative
	GameModeAdventure
	GameModeSpectator
)

func (g GameMode) String() string {
	switch g {
	case GameModeSurvival:
		return "survival"
	case GameModeCreative:
		return "creative"
	case GameModeAdventure:
		return "adventure"
	case GameModeSpectator:
		return "spectator"
	default:
		return "unknown"
	}
}

// Damageable reports whether ordinary damage can reach a player in this mode.
//
// Creative and spectator absorb everything, which is what makes an unresettable
// scenario look like a broken one.
func (g GameMode) Damageable() bool {
	return g == GameModeSurvival || g == GameModeAdventure
}

// Effect is one active status effect.
type Effect struct {
	ID        int32  `json:"id"`
	Name      string `json:"name"`
	Amplifier int32  `json:"amplifier"`
	// Duration in ticks. -1 means infinite, which the server sends for effects
	// given with an infinite duration.
	Duration int32 `json:"duration"`
}

// Level is the effect's level as a player would say it: amplifier 0 is I.
func (e Effect) Level() int32 { return e.Amplifier + 1 }

// effects tracks the player's own status effects.
type effectSet struct {
	mu sync.RWMutex
	by map[int32]Effect
}

func newEffectSet() *effectSet { return &effectSet{by: map[int32]Effect{}} }

func (s *effectSet) set(e Effect) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.by[e.ID] = e
}

func (s *effectSet) remove(id int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.by, id)
}

func (s *effectSet) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.by)
}

func (s *effectSet) all() []Effect {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Effect, 0, len(s.by))
	for _, e := range s.by {
		out = append(out, e)
	}
	return out
}

// readSpawnInfo steps through a SpawnInfo block and returns the game mode it
// carries.
//
// Both login and respawn end with one of these, and it is the only place the
// server states the mode outright — game_state_change only reports *changes*,
// so a bot that joins in creative and stays there would never hear about it.
func readSpawnInfo(r *protocol.Reader) GameMode {
	r.VarInt()     // dimension type
	_ = r.String() // dimension name
	r.I64()        // hashed seed
	mode := GameMode(r.U8())
	r.U8() // previous mode
	return mode
}

// readLoginGameMode steps through the login packet as far as its SpawnInfo.
//
// The fields before it are skipped rather than kept: this client has no use for
// the world list or the simulation distance, but it cannot reach the mode
// without stepping over them exactly.
func readLoginGameMode(r *protocol.Reader) GameMode {
	r.Bool() // hardcore
	for range r.VarInt() {
		_ = r.String() // world names
	}
	r.VarInt() // max players
	r.VarInt() // view distance
	r.VarInt() // simulation distance
	r.Bool()   // reduced debug info
	r.Bool()   // enable respawn screen
	r.Bool()   // limited crafting
	return readSpawnInfo(r)
}

// GameMode returns the player's mode, or GameModeUnknown if the server has not
// said.
func (c *Client) GameMode() GameMode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gameMode
}

// Effects returns the player's active status effects.
func (c *Client) Effects() []Effect { return c.effects.all() }

// Effect looks up one active effect by name.
func (c *Client) Effect(name string) (Effect, bool) {
	want := protocol.Namespaced(name)
	for _, e := range c.effects.all() {
		if e.Name == want {
			return e, true
		}
	}
	return Effect{}, false
}

// WhyNotDamageable explains what would stop damage reaching the player, or
// returns nil if nothing would.
//
// Written as a question about the *precondition* rather than a check on the
// result, because checking the result is what goes wrong: health read after a
// hit cannot tell "survived" from "died and respawned to full", and an intact
// totem cannot tell "did not fire" from "was never needed".
func (c *Client) WhyNotDamageable() error {
	mode := c.GameMode()
	if mode == GameModeUnknown {
		return fmt.Errorf("understudy: the server has not said what game mode " +
			"this player is in, so whether damage can reach them is unknown")
	}
	if !mode.Damageable() {
		return fmt.Errorf("understudy: the player is in %s, which absorbs all "+
			"damage", mode)
	}
	// Resistance V and above is total immunity; below that damage still lands.
	if e, ok := c.Effect("minecraft:resistance"); ok && e.Level() >= 5 {
		return fmt.Errorf("understudy: the player has Resistance %d, which "+
			"blocks all damage", e.Level())
	}
	return nil
}
