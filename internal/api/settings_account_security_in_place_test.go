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
	"golang.org/x/net/html"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The recovery-code, password-change and two-factor forms answer where they
// are: a refusal renders beside the form it is about, a committed 2FA change
// rebuilds the panel from a read taken after the write, and no answer carries
// a secret that was not already sanctioned.

func htmxHeaders() map[string]string {
	return map[string]string{"HX-Request": "true", "Accept-Language": "en"}
}

func firstNodeWithID(t *testing.T, root *html.Node, id string) *html.Node {
	t.Helper()
	found := htmlFindElements(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "id") == id
	})
	if len(found) == 0 {
		t.Fatalf("no element with id %q in the response", id)
	}
	return found[0]
}

func TestRecoveryCodeRegenerationRefusalAnswersInPlaceForHTMX(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "recovery-in-place@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code",
		url.Values{"password": {"WrongPass9"}}, htmxHeaders())

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 with the rebuilt block", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "" {
		t.Fatalf("an htmx refusal must not redirect, got Location %q", location)
	}
	body := mustReadBodyString(t, response.Body)
	root := mustParseHTMLDocument(t, body)

	block := firstNodeWithID(t, root, "settings-recovery-code")
	if !htmlHasAttr(block, "data-status-island-host") {
		t.Error("the block replaces itself, so it must declare itself the status island host")
	}
	island := firstNodeWithID(t, root, "settings-recovery-code-status")
	if htmlAttr(island, "aria-live") != "polite" {
		t.Error("the refusal must land in a live region")
	}
	if !strings.Contains(body, "status-error") {
		t.Errorf("the refusal must render in the island, got %q", body)
	}
	field := firstNodeWithID(t, root, "settings-recovery-code-password")
	if htmlAttr(field, "aria-invalid") != "true" || !htmlHasAttr(field, "data-focus-after-swap") {
		t.Error("the password field must be marked invalid and take focus after the swap")
	}
	if htmlAttr(field, "value") != "" {
		t.Error("the password field must come back empty")
	}
	if strings.Contains(body, "WrongPass9") {
		t.Error("the refused password must never be echoed")
	}
}

func TestRecoveryCodeRegenerationPlainFormRefusalReturnsToTheForm(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "recovery-no-js@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code",
		url.Values{"password": {"WrongPass9"}}, nil)

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.StatusCode)
	}
	if got := response.Header.Get("Location"); got != "/settings#settings-recovery-code" {
		t.Fatalf("Location = %q, want the settings page at the recovery block", got)
	}
}

func TestRecoveryCodeRegenerationSuccessForHTMXFollowsTheRevealPage(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "recovery-reveal-htmx@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code",
		url.Values{"password": {"StrongPass1"}}, htmxHeaders())

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if got := response.Header.Get("HX-Redirect"); got != "/recovery-code" {
		t.Fatalf("HX-Redirect = %q, want the reveal page", got)
	}
	if responseCookieValue(response.Cookies(), recoveryCodeCookieName) == "" {
		t.Error("the sealed reveal cookie must ride the response")
	}
	if body := strings.TrimSpace(mustReadBodyString(t, response.Body)); body != "" && body != "OK" {
		t.Errorf("the new code must never travel in the body, got %q", body)
	}
}

func TestSettingsPageRecoveryAndPasswordFormsAnswerInPlace(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "forms-markup@example.com")
	root := mustParseHTMLDocument(t, renderSettingsPageForTest(t, ctx.app, ctx.authCookie))

	forms := htmlFindElements(root, formWithAttr("hx-post", "/api/v1/users/current/recovery-code"))
	if len(forms) != 1 {
		t.Fatalf("recovery forms = %d, want 1", len(forms))
	}
	form := forms[0]
	if htmlAttr(form, "hx-target") != "#settings-recovery-code" || htmlAttr(form, "hx-swap") != "outerHTML" {
		t.Error("the recovery form must replace its own block")
	}
	if htmlHasAttr(form, "data-confirm") || htmlAttr(form, "hx-confirm") == "" {
		t.Error("an htmx form confirms through hx-confirm, not data-confirm")
	}

	// The password island opts out of the toast: the in-card status is the one
	// channel, so a success is not announced twice.
	island := firstNodeWithID(t, root, "settings-change-password-status")
	if htmlHasAttr(island, "data-success-toast") {
		t.Error("the password island must not also toast")
	}
}

