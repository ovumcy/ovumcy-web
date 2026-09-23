package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestUserRepositoryOwnerScopedWritersPersist exercises, once each, the
// remaining users-table UPDATE methods that route through scopedUserUpdate
// but had no dedicated regression elsewhere in this package: UpdateByID,
// SaveOnboardingStep1, SaveOnboardingStep2, UpdateTOTPFieldsAndRevokeSessions
// and UpdatePasswordRecoveryCodeAndRevokeSessions. Each is otherwise reached
// only through internal/services, whose tests do not run against this
// package's coverage profile.
func TestUserRepositoryOwnerScopedWritersPersist(t *testing.T) {
	repo := openTimezoneRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "owner-scope-coverage@example.com")
	ctx := context.Background()

	if err := repo.UpdateByID(ctx, user.ID, map[string]any{"display_name": "Renamed"}); err != nil {
		t.Fatalf("UpdateByID: %v", err)
	}
	reloaded, err := repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("reload after UpdateByID: %v", err)
	}
	if reloaded.DisplayName != "Renamed" {
		t.Fatalf("expected display name Renamed, got %q", reloaded.DisplayName)
	}

	if err := repo.SaveOnboardingStep1(ctx, user.ID, reloaded.CreatedAt); err != nil {
		t.Fatalf("SaveOnboardingStep1: %v", err)
	}
	if err := repo.SaveOnboardingStep2(ctx, user.ID, 30, 6, true, false, "health"); err != nil {
		t.Fatalf("SaveOnboardingStep2: %v", err)
	}
	reloaded, err = repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("reload after onboarding steps: %v", err)
	}
	if reloaded.CycleLength != 30 || reloaded.PeriodLength != 6 {
		t.Fatalf("expected onboarding step 2 to persist cycle/period length, got %d/%d", reloaded.CycleLength, reloaded.PeriodLength)
	}

	if err := repo.UpdateTOTPFieldsAndRevokeSessions(ctx, user.ID, "encrypted-secret", true); err != nil {
		t.Fatalf("UpdateTOTPFieldsAndRevokeSessions: %v", err)
	}
	reloaded, err = repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("reload after UpdateTOTPFieldsAndRevokeSessions: %v", err)
	}
	if !reloaded.TOTPEnabled || reloaded.TOTPSecret != "encrypted-secret" {
		t.Fatalf("expected TOTP fields persisted, got enabled=%v secret=%q", reloaded.TOTPEnabled, reloaded.TOTPSecret)
	}
	versionBeforeReset := reloaded.AuthSessionVersion

	if err := repo.UpdatePasswordRecoveryCodeAndRevokeSessions(ctx, user.ID, "new-password-hash", "new-recovery-hash", true, nil); err != nil {
		t.Fatalf("UpdatePasswordRecoveryCodeAndRevokeSessions: %v", err)
	}
	reloaded, err = repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("reload after UpdatePasswordRecoveryCodeAndRevokeSessions: %v", err)
	}
	if reloaded.PasswordHash != "new-password-hash" || reloaded.RecoveryCodeHash != "new-recovery-hash" {
		t.Fatalf("expected password and recovery hash persisted, got %q/%q", reloaded.PasswordHash, reloaded.RecoveryCodeHash)
	}
	if reloaded.AuthSessionVersion <= versionBeforeReset {
		t.Fatalf("expected auth_session_version to advance past %d, got %d", versionBeforeReset, reloaded.AuthSessionVersion)
	}
}

