package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// Test fixture for the OIDC step-up "enable local password" flow.
//
// We assemble the app with cookieSecure=true (required by the stepup cookie's
// SameSite=None;Secure attributes), drop in a stubOIDCWorkflowService whose
// reauth result we control, create an OIDC-only owner, and link the OIDC
// identity to that owner so ValidateReauthExchange's identity-binding check
// has a row to match.
type oidcStepupFixture struct {
	app           *fiber.App
	database      *gorm.DB
	user          models.User
	identity      models.OIDCIdentity
	authCookie    string
	oidcStub      *stubOIDCWorkflowService
	cookieSecure  bool
	stepupIssuer  string
	stepupSubject string
}

func newOIDCStepupFixture(t *testing.T, email string) *oidcStepupFixture {
	t.Helper()

	stub := newStubOIDCWorkflowService(true)
	stub.localPublicAuthEnabled = true
	stub.reauthURL = "https://id.example.com/authorize?prompt=login"

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		enableCSRF:   true,
		cookieSecure: true,
		oidcService:  stub,
	})

	user := models.User{
		Email:               strings.ToLower(strings.TrimSpace(email)),
		LocalAuthEnabled:    false,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create oidc-only user: %v", err)
	}

	issuer := "https://id.example.com"
	subject := "stepup-test-" + strings.TrimSuffix(email, "@example.com")
	identity := models.OIDCIdentity{
		UserID:    user.ID,
		Issuer:    issuer,
		Subject:   subject,
		CreatedAt: time.Now().UTC(),
	}
	if err := database.Create(&identity).Error; err != nil {
		t.Fatalf("link oidc identity: %v", err)
	}

	return &oidcStepupFixture{
		app:           app,
		database:      database,
		user:          user,
		identity:      identity,
		authCookie:    issueAuthCookieForUser(t, user),
		oidcStub:      stub,
		cookieSecure:  true,
		stepupIssuer:  issuer,
		stepupSubject: subject,
	}
}

func (fixture *oidcStepupFixture) settingsCSRF(t *testing.T) (*http.Cookie, string) {
	t.Helper()
	return loadSettingsCSRFContext(t, fixture.app, fixture.authCookie)
}

func (fixture *oidcStepupFixture) postStart(t *testing.T, newPassword, confirmPassword string) *http.Response {
	t.Helper()
	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{
		"new_password":     {newPassword},
		"confirm_password": {confirmPassword},
		"csrf_token":       {csrfToken},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/password/step-up", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))
	return mustAppResponse(t, fixture.app, request)
}

// readStepupCookie extracts the stepup cookie from a start response and
// returns the "name=value" pair usable in a follow-up Cookie header.
func readStepupCookie(t *testing.T, response *http.Response) string {
	t.Helper()
	for _, c := range response.Cookies() {
		if c.Name == oidcStepupCookieName && c.Value != "" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatal("expected ovumcy_oidc_stepup cookie in start response")
	return ""
}

func decodeStepupRedirectJSON(t *testing.T, body []byte) string {
	t.Helper()
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode start response JSON: %v", err)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %#v", payload)
	}
	target, _ := payload["redirect_url"].(string)
	if target == "" {
		t.Fatalf("expected redirect_url in start response, got %#v", payload)
	}
	return target
}

func extractStepupCallbackState(t *testing.T, fixture *oidcStepupFixture, stepupCookieHeader string) string {
	t.Helper()
	// We only need the State value that the stub recorded — the cookie is
	// opaque to the test code but the stub captured the same string the
	// handler put inside the sealed cookie.
	if fixture.oidcStub.lastReauthState == "" {
		t.Fatal("expected stub to have recorded a reauth state")
	}
	return fixture.oidcStub.lastReauthState
}

func postOIDCStepupCallback(t *testing.T, fixture *oidcStepupFixture, stepupCookieHeader, state, code string) *http.Response {
	t.Helper()
	form := url.Values{
		"state": {state},
		"code":  {code},
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, stepupCookieHeader))
	return mustAppResponse(t, fixture.app, request)
}

func TestOIDCStartLocalPasswordSetupIssuesRedirectAndStepupCookie(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-stepup-start@example.com")

	response := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)
	body := mustReadBodyString(t, response.Body)
	target := decodeStepupRedirectJSON(t, []byte(body))
	if target != fixture.oidcStub.reauthURL {
		t.Fatalf("expected redirect_url %q, got %q", fixture.oidcStub.reauthURL, target)
	}
	_ = readStepupCookie(t, response)

	// Stub recorded the state/nonce/verifier the handler passed into StartReauth.
	if fixture.oidcStub.lastReauthState == "" {
		t.Fatal("expected stub to capture stepup state")
	}
	if fixture.oidcStub.lastReauthNonce == "" {
		t.Fatal("expected stub to capture stepup nonce")
	}
	if fixture.oidcStub.lastReauthVerifier == "" {
		t.Fatal("expected stub to capture stepup pkce verifier")
	}

	// Nothing must be written to the DB yet — finalize only happens on a
	// successful callback.
	var persisted models.User
	if err := fixture.database.First(&persisted, fixture.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if persisted.LocalAuthEnabled {
		t.Fatal("local auth must not flip on at the start step")
	}
	if strings.TrimSpace(persisted.PasswordHash) != "" {
		t.Fatal("password hash must not be persisted before reauth completes")
	}
}

