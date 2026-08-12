package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// Interface language as an account property (migration 034).
//
// The cookie is still what every render resolves from — this aggregator covers
// the other half: the account column is re-issued as the `ovumcy_lang` cookie
// at session issue, so a device that has no cookie is served the language its
// owner chose rather than whatever Accept-Language negotiates. Every path that
// issues a session goes through setAuthCookie, so the cases below drive three
// different entry points into that one helper (password login, TOTP challenge
// completion, OIDC callback) plus the two degradations that must stay silent.
//
// Every request here sends `Accept-Language: en`, which is what makes the
// assertion meaningful: without the stored preference the same request resolves
// to English.

func seedStoredInterfaceLanguage(t *testing.T, database *gorm.DB, userID uint, language string) {
	t.Helper()
	if err := database.Model(&models.User{}).Where("id = ?", userID).
		Update("interface_language", language).Error; err != nil {
		t.Fatalf("seed interface_language=%q: %v", language, err)
	}
}

func passwordLoginResponse(t *testing.T, app *fiber.App, email string) *http.Response {
	t.Helper()
	form := url.Values{"email": {email}, "password": {"StrongPass1"}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("password login request failed: %v", err)
	}
	return response
}

func TestPasswordLoginIssuesTheLanguageCookieFromTheStoredPreference(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "login-stored-language@example.com", "StrongPass1", true)
	seedStoredInterfaceLanguage(t, database, user.ID, "ru")

	response := passwordLoginResponse(t, app, user.Email)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if got := responseCookieValue(response.Cookies(), languageCookieName); got != "ru" {
		t.Fatalf("expected the login response to re-issue ovumcy_lang=ru from the account, got %q", got)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie == nil {
		t.Fatal("expected the login to issue an auth cookie alongside the language cookie")
	}
}

// TestPasswordLoginWithoutAStoredLanguageLeavesTheLanguageCookieAlone is the
// negative half: the empty column means "never chosen", so a sign-in must not
// start pinning accounts to the default language. Without this case the feature
// would pass its positive test while quietly overriding the visitor's own
// pre-auth choice on every sign-in.
func TestPasswordLoginWithoutAStoredLanguageLeavesTheLanguageCookieAlone(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "login-no-stored-language@example.com", "StrongPass1", true)

	response := passwordLoginResponse(t, app, user.Email)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if cookie := responseCookie(response.Cookies(), languageCookieName); cookie != nil {
		t.Fatalf("expected no ovumcy_lang cookie for an account that never chose one, got %#v", cookie)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie == nil {
		t.Fatal("expected the login to issue an auth cookie")
	}
}

// TestPasswordLoginIgnoresAnUnsupportedStoredLanguage covers a stored code this
// build does not ship — a locale dropped between releases, or a value written
// straight into the database. It degrades to the request's own resolution
// (English here) instead of answering 500 or pinning the account to a locale
// with no catalogue behind it.
func TestPasswordLoginIgnoresAnUnsupportedStoredLanguage(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "login-unknown-language@example.com", "StrongPass1", true)
	seedStoredInterfaceLanguage(t, database, user.ID, "zz")

	response := passwordLoginResponse(t, app, user.Email)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if cookie := responseCookie(response.Cookies(), languageCookieName); cookie != nil {
		t.Fatalf("expected an unsupported stored language to be ignored, got ovumcy_lang=%#v", cookie)
	}

	authCookie := responseCookie(response.Cookies(), authCookieName)
	if authCookie == nil {
		t.Fatal("expected the login to issue an auth cookie")
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", cookiePair(authCookie))
	dashboardResponse := mustAppResponse(t, app, dashboardRequest)
	assertStatusCode(t, dashboardResponse, http.StatusOK)

	document := mustParseHTMLDocument(t, mustReadBodyString(t, dashboardResponse.Body))
	if htmlElementByAttr(document, "lang", "en") == nil {
		t.Fatal("expected the dashboard to render in the default language after an unsupported stored code")
	}
}

// TestTOTPChallengeCompletionIssuesTheLanguageCookieFromTheStoredPreference
// covers the second-factor path. It is a separate session-issue entry point:
// the password step answers `requires_totp` and issues no session at all, so a
// 2FA-enabled account would never see its stored language if the re-issue hung
// off the password handler instead of the shared cookie helper.
func TestTOTPChallengeCompletionIssuesTheLanguageCookieFromTheStoredPreference(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "totp-stored-language@example.com", "StrongPass1", true)
	seedStoredInterfaceLanguage(t, database, user.ID, "ru")

	secretKey := []byte(testAppSecretKey)
	rawSecret := setupTOTPForUser(t, database, user.ID, secretKey)
	pendingCookie := sealTOTPPendingCookieForTest(t, secretKey, user.ID, false)

	code, err := totp.GenerateCode(rawSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)
	response := doTOTPChallengeRequest(t, app, joinCookieHeader(pendingCookie, csrfCookieHeader), code, csrfToken)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if got := responseCookieValue(response.Cookies(), languageCookieName); got != "ru" {
		t.Fatalf("expected the 2FA challenge to re-issue ovumcy_lang=ru from the account, got %q", got)
	}
}

// TestOIDCCallbackIssuesTheLanguageCookieFromTheStoredPreference covers the
// federated sign-in path against the stubbed provider, which is as far as the
// Go harness reaches: the browser lane for OIDC is opt-in and skipped in CI.
// The stub returns the account the real service would have loaded, so what this
// pins is the transport wiring — the callback issues its session through the
// same helper as every other path.
func TestOIDCCallbackIssuesTheLanguageCookieFromTheStoredPreference(t *testing.T) {
	stub := newStubOIDCWorkflowService(true)
	stub.authURL = "https://id.example.com/authorize"
	stub.result = services.OIDCLoginResult{
		User: models.User{
			ID:                  21,
			Role:                models.RoleOwner,
			AuthSessionVersion:  1,
			OnboardingCompleted: true,
			InterfaceLanguage:   "ru",
		},
	}
	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	assertStatusCode(t, startResponse, http.StatusTemporaryRedirect)
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Accept-Language", "en")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if got := responseCookieValue(callbackResponse.Cookies(), languageCookieName); got != "ru" {
		t.Fatalf("expected the OIDC callback to re-issue ovumcy_lang=ru from the account, got %q", got)
	}
}
