# Changelog

Notable changes. Dates are when the work landed, not when a tag was cut.

## Unreleased

Nothing yet.

### Added

- Twenty endpoints refused until the caller had opened the right window
  themselves, while the client already knew which window each one needed. Every
  workstation and every `POST /container/*` verb now takes the block to work at
  and opens it, turning three calls into one. Additive: with no position given
  the behaviour is exactly what it was, and the window is left open afterwards.
  Trading also takes `at` or `at_entity_id` for a merchant, which is an entity
  rather than a block.
- `/place` and `/shoot` take an optional `item` and hold it first, the way
  `/consume` always has.

### Changed

- A single-block `/dig` now reports `dug` like the batch form, so a caller
  reading that number does not have to know which form it sent.
- `/drop` says what it dropped and how many. It answered with nothing, which is
  how the stale-inventory bug above stayed invisible.
- `/place` says what it placed, which face it used and whether it verified.

## v0.1.0 — 2026-08-28

First tagged release, and the starting state rather than a diff from one.
Everything below describes what the client is, not what changed.

Speaks Minecraft 26.2, 26.1 and 1.21.11 in full, and decodes 1.21.4 components
without its recipe book. One binary carries all four and picks by the
server-list ping.

### Fixed

- Opening a merchant that had only just spawned failed about two thirds of the
  time. An entity is not ready to trade for a second or so after spawning, and
  a single interact sent inside that window is answered with silence, which
  surfaced as "the target may not have a UI" — a confident and wrong diagnosis
  of "not yet". Opening now repeats the interaction while it waits, and the
  timeout is five seconds rather than three. Six freshly summoned wandering
  traders opened two times in six before, and six in six after.


- Walking off a ledge left the bot standing in mid-air until the server kicked
  it for flying. `WalkTo` is dead reckoning at a constant height and gravity was
  never part of it, while auto-fall only covers a *teleport* into mid-air. Two
  different paths, and only one of them had been fixed — which is why the flying
  kick kept coming back. A walk that ends over a drop now falls, with real
  gravity and real fall damage.


- Dropping moved nothing in the client's own view. The server's `minecraft:drop`
  statistic incremented and the item landed on the ground, while `/inventory`
  went on reporting a full stack indefinitely. The server sends no slot update
  for a drop — a vanilla client predicts the change and is corrected only on
  disagreement — and this one waited to be told. Found by the conformance suite.


- `WalkTo` spun until the caller's context expired when something was in the
  way, then reported "context deadline exceeded" — sixty seconds of nothing,
  blamed on the timeout rather than on the wall. It now gives up after a second
  of getting no closer and says where it stopped and how far short it was.


- `POST /container/trade` completed no trade when asked for one. The `times == 1`
  path selected the offer and stopped, leaving the result in the merchant slot —
  the server counts a trade only when the result is taken, so no
  `traded_with_villager`, no trade event, nothing downstream. It also reported
  `"traded": 1` as a literal, so the response agreed with any assertion put to
  it. Every count now goes through `TradeAndTake`, and `traded` is measured from
  the stock gained.

### Protocol

- Speaks **26.2**, **26.1**, **1.21.11** and **1.21.4**. Support is per-version
  and measured, not assumed: component ids and payload encodings both move
  between versions, and each version carries its own tables. A version whose
  encodings have not been measured refuses to decode components and says so.
- Data components: 108 of the 111 types the servers register. The three left —
  `creative_slot_lock`, `additional_trade_cost`, `map_post_processing` — were
  each chased down the route that should produce them and appear not to reach
  an item at all.
- Recipe book decodes on 26.2, 26.1 and 1.21.11. On 1.21.4 it stops partway,
  and `MissingRecipes` reports the shortfall so a half-decoded book cannot be
  mistaken for a small one.

### Robustness

- Fuzz tests for the three decoders that read arbitrary server bytes. They
  found unbounded recursion through nested components and unbounded list
  lengths throughout; both are bounded now.
- Entity tracking survives a teleport: entries beyond any possible view
  distance are dropped rather than reported as current.
- An unloaded chunk is no longer read as "no ground". `Support.Known` is
  separate from `Support.Found`, because "I have not been sent the terrain" is
  not "there is no terrain" — the client claims to be standing rather than
  volunteering that it is airborne, which is what a server kicks for.

### Licence

Apache License 2.0. Commercial use, including in closed-source products, is
explicitly permitted; the Blocktopia name and marks are not granted.

### Verified against

Paper 26.2, Fabric 26.2, Fabric 26.1.2, and vanilla 26.2 / 1.21.11 / 1.21.4.
Every live assertion goes through RCON — the server's view, not the
client's self-report.
