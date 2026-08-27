# Code review

A full review of this client, with every finding verified by a reproducer
before it was called a bug. Findings are grouped by what they cost, and each
records how it was confirmed and where the fix lives.

The bar throughout: **a claim is not a finding until something fails on
purpose.** Several plausible-looking problems did not survive that test, and
they are recorded at the bottom, because "we looked and it was fine" is worth
as much to the next reader as the fixes.

---

## Crashes and unbounded resources

### 1. A bogus length prefix allocated 1 GiB

`take()` returned `make([]byte, n)` on the failure path, so a corrupt or
desynced stream — where arbitrary bytes land where a length prefix belongs —
sized an allocation from attacker-controlled input *and did so even when the
read had already failed*.

**Confirmed by** feeding a frame with a maximal VarInt length prefix and
watching RSS climb.

**Fixed** in `protocol/reader.go`: a shared 16-byte `zeroPad` is returned on
failure so nothing is allocated, and `MaxStringLen` (32767 × 4, the protocol's
own ceiling) bounds the success path. The regression test asserts heap growth
stays under 1 MiB.

### 2. Two divide-by-zero panics from `bitsPerEntry`

Paletted chunk containers carry a bits-per-entry byte used as a divisor. A
value above 64 made `64 / bitsPerEntry` zero, and the next division panicked.
A malformed chunk packet crashed the client.

**Confirmed by** a hand-built chunk section with `bitsPerEntry = 200`.

**Fixed** in `protocol/chunk.go` with `MaxBitsPerEntry = 32` and guards at both
sites. `MaxSections = 64` bounds the related preallocation.

### 3. Unbounded entity-destroy preallocation

A destroy packet's count was used to preallocate directly.

**Fixed** with `maxEntitiesPerPacket = 1 << 16` in `understudy/entities.go`.

### 4. Unbounded recursion through nested components

Found later, by fuzzing rather than by reading. Components nest through items:
a container holds item stacks, an item stack holds components, and one of those
components is a container again. Nothing bounded the cycle.

An eight-MiB packet — `MaxPacketSize`, the largest this client accepts — buys
about 1.2 million layers. That does not quite overflow the stack, but it grows
it to 1.2 million frames and spends a second there; a little deeper is
`fatal error: stack overflow`, which no `recover` catches. The recipe decoder
had bounded its own recursion at `slotDisplayDepth` from the start, so this was
an inconsistency as much as a defect.

**Fixed** with `componentDepth = 16` in `understudy/components.go`.

### 5. Unbounded list lengths, everywhere

The worse of the two, because it needs no nesting at all. Every
`for range r.VarInt()` ran on the decoded count alone, and an exhausted reader
returns zeros rather than failing — so a claimed two billion elements is two
billion cheap iterations instead of an error. The fuzzer hung `skipComponent`
with a `can_place_on` payload of repeated `0xd3` bytes, and hung `readSlot`
with a large removed-components count.

**Fixed** with `listLen`, which bounds a count by the bytes remaining rather
than by a tuned constant: every element costs at least one byte, so a count
larger than what is left is not a long list, it is a corrupt one. That cannot
drift from what the data allows. `readSlotFinal` is deliberately exempt —
nothing there walks the components, because the item is the last field of its
packet, which is the whole reason that variant exists.

---

## Correctness

### 4. A data race on the world model

`SetBlockState` mutated the chunk store while holding only the **read** lock.
Two goroutines writing block states — the read loop and a dig's world update —
could corrupt it.

**Confirmed by** `go test -race`, which reports it reliably.

**Fixed** in `internal/world`: the write lock is held to mutate, and `Scan`
holds the read lock across a whole traversal so callers cannot iterate a
structure being mutated underneath them.

### 5. Blocks below the column floor read as the wrong block

Locating a section used truncating division, which rounds toward zero. For
`y` below `MinY` that produced a valid-looking index instead of a miss, so a
block below the world returned whatever happened to be in section 0.

**Found by** a test written while extracting the chunk code — not by reading
it. Signed division rounding toward zero rather than negative infinity is
invisible until something crosses zero.

**Fixed** with an explicit `locate()` that rejects negatives before dividing.

### 6. `AttackTimes` reported success as failure

Asking for three hits on a chicken while holding a diamond pickaxe landed one
hit — 5 damage against 4 HP — and then failed with `no tracked entity of type
"chicken"`. The target was gone because the caller had killed it, which is the
outcome they wanted, reported as an error naming the wrong problem.

**Confirmed by** the live acceptance sweep, whose diagnostics showed the
tracker holding the chicken at distance 1 immediately before the attack failed
to find one.

