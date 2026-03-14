package services

import "testing"

func TestDomainLabelPolicy(t *testing.T) {
	if got := PhaseTranslationKey("ovulation"); got != "phases.ovulation" {
		t.Fatalf("expected ovulation key, got %q", got)
	}
	if got := PhaseTranslationKey("unknown-phase"); got != "phases.unknown" {
		t.Fatalf("expected unknown phase key, got %q", got)
	}

	if got := FlowTranslationKey("light"); got != "dashboard.flow.light" {
		t.Fatalf("expected light flow key, got %q", got)
	}
	if got := FlowTranslationKey("unexpected"); got != "dashboard.flow.none" {
		t.Fatalf("expected none flow key, got %q", got)
	}

	if got := RoleTranslationKey("owner"); got != "role.owner" {
		t.Fatalf("expected owner role key, got %q", got)
	}
	if got := RoleTranslationKey("guest"); got != "guest" {
		t.Fatalf("expected passthrough role for unknown, got %q", got)
	}

	if got := PhaseIcon("menstrual"); got != "\U0001FA78" {
		t.Fatalf("expected menstrual icon, got %q", got)
	}
	if got := PhaseIcon("fertile"); got != "\U0001F33F" {
		t.Fatalf("expected fertile icon, got %q", got)
	}
	if got := PhaseIcon("bad"); got != "\u2728" {
		t.Fatalf("expected default icon, got %q", got)
	}

	if got := SymptomGroup("Cramps"); got != "pain" {
		t.Fatalf("expected pain group, got %q", got)
	}
	if got := SymptomGroup("Food cravings"); got != "digestion" {
		t.Fatalf("expected digestion group, got %q", got)
	}
	if got := SymptomGroup("Custom symptom"); got != "other" {
		t.Fatalf("expected other group, got %q", got)
	}
}
