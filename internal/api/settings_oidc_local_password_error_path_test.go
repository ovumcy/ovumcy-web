package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestOIDCStartLocalPasswordSetupRejectsLocalAuthAlreadyEnabled(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(true)
	stub.localPublicAuthEnabled = true
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		enableCSRF:   true,
		cookieSecure: true,
		oidcService:  stub,
	})

	// Use a user with LocalAuthEnabled=true — start should reject with 400.
	user := models.User{
		Email:               "stepup-local-enabled@example.com",
		PasswordHash:        "$2a$10$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		RecoveryCodeHash:    "recovery",
		Role:                models.RoleOwner,
		LocalAuthEnabled:    true,
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	authCookie := issueAuthCookieForUser(t, user)

	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)
	form := url.Values{
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
		"csrf_token":       {csrfToken},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/password/step-up", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", settingsCookieHeader(authCookie, csrfCookie))
	resp := mustAppResponse(t, app, req)
	defer func() { _ = resp.Body.Close() }()

	assertStatusCode(t, resp, http.StatusBadRequest)
	if got := readAPIError(t, resp.Body); got != "invalid settings input" {
		t.Fatalf("expected error %q, got %q", "invalid settings input", got)
	}
}

func TestOIDCStartLocalPasswordSetupOIDCServiceDisabled(t *testing.T) {
	t.Parallel()

	stub := newStubOIDCWorkflowService(false)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		enableCSRF:   true,
		cookieSecure: true,
		oidcService:  stub,
	})

	user := models.User{
		Email:               "stepup-disabled-svc@example.com",
		Role:                models.RoleOwner,
		LocalAuthEnabled:    false,
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	authCookie := issueAuthCookieForUser(t, user)

	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)
	form := url.Values{
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
		"csrf_token":       {csrfToken},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/password/step-up", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", settingsCookieHeader(authCookie, csrfCookie))
	resp := mustAppResponse(t, app, req)
	defer func() { _ = resp.Body.Close() }()

	assertStatusCode(t, resp, http.StatusServiceUnavailable)
	if got := readAPIError(t, resp.Body); got != "sso temporarily unavailable" {
		t.Fatalf("expected error %q, got %q", "sso temporarily unavailable", got)
	}
}

func TestOIDCStartLocalPasswordSetupStartReauthError(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "stepup-start-err@example.com")
	fixture.oidcStub.reauthStartErr = services.ErrOIDCUnavailable

	resp := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = resp.Body.Close() }()

	assertStatusCode(t, resp, http.StatusServiceUnavailable)
	for _, c := range resp.Cookies() {
		if c.Name == oidcStepupCookieName && c.Value != "" {
			t.Fatal("expected stepup cookie to be cleared after start error")
		}
	}
}

func TestOIDCCompleteLocalPasswordSetupStateMismatch(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "stepup-state-mismatch@example.com")

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, "wrong-state-value", "code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect on state mismatch, got %d", callbackResponse.StatusCode)
	}
	if location := callbackResponse.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}
}

func TestOIDCCompleteLocalPasswordSetupProviderErrorParam(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "stepup-provider-error@example.com")

	startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture, stepupCookie)

	form := url.Values{
		"state": {state},
		"error": {"access_denied"},
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, stepupCookie))
	callbackResponse := mustAppResponse(t, fixture.app, req)
	defer func() { _ = callbackResponse.Body.Close() }()

	if callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect on provider error, got %d", callbackResponse.StatusCode)
	}
	if location := callbackResponse.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}
	if flashValue := responseCookieValue(callbackResponse.Cookies(), flashCookieName); flashValue == "" {
		t.Fatal("expected flash cookie on provider error callback")
	}
}

func TestMapLocalPasswordSetupReauthError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantKey string
	}{
		{"callback invalid", services.ErrOIDCCallbackInvalid, "sso authentication failed"},
		{"disabled", services.ErrOIDCDisabled, "sso temporarily unavailable"},
		{"unavailable", services.ErrOIDCUnavailable, "sso temporarily unavailable"},
		{"reauth stale", services.ErrOIDCReauthStale, "oidc reauth stale"},
		{"identity mismatch", services.ErrOIDCReauthIdentityMismatch, "oidc reauth identity mismatch"},
		{"authentication failed", services.ErrOIDCAuthenticationFailed, "sso authentication failed"},
		{"unknown error", errors.New("some other error"), "sso authentication failed"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := mapLocalPasswordSetupReauthError(tc.err)
			if spec.Key != tc.wantKey {
				t.Fatalf("expected key %q, got %q", tc.wantKey, spec.Key)
			}
		})
	}
}
