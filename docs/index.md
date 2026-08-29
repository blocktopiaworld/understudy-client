---
title: Home
layout: home
nav_order: 1
---

# understudy-client

A headless Minecraft client, written in Go, that speaks the Java Edition wire
protocol directly — no game, no rendering, no mod loader. It connects as an
ordinary offline-mode player and can mine, place, craft, eat, shoot, fall and
fight.

It exists for **automated in-game testing**. If you maintain a server plugin or
datapack and want to assert that playing the game produces the effect you
intended, you need something that plays the game. A bot that issues commands
proves nothing: `/setblock` does not fire the events a player mining a block
fires, and a statistic that only a real interaction increments will not move.

## Start here

| | |
| --- | --- |
| [HTTP control API](api/) | All 53 endpoints, with every request and response captured from a live server |
| [API reference](reference/) | The same surface as OpenAPI, generated from the route table |
| [Adding a version](adding-a-version.md) | The generator runs a new Minecraft release needs |

Runnable scripts live in
[examples/](https://github.com/blocktopiaworld/understudy-client/tree/main/examples)
in the repository, because they are code rather than documentation.

## Install

```sh
go install github.com/blocktopiaworld/understudy-client/cmd/understudy-client@latest
```

Go 1.26 or newer. No cgo, no runtime dependencies, and the module needs only the
standard library — `go.sum` is empty and there is no supply chain to audit.

## Run it

```sh
# Connect, stay 15s, leave.
understudy-client -addr localhost:25565 -username Probe

# Stay until interrupted, and expose the control API on :8181.
understudy-client -addr localhost:25565 -username Probe -hold 0 -control 8181
```

## Supported versions

**Minecraft 1.21.11 and newer**, on Paper, Fabric and vanilla. One binary
carries every supported version and picks by the server-list ping, so there is
nothing to configure. `understudy-client -versions` lists what your build
speaks.

Support is measured per version rather than assumed to carry across, because
payload shapes move between releases. A version whose encodings have not been
measured refuses to decode rather than reading them at the wrong offsets and
desynchronising quietly — which is why 1.21.4 tables ship but 1.21.4 is not a
supported target.

## What it will not do

- **Movement is dead reckoning.** It walks a straight line and knows nothing
  about walls, drops or water. There is no pathfinding.
- **Offline mode only.** No Mojang authentication and no encryption, so it
  cannot join an online-mode server.
- **The control API is unauthenticated.** Loopback only.
