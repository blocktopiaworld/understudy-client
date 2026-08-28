# Items

Item names may be given bare (`bread`) or namespaced (`minecraft:bread`).

---

## `POST /slot`

```json
{"slot": 0}
```

Selects a hotbar slot, 0-8. The low-level form of `/hold`.

---

## `POST /hold`

```json
{"item": "minecraft:diamond_pickaxe"}
```

Finds an item anywhere in the inventory, moves it to the hotbar if needed, and
selects it. Returns `found_in_slot`, `held_slot` and `held_item`.

Fails with `409` if the item is not there — which is the useful answer, since
the alternative is digging with a fist and wondering why it takes so long.

---

## `POST /equip`

```json
{"item": "minecraft:diamond_helmet"}
```

Wears armour rather than holding it. Returns `item` and `from_slot`.

---

## `POST /drop`

```sh
curl -X POST localhost:8181/drop
curl -X POST localhost:8181/drop -d '{"all":true}'
```

Drops the held item. `all` drops the whole stack instead of one.

---

## `POST /consume`

```json
{"item": "minecraft:bread"}
```

Eats or drinks, and waits for the effect. Returns `health`, `food` and the
`health_gained`/`food_gained` deltas, so a test can assert on the change rather
than on the absolute.

---

## `POST /craft`

```sh
curl -X POST localhost:8181/craft -d '{"layout":{"1":"stick","4":"stick"}}'
```

Crafts in the 2x2 inventory grid. `layout` maps slot number to item name.
Returns `crafted` and `count`.

For a 3x3, open a crafting table and use
[`/container/grid`](containers.md#post-containergrid).
