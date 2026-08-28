#!/usr/bin/env bash
# The whole point: make the server's own numbers move, and check them there.
#
# This is what separates a real client from a bot that issues commands.
# /setblock does not fire the events a player mining a block fires, and a
# statistic that only a real interaction increments will not move for it.
#
# Needs an RCON client; mcrcon is assumed here. Set RCON_CMD to your own.
set -euo pipefail
BOT="${BOT:-localhost:8181}"
X="${X:?set X}"; Y="${Y:?set Y}"; Z="${Z:?set Z}"
NAME="${NAME:-Probe}"
RCON_CMD="${RCON_CMD:-mcrcon -H 127.0.0.1 -P 25575 -p password}"

# Pretty-print with jq when it is there, pass the raw JSON through when it is
# not. The examples are meant to run on a bare machine.

rcon() { $RCON_CMD "$@"; }

# The server's reply is prose: "Probe has 3 [mined]", or "Can't get value of
# mined for Probe; none is set" before anything has been counted. Match the
# "has <n>" shape rather than the first number in the line — a player name with
# a digit in it will otherwise be read as the score, which is a bug this
# example had until it was run against a bot called Ex5.
score() {
  local out num
  out=$(rcon "scoreboard players get $NAME mined" 2>/dev/null || true)
  num=$(printf '%s' "$out" | grep -oE 'has [0-9]+' | grep -oE '[0-9]+' || true)
  printf '%s' "${num:-0}"
}

# Ask the server to count, rather than trusting what the client believes.
rcon "scoreboard objectives remove mined" > /dev/null 2>&1 || true
rcon "scoreboard objectives add mined minecraft.mined:minecraft.stone" > /dev/null
before=$(score)

curl -s -X POST "$BOT/hold" -d '{"item":"minecraft:netherite_pickaxe"}' > /dev/null
# The batch form is used deliberately, even for one block: only it reports
# "dug". A single-block dig answers with the envelope alone, where 200 means it
# broke and there is no number to read.
#
# Pulling that number out, without assuming jq is installed.
dug=$(curl -s -X POST "$BOT/dig" \
        -d "{\"blocks\":[{\"X\":$X,\"Y\":$Y,\"Z\":$Z}],\"hold_ms\":1500}" \
      | grep -oE '"dug":[0-9]+' | grep -oE '[0-9]+' || true)
dug=${dug:-0}

sleep 1
after=$(score)

echo "client says dug:     $dug"
echo "server says mined:   $before -> $after"
[ "$after" -gt "$before" ] || { echo "FAIL: the statistic did not move"; exit 1; }
echo "PASS"
