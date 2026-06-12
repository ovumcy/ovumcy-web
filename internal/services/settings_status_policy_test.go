package services

import "testing"

func TestResolveSettingsStatusPrefersFlashValue(t *testing.T) {
	got := ResolveSettingsStatus(" cycle_updated ")
	if got != "cycle_updated" {
		t.Fatalf("expected cycle_updated, got %q", got)
	}
}

func TestResolveSettingsStatusReturnsEmptyWithoutFlash(t *testing.T) {
	got := ResolveSettingsStatus("   ")
	if got != "" {
		t.Fatalf("expected empty status, got %q", got)
	}
}

func TestResolveSettingsErrorSourcePrefersFlashValue(t *testing.T) {
	got := ResolveSettingsErrorSource(" invalid current password ")
	if got != "invalid current password" {
		t.Fatalf("expected invalid current password, got %q", got)
	}
}

func TestClassifySettingsErrorSource_ChangePassword(t *testing.T) {
	got := ClassifySettingsErrorSource(" password mismatch ")
	if got != SettingsErrorTargetChangePassword {
		t.Fatalf("expected change-password target, got %q", got)
	}
}

// TestClassifySettingsErrorSource_AllPasswordChangeKeys ensures every
// SettingsPasswordChangeKey* constant routes to the change-password section.
// If a constant value is renamed or a new case is added to
// mapSettingsPasswordChangeError without a matching entry here, this table
// will catch the drift.
func TestClassifySettingsErrorSource_AllPasswordChangeKeys(t *testing.T) {
	keys := []string{
		SettingsPasswordChangeKeyInvalidInput,
		SettingsPasswordChangeKeyPasswordMismatch,
		SettingsPasswordChangeKeyInvalidCurrent,
		SettingsPasswordChangeKeyMustDiffer,
		SettingsPasswordChangeKeyWeakPassword,
	}
	for _, key := range keys {
		if got := ClassifySettingsErrorSource(key); got != SettingsErrorTargetChangePassword {
			t.Errorf("key %q: expected change-password target, got %q", key, got)
		}
	}
}

func TestClassifySettingsErrorSource_General(t *testing.T) {
	got := ClassifySettingsErrorSource("invalid profile input")
	if got != SettingsErrorTargetGeneral {
		t.Fatalf("expected general target, got %q", got)
	}
}
