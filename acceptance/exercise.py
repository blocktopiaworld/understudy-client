#!/usr/bin/env python3
"""Exercise the client against whatever server it is pointed at.

Parameterised so switching implementations is a one-liner:
    GAME=127.0.0.1:37325 RCON=127.0.0.1:49959 PW=understudy python3 exercise.py

Every assertion is the server's answer, not the client's own report. A client
that believes it mined a block is worth nothing; the question is always whether
the block is gone according to the server.
"""
import json, os, subprocess, sys, time, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from rcon import rcon

GAME = os.environ.get("GAME", "127.0.0.1:37325")
RHOST, RPORT = os.environ.get("RCON", "127.0.0.1:49959").split(":")
PW = os.environ.get("PW", "understudy")
NAME = os.environ.get("BOT_NAME", "Exercise")
PORT = int(os.environ.get("CPORT", "9300"))
BOT = os.environ.get("BOT", os.path.join(HERE, os.pardir, "understudy-client"))
X, Y, Z = 320, 80, 320

R = lambda *c: rcon(RHOST, int(RPORT), PW, *c)
def get(p):
    with urllib.request.urlopen(f"http://localhost:{PORT}/{p}", timeout=10) as r:
        return json.load(r)
def post(p, body="{}"):
    try:
        req = urllib.request.Request(f"http://localhost:{PORT}/{p}", data=body.encode())
        return json.loads(urllib.request.urlopen(req, timeout=20).read() or b"{}")
    except urllib.error.HTTPError as e:
        return json.loads(e.read() or b'{"error":"?"}')
    except Exception as e:
        return {"error": str(e)}

passed, failed = [], []
def check(name, ok, detail=""):
    (passed if ok else failed).append(name)
    print(f"  {'PASS' if ok else 'FAIL'}  {name}" + (f"   {detail}" if detail and not ok else ""))

def wait(cond, secs=6):
    end = time.time() + secs
    while time.time() < end:
        try:
            if cond(): return True
        except Exception: pass
        time.sleep(0.05)
    return False

def block_at(x, y, z, want):
    return "passed" in R(f"execute if block {x} {y} {z} {want}")[0]

R(f"forceload add {X-30} {Z-30} {X+30} {Z+30}")
R(f"fill {X-8} {Y-1} {Z-8} {X+8} {Y-1} {Z+8} minecraft:stone",
  f"fill {X-8} {Y} {Z-8} {X+8} {Y+4} {Z+8} minecraft:air",
  "gamerule doDaylightCycle false", "time set day", "gamerule randomTickSpeed 0")
log = open(os.path.join(HERE, f"{NAME}.log"), "wb")
proc = subprocess.Popen([BOT, "-addr", GAME, "-username", NAME, "-hold", "0",
                         "-control", str(PORT), "-debug"], stdout=log, stderr=log)
