#!/usr/bin/env bash
# Trade with the nearest villager.
set -euo pipefail
BOT="${BOT:-localhost:8181}"

# Pretty-print with jq when it is there, pass the raw JSON through when it is
# not. The examples are meant to run on a bare machine.
show() { if command -v jq > /dev/null 2>&1; then jq "$@"; else cat; fi; }

curl -s -X POST "$BOT/container/open" -d '{"type":"villager"}' | show '{type, target_id}'

# Spent offers are listed too, not filtered away, because a test for lockout
# needs to see them.
echo "== offers =="
curl -s "$BOT/trades" | show '[.trades[] | {index, input, output, uses, max_uses, available}]'

echo "== trade by what it produces, not by index =="
# Selecting by output survives a villager whose offers are in a different order.
curl -s -X POST "$BOT/container/trade" -d '{"item":"bread","times":2}' | show '{traded, requested}'

# traded is counted from the stock gained, so it can exceed requested: taking a
# merchant result is a shift-click, and vanilla batches that into every use the
# villager has left and the player can afford.

curl -s -X POST "$BOT/container/close" -d '{}' > /dev/null
