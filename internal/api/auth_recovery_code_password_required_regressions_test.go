package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestRegenerateRecoveryCodeRejectsMissingPassword(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-regenerate-missing-pass@example.com")

	priorHash := loadUserRecoveryCodeHash(t, ctx)

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusBadRequest)
	if got := readAPIError(t, response.Body); got != "invalid password" {
		t.Fatalf("expected error %q, got %q", "invalid password", got)
	}
	assertRecoveryCodeHashUnchanged(t, ctx, priorHash)
}

func TestRegenerateRecoveryCodeRejectsWrongPassword(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-regenerate-wrong-pass@example.com")

	priorHash := loadUserRecoveryCodeHash(t, ctx)

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{
		"password": {"WrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusUnauthorized)
	if got := readAPIError(t, response.Body); got != "invalid password" {
		t.Fatalf("expected error %q, got %q", "invalid password", got)
	}
	assertRecoveryCodeHashUnchanged(t, ctx, priorHash)
}

// TestRegenerateRecoveryCodeBumpsSessionVersionAndReissuesCookie is the success
// path of POST /api/v1/users/current/recovery-code.
//
// It closes a gap found while auditing the SECURITY.md matrix: the row claims the
// flow "bumps auth_session_version; originating session refreshed inline", but the
// only test cited was a service-level one that proves the bump alone. Every
// sibling flow — password change, TOTP enable/disable, clear-data — pins the
// reissue explicitly; this one did not.
//
// The gap had teeth: the handler derives the cookie's version by hand
// (NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1) instead of re-reading
// the row, so an off-by-one or a dropped normalize would hand the caller a cookie
// that authenticates nothing on its next request. Hence the probe below asserts
// the REISSUED cookie still works, not merely that some cookie came back.
func TestRegenerateRecoveryCodeBumpsSessionVersionAndReissuesCookie(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-regenerate-success@example.com")
	preRegenCookie := ctx.authCookie
	priorHash := loadUserRecoveryCodeHash(t, ctx)
	priorVersion := ctx.user.AuthSessionVersion

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()
	assertStatusCode(t, response, http.StatusOK)

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.AuthSessionVersion <= priorVersion {
		t.Fatalf("auth_session_version did not advance after regeneration: before=%d after=%d", priorVersion, reloaded.AuthSessionVersion)
	}
	if strings.TrimSpace(reloaded.RecoveryCodeHash) == priorHash {
		t.Fatal("expected the recovery-code hash to rotate")
	}

	refreshed := responseCookie(response.Cookies(), authCookieName)
	if refreshed == nil || strings.TrimSpace(refreshed.Value) == "" {
		t.Fatal("regeneration must reissue ovumcy_auth so the originating session stays alive")
	}

	// The reissued cookie must carry the version the row now holds: a cookie minted
	// from a stale or mis-incremented version would be refused here.
	probe := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	probe.Header.Set("Cookie", authCookieName+"="+refreshed.Value)
	probeResponse, err := ctx.app.Test(probe, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("reissued-cookie probe: %v", err)
	}
	defer func() { _ = probeResponse.Body.Close() }()
	if probeResponse.StatusCode != http.StatusOK {
		t.Fatalf("the reissued cookie must authenticate the next request, got %d", probeResponse.StatusCode)
	}

	// A cookie issued before the bump — another device — is signed out.
	otherProbe := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	otherProbe.Header.Set("Cookie", preRegenCookie)
	otherResponse, err := ctx.app.Test(otherProbe, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("pre-regeneration cookie probe: %v", err)
	}
	defer func() { _ = otherResponse.Body.Close() }()
	if otherResponse.StatusCode == http.StatusOK {
		t.Fatalf("pre-regeneration cookie still accepted on /dashboard (status=%d); the version bump must invalidate it", otherResponse.StatusCode)
	}
}

func loadUserRecoveryCodeHash(t *testing.T, ctx settingsSecurityTestContext) string {
	t.Helper()

	var current models.User
	if err := ctx.database.Select("recovery_code_hash").First(&current, ctx.user.ID).Error; err != nil {
		t.Fatalf("load recovery_code_hash: %v", err)
	}
	return strings.TrimSpace(current.RecoveryCodeHash)
}

func assertRecoveryCodeHashUnchanged(t *testing.T, ctx settingsSecurityTestContext, priorHash string) {
	t.Helper()

	current := loadUserRecoveryCodeHash(t, ctx)
	if current != priorHash {
		t.Fatalf("expected recovery_code_hash unchanged after rejected request")
	}
}
