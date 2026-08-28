package control

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	understudy "github.com/blocktopiaworld/understudy-client/understudy"
)

func newTestServer(bot Bot) http.Handler {
	return New(bot, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
}

// call issues a request and returns the status and decoded body.
func call(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.ContentLength = int64(len(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: response is not JSON: %v (body %q)", method, path, err, rec.Body)
		}
	}
	return rec.Code, out
}

func TestStateReportsTheBot(t *testing.T) {
	bot := newStubBot()
	bot.deaths = 2
	code, out := call(t, newTestServer(bot), http.MethodGet, "/state", "")

	if code != http.StatusOK {
		t.Fatalf("GET /state = %d, want 200", code)
	}
	for k, v := range map[string]any{
		"username": "StubBot",
		"state":    "play",
		"joined":   true,
		"dead":     false,
		"deaths":   float64(2),
		"health":   float64(20),
		"x":        float64(1),
		"y":        float64(64),
	} {
		if out[k] != v {
			t.Errorf("/state[%q] = %v, want %v", k, out[k], v)
		}
	}
	// The UUID is how anything checking up on the bot addresses it, so it has
	// to be reported.
	if uuid, _ := out["uuid"].(string); uuid != bot.UUID().String() {
		t.Errorf("/state[uuid] = %q, want %q", uuid, bot.UUID())
	}
}

// A malformed request is the caller's fault (400); an action the world refuses
// is not (409). Answering 409 for both leaves a caller unable to tell a bug in
// its own request from a refusal.
func TestStatusCodesDistinguishBadRequestFromRefusal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bot      func() *stubBot
		path     string
		body     string
		wantCode int
	}{
		{"malformed JSON", newStubBot, "/move", `{"x": }`, http.StatusBadRequest},
		{"unknown field", newStubBot, "/move", `{"xx": 1}`, http.StatusBadRequest},
		{"missing required argument", newStubBot, "/look", `{}`, http.StatusBadRequest},
		{
			name: "the world refuses",
			bot: func() *stubBot {
				b := newStubBot()
				b.err = errRefused
				return b
			},
			path: "/move", body: `{"x":1,"y":2,"z":3}`, wantCode: http.StatusConflict,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := call(t, newTestServer(tc.bot()), http.MethodPost, tc.path, tc.body)
			if code != tc.wantCode {
				t.Errorf("POST %s = %d, want %d (body %v)", tc.path, code, tc.wantCode, out)
			}
			if out["error"] == nil {
				t.Errorf("POST %s returned no error message", tc.path)
			}
		})
	}
}

// A typo'd key would otherwise be silently ignored and the verb would run with
// defaults, which reads as "the bot ignored me".
func TestUnknownFieldsAreRejected(t *testing.T) {
	code, out := call(t, newTestServer(newStubBot()), http.MethodPost, "/dig",
		`{"X":1,"Y":2,"Z":3,"hold_msec":900}`)
	if code != http.StatusBadRequest {
		t.Errorf("POST /dig with a typo'd field = %d, want 400 (%v)", code, out)
	}
}

func TestVerbsWithNoArgumentsAcceptAnEmptyBody(t *testing.T) {
	for _, path := range []string{"/swing", "/use", "/drop"} {
		t.Run(path, func(t *testing.T) {
			if code, _ := call(t, newTestServer(newStubBot()), http.MethodPost, path, ""); code != http.StatusOK {
				t.Errorf("POST %s with no body = %d, want 200", path, code)
			}
		})
	}
}

// Every form is checked most-specific first, so a body carrying several does
// something predictable rather than depending on field order.
func TestLookAcceptsEveryForm(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantCall string
	}{
		{"named direction", `{"direction":"north"}`, "LookDirection:north"},
		{"a named player", `{"player":"Someone"}`, "LookAtPlayer:Someone"},
		{"nearest of a type", `{"entity_type":"chicken"}`, "LookAtNearest:chicken"},
		{"a block", `{"block":{"X":1,"Y":2,"Z":3}}`, "LookAtBlock"},
		{"an exact point", `{"x":1,"y":2,"z":3}`, "LookAt"},
		{"yaw alone", `{"yaw":90}`, "LookYawPitch"},
		{"pitch alone", `{"pitch":-20}`, "LookYawPitch"},
		{"player wins over entity_type", `{"player":"A","entity_type":"pig"}`, "LookAtPlayer:A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := newStubBot()
			code, out := call(t, newTestServer(bot), http.MethodPost, "/look", tc.body)
			if code != http.StatusOK {
				t.Fatalf("POST /look %s = %d (%v)", tc.body, code, out)
			}
			if !bot.called(tc.wantCall) {
				t.Errorf("POST /look %s called %v, want %s", tc.body, bot.calls, tc.wantCall)
			}
		})
	}
}