// TestUserRepositoryOwnerScopedWritersRefuseZeroOwner is the zero-id half of
// the coverage above, one call per method, so both branches of
// scopedUserUpdate's refusal are exercised for every writer this file adds.
func TestUserRepositoryOwnerScopedWritersRefuseZeroOwner(t *testing.T) {
	repo := openTimezoneRepoForTest(t)
	ctx := context.Background()

	cases := map[string]func() error{
		"UpdateByID": func() error {
			return repo.UpdateByID(ctx, 0, map[string]any{"display_name": "x"})
		},
		"SaveOnboardingStep1": func() error {
			return repo.SaveOnboardingStep1(ctx, 0, time.Now().UTC())
		},
		"SaveOnboardingStep2": func() error {
			return repo.SaveOnboardingStep2(ctx, 0, 28, 5, true, false, "health")
		},
		"UpdateTOTPFieldsAndRevokeSessions": func() error {
			return repo.UpdateTOTPFieldsAndRevokeSessions(ctx, 0, "secret", true)
		},
		"UpdatePasswordRecoveryCodeAndRevokeSessions": func() error {
			return repo.UpdatePasswordRecoveryCodeAndRevokeSessions(ctx, 0, "hash", "recovery", true, nil)
		},
	}

	for name, call := range cases {
		if err := call(); err == nil {
			t.Fatalf("%s: expected ErrUserOwnerRequired for a zero owner id, got nil", name)
		} else if err != ErrUserOwnerRequired {
			t.Fatalf("%s: expected ErrUserOwnerRequired, got %v", name, err)
		}
	}
}

// TestUserRepositoryRemainingScopedWritersRefuseZeroOwner covers the zero-id
// refusal path (the `if err != nil { return err }` arm right after
// scopedUserUpdate/requireUserOwnerID) for every users-table UPDATE method
// that had no dedicated zero-id regression elsewhere in this package. Each of
// these has no other test constructing it with a zero id, so this file is the
// only place their guard's error branch runs at all.
//
// The last three cases (MarkWebhookDelivered, ReleaseWebhookWatermark,
// BackfillCalendarFeedVerifierMAC) are the compound-predicate writers that
// call requireUserOwnerID directly rather than going through
// scopedUserUpdate/scopedUserUpdateTx — see the owner-scope guard test.
func TestUserRepositoryRemainingScopedWritersRefuseZeroOwner(t *testing.T) {
	repo := openTimezoneRepoForTest(t)
	ctx := context.Background()

	cases := map[string]func() error{
		"UpdateDisplayName": func() error {
			return repo.UpdateDisplayName(ctx, 0, "x")
		},
		"UpdateInterfaceLanguage": func() error {
			_, err := repo.UpdateInterfaceLanguage(ctx, 0, "en")
			return err
		},
		"UpdateReminderLeadDays": func() error {
			return repo.UpdateReminderLeadDays(ctx, 0, 3)
		},
		"SaveWebhookSettings": func() error {
			return repo.SaveWebhookSettings(ctx, 0, models.WebhookSettingsColumns{})
		},
		"RemoveWebhookDestination": func() error {
			return repo.RemoveWebhookDestination(ctx, 0)
		},
		"SaveCalendarFeedToken": func() error {
			return repo.SaveCalendarFeedToken(ctx, 0, models.CalendarFeedTokenColumns{})
		},
		"ClearCalendarFeedToken": func() error {
			return repo.ClearCalendarFeedToken(ctx, 0)
		},
		"UpdateRecoveryCodeHashAndRevokeSessions": func() error {
			return repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, 0, "recovery", nil)
		},
		"UpdatePasswordAndRevokeSessions": func() error {
			return repo.UpdatePasswordAndRevokeSessions(ctx, 0, "hash", false)
		},
		"ForceResetPasswordAndRevokeSessions": func() error {
			return repo.ForceResetPasswordAndRevokeSessions(ctx, 0, "hash")
		},
		"UpgradePasswordHashCAS": func() error {
			_, err := repo.UpgradePasswordHashCAS(ctx, 0, "hash", "upgraded")
			return err
		},
		"BumpAuthSessionVersion": func() error {
			return repo.BumpAuthSessionVersion(ctx, 0)
		},
		"UpdateTOTPSecretCiphertext": func() error {
			return repo.UpdateTOTPSecretCiphertext(ctx, 0, "secret")
		},
		"ClearAllDataAndResetSettings": func() error {
			return repo.ClearAllDataAndResetSettings(ctx, 0)
		},
		"MarkWebhookDelivered": func() error {
			return repo.MarkWebhookDelivered(ctx, 0, time.Now().UTC(), 1)
		},
		"ReleaseWebhookWatermark": func() error {
			return repo.ReleaseWebhookWatermark(ctx, 0, models.WebhookReminderTypePeriod, time.Now().UTC(), nil)
		},
		"BackfillCalendarFeedVerifierMAC": func() error {
			return repo.BackfillCalendarFeedVerifierMAC(ctx, 0, "selector", "mac")
		},
	}

	for name, call := range cases {
		if err := call(); !errors.Is(err, ErrUserOwnerRequired) {
			t.Fatalf("%s: expected ErrUserOwnerRequired for a zero owner id, got %v", name, err)
		}
	}
}