func TestPasswordChangeSuccessForHTMXDeclaresItClearsTheFieldsAndEchoesNothing(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "password-clears@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", url.Values{
		"current_password": {"StrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}, htmxHeaders())

	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, `data-status-kind="clears-fields"`) {
		t.Errorf("the success status must tell the client to empty the form, got %q", body)
	}
	for _, typed := range []string{"StrongPass1", "EvenStronger2"} {
		if strings.Contains(body, typed) {
			t.Errorf("the response echoed a typed password %q", typed)
		}
	}
	if strings.Contains(body, "data-success-toast") {
		t.Error("the answer must not opt into a second feedback channel")
	}
}

func enableTOTPForTest(t *testing.T, ctx *settingsSecurityTestContext) {
	t.Helper()
	if err := getTOTPServiceForTest(ctx.database).EnableTOTP(context.Background(), ctx.user.ID, ctx.user.AuthSessionVersion, "JBSWY3DPEHPK3PXP", verifiedEnrollmentStepForTest(t, "JBSWY3DPEHPK3PXP")); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}
	ctx.refreshAuthCookie(t)
}

func assertNoEnrollmentSeed(t *testing.T, response *http.Response, body string) {
	t.Helper()
	for _, forbidden := range []string{"otpauth://", "data:image/png", "data-totp-manual-secret", "settings-2fa-code"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the response must carry no enrollment seed, found %q", forbidden)
		}
	}
	if cookie := responseCookie(response.Cookies(), totpSetupCookieName); cookie != nil && strings.TrimSpace(cookie.Value) != "" {
		t.Error("the response must mint no setup cookie")
	}
}

func TestTOTPEnableForHTMXAnswersWithTheRebuiltPanel(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-panel-enable@example.com")
	key, err := services.NewTOTPService(&dbUserRepoForTest{ctx.database}, []byte("test-secret-key"), nil).GenerateSetupKey("Ovumcy", ctx.user.Email)
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
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("PUT 2fa: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	body := mustReadBodyString(t, response.Body)
	if response.Header.Get("HX-Retarget") != "#settings-2fa-panel" || response.Header.Get("HX-Reswap") != "outerHTML" {
		t.Errorf("a committed enable must retarget the panel, got %q %q", response.Header.Get("HX-Retarget"), response.Header.Get("HX-Reswap"))
	}
	root := mustParseHTMLDocument(t, body)
	panel := firstNodeWithID(t, root, "settings-2fa-panel")
	if !htmlHasAttr(panel, "data-status-island-host") {
		t.Error("the panel replaces itself, so it must host the status island")
	}
	if len(htmlFindElements(root, formWithFlag("data-settings-2fa-enabled"))) == 0 && !strings.Contains(body, "data-settings-2fa-enabled") {
		t.Error("the rebuilt panel must show the enabled state")
	}
	if !strings.Contains(body, "Two-factor authentication enabled.") {
		t.Error("the success message must ride the rebuilt panel's live region")
	}
	if !strings.Contains(body, `href="/settings#settings-recovery-code"`) {
		t.Error("the enabled panel must point at the recovery code")
	}
	if !strings.Contains(body, "data-focus-after-swap") {
		t.Error("focus must move to the new state")
	}
	if strings.Contains(body, key.Secret()) || strings.Contains(body, "settings-2fa-enroll-password") {
		t.Error("the enabled panel must carry no enrollment form or seed")
	}
}

func TestTOTPDisableForHTMXAnswersWithTheDisabledPanelAndNoSeed(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-panel-disable@example.com")
	enableTOTPForTest(t, &ctx)

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa",
		url.Values{"password": {"StrongPass1"}}, htmxHeaders())

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	body := mustReadBodyString(t, response.Body)
	if response.Header.Get("HX-Retarget") != "#settings-2fa-panel" {
		t.Error("a committed disable must retarget the panel")
	}
	if !strings.Contains(body, "data-settings-2fa-disabled") || strings.Contains(body, "data-settings-2fa-enabled") {
		t.Errorf("the panel must show the state after the write, got %q", body)
	}
	if !strings.Contains(body, "Two-factor authentication disabled.") {
		t.Error("the success message must ride the rebuilt panel")
	}
	if !strings.Contains(body, `href="/settings/2fa"`) {
		t.Error("the setup entry point must be offered")
	}
	assertNoEnrollmentSeed(t, response, body)
}

func TestTOTPDisableFormAsksForConfirmation(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-confirm@example.com")
	enableTOTPForTest(t, &ctx)

	request := httptest.NewRequest(http.MethodGet, "/settings/2fa", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", ctx.authCookie)
	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /settings/2fa: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	root := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	forms := htmlFindElements(root, formWithAttr("hx-delete", "/api/v1/users/current/2fa"))
	if len(forms) != 1 {
		t.Fatalf("disable forms = %d, want 1", len(forms))
	}
	if htmlAttr(forms[0], "hx-confirm") == "" || htmlAttr(forms[0], "data-confirm-accept") == "" {
		t.Error("the disable form must confirm through the app's modal")
	}
	if htmlHasAttr(forms[0], "data-confirm") {
		t.Error("an htmx form must not also carry data-confirm")
	}
}

func TestTOTPDisableWithAWrongPasswordIsTheSettingsPasswordRefusal(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-wrong-key@example.com")
	enableTOTPForTest(t, &ctx)

	response := sendDisableTOTP(t, ctx, "WrongPassword1")

	assertDisableTOTPRefused(t, response, http.StatusUnauthorized, "invalid password", "wrong disable password")
}

func TestTOTPEnableOnAnEnabledAccountWithoutASetupCookieSaysSo(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enable-twice@example.com")
	enableTOTPForTest(t, &ctx)

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/2fa",
		url.Values{"code": {"123456"}, "password": {"StrongPass1"}},
		map[string]string{"Accept-Language": "en", "Accept": "application/json"})

	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "totp already enabled" {
		t.Fatalf("error key = %q, want the already-enabled refusal, not an expired session", got)
	}
	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !reloaded.TOTPEnabled {
		t.Error("the refusal must leave the factor on")
	}
}

