---
title: Reading the world
parent: HTTP control API
nav_order: 1
---

All `GET`. These answer from the client's own state and never wait on the
server, so they cost about 0.35 ms and are safe to poll.

They carry no response envelope — only their own fields.

Every response below was captured from a live 26.2 server.

---

## `GET /state`

Everything about the bot itself.

**Parameters** — none.

**Response**

| field | type | meaning |
| --- | --- | --- |
| `username` | string | the offline-mode name it joined with |
| `uuid` | string | derived from the username |
| `entity_id` | int | the server's id for this player, as other entities see it |
| `state` | string | `handshake`, `login`, `configuration` or `play` |
| `joined` | bool | true once it is in `play` |
| `x`, `y`, `z` | float | position |
| `yaw`, `pitch` | float | facing; yaw 0 is south, pitch −90 is straight up |
| `on_ground` | bool | what the bot last told the server |
| `health` | float | 0–20 |
| `food` | int | 0–20 |
| `dead` | bool | awaiting respawn |
| `deaths` | int | count since connecting |
| `held_slot` | int | 0–8, the hotbar index |
| `game_mode` | string | `survival`, `creative`, `adventure`, `spectator` |
| `effects` | array | active potion effects: `id`, `name`, `amplifier`, `duration` in ticks |
| `damageable` | bool | whether damage can land at all |
| `not_damageable_because` | string | present only when `damageable` is false |

```sh
curl localhost:8181/state
```

