# Containers

Every window is laid out as `[the container's own slots][the player's 36]`, so
`own_slots == size - 36`. That one rule covers a single chest (27), a double
(54), a copper or trapped chest, a shulker box, a hopper and a chest minecart,
with nothing special-cased. `type` exists only to report what was opened.

Every response below was captured from a live 26.2 server.

## Opening what you need

Every `POST /container/*` verb needs a window open. Give it the position and
it opens the block itself:

| field | type | meaning |
| --- | --- | --- |
| `X`, `Y`, `Z` | int | the block to work at; all three or none |
| `face` | int | which face to click, 0–5; defaults to the one facing the bot |
| `at` | string | a merchant to open instead: the nearest entity of a type |
| `at_entity_id` | int | a merchant by exact id |

With no position, the verb uses whatever window is already open, which is how
it has always behaved. The window is left open afterwards, so a caller that
opens once and acts several times still works.

It cannot find the block for you. The version tables carry item names and block
*classification*, not block names, so "the nearest furnace" is not a question
this client can answer.


---

## `POST /container/open`

**Parameters** — give a block position or an entity type.

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `X`, `Y`, `Z` | int | one form | | the block to open |
| `face` | int | no | the face toward the bot | which face to click |
| `type` | string | one form | | the nearest entity of a type |

| field | type | meaning |
| --- | --- | --- |
| `window_id` | int | the server's id for this window |
| `type` | int | the window-type id |
| `title` | string | the window's title as the server sent it |
| `size` | int | total slots, container plus the player's 36 |
| `own_slots` | int | how many belong to the container |
| `target_id`, `target_type` | int, string | for the entity form |

### A block

```json
{
  "X": 5001,
  "Y": 100,
  "Z": 5000,
  "face": 1
}
```

