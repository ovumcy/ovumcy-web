package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestClearAllData_BumpsSessionVersion captures the contract that a
// successful POST /api/v1/users/current/data-wipe invalidates every auth cookie
// issued before the wipe. The originating device is refreshed inline so
// the user that triggered the clear-data flow stays signed in, while a
// session that existed on a different device (whose cookie was issued
// before the version bump) is rejected with "revoked session" on its
// next request.
func TestClearAllData_BumpsSessionVersion(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "clear-data-bumps-sv@example.com")
	otherSessionCookie := ctx.authCookie
	preWipeVersion := ctx.user.AuthSessionVersion

	form := url.Values{"password": {"StrongPass1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", form, map[string]string{"Accept": "application/json"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("clear-data status = %d, want 200 or 303", resp.StatusCode)
	}

	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.AuthSessionVersion <= preWipeVersion {
		t.Fatalf("auth_session_version did not advance after clear-data: before=%d after=%d", preWipeVersion, reloaded.AuthSessionVersion)
	}

	refreshed := responseCookie(resp.Cookies(), authCookieName)
	if refreshed == nil || strings.TrimSpace(refreshed.Value) == "" {
		t.Fatal("clear-data handler must reissue ovumcy_auth so the originating session stays alive")
	}

	otherProbe := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	otherProbe.Header.Set("Cookie", otherSessionCookie)
	otherResp, err := ctx.app.Test(otherProbe, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("other-session probe: %v", err)
	}
	defer func() { _ = otherResp.Body.Close() }()
	if otherResp.StatusCode == http.StatusOK {
		t.Fatalf("pre-wipe cookie still accepted on /dashboard (status=%d); clear-data must invalidate other sessions", otherResp.StatusCode)
	}
}
