# Reading the world

All `GET`. These carry no response envelope — only their own fields.

Coordinates come from the query string: `?x=10&y=64&z=10`, all three required
and all integers.

---

## `GET /state`

Everything about the bot itself. Cheap, and safe to poll.

```sh
curl localhost:8181/state
```

| field | meaning |
| --- | --- |
| `username`, `uuid`, `entity_id` | identity as the server sees it |
| `state` | connection phase: `handshake`, `login`, `configuration`, `play` |
| `joined` | true once the bot is in `play` and has terrain |
| `x`, `y`, `z`, `yaw`, `pitch` | position and facing |
| `on_ground` | what the bot last told the server |
| `health`, `food` | 0-20 each |
| `dead`, `deaths` | current, and the count since connecting |
| `held_slot` | 0-8, the hotbar index |
| `game_mode` | `survival`, `creative`, `adventure`, `spectator` |
| `effects` | active potion effects |
| `damageable` | whether damage can land at all |
| `not_damageable_because` | present only when `damageable` is false |

`damageable` exists because its absence cost real time: a working totem of
undying was reported as broken, and the actual cause was the player being in
creative with nothing anywhere saying so.

---

## `GET /inventory`

```sh
curl localhost:8181/inventory
curl 'localhost:8181/inventory?count=minecraft:bread'
curl 'localhost:8181/inventory?count=dirt&want=2304'
```

Without a query, the full contents: `items` (each with `slot`, `item`,
`count`), `held_slot`, `held_item`, and `pickups`/`picked_up` for what has been
collected since connecting.

`?count=<item>` answers about one item and returns `total`, `storage_only`,
`free_slots`, `stack_size`, `slots_needed` and `fits`. Adding `?want=<n>` asks
whether that many would physically fit.

Item names may be given bare (`bread`) or namespaced (`minecraft:bread`).

---

## `GET /block?x=&y=&z=`

What the client believes is at a position, and what that means.

| field | meaning |
| --- | --- |
| `state` | the raw block-state id |
| `loaded` | whether the chunk has arrived |
| `solid`, `water`, `lava`, `air` | classification of that state |
| `targetable` | whether it is something a dig could break |

**Check `loaded` before trusting the rest.** An unsent chunk reads as air, and
treating "no data" as "no block" is the single most expensive mistake against
this API. It is what made a bot hover in place until the server kicked it for
flying.

---

## `GET /ground`

Where the floor is beneath the bot, and whether that is known at all.

| field | meaning |
| --- | --- |
| `known` | whether the column below has been sent |
| `found` | whether a floor was found within it |
| `ground_y` | the surface height |
| `gap` | blocks of air between the bot and it |
| `in_water`, `in_lava`, `submerged` | fluid state |
| `chunks` | how many chunks the client currently holds |

`known: false` is not `found: false`. The first means "ask again", the second
means "there is nothing there". After a teleport, wait on `known` rather than
on the coordinate: chunks trail the position.

---

## `GET /reach?x=&y=&z=`

Returns `distance`, the server's `reach` limit, and `can`. Asking first turns a
silently-ignored packet into a decision you made.

---

## `GET /lookingat`

Raycasts from the eye along the current facing. Returns `hit`, and when true
the block `x`, `y`, `z`, its `state`, the `face` struck and the `distance`.

---

## `GET /entities`

```sh
curl 'localhost:8181/entities?type=zombie&radius=16'
```

`type` filters by entity type, `radius` by distance from the bot. Returns
`count` and `entities`, each with `id`, `type_name`, `x`, `y`, `z` and
`distance`.

The `id` is what `/attack` and `/interact` take as `entity_id`. Targeting by id
rather than by type is the difference between feeding the animal you meant and
feeding whichever one is nearest.

---

## `GET /recipes?item=`

What the server's recipe book holds for an item. Returns `found`, the `recipe`
id, and `known`/`missing` counts for the book as a whole.

`missing` is not zero on every version. It reports how much of the book failed
to decode, so a half-read book cannot be mistaken for a small one.
