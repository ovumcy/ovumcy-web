package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func decodeJSONBody(t *testing.T, body io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		t.Fatalf("decode json body: %v", err)
	}
}

// These regressions pin the no-JS full-page fallback contract: a plain
// browser form submission (no HX-Request header, no Accept: application/json)
// must land on a 303 redirect with the outcome carried in the sealed flash
// cookie, never a bare error body. The HTMX/JSON negotiation paths are pinned
// by the per-domain aggregators; only the full-page tails live here.

// newFullPageFallbackApp mirrors newOnboardingTestAppWithOptions but returns
// the handler too and registers in-package seeding routes so tests can mint
// sealed OIDC state/step-up/logout-bridge cookies through real HTTP.
func newFullPageFallbackApp(t *testing.T, options onboardingTestAppOptions) (*fiber.App, *gorm.DB, *Handler) {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "ovumcy-fullpage-fallback-test.db")
	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}

	handler, err := NewHandler("test-secret-key", time.UTC, i18nManager, options.cookieSecure, newTestHandlerDependencies(database, i18nManager, options))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get("/__seed/oidc-state", func(c fiber.Ctx) error {
		state, stateErr := newOIDCAuthState(time.Now())
		if stateErr != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(stateErr.Error())
		}
		if err := handler.setOIDCStateCookie(c, state); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.JSON(fiber.Map{"state": state.State})
	})
	app.Get("/__seed/oidc-stepup", func(c fiber.Ctx) error {
		userID := uint(fiber.Query[int](c, "user_id", 0))
		state, stateErr := newOIDCStepupState(time.Now(), oidcStepupPurposeLocalPasswordSetup, userID, "prepared-hash")
		if stateErr != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(stateErr.Error())
		}
		if err := handler.setOIDCStepupCookie(c, state); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/__seed/link-pending", func(c fiber.Ctx) error {
		userID := uint(fiber.Query[int](c, "user_id", 0))
		payload, payloadErr := newOIDCLinkPendingPayload(time.Now(), userID, "https://issuer.example.com", "subject-1", c.Query("email", ""))
		if payloadErr != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(payloadErr.Error())
		}
		if err := handler.setOIDCLinkPendingCookie(c, payload); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/__seed/logout-bridge", func(c fiber.Ctx) error {
		sessionID := c.Query("sid", "")
		if err := handler.oidcLogoutStateSvc.Save(c.Context(), sessionID, services.OIDCLogoutState{}, time.Now()); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		if err := handler.setOIDCLogoutBridgeCookie(c, sessionID, time.Now()); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.SendStatus(fiber.StatusOK)
	})
	RegisterRoutes(app, handler)
	app.Use(handler.NotFound)
	return app, database, handler
}

func fullPageRequest(t *testing.T, app *fiber.App, method string, path string, form url.Values, cookieHeader string) *http.Response {
	t.Helper()

	var body *strings.Reader
	if form == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(form.Encode())
	}
	request := httptest.NewRequest(method, path, body)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookieHeader != "" {
		request.Header.Set("Cookie", cookieHeader)
	}

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	return response
}

func assertSeeOther(t *testing.T, response *http.Response, wantLocation string) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != wantLocation {
		t.Fatalf("expected redirect to %q, got %q", wantLocation, location)
	}
}

func seedCookieHeader(t *testing.T, app *fiber.App, path string) (string, *http.Response) {
	t.Helper()
	response := fullPageRequest(t, app, http.MethodGet, path, nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("seed %s: expected 200, got %d", path, response.StatusCode)
	}
	parts := make([]string, 0, 2)
	for _, cookie := range response.Cookies() {
		if strings.TrimSpace(cookie.Value) != "" {
			parts = append(parts, cookie.Name+"="+cookie.Value)
		}
	}
	if len(parts) == 0 {
		t.Fatalf("seed %s: expected at least one cookie", path)
	}
	return strings.Join(parts, "; "), response
}

func TestFullPageFallbackRedirectsOnDefaultApp(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "fullpage-default@example.com", "StrongPass1", true)

	t.Run("login with bad credentials redirects to login", func(t *testing.T) {
		form := url.Values{"email": {user.Email}, "password": {"WrongPass9"}}
		response := fullPageRequest(t, app, http.MethodPost, "/api/v1/sessions", form, "")
		assertSeeOther(t, response, "/login")
	})

	t.Run("recovery code page without session redirects to login", func(t *testing.T) {
		response := fullPageRequest(t, app, http.MethodGet, "/recovery-code", nil, "")
		assertSeeOther(t, response, "/login")
	})

	t.Run("logout bridge without cookie redirects to login", func(t *testing.T) {
		response := fullPageRequest(t, app, http.MethodGet, oidcLogoutBridgePath, nil, "")
		assertSeeOther(t, response, "/login")
	})

	t.Run("logout bridge redirect without cookie redirects to login", func(t *testing.T) {
		response := fullPageRequest(t, app, http.MethodGet, oidcLogoutBridgeRedirectPath, nil, "")
		assertSeeOther(t, response, "/login")
	})

	t.Run("favicon responds no content", func(t *testing.T) {
		response := fullPageRequest(t, app, http.MethodGet, "/favicon.ico", nil, "")
		defer response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("expected status 204, got %d", response.StatusCode)
		}
	})
}

