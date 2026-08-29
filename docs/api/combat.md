---
title: Combat
parent: HTTP control API
nav_order: 5
---

Every response below was captured from a live 26.2 server.

---

## `POST /attack`

**Parameters** — give `entity_id` or `type`.

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `entity_id` | int | one form | | a specific entity, from [`/entities`](reading.md#get-entities) |
| `type` | string | one form | | the nearest entity of a type |
| `times` | int | no | 1 | how many swings |

| field | type | meaning |
| --- | --- | --- |
| `hits` | int | swings that landed |
| `requested` | int | swings asked for |
| `target_id`, `target_type` | int, string | what was hit |

```json
{
  "entity_id": 32632,
  "times": 2
}
```

```json
{
  "hits": 2,
  "ok": true,
  "pitch": 17.223436,
  "target_id": 32632,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

**Prefer `entity_id`.** Targeting by type hits whichever the client thinks is
nearest, and nearest is three-dimensional — an entity forty blocks straight
down can beat one two blocks away.

The attack is refused rather than sent when the target is out of reach or
behind a wall, and the error names the distance or the obstruction. A swing the
server would silently drop is worse than an error.

---

## `POST /swing`

Swings the arm at nothing in particular. Useful for animations and for the
statistics that count swings.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `hold_ms` | int | no | 0 | hold the swing |



```json
{
  "ok": true,
  "pitch": 17.223436,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

---

## `POST /shoot`

Draws a bow and looses at a target. The launch angle accounts for drop over the
distance, so this aims rather than points.

**Parameters** — give one target form.

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `x`, `y`, `z` | float | one form | | an exact point |
| `block` | object | one form | | `{x, y, z}`, aimed at the block's centre |
| `type` | string | one form | | the nearest entity of a type, aimed at body height |
| `draw_ms` | int | no | full draw | how long to draw |
| `item` | string | no | | the bow to draw; held first when given |

| field | type | meaning |
| --- | --- | --- |
| `draw_ms` | int | the draw actually held |
| `power` | float | 0–1, the resulting bow power |

```json
{
  "block": {
    "x": 5010,
    "y": 100,
    "z": 5000
  }
}
```

```json
{
  "draw_ms": 1000,
  "ok": true,
  "pitch": 3.5,
  "power": 1,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

Against a mob at 4, 8 and 16 blocks it lands three arrows in four, the fourth
having nothing left to hit.

**Two things make bow tests look broken when they are not.** The arrow is gone
before you can look for it, so assert on the target's health rather than on a
projectile entity existing. And an RCON selector without explicit coordinates
resolves at **world spawn**, not at the bot, so `@e[type=zombie,distance=..60]`
will cheerfully report a different zombie entirely.

---

## `POST /interact`

Right-clicks an entity: opening a villager's trades, feeding an animal,
shearing a sheep.

**Parameters** — give `entity_id` or `type`.

| name | type | required | meaning |
| --- | --- | --- | --- |
| `entity_id` | int | one form | a specific entity |
| `type` | string | one form | the nearest entity of a type |

| field | type | meaning |
| --- | --- | --- |
| `target_id`, `target_type` | int, string | what was interacted with |

```json
{
  "type": "cow"
}
```

```json
{
  "ok": true,
  "pitch": 23.672943,
  "target_id": 32719,
  "target_type": "minecraft:cow",
  "x": 5000.5,
  "y": 100,
  "yaw": -45,
  "z": 5000.5
}
```

`entity_id` matters more here than anywhere else. Breeding two animals means
feeding *each* one, and an animal already in love walks toward its partner — so
targeting by type feeds the first one twice and the second never.

---

## `POST /interactat`

Right-clicks a specific point on an entity's hitbox.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `entity_id` | int | one form | a specific entity |
| `type` | string | one form | the nearest entity of a type |
| `dx`, `dy`, `dz` | float | yes | the hit point, relative to the entity's position |

```json
{
  "type": "cow",
  "dx": 0.0,
  "dy": 0.4,
  "dz": 0.0
}
```

```json
{
  "dx": 0,
  "dy": 0.4,
  "dz": 0,
  "entity_id": 32719,
  "ok": true,
  "pitch": 23.672943,
  "x": 5000.5,
  "y": 100,
  "yaw": -45,
  "z": 5000.5
}
```

This is how a chest boat works: the chest and the seat are separate targets on
one entity, and the centre boards the boat.
