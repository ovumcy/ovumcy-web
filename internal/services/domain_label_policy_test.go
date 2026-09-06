package services

import (
	"fmt"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestDomainLabelPolicy(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"phase ovulation", PhaseTranslationKey("ovulation"), "phases.ovulation"},
		{"phase unknown", PhaseTranslationKey("unknown-phase"), "phases.unknown"},
		{"flow light", FlowTranslationKey("light"), "dashboard.flow.light"},
		{"flow fallback", FlowTranslationKey("unexpected"), "dashboard.flow.none"},
		{"pregnancy negative", PregnancyTestTranslationKey("negative"), "dashboard.pregnancy_test.negative"},
		{"pregnancy positive", PregnancyTestTranslationKey("positive"), "dashboard.pregnancy_test.positive"},
		{"pregnancy fallback", PregnancyTestTranslationKey("unexpected"), "dashboard.pregnancy_test.none"},
		{"icon menstrual", PhaseIcon("menstrual"), "drop"},
		{"icon default", PhaseIcon("bad"), "sparkle"},
		// "fertile" is a fertility status, not a phase (plan item 24): the
		// retired phase value must fall through to the unknown mapping.
		{"phase retired fertile", PhaseTranslationKey("fertile"), "phases.unknown"},
		{"icon retired fertile", PhaseIcon("fertile"), "sparkle"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// domainLabelTranslationKeys feeds every enum value the day-entry validator
// accepts through its label function and returns the resulting catalogue keys,
// spec-labelled so a failure names the value that produced the key.
//
// The switch-based key functions sit OUTSIDE the map sweep in i18n_policy_test.go
// — that helper was written as "the guard for the defect CLASS", but it takes a
// map[string]string, and these keys are produced by switch statements — so a key
// added with a typo, or renamed in the catalogue alone, passes both the switch
// test above and the map sweep while rendering as the raw key
// ("dashboard.mood.very_low") on the dashboard and the calendar in all six
// languages: the #287 failure the sweep exists to stop.
func domainLabelTranslationKeys(t *testing.T) map[string]string {
	t.Helper()

	keys := map[string]string{}
	add := func(spec string, key string) {
		if key == "" {
			t.Fatalf("%s resolved to no key at all", spec)
		}
		keys[spec] = key
	}

	for _, phase := range []string{"menstrual", "follicular", "ovulation", "luteal", "unrecognized"} {
		add("phase "+phase, PhaseTranslationKey(phase))
	}
	for _, flow := range []string{models.FlowNone, models.FlowSpotting, models.FlowLight, models.FlowMedium, models.FlowHeavy, "unrecognized"} {
		add("flow "+flow, FlowTranslationKey(flow))
	}
	for _, activity := range []string{models.SexActivityNone, models.SexActivityProtected, models.SexActivityUnprotected, "unrecognized"} {
		add("sex activity "+activity, SexActivityTranslationKey(activity))
	}
	for _, mucus := range []string{models.CervicalMucusNone, models.CervicalMucusDry, models.CervicalMucusMoist, models.CervicalMucusCreamy, models.CervicalMucusEggWhite, "unrecognized"} {
		add("cervical mucus "+mucus, CervicalMucusTranslationKey(mucus))
	}
	for _, result := range []string{models.PregnancyTestNone, models.PregnancyTestNegative, models.PregnancyTestPositive, "unrecognized"} {
		add("pregnancy test "+result, PregnancyTestTranslationKey(result))
	}
	for _, step := range DayMoodScale().Steps {
		add(fmt.Sprintf("mood step %d", step), MoodTranslationKey(step))
	}
	return keys
}

// TestDomainLabelTranslationKeysResolveInEveryLocale is the switch-statement
// sibling of the authErrorTranslationKeys / settingsStatusTranslationKeys
// sweeps: every key a day-field label function can produce must exist in every
// SupportedLanguages() catalogue. A locale-parity test in internal/i18n catches
// a key missing from ONE locale; only this sweep catches a key present in NO
// locale, which is exactly what a typo in a switch arm produces.
func TestDomainLabelTranslationKeysResolveInEveryLocale(t *testing.T) {
	assertMappedTranslationKeysResolveInEveryLocale(t, "domain label switch functions", domainLabelTranslationKeys(t))
}

// TestPhaseTranslationKeyNamesTheWithheldStatus keeps the ribbon's suppressed
// status out of the "unknown" bucket: unknown reads as a phase the app failed
// to work out, and this one is a phase the app is deliberately not naming.
func TestPhaseTranslationKeyNamesTheWithheldStatus(t *testing.T) {
	if got := PhaseTranslationKey("withheld"); got != "phases.withheld" {
		t.Errorf("PhaseTranslationKey(\"withheld\") = %q, want phases.withheld", got)
	}
	if got := PhaseTranslationKey("beyond"); got != "phases.unknown" {
		t.Errorf("PhaseTranslationKey(\"beyond\") = %q, want phases.unknown", got)
	}
}
