# Changelog

Notable changes. Dates are when the work landed, not when a tag was cut.

## Unreleased

First public release candidate. Nothing has been tagged yet, so everything
below is the starting state rather than a diff from one.

### Fixed

- `POST /container/trade` completed no trade when asked for one. The `times == 1`
  path selected the offer and stopped, leaving the result in the merchant slot —
  the server counts a trade only when the result is taken, so no
  `traded_with_villager`, no trade event, nothing downstream. It also reported
  `"traded": 1` as a literal, so the response agreed with any assertion put to
  it. Every count now goes through `TradeAndTake`, and `traded` is measured from
  the stock gained.

### Protocol

- Speaks **26.2**, **26.1**, **1.21.11** and **1.21.4**. Support is per-version
  and measured, not assumed: component ids and payload encodings both move
  between versions, and each version carries its own tables. A version whose
  encodings have not been measured refuses to decode components and says so.
- Data components: 108 of the 111 types the servers register. The three left —
  `creative_slot_lock`, `additional_trade_cost`, `map_post_processing` — were
  each chased down the route that should produce them and appear not to reach
  an item at all.
- Recipe book decodes on 26.2, 26.1 and 1.21.11. On 1.21.4 it stops partway,
  and `MissingRecipes` reports the shortfall so a half-decoded book cannot be
  mistaken for a small one.

### Robustness

- Fuzz tests for the three decoders that read arbitrary server bytes. They
  found unbounded recursion through nested components and unbounded list
  lengths throughout; both are bounded now.
- Entity tracking survives a teleport: entries beyond any possible view
  distance are dropped rather than reported as current.
- An unloaded chunk is no longer read as "no ground". `Support.Known` is
  separate from `Support.Found`, because "I have not been sent the terrain" is
  not "there is no terrain" — the client claims to be standing rather than
  volunteering that it is airborne, which is what a server kicks for.

### Licence

Apache License 2.0. Commercial use, including in closed-source products, is
explicitly permitted; the Blocktopia name and marks are not granted.

### Verified against

Paper 26.2, Fabric 26.2, Fabric 26.1.2, and vanilla 26.2 / 1.21.11 / 1.21.4.
Every acceptance assertion goes through RCON — the server's view, not the
client's self-report.