// The already-enabled answer names the account's state, so it sits behind the
// password: a wrong password on the same stale submit is refused as a wrong
// password and says nothing about the factor.
func TestTOTPEnableOnAnEnabledAccountWithAWrongPasswordIsNotToldItIsEnabled(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enable-twice-wrong-pass@example.com")
	enableTOTPForTest(t, &ctx)

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/2fa",
		url.Values{"code": {"123456"}, "password": {"NotMyPass99"}},
		map[string]string{"Accept-Language": "en", "Accept": "application/json"})

	if response.StatusCode == http.StatusConflict {
		t.Fatal("a wrong password must not reach the already-enabled answer")
	}
	if got := readAPIError(t, response.Body); got == "totp already enabled" {
		t.Fatalf("error key = %q: the factor state leaked past a wrong password", got)
	}
}

func hasAncestorWithID(node *html.Node, id string) bool {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && htmlAttr(parent, "id") == id {
			return true
		}
	}
	return false
}

func TestRecoveryCodePlainFormRefusalRendersInTheBlockIslandNotTheBanner(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "recovery-no-js-island@example.com")

	refusal := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code",
		url.Values{"password": {"WrongPass9"}}, nil)
	flash := responseCookieValue(refusal.Cookies(), flashCookieName)
	if flash == "" {
		t.Fatal("the refusal must ride the sealed flash cookie")
	}
	if strings.Contains(refusal.Header.Get("Location"), "Invalid") || strings.Contains(refusal.Header.Get("Location"), "?") {
		t.Fatalf("Location %q must carry no message", refusal.Header.Get("Location"))
	}

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Cookie", joinCookieHeader(settingsCookieHeader(ctx.authCookie, ctx.csrfCookie), flashCookieName+"="+flash))
	request.Header.Set("Accept-Language", "en")
	page, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /settings failed: %v", err)
	}
	t.Cleanup(func() { _ = page.Body.Close() })
	root := mustParseHTMLDocument(t, mustReadBodyString(t, page.Body))

	errors := htmlFindElements(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-flash-status") == "error"
	})
	if len(errors) != 1 {
		t.Fatalf("rendered error messages = %d, want exactly the recovery island's", len(errors))
	}
	refusalNode := errors[0]
	if !hasAncestorWithID(refusalNode, "settings-recovery-code-status") || !hasAncestorWithID(refusalNode, "settings-recovery-code") {
		t.Error("the refusal must render inside the recovery block's island, not the page-top banner")
	}
	if htmlAttr(refusalNode, "role") != "alert" {
		t.Error("the island's refusal must be announced with role=alert")
	}
	if got := htmlNodeText(refusalNode); !strings.Contains(got, englishCopy(t, "settings.error.invalid_password")) {
		t.Errorf("island text = %q, want the localized wrong-password copy", got)
	}
	field := firstNodeWithID(t, root, "settings-recovery-code-password")
	if htmlAttr(field, "aria-invalid") != "true" {
		t.Error("the password field must be marked invalid")
	}
}

func TestRecoveryErrorMessageKeyFallsBackToTheGenericLocalizedError(t *testing.T) {
	if got := recoveryErrorMessageKey("a spec key with no mapping"); got != "common.error.internal_error" {
		t.Fatalf("unmapped key resolved to %q, want the generic localized error key", got)
	}
	if got := recoveryErrorMessageKey("invalid password"); got != "settings.error.invalid_password" {
		t.Fatalf("mapped key resolved to %q", got)
	}
}

func TestFlashSettingsErrorTargetKeepsOnlyTheKnownValue(t *testing.T) {
	if got := normalizeFlashPayload(FlashPayload{SettingsErrorTarget: " " + flashTargetRecoveryCode + " "}).SettingsErrorTarget; got != flashTargetRecoveryCode {
		t.Errorf("known target = %q", got)
	}
	if got := normalizeFlashPayload(FlashPayload{SettingsErrorTarget: "<script>"}).SettingsErrorTarget; got != "" {
		t.Errorf("unknown target survived as %q", got)
	}
}
