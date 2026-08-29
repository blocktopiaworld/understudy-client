---
title: HTTP control API
has_children: true
nav_order: 2
---

Start the bot with `-control` and it serves a JSON API, so a test suite in any
language can drive it.

```sh
understudy-client -addr localhost:25565 -username Probe -hold 0 -control 8181
```

There is **no authentication**. Bind it to loopback.

Field names are matched case-insensitively, so `{"x": 10}` and `{"X": 10}` are
the same request. The captured examples below show whichever spelling the call
that produced them used; the OpenAPI document names the lowercase one, and a
caller that follows it is always accepted.

Prefer a reference to a guide? The same surface is published as
[OpenAPI](../reference/), generated from the server's own route table — so the
request shapes there are the ones the server accepts, by construction. The
worked examples are here.

## Pages

| Page | Endpoints |
| --- | --- |
| [Reading the world](reading.md) | `/state` `/inventory` `/block` `/ground` `/reach` `/lookingat` `/entities` `/recipes` |
| [Aiming and moving](movement.md) | `/look` `/lookat` `/move` `/walk` `/fall` `/sneak` |
| [Blocks](blocks.md) | `/dig` `/diglook` `/place` `/use` |
| [Items](items.md) | `/slot` `/hold` `/equip` `/drop` `/consume` `/craft` |
| [Combat](combat.md) | `/attack` `/swing` `/shoot` `/interact` `/interactat` |
| [Containers](containers.md) | `GET /container` `/trades`, `POST /container/*` |
| [Workstations](workstations.md) | `/smelt` `/anvil` `/rename` `/loom` `/grindstone` `/smith` `/enchant` `/brew` `/cartography` `/beacon` |

Runnable versions of most of this are in [examples/](../../examples/).

Every request and response block in these pages was captured from a live
Minecraft 26.2 server, not written by hand. If a sample looks odd, it is
because the server really answered that way.

## The response envelope

Every `POST` that succeeds returns `200` with where the bot ended up, plus
whatever that verb measured:

```json
{
  "ok": true,
  "pitch": 29.248827,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

`GET` responses carry only their own fields, without the envelope.

## Errors

Two failure kinds, deliberately given different status codes, because a test
harness needs to tell them apart.

| Status | Meaning |
| --- | --- |
| `400` | You asked wrongly. Bad JSON, an unknown field, an unparseable coordinate. |
| `409` | You asked correctly and the world said no. Out of reach, dead, not in play, nothing to trade. |

Both carry `{"error": "..."}`. A `409` also carries whatever the verb managed
before it failed, so a partial dig still tells you how many blocks broke.

### Which kind of "no"

A `409` covers two situations that need opposite handling. A swing sent before
the spawn packet arrived clears on its own; an item the player does not hold
never will. So a `409` also carries a machine-readable pair:

```json
{
  "error": "understudy: no \"minecraft:elytra\" in inventory (0 slots known)",
  "reason": "no_such_item",
  "retryable": false
}
```

`reason` is a short stable code and `retryable` is whether the same call,
unchanged, could succeed later. A caller waiting on an action can stop the
moment it sees `false`, instead of retrying until its own timeout on something
that was never going to become true.

**Both are absent when the client has not classified the refusal.** That is
deliberate: a guess would be worse than a silence, because a wrong `retryable:
false` turns a passing test into a flaky failure. Treat an absent pair the way
you treated every refusal before these existed.

Unknown JSON fields are rejected rather than ignored, so a typo in a field name
is a `400` and not a silently skipped argument.

## What "it worked" means here

Every endpoint reports what *actually* happened rather than what was asked for.
The server accepts and silently ignores a great deal: a click on the wrong
window id, a block interaction during the post-teleport window, a trade with a
villager that has run out, an unsmeltable furnace input, a deposit into a full
container. None of that comes back as an error on the wire.

So the verbs here confirm through observed state instead. `traded` is counted
from the stock gained, `dug` from blocks that actually changed, `moved` from
what left the inventory. Where a number could be either the request or the
result, both are returned — `requested` alongside `traded`, and they are
allowed to differ.
