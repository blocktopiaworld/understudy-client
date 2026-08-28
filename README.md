# understudy-client

A headless Minecraft client, written in Go, that speaks the Java Edition wire
protocol directly — no game, no rendering, no mod loader. It connects as an
ordinary offline-mode player and can mine, place, craft, eat, shoot, fall and
fight.

It exists for **automated in-game testing**. If you maintain a server plugin or
datapack and want to assert that playing the game produces the effect you
intended, you need something that plays the game. A bot that issues commands
proves nothing: `/setblock` does not fire the events a player mining a block
fires, and a statistic that only a real interaction increments will not move.

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

## Supported versions

**Minecraft 1.21.11 and newer**, on Paper, Fabric and vanilla. One binary
carries every supported version and picks by the server-list ping, so there is
nothing to configure. `understudy-client -versions` lists what your build
speaks.

Support is measured per version rather than assumed to carry across, because
payload shapes move between releases. A version whose encodings have not been
measured refuses to decode rather than reading them at the wrong offsets and
desynchronising quietly — which is why 1.21.4 tables ship but 1.21.4 is not a
supported target.

## Install

```sh
go install github.com/blocktopiaworld/understudy-client/cmd/understudy-client@latest
```

Go 1.26 or newer. No cgo, no runtime dependencies, and the module needs only the
standard library — `go.sum` is empty and there is no supply chain to audit.

## Run it

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

## Drive it over HTTP

With `-control`, a test suite in any language can drive the bot over JSON.

```sh
curl localhost:8181/state
curl -X POST localhost:8181/hold -d '{"item":"diamond_pickaxe"}'
curl -X POST localhost:8181/dig  -d '{"X":10,"Y":64,"Z":10,"hold_ms":1500}'
```

Reading, movement, digging, placing, combat, containers and workstations are
all covered. **[Full endpoint reference →](docs/wiki/api/)**

There is no authentication. Bind it to loopback.

## Use it as a library

```go
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
err = c.DigBlock(ctx, 10, 64, 10, 1, 1500*time.Millisecond)
```

The `Client` refuses what a real client could not do rather than sending a
packet the server will ignore: an out-of-reach block, a swing at something
behind a wall, an attack on a version with no attack packet. Those come back as
errors that name the distance or the obstruction, because a silent no-op is the
single most expensive failure mode in a test harness.

## Caveats

- **Movement is dead reckoning.** `WalkTo` walks a straight line and knows
  nothing about walls, drops or water. There is no pathfinding.
- **Teleports are treated as absolute.** The relative-teleport flags in the
  position packet are ignored.
- **Offline mode only.** No Mojang authentication and no encryption, so it
  cannot join an online-mode server.
- **The control API is unauthenticated.** Loopback only.

## Contributing

```sh
make check     # fmt, vet, lint, and the tests under -race
```

Tests are hermetic: sessions run against a `net.Pipe` fake server, so the suite
needs no Minecraft server and no network. See [CONTRIBUTING.md](CONTRIBUTING.md),
and [Adding a version](docs/wiki/Adding-a-Version.md) for the generator runs a
new Minecraft release needs.

## How this is built

This project is written with heavy use of AI, under human direction and review.
We think that is worth saying plainly rather than leaving you to guess.

It also shapes how the code is checked. Nothing version-specific here is written
from memory: protocol tables are generated from the server's own output, payload
encodings are measured against a running server, and the status table above
comes from a suite that asks the server what happened rather than asking the
client what it thinks it did.

## Licence

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).

Commercial use is explicitly allowed, including in closed-source products. The
grant covers use, modification, sublicensing and distribution, with no
obligation to publish your changes. Keep the licence text, the NOTICE and the
copyright notices, and say if you significantly changed a file.

It does not grant rights to the Blocktopia name or marks (section 6). Build what
you like on this; just do not brand it as ours.

## Credits

Protocol tables are generated from [minecraft-data][md] (MIT) and from the
Minecraft server's own `--reports` output — see [NOTICE](NOTICE) for exactly
which parts come from where.

Minecraft is a trademark of Mojang Synergies AB. This project is not affiliated
with, endorsed by, or connected to Mojang or Microsoft.

[md]: https://github.com/PrismarineJS/minecraft-data