// A real browser submits the local-password step-up form as an ordinary HTML
// form POST (Accept: text/html), not via fetch. The settings page CSP pins
// form-action to 'self', and Chromium enforces that across the whole redirect
// chain of a form navigation — so answering the submit with a 303 to the
// cross-origin provider authorize endpoint aborts client-side
// (net::ERR_ABORTED, reproduced in the oidc_only e2e lane). The browser path
// must instead return a same-origin 200 interstitial whose meta-refresh
// performs the cross-origin hop, keeping it out of the form submission.
func TestOIDCStartLocalPasswordSetupBrowserPathReturnsSameOriginInterstitial(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-stepup-browser@example.com")

	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
		"csrf_token":       {csrfToken},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/password/step-up", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)
	if location := response.Header.Get("Location"); location != "" {
		t.Fatalf("browser step-up path must not emit a redirect the form navigation would follow; got Location %q", location)
	}
	if ct := response.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected an html interstitial, got content-type %q", ct)
	}
	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, `http-equiv="refresh"`) {
		t.Fatalf("expected a meta-refresh interstitial, got %q", body)
	}
	if !strings.Contains(body, fixture.oidcStub.reauthURL) {
		t.Fatalf("expected the interstitial to target the provider authorize URL %q, got %q", fixture.oidcStub.reauthURL, body)
	}
	// The sealed step-up cookie must still ride along so the callback can finalize.
	_ = readStepupCookie(t, response)
}

func TestOIDCStartLocalPasswordSetupRejectsWeakPassword(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-stepup-weak@example.com")
	response := fixture.postStart(t, "short", "short")
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusBadRequest)
	if got := readAPIError(t, response.Body); got != "weak password" {
		t.Fatalf("expected error %q, got %q", "weak password", got)
	}
	if fixture.oidcStub.lastReauthState != "" {
		t.Fatal("must not initiate reauth when password validation fails")
	}
	for _, c := range response.Cookies() {
		if c.Name == oidcStepupCookieName && c.Value != "" {
			t.Fatal("must not issue stepup cookie when password validation fails")
		}
	}
}

func TestOIDCCompleteLocalPasswordSetupFinalizesOnFreshReauth(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-stepup-complete@example.com")
	fixture.oidcStub.reauthErr = nil

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture, stepupCookie)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if callbackResponse.StatusCode != http.StatusOK && callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 200/303 after fresh reauth, got %d", callbackResponse.StatusCode)
	}
	if fixture.oidcStub.lastReauthUserID != fixture.user.ID {
		t.Fatalf("expected ValidateReauthExchange to be called with user %d, got %d", fixture.user.ID, fixture.oidcStub.lastReauthUserID)
	}
	if fixture.oidcStub.lastReauthMaxAge == 0 {
		t.Fatal("expected max-age to be passed into ValidateReauthExchange")
	}

	var persisted models.User
	if err := fixture.database.First(&persisted, fixture.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !persisted.LocalAuthEnabled {
		t.Fatal("expected local auth to be enabled after successful reauth")
	}
	if strings.TrimSpace(persisted.PasswordHash) == "" {
		t.Fatal("expected stored password hash after successful reauth")
	}
	if strings.TrimSpace(persisted.RecoveryCodeHash) == "" {
		t.Fatal("expected stored recovery code hash after successful reauth")
	}
}

func TestOIDCCompleteLocalPasswordSetupRejectsStaleReauth(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-stepup-stale@example.com")
	fixture.oidcStub.reauthErr = services.ErrOIDCReauthStale

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture, stepupCookie)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect after stale reauth, got %d", callbackResponse.StatusCode)
	}
	if location := callbackResponse.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}

	var persisted models.User
	if err := fixture.database.First(&persisted, fixture.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if persisted.LocalAuthEnabled {
		t.Fatal("stale reauth must not enable local auth")
	}
}

func TestOIDCCompleteLocalPasswordSetupRejectsIdentityMismatch(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-stepup-mismatch@example.com")
	fixture.oidcStub.reauthErr = services.ErrOIDCReauthIdentityMismatch

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture, stepupCookie)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect after identity mismatch, got %d", callbackResponse.StatusCode)
	}

	var persisted models.User
	if err := fixture.database.First(&persisted, fixture.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if persisted.LocalAuthEnabled {
		t.Fatal("identity-mismatch reauth must not enable local auth")
	}
}
