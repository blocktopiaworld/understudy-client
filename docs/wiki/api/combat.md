# Combat

---

## `POST /attack`

```sh
# The nearest zombie, once.
curl -X POST localhost:8181/attack -d '{"type":"zombie"}'

# A specific entity, four times.
curl -X POST localhost:8181/attack -d '{"entity_id":18349,"times":4}'
```

| field | meaning |
| --- | --- |
| `entity_id` | a specific entity, from `GET /entities` |
| `type` | the nearest entity of a type, if no id is given |
| `times` | how many swings; defaults to one |

Returns `hits`, `requested`, `target_id` and `target_type`.

Prefer `entity_id`. Targeting by type hits whichever one the client thinks is
nearest, and "nearest" is three-dimensional — an entity forty blocks straight
down can win against one two blocks away.

The attack is refused rather than sent if the target is out of reach or behind
a wall, and the error names the distance or the obstruction.

---

## `POST /swing`

```json
{"hold_ms": 0}
```

Swings the arm at nothing in particular. Useful for animations and for the
statistics that count swings.

---

## `POST /shoot`

Draws a bow and looses at a target.

```sh
# At an entity type.
curl -X POST localhost:8181/shoot -d '{"type":"zombie"}'

# At a point.
curl -X POST localhost:8181/shoot -d '{"x":10.0,"y":64.0,"z":-5.0}'

# At a block.
curl -X POST localhost:8181/shoot -d '{"block":{"x":10,"y":64,"z":-5}}'
```

| field | meaning |
| --- | --- |
| `x`, `y`, `z` | an exact point |
| `block` | a block, aimed at its centre |
| `type` | the nearest entity of a type, aimed at body height |
| `draw_ms` | how long to draw; defaults to a full draw |

Returns the `draw_ms` used and the resulting `power`.

The arc is computed, not guessed: the launch angle accounts for drop over the
distance. Against a mob at 4, 8 and 16 blocks it lands three arrows in four,
the fourth having nothing left to hit.

Two things make bow tests look broken when they are not. The arrow is gone
before you can look for it, so assert on the target's health rather than on a
projectile entity existing. And an RCON selector without explicit coordinates
resolves at **world spawn**, not at the bot, so `@e[type=zombie,distance=..60]`
will happily report a different zombie entirely.

---

## `POST /interact`

```sh
curl -X POST localhost:8181/interact -d '{"entity_id":18349}'
curl -X POST localhost:8181/interact -d '{"type":"villager"}'
```

Right-clicks an entity: opening a villager's trades, feeding an animal,
shearing a sheep. Returns `target_id` and `target_type`.

`entity_id` matters more here than anywhere else. Feeding two animals to breed
them means feeding *each* one, and an animal already in love walks toward its
partner — so targeting by type feeds the first one twice and the second never.

---

## `POST /interactat`

```json
{"entity_id": 18349, "dx": 0.0, "dy": 0.4, "dz": -0.5}
```

Right-clicks a specific point on an entity's hitbox. This is how a chest boat
works: the chest and the seat are separate targets on one entity, and the
centre boards the boat.
