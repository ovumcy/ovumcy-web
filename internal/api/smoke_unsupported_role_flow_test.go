package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
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
// The route redeems three token purposes through the same resolver — recovery,
// forced-from-LOCAL and forced-from-OIDC — so each is minted and refused on its
// own: a role check that moved into one purpose's branch would leave the other
// two rotating the code.
//
// The positive anchor is the same cookie redeemed once the role is back to
// owner: it succeeds AND rewrites the row, so the refusal above is the role's
// doing and not a stale or malformed token, proven on the same channel.
func TestUnsupportedLegacyRoleResetRedeemWritesNothing(t *testing.T) {
	t.Parallel()

	for _, purpose := range []string{
		services.PasswordResetTokenPurposeRecovery,
		services.PasswordResetTokenPurposeForcedLocal,
		services.PasswordResetTokenPurposeForcedOIDC,
	} {
		t.Run(purpose, func(t *testing.T) {
			t.Parallel()
			assertRoleRefusedResetRedeemWritesNothing(t, purpose)
		})
	}
}

func assertRoleRefusedResetRedeemWritesNothing(t *testing.T, purpose string) {
	// The three purposes run the same assertions, so each message names its purpose.
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("[%s] "+format, append([]any{purpose}, args...)...)
	}

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "smoke-legacy-reset-"+purpose+"@example.com", "StrongPass1", true)
	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)

	var resetCookieValue string
	if purpose == services.PasswordResetTokenPurposeRecovery {
		// The recovery purpose is minted by the real start route, as a browser gets it.
		resetCookieValue = requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")
	} else {
		// The forced purposes are built directly, not through the login and OIDC
		// routes that mint them in production: this pins the redeem-side resolver
		// for each purpose, not the minting routes. The token binds the stored
		// row, re-read here so a setup step that touched it cannot leave the
		// token stale and the refusal owed to that instead of to the role.
		var current models.User
		if err := database.First(&current, user.ID).Error; err != nil {
			fail("load user before minting: %v", err)
		}
		token, err := services.BuildPasswordResetToken([]byte(testHandlerSecretKey), current.ID, current.PasswordHash, current.AuthSessionVersion, purpose, 30*time.Minute, time.Now())
		if err != nil {
			fail("BuildPasswordResetToken: %v", err)
		}
		resetCookieValue = mustSealResetCookieValueForTest(t, []byte(testHandlerSecretKey), token)
	}

	var before models.User
	if err := database.First(&before, user.ID).Error; err != nil {
		fail("load user before redeem: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("role", "partner").Error; err != nil {
		fail("set unsupported legacy role: %v", err)
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
		fail("load user after refused redeem: %v", err)
	}
	if after.PasswordHash != before.PasswordHash {
		fail("the role-refused redeem rewrote the password")
	}
	if after.RecoveryCodeHash != before.RecoveryCodeHash {
		fail("the role-refused redeem rotated the recovery code: the new one was never revealed and the old one is gone")
	}
	if after.AuthSessionVersion != before.AuthSessionVersion {
		fail("the role-refused redeem bumped auth_session_version from %d to %d", before.AuthSessionVersion, after.AuthSessionVersion)
	}
	if refused.StatusCode != http.StatusBadRequest {
		fail("expected the role-refused redeem to answer %d, got %d", http.StatusBadRequest, refused.StatusCode)
	}
	refusedBody, err := io.ReadAll(refused.Body)
	if err != nil {
		fail("read refused redeem body: %v", err)
	}
	var refusal struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(refusedBody, &refusal); err != nil {
		fail("decode refused redeem body %q: %v", refusedBody, err)
	}
	if refusal.Error != "invalid reset token" {
		fail("expected the role-refused redeem to answer as an invalid reset token before any write, got %q", refusal.Error)
	}
	for _, name := range []string{authCookieName, recoveryCodeCookieName} {
		if cookie := responseCookie(refused.Cookies(), name); cookie != nil && strings.TrimSpace(cookie.Value) != "" {
			fail("a role-refused redeem must not set %s", name)
		}
	}
	if cookie := responseCookie(refused.Cookies(), resetPasswordCookieName); cookie == nil || strings.TrimSpace(cookie.Value) != "" {
		fail("a role-refused redeem answers as an invalid reset token and must retract the reset cookie, got %#v", cookie)
	}

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("role", models.RoleOwner).Error; err != nil {
		fail("restore owner role: %v", err)
	}
	accepted := redeem()
	if accepted.StatusCode != http.StatusOK {
		fail("anchor: expected the owner's redeem to answer %d, got %d", http.StatusOK, accepted.StatusCode)
	}
	if cookie := responseCookie(accepted.Cookies(), recoveryCodeCookieName); cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		fail("anchor: the same cookie redeemed by an owner must stage the recovery-code reveal — without it the refusal above proves nothing about the role")
	}
	var rewritten models.User
	if err := database.First(&rewritten, user.ID).Error; err != nil {
		fail("load user after accepted redeem: %v", err)
	}
	if rewritten.PasswordHash == before.PasswordHash || rewritten.RecoveryCodeHash == before.RecoveryCodeHash {
		fail("anchor: the owner's redeem must rewrite the password and rotate the recovery code on the row — the refusal above is only meaningful against a write that does happen")
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
