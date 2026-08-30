---
title: Blocks
parent: HTTP control API
nav_order: 3
---

Every response below was captured from a live 26.2 server.

---

## `POST /dig`

Breaks one block, or a list of them, re-aiming for each.

Named for the packet rather than the activity. The wire message is
`player_digging`, and dropping an item and releasing a bow ride on it too, so
"mine" would be the wrong word for the family — throwing a stack on the ground
is not mining. The gameplay-level name belongs a layer up: understudy calls it
`player.mine(pos)`, and this is what that sends.

**Parameters** — give either `X`,`Y`,`Z` or `blocks`.

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `X`, `Y`, `Z` | int | one form | | a single block |
| `blocks` | array | one form | | `[{X,Y,Z}, …]` for a batch |
| `face` | int | no | the face toward the bot | which face to strike, 0–5 |
| `hold_ms` | int | no | 3000 | how long to hold the swing before giving up |

### One block

```json
{
  "X": 5002,
  "Y": 100,
  "Z": 5000,
  "hold_ms": 1500
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

### A batch

```json
{
  "blocks": [
    {
      "X": 5003,
      "Y": 100,
      "Z": 5000
    },
    {
      "X": 5004,
      "Y": 100,
      "Z": 5000
    }
  ]
}
```

```json
{
  "dug": 2,
  "ok": true,
  "pitch": 15.642246,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

`dug` counts what broke, not what was asked for, so a batch that meets an
unbreakable block tells you how many were fine before it stopped.

### When it will not break

```json
{
  "X": 5040,
  "Y": 100,
  "Z": 5000
}
```

`409`

```json
{
  "error": "understudy: cannot break block at 5040,100,5000 — 39.50 blocks away, beyond the 4.5 block reach"
}
```

**Hold time is a real constraint.** Below the instant-break threshold the
server ignores the start packet and the block must be held for its full
hardness: 0.55 s against 0.058 s on obsidian. If digs are timing out, check the
tool before the timeout.

A block staged moments earlier may not have reached the client yet. `/dig`
waits briefly for that rather than refusing, because "there is nothing there"
and "it has not arrived" look identical from here and only one of them is a
real failure.

---

## `POST /diglook`

Digs whatever the bot is currently looking at, so aiming and breaking are
separate steps.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `hold_ms` | int | no | 3000 | how long to hold the swing |

| field | type | meaning |
| --- | --- | --- |
| `x`, `y`, `z` | int | the block that was under the crosshair |
| `face` | int | the face struck |
| `distance` | float | how far away it was |

```json
{
  "hold_ms": 1500
}
```

```json
{
  "distance": 1.719185864649232,
  "face": 4,
  "ok": true,
  "pitch": 29.248827,
  "x": 5002,
  "y": 100,
  "yaw": -90,
  "z": 5000
}
```

---

## `POST /place`

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `X`, `Y`, `Z` | int | yes | | the block placed **against** |
| `face` | int | no | the face toward the bot | which face of it, 0–5 |
| `verify` | bool | no | false | read the position back and confirm it changed |
| `item` | string | no | | what to place; held first when given |

| field | type | meaning |
| --- | --- | --- |
| `placed` | string | what was in hand when it went down |
| `face` | int | the face used |
| `verified` | bool | whether the world was read back |
| `against` | object | the block placed against |

Face numbering is the protocol's: 0 down, 1 up, 2 north, 3 south, 4 west,
5 east. So placing on top of the floor block below you is `face: 1`.

```json
{
  "X": 5002,
  "Y": 99,
  "Z": 5000,
  "face": 1,
  "verify": true
}
```

```json
{
  "ok": true,
  "pitch": 46.66834,
  "x": 5000.5,
  "y": 100,
  "yaw": -90,
  "z": 5000.5
}
```

**Use `verify`.** It costs one round trip — the whole call is 6.4 ms with it —
and placement fails silently more often than anything else here. A block cannot
be placed inside an entity, and a cow standing where you are building produces
a success on the wire and no block in the world.

---

## `POST /use`

Right-clicks with the held item, aiming where the bot is already looking. This
is eating, drawing a bow, throwing a snowball, using a bucket.

**Parameters**

| name | type | required | default | meaning |
| --- | --- | --- | --- | --- |
| `hold_ms` | int | no | 0 | hold the use down, which is what a bow needs |

`/place` takes an optional `item` and holds it first, the same way `/consume`
always has.



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

The response says the packet went out; it cannot say what the server made of
it. **Item counts are the reliable signal**: a thrown snowball going from 8 to
7 is unambiguous. Looking for the projectile entity is not — it has usually
landed or despawned by the time you ask.
