# HTTP control API

Start the bot with `-control` and it serves a JSON API, so a test suite in any
language can drive it.

```sh
understudy-client -addr localhost:25565 -username Probe -hold 0 -control 8181
```

There is **no authentication**. Bind it to loopback.

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

## The response envelope

Every `POST` that succeeds returns `200` with where the bot ended up, plus
whatever that verb measured:

```json
{"ok": true, "x": 10.5, "y": 64, "z": 10.5, "yaw": 0, "pitch": 0}
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