**Fixed** in `understudy/entities.go`. `AttackTimes` now returns the number of
hits that actually landed, and running out of targets after at least one hit is
success. Finding nothing on the *first* swing is still an error. `ErrNoSuchEntity`
was introduced as a sentinel so the two cases can be told apart. The control
API's `/attack` reported `"hits": times` — the number *requested* — and now
reports what landed, alongside `"requested"`.

### 7. Offline UUID test vectors were wrong

Two of three fixtures were the **online-mode Mojang** UUIDs for those names
rather than offline-derived ones. The test would have passed against a broken
implementation for those two names.

**Fixed** by deriving them independently in Python from Java's
`nameUUIDFromBytes`, with a comment naming the trap. Everything
version-specific now comes from `minecraft-data` or a live server rather than
from memory.

---

## Performance

### 8. The client sent no idle position packets

A real client sends a movement packet roughly twenty times a second whether or
not the player moved. This one spoke only when it had somewhere to be, so it
was silent for most of a session. That silence is visible to the server:
movement and idle bookkeeping never advance, statistics derived from a player
simply *being* somewhere do not accrue, and anti-cheat and AFK plugins score it
differently from a real player.

**Fixed** in `understudy/heartbeat.go`. The loop ticks at `TickRate` and yields
when something else has already sent a position this tick, so it never
interleaves a stale position into a descent. It starts on the first teleport,
because before that there is no position to report and `0,0,0` is a lie the
server has to correct.

### 9. The teleport settle gate slept 3.8 s per mining field

The gate exists because the server keeps an "awaiting position from client"
state after a teleport and **silently ignores** `use_item`, `use_item_on` and
`player_action` while it lasts. Mining never noticed, because `awaitBreak` keeps
swinging and re-sends its finish packet, retrying through the window by
accident; a one-shot place just vanishes.

The first implementation slept out a fixed 350 ms window after every teleport.

**Measured** against a live Fabric server — eleven repositions, one dig each:
the gate fired 11/11 times at a **346 ms mean, 3.80 s per field**. Nothing was
absorbing it: the test driver does pay a settle, but in `stand()`, and its
mining path anchors through `tp()` directly and repositions about eleven times
per field.

**Fixed** by reading the name of the state. The server is waiting for a position
from the client, so `awaitTeleportSettle` now *sends* one and waits a single
tick for the server to act on it. Same benchmark: **21.85 s against 25.23 s,
gate waits 11 → 0, still 11/11 dug.** `TeleportSettle` survives only as a
ceiling on the fallback path — no connection, or outside play.

An earlier A/B on this gate was **invalid** and is recorded here so the result
is not trusted: `/fill` answers "That position is not loaded" and carries on, so
with its output discarded every arena silently failed to exist and the
experiment measured its own setup. Re-run with `forceload` and an asserted
arena, it showed 6/6 both with and without the gate — because both arms ran the
idle position loop, which is the actual fix.

---

## Structure

### 10. The library was a flat wall of files

23 `.go` files at the repo root, then 22 in `understudy/` after the first pass.

**Fixed** by extracting what genuinely did not need the central type — pure
computation first (`internal/geom`, `internal/ballistics`, `internal/nbt`),
then self-contained state (`internal/world`, `internal/entities`,
`internal/inventory`) — and moving the ~9,700 generated lines into
`protocol/versions`. Type aliases and re-exported constants mean callers never
import an `internal/` package.

Each extracted package then got the exhaustive tests that were impractical when
a test needed a `Client`, a world model and a version table. `internal/geom`
reached 100%.

The convention this settled on is written up in [go-style.md](go-style.md).

### 11. Tests asserted implementation rather than contract

The generated-table tests checked row counts — `len(itemNames) >= 500`. That
fails when the data *changes* rather than when it is *wrong*.

**Fixed** when they moved to `protocol/versions`: they now resolve known item
names, round-trip IDs, and check stack sizes, which is what inventory maths
actually depends on.

---

## Looked at, not a bug

- **The idle position ticker as the cause of the mining regression.** The
  obvious suspect, and wrong. With the old gate, ticker on and ticker off
  measured 25.23 s to the centisecond. It is ~20 small packets a second, and it
  is what makes the fixed gate free in the common case.
- **The entity tracker losing a wandering chicken.** The tracker followed it
  accurately from 1.0 to 7.55 blocks over four seconds. Chickens are simply
  fast, and an out-of-reach refusal naming the distance is correct behaviour.
- **`nextSequence` leaking between clients.** Checked; it is per-connection.

---

## Not addressed

- **Relative-teleport flags are ignored.** `CBPlayPosition` treats every
  teleport as absolute. Correct for a harness that teleports bots into arenas it
  built, wrong in general.
- **No Paper coverage.** The protocol is the same and it is expected to work.
  Expected is not tested.
- **The control API is unauthenticated.** Documented, loopback-only.
