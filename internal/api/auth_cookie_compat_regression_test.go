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
	"github.com/golang-jwt/jwt/v5"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

func TestLoginSetsSealedAuthCookieValue(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "sealed-auth-cookie@example.com", "StrongPass1", true)

	form := url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	authCookie := responseCookie(response.Cookies(), authCookieName)
	if authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatal("expected non-empty auth cookie in login response")
	}
	if !strings.HasPrefix(authCookie.Value, secureCookieVersion+".") {
		t.Fatalf("expected sealed auth cookie with %q prefix, got %q", secureCookieVersion+".", authCookie.Value)
	}

	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(authCookie.Value, claims); err == nil {
		t.Fatal("expected sealed auth cookie value not to be directly parseable as JWT")
	}
}

func TestAuthMiddlewareRejectsLegacyJWTAuthCookieFallback(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "legacy-auth-cookie@example.com", "StrongPass1", true)
	legacyToken := buildLegacyJWTForUser(t, user)

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Cookie", authCookieName+"="+legacyToken)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request with legacy jwt cookie failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303 with rejected legacy jwt cookie, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	clearedCookie := responseCookie(response.Cookies(), authCookieName)
	if clearedCookie == nil {
		t.Fatal("expected response to clear legacy auth cookie")
	}
	if clearedCookie.Value != "" {
		t.Fatalf("expected cleared auth cookie value, got %q", clearedCookie.Value)
	}
}

func TestAuthMiddlewareRejectsRevokedAuthSessionCookieAfterSessionVersionBumpForHTML(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forced-reset-stale-html@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	assertCompatCookieOpensDashboard(t, app, authCookie)
	revokeSessionsThroughRepository(t, database, user.ID)

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request with revoked auth cookie failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303 with revoked auth cookie, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	clearedCookie := responseCookie(response.Cookies(), authCookieName)
	if clearedCookie == nil {
		t.Fatal("expected response to clear revoked auth cookie")
	}
	if clearedCookie.Value != "" {
		t.Fatalf("expected cleared auth cookie value, got %q", clearedCookie.Value)
	}
}

func TestAuthMiddlewareRejectsRevokedAuthSessionCookieForAPI(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forced-reset-stale-api@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	assertCompatCookieOpensDashboard(t, app, authCookie)
	revokeSessionsThroughRepository(t, database, user.ID)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/stats/overview", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("api request with revoked auth cookie failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401 with revoked auth cookie, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "unauthorized" {
		t.Fatalf("expected unauthorized api error, got %q", got)
	}

	clearedCookie := responseCookie(response.Cookies(), authCookieName)
	if clearedCookie == nil {
		t.Fatal("expected api response to clear revoked auth cookie")
	}
	if clearedCookie.Value != "" {
		t.Fatalf("expected cleared auth cookie value, got %q", clearedCookie.Value)
	}
}

// TestAuthMiddlewareRejectsAnIssuedCookieOnceMustChangePasswordIsSet pins the
// forced-reset arm of ResolveAuthSession on its own: only must_change_password
// moves, the session version is asserted untouched, so the version compare
// cannot be what refuses the cookie.
func TestAuthMiddlewareRejectsAnIssuedCookieOnceMustChangePasswordIsSet(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		accept     string
		wantStatus int
	}{
		{name: "html", path: "/dashboard", wantStatus: http.StatusSeeOther},
		{name: "api", path: "/api/v1/stats/overview", accept: "application/json", wantStatus: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "flag-only-"+tc.name+"@example.com", "StrongPass1", true)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			assertCompatCookieOpensDashboard(t, app, authCookie)

			var before models.User
			if err := database.First(&before, user.ID).Error; err != nil {
				t.Fatalf("load user: %v", err)
			}
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
				t.Fatalf("set must_change_password: %v", err)
			}
			var after models.User
			if err := database.First(&after, user.ID).Error; err != nil {
				t.Fatalf("reload user: %v", err)
			}
			if !after.MustChangePassword {
				t.Fatal("fixture: must_change_password was not set")
			}
			if after.AuthSessionVersion != before.AuthSessionVersion {
				t.Fatalf("fixture: the session version moved (%d -> %d), so the version compare could refuse the cookie", before.AuthSessionVersion, after.AuthSessionVersion)
			}

			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.accept != "" {
				request.Header.Set("Accept", tc.accept)
			}
			request.Header.Set("Cookie", authCookie)
			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("request with flagged account cookie failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != tc.wantStatus {
				t.Fatalf("expected status %d for a cookie of an account flagged must_change_password, got %d", tc.wantStatus, response.StatusCode)
			}
			cleared := responseCookie(response.Cookies(), authCookieName)
			if cleared == nil || cleared.Value != "" {
				t.Fatal("expected the refused cookie to be cleared")
			}
		})
	}
}