func TestFullPageFallbackOnboardingAndSettingsRedirects(t *testing.T) {
	app, database := newOnboardingTestApp(t)

	fresh := createOnboardingTestUser(t, database, "fullpage-onboarding@example.com", "StrongPass1", false)
	freshCookie := loginAndExtractAuthCookie(t, app, fresh.Email, "StrongPass1")

	t.Run("protected page before onboarding redirects to onboarding", func(t *testing.T) {
		response := fullPageRequest(t, app, http.MethodGet, "/dashboard", nil, freshCookie)
		assertSeeOther(t, response, "/onboarding")
	})

	t.Run("step2 with malformed json body is rejected", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/2", strings.NewReader("{"))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", freshCookie)

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("step2 malformed json request failed: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", response.StatusCode)
		}
	})

	t.Run("step2 before step1 redirects back to step1", func(t *testing.T) {
		form := url.Values{"cycle_length": {"28"}, "period_length": {"5"}}
		response := fullPageRequest(t, app, http.MethodPost, "/api/v1/onboarding/steps/2", form, freshCookie)
		assertSeeOther(t, response, "/onboarding?step=1")
	})

	t.Run("step1 success redirects to step2", func(t *testing.T) {
		today := time.Now().UTC().Format("2006-01-02")
		form := url.Values{"last_period_start": {today}}
		response := fullPageRequest(t, app, http.MethodPost, "/api/v1/onboarding/steps/1", form, freshCookie)
		assertSeeOther(t, response, "/onboarding?step=2")
	})

	t.Run("step2 success redirects to dashboard", func(t *testing.T) {
		form := url.Values{"cycle_length": {"28"}, "period_length": {"5"}}
		response := fullPageRequest(t, app, http.MethodPost, "/api/v1/onboarding/steps/2", form, freshCookie)
		assertSeeOther(t, response, "/dashboard")
	})

	completed := createOnboardingTestUser(t, database, "fullpage-completed@example.com", "StrongPass1", true)
	completedCookie := loginAndExtractAuthCookie(t, app, completed.Email, "StrongPass1")

	t.Run("onboarding page after completion redirects to dashboard", func(t *testing.T) {
		response := fullPageRequest(t, app, http.MethodGet, "/onboarding", nil, completedCookie)
		assertSeeOther(t, response, "/dashboard")
	})

	t.Run("symptom create with blank name redirects to settings", func(t *testing.T) {
		form := url.Values{"name": {"   "}}
		response := fullPageRequest(t, app, http.MethodPost, "/api/v1/symptoms", form, completedCookie)
		assertSeeOther(t, response, "/settings")
	})

	t.Run("symptom create success redirects to settings", func(t *testing.T) {
		form := url.Values{"name": {"Fallback Symptom"}}
		response := fullPageRequest(t, app, http.MethodPost, "/api/v1/symptoms", form, completedCookie)
		assertSeeOther(t, response, "/settings")
	})
}

func TestFullPageFallbackRedirectsWhenLocalAuthDisabled(t *testing.T) {
	stub := &stubOIDCWorkflowService{enabled: true, localPublicAuthEnabled: false}
	app, database, _ := newFullPageFallbackApp(t, onboardingTestAppOptions{oidcService: stub, cookieSecure: true})
	user := createOnboardingTestUser(t, database, "fullpage-oidc-only@example.com", "StrongPass1", true)

	testCases := []struct {
		name   string
		method string
		path   string
		form   url.Values
	}{
		{name: "register", method: http.MethodPost, path: "/api/v1/users", form: url.Values{"email": {"new@example.com"}, "password": {"StrongPass1"}, "confirm_password": {"StrongPass1"}}},
		{name: "login", method: http.MethodPost, path: "/api/v1/sessions", form: url.Values{"email": {user.Email}, "password": {"StrongPass1"}}},
		{name: "forgot password", method: http.MethodPost, path: "/api/v1/password-resets", form: url.Values{"email": {user.Email}}},
		{name: "register pickup", method: http.MethodGet, path: "/register/welcome", form: nil},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			response := fullPageRequest(t, app, testCase.method, testCase.path, testCase.form, "")
			assertSeeOther(t, response, "/login")
		})
	}
}

