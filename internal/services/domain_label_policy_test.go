package services

import "testing"

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