func buildLegacyJWTForUser(t *testing.T, user models.User) string {
	t.Helper()

	// Session version 1 is pinned deliberately rather than read from the
	// fixture: ResolveAuthSession checks the version before the role, so a
	// mismatched one would make this token die in the revoked arm instead of
	// the legacy-shape arm the test names.
	signed, _, err := services.BuildAuthSessionTokenWithVersionAndSessionID([]byte("test-secret-key"), user.ID, user.Role, 1, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("sign legacy jwt token: %v", err)
	}
	return signed
}

// revokeSessionsThroughRepository revokes by the version bump alone. A forced
// reset would also set must_change_password, which ResolveAuthSession refuses
// on before it ever compares versions, so a test revoking that way stays green
// with the version compare deleted.
func revokeSessionsThroughRepository(t *testing.T, database *gorm.DB, userID uint) {
	t.Helper()

	if err := db.NewUserRepository(database).BumpAuthSessionVersion(context.Background(), userID); err != nil {
		t.Fatalf("bump auth session version: %v", err)
	}
	var reloaded models.User
	if err := database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.MustChangePassword {
		t.Fatal("the revocation must be the version bump alone: must_change_password is set, so the version compare is not what refuses")
	}
}

// assertCompatCookieOpensDashboard is the 200 anchor taken before a
// revocation: without it a later refusal could come from a cookie that never
// opened anything.
func assertCompatCookieOpensDashboard(t *testing.T, app *fiber.App, authCookie string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Cookie", authCookie)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard anchor request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("anchor: a freshly issued auth cookie must open /dashboard, got %d", response.StatusCode)
	}
}

// TestAuthMiddlewareMapsSessionResolveErrorsToClearedAuthCookie drives the
// authenticateRequest switch that maps ResolveAuthSession failures onto the
// generic "clear the cookie and bounce to /login" response. Two arms are
// reachable with a sealed cookie: an expired-but-well-formed token, and a valid
// token whose user id no longer resolves (ErrAuthInvalidCreds).
func TestAuthMiddlewareMapsSessionResolveErrorsToClearedAuthCookie(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "auth-session-resolve-errors@example.com", "StrongPass1", true)

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("init secure cookie codec: %v", err)
	}

	// A well-formed session token whose TTL already elapsed: parsing reports
	// expiry before any user lookup, exercising the token-expired arm.
	expiredToken, _, err := services.BuildAuthSessionTokenWithVersionAndSessionID([]byte("test-secret-key"), user.ID, user.Role, 1, time.Hour, time.Now().Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("build expired session token: %v", err)
	}
	sealedExpired, err := codec.seal(authCookieName, []byte(expiredToken))
	if err != nil {
		t.Fatalf("seal expired session token: %v", err)
	}

	// A valid, unexpired token for a user id that does not exist, so
	// ResolveAuthSession's FindByID fails and returns ErrAuthInvalidCreds.
	unknownUserToken, _, err := services.BuildAuthSessionTokenWithVersionAndSessionID([]byte("test-secret-key"), user.ID+900000, user.Role, 0, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("build unknown-user session token: %v", err)
	}
	sealedUnknownUser, err := codec.seal(authCookieName, []byte(unknownUserToken))
	if err != nil {
		t.Fatalf("seal unknown-user session token: %v", err)
	}

	testCases := []struct {
		name        string
		cookieValue string
	}{
		{name: "expired session token", cookieValue: sealedExpired},
		{name: "unknown user credentials", cookieValue: sealedUnknownUser},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
			request.Header.Set("Cookie", authCookieName+"="+testCase.cookieValue)

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("dashboard request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusSeeOther {
				t.Fatalf("expected status 303, got %d", response.StatusCode)
			}
			if location := response.Header.Get("Location"); location != "/login" {
				t.Fatalf("expected redirect to /login, got %q", location)
			}
			clearedCookie := responseCookie(response.Cookies(), authCookieName)
			if clearedCookie == nil {
				t.Fatal("expected response to clear the auth cookie")
			}
			if clearedCookie.Value != "" {
				t.Fatalf("expected cleared auth cookie value, got %q", clearedCookie.Value)
			}
		})
	}
}
