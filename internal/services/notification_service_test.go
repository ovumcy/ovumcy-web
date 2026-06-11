package services

import "testing"

func TestResolveSettingsStatusPrefersFlashValue(t *testing.T) {
	service := NewNotificationService()

	got := service.ResolveSettingsStatus(" cycle_updated ")
	if got != "cycle_updated" {
		t.Fatalf("expected cycle_updated, got %q", got)
	}
}

func TestResolveSettingsStatusReturnsEmptyWithoutFlash(t *testing.T) {
	service := NewNotificationService()

	got := service.ResolveSettingsStatus("   ")
	if got != "" {
		t.Fatalf("expected empty status, got %q", got)
	}
}

func TestResolveSettingsErrorSourcePrefersFlashValue(t *testing.T) {
	service := NewNotificationService()

	got := service.ResolveSettingsErrorSource(" invalid current password ")
	if got != "invalid current password" {
		t.Fatalf("expected invalid current password, got %q", got)
	}
}

func TestClassifySettingsErrorSource_ChangePassword(t *testing.T) {
	service := NewNotificationService()

	got := service.ClassifySettingsErrorSource(" password mismatch ")
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
	service := NewNotificationService()

	keys := []string{
		SettingsPasswordChangeKeyInvalidInput,
		SettingsPasswordChangeKeyPasswordMismatch,
		SettingsPasswordChangeKeyInvalidCurrent,
		SettingsPasswordChangeKeyMustDiffer,
		SettingsPasswordChangeKeyWeakPassword,
	}
	for _, key := range keys {
		if got := service.ClassifySettingsErrorSource(key); got != SettingsErrorTargetChangePassword {
			t.Errorf("key %q: expected change-password target, got %q", key, got)
		}
	}
}

func TestClassifySettingsErrorSource_General(t *testing.T) {
	service := NewNotificationService()

	got := service.ClassifySettingsErrorSource("invalid profile input")
	if got != SettingsErrorTargetGeneral {
		t.Fatalf("expected general target, got %q", got)
	}
}
