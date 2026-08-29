# Aiming and moving

Every response below was captured from a live 26.2 server.

---

## `POST /look`

The one place aiming is controlled. It accepts every form a caller might
reasonably have to hand, and checks them most-specific first, so a body
carrying several does something predictable rather than depending on field
order.

**Parameters** — give exactly one form.

| name | type | meaning |
| --- | --- | --- |
| `direction` | string | `north`, `south`, `east`, `west`, `up`, `down` |
| `yaw` | float | absolute yaw; the pitch is kept |
| `pitch` | float | absolute pitch; the yaw is kept |
| `x`, `y`, `z` | float | an exact point to face |
| `block` | object | `{x, y, z}` integers, aimed at the block's centre |
| `entity_type` | string | the nearest entity of a type |
| `player` | string | a named player, aimed at eye height |

The order checked is: `player`, `entity_type`, `block`, point, `direction`,
then `yaw`/`pitch`.

**Response** — the standard envelope, plus `target_id` and `target_type` when
it aimed at something living.

### A named direction

```json
{
  "direction": "north"
}
```

```json
{
  "ok": true,
  "pitch": 0,
  "x": 5000.5,
  "y": 100,
  "yaw": 180,
  "z": 5000.5
}
```

### Absolute rotation

Yaw 0 faces south and increases clockwise. Pitch is −90 straight up, +90
straight down.

```json
{
  "yaw": 90,
  "pitch": -20
}
```

```json
{
  "ok": true,
  "pitch": -20,
  "x": 5000.5,
  "y": 100,
  "yaw": 90,
  "z": 5000.5
}
```

### A block

```json
{
  "block": {
    "x": 5006,
    "y": 100,
    "z": 5000
  }
}
```

```json
{
  "ok": true,
  "pitch": 10.5735235,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

---

## `POST /lookat`

The point form of `/look`, without the other spellings.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `x`, `y`, `z` | float | yes | the point to face |

```json
{
  "x": 5006.0,
  "y": 100.0,
  "z": 5000.0
}
```

```json
{
  "ok": true,
  "pitch": 16.348303,
  "x": 5000.5,
  "y": 100,
  "yaw": -95.19443,
  "z": 5000.5
}
```

---

## `POST /move`

Repositions the bot instantly and tells the server.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `x`, `y`, `z` | float | yes | where to be |

```json
{
  "x": 5000.5,
  "y": 100.0,
  "z": 5000.5
}
```

```json
{
  "ok": true,
  "pitch": 16.348303,
  "x": 5000.5,
  "y": 100,
  "yaw": -95.19443,
  "z": 5000.5
}
```

This is a teleport, not a walk. Large jumps may be rejected by the server as
moving too quickly, which arrives as a correction rather than as an error here.

---

## `POST /walk`

Walks in a straight line at player speed, sending a position each tick, and
returns on arrival.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `x`, `y`, `z` | float | yes | where to walk to |

```json
{
  "x": 5000.5,
  "y": 100.0,
  "z": 5004.5
}
```

```json
{
  "ok": true,
  "pitch": 16.348303,
  "x": 5000.5,
  "y": 100,
  "yaw": -95.19443,
  "z": 5004.5
}
```

Four blocks takes about 900 ms, which is walking speed and not overhead: a
player walks 4.317 blocks per second, so four blocks is 927 ms of arithmetic.

`sprint: true` holds the sprint input for the journey and moves at 5.612
blocks/s, vanilla's walking speed times 1.3 — measured at 5.63 against 4.35 for
the same twenty blocks. Sprinting needs more than six food and costs hunger;
below that it is refused, because moving at sprint speed without the server
agreeing you are sprinting earns a position correction rather than an error.

**This is dead reckoning, not pathfinding.** It knows nothing about walls,
drops or water. Walking into something solid is refused after a second of
getting no closer, rather than continuing until your timeout runs out:

```json
{
  "error": "understudy: walking to 5006.5,100.0,5000.5 made no progress for 1s at 5001.6,100.0,5000.5, 4.9 blocks short — something is in the way (this is dead reckoning, not pathfinding)"
}
```

Knockback does the same thing, so a bot being hit by a mob will fail a long
walk rather than complete it.

---

## `POST /sneak`

Holds sneak for a duration.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `ms` | int | no | 1000 | how long to hold it |

```json
{
  "ms": 300
}
```

```json
{
  "ok": true,
  "pitch": 16.348303,
  "x": 5000.5,
  "y": 100,
  "yaw": -95.19443,
  "z": 5000.5
}
```

Sneaking changes what right-clicking a block does, which is how you place a
block against a chest instead of opening it.

---

## `POST /fall`

Drops the bot to a floor, taking real fall damage. The descent is simulated at
vanilla gravity, so the damage matches what a player would take.

**Parameters**

| name | type | required | meaning |
| --- | --- | --- | --- |
| `to_y` | float | no | stop at this height instead of the first floor found |

| field | type | meaning |
| --- | --- | --- |
| `fell_blocks` | float | how far it actually fell |
| `health_before`, `health_after` | float | damage taken |
| `deaths` | int | the death count after, so a lethal fall is unambiguous |

```sh
curl -X POST localhost:8181/fall
curl -X POST localhost:8181/fall -d '{"to_y":64}'
```

Vanilla fall damage is `floor(distance) - 3` half-hearts, so ten blocks hurts
and forty kills. `deaths` is reported because health after a lethal fall is
health after respawning, which is 20 either way.

It refuses to fall into a column that has not been sent — `known: false` from
[`/ground`](reading.md#get-ground) is an error, not a guess.
