package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
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
//
// The cases at the end of the file cover the two halves the cookie-only switch
// left open: `POST /lang` writing the same account column when a session is
// present (so a re-issue stops reverting a mid-session switch), and the cookie's
// own lifecycle — retracted when a session ends on purpose, kept when one is
// refused.

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

// storedInterfaceLanguage reads the account column back through the SERVICE the
// handlers write it with, not with a hand-written SELECT: what the next sign-in
// re-issues is whatever that read returns, so a save the repository accepted but
// the settings read never selects would still be a reverted preference.
func storedInterfaceLanguage(t *testing.T, database *gorm.DB, userID uint) string {
	t.Helper()

	settings, err := services.NewSettingsService(db.NewUserRepository(database)).LoadSettings(context.Background(), userID)
	if err != nil {
		t.Fatalf("load settings for the stored interface language: %v", err)
	}
	return settings.InterfaceLanguage
}

func languageSwitchRequest(language string, nextPath string, cookieHeader string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, LanguageSwitchPath, strings.NewReader(url.Values{
		"lang": {language},
		"next": {nextPath},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "en")
	if cookieHeader != "" {
		request.Header.Set("Cookie", cookieHeader)
	}
	return request
}

// TestAuthenticatedLanguageSwitchPersistsToTheAccount pins the account-side half
// of the public switcher, under the real CSRF posture. The switcher is the only
// language control on the onboarding pages, where the visitor already holds a
// session: writing the cookie alone left the account column untouched, so the
// next session issue re-wrote the cookie from that column and reverted the
// choice. The read-back goes through the settings service, the same call
// `/interface` performs.
func TestAuthenticatedLanguageSwitchPersistsToTheAccount(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "lang-switch-persist@example.com")
	if before := storedInterfaceLanguage(t, ctx.database, ctx.user.ID); before != "" {
		t.Fatalf("expected no stored interface language before the switch, got %q", before)
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, LanguageSwitchPath, url.Values{
		"lang": {"ru"},
		"next": {"/dashboard"},
	}, nil)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected the switch to redirect to /dashboard, got %q", location)
	}
	if got := responseCookieValue(response.Cookies(), languageCookieName); got != "ru" {
		t.Fatalf("expected ovumcy_lang=ru on the switch response, got %q", got)
	}
	if got := storedInterfaceLanguage(t, ctx.database, ctx.user.ID); got != "ru" {
		t.Fatalf("expected the switch to store interface_language=ru on the account, got %q", got)
	}
}

// TestAuthenticatedLanguageSwitchStoresNothingForAnUnsupportedLanguage pins the
// validation the account write inherits from `/interface`: a code this build
// does not ship never reaches the column, so a hand-made request cannot pin an
// account — and every device it signs in from — to a language nobody picked.
// The cookie half is deliberately unchanged: it still falls back to the default,
// which is what an unauthenticated caller has always received for the same
// input, so the two callers still cannot be told apart by the answer.
func TestAuthenticatedLanguageSwitchStoresNothingForAnUnsupportedLanguage(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "lang-switch-unsupported@example.com")
	seedStoredInterfaceLanguage(t, ctx.database, ctx.user.ID, "ru")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, LanguageSwitchPath, url.Values{
		"lang": {"zz"},
		"next": {"/dashboard"},
	}, nil)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if got := responseCookieValue(response.Cookies(), languageCookieName); got != "en" {
		t.Fatalf("expected the unsupported code to fall back to the default cookie, got %q", got)
	}
	if got := storedInterfaceLanguage(t, ctx.database, ctx.user.ID); got != "ru" {
		t.Fatalf("expected the account language to survive an unsupported switch, got %q", got)
	}
}

// TestAuthenticatedLanguageSwitchReportsAFailedAccountWrite pins the same
// refusal `/interface` makes: the switch is not silently degraded to a
// cookie-only change when the account write fails, because that is exactly the
// state whose symptom — a language that reverts at the next session issue — this
// change exists to remove. The failure is injected by dropping the column the
// save targets, which leaves session, CSRF and validation working. The error is
// the shared mapped spec, not a status pair invented at the call site.
func TestAuthenticatedLanguageSwitchReportsAFailedAccountWrite(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "lang-switch-write-fails@example.com")
	if err := ctx.database.Exec("ALTER TABLE users DROP COLUMN interface_language").Error; err != nil {
		t.Fatalf("drop interface_language column: %v", err)
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, LanguageSwitchPath, url.Values{
		"lang": {"ru"},
		"next": {"/dashboard"},
	}, map[string]string{"Accept": "application/json"})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusInternalServerError)
	if got := readAPIError(t, response.Body); got != "failed to update interface settings" {
		t.Fatalf("expected the interface-update failure envelope, got %q", got)
	}
	if cookie := responseCookie(response.Cookies(), languageCookieName); cookie != nil {
		t.Fatalf("expected no language cookie when the account write failed, got %#v", cookie)
	}
}