func TestLookReportsTheTarget(t *testing.T) {
	_, out := call(t, newTestServer(newStubBot()), http.MethodPost, "/look", `{"entity_type":"chicken"}`)
	if out["target_id"] != float64(7) {
		t.Errorf("target_id = %v, want 7", out["target_id"])
	}
	if out["target_type"] != "minecraft:chicken" {
		t.Errorf("target_type = %v, want minecraft:chicken", out["target_type"])
	}
}

// A fatal fall respawns the bot at full health, so comparing health across it
// reports a negative "damage". The kill has to be reported instead.
func TestFallReportsDamageOnlyWhenTheSameLifeSurvived(t *testing.T) {
	t.Run("survived", func(t *testing.T) {
		code, out := call(t, newTestServer(newStubBot()), http.MethodPost, "/fall", `{}`)
		if code != http.StatusOK {
			t.Fatalf("POST /fall = %d (%v)", code, out)
		}
		if out["fatal"] != false {
			t.Errorf("fatal = %v, want false", out["fatal"])
		}
		if _, ok := out["damage"]; !ok {
			t.Error("no damage reported for a survivable fall")
		}
		if out["fell_blocks"] != 3.5 {
			t.Errorf("fell_blocks = %v, want 3.5", out["fell_blocks"])
		}
	})

	t.Run("fatal", func(t *testing.T) {
		_, out := call(t, newTestServer(&fatalFallBot{stubBot: newStubBot()}),
			http.MethodPost, "/fall", `{}`)
		if out["fatal"] != true {
			t.Errorf("fatal = %v, want true", out["fatal"])
		}
		if _, ok := out["damage"]; ok {
			t.Error("damage reported across a fatal fall, where the subtraction is meaningless")
		}
	})
}

// fatalFallBot dies during the fall, so Deaths rises across the call.
type fatalFallBot struct{ *stubBot }

func (b *fatalFallBot) Fall(ctx context.Context) (float64, error) {
	b.deaths++
	return 40, nil
}

func TestFallToUsesTheGivenHeight(t *testing.T) {
	bot := newStubBot()
	if code, _ := call(t, newTestServer(bot), http.MethodPost, "/fall", `{"to_y":77}`); code != http.StatusOK {
		t.Fatalf("POST /fall = %d", code)
	}
	if !bot.called("FallTo") {
		t.Errorf("calls = %v, want FallTo", bot.calls)
	}
}

func TestDigDefaults(t *testing.T) {
	bot := newStubBot()
	if code, _ := call(t, newTestServer(bot), http.MethodPost, "/dig", `{"X":1,"Y":2,"Z":3}`); code != http.StatusOK {
		t.Fatalf("POST /dig = %d", code)
	}
	if bot.lastDig != [3]int32{1, 2, 3} {
		t.Errorf("dug %v, want (1,2,3)", bot.lastDig)
	}
	if bot.lastFace != defaultDigFace {
		t.Errorf("face = %d, want the default %d", bot.lastFace, defaultDigFace)
	}
	if bot.lastHold != defaultDigHold {
		t.Errorf("hold = %v, want the default %v", bot.lastHold, defaultDigHold)
	}
}

func TestDigOverrides(t *testing.T) {
	bot := newStubBot()
	call(t, newTestServer(bot), http.MethodPost, "/dig", `{"X":1,"Y":2,"Z":3,"face":4,"hold_ms":1200}`)
	if bot.lastFace != 4 {
		t.Errorf("face = %d, want 4", bot.lastFace)
	}
	if bot.lastHold != 1200*time.Millisecond {
		t.Errorf("hold = %v, want 1.2s", bot.lastHold)
	}
}

// The wire encodes a face as one signed byte, so 260 would truncate to 4 — a
// valid face — and work the wrong side of the block.
func TestFaceIsValidated(t *testing.T) {
	for _, face := range []string{"260", "-1", "6", "99999"} {
		for _, path := range []string{"/dig", "/place"} {
			code, out := call(t, newTestServer(newStubBot()), http.MethodPost, path,
				`{"X":1,"Y":2,"Z":3,"face":`+face+`}`)
			if code != http.StatusBadRequest {
				t.Errorf("POST %s with face %s = %d, want 400 (%v)", path, face, code, out)
			}
		}
	}
}

