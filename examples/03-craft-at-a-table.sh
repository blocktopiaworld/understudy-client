#!/usr/bin/env bash
# Craft at a table. Takes the table's position as $X $Y $Z.
set -euo pipefail
BOT="${BOT:-localhost:8181}"
X="${X:?set X}"; Y="${Y:?set Y}"; Z="${Z:?set Z}"

# Pretty-print with jq when it is there, pass the raw JSON through when it is
# not. The examples are meant to run on a bare machine.
show() { if command -v jq > /dev/null 2>&1; then jq "$@"; else cat; fi; }

curl -s -X POST "$BOT/container/open" \
  -d "{\"X\":$X,\"Y\":$Y,\"Z\":$Z,\"face\":1}" | show '{window_id, type, size, own_slots}'

# A window existing is not a window being populated: the open packet and the
# contents packet are separate. Reading it back is the wait.
curl -s "$BOT/container" | show '{open, kind, size, own_slots}'

echo "== by layout: readable, and what you want in a test =="
curl -s -X POST "$BOT/container/grid" -d '{"layout":{
  "1":"white_wool","2":"white_wool","3":"white_wool",
  "4":"white_wool","5":"white_wool","6":"white_wool",
  "8":"stick"},"repeat":1}' | show '{item, count, repeat}'

echo "== or by the server's own recipe book =="
curl -s "$BOT/recipes?item=stick" | show '{found, recipe}'
curl -s -X POST "$BOT/container/craft" -d '{"item":"minecraft:stick","all":false}' | show '{item, recipe}'
curl -s -X POST "$BOT/container/take" -d '{"slot":0}' > /dev/null

curl -s -X POST "$BOT/container/close" -d '{}' > /dev/null
