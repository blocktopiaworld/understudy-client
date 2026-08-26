// Package understudy drives a Minecraft server connection far enough to be a
// real player: it completes the handshake, login and configuration states,
// enters play, and then keeps the connection healthy.
//
// An understudy stands in for the lead. This is a headless client that takes a
// player's place on a server — it joins, walks, mines, places, crafts, fights
// and eats over the real wire protocol, so a server cannot tell it apart from
// somebody at a keyboard. That is the point: anything that measures players —
// statistics, advancements, anti-cheat, progression systems — sees a player.
//
// Everything here is vanilla protocol behaviour and nothing above it: there is
// no scenario language, no assertions, no opinion about why you are driving a
// player. That layer belongs to whatever is using this.
//
// # Getting started
//
//	bot, err := understudy.New(understudy.Options{
//		Addr:     "127.0.0.1:25565",
//		Username: "Understudy",
//	})
//	if err != nil {
//		return err
//	}
//	if err := bot.Connect(ctx); err != nil {
//		return err
//	}
//	defer bot.Close()
//
//	go bot.Run(ctx)   // pump packets; everything below needs this running
//
//	if _, err := bot.Fall(ctx); err != nil {
//		return err
//	}
//	return bot.DigBlock(ctx, 10, 63, -5, protocol.FaceTop, 400*time.Millisecond)
//
// Connect and Run are deliberately separate: it lets a caller assert on having
// joined before anything starts pumping, which keeps "did the bot get in?"
// answerable apart from "what happened after it got in?".
//
// The server must run with online-mode=false. This client implements no
// encryption and no Mojang authentication, and says so explicitly rather than
// hanging if a server asks for it.
//
// # What it decodes
//
// Only what it needs to act and to know where it is: position, health, death,
// chunks, entities and inventory. Frames are length-prefixed, so everything
// else is skipped by its prefix — 26.1 has 141 clientbound play packets and
// this handles a couple of dozen.
//
// # Silence is the failure mode
//
// A Minecraft server ignores a great deal without complaining. An out-of-reach
// dig, a placement into an occupied space, any action from a dead player — no
// rejection, no reply, nothing distinguishable from success.
//
// So this client checks what it can itself and turns a silent miss into a real
// error, and confirms state changes by observing them rather than by assuming
// the packet worked. Where a check is impossible, the doc comment says what the
// symptom looks like instead.
//
// # Where things are
//
// One package, because Client's methods cannot be split across directories —
// but the files are named for what they hold, and pure computation that needed
// none of this lives in its own packages under internal/.
//
//	client.go      lifecycle: New, Connect, Close, and the play-state dispatch
//	login.go       handshake -> login -> configuration
//	state.go       every mutex-guarded accessor, in one place
//	heartbeat.go   idle position reporting, ~20/s, like a real client
//	settle.go      the post-teleport gate on block interactions
//	detect.go      server-list ping and version auto-detection
//	versions.go    the blank import that registers the generated protocol tables
//
//	world.go       terrain: chunk storage, ground scans, block queries
//	entities.go    entity tracking, targeting, attacking, interacting
//	inventory.go   slots, item lookup, container clicks, pickups
//	craft.go       the player's own 2x2 grid
//
//	look.go        aiming, by direction / point / block / entity / player
//	move.go        position packets and walking
//	fall.go        gravity-driven descent, water entry, auto-fall
//	dig.go         breaking blocks, and observing that they broke
//	place.go       placing blocks, and confirming they appeared
//	reach.go       the reach and liveness checks a server enforces silently
//	raytrace.go    what the crosshair is actually on
//	bow.go         drawing and loosing
//	verbs.go       input bits, sneaking, equipping, eating
//	geometry.go    re-exports of internal/geom, so callers need not import it
//
// The state those files operate on lives in its own packages, because a
// mutex-guarded store that never needs a Client is testable without one:
// internal/world (chunks and block states), internal/entities (the tracker)
// and internal/inventory (slots and stacks). Client keeps thin delegating
// methods, and Entity and ItemStack are aliases, so no caller imports an
// internal package to name a type.
//
// # Concurrency
//
// A Client is safe for concurrent use once Connect returns. Exactly one
// goroutine may call Run; every other method may be called from any goroutine,
// which is what lets a control API drive the bot while the read loop pumps.
package understudy
