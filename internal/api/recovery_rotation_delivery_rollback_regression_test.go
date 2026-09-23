package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// WEB-58: the three recovery-code rotations used to commit the new code first
// and mint the session and the reveal after, so a failure in between answered
// an error over an account whose old code was gone and whose new code nobody
// had seen. The session and the reveal are now sealed inside the write's
// transaction, before it commits. These tests make session minting fail the
// way no request can (the handler's sessionIssuanceFault seam) and pin, for
// each rotation, that the users row is exactly what it was, that no auth or
// reveal cookie went out, and that the account is still usable.

var errInjectedSessionIssuance = errors.New("injected session issuance failure")

func armableSessionIssuanceFault() (*atomic.Bool, func() error) {
	armed := &atomic.Bool{}
	return armed, func() error {
		if armed.Load() {
			return errInjectedSessionIssuance
		}
		return nil
	}
}

func loadUserRowForRotationTest(t *testing.T, database *gorm.DB, userID uint) models.User {
	t.Helper()
	var row models.User
	if err := database.First(&row, userID).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	return row
}

func assertRotationLeftRowAndCookiesUntouched(t *testing.T, database *gorm.DB, before models.User, response *http.Response) {
	t.Helper()
	after := loadUserRowForRotationTest(t, database, before.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a rotation whose delivery failed changed the users row:\nbefore %+v\nafter  %+v", before, after)
	}
	for _, name := range []string{authCookieName, recoveryCodeCookieName} {
		if cookie := responseCookie(response.Cookies(), name); cookie != nil && strings.TrimSpace(cookie.Value) != "" {
			t.Fatalf("a rotation whose delivery failed must not set %s", name)
		}
	}
}

func TestResetPasswordDeliveryFailureLeavesTheAccountAsItWas(t *testing.T) {
	t.Parallel()

	armed, fault := armableSessionIssuanceFault()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{sessionIssuanceFault: fault})
	user := createOnboardingTestUser(t, database, "web58-reset@example.com", "StrongPass1", true)
	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookie := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	redeem := func() *http.Response {
		form := url.Values{"password": {"EvenStronger2"}, "confirm_password": {"EvenStronger2"}}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookie)
		return mustAppResponse(t, app, request)
	}

	before := loadUserRowForRotationTest(t, database, user.ID)
	armed.Store(true)
	refused := redeem()
	defer func() { _ = refused.Body.Close() }()

	if refused.StatusCode < 400 {
		t.Fatalf("expected the redeem to be refused, got %d", refused.StatusCode)
	}
	if got := readAPIError(t, refused.Body); got != authSessionCreateErrorSpec().Key {
		t.Fatalf("expected %q, got %q", authSessionCreateErrorSpec().Key, got)
	}
	assertRotationLeftRowAndCookiesUntouched(t, database, before, refused)
	if cookie := responseCookie(refused.Cookies(), resetPasswordCookieName); cookie != nil && strings.TrimSpace(cookie.Value) == "" {
		t.Fatal("the reset token is still valid after a rolled-back redeem, so its cookie must not be retracted")
	}

	// Nothing was spent: the same token redeems once issuance works again.
	armed.Store(false)
	accepted := redeem()
	defer func() { _ = accepted.Body.Close() }()
	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("expected the retried redeem to succeed, got %d (body %q)", accepted.StatusCode, mustReadBodyString(t, accepted.Body))
	}
	if cookie := responseCookie(accepted.Cookies(), recoveryCodeCookieName); cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		t.Fatal("the retried redeem must stage the reveal of the code it rotated")
	}
	if loadUserRowForRotationTest(t, database, user.ID).RecoveryCodeHash == before.RecoveryCodeHash {
		t.Fatal("the retried redeem must rotate the recovery code")
	}
}

