---
title: Workstations
parent: HTTP control API
nav_order: 7
---

These place the inputs, press whatever needs pressing, wait for the result and
take it.

Most return the resulting `item` and `count`, and all fail with `409` when the
station produces nothing — which the server does not otherwise report.

`/brew` and `/beacon` are the exceptions: they answer with the envelope alone.
A `200` means the action completed, and there is no item to report because
brewing changes bottles in place and a beacon has no output.

Every response below was captured from a live 26.2 server.

## Opening what you need

Every endpoint on this page needs the right block open. Give it the position
and it opens the block itself:

| field | type | meaning |
| --- | --- | --- |
| `X`, `Y`, `Z` | int | the block to work at; all three or none |
| `face` | int | which face to click, 0–5; defaults to the one facing the bot |

With no position, the verb uses whatever window is already open, which is how
it has always behaved. The window is left open afterwards, so a caller that
opens once and acts several times still works.

It cannot find the block for you. The version tables carry item names and block
*classification*, not block names, so "the nearest furnace" is not a question
this client can answer.


---

## `POST /smelt`

Works at a furnace, blast furnace or smoker.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `input` | string | yes | | what to smelt |
| `fuel` | string | yes | | what to burn |
| `count` | int | no | 1 | how many to smelt |

```json
{
  "X": 7001, "Y": 100, "Z": 7000,
  "input": "raw_iron",
  "fuel": "coal",
  "count": 2
}
```

```json
{
  "count": 2,
  "item": "minecraft:iron_ingot",
  "ok": true,
  "pitch": 48.2397,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

It waits for the smelt rather than assuming it, so `count` is what came out. An
unsmeltable input produces nothing and no error on the wire; this reports it.

---

## `POST /anvil`

Combines two items.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `first` | string | yes | the item being upgraded |
| `second` | string | yes | what is applied to it |

```sh
curl -X POST localhost:8181/anvil \
  -d '{"first":"diamond_sword","second":"minecraft:enchanted_book"}'
```

---

## `POST /rename`

Renames at an anvil.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `item` | string | yes | what to rename |
| `name` | string | yes | the new name |

```json
{
  "item": "minecraft:diamond_sword",
  "name": "Understudy"
}
```

```json
{
  "count": 1,
  "item": "minecraft:diamond_sword",
  "ok": true,
  "pitch": 48.2397,
  "renamed_to": "Understudy",
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

---

## `POST /grindstone`

Strips enchantments.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `item` | string | yes | what to strip |

```json
{
  "item": "minecraft:diamond_sword"
}
```

```json
{
  "count": 1,
  "item": "minecraft:diamond_sword",
  "ok": true,
  "pitch": 48.2397,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

---

## `POST /enchant`

Enchants at a table.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `item` | string | yes | what to enchant |
| `level` | int | yes | which of the three offers to take, 1–3 |

`level` is the offer slot, not the resulting enchantment level.

```sh
curl -X POST localhost:8181/enchant -d '{"item":"diamond_sword","level":3}'
```

---

## `POST /smith`

Upgrades at a smithing table.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `template` | string | yes | the smithing template |
| `base` | string | yes | the item being upgraded |
| `addition` | string | yes | the material |

```sh
curl -X POST localhost:8181/smith -d '{
  "template":"netherite_upgrade_smithing_template",
  "base":"diamond_pickaxe",
  "addition":"netherite_ingot"}'
```

Note that the vanilla statistic `interact_with_smithing_table` counts *opening*
the table, not completing an upgrade. If you are asserting that something was
smithed, assert on the resulting item.

---

## `POST /loom`

Applies a banner pattern.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `banner` | string | yes | | the banner |
| `dye` | string | yes | | the dye |
| `pattern_item` | string | no | | for patterns that need a pattern item |
| `index` | int | no | 0 | which of the offered patterns to take |

```json
{
  "banner": "minecraft:white_banner",
  "dye": "minecraft:red_dye",
  "index": 0
}
```

```json
{
  "count": 1,
  "item": "minecraft:white_banner",
  "ok": true,
  "pitch": 48.2397,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

---

## `POST /brew`

Brews at a stand.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `bottle` | string | yes | | the bottles to brew into |
| `ingredient` | string | yes | | what to brew with |
| `fuel` | string | yes | | blaze powder |
| `count` | int | no | 1 | how many bottles |

```json
{
  "bottle": "minecraft:potion",
  "ingredient": "minecraft:nether_wart",
  "fuel": "minecraft:blaze_powder",
  "count": 1
}
```

```json
{
  "ok": true,
  "pitch": 48.2397,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

**The bottle must be a water bottle**, which is `minecraft:potion` carrying a
`potion_contents` component. A bare `minecraft:potion` is an empty potion item
and brews nothing, which the stand reports as silence and this reports as a
timeout naming the ingredient.

---

## `POST /cartography`

Extends, copies or locks a map.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `map` | string | yes | the map item |
| `applied` | string | yes | paper, an empty map, or a glass pane |

```sh
curl -X POST localhost:8181/cartography -d '{"map":"filled_map","applied":"paper"}'
```

---

## `POST /beacon`

Sets a beacon's effects.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `payment` | string | yes | the ingot or gem to pay with |
| `primary` | int | yes | the primary effect id |
| `secondary` | int | no | the secondary effect id |

Effect ids are the registry's own, the same numbers `effects` reports in
[`GET /state`](reading.md#get-state).

```sh
curl -X POST localhost:8181/beacon \
  -d '{"payment":"netherite_ingot","primary":1,"secondary":0}'
```
