# Aiming and moving

---

## `POST /look`

The one place aiming is controlled. It accepts every form a caller might
reasonably have to hand, checked most-specific first so a body carrying several
does something predictable rather than depending on field order.

```sh
curl -X POST localhost:8181/look -d '{"direction":"north"}'
curl -X POST localhost:8181/look -d '{"yaw":90}'
curl -X POST localhost:8181/look -d '{"yaw":90,"pitch":-20}'
curl -X POST localhost:8181/look -d '{"x":10,"y":64,"z":-5}'
curl -X POST localhost:8181/look -d '{"block":{"x":10,"y":64,"z":-5}}'
curl -X POST localhost:8181/look -d '{"entity_type":"chicken"}'
curl -X POST localhost:8181/look -d '{"player":"Someone"}'
```

| field | meaning |
| --- | --- |
| `direction` | a named compass direction |
| `yaw`, `pitch` | absolute rotation; either alone keeps the other |
| `x`, `y`, `z` | an exact point |
| `block` | a block, aimed at its centre |
| `entity_type` | the nearest entity of a type |
| `player` | a named player, aimed at eye height |

When it aimed at something, the response carries `target_id` and `target_type`.

---

## `POST /lookat`

```json
{"x": 10.0, "y": 64.0, "z": -5.0}
```

The point form of `/look`, without the other spellings.

---

## `POST /move`

```json
{"x": 10.5, "y": 64.0, "z": 10.5}
```

Teleports the bot's own idea of where it is and tells the server. Instant, and
the server may reject it as moving too quickly.

---

## `POST /walk`

```json
{"x": 10.5, "y": 64.0, "z": 10.5}
```

Walks there at player speed, sending positions along the way. Returns when it
arrives.

**Movement is dead reckoning.** It walks a straight line and knows nothing
about walls, drops or water. There is no pathfinding.

---

## `POST /fall`

Drops the bot to a floor and takes real fall damage.

```sh
curl -X POST localhost:8181/fall
curl -X POST localhost:8181/fall -d '{"to_y":64}'
```

| field | meaning |
| --- | --- |
| `to_y` | stop at this height instead of the first floor found |

| response | meaning |
| --- | --- |
| `fell_blocks` | how far it actually fell |
| `health_before`, `health_after` | damage taken |
| `deaths` | the death count after, so a lethal fall is unambiguous |

The descent is simulated at vanilla gravity, so damage matches what a player
would take: `floor(distance) - 3` hearts-worth. It will not fall into a column
that has not been sent — `known: false` on `/ground` is refused, not guessed at.

---

## `POST /sneak`

```json
{"ms": 500}
```

Holds sneak for a duration. Sneaking changes what right-clicking a block does,
which is how you place a block against a chest instead of opening it.