// One unreachable corner should not abandon the rest of the field, so the
// count is part of the answer even on failure.
func TestDigBlocksReportsProgressOnFailure(t *testing.T) {
	bot := newStubBot()
	bot.err = errRefused
	code, out := call(t, newTestServer(bot), http.MethodPost, "/dig",
		`{"blocks":[{"X":1,"Y":1,"Z":1},{"X":2,"Y":1,"Z":1},{"X":3,"Y":1,"Z":1}]}`)

	if code != http.StatusConflict {
		t.Errorf("POST /dig = %d, want 409", code)
	}
	if out["dug"] != float64(2) {
		t.Errorf("dug = %v, want 2 — progress must survive the error", out["dug"])
	}
}

// /place doubles as "right-click this block" for opening UIs, where nothing
// appears — so verifying is opt-in.
func TestPlaceVerifyIsOptIn(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		bot := newStubBot()
		call(t, newTestServer(bot), http.MethodPost, "/place", `{"X":1,"Y":2,"Z":3}`)
		if !bot.called("PlaceBlock") || bot.called("PlaceBlockVerified") {
			t.Errorf("calls = %v, want the unverified PlaceBlock", bot.calls)
		}
	})
	t.Run("verify true", func(t *testing.T) {
		bot := newStubBot()
		call(t, newTestServer(bot), http.MethodPost, "/place", `{"X":1,"Y":2,"Z":3,"verify":true}`)
		if !bot.called("PlaceBlockVerified") {
			t.Errorf("calls = %v, want PlaceBlockVerified", bot.calls)
		}
	})
}

func TestAttackDefaultsToOneHit(t *testing.T) {
	bot := newStubBot()
	code, out := call(t, newTestServer(bot), http.MethodPost, "/attack", `{"type":"zombie"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /attack = %d (%v)", code, out)
	}
	if bot.attacks != 1 {
		t.Errorf("attacks = %d, want 1", bot.attacks)
	}
	if out["hits"] != float64(1) {
		t.Errorf("hits = %v, want 1", out["hits"])
	}
}

func TestAttackNeedsATarget(t *testing.T) {
	if code, _ := call(t, newTestServer(newStubBot()), http.MethodPost, "/attack", `{}`); code != http.StatusBadRequest {
		t.Errorf("POST /attack with no target = %d, want 400", code)
	}
}

func TestCraftParsesTheLayout(t *testing.T) {
	bot := newStubBot()
	code, out := call(t, newTestServer(bot), http.MethodPost, "/craft",
		`{"layout":{"1":"oak_planks","2":"oak_planks","3":"oak_planks","4":"oak_planks"}}`)
	if code != http.StatusOK {
		t.Fatalf("POST /craft = %d (%v)", code, out)
	}
	if len(bot.layout) != 4 || bot.layout[1] != "oak_planks" {
		t.Errorf("layout = %v, want slots 1..4 of oak_planks", bot.layout)
	}
	if out["crafted"] != "minecraft:oak_planks" || out["count"] != float64(4) {
		t.Errorf("crafted = %v x%v, want minecraft:oak_planks x4", out["crafted"], out["count"])
	}
}

func TestCraftRejectsBadInput(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"no layout", `{}`},
		{"non-numeric slot", `{"layout":{"top-left":"oak_planks"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, _ := call(t, newTestServer(newStubBot()), http.MethodPost, "/craft", tc.body); code != http.StatusBadRequest {
				t.Errorf("POST /craft %s = %d, want 400", tc.body, code)
			}
		})
	}
}

// Draw time is the power control, and the curve is not linear: half a second
// gives roughly 40% power, not 50%.
func TestShootReportsThePowerCurve(t *testing.T) {
	bot := newStubBot()
	code, out := call(t, newTestServer(bot), http.MethodPost, "/shoot", `{"type":"zombie","draw_ms":500}`)
	if code != http.StatusOK {
		t.Fatalf("POST /shoot = %d (%v)", code, out)
	}
	if bot.lastDraw != 500*time.Millisecond {
		t.Errorf("draw = %v, want 500ms", bot.lastDraw)
	}
	power, _ := out["power"].(float64)
	if power >= 0.5 || power <= 0.3 {
		t.Errorf("power at half a second = %v, want ~0.4 (the curve is (t²+2t)/3)", power)
	}
}