// TestUserRepositoryRemainingScopedWritersPersist is the success-path half of
// the coverage above: each of those methods also has an unexercised success
// line (the write past the guard's `if err != nil` branch), because no other
// test in this package calls it with a real owner id. One valid-id call per
// method is enough to run that line; the shared TOTP/webhook/onboarding
// behavior is already covered elsewhere.
func TestUserRepositoryRemainingScopedWritersPersist(t *testing.T) {
	repo := openTimezoneRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "owner-scope-success@example.com")
	ctx := context.Background()

	if err := repo.UpdateDisplayName(ctx, user.ID, "Renamed Owner"); err != nil {
		t.Fatalf("UpdateDisplayName: %v", err)
	}
	if _, err := repo.UpdateInterfaceLanguage(ctx, user.ID, "en"); err != nil {
		t.Fatalf("UpdateInterfaceLanguage: %v", err)
	}
	if err := repo.UpdateReminderLeadDays(ctx, user.ID, 2); err != nil {
		t.Fatalf("UpdateReminderLeadDays: %v", err)
	}
	if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:      true,
		EncryptedURL: "ciphertext",
	}); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}
	if err := repo.RemoveWebhookDestination(ctx, user.ID); err != nil {
		t.Fatalf("RemoveWebhookDestination: %v", err)
	}
	if err := repo.SaveCalendarFeedToken(ctx, user.ID, models.CalendarFeedTokenColumns{
		Selector:     "selector",
		VerifierHash: "hash",
		VerifierMAC:  "mac",
		KeyEpoch:     "epoch",
	}); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}
	if err := repo.ClearCalendarFeedToken(ctx, user.ID); err != nil {
		t.Fatalf("ClearCalendarFeedToken: %v", err)
	}
	if err := repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, user.ID, "new-recovery", nil); err != nil {
		t.Fatalf("UpdateRecoveryCodeHashAndRevokeSessions: %v", err)
	}
	if err := repo.UpdatePasswordAndRevokeSessions(ctx, user.ID, "new-hash", false); err != nil {
		t.Fatalf("UpdatePasswordAndRevokeSessions: %v", err)
	}
	if err := repo.ForceResetPasswordAndRevokeSessions(ctx, user.ID, "forced-hash"); err != nil {
		t.Fatalf("ForceResetPasswordAndRevokeSessions: %v", err)
	}
	if applied, err := repo.UpgradePasswordHashCAS(ctx, user.ID, "forced-hash", "hash-only"); err != nil || !applied {
		t.Fatalf("UpgradePasswordHashCAS: applied=%v err=%v", applied, err)
	}
	if err := repo.BumpAuthSessionVersion(ctx, user.ID); err != nil {
		t.Fatalf("BumpAuthSessionVersion: %v", err)
	}
	if err := repo.ClearAllDataAndResetSettings(ctx, user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	reloaded, err := repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("reload after the writer sequence: %v", err)
	}
	if reloaded.DisplayName != "Renamed Owner" {
		t.Fatalf("expected display name Renamed Owner to persist, got %q", reloaded.DisplayName)
	}
	if reloaded.PasswordHash != "hash-only" {
		t.Fatalf("expected password hash hash-only to persist, got %q", reloaded.PasswordHash)
	}
}
