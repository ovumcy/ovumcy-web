package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRegisterJSONSuccessDoesNotExposeRecoveryCode(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(url.Values{
		"email":            {"json-register@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusCreated)

	payload := readRecoveryCodeFlowJSON(t, response)
	assertRecoveryCodeIssuedViaSurface(t, payload, "/register")
	assertRecoveryCodeTransportCookies(t, response)
}

func TestResetPasswordJSONSuccessDoesNotExposeRecoveryCode(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "json-reset@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	startResetRequest := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(url.Values{
		"email":         {user.Email},
		"recovery_code": {recoveryCode},
	}.Encode()))
	startResetRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	startResetResponse := mustAppResponse(t, app, startResetRequest)
	assertStatusCode(t, startResetResponse, http.StatusSeeOther)

	resetCookie := responseCookieValue(startResetResponse.Cookies(), resetPasswordCookieName)
	if resetCookie == "" {
		t.Fatal("expected reset-password cookie after forgot-password flow")
	}

	completeResetRequest := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(url.Values{
		"password":         {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}.Encode()))
	completeResetRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	completeResetRequest.Header.Set("Accept", "application/json")
	completeResetRequest.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookie)

	completeResetResponse := mustAppResponse(t, app, completeResetRequest)
	assertStatusCode(t, completeResetResponse, http.StatusOK)

	payload := readRecoveryCodeFlowJSON(t, completeResetResponse)
	assertRecoveryCodeIssuedViaSurface(t, payload, "/recovery-code")
	assertRecoveryCodeTransportCookies(t, completeResetResponse)
}

func TestRegenerateRecoveryCodeRedirectsToDedicatedRecoveryPage(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-regenerate@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/regenerate-recovery-code", url.Values{}, nil)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/recovery-code" {
		t.Fatalf("expected redirect to /recovery-code, got %q", location)
	}

	recoveryCookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName)
	if recoveryCookie == "" {
		t.Fatal("expected recovery-code page cookie after regeneration")
	}

	recoveryPageRequest := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	recoveryPageRequest.Header.Set("Accept-Language", "en")
	recoveryPageRequest.Header.Set("Cookie", ctx.authCookie+"; "+recoveryCodeCookieName+"="+recoveryCookie)

	recoveryPageResponse := mustAppResponse(t, ctx.app, recoveryPageRequest)
	assertStatusCode(t, recoveryPageResponse, http.StatusOK)
	recoveryPage := mustReadBodyString(t, recoveryPageResponse.Body)

	assertBodyContainsAll(t, recoveryPage,
		bodyStringMatch{fragment: `id="recovery-code"`, message: "expected dedicated recovery code page after regeneration"},
		bodyStringMatch{fragment: `form action="/settings"`, message: "expected regeneration flow to return to settings after confirmation"},
	)
	assertBodyNotContainsAll(t, recoveryPage,
		bodyStringMatch{fragment: "settings.success.recovery_code_regenerated", message: "did not expect raw settings success key on recovery page"},
	)
}

func TestRegenerateRecoveryCodeJSONDoesNotExposeRecoveryCode(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-regenerate-json@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/regenerate-recovery-code", url.Values{}, map[string]string{
		"Accept": "application/json",
	})
	assertStatusCode(t, response, http.StatusOK)

	payload := readRecoveryCodeFlowJSON(t, response)
	assertRecoveryCodeIssuedViaSurface(t, payload, "/recovery-code")

	recoveryCookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName)
	if recoveryCookie == "" {
		t.Fatal("expected recovery-code page cookie after json regeneration")
	}
}