func TestShootDefaultsToFullDraw(t *testing.T) {
	bot := newStubBot()
	call(t, newTestServer(bot), http.MethodPost, "/shoot", `{"type":"zombie"}`)
	if bot.lastDraw != understudy.BowFullDraw {
		t.Errorf("draw = %v, want the full draw %v", bot.lastDraw, understudy.BowFullDraw)
	}
}

func TestShootNeedsATarget(t *testing.T) {
	if code, _ := call(t, newTestServer(newStubBot()), http.MethodPost, "/shoot", `{}`); code != http.StatusBadRequest {
		t.Errorf("POST /shoot with no target = %d, want 400", code)
	}
}

func TestSneakDefaultsToASecond(t *testing.T) {
	bot := newStubBot()
	call(t, newTestServer(bot), http.MethodPost, "/sneak", `{}`)
	if bot.sneakFor != time.Second {
		t.Errorf("sneak = %v, want the default 1s", bot.sneakFor)
	}
	call(t, newTestServer(bot), http.MethodPost, "/sneak", `{"ms":2500}`)
	if bot.sneakFor != 2500*time.Millisecond {
		t.Errorf("sneak = %v, want 2.5s", bot.sneakFor)
	}
}

func TestDropAllFlag(t *testing.T) {
	bot := newStubBot()
	call(t, newTestServer(bot), http.MethodPost, "/drop", `{"all":true}`)
	if !bot.dropAll {
		t.Error("drop did not pass all=true through")
	}
}

func TestHoldAndEquipRequireAnItem(t *testing.T) {
	for _, path := range []string{"/hold", "/equip"} {
		if code, _ := call(t, newTestServer(newStubBot()), http.MethodPost, path, `{}`); code != http.StatusBadRequest {
			t.Errorf("POST %s with no item = %d, want 400", path, code)
		}
	}
}

// --- read-only endpoints ---

func TestBlockRequiresCoordinates(t *testing.T) {
	for _, path := range []string{"/block", "/block?x=1", "/block?x=1&y=2", "/block?x=a&y=2&z=3"} {
		t.Run(path, func(t *testing.T) {
			if code, _ := call(t, newTestServer(newStubBot()), http.MethodGet, path, ""); code != http.StatusBadRequest {
				t.Errorf("GET %s = %d, want 400", path, code)
			}
		})
	}
}

// `loaded` matters as much as the state does: an unloaded chunk reads as air
// everywhere, so "air" is only meaningful when the terrain is known.
func TestBlockReportsClassification(t *testing.T) {
	code, out := call(t, newTestServer(newStubBot()), http.MethodGet, "/block?x=1&y=2&z=3", "")
	if code != http.StatusOK {
		t.Fatalf("GET /block = %d", code)
	}
	for _, key := range []string{"state", "loaded", "solid", "water", "lava", "air", "targetable"} {
		if _, ok := out[key]; !ok {
			t.Errorf("GET /block did not report %q", key)
		}
	}
	if out["solid"] != true {
		t.Errorf("solid = %v, want true for state 1", out["solid"])
	}
}

func TestReachReportsTheLimit(t *testing.T) {
	code, out := call(t, newTestServer(newStubBot()), http.MethodGet, "/reach?x=1&y=2&z=3", "")
	if code != http.StatusOK {
		t.Fatalf("GET /reach = %d", code)
	}
	if out["reach"] != understudy.BlockReach {
		t.Errorf("reach = %v, want %v", out["reach"], understudy.BlockReach)
	}
	if out["can"] != true {
		t.Errorf("can = %v, want true", out["can"])
	}
}

func TestLookingAtReportsNoHit(t *testing.T) {
	bot := newStubBot()
	code, out := call(t, newTestServer(bot), http.MethodGet, "/lookingat", "")
	if code != http.StatusOK {
		t.Fatalf("GET /lookingat = %d", code)
	}
	if out["hit"] != false {
		t.Errorf("hit = %v, want false", out["hit"])
	}

	bot.hitOK = true
	bot.hit = understudy.RayHit{X: 1, Y: 2, Z: 3, Face: 4, Distance: 2.5, State: 9}
	_, out = call(t, newTestServer(bot), http.MethodGet, "/lookingat", "")
	if out["hit"] != true || out["x"] != float64(1) || out["face"] != float64(4) {
		t.Errorf("GET /lookingat = %v, want the hit reported", out)
	}
}