```json
{
  "damageable": true,
  "dead": false,
  "deaths": 0,
  "effects": [
    {
      "id": 21,
      "name": "minecraft:absorption",
      "amplifier": 0,
      "duration": 652
    }
  ],
  "entity_id": 32428,
  "food": 20,
  "game_mode": "survival",
  "health": 20,
  "held_slot": 5,
  "joined": true,
  "on_ground": true,
  "pitch": 3.5,
  "state": "play",
  "username": "ApiDocs",
  "uuid": "31a931ac-1afc-322a-87cb-685d1076b91d",
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

`damageable` exists because its absence cost real time. A working totem of
undying was reported as broken, and the cause was the player being in creative
with nothing anywhere saying so.

---

## `GET /inventory`

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `count` | string | no | ask about one item instead of listing everything |
| `want` | int | no | with `count`, ask whether that many would fit |

Item names may be bare (`bread`) or namespaced (`minecraft:bread`), and may
carry a block state or a component list — `wheat[age=7]`,
`potion[potion_contents={potion:"minecraft:water"}]`. This client knows an item
by its id, so the qualifier is dropped and the answer covers the id: every
potion, not only the water ones. When that happens the response says so, with
`matched_as` and `ignored_qualifier`, because giving that answer without saying
it is how a caller comes to trust a number that does not mean what they think.

### Everything carried

```sh
curl localhost:8181/inventory
```

| field | type | meaning |
| --- | --- | --- |
| `items` | array | `slot`, `id`, `name`, `count`, and `potion` when the stack is one |
| `held_item`, `held_slot` | string, int | what is in hand |
| `pickups`, `picked_up` | int, array | items collected since connecting |
| `truncated` | bool | present when the list was cut for size |

Slot numbers are the protocol's own: 36–44 is the hotbar, 9–35 the main rows,
5–8 armour, 45 the off-hand.

```json
{
  "held_item": "minecraft:golden_apple",
  "held_slot": 5,
  "items": [
    {
      "slot": 36,
      "id": 971,
      "name": "minecraft:netherite_pickaxe",
      "count": 1,
      "potion": -1
    },
    {
      "slot": 37,
      "id": 1,
      "name": "minecraft:stone",
      "count": 64,
      "potion": -1
    },
    {
      "slot": 38,
      "id": 1044,
      "name": "minecraft:snowball",
      "count": 8,
      "potion": -1
    },
    {
      "slot": 39,
      "id": 998,
      "name": "minecraft:diamond_helmet",
      "count": 1,
      "potion": -1
    },
    {
      "slot": 40,
      "id": 161,
      "name": "minecraft:oak_log",
      "count": 16,
      "potion": -1
    },
    {
      "slot": 41,
      "id": 1014,
      "name": "minecraft:golden_apple",
      "count": 4,
      "potion": -1
    }
  ],
  "picked_up": 0,
  "pickups": {},
  "truncated": false
}
```

### One item

```sh
curl 'localhost:8181/inventory?count=minecraft:stone'
```

| field | type | meaning |
| --- | --- | --- |
| `total` | int | how many are carried anywhere |
| `storage_only` | int | how many are outside the hotbar |
| `free_slots` | int | empty slots left |
| `stack_size` | int | the item's own stack limit |
| `slots_needed` | int | slots `want` would occupy |
| `fits` | bool | whether `want` would physically fit |

```json
{
  "fits": true,
  "free_slots": 30,
  "item": "minecraft:stone",
  "slots_needed": 1,
  "stack_size": 64,
  "storage_only": 64,
  "total": 64,
  "want": 1
}
```

### Would this many fit

```sh
curl 'localhost:8181/inventory?count=dirt&want=2304'
```

```json
{
  "fits": true,
  "free_slots": 30,
  "item": "dirt",
  "slots_needed": 36,
  "stack_size": 64,
  "storage_only": 0,
  "total": 0,
  "want": 2304
}
```

2304 is 36 stacks of 64, so it fits only in a completely empty inventory.

---

## `GET /block`

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `x`, `y`, `z` | int | yes | the block position |

| field | type | meaning |
| --- | --- | --- |
| `state` | int | the raw block-state id |
| `loaded` | bool | whether the chunk has arrived |
| `solid`, `water`, `lava`, `air` | bool | classification of that state |
| `targetable` | bool | whether a dig could break it |

```sh
curl 'localhost:8181/block?x=5006&y=100&z=5000'
```

```json
{
  "air": false,
  "lava": false,
  "loaded": true,
  "solid": true,
  "state": 1,
  "targetable": true,
  "water": false,
  "x": 5006,
  "y": 100,
  "z": 5000
}
```

**Check `loaded` before trusting anything else.** An unsent chunk reads as air:

```sh
curl 'localhost:8181/block?x=0&y=200&z=0'
```

```json
{
  "air": true,
  "lava": false,
  "loaded": false,
  "solid": false,
  "state": 0,
  "targetable": false,
  "water": false,
  "x": 0,
  "y": 200,
  "z": 0
}
```

`air: true, solid: false` there means "nothing has been sent", not "there is
nothing there". Treating the two as the same is the single most expensive
mistake against this API, and is what once made a bot hover in place until the
server kicked it for flying.

---

## `GET /ground`

Where the floor is beneath the bot, and whether that is known at all.

**Parameters** — none.

| field | type | meaning |
| --- | --- | --- |
| `known` | bool | whether the column below has been sent |
| `found` | bool | whether a floor was found in it |
| `ground_y` | float | the height of the surface stood on, not the index of the block below it |
| `y` | float | the bot's own height |
| `gap` | float | blocks of air between the two |
| `in_water`, `in_lava`, `submerged` | bool | fluid state |
| `chunks` | int | how many chunks the client currently holds |

```sh
curl localhost:8181/ground
```

```json
{
  "chunks": 159,
  "found": true,
  "gap": 0,
  "ground_y": 100,
  "in_lava": false,
  "in_water": false,
  "known": true,
  "submerged": false,
  "y": 100
}
```

A floor of blocks at y=99 gives `ground_y: 100`: the number is the top face
they present, which is also where a bot standing on them is, hence `gap: 0`.

`known: false` is not `found: false`. The first means ask again, the second
means there is nothing there. **After a teleport, wait on `known` rather than
on the coordinate** — chunks trail the position, and the coordinate arrives
first.

---

## `GET /reach`

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `x`, `y`, `z` | int | yes | the block position |

| field | type | meaning |
| --- | --- | --- |
| `distance` | float | eye to block centre |
| `reach` | float | the limit the server enforces |
| `can` | bool | whether the block is within it |

```sh
curl 'localhost:8181/reach?x=5006&y=100&z=5000'
```

```json
{
  "can": false,
  "distance": 5.534835137562817,
  "reach": 4.5
}
```

```sh
curl 'localhost:8181/reach?x=5040&y=100&z=5000'
```

```json
{
  "can": false,
  "distance": 39.50486552312259,
  "reach": 4.5
}
```

Asking first turns a silently-ignored packet into a decision you made.

---

## `GET /lookingat`

Raycasts from the eye along the current facing.

**Parameters** — none.

| field | type | meaning |
| --- | --- | --- |
| `hit` | bool | whether anything was struck |
| `x`, `y`, `z` | int | the block, when `hit` |
| `state` | int | its block-state id |
| `face` | int | which face was struck, 0–5 |
| `distance` | float | how far away it is |

```sh
curl -X POST localhost:8181/look -d '{"block":{"x":5006,"y":100,"z":5000}}'
curl localhost:8181/lookingat
```

```json
{
  "hit": false
}
```

Face numbering is the protocol's: 0 down, 1 up, 2 north, 3 south, 4 west,
5 east.

---

## `GET /entities`

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `type` | string | no | keep only this entity type |
| `radius` | float | no | keep only those within this distance |

| field | type | meaning |
| --- | --- | --- |
| `count` | int | how many matched |
| `entities` | array | `id`, `type_name`, `x`, `y`, `z`, `distance` |

```sh
curl 'localhost:8181/entities?type=zombie&radius=12'
```

```json
{
  "count": 1,
  "entities": [
    {
      "distance": 3.1622776601683795,
      "id": 32632,
      "type_name": "minecraft:zombie",
      "x": 5003.5,
      "y": 100,
      "z": 5001.5
    }
  ]
}
```

`id` is what `/attack`, `/interact` and `/interactat` take as `entity_id`.
Targeting by id rather than by type is the difference between feeding the
animal you meant and feeding whichever one happens to be nearest — and
"nearest" is three-dimensional, so an entity forty blocks straight down can
beat one two blocks away.

---

## `GET /recipes`

What the **player's unlocked** recipe book holds for an item — not the server's
catalogue.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `item` | string | yes | the item to look up |

| field | type | meaning |
| --- | --- | --- |
| `found` | bool | whether the player's book has a recipe for it |
| `item` | string | what was asked for |
| `recipe` | int | the recipe id, for `/container/craft` |
| `known` | int | recipes decoded from the book so far |
| `missing` | int | entries that did not decode |

```sh
curl 'localhost:8181/recipes?item=stick'
```

```json
{
  "found": true,
  "item": "stick",
  "known": 108,
  "missing": 0,
  "recipe": 1259
}
```

A fresh survival player has almost nothing unlocked, so a bot that just joined
answers `found: false` for sticks — a recipe that obviously exists. That reads
as a decoding bug and is not one. Unlock the book first if you need all of it:

```
recipe give <player> *
```

`known` climbs as recipes unlock, and the 108 above is a partly-unlocked book
rather than the whole catalogue.

`missing` is separate, and is not zero on every version. It reports how much of
what *was* sent failed to decode, so a half-read book cannot be mistaken for a
small one.
