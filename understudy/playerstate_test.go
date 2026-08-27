package understudy

import "testing"

// A working totem is consumed. "Alive with the totem intact" is not a totem
// that failed to fire, it is damage that never arrived — and these are the
// states that stop it arriving.
func TestWhyNotDamageableNamesWhatBlocksDamage(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode GameMode
		eff  []Effect
		want string
	}{
		{"survival with nothing on", GameModeSurvival, nil, ""},
		{"adventure is still damageable", GameModeAdventure, nil, ""},
		{"creative", GameModeCreative, nil, "creative"},
		{"spectator", GameModeSpectator, nil, "spectator"},
		{"never told", GameModeUnknown, nil, "has not said"},
		{
			"resistance V blocks everything", GameModeSurvival,
			[]Effect{{Name: "minecraft:resistance", Amplifier: 4}}, "Resistance 5",
		},
		{
			// Below V it reduces damage but does not stop it, so a totem there
			// really would fire and this must not blame the effect.
			"resistance IV still lets damage through", GameModeSurvival,
			[]Effect{{Name: "minecraft:resistance", Amplifier: 3}}, "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			c.gameMode = tc.mode
			for _, e := range tc.eff {
				c.effects.set(e)
			}
			err := c.WhyNotDamageable()
			if tc.want == "" {
				if err != nil {
					t.Errorf("expected damageable, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected %q to block damage", tc.want)
			}
			if !contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// Amplifier 0 is level I. Getting this off by one would call Resistance IV
// total immunity and blame the wrong thing.
func TestEffectLevelIsAmplifierPlusOne(t *testing.T) {
	if got := (Effect{Amplifier: 0}).Level(); got != 1 {
		t.Errorf("amplifier 0 is level %d, want I", got)
	}
	if got := (Effect{Amplifier: 4}).Level(); got != 5 {
		t.Errorf("amplifier 4 is level %d, want V", got)
	}
}

// Unknown must not be survival's zero value, or "never told" and "told it was
// survival" become the same answer.
func TestUnknownGameModeIsDistinctFromSurvival(t *testing.T) {
	if GameModeUnknown == GameModeSurvival {
		t.Fatal("unknown and survival must differ")
	}
	if GameModeUnknown.Damageable() {
		t.Error("an unknown mode must not claim to be damageable")
	}
	if !GameModeSurvival.Damageable() || !GameModeAdventure.Damageable() {
		t.Error("survival and adventure take damage")
	}
	if GameModeCreative.Damageable() || GameModeSpectator.Damageable() {
		t.Error("creative and spectator do not")
	}
}
