# Go style

The conventions this repository is held to. Most of it is the Go community's
common ground — [Effective Go][eg], the [Code Review Comments][crc] wiki, and
Google's [Go Style Guide][gsg] — and where those already say something, this
document does not repeat it. What follows is the parts that came up while
building and reviewing this client, with the reasoning attached, because a rule
without its reason gets cargo-culted or dropped at the first inconvenience.

[eg]: https://go.dev/doc/effective_go
[crc]: https://go.dev/wiki/CodeReviewComments
[gsg]: https://google.github.io/styleguide/go/

## Packages and directories

**A directory must not become a dump of files.** Go allows one package per
directory, so the only levers are extracting a package, folding a trivial file
into its caller, or renaming. Use them before a directory swamps.

This came up twice here: first when the repo root held 23 flat `.go` files
beside `go.mod`, then again when `understudy/` reached 22. A reader navigates by
directory listing before they navigate by grep, and a flat wall of files hides
which parts are cohesive and which merely happen to compile together.

What to look for, in order of how cleanly it extracts:

1. **Pure functions.** They have no dependency on the central type, so they
   move without ceremony and become testable in isolation. That yielded
   `internal/geom` (raycasting), `internal/ballistics` (projectile arcs) and
   `internal/nbt`, all of which then reached exhaustive coverage that was
   impractical when a test needed a `Client`, a world model and a version table.
2. **Self-contained state.** A mutex-guarded data structure that never reads the
   parent is a package: `internal/world`, `internal/entities`,
   `internal/inventory`.
3. **Generated code.** `protocol/versions` holds ~9,700 generated lines that
   otherwise buried ten hand-written files averaging 130.

Then:

- **Fold trivial files.** Under ~30 lines, one function, one caller — put it in
  the file that uses it rather than leaving a separate entry.
- **Keep the root for furniture.** `go.mod`, README, Makefile, lint config.
  Library code lives in a named directory.
- **Re-export so callers never import `internal/`.** `understudy` declares
  `type Entity = entities.Entity` and re-exports the slot constants. A caller
  naming a type should not have to know where it lives.
- **When a package legitimately stays large, put a file map in its package
  doc**, so `go doc` answers the navigation question. See `protocol/doc.go`.

## Naming

Standard Go naming, plus: **name for behaviour, not mechanism.** `stubBot` is
named for what makes it interesting — it answers without a server — not for the
pattern it implements.

Error strings are lower-case, no trailing punctuation, and prefixed with the
package: `understudy: no tracked entity of type "chicken"`.

## Errors

**Say what would fix it.** An error a test suite hits at 3am should name the
value that was wrong, not merely the operation that failed:

```go
// Good — the number is the whole diagnosis.
return fmt.Errorf("understudy: nearest %s (entity %d) is %.2f blocks away, "+
    "beyond the %.1f block attack reach", typeName, e.ID, dist, AttackReach)
```

**Refuse rather than send a packet the server will ignore.** A silent no-op is
the most expensive failure mode in a harness: the test fails somewhere else,
later, for a reason that has nothing to do with the cause. Out-of-reach blocks,
swings through walls, and packets a version does not have are all errors here.

**Use a sentinel when callers act differently on different failures.**
`ErrNoSuchEntity` exists because "there is none" and "there is one but you
cannot reach it" need different handling — killing the last of something is a
success, being out of range is not.

**Distinguish "no results" from "failed".** A lookup that legitimately finds
nothing returns a zero value and `false`, not an error.

## Concurrency

Document the contract in the package doc, and make it true. From
`protocol/doc.go`:

> A `Conn` may be written from any goroutine; reads are single-threaded by
> contract, since there is one read loop. A `Version` is immutable once
> registered and safe to share.

- **Hold the write lock to mutate.** A read lock around a mutation is a data
  race that `-race` will find and review will not. This repo shipped exactly
  that bug in the world model.
- **Expose a traversal rather than the collection.** `World.Scan` holds the
  read lock for a whole traversal, so a caller cannot iterate a structure that
  is being mutated underneath.
- **`atomic.Int64` for timestamps** that one goroutine stamps and another
  reads, rather than widening a mutex's scope.
- **Every test runs under `-race`.** `make check` does not pass otherwise.

## Untrusted input

Everything off a socket is untrusted, including lengths. A desynced stream puts
arbitrary bytes where a length prefix should be.

- **Bound every length before it sizes an allocation or acts as a divisor.**
  `MaxStringLen`, `MaxSections`, `MaxBitsPerEntry`. This repo shipped a 1 GiB
  allocation from a bogus prefix and two divide-by-zero panics from an
  out-of-range `bitsPerEntry`.
- **Never allocate on the failure path.** A failed read returns a shared zero
  buffer, not `make([]byte, n)`.
- **Say what the symptom is when the constant is wrong**, in the comment beside
  it. Format flags like `HasFluidCount` produce plausible garbage several
  sections downstream rather than an error at the mistake.

## Comments

**Comment the why.** The what is in the code. A comment that restates the
statement below it is noise that goes stale.

Specifically worth writing down:

- **Why a constant has the value it has**, and how it was measured. See
  `TeleportSettle`.
- **Non-obvious protocol behaviour**, with the symptom it causes. "The server
  silently ignores `use_item_on` while awaiting a position from the client" is
  hours of debugging for the next person.
- **Why code is somewhere surprising.** The teleport gate lives in the client
  rather than the driver because it is a property of the protocol, not of any
  one caller.
- **Deliberate limits.** `WalkTo` is dead reckoning on purpose; saying so stops
  it being read as an oversight.

**When a measurement is superseded, correct the comment rather than layering a
new claim on top.** `TeleportSettle` first recorded a reported failure, then a
local A/B that could not reproduce it, and finally the benchmark that explained
both. Each revision replaced the previous wording.

## Tests

- **Assert on the observable contract, not the implementation.**
  `protocol/versions` checks that item names resolve and round-trip, rather than
  that a table has ≥500 rows. The first fails when the data is wrong; the second
  fails when the data changes.
- **Test the failure paths.** The bounds checks above exist because of bugs;
  each has a regression test that asserts the bound holds, not merely that the
  happy path works.
- **Hermetic by default.** Sessions run against a `net.Pipe` fake server, so the
  suite needs no Minecraft server and no network.
- **Derive fixtures, never recall them.** Two of three UUID vectors here were
  wrong because they were the online-mode Mojang UUIDs rather than
  offline-derived ones. They are now derived independently, with a comment
  naming the trap. Anything version- or wire-specific comes from
  `minecraft-data` or a live server.
- **A setup step that can fail silently must be asserted.** A benchmark here
  measured nothing for several runs because `/fill` answers "That position is
  not loaded" and carries on, so with output discarded every arena silently
  failed to exist.
- **Name the test for the claim.** `TestAttackTimesStopsWhenTheTargetDies`, not
  `TestAttackTimes2`.

## Generated code

- Header on every generated file: what generated it, from what, and **DO NOT
  EDIT**.
- **Generated data lives in its own package** so it does not bury hand-written
  code, and so a caller who wants one small synthetic version can build it
  without linking the full tables.
- **Construct through an exported door.** `NewVersion(VersionSpec{...})` keeps
  the internals unexported while letting the generated package — and tests, and
  tools — build one.
- **Derive, do not hard-code, anything the source already knows.** The chunk
  format flags are computed from the protocol number.

## Tooling

`make check` runs `fmt-check`, `vet`, `golangci-lint` and the tests under
`-race`. It is the gate; if it does not pass, the change is not finished.
