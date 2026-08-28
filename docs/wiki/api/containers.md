# Containers

Every window is laid out as `[the container's own slots][the player's 36]`. So
`own_slots == size - 36`, and that single rule covers a single chest (27), a
double (54), a copper or trapped chest, a shulker box, a hopper and a chest
minecart with nothing special-cased. `type` exists only to report what was
opened.

---

## `POST /container/open`

```sh
# A block.
curl -X POST localhost:8181/container/open -d '{"X":10,"Y":64,"Z":10,"face":1}'

# An entity: a villager, a chest minecart.
curl -X POST localhost:8181/container/open -d '{"type":"villager"}'
```

Returns `window_id`, `type`, `title`, `size`, and for the entity form
`target_id` and `target_type`.

A window existing is not the same as a window being populated: the open packet
and the contents packet are separate, and acting the instant a window appears
reads an empty container. `GET /container` after opening is the wait.

Blocks that only look like containers — a fletching table, a composter, an
empty lectern — open nothing. A timeout on those is correct, not a bug.

---

## `GET /container`

The open window's contents.

| field | meaning |
| --- | --- |
| `open` | whether anything is open |
| `window_id`, `kind`, `type`, `title` | which window |
| `size`, `own_slots` | total slots, and how many belong to the container |
| `items` | each with `slot`, `item`, `count` |

---

## `POST /container/close`

Closes it. The server tracks the open window, and clicks against a stale window
id are accepted and ignored.

---

## `POST /container/take`

```json
{"slot": 0}
```

Shift-clicks a slot into the player's inventory.

**Shift-click is all-or-nothing.** It moves a whole stack. For exact counts,
use `/container/click` one item at a time.

---

## `POST /container/put`

```json
{"item": "minecraft:coal", "slot": 1, "one": false}
```

Moves an item from the player's rows into a container slot. `one` moves a
single item instead of the stack.

Searches start from the player's rows, never the whole window — otherwise a
search for an ingredient finds the one just placed in the crafting grid and
picks its own work back up.

---

## `POST /container/click`

```json
{"slot": 0, "button": 0, "mode": 0}
```

The raw click, for cases the helpers do not cover. `mode` follows the
protocol's own click modes; `0` is pickup, `1` is quick-move.

---

## `POST /container/deposit` and `POST /container/withdraw`

```sh
curl -X POST localhost:8181/container/deposit -d '{"item":"cobblestone","all":true}'
curl -X POST localhost:8181/container/withdraw -d '{"item":"cobblestone","count":64}'
```

Move items in bulk between the player and the container. Deposit returns
`moved`, `requested` and `in_container`; withdraw returns `moved`, `requested`
and `left_in_container`.

`moved` and `requested` differ when the container is full or does not hold that
much. That is the answer, not an error — a full chest keeps the remainder and
the server says nothing about it.

---

## `POST /container/clear`

```json
{"item": "minecraft:cobblestone", "all": true}
```

Empties the player's inventory into the open container, either one item type or
everything.

---

## `POST /container/craft`

```json
{"item": "minecraft:white_banner", "all": true}
```

Asks the server to craft from its own recipe book — one packet, and `all`
repeats until the ingredients run out. `recipe` takes a numeric id instead if
you have one.

---

## `POST /container/grid`

```sh
curl -X POST localhost:8181/container/grid -d '{"layout":{
  "1":"white_wool","2":"white_wool","3":"white_wool",
  "4":"white_wool","5":"white_wool","6":"white_wool",
  "8":"stick"},"repeat":3}'
```

Lays a recipe out by slot in an open crafting table and takes the result.
Preferred over `/container/craft` for hand-written tests: a layout is readable,
a numeric recipe id is not. Returns `item`, `count` and `repeat`.

---

## `POST /container/button`

```json
{"button": 0}
```

Presses a numbered button: a stonecutter or loom selecting a recipe, an
enchanting table picking a level.

---

## `GET /trades`

The open merchant's offers, decoded — including the spent ones, because a test
for lockout needs to see them rather than have them filtered away.

```json
{"index":0,"input":"minecraft:emerald","input_count":1,
 "output":"minecraft:bread","count":6,"uses":0,"max_uses":4,"available":true}
{"index":1,"input":"minecraft:emerald","input2":"minecraft:wheat",
 "output":"minecraft:golden_carrot","uses":5,"max_uses":5,"available":false}
```

---

## `POST /container/trade`

```sh
# By what it produces — survives a villager whose offers are in a different order.
curl -X POST localhost:8181/container/trade -d '{"item":"bread","times":10}'

# By index.
curl -X POST localhost:8181/container/trade -d '{"index":0}'
```

| field | meaning |
| --- | --- |
| `item` | select by output |
| `index` | select by position in the offer list |
| `times` | how many trades to attempt; defaults to one |
| `raw` | select the offer and stop, for a caller inspecting the window itself |

Returns `traded` and `requested`.

**`traded` is measured from the stock gained, not from clicks**, and it may
exceed `requested`. Taking a merchant result is a shift-click, and vanilla
batches that into every use the villager has left and the player can afford. A
single request against a fully-stocked villager can complete twelve trades.

A trade is not counted by the server until the result is *taken*. Selecting an
offer makes the result appear, which reads as success and is not: no statistic
moves and no trade event fires. This endpoint always takes the result, except
under `raw`.

Trading a locked-out offer is refused *before* the packet goes out, naming the
reason. A villager that has run out accepts the trade and silently does
nothing, so the alternative symptom is an unexplained timeout.
