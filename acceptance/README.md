# Acceptance

Runs the client against a real server and asserts every result through RCON.

That last part is the whole point. A test that asks the client whether it
placed a block is asking the accused; the question is whether the *server*
thinks the block is there. Every check here reads the server's own view — via
RCON commands, `execute if block`, `data get entity` — and the client's report
is only ever used to decide when to look.

## Running it

The client must be built (`make build`), and the server must have RCON enabled
and be willing to accept an offline-mode connection.

```sh
GAME=127.0.0.1:25565 RCON=127.0.0.1:25575 PW=secret python3 acceptance/exercise.py
```

| variable | meaning | default |
| --- | --- | --- |
| `GAME` | host:port the client connects to | `127.0.0.1:37325` |
| `RCON` | host:port for RCON | `127.0.0.1:49959` |
| `PW` | RCON password | `understudy` |
| `BOT` | path to the built binary | `../understudy-client` |
| `BOT_NAME` | username to join as | `Exercise` |
| `CPORT` | port for the client's control API | `9300` |

It carves a small arena, does its work, and forceloads/unloads around itself. It
does not clean up the arena blocks, so point it at a scratch world.

## What it covers

Connecting, terrain arriving after a teleport, block reads, placing, digging,
the recipe book, crafting by name, chest deposit and withdraw, furnace smelting,
entity tracking, attacking, and auto-fall — eighteen checks.

## Waiting, and why there are no sleeps

Every wait here is on a condition, never a duration. RCON is synchronous, so the
server has already applied a command by the time it answers, and the client sees
the resulting packets in about twenty milliseconds. An earlier version of this
harness was built on fixed sleeps and spent literally all of its runtime asleep:
6.8 seconds elapsed, 6.8 seconds of `sleep`. Converted, the same work takes 0.6.

Three specific waits are load-bearing, and each one is a bug this harness had:

- **Wait for terrain, not just position.** Chunks trail a teleport. Asking about
  the world immediately after arriving asks about a world the client has not
  been sent, and it will honestly answer that it does not know.
- **`/inventory` is stale while a container is open.** The container view is the
  truth then; the player's own view catches up when it closes.
- **Wait for an item to appear before holding it.** RCON put it in the player's
  inventory server-side; the client learns a packet later, and holding what is
  not there yet leaves the bot swinging its fist.

## Adding a check

Assert through RCON. If a check can pass while the server disagrees, it is not a
check — an early version of this file had `dig removes it again` passing because
`place` was silently failing and there was nothing left to dig.