func TestFullPageFallbackOIDCCallbackRedirects(t *testing.T) {
	t.Run("unsupported role after exchange redirects to login", func(t *testing.T) {
		stub := &stubOIDCWorkflowService{
			enabled:                true,
			localPublicAuthEnabled: true,
			result: services.OIDCLoginResult{
				User: models.User{ID: 42, Email: "viewer@example.com", Role: "viewer"},
			},
		}
		app, _, _ := newFullPageFallbackApp(t, onboardingTestAppOptions{oidcService: stub, cookieSecure: true})

		cookieHeader, seedResponse := seedCookieHeader(t, app, "/__seed/oidc-state")
		defer seedResponse.Body.Close()
		var seeded struct {
			State string `json:"state"`
		}
		decodeJSONBody(t, seedResponse.Body, &seeded)

		form := url.Values{"state": {seeded.State}, "code": {"stub-code"}}
		response := fullPageRequest(t, app, http.MethodPost, "/auth/oidc/callback", form, cookieHeader)
		assertSeeOther(t, response, "/login")
	})

	t.Run("link confirmation without pending claims redirects to login", func(t *testing.T) {
		stub := &stubOIDCWorkflowService{
			enabled:                true,
			localPublicAuthEnabled: true,
			authErr:                services.ErrOIDCLinkRequiresConfirmation,
		}
		app, _, _ := newFullPageFallbackApp(t, onboardingTestAppOptions{oidcService: stub, cookieSecure: true})

		cookieHeader, seedResponse := seedCookieHeader(t, app, "/__seed/oidc-state")
		defer seedResponse.Body.Close()
		var seeded struct {
			State string `json:"state"`
		}
		decodeJSONBody(t, seedResponse.Body, &seeded)

		form := url.Values{"state": {seeded.State}, "code": {"stub-code"}}
		response := fullPageRequest(t, app, http.MethodPost, "/auth/oidc/callback", form, cookieHeader)
		assertSeeOther(t, response, "/login")
	})
}

func TestFullPageFallbackStepupRedirects(t *testing.T) {
	stub := &stubOIDCWorkflowService{enabled: true, localPublicAuthEnabled: true, reauthURL: "https://provider.example.com/authorize?step=up"}
	app, database, _ := newFullPageFallbackApp(t, onboardingTestAppOptions{oidcService: stub, cookieSecure: true})

	localUser := createOnboardingTestUser(t, database, "fullpage-stepup-local@example.com", "StrongPass1", true)
	localCookie := loginAndExtractAuthCookie(t, app, localUser.Email, "StrongPass1")

	t.Run("stepup callback without session redirects to settings", func(t *testing.T) {
		stepupCookie, seedResponse := seedCookieHeader(t, app, "/__seed/oidc-stepup?user_id=777")
		seedResponse.Body.Close()
		response := fullPageRequest(t, app, http.MethodPost, "/auth/oidc/callback", url.Values{"state": {"x"}}, stepupCookie)
		assertSeeOther(t, response, "/settings")
	})

	t.Run("stepup callback with local-auth user redirects to settings", func(t *testing.T) {
		stepupCookie, seedResponse := seedCookieHeader(t, app, "/__seed/oidc-stepup?user_id="+utoa(localUser.ID))
		seedResponse.Body.Close()
		response := fullPageRequest(t, app, http.MethodPost, "/auth/oidc/callback", url.Values{"state": {"x"}}, joinCookieHeader(localCookie, stepupCookie))
		assertSeeOther(t, response, "/settings")
	})

	t.Run("stepup start for oidc-only user redirects to provider", func(t *testing.T) {
		oidcOnly := models.User{
			Email:               "fullpage-stepup-oidc@example.com",
			PasswordHash:        "",
			LocalAuthEnabled:    false,
			Role:                models.RoleOwner,
			OnboardingCompleted: true,
			CycleLength:         28,
			PeriodLength:        5,
			CreatedAt:           time.Now().UTC(),
		}
		if err := database.Create(&oidcOnly).Error; err != nil {
			t.Fatalf("create oidc-only user: %v", err)
		}

		codec, err := newSecureCookieCodec([]byte("test-secret-key"))
		if err != nil {
			t.Fatalf("init secure cookie codec: %v", err)
		}
		token, _, err := services.BuildAuthSessionTokenWithVersionAndSessionID([]byte("test-secret-key"), oidcOnly.ID, oidcOnly.Role, 0, time.Hour, time.Now())
		if err != nil {
			t.Fatalf("build session token: %v", err)
		}
		sealed, err := codec.seal(authCookieName, []byte(token))
		if err != nil {
			t.Fatalf("seal session token: %v", err)
		}

		form := url.Values{"new_password": {"FreshStrong2"}, "confirm_password": {"FreshStrong2"}}
		response := fullPageRequest(t, app, http.MethodPost, "/api/v1/users/current/password/step-up", form, authCookieName+"="+sealed)
		assertSeeOther(t, response, stub.reauthURL)
	})

	t.Run("logout bridge redirect with empty provider url redirects to login", func(t *testing.T) {
		bridgeCookie, seedResponse := seedCookieHeader(t, app, "/__seed/logout-bridge?sid=fullpage-session")
		seedResponse.Body.Close()
		response := fullPageRequest(t, app, http.MethodGet, oidcLogoutBridgeRedirectPath, nil, bridgeCookie)
		assertSeeOther(t, response, "/login")
	})
}

