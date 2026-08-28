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

Verified against real servers, not just unit tests. Every assertion goes
through RCON — the server's view, not the client's self-report, because "the
client believes it placed a block" is exactly the failure being tested for.

| Server | Verified |
| --- | --- |
| Paper 26.2 | acceptance sweep, components, recipe book, mining, reach |
| Fabric 26.2 | acceptance sweep, components, recipe book |
| Fabric 26.1.2 | acceptance sweep, components, recipe book |
| vanilla 26.2, 1.21.11, 1.21.4 | components; 1.21.11 recipe book |

Protocol support is per-version and not assumed to carry across. Data component
ids and payload shapes both move between versions — between 1.21.4 and 26.1
only five of sixty-seven component ids kept their number — so each version
carries its own tables and its own measured encodings. A version whose
encodings have not been measured refuses to decode components and says so,
rather than reading them at the wrong offsets and desynchronising quietly.

See [protocol/versions/doc.go](protocol/versions/doc.go) for what "supported"
means per version, and what adding one costs.

The acceptance harness that produces that table is in
[acceptance/](acceptance/) and takes the server as parameters, so pointing it
at your own is one line.

## Dependencies

None. The module requires only the Go standard library, so `go.sum` is empty
and there is no supply chain to audit.

## Install

```sh
go install github.com/block-topia/understudy-client/cmd/understudy-client@latest
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

Supported versions: **26.2**, **26.1**, **1.21.11**, **1.21.4**. Adding one is
mostly generator runs — see [Adding a version](#adding-a-version).

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

# Or ask the server to do it from its own recipe book — one packet, and it
# repeats until the ingredients run out.
curl localhost:8181/recipes?item=white_banner
curl -X POST localhost:8181/container/craft -d '{"item":"white_banner","all":true}'
curl -X POST localhost:8181/container/take  -d '{"slot":0}'

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

	"github.com/block-topia/understudy-client/understudy"
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
internal/gen/        the version-table and component-table generators
cmd/understudy-client/  the binary
acceptance/          the live-server harness the status table comes from
docs/                style, and a record of past defects and how each was found
scripts/             one-off maintenance, e.g. changing the module path
```

Each `internal/` package is pure and independently testable — that is why they
are separate. `protocol` knows nothing about playing the game; `understudy`
knows nothing about HTTP.

## Adding a version

Three things move between versions, and only the first is in minecraft-data.

**Packet ids, item and block tables.** Dense indices that shift whenever Mojang
inserts an entry, so they are generated:

```sh
npm pack minecraft-data && tar xf minecraft-data-*.tgz
node internal/gen/genversion.mjs package/minecraft-data/data \
     1.21.11 protocol/versions/version_1_21_11.go
```

If minecraft-data has not shipped the version yet — it had no 26.2 when 26.2
was added — build its input from the server's own reports first:

```sh
java -DbundlerMainClass=net.minecraft.data.Main -jar server.jar --reports
node internal/gen/reports-to-mcdata.mjs generated/reports 26.2 776 \
     package/minecraft-data/data 26.1
```

**Data component and slot display ids.** In no published dataset at all, so
they come from the server:

```sh
node internal/gen/gencomponents.mjs generated/reports/registries.json \
     26.2 protocol/versions/version_26_2_components.go
```

**Payload encodings.** These are measured against a running server, not
generated, and they are the part that cannot be skipped: knowing which id is
which does not tell you how the payload is laid out. 1.21.11 writes an item
nested in a component count-first where 26.1 writes it id-first. Fill in
`Components` only after checking, and never by copying another version's —
1.21.4 and 1.21.11 disagree with each other as much as either does with 26.1.

Leaving `Components` nil is a valid answer: components then refuse to decode on
that version and report why, which costs a partial inventory view. Guessing
costs a desynchronised one that reports nothing.

```sh
gofmt -w protocol/versions
go test ./protocol/... ./understudy/...
```

The tests check that each table registers itself, that every packet the client
needs is present rather than absent, that item and entity names resolve, and
that no version is left without component tables — a table that quietly lost an
id would otherwise fail silently at runtime.

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
