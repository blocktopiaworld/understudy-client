#!/usr/bin/env bash
# Read the world. Nothing here changes anything.
set -euo pipefail
BOT="${BOT:-localhost:8181}"

# Pretty-print with jq when it is there, pass the raw JSON through when it is
# not. The examples are meant to run on a bare machine.
show() { if command -v jq > /dev/null 2>&1; then jq "$@"; else cat; fi; }

echo "== who and where =="
curl -s "$BOT/state" | show '{username, x, y, z, health, food, game_mode, on_ground}'

echo "== the floor beneath =="
# known:false means the chunk has not arrived, which is not the same as
# found:false. After a teleport, wait on this rather than on the coordinate.
curl -s "$BOT/ground" | show '{known, found, ground_y, gap, chunks}'

echo "== what is in front =="
curl -s -X POST "$BOT/look" -d '{"direction":"north"}' > /dev/null
curl -s "$BOT/lookingat" | show

echo "== what is nearby =="
curl -s "$BOT/entities?radius=16" | show '{count, entities: [.entities[] | {id, type_name, distance}]}'

echo "== what is carried =="
curl -s "$BOT/inventory" | show '{held_item, items: [.items[] | {slot, item, count}]}'
