package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestLoginSetsSealedAuthCookieValue(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "sealed-auth-cookie@example.com", "StrongPass1", true)

	form := url.Values{
		"email":    {user.Email},
		"password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer response.Body.Close()

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

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request with legacy jwt cookie failed: %v", err)
	}
	defer response.Body.Close()

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

func TestAuthMiddlewareRejectsRevokedAuthSessionCookieAfterForcedResetForHTML(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forced-reset-stale-html@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	forceResetPasswordHash(t, database, user.ID, "EvenStronger2")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request with revoked auth cookie failed: %v", err)
	}
	defer response.Body.Close()

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
	forceResetPasswordHash(t, database, user.ID, "EvenStronger2")

	request := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("api request with revoked auth cookie failed: %v", err)
	}
	defer response.Body.Close()

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

func buildLegacyJWTForUser(t *testing.T, user models.User) string {
	t.Helper()

	signed, err := services.BuildAuthSessionToken([]byte("test-secret-key"), user.ID, user.Role, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("sign legacy jwt token: %v", err)
	}
	return signed
}

func forceResetPasswordHash(t *testing.T, database *gorm.DB, userID uint, password string) {
	t.Helper()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash forced-reset password: %v", err)
	}

	updateResult := database.Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"password_hash":        string(passwordHash),
			"must_change_password": true,
		})
	if err := updateResult.Error; err != nil {
		t.Fatalf("mark user forced-reset state: %v", err)
	}
}
