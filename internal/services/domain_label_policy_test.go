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
		{"role owner", RoleTranslationKey("owner"), "role.owner"},
		{"role passthrough", RoleTranslationKey("guest"), "guest"},
		{"icon menstrual", PhaseIcon("menstrual"), "\U0001FA78"},
		{"icon default", PhaseIcon("bad"), "\u2728"},
		// "fertile" is a fertility status, not a phase (plan item 24): the
		// retired phase value must fall through to the unknown mapping.
		{"phase retired fertile", PhaseTranslationKey("fertile"), "phases.unknown"},
		{"icon retired fertile", PhaseIcon("fertile"), "\u2728"},
		{"symptom pain", SymptomGroup("Cramps"), "pain"},
		{"symptom digestion", SymptomGroup("Food cravings"), "digestion"},
		{"symptom other", SymptomGroup("Custom symptom"), "other"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