func TestEntitiesFiltering(t *testing.T) {
	bot := newStubBot()
	bot.entities = []understudy.Entity{
		{ID: 1, TypeName: "minecraft:pig", X: 1, Y: 64, Z: -2},
		{ID: 2, TypeName: "minecraft:zombie", X: 100, Y: 64, Z: -2},
		{ID: 3, TypeName: "minecraft:pig", X: 100, Y: 64, Z: -2},
	}
	h := newTestServer(bot)

	for _, tc := range []struct {
		path      string
		wantCount int
	}{
		{"/entities", 3},
		{"/entities?type=pig", 2},
		{"/entities?type=minecraft:pig", 2},
		{"/entities?radius=1", 1},
		{"/entities?type=pig&radius=1", 1},
	} {
		t.Run(tc.path, func(t *testing.T) {
			code, out := call(t, h, http.MethodGet, tc.path, "")
			if code != http.StatusOK {
				t.Fatalf("GET %s = %d", tc.path, code)
			}
			if out["count"] != float64(tc.wantCount) {
				t.Errorf("GET %s count = %v, want %d", tc.path, out["count"], tc.wantCount)
			}
		})
	}
}

func TestEntitiesRejectsBadRadius(t *testing.T) {
	if code, _ := call(t, newTestServer(newStubBot()), http.MethodGet, "/entities?radius=near", ""); code != http.StatusBadRequest {
		t.Errorf("GET /entities?radius=near = %d, want 400", code)
	}
}

// The old endpoint reported `fits_in_36` from SlotsNeeded(name, 1), which is
// trivially true for every item in the game and answered nothing.
func TestInventoryCountAnswersARealQuestion(t *testing.T) {
	h := newTestServer(newStubBot())

	code, out := call(t, h, http.MethodGet, "/inventory?count=dirt&want=2304", "")
	if code != http.StatusOK {
		t.Fatalf("GET /inventory = %d (%v)", code, out)
	}
	if out["slots_needed"] != float64(36) {
		t.Errorf("slots_needed for 2304 dirt = %v, want 36", out["slots_needed"])
	}
	if out["fits"] != true {
		t.Errorf("fits = %v, want true — 2304 dirt is exactly the inventory", out["fits"])
	}

	// Totems stack to 1, so 40 of them do not fit in 36 slots.
	_, out = call(t, h, http.MethodGet, "/inventory?count=totem_of_undying&want=40", "")
	if out["slots_needed"] != float64(40) {
		t.Errorf("slots_needed for 40 totems = %v, want 40", out["slots_needed"])
	}
	if out["fits"] != false {
		t.Errorf("fits = %v, want false — 40 unstackable items exceed 36 slots", out["fits"])
	}
}

func TestInventoryCountRejectsBadWant(t *testing.T) {
	if code, _ := call(t, newTestServer(newStubBot()), http.MethodGet, "/inventory?count=dirt&want=lots", ""); code != http.StatusBadRequest {
		t.Errorf("GET /inventory?want=lots = %d, want 400", code)
	}
}

func TestInventoryListing(t *testing.T) {
	bot := newStubBot()
	bot.items = []understudy.ItemStack{{Slot: 36, Name: "minecraft:dirt", Count: 5}}
	code, out := call(t, newTestServer(bot), http.MethodGet, "/inventory", "")
	if code != http.StatusOK {
		t.Fatalf("GET /inventory = %d", code)
	}
	if out["held_item"] != "minecraft:dirt" {
		t.Errorf("held_item = %v, want minecraft:dirt", out["held_item"])
	}
	if out["picked_up"] != float64(4) {
		t.Errorf("picked_up = %v, want 4", out["picked_up"])
	}
}

func TestGroundReportsTheGap(t *testing.T) {
	bot := newStubBot()
	bot.support = understudy.Support{Found: true, GroundY: 60}
	code, out := call(t, newTestServer(bot), http.MethodGet, "/ground", "")
	if code != http.StatusOK {
		t.Fatalf("GET /ground = %d", code)
	}
	if out["gap"] != float64(4) {
		t.Errorf("gap = %v, want 4 (y 64 over ground 60)", out["gap"])
	}
	if out["found"] != true {
		t.Errorf("found = %v, want true", out["found"])
	}
}

// --- routing ---