func TestRegenerateRecoveryCodeDeliveryFailureLeavesTheAccountAsItWas(t *testing.T) {
	t.Parallel()

	armed, fault := armableSessionIssuanceFault()
	ctx := newSettingsSecurityTestContextWithOptions(t, "web58-regenerate@example.com", onboardingTestAppOptions{
		enableCSRF:           true,
		sessionIssuanceFault: fault,
	})
	// The row comparison below covers the code the owner holds: its hash is
	// the one minted here, and a rolled-back regeneration must leave it as is.
	_ = mustSetRecoveryCodeForUser(t, ctx.database, ctx.user.ID)
	regenerate := func() *http.Response {
		return settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{
			"password": {"StrongPass1"},
		}, map[string]string{"Accept": "application/json"})
	}

	before := loadUserRowForRotationTest(t, ctx.database, ctx.user.ID)
	armed.Store(true)
	refused := regenerate()
	defer func() { _ = refused.Body.Close() }()

	if got := readAPIError(t, refused.Body); got != authSessionCreateErrorSpec().Key {
		t.Fatalf("expected %q, got %q", authSessionCreateErrorSpec().Key, got)
	}
	assertRotationLeftRowAndCookiesUntouched(t, ctx.database, before, refused)

	// The session the owner is using was not revoked.
	probe := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	probe.Header.Set("Cookie", ctx.authCookie)
	if response := mustAppResponse(t, ctx.app, probe); response.StatusCode != http.StatusOK {
		t.Fatalf("the session in use must survive a rolled-back regeneration, got %d", response.StatusCode)
	}

	// Nothing was spent: the same request rotates once issuance works again.
	armed.Store(false)
	accepted := regenerate()
	defer func() { _ = accepted.Body.Close() }()
	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("expected the retried regeneration to succeed, got %d (body %q)", accepted.StatusCode, mustReadBodyString(t, accepted.Body))
	}
	if loadUserRowForRotationTest(t, ctx.database, ctx.user.ID).RecoveryCodeHash == before.RecoveryCodeHash {
		t.Fatal("the retried regeneration must rotate the recovery code")
	}
}

func TestLocalPasswordSetupDeliveryFailureLeavesTheAccountAsItWas(t *testing.T) {
	t.Parallel()

	stub := &stubOIDCWorkflowService{enabled: true, localPublicAuthEnabled: true}
	app, database, handler := newSettingsMutationStepupApp(t, stub)
	oidcUser := models.User{
		Email:               "web58-local-setup@example.com",
		LocalAuthEnabled:    false,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&oidcUser).Error; err != nil {
		t.Fatalf("create oidc-only user: %v", err)
	}
	authCookie := issueAuthCookieForUser(t, oidcUser)
	stepupCookie, stateValue := seedStepupCookie(t, app, handler, oidcUser.ID)

	before := loadUserRowForRotationTest(t, database, oidcUser.ID)
	handler.sessionIssuanceFault = func() error { return errInjectedSessionIssuance }

	form := url.Values{"state": {stateValue}, "code": {"stub-code"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, stepupCookie))
	refused := mustAppResponse(t, app, request)
	defer func() { _ = refused.Body.Close() }()

	if refused.StatusCode != http.StatusSeeOther || !strings.HasPrefix(refused.Header.Get("Location"), "/settings") {
		t.Fatalf("expected the refusal to flash back to /settings, got %d to %q", refused.StatusCode, refused.Header.Get("Location"))
	}
	// Every refusal on this callback takes the same 303, so the flash is what
	// names the cause: the delivery failure, not the write it rolled back.
	flash := responseCookie(refused.Cookies(), flashCookieName)
	if flash == nil || strings.TrimSpace(flash.Value) == "" {
		t.Fatal("expected the refusal to carry a settings flash")
	}
	if got := decodeFlashCookieForTest(t, flash.Value).SettingsError; got != authSessionCreateErrorSpec().Key {
		t.Fatalf("expected the flash to carry %q, got %q", authSessionCreateErrorSpec().Key, got)
	}
	assertRotationLeftRowAndCookiesUntouched(t, database, before, refused)

	handler.sessionIssuanceFault = nil
	probe := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	probe.Header.Set("Cookie", authCookie)
	if response := mustAppResponse(t, app, probe); response.StatusCode != http.StatusOK {
		t.Fatalf("the session in use must survive a rolled-back enrollment, got %d", response.StatusCode)
	}
}
