package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

type settingsSecurityTestContext struct {
	app        *fiber.App
	database   *gorm.DB
	user       models.User
	authCookie string
	csrfCookie *http.Cookie
	csrfToken  string
}

func newSettingsSecurityTestContext(t *testing.T, email string) settingsSecurityTestContext {
	t.Helper()
	return newSettingsSecurityTestContextWithOptions(t, email, onboardingTestAppOptions{enableCSRF: true})
}

func newSettingsSecurityTestContextWithOptions(t *testing.T, email string, options onboardingTestAppOptions) settingsSecurityTestContext {
	t.Helper()

	app, database := newOnboardingTestAppWithOptions(t, options)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	return settingsSecurityTestContext{
		app:        app,
		database:   database,
		user:       user,
		authCookie: authCookie,
		csrfCookie: csrfCookie,
		csrfToken:  csrfToken,
	}
}

func newOIDCOnlySettingsSecurityTestContext(t *testing.T, email string) settingsSecurityTestContext {
	t.Helper()

	app, database := newOnboardingTestAppWithCSRF(t)
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
	var persisted models.User
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("reload oidc-only user: %v", err)
	}
	if persisted.LocalAuthEnabled {
		t.Fatal("expected oidc-only test user to persist with local auth disabled")
	}

	authCookie := issueAuthCookieForUser(t, user)
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	return settingsSecurityTestContext{
		app:        app,
		database:   database,
		user:       user,
		authCookie: authCookie,
		csrfCookie: csrfCookie,
		csrfToken:  csrfToken,
	}
}

// refreshAuthCookie re-issues the context's auth cookie against the latest
// auth_session_version persisted for the user. Tests that mutate the user row
// via a back-door service call (for example, enabling TOTP directly through
// services.TOTPService) need to invoke this so the cookie they carry forward
// matches the freshly bumped session version, instead of being rejected as a
// revoked session by AuthRequired.
func (ctx *settingsSecurityTestContext) refreshAuthCookie(t *testing.T) {
	t.Helper()
	var reloaded models.User
	if err := ctx.database.First(&reloaded, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user for auth refresh: %v", err)
	}
	ctx.user = reloaded
	ctx.authCookie = issueAuthCookieForUser(t, reloaded)
}

func loadSettingsCSRFContext(t *testing.T, app *fiber.App, authCookie string) (*http.Cookie, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/settings", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request for csrf context failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected settings status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read settings body for csrf context: %v", err)
	}
	csrfToken := extractCSRFTokenFromHTML(t, string(body))
	csrfCookie := responseCookie(response.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatalf("expected csrf cookie in settings response")
	}

	return csrfCookie, csrfToken
}

func settingsCookieHeader(authCookie string, csrfCookie *http.Cookie) string {
	if csrfCookie == nil {
		return authCookie
	}
	return joinCookieHeader(authCookie, cookiePair(csrfCookie))
}

func settingsFormRequestWithCSRF(t *testing.T, ctx settingsSecurityTestContext, method string, path string, form url.Values, headers map[string]string) *http.Response {
	t.Helper()

	cloned := cloneFormValues(form)
	cloned.Set("csrf_token", ctx.csrfToken)

	request := httptest.NewRequest(method, path, strings.NewReader(cloned.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings request %s %s failed: %v", method, path, err)
	}
	return response
}