```json
{
  "ok": true,
  "pitch": 48.2397,
  "size": 63,
  "title": "translate container.chest",
  "type": 2,
  "window_id": 1,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

### An entity

```json
{
  "type": "villager"
}
```

```json
{
  "ok": true,
  "pitch": 17.223436,
  "size": 39,
  "target_id": 32617,
  "target_type": "minecraft:villager",
  "title": "insertion $6f3ac823-39fa-4e46-9261-290a36a242c2 hover_event name translate  entity.minecraft.villager.farmer minecraft:villager action show_entity uuid translate  entity.minecraft.villager.farmer",
  "type": 19,
  "window_id": 8,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

A window existing is not a window being populated: the open packet and the
contents packet are separate, and acting the instant a window appears reads an
empty container. Reading `GET /container` back is the wait.

Blocks that only look like containers — a fletching table, a composter, an
empty lectern — open nothing at all. A timeout on those is correct, not a bug.

---

## `GET /container`

The open window's contents.

**Parameters** — none.

| field | type | meaning |
| --- | --- | --- |
| `open` | bool | whether anything is open |
| `window_id`, `kind`, `type`, `title` | | which window |
| `size`, `own_slots` | int | total slots, and how many are the container's |
| `items` | array | `slot`, `item`, `count` |
| `truncated` | bool | present when the list was cut for size |

```json
{
  "items": [
    {
      "count": 1,
      "item": "minecraft:netherite_pickaxe",
      "slot": 54
    },
    {
      "count": 63,
      "item": "minecraft:stone",
      "slot": 55
    },
    {
      "count": 7,
      "item": "minecraft:snowball",
      "slot": 56
    },
    {
      "count": 15,
      "item": "minecraft:oak_log",
      "slot": 58
    },
    {
      "count": 2,
      "item": "minecraft:golden_apple",
      "slot": 59
    },
    {
      "count": 4,
      "item": "minecraft:oak_planks",
      "slot": 62
    }
  ],
  "kind": "chest",
  "open": true,
  "own_slots": 27,
  "size": 63,
  "title": "translate container.chest",
  "truncated": false,
  "type": 2,
  "window_id": 1
}
```

---

## `POST /container/close`

**Parameters** — none.

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

The server tracks which window is open, and clicks against a stale window id
are accepted and ignored.

---

## `POST /container/take`

Shift-clicks a container slot into the player's inventory. Answers with the
envelope alone — read `GET /container` back if you need to know what moved.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `slot` | int | yes | the slot to take |

```json
{
  "slot": 0
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

**Shift-click is all-or-nothing** — it moves a whole stack. For exact counts,
use `/container/click` one item at a time.

---

## `POST /container/put`

Moves an item from the player's rows into a container slot.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `item` | string | yes | | what to move |
| `slot` | int | yes | | the destination slot |
| `one` | bool | no | false | move a single item instead of the stack |

```json
{
  "item": "minecraft:stone",
  "slot": 0
}
```

```json
{
  "count": 63,
  "item": "minecraft:stone",
  "ok": true,
  "pitch": 48.2397,
  "slot": 0,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

Searches start from the player's rows, never the whole window. Otherwise a
search for an ingredient finds the one just placed in the crafting grid and the
call picks its own work back up.

---

## `POST /container/click`

The raw click, for cases the helpers do not cover.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `slot` | int | yes | which slot |
| `button` | int | yes | 0 left, 1 right |
| `mode` | int | yes | 0 pickup, 1 quick-move, and the rest of the protocol's modes |

```json
{
  "slot": 0,
  "button": 0,
  "mode": 1
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

---

## `POST /container/deposit`

Moves items in bulk from the player into the container.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `item` | string | yes | | what to move |
| `count` | int | no | | how many |
| `all` | bool | no | false | move everything of that item |

The response shape follows which form you used. With `count`, it reports
`moved`, `requested` and `in_container`. With `all`, there is no requested
number to compare against, so it reports `stacks` — how many stacks moved.

```json
{
  "item": "minecraft:stone",
  "all": true
}
```

```json
{
  "ok": true,
  "pitch": 48.2397,
  "stacks": 5,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

---

## `POST /container/withdraw`

The reverse.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `item` | string | yes | what to move |
| `count` | int | no | how many |

| field | type | meaning |
| --- | --- | --- |
| `moved` | int | how many actually moved |
| `requested` | int | how many were asked for |
| `left_in_container` | int | what remains |

```json
{
  "item": "minecraft:stone",
  "count": 16
}
```

```json
{
  "left_in_container": 47,
  "moved": 16,
  "ok": true,
  "pitch": 48.2397,
  "requested": 16,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

`moved` and `requested` differ when the container is full or does not hold that
much. That is the answer, not an error: a full chest keeps the remainder and
the server says nothing about it.

---

## `POST /container/clear`

Empties the player's inventory into the open container.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `item` | string | no | | just this item |
| `count` | int | no | | how many |
| `all` | bool | no | false | everything |

```sh
curl -X POST localhost:8181/container/clear -d '{"all":true}'
```

Returns `stacks`, the number of stacks moved.

---

## `POST /container/craft`

Asks the server to craft from its own recipe book — one packet, and `all`
repeats until the ingredients run out.

**Parameters** — give `item` or `recipe`.

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `item` | string | one form | | what to craft, looked up in the book |
| `recipe` | int | one form | | a numeric recipe id from [`/recipes`](reading.md#get-recipes) |
| `all` | bool | no | false | repeat until the ingredients run out |

```json
{
  "item": "minecraft:stick",
  "all": false
}
```

```json
{
  "all": false,
  "item": "minecraft:stick",
  "ok": true,
  "pitch": 48.2397,
  "recipe": 1259,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

---

## `POST /container/grid`

Lays a recipe out by slot in an open crafting table and takes the result.
Preferred over `/container/craft` for hand-written tests: a layout is readable,
a numeric recipe id is not.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `layout` | object | yes | | slot number to item name |
| `repeat` | int | no | 1 | craft it this many times |

Slots are `"1"`–`"9"`, left to right and top to bottom.

```json
{
  "layout": {
    "1": "white_wool",
    "2": "white_wool",
    "3": "white_wool",
    "4": "white_wool",
    "5": "white_wool",
    "6": "white_wool",
    "8": "stick"
  },
  "repeat": 1
}
```

```json
{
  "count": 1,
  "item": "minecraft:white_banner",
  "ok": true,
  "pitch": 48.2397,
  "repeat": 1,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

---

## `POST /container/button`

Presses a numbered button: a stonecutter or loom selecting a recipe, an
enchanting table picking a level.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `button` | int | yes | which button |

```sh
curl -X POST localhost:8181/container/button -d '{"button":0}'
```

---

## `GET /trades`

The open merchant's offers, decoded.

**Parameters** — none.

| field | type | meaning |
| --- | --- | --- |
| `trades` | array | the offers |
| `index` | int | position in the list, which `/container/trade` takes |
| `input`, `input_count` | string, int | the first cost |
| `input2` | string | the second cost, when there is one |
| `output`, `count` | string, int | what it produces |
| `uses`, `max_uses` | int | how spent the offer is |
| `disabled` | bool | the server's own "not right now" |
| `available` | bool | `!disabled && uses < max_uses` |
| `xp` | int | villager experience granted |

```json
{
  "count": 1,
  "trades": [
    {
      "available": true,
      "count": 6,
      "disabled": false,
      "index": 0,
      "input": "minecraft:emerald",
      "input_count": 1,
      "max_uses": 12,
      "output": "minecraft:bread",
      "uses": 0,
      "xp": 1
    }
  ]
}
```

Spent offers are listed, not filtered away, because a test for lockout needs to
see them.

---

## `POST /container/trade`

**Parameters** — give `item` or `index`.

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `item` | string | one form | | select by what the trade produces |
| `index` | int | one form | | select by position in the offer list |
| `times` | int | no | 1 | how many trades to attempt |
| `raw` | bool | no | false | select the offer and stop, without taking the result |

| field | type | meaning |
| --- | --- | --- |
| `traded` | int | trades that completed, measured from the stock gained |
| `requested` | int | how many were asked for |
| `item`, `count` | string, int | what the offer produces |

```json
{
  "item": "bread",
  "times": 2
}
```

```json
{
  "item": "bread",
  "ok": true,
  "pitch": 17.223436,
  "requested": 2,
  "traded": 12,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

Selecting by output survives a villager whose offers are in a different order,
which selecting by index does not.

**`traded` may exceed `requested`.** Taking a merchant result is a shift-click,
and vanilla batches that into every use the villager has left and the player
can afford, so one request against a fully-stocked villager can complete twelve
trades. `times` is a floor, not a cap.

**A trade is not counted by the server until the result is taken.** Selecting
an offer makes the result appear, which reads as success and is not: no
statistic moves and no trade event fires. This endpoint always takes the
result, except under `raw`.

Trading a locked-out offer is refused *before* the packet goes out, naming the
reason. A villager that has run out accepts the trade and silently does
nothing, so the alternative symptom is an unexplained timeout.
