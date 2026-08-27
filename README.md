# understudy-client

A headless Minecraft Java Edition client, in Go, that speaks the wire protocol
directly — no game, no rendering, no mod loader. It connects as an ordinary
offline-mode player and can mine, place, craft, eat, shoot, fall and fight.

It exists for **automated in-game testing**. If you maintain a server plugin or
datapack and want to assert that playing the game actually produces the effect
you intended, you need something that plays the game. A bot that issues
commands proves nothing: `/setblock` does not fire the events a player mining a
block fires, and a statistic that only a real interaction increments will not
move.

```
              ┌─ your test suite ─┐
              │  HTTP: /dig /place│
              └─────────┬─────────┘
                        │
              ┌─────────▼─────────┐        TCP, Minecraft wire protocol
              │ understudy-client │◄──────────────────────────────────►  server
              └───────────────────┘        (Fabric, Paper, vanilla…)
```

Because it is a real connection, everything the server does in response is
real: statistics accrue, advancements fire, plugins see the events they would
see from a person.

## Status

Verified against **Fabric 26.1.2** by a live acceptance sweep that asserts
every result through RCON — the server's view, not the client's self-report,
because "the client believes it placed a block" is exactly the failure being
tested for.

Paper is not yet covered. The protocol is the same, so it is expected to work;
"expected to work" is not the same as tested, and this README will say so when
it changes.

## Install

```sh
go install github.com/blocktopia/understudy-client/cmd/understudy-client@latest
```

Or from a checkout:

```sh
go build ./cmd/understudy-client
```

Go 1.26 or newer. No cgo, no runtime dependencies.

## Use it as a command

```sh
# Connect, stay 15s, leave.
understudy-client -addr localhost:25565 -username Probe

# Stay until interrupted, and expose the control API on :8181.
understudy-client -addr localhost:25565 -username Probe -hold 0 -control 8181
```

| flag | meaning |
|---|---|
| `-addr` | server `host:port` (default `127.0.0.1:25565`) |
| `-username` | offline-mode player name (default `Understudy`) |
| `-version` | protocol version to speak; default auto-detects by pinging |
| `-versions` | list supported versions and exit |
| `-hold` | how long to stay after joining; `0` means until interrupted |
| `-control` | serve the HTTP control API on this port or `host:port` |
| `-no-idle-position` | stop reporting position while standing still |
| `-no-respawn` | stay dead instead of respawning |
| `-debug`, `-trace` | debug logging; log every clientbound packet ID |