func TestRegisterInlineRecoveryStepConsumesCookieAfterFirstView(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(url.Values{
		"email":            {"one-time-recovery@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "en")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected register success redirect /register, got %q", location)
	}

	authCookie := responseCookieValue(response.Cookies(), authCookieName)
	recoveryCookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName)
	if authCookie == "" || recoveryCookie == "" {
		t.Fatal("expected auth and recovery cookies after registration")
	}

	firstViewRequest := httptest.NewRequest(http.MethodGet, "/register", nil)
	firstViewRequest.Header.Set("Accept-Language", "en")
	firstViewRequest.Header.Set("Cookie", authCookieName+"="+authCookie+"; "+recoveryCodeCookieName+"="+recoveryCookie)

	firstViewResponse := mustAppResponse(t, app, firstViewRequest)
	assertStatusCode(t, firstViewResponse, http.StatusOK)
	body := mustReadBodyString(t, firstViewResponse.Body)
	assertBodyContainsAll(t, body,
		bodyStringMatch{fragment: `data-auth-inline-recovery`, message: "expected inline recovery block on first view"},
		bodyStringMatch{fragment: `id="recovery-code"`, message: "expected recovery code on first view"},
	)

	clearedCookie := responseCookie(firstViewResponse.Cookies(), recoveryCodeCookieName)
	if clearedCookie == nil {
		t.Fatal("expected recovery-code cookie to be cleared after first view")
	}
	if clearedCookie.Value != "" || !clearedCookie.Expires.Before(time.Now()) {
		t.Fatalf("expected recovery-code cookie to be cleared, got %#v", clearedCookie)
	}

	secondViewRequest := httptest.NewRequest(http.MethodGet, "/register", nil)
	secondViewRequest.Header.Set("Accept-Language", "en")
	secondViewRequest.Header.Set("Cookie", authCookieName+"="+authCookie)

	secondViewResponse := mustAppResponse(t, app, secondViewRequest)
	assertStatusCode(t, secondViewResponse, http.StatusSeeOther)
	if location := secondViewResponse.Header.Get("Location"); location != "/onboarding" {
		t.Fatalf("expected second recovery-code request to redirect to /onboarding, got %q", location)
	}
}

func TestRecoveryCodePageRedirectsInlineRegistrationSurfaceBackToRegister(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(url.Values{
		"email":            {"inline-surface@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "en")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	authCookie := responseCookieValue(response.Cookies(), authCookieName)
	recoveryCookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName)
	if authCookie == "" || recoveryCookie == "" {
		t.Fatal("expected auth and recovery cookies after registration")
	}

	recoveryPageRequest := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	recoveryPageRequest.Header.Set("Accept-Language", "en")
	recoveryPageRequest.Header.Set("Cookie", authCookieName+"="+authCookie+"; "+recoveryCodeCookieName+"="+recoveryCookie)

	recoveryPageResponse := mustAppResponse(t, app, recoveryPageRequest)
	assertStatusCode(t, recoveryPageResponse, http.StatusSeeOther)
	if location := recoveryPageResponse.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected inline registration recovery cookie to redirect back to /register, got %q", location)
	}
}

func readRecoveryCodeFlowJSON(t *testing.T, response *http.Response) map[string]any {
	t.Helper()

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode recovery-code json response: %v", err)
	}
	return payload
}

func assertRecoveryCodeIssuedViaSurface(t *testing.T, payload map[string]any, expectedPath string) {
	t.Helper()

	if got, ok := payload["ok"].(bool); !ok || !got {
		t.Fatalf("expected ok=true payload, got %#v", payload)
	}
	if got := strings.TrimSpace(stringValue(payload["next_step"])); got != "recovery_code" {
		t.Fatalf("expected next_step recovery_code, got %#v", payload["next_step"])
	}
	if got := strings.TrimSpace(stringValue(payload["next_path"])); got != expectedPath {
		t.Fatalf("expected next_path %s, got %#v", expectedPath, payload["next_path"])
	}
	if _, exposed := payload["recovery_code"]; exposed {
		t.Fatalf("did not expect recovery_code in json payload: %#v", payload)
	}
}

func assertRecoveryCodeTransportCookies(t *testing.T, response *http.Response) {
	t.Helper()

	authCookie := responseCookieValue(response.Cookies(), authCookieName)
	if authCookie == "" {
		t.Fatal("expected auth cookie in recovery-code issuance response")
	}
	recoveryCookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName)
	if recoveryCookie == "" {
		t.Fatal("expected recovery-code page cookie in issuance response")
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
