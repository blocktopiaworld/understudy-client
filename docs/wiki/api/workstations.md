# Workstations

Each of these needs the matching block open first, via
[`/container/open`](containers.md#post-containeropen). They take item names,
place the inputs, press what needs pressing, and take the result.

All of them return the resulting `item` and `count`, and fail with `409` when
the station produces nothing — which the server does not otherwise report.

---

## `POST /smelt`

```json
{"input": "raw_iron", "fuel": "coal", "count": 8}
```

Works at a furnace, blast furnace or smoker. Waits for the smelt rather than
assuming it, so `count` is what came out.

An unsmeltable input produces nothing and no error on the wire. This reports it.

---

## `POST /anvil`

```json
{"first": "diamond_sword", "second": "minecraft:sharpness_book"}
```

Combines two items.

---

## `POST /rename`

```json
{"item": "diamond_sword", "name": "Understudy"}
```

Renames at an anvil. Returns `renamed_to` alongside the item.

---

## `POST /grindstone`

```json
{"item": "diamond_sword"}
```

Strips enchantments.

---

## `POST /enchant`

```json
{"item": "diamond_sword", "level": 3}
```

Enchants at a table. `level` is which of the three offers to take, not the
resulting enchantment level.

---

## `POST /smith`

```json
{"template": "netherite_upgrade_smithing_template",
 "base": "diamond_pickaxe",
 "addition": "netherite_ingot"}
```

Upgrades at a smithing table.

Note that the vanilla statistic `interact_with_smithing_table` counts *opening*
the table, not completing an upgrade. If you are asserting that something was
smithed, assert on the resulting item.

---

## `POST /loom`

```json
{"banner": "white_banner", "dye": "red_dye",
 "pattern_item": "creeper_banner_pattern", "index": 0}
```

Applies a banner pattern. `pattern_item` is optional for patterns that need no
item; `index` picks from the offered list.

---

## `POST /brew`

```json
{"bottle": "water_bottle", "ingredient": "nether_wart",
 "fuel": "blaze_powder", "count": 3}
```

Brews at a stand.

---

## `POST /cartography`

```json
{"map": "filled_map", "applied": "paper"}
```

Extends, copies or locks a map.

---

## `POST /beacon`

```json
{"payment": "netherite_ingot", "primary": 1, "secondary": 0}
```

Sets a beacon's effects. `primary` and `secondary` are effect ids.
