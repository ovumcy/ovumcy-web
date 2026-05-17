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
// the contract for `POST /api/settings/clear-data`.
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
	}, http.MethodPost, "/api/settings/clear-data", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected clear data status 200, got %d", response.StatusCode)
	}

	var after models.User
	if err := scenario.database.First(&after, scenario.user.ID).Error; err != nil {
		t.Fatalf("load user after clear-data: %v", err)
	}

	if after.Email != before.Email {
		t.Fatalf("expected email preserved, before=%q after=%q", before.Email, after.Email)
	}
	if after.PasswordHash != before.PasswordHash {
		t.Fatal("expected password hash preserved, but it changed")
	}
	if after.RecoveryCodeHash != before.RecoveryCodeHash {
		t.Fatal("expected recovery code hash preserved, but it changed")
	}
	if after.Role != before.Role {
		t.Fatalf("expected role preserved, before=%q after=%q", before.Role, after.Role)
	}
	if after.DisplayName != displayName {
		t.Fatalf("expected display name preserved %q, got %q", displayName, after.DisplayName)
	}
	if !after.LocalAuthEnabled {
		t.Fatal("expected local_auth_enabled preserved, but it was cleared")
	}
	if !after.OnboardingCompleted {
		t.Fatal("expected onboarding_completed preserved, but it was cleared")
	}
	if after.TOTPSecret != totpSecret {
		t.Fatalf("expected totp_secret preserved, before=%q after=%q", totpSecret, after.TOTPSecret)
	}
	if !after.TOTPEnabled {
		t.Fatal("expected totp_enabled preserved, but it was disabled")
	}
	if after.TOTPLastUsedStep != totpLastUsedStep {
		t.Fatalf("expected totp_last_used_step preserved, before=%d after=%d", totpLastUsedStep, after.TOTPLastUsedStep)
	}

	var oidcRowCount int64
	if err := scenario.database.Model(&models.OIDCIdentity{}).Where("user_id = ?", scenario.user.ID).Count(&oidcRowCount).Error; err != nil {
		t.Fatalf("count oidc identities after clear-data: %v", err)
	}
	if oidcRowCount != 1 {
		t.Fatalf("expected oidc identity link preserved (count=1), got count=%d", oidcRowCount)
	}
}
