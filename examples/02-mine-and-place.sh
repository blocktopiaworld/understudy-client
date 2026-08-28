#!/usr/bin/env bash
# Break blocks and put one back. Takes the block to work on as $X $Y $Z.
set -euo pipefail
BOT="${BOT:-localhost:8181}"
X="${X:?set X}"; Y="${Y:?set Y}"; Z="${Z:?set Z}"

# Pretty-print with jq when it is there, pass the raw JSON through when it is
# not. The examples are meant to run on a bare machine.
show() { if command -v jq > /dev/null 2>&1; then jq "$@"; else cat; fi; }

# Hold a tool first. Below the instant-break threshold the server ignores the
# start packet and every block costs its full hardness, which reads as a
# timeout rather than as the wrong tool.
curl -s -X POST "$BOT/hold" -d '{"item":"minecraft:netherite_pickaxe"}' | show '{held_item, held_slot}'

# Is it even reachable? Asking turns a silently ignored packet into a decision.
curl -s "$BOT/reach?x=$X&y=$Y&z=$Z" | show '{distance, reach, can}'

echo "== one block =="
curl -s -X POST "$BOT/dig" -d "{\"X\":$X,\"Y\":$Y,\"Z\":$Z,\"hold_ms\":1500}" | show '{dug}'

echo "== a batch: dug counts what actually broke =="
curl -s -X POST "$BOT/dig" -d "{\"blocks\":[
  {\"X\":$((X+1)),\"Y\":$Y,\"Z\":$Z},
  {\"X\":$((X+2)),\"Y\":$Y,\"Z\":$Z}]}" | show '{dug}'

echo "== put one back =="
# X,Y,Z here is the block placed *against*, and face 1 is its top.
curl -s -X POST "$BOT/hold" -d '{"item":"minecraft:stone"}' > /dev/null
curl -s -X POST "$BOT/place" \
  -d "{\"X\":$X,\"Y\":$((Y-1)),\"Z\":$Z,\"face\":1,\"verify\":true}" | show '{ok}'

# verify:true costs a round trip and is worth it. A block cannot be placed
# inside an entity, and a cow standing where you are building produces a
# success on the wire and no block in the world.
