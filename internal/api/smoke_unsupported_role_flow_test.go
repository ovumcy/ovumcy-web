package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestUnsupportedLegacyRoleResetRedeemWritesNothing pins the guard that keeps a
// role-refused reset from costing the account its recovery code.
// CompleteReset rotates the password AND the recovery code in one write, and
// the reveal of the new code is staged only after a session is issued — so a
// redeem that reached the write and was refused a session afterwards would
// leave the account with a recovery code nobody saw and the previous one
// destroyed. What stops that is the role check inside the reset-token
// resolution, which answers "invalid reset token" before any write; the
// handler's own unsupported-role arm after the write is never reached on this
// route. The claim is proven on the row, not on the status.
//
// The positive anchor is the same cookie redeemed once the role is back to
// owner: it succeeds, so the refusal above is the role's doing and not a stale
// or malformed token.
func TestUnsupportedLegacyRoleResetRedeemWritesNothing(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "smoke-legacy-reset@example.com", "StrongPass1", true)
	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookieValue := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	var before models.User
	if err := database.First(&before, user.ID).Error; err != nil {
		t.Fatalf("load user before redeem: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}

	redeem := func() *http.Response {
		form := url.Values{"password": {"EvenStronger2"}, "confirm_password": {"EvenStronger2"}}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)
		return mustAppResponse(t, app, request)
	}

	refused := redeem()

	var after models.User
	if err := database.First(&after, user.ID).Error; err != nil {
		t.Fatalf("load user after refused redeem: %v", err)
	}
	if after.PasswordHash != before.PasswordHash {
		t.Fatal("the role-refused redeem rewrote the password")
	}
	if after.RecoveryCodeHash != before.RecoveryCodeHash {
		t.Fatal("the role-refused redeem rotated the recovery code: the new one was never revealed and the old one is gone")
	}
	if after.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("the role-refused redeem bumped auth_session_version from %d to %d", before.AuthSessionVersion, after.AuthSessionVersion)
	}
	assertStatusCode(t, refused, http.StatusBadRequest)
	if got := readAPIError(t, refused.Body); got != "invalid reset token" {
		t.Fatalf("expected the role-refused redeem to answer as an invalid reset token before any write, got %q", got)
	}
	for _, name := range []string{authCookieName, recoveryCodeCookieName} {
		if cookie := responseCookie(refused.Cookies(), name); cookie != nil && strings.TrimSpace(cookie.Value) != "" {
			t.Fatalf("a role-refused redeem must not set %s", name)
		}
	}

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("role", models.RoleOwner).Error; err != nil {
		t.Fatalf("restore owner role: %v", err)
	}
	accepted := redeem()
	assertStatusCode(t, accepted, http.StatusOK)
	if cookie := responseCookie(accepted.Cookies(), recoveryCodeCookieName); cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		t.Fatal("anchor: the same cookie redeemed by an owner must stage the recovery-code reveal — without it the refusal above proves nothing about the role")
	}
}

func TestUnsupportedLegacyRoleLoginIsRejected(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "smoke-legacy@example.com", "StrongPass1", true)
	if err := database.Model(&user).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}

	form := url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusForbidden)
	if got := readAPIError(t, response.Body); got != "web sign-in unavailable" {
		t.Fatalf("expected unsupported-role sign-in error, got %q", got)
	}
	if cookie := responseCookie(response.Cookies(), authCookieName); cookie != nil && strings.TrimSpace(cookie.Value) != "" {
		t.Fatalf("did not expect auth cookie for unsupported legacy role")
	}
}

func TestUnsupportedLegacyRoleSessionIsDeniedAndCleared(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "smoke-legacy-session@example.com", "StrongPass1", true)
	if err := database.Model(&user).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}
	user.Role = "partner"
	authCookie := issueAuthCookieForUser(t, user)

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}
	cleared := responseCookie(response.Cookies(), authCookieName)
	if cleared == nil || strings.TrimSpace(cleared.Value) != "" {
		t.Fatalf("expected dashboard denial to clear auth cookie, got %#v", cleared)
	}
}

func TestUnsupportedLegacyRoleAPIAccessIsRejected(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "smoke-legacy-api@example.com", "StrongPass1", true)
	if err := database.Model(&user).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}
	user.Role = "partner"
	authCookie := issueAuthCookieForUser(t, user)

	request := newExportRequestForTest(t, "/api/v1/exports/csv?from=2026-02-01&to=2026-02-28", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusForbidden)
	if got := readAPIError(t, response.Body); got != "web sign-in unavailable" {
		t.Fatalf("expected unsupported-role sign-in error, got %q", got)
	}
	cleared := responseCookie(response.Cookies(), authCookieName)
	if cleared == nil || strings.TrimSpace(cleared.Value) != "" {
		t.Fatalf("expected api denial to clear auth cookie, got %#v", cleared)
	}
}

func TestUnsupportedLegacyRoleOnboardingMutationsAreRejected(t *testing.T) {
	t.Parallel()

	onboardingMutations := []struct {
		name string
		path string
	}{
		{name: "step1", path: "/api/v1/onboarding/steps/1"},
		{name: "step2", path: "/api/v1/onboarding/steps/2"},
		{name: "complete", path: "/api/v1/onboarding/complete"},
	}

	for _, mutation := range onboardingMutations {
		t.Run(mutation.name, func(t *testing.T) {
			t.Parallel()

			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "smoke-legacy-onboarding-"+mutation.name+"@example.com", "StrongPass1", false)
			if err := database.Model(&user).Update("role", "partner").Error; err != nil {
				t.Fatalf("set unsupported legacy role: %v", err)
			}
			user.Role = "partner"
			authCookie := issueAuthCookieForUser(t, user)

			request := httptest.NewRequest(http.MethodPost, mutation.path, strings.NewReader(""))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "application/json")
			request.Header.Set("Cookie", authCookie)

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusForbidden)
			if got := readAPIError(t, response.Body); got != "web sign-in unavailable" {
				t.Fatalf("expected unsupported-role error on %s, got %q", mutation.path, got)
			}
			cleared := responseCookie(response.Cookies(), authCookieName)
			if cleared == nil || strings.TrimSpace(cleared.Value) != "" {
				t.Fatalf("expected onboarding %s denial to clear auth cookie, got %#v", mutation.name, cleared)
			}
		})
	}
}
