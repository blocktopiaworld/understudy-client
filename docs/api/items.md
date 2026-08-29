---
title: Items
parent: HTTP control API
nav_order: 4
---

Item names may be bare (`bread`) or namespaced (`minecraft:bread`).

Every response below was captured from a live 26.2 server.

---

## `POST /slot`

Selects a hotbar slot. The low-level form of `/hold`.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `slot` | int | yes | hotbar index, 0–8 |

```json
{
  "slot": 0
}
```

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

---

## `POST /hold`

Finds an item anywhere in the inventory, moves it to the hotbar if it is not
already there, and selects it.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `item` | string | yes | what to hold |

| field | type | meaning |
| --- | --- | --- |
| `found_in_slot` | int | where it was before |
| `held_slot` | int | the hotbar index now selected |
| `held_item` | string | what is in hand |

```json
{
  "item": "minecraft:netherite_pickaxe"
}
```

```json
{
  "found_in_slot": 36,
  "held_item": "minecraft:netherite_pickaxe",
  "held_slot": 0,
  "ok": true,
  "pitch": 16.348303,
  "x": 5000.5,
  "y": 100,
  "yaw": -95.19443,
  "z": 5000.5
}
```

Not carrying it is a `409`, and the error says how much of the inventory the
client has actually seen — a distinction that matters right after joining:

```json
{
  "item": "minecraft:elytra"
}
```

`409`

```json
{
  "error": "understudy: no \"minecraft:elytra\" in inventory (6 slots known)"
}
```

That is the useful answer. The alternative is digging with a fist and wondering
why everything takes so long.

---

## `POST /equip`

Wears armour rather than holding it.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `item` | string | yes | the piece to wear |

| field | type | meaning |
| --- | --- | --- |
| `item` | string | what was equipped |
| `from_slot` | int | where it came from |

```json
{
  "item": "minecraft:diamond_helmet"
}
```

```json
{
  "from_slot": 39,
  "item": "minecraft:diamond_helmet",
  "ok": true,
  "pitch": 29.248827,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

---

## `POST /drop`

Drops the held item.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `all` | bool | no | false | drop the whole stack instead of one |

An empty body drops one.

| field | type | meaning |
| --- | --- | --- |
| `item` | string | what was in hand |
| `dropped` | int | how many went |
| `all` | bool | which form was used |

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

---

## `POST /consume`

Eats or drinks, and waits for the effect.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `item` | string | yes | what to consume |

| field | type | meaning |
| --- | --- | --- |
| `health`, `food` | float, int | the values afterwards |
| `health_gained`, `food_gained` | float, int | the change |

```json
{
  "item": "minecraft:golden_apple"
}
```

```json
{
  "food": 20,
  "food_gained": 0,
  "health": 20,
  "health_gained": 3,
  "ok": true,
  "pitch": 29.248827,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

The deltas are there so a test can assert on the change rather than on the
absolute, which is what you almost always mean.

**Ordinary food is refused at full hunger** — that is vanilla, not this client
— and the error says so rather than timing out. A golden apple is always
edible, which is why the sample above gains health and no food. If you need an
ordinary food to be eaten, make the bot hungry first.

---

## `POST /craft`

Crafts in the 2×2 inventory grid.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `layout` | object | yes | slot number to item name |

Slots are `"1"`–`"4"`, left to right and top to bottom.

| field | type | meaning |
| --- | --- | --- |
| `crafted` | string | what came out |
| `count` | int | how many |

```json
{
  "layout": {
    "1": "oak_log"
  }
}
```

```json
{
  "count": 4,
  "crafted": "minecraft:oak_planks",
  "ok": true,
  "pitch": 29.248827,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

For a 3×3, open a crafting table and use
[`/container/grid`](containers.md#post-containergrid).
