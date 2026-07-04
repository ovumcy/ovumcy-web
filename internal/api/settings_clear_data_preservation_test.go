package api

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestClearDataPreservesAccountIdentityFields enforces the SECURITY.md claim:
//
//	clear-data does NOT touch email, password hash, recovery code hash,
//	role, display name, OIDC identity links, TOTP state, or onboarding status.
//
// Sister test to TestClearDataRemovesTrackedCalendarEntriesAndResetsCycleSettings,
// which covers the inverse claim (what clear-data DOES wipe). Together they form
// the contract for `POST /api/v1/users/current/data-wipe`.
func TestClearDataPreservesAccountIdentityFields(t *testing.T) {
	scenario := setupClearDataScenario(t)

	// Layer in the identity-shaped state that the SECURITY.md preservation
	// claim specifically guards. setupClearDataScenario already sets cycle
	// data and symptoms; here we add the bits we must NOT wipe.
	displayName := "Owner Persona"
	totpSecret := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	totpLastUsedStep := int64(1747526400)
	if err := scenario.database.Model(&models.User{}).Where("id = ?", scenario.user.ID).Updates(map[string]any{
		"display_name":         displayName,
		"totp_secret":          totpSecret,
		"totp_enabled":         true,
		"totp_last_used_step":  totpLastUsedStep,
		"local_auth_enabled":   true,
		"onboarding_completed": true,
	}).Error; err != nil {
		t.Fatalf("seed identity-related user fields: %v", err)
	}

	oidcIdentity := models.OIDCIdentity{
		UserID:    scenario.user.ID,
		Issuer:    "https://idp.example.com",
		Subject:   "sub-12345",
		CreatedAt: time.Now().UTC(),
	}
	if err := scenario.database.Create(&oidcIdentity).Error; err != nil {
		t.Fatalf("seed oidc identity link: %v", err)
	}

	var before models.User
	if err := scenario.database.First(&before, scenario.user.ID).Error; err != nil {
		t.Fatalf("load user baseline: %v", err)
	}

	response := settingsFormRequestWithCSRF(t, settingsSecurityTestContext{
		app:        scenario.app,
		authCookie: scenario.authCookie,
		csrfCookie: scenario.csrfCookie,
		csrfToken:  scenario.csrfToken,
	}, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected clear data status 200, got %d", response.StatusCode)
	}

	var after models.User
	if err := scenario.database.First(&after, scenario.user.ID).Error; err != nil {
		t.Fatalf("load user after clear-data: %v", err)
	}

	assertUserIdentityPreserved(t, before, after, displayName, totpSecret, totpLastUsedStep)

	var oidcRowCount int64
	if err := scenario.database.Model(&models.OIDCIdentity{}).Where("user_id = ?", scenario.user.ID).Count(&oidcRowCount).Error; err != nil {
		t.Fatalf("count oidc identities after clear-data: %v", err)
	}
	if oidcRowCount != 1 {
		t.Fatalf("expected oidc identity link preserved (count=1), got count=%d", oidcRowCount)
	}
}

// assertUserIdentityPreserved walks the identity-shaped User columns that the
// clear-data contract must NOT wipe. Pulled out of the parent test so the
// per-field branches do not balloon its cyclomatic complexity.
func assertUserIdentityPreserved(t *testing.T, before, after models.User, displayName, totpSecret string, totpLastUsedStep int64) {
	t.Helper()

	stringChecks := []struct {
		name          string
		before, after string
	}{
		{"email", before.Email, after.Email},
		{"password_hash", before.PasswordHash, after.PasswordHash},
		{"recovery_code_hash", before.RecoveryCodeHash, after.RecoveryCodeHash},
		{"role", string(before.Role), string(after.Role)},
		{"display_name", displayName, after.DisplayName},
		{"totp_secret", totpSecret, after.TOTPSecret},
	}
	for _, check := range stringChecks {
		if check.before != check.after {
			t.Fatalf("expected %s preserved, before=%q after=%q", check.name, check.before, check.after)
		}
	}

	boolChecks := []struct {
		name string
		got  bool
	}{
		{"local_auth_enabled", after.LocalAuthEnabled},
		{"onboarding_completed", after.OnboardingCompleted},
		{"totp_enabled", after.TOTPEnabled},
	}
	for _, check := range boolChecks {
		if !check.got {
			t.Fatalf("expected %s preserved, but it was cleared", check.name)
		}
	}

	if after.TOTPLastUsedStep != totpLastUsedStep {
		t.Fatalf("expected totp_last_used_step preserved, before=%d after=%d", totpLastUsedStep, after.TOTPLastUsedStep)
	}
}