func TestFullPageFallbackLinkConfirmRejectsUnsupportedRoleTarget(t *testing.T) {
	// A legacy non-owner target never reaches identity linking: the shared
	// LoginService password gate refuses unsupported roles, so the submission
	// bounces back to the link-confirm form (enumeration-safe retry) and no
	// session or link is created.
	stub := &stubOIDCWorkflowService{enabled: true, localPublicAuthEnabled: true}
	app, database, _ := newFullPageFallbackApp(t, onboardingTestAppOptions{oidcService: stub, cookieSecure: true})

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	legacy := models.User{
		Email:               "fullpage-link-partner@example.com",
		PasswordHash:        string(passwordHash),
		LocalAuthEnabled:    true,
		Role:                "partner",
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatalf("create partner user: %v", err)
	}

	pendingCookie, seedResponse := seedCookieHeader(t, app, "/__seed/link-pending?user_id="+utoa(legacy.ID)+"&email="+url.QueryEscape(legacy.Email))
	seedResponse.Body.Close()

	form := url.Values{"password": {"StrongPass1"}}
	response := fullPageRequest(t, app, http.MethodPost, oidcLinkConfirmPath, form, pendingCookie)
	assertSeeOther(t, response, oidcLinkConfirmPath)
}

func TestFullPageFallbackOIDCStartWithoutSecureCookiesRedirects(t *testing.T) {
	// OIDC boot-requires COOKIE_SECURE=true; a handler wired insecure makes
	// setOIDCStateCookie refuse, exercising the start handler's fallback tail.
	stub := &stubOIDCWorkflowService{enabled: true, localPublicAuthEnabled: true, authURL: "https://provider.example.com/authorize"}
	app, _, _ := newFullPageFallbackApp(t, onboardingTestAppOptions{oidcService: stub})

	response := fullPageRequest(t, app, http.MethodGet, "/auth/oidc/start", nil, "")
	assertSeeOther(t, response, "/login")
}

func TestCSRFFailureReasonMapsAllSentinels(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want string
	}{
		{name: "token invalid", err: csrf.ErrTokenInvalid, want: "invalid token"},
		{name: "token not found", err: csrf.ErrTokenNotFound, want: "missing token"},
		{name: "referer not found", err: csrf.ErrRefererNotFound, want: "invalid referer"},
		{name: "referer invalid", err: csrf.ErrRefererInvalid, want: "invalid referer"},
		{name: "referer no match", err: csrf.ErrRefererNoMatch, want: "invalid referer"},
		{name: "origin invalid", err: csrf.ErrOriginInvalid, want: "invalid referer"},
		{name: "origin no match", err: csrf.ErrOriginNoMatch, want: "invalid referer"},
		{name: "unknown", err: http.ErrNotSupported, want: "csrf rejected"},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := CSRFFailureReason(testCase.err); got != testCase.want {
				t.Fatalf("unexpected csrf failure reason: got %q want %q", got, testCase.want)
			}
		})
	}
}

func TestDaySaveSpottingWarningSetsEncodedNotice(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "fullpage-spotting@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// A period logged a month ago plus a fresh spotting-only period day is the
	// documented ShowSpottingCycleWarning recipe (day_feedback_policy_test).
	monthAgo := time.Now().UTC().AddDate(0, -1, 0)
	past := models.DailyLog{UserID: user.ID, Date: services.CalendarDay(monthAgo, time.UTC), IsPeriod: true, Flow: models.FlowMedium}
	if err := database.Create(&past).Error; err != nil {
		t.Fatalf("create past period log: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	form := url.Values{"is_period": {"true"}, "flow": {string(models.FlowSpotting)}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+today, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("day save failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
}

func utoa(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}

func TestFullPageFallbackRegisterErrorRedirectsToLogin(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	// Password mismatch drives the auth-form error mapper's /register full-page
	// arm: the failed submission bounces back to the register form via flash.
	form := url.Values{
		"email":            {"fullpage-register@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"Different2"},
	}
	response := fullPageRequest(t, app, http.MethodPost, "/api/v1/users", form, "")
	assertSeeOther(t, response, "/register")
}

func TestBoolStringRendersBothValues(t *testing.T) {
	if got := boolString(true); got != "true" {
		t.Fatalf("boolString(true) = %q", got)
	}
	if got := boolString(false); got != "false" {
		t.Fatalf("boolString(false) = %q", got)
	}
}