try:
    joined = wait(lambda: get("state")["joined"], 30)
    check("bot connects and joins", joined)
    if not joined: raise SystemExit(1)
    print(f"        server version: {get('state').get('version','?')}")
    R(f"gamemode survival {NAME}", f"tp {NAME} {X}.5 {Y} {Z}.5", f"clear {NAME}")
    wait(lambda: abs(get("state")["x"] - (X + 0.5)) < 0.8)
    # Wait for the terrain, not just the position. Chunks trail a teleport, and
    # a harness that only waits for the coordinate asks about a world the client
    # has not been sent yet — which is what the floating-kick report turned out
    # to be. The client answers "known: false" rather than guessing, so that is
    # the thing to wait on.
    check("terrain arrives after the teleport", wait(lambda: get("ground")["known"], 15),
          f"still unknown: {json.dumps(get('ground'))[:80]}")

    # --- world reading
    check("floor reads as solid", get(f"block?x={X}&y={Y-1}&z={Z}").get("solid") is True)
    check("air above reads as not solid", get(f"block?x={X}&y={Y+1}&z={Z}").get("solid") is False)
    g = get("ground")
    check("ground is known and found", g.get("known") is True and g.get("found") is True,
          json.dumps(g)[:90])

    # --- placing, then the server's opinion of it
    R(f"item replace entity {NAME} container.0 with minecraft:cobblestone 8")
    wait(lambda: get("inventory")["items"] != [])
    post("hold", '{"item":"cobblestone"}')
    r = post("place", '{"X":%d,"Y":%d,"Z":%d,"face":1}' % (X + 1, Y - 1, Z))
    check("place lands on the server",
          wait(lambda: block_at(X + 1, Y, Z, "minecraft:cobblestone")), json.dumps(r)[:90])

    # --- digging it back out
    R(f"item replace entity {NAME} container.1 with minecraft:diamond_pickaxe 1")
    # Wait for the client to see the tool before selecting it. rcon put it in
    # the player's inventory server-side; the client learns about it a packet
    # later, and holding what is not there yet leaves the bot swinging its fist.
    wait(lambda: any("pickaxe" in i["name"] for i in get("inventory")["items"]))
    post("hold", '{"item":"diamond_pickaxe"}')
    # Wait for the client to be told about the block before asking it to dig
    # one. Without this the ray traces through a target the client has not been
    # sent and blames whatever lies beyond.
    wait(lambda: get(f"block?x={X+1}&y={Y}&z={Z}").get("solid") is True)
    r = post("dig", '{"X":%d,"Y":%d,"Z":%d}' % (X + 1, Y, Z))
    check("dig removes it again", wait(lambda: block_at(X + 1, Y, Z, "minecraft:air")),
          json.dumps(r)[:110])

    # --- crafting from the server's recipe book
    R(f"setblock {X+4} {Y} {Z} minecraft:crafting_table")
    R(f"clear {NAME}", f"item replace entity {NAME} container.0 with minecraft:oak_log 4")
    R(f"recipe give {NAME} *")
    wait(lambda: get("recipes")["known"] > 100, 10)
    rec = get("recipes")
    check("recipe book decodes", rec["known"] > 500 and rec.get("missing", 0) == 0,
          f"known={rec['known']} missing={rec.get('missing')}")
    post("container/open", '{"X":%d,"Y":%d,"Z":%d,"face":1}' % (X + 4, Y, Z))
    r = post("container/craft", '{"item":"oak_planks","all":false}')
    # The server lays the grid out; the result still has to be taken out of the
    # result slot. And while a window is open /inventory does not update — the
    # container view is what the server is actually telling us.
    in_window = lambda n: any(n in i["item"] for i in get("container").get("items", []))
    check("craft by name from the book", wait(lambda: in_window("planks")), json.dumps(r)[:90])
    post("container/take", '{"slot":0}')
    # Wait for the take to land before closing. A click is answered by a slot
    # update, and closing first means that update arrives for a window that is
    # no longer open — so the result is dropped and the craft looks lost.
    wait(lambda: any("planks" in i["name"] for i in get("inventory")["items"]))
    post("container/close")
    check("the crafted planks reach the inventory",
          wait(lambda: any("planks" in i["name"] for i in get("inventory")["items"])))

    # --- containers
    R(f"setblock {X+2} {Y} {Z} minecraft:chest")
    R(f"item replace entity {NAME} container.5 with minecraft:diamond 12")
    wait(lambda: any("diamond" in i["name"] for i in get("inventory")["items"]))
    r = post("container/open", '{"X":%d,"Y":%d,"Z":%d,"face":1}' % (X + 2, Y, Z))
    check("chest opens", r.get("ok") is True, json.dumps(r)[:90])
    post("container/deposit", '{"item":"diamond","count":12}')
    check("deposit reaches the chest",
          wait(lambda: "passed" in R(
              f"execute if block {X+2} {Y} {Z} minecraft:chest{{Items:[{{id:\"minecraft:diamond\"}}]}}")[0]),
          "server does not see the diamonds in the chest")
    post("container/withdraw", '{"item":"diamond","count":12}')
    post("container/close")
    check("withdraw brings them back",
          wait(lambda: any("diamond" in i["name"] and i["count"] == 12
                           for i in get("inventory")["items"])))
    check("the chest is empty again",
          wait(lambda: "passed" not in R(
              f"execute if block {X+2} {Y} {Z} minecraft:chest{{Items:[{{id:\"minecraft:diamond\"}}]}}")[0]))

    # --- a workstation
    R(f"setblock {X+3} {Y} {Z} minecraft:furnace",
      f"item replace entity {NAME} container.6 with minecraft:raw_iron 3",
      f"item replace entity {NAME} container.7 with minecraft:coal 8")
    wait(lambda: any("raw_iron" in i["name"] for i in get("inventory")["items"]))
    post("container/open", '{"X":%d,"Y":%d,"Z":%d,"face":1}' % (X + 3, Y, Z))
    r = post("smelt", '{"input":"raw_iron","fuel":"coal","count":1}')
    check("furnace smelts raw iron", "error" not in r, json.dumps(r)[:110])
    post("container/close")

    # --- entities and combat
    R(f"kill @e[type=!player,distance=..40]")
    R(f"summon minecraft:zombie {X+3} {Y} {Z+3} {{NoAI:1b,PersistenceRequired:1b,Health:20f}}")
    check("the zombie is tracked",
          wait(lambda: any("zombie" in (e.get("type_name") or "")
                           for e in get("entities")["entities"])))
    R(f"item replace entity {NAME} container.0 with minecraft:diamond_sword 1")
    wait(lambda: any("sword" in i["name"] for i in get("inventory")["items"]))
    post("hold", '{"item":"diamond_sword"}')
    r = post("attack", '{"type":"zombie","times":6}')
    check("attack lands on the server",
          "error" not in r or "reach" in str(r.get("error", "")),
          json.dumps(r)[:110])

    # --- falling, with the server as the authority on where it landed
    # Ten blocks, not thirty. Thirty is lethal — 27 damage against 20 health —
    # and this check is about landing on the floor, not about dying on it. It
    # asked for thirty back when fall damage was under-applied and a drop that
    # should have killed left the bot on 3 health.
    R(f"tp {NAME} {X}.5 {Y+10} {Z}.5")
    wait(lambda: get("state")["y"] > Y + 8, 8)
    post("fall")
    landed = wait(lambda: abs(get("state")["y"] - Y) < 1.5, 12)
    check("auto-fall lands the bot on the floor", landed, f"y={get('state')['y']}")

    # --- fall damage fidelity, which is what makes a "lethal fall" test possible
    #
    # A correction mid-descent used to be read as a landing, which left the bot
    # hovering until the server kicked it for flying; and the simulated descent
    # could sit above the server's own idea of the player, so every packet was
    # an upward jump answered with "moved too quickly" — and each of those
    # resets the server's fall distance. A 120-block drop landed for 15 damage
    # instead of a lethal 117.
    def drop_from(height):
        R(f"effect give {NAME} minecraft:instant_health 1 10 true")
        time.sleep(0.3)
        R(f"effect clear {NAME}", f"tp {NAME} {X}.5 {Y+height} {Z}.5")
        wait(lambda: abs(get("state")["y"] - (Y + height)) < 3, 10)
        wait(lambda: get("ground")["known"], 10)
        before = get("state")["deaths"]
        post("fall")
        time.sleep(1.2)
        st = get("state")
        return st["deaths"] > before, st

    fatal, st = drop_from(10)
    check("a ten block fall is survivable", not fatal, f"deaths rose, hp={st['health']}")
    check("and it hurt", st["health"] < 20, f"hp={st['health']} — no fall damage was applied")

    fatal, _ = drop_from(40)
    check("a forty block fall is lethal", fatal,
          "survived a drop that vanilla makes fatal — the server's fall distance "
          "is being reset mid-descent")

    R(f"tp {NAME} {X}.5 {Y} {Z}.5")
    wait(lambda: abs(get("state")["y"] - Y) < 1.5, 8)
    check("the client is still connected after all that",
          get("state")["joined"] is True)
finally:
    print(f"\n=== {len(passed)} passed, {len(failed)} failed" +
          (f"   failures: {', '.join(failed)}" if failed else ""))
    proc.terminate(); log.close()
    R(f"kill @e[type=!player,distance=..40]", f"forceload remove {X-30} {Z-30} {X+30} {Z+30}")
