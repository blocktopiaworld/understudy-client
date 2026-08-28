# Blocks

---

## `POST /dig`

Breaks one block, or a list of them.

```sh
# One block.
curl -X POST localhost:8181/dig -d '{"X":10,"Y":64,"Z":10,"hold_ms":1500}'

# Several. The bot re-aims for each.
curl -X POST localhost:8181/dig -d '{"blocks":[
  {"X":10,"Y":64,"Z":10},{"X":11,"Y":64,"Z":10}]}'
```

| field | meaning |
| --- | --- |
| `X`, `Y`, `Z` | the block, for the single form |
| `blocks` | a list of `{X,Y,Z}`, for the batch form |
| `face` | which face to strike; defaults to the one facing the bot |
| `hold_ms` | how long to hold the swing before giving up |

Returns `dug`, the number that actually broke. On a partial failure the status
is `409` and `dug` still reports how far it got, so a batch that hits an
unbreakable block tells you which blocks were fine.

**Hold time is a real constraint.** Below the instant-break threshold the
server ignores the start packet and the swing must be held for the block's full
hardness. With an efficient enough tool the same block takes a few tens of
milliseconds. If digs are timing out, check the tool before the timeout.

A block staged moments earlier may not have reached the client yet. `/dig`
waits briefly for that rather than refusing, because "there is nothing there"
and "it has not arrived" look identical from here and only one of them is a
real failure.

---

## `POST /diglook`

```json
{"hold_ms": 1500}
```

Digs whatever the bot is currently looking at, so aim and break are separate
steps. Returns the block's `x`, `y`, `z`, the `face` and the `distance`.

---

## `POST /place`

```sh
curl -X POST localhost:8181/place -d '{"X":10,"Y":64,"Z":10,"face":1,"verify":true}'
```

| field | meaning |
| --- | --- |
| `X`, `Y`, `Z` | the block being placed *against* |
| `face` | which face of it, 0-5; `1` is the top |
| `verify` | read the position back and confirm it changed |

`verify` is opt-in because it costs a round trip. Use it. Placement fails
silently more often than anything else here: a block cannot be placed inside an
entity, and a cow standing where you are building produces a success on the
wire and no block in the world.

---

## `POST /use`

```json
{"hold_ms": 0}
```

Right-clicks with the held item, aiming where the bot is already looking. This
is eating, drawing a bow, throwing a snowball, using a bucket.

`hold_ms` holds the use down, which is what a bow needs; see `/shoot` for the
aimed version.

Item counts are the reliable signal that a use worked. A thrown snowball going
from 8 to 7 is unambiguous, and looking for the projectile entity is not — it
has usually already landed or despawned by the time you ask.