func TestMethodsAreEnforced(t *testing.T) {
	h := newTestServer(newStubBot())
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/dig"},
		{http.MethodPost, "/state"},
		{http.MethodDelete, "/move"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				t.Errorf("%s %s = 200, want a method rejection", tc.method, tc.path)
			}
		})
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/nonsense", nil)
	rec := httptest.NewRecorder()
	newTestServer(newStubBot()).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /nonsense = %d, want 404", rec.Code)
	}
}

// Every action response carries where the bot ended up, so a caller never has
// to make a second request to find out.
func TestActionResponsesCarryPosition(t *testing.T) {
	_, out := call(t, newTestServer(newStubBot()), http.MethodPost, "/move", `{"x":1,"y":2,"z":3}`)
	for _, key := range []string{"ok", "x", "y", "z", "yaw", "pitch"} {
		if _, ok := out[key]; !ok {
			t.Errorf("action response is missing %q: %v", key, out)
		}
	}
}

// `--control 8080` and `--control 127.0.0.1:8080` must both work.
func TestParseAddr(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"8080", ":8080"},
		{"127.0.0.1:8080", "127.0.0.1:8080"},
		{":9000", ":9000"},
		{"localhost:1", "localhost:1"},
	} {
		if got := ParseAddr(tc.in); got != tc.want {
			t.Errorf("ParseAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A cancelled context must shut the listener down and return cleanly, not
// surface ErrServerClosed as a failure.
func TestServeStopsOnContextCancel(t *testing.T) {
	s := New(newStubBot(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, "127.0.0.1:0") }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the context was cancelled")
	}
}

// A single trade must collect its result. Selecting an offer makes the result
// stack appear, which looks like success and is not: the server counts nothing
// until the result is taken, so a bot that stops there leaves the villager
// holding the bread and no trade is recorded anywhere. This route once had a
// times == 1 shortcut that called Trade and reported a hardcoded "traded": 1 —
// a literal, not a measurement, so it agreed with any assertion put to it.
func TestASingleTradeIsTakenAndCounted(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       float64
	}{
		{"implicit", `{"index":0}`, 1},
		{"explicit one", `{"index":0,"times":1}`, 1},
		{"several", `{"index":0,"times":2}`, 2},
		{"villager runs out", `{"index":0,"times":5}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := &stubBot{}
			code, out := call(t, newTestServer(bot), "POST", "/container/trade", tc.body)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%v)", code, out)
			}
			if bot.called("Trade") {
				t.Error("used Trade, which selects without collecting the result")
			}
			if !bot.called("TradeAndTake") {
				t.Error("did not collect the result")
			}
			if out["traded"] != tc.want {
				t.Errorf("traded = %v, want %v", out["traded"], tc.want)
			}
			// The count reported is the one that happened, not the one asked for.
			if out["item"] != "minecraft:bread" || out["count"] != float64(6) {
				t.Errorf("item = %v x%v, want minecraft:bread x6", out["item"], out["count"])
			}
		})
	}
}

// raw is the one path that may stop at selecting, because that is what it is
// for: a caller that wants to inspect the window itself.
func TestRawTradeOnlySelects(t *testing.T) {
	bot := &stubBot{}
	if code, out := call(t, newTestServer(bot), "POST", "/container/trade",
		`{"index":0,"raw":true}`); code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%v)", code, out)
	}
	if bot.called("TradeAndTake") {
		t.Error("raw collected the result; it is meant to select and stop")
	}
	if !bot.called("SelectTrade") {
		t.Error("raw did not select the trade")
	}
}

// Twenty endpoints used to refuse until the caller had opened the right window
// themselves, while the client already knew which window each one needed. They
// now take the block to work at. Nothing that already worked changes: with no
// position given, the behaviour is what it was.
func TestVerbsOpenTheStationTheyAreGiven(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"smelt", "/smelt", `{"X":1,"Y":2,"Z":3,"input":"raw_iron","fuel":"coal"}`},
		{"anvil", "/anvil", `{"X":1,"Y":2,"Z":3,"first":"a","second":"b"}`},
		{"rename", "/rename", `{"X":1,"Y":2,"Z":3,"item":"a","name":"b"}`},
		{"grindstone", "/grindstone", `{"X":1,"Y":2,"Z":3,"item":"a"}`},
		{"loom", "/loom", `{"X":1,"Y":2,"Z":3,"banner":"a","dye":"b"}`},
		{"brew", "/brew", `{"X":1,"Y":2,"Z":3,"bottle":"a","ingredient":"b","fuel":"c"}`},
		{"deposit", "/container/deposit", `{"X":1,"Y":2,"Z":3,"item":"a","all":true}`},
		{"withdraw", "/container/withdraw", `{"X":1,"Y":2,"Z":3,"item":"a","count":1}`},
		{"put", "/container/put", `{"X":1,"Y":2,"Z":3,"item":"a","slot":0}`},
		{"take", "/container/take", `{"X":1,"Y":2,"Z":3,"slot":0}`},
		{"grid", "/container/grid", `{"X":1,"Y":2,"Z":3,"layout":{"1":"a"}}`},
		{"trade", "/container/trade", `{"X":1,"Y":2,"Z":3,"index":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := &stubBot{}
			if code, out := call(t, newTestServer(bot), "POST", tc.path, tc.body); code != http.StatusOK {
				t.Fatalf("status = %d (%v)", code, out)
			}
			if !bot.called("OpenContainer") {
				t.Errorf("%s did not open the block it was given; calls were %v", tc.path, bot.calls)
			}
		})
	}
}

// A merchant is an entity, not a block, so trading names one the same way.
func TestTradeOpensTheMerchantItIsGiven(t *testing.T) {
	bot := &stubBot{}
	if code, out := call(t, newTestServer(bot), "POST", "/container/trade",
		`{"at":"villager","index":0}`); code != http.StatusOK {
		t.Fatalf("status = %d (%v)", code, out)
	}
	if !bot.called("OpenContainerOnNearest:villager") {
		t.Errorf("did not open the merchant; calls were %v", bot.calls)
	}
}

// Naming nothing has to behave exactly as before, or this is not additive.
func TestVerbsWithoutAStationDoNotOpenAnything(t *testing.T) {
	bot := &stubBot{}
	if code, out := call(t, newTestServer(bot), "POST", "/container/deposit",
		`{"item":"a","all":true}`); code != http.StatusOK {
		t.Fatalf("status = %d (%v)", code, out)
	}
	if bot.called("OpenContainer") {
		t.Error("opened a window nobody asked for")
	}
}

// Half a position is a mistake worth naming, not a shrug.
func TestAPartialStationIsRejected(t *testing.T) {
	bot := &stubBot{}
	code, out := call(t, newTestServer(bot), "POST", "/container/deposit",
		`{"X":1,"Y":2,"item":"a","all":true}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%v)", code, out)
	}
}

// /consume has always taken its item and held it. Placing and shooting made the
// caller arrange their own hand, for no reason anyone could name.
func TestPlaceAndShootHoldTheItemTheyAreGiven(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"place", "/place", `{"X":1,"Y":2,"Z":3,"item":"minecraft:stone"}`},
		{"shoot", "/shoot", `{"type":"zombie","item":"minecraft:bow"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := &stubBot{}
			if code, out := call(t, newTestServer(bot), "POST", tc.path, tc.body); code != http.StatusOK {
				t.Fatalf("status = %d (%v)", code, out)
			}
			if !bot.calledPrefix("HoldItem") {
				t.Errorf("%s did not hold the item; calls were %v", tc.path, bot.calls)
			}
		})
	}
}

// A single dig answered with the envelope alone while a batch answered with a
// count, so a caller wanting one number had to send a one-element array.
func TestDigReportsACountForOneBlockAndForMany(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       float64
	}{
		{"one", `{"X":1,"Y":2,"Z":3}`, 1},
		{"several", `{"blocks":[{"X":1,"Y":2,"Z":3},{"X":1,"Y":2,"Z":4}]}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := call(t, newTestServer(&stubBot{}), "POST", "/dig", tc.body)
			if code != http.StatusOK {
				t.Fatalf("status = %d (%v)", code, out)
			}
			if out["dug"] != tc.want {
				t.Errorf("dug = %v, want %v", out["dug"], tc.want)
			}
		})
	}
}

// Dropping answered with nothing at all, which is how the client's inventory
// going stale after a drop stayed invisible until a test asked the server.
func TestDropReportsWhatWent(t *testing.T) {
	bot := &stubBot{}
	code, out := call(t, newTestServer(bot), "POST", "/drop", `{"all":true}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d (%v)", code, out)
	}
	if out["item"] == nil || out["dropped"] == nil {
		t.Errorf("drop said nothing about what it dropped: %v", out)
	}
}
