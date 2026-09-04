package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A settings verdict is only delivered if the page it redirects to READS the
// channel it was written to. Only /settings pops the flash cookie
// (buildSettingsPageData -> popFlashCookie) and only it renders the resulting
// key through the single status island in settings.html. The three tests below
// pin the sites where a success verdict used to be written to a destination
// that reads nothing:
//
//   - the 2FA enable and disable arms redirected to /settings/2fa, whose
//     renderer (ShowTOTPSetupPage) builds its data inline and never pops the
//     flash — and flashed a raw translation key, which
//     SettingsStatusTranslationKey answers "" for, so even a page that did read
//     it would render an empty banner;
//   - the dashboard usage-goal quick switch redirects to /dashboard, which has
//     no flash reader either; there the fix is the other direction — write no
//     flash, because the destination carries the verdict in its own state.
//
// Each test asserts the RENDERED page, never merely that a cookie was set.

// assertFlashedSettingsSuccessRenders follows the redirect a settings mutation
// answered with, carrying the cookies it issued, and requires the settings page
// to render the success island for translationKey with copyText inside it. The
// copy assertion is what separates "the flash was resolved" from "the flash was
// dropped": an unmapped status yields no island at all, and a mapped one whose
// catalogue entry is missing renders the key as its own text.
func assertFlashedSettingsSuccessRenders(
	t *testing.T,
	ctx settingsSecurityTestContext,
	response *http.Response,
	translationKey string,
	copyText string,
) {
	t.Helper()

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatal("expected the mutation to flash its confirmation")
	}

	// The 2FA arms re-issue this device's auth cookie (the session version was
	// bumped), so the follow-up must carry the fresh one or it is signed out.
	authCookieHeader := ctx.authCookie
	if refreshed := responseCookie(response.Cookies(), authCookieName); refreshed != nil && refreshed.Value != "" {
		authCookieHeader = cookiePair(refreshed)
	}

	followRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", joinCookieHeader(authCookieHeader, flashCookieName+"="+flashValue))

	followResponse, err := ctx.app.Test(followRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("follow the redirect to /settings: %v", err)
	}
	defer func() { _ = followResponse.Body.Close() }()

	if followResponse.StatusCode != http.StatusOK {
		t.Fatalf("follow-up GET /settings = %d, want 200", followResponse.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, followResponse.Body))
	island := htmlFlashByKey(document, translationKey)
	if island == nil {
		t.Fatalf("the redirect target rendered no success island for %q", translationKey)
	}
	if text := normalizeHTMLText(htmlNodeText(island)); !strings.Contains(text, copyText) {
		t.Fatalf("success island text = %q, want it to carry %q", text, copyText)
	}
}

// TestTOTPEnrollmentConfirmationRendersOnTheRedirectTarget drives the non-HTMX
// success arm of VerifyTOTP2FAEnrollment and requires the page it redirects to
// to show the enrollment confirmation. Both halves of the fix are asserted:
// the redirect goes to the page that reads the flash, and the flashed status is
// a slug settingsStatusTranslationKeys maps onto the copy the HTMX arm renders
// inline.
func TestTOTPEnrollmentConfirmationRendersOnTheRedirectTarget(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-confirmation@example.com")

	key, err := getTOTPServiceForTest(ctx.database).GenerateSetupKey("Ovumcy", ctx.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), ctx.user.ID, key.Secret())
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	form := url.Values{"code": {code}, "password": {"StrongPass1"}, "csrf_token": {ctx.csrfToken}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/2fa", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))

	response := mustAppResponse(t, ctx.app, request)

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("non-HTMX enrollment status = %d, want 303", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("enrollment redirected to %q, want %q — the flash is only read there", location, "/settings")
	}

	assertFlashedSettingsSuccessRenders(t, ctx, response, "settings.2fa.enabled_status", "Two-factor authentication enabled.")
}

// TestTOTPDisableConfirmationRendersOnTheRedirectTarget is the disable-side twin
// of the test above. It is a separate site in the same class: the two arms carry
// their own destination and their own status, so fixing one leaves the other
// silent.
func TestTOTPDisableConfirmationRendersOnTheRedirectTarget(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-confirmation@example.com")
	if err := getTOTPServiceForTest(ctx.database).EnableTOTP(context.Background(), ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP setup: %v", err)
	}
	ctx.refreshAuthCookie(t)

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa",
		url.Values{"password": {"StrongPass1"}},
		map[string]string{"Accept-Language": "en"},
	)

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("non-HTMX disable status = %d, want 303", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("disable redirected to %q, want %q — the flash is only read there", location, "/settings")
	}

	assertFlashedSettingsSuccessRenders(t, ctx, response, "settings.2fa.disabled_status", "Two-factor authentication disabled.")
}

// TestUsageGoalQuickSwitchLeavesNoFlashForTheDashboard pins the third site of
// the class, fixed the other way round. The goal-only save returns the owner to
// /dashboard, which has no flash reader; a "cycle_updated" written here was
// invisible on arrival and then surfaced on the next settings page the owner
// opened, as a save they had not just made. The save itself is asserted first,
// so "no flash" cannot be satisfied by a request that was simply refused.
func TestUsageGoalQuickSwitchLeavesNoFlashForTheDashboard(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "usage-goal-quick-switch@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/cycle",
		url.Values{"usage_goal": {models.UsageGoalTrying}},
		map[string]string{"Accept-Language": "en"},
	)

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("goal-only save status = %d, want 303", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("goal-only save redirected to %q, want %q", location, "/dashboard")
	}
	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.UsageGoal != models.UsageGoalTrying {
		t.Fatalf("usage goal = %q, want %q — the premise of this test is a save that succeeded", reloaded.UsageGoal, models.UsageGoalTrying)
	}

	if flashValue := responseCookieValue(response.Cookies(), flashCookieName); flashValue != "" {
		t.Fatal("the dashboard reads no flash: a confirmation written here rides along to the next settings page")
	}
}