Supported versions: **26.1**, **1.21.11**, **1.21.4**. Adding one is a
generator run, not a code change — see [Adding a version](#adding-a-version).

## Drive it over HTTP

With `-control`, the bot exposes a small JSON API so a test suite in any
language can drive it. There is **no authentication** — bind it to loopback.

```sh
curl localhost:8181/state
curl -X POST localhost:8181/hold  -d '{"item":"diamond_pickaxe"}'
curl -X POST localhost:8181/dig   -d '{"X":10,"Y":64,"Z":10,"hold_ms":1500}'
curl -X POST localhost:8181/place -d '{"X":10,"Y":64,"Z":10,"face":1,"verify":true}'
```

**Reading** — `GET /state` `/inventory` `/block` `/ground` `/reach`
`/lookingat` `/entities`

**Acting** — `POST /look` `/lookat` `/move` `/walk` `/fall` `/slot` `/hold`
`/drop` `/sneak` `/equip` `/interact` `/consume` `/shoot` `/craft` `/swing`
`/dig` `/diglook` `/attack` `/place` `/use`

**Containers** — `GET /container`, and `POST /container/`{`open`, `close`,
`click`, `take`, `put`, `clear`, `button`, `craft`, `grid`, `trade`, `deposit`,
`withdraw`}

**Workstations** — `POST /smelt` `/rename` `/anvil` `/loom` `/grindstone`
`/smith` `/enchant` `/brew`

`/dig` takes either one block or a `blocks` array, and reports how many it
actually broke.

### Containers and workstations

Open a block's UI, act on it, and read back what the server actually did:

```sh
# Craft banners at a table — six wool and a stick in a 3x3, three times over.
curl -X POST localhost:8181/container/open -d '{"X":10,"Y":64,"Z":10,"face":1}'
curl -X POST localhost:8181/container/grid -d '{"layout":{"1":"white_wool",
  "2":"white_wool","3":"white_wool","4":"white_wool","5":"white_wool",
  "6":"white_wool","8":"stick"},"repeat":3}'

# Smelt, at a furnace, blast furnace or smoker.
curl -X POST localhost:8181/smelt -d '{"input":"raw_iron","fuel":"coal","count":8}'

# Read a merchant's offers, then trade by what it produces rather than by index.
curl -X POST localhost:8181/container/open  -d '{"type":"villager"}'
curl localhost:8181/trades
curl -X POST localhost:8181/container/trade -d '{"item":"bread","times":10}'
```

Offers come back decoded, including the spent ones — a test for lockout needs
to see them rather than have them filtered away:

```json
{"index":0,"input":"minecraft:emerald","input_count":1,
 "output":"minecraft:bread","count":6,"uses":0,"max_uses":4,"available":true}
{"index":1,"input":"minecraft:emerald","input2":"minecraft:wheat",
 "output":"minecraft:golden_carrot","uses":5,"max_uses":5,"available":false}
```

Trading a locked-out offer is refused *before* the packet goes out, naming the
reason — a villager that has run out accepts the trade and silently does
nothing, so the alternative symptom is an unexplained timeout.

Every one reports what *actually* happened rather than what was asked for. A
full chest keeps the remainder, a villager runs out of stock, a furnace given
something unsmeltable produces nothing — and the server reports none of that,
so the client observes the outcome instead of assuming the packet worked.

Storage capacity comes from the window, so a double chest (54 slots), a copper
or trapped chest, a shulker box, a hopper and a chest minecart all work with no
special case. Blocks that only look like containers — a fletching table, a
composter, an empty lectern — open nothing, and say so rather than hanging.

## Use it as a library

```go
package main

import (
	"context"
	"log"
	"time"

	"github.com/blocktopia/understudy-client/understudy"
)

func main() {
	c, err := understudy.New(understudy.Options{
		Address:  "127.0.0.1:25565",
		Username: "Probe",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	if _, err := c.HoldItem("minecraft:diamond_pickaxe"); err != nil {
		log.Fatal(err)
	}
	if err := c.DigBlock(ctx, 10, 64, 10, 1, 1500*time.Millisecond); err != nil {
		log.Fatal(err)
	}
}
```

The `Client` refuses what a real client could not do rather than sending a
packet the server will ignore: an out-of-reach block, a swing at something
behind a wall, an attack on a version with no attack packet. Those come back as
errors that name the distance or the obstruction, because a silent no-op is the
single most expensive failure mode in a test harness.

## Layout

```
understudy/          the Client: session, movement, digging, inventory, combat
protocol/            the wire format: framing, primitives, chunks, versions
  versions/          generated per-version tables (~9,700 lines)
internal/geom/       raycasting and block geometry
internal/ballistics/ projectile arcs
internal/world/      the chunk and block-state store
internal/entities/   the entity tracker
internal/inventory/  slots and item stacks
internal/control/    the HTTP control API
internal/nbt/        the subset of NBT the client reads
internal/gen/        the version-table generator
cmd/understudy-client/
```

Each `internal/` package is pure and independently testable — that is why they
are separate. `protocol` knows nothing about playing the game; `understudy`
knows nothing about HTTP.

## Adding a version

Packet IDs are dense indices that shift whenever Mojang inserts a packet, so
they are generated rather than written:

```sh
npm pack minecraft-data && tar xf minecraft-data-*.tgz
node internal/gen/genversion.mjs package/minecraft-data/data \
     1.21.11 protocol/versions/version_1_21_11.go
gofmt -w protocol/versions
go test ./protocol/...
```

The tests check that each table registers itself, that every packet the client
needs is present rather than absent, and that item and entity names resolve —
a table that quietly lost a packet ID would otherwise fail silently at runtime.

## Development

```sh
make check     # fmt, vet, lint, and the tests under -race
make test
```

Tests are hermetic: sessions run against a `net.Pipe` fake server, so the suite
needs no Minecraft server and no network.

## Caveats

- **Movement is dead reckoning.** `WalkTo` walks a straight line and knows
  nothing about walls, drops or water. There is no pathfinding.
- **Teleports are treated as absolute.** The relative-teleport flags in the
  position packet are ignored.
- **Offline mode only.** No Mojang authentication and no encryption, so it
  cannot join an online-mode server.
- **The control API is unauthenticated.** Loopback only.

## Licence

MIT.