// TestSwitchedLanguageSurvivesASessionReissue is the defect itself, end to end:
// switch mid-session, sign in again, and the re-issued cookie must carry the
// switched language rather than the one the account held before it. Without the
// account write the sign-in re-issues from an empty column, the cookie is not
// written at all, and the owner is back on `Accept-Language: en`.
func TestSwitchedLanguageSurvivesASessionReissue(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "lang-switch-reissue@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	switchResponse := mustAppResponse(t, app, languageSwitchRequest("ru", "/dashboard", authCookie))
	assertStatusCode(t, switchResponse, http.StatusSeeOther)

	reissueResponse := passwordLoginResponse(t, app, user.Email)
	defer func() { _ = reissueResponse.Body.Close() }()

	assertStatusCode(t, reissueResponse, http.StatusSeeOther)
	if got := responseCookieValue(reissueResponse.Cookies(), languageCookieName); got != "ru" {
		t.Fatalf("expected the re-issued session to serve ovumcy_lang=ru, got %q", got)
	}
}

// TestUnauthenticatedLanguageSwitchStaysCookieOnly is the boundary of the write
// above: a visitor with no session changes their own rendering and nothing else.
// The account keeps the language it already had — the anchor that the write is
// genuinely gated on the session rather than merely failing to find a row — and
// the answer is the same 303 to the same path an authenticated switch receives,
// so the response tells nobody whether the request carried a live session.
func TestUnauthenticatedLanguageSwitchStaysCookieOnly(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "lang-switch-anonymous@example.com", "StrongPass1", true)
	seedStoredInterfaceLanguage(t, database, user.ID, "ru")

	response := mustAppResponse(t, app, languageSwitchRequest("de", "/login", ""))
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected the anonymous switch to redirect to /login, got %q", location)
	}
	if got := responseCookieValue(response.Cookies(), languageCookieName); got != "de" {
		t.Fatalf("expected ovumcy_lang=de for the anonymous switch, got %q", got)
	}
	if got := storedInterfaceLanguage(t, database, user.ID); got != "ru" {
		t.Fatalf("expected the anonymous switch to leave the account language alone, got %q", got)
	}
}

// TestSessionRejectionLeavesTheLanguageCookieInPlace is the other side of the
// logout and account-deletion clears. Rejection is not an ending the owner
// chose: it fires on an expired cookie mid-use and on any unauthenticated probe
// carrying a stale one, and taking the language away there would drop a visitor
// back to `Accept-Language` on the login page they are reading — reachable by
// anyone who can send a request. Both rejection shapes are covered: the invalid
// token, and the unsupported role, which is the branch that runs
// clearAuthRelatedCookies and would silently inherit the language clear if it
// were added to that helper instead of to clearSessionEndCookies. The auth
// cookie's own retraction is the anchor: a response that cleared nothing at all
// would otherwise pass.
func TestSessionRejectionLeavesTheLanguageCookieInPlace(t *testing.T) {
	tests := []struct {
		name       string
		authCookie func(t *testing.T, database *gorm.DB) string
	}{
		{
			name: "invalid token",
			authCookie: func(t *testing.T, _ *gorm.DB) string {
				t.Helper()
				return authCookieName + "=not-a-sealed-value"
			},
		},
		{
			name: "unsupported role",
			authCookie: func(t *testing.T, database *gorm.DB) string {
				t.Helper()
				user := createOnboardingTestUser(t, database, "lang-reject-role@example.com", "StrongPass1", true)
				if err := database.Model(&user).Update("role", "partner").Error; err != nil {
					t.Fatalf("set unsupported legacy role: %v", err)
				}
				user.Role = "partner"
				return issueAuthCookieForUser(t, user)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)

			request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", joinCookieHeader(testCase.authCookie(t, database), languageCookieName+"=ru"))

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusSeeOther)

			clearedAuth := responseCookie(response.Cookies(), authCookieName)
			if clearedAuth == nil || strings.TrimSpace(clearedAuth.Value) != "" {
				t.Fatalf("expected the rejected session to clear the auth cookie, got %#v", clearedAuth)
			}
			if cookie := responseCookie(response.Cookies(), languageCookieName); cookie != nil {
				t.Fatalf("expected the rejected session to leave ovumcy_lang alone, got %#v", cookie)
			}
		})
	}
}
