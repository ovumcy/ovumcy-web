package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func newRateLimitTestI18nManager(t *testing.T) *i18n.Manager {
	t.Helper()

	candidates := []string{
		filepath.Join("..", "..", "internal", "i18n", "locales"),
		filepath.Join("internal", "i18n", "locales"),
	}
	for _, candidate := range candidates {
		manager, err := i18n.NewManager("en", candidate)
		if err == nil {
			return manager
		}
	}
	t.Fatal("failed to initialize i18n manager for rate-limit tests")
	return nil
}

func newRateLimitTestHandler(t *testing.T) *api.Handler {
	t.Helper()

	templateCandidates := []string{
		filepath.Join("..", "..", "internal", "templates"),
		filepath.Join("internal", "templates"),
	}
	templateDir := ""
	for _, candidate := range templateCandidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			templateDir = candidate
			break
		}
	}
	if templateDir == "" {
		t.Fatal("failed to locate templates directory for rate-limit tests")
	}

	tempDB, err := os.CreateTemp("", "ovumcy-rate-limit-*.db")
	if err != nil {
		t.Fatalf("create rate-limit test database path: %v", err)
	}
	if err := tempDB.Close(); err != nil {
		t.Fatalf("close temp database file: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(tempDB.Name())
	})

	database, err := db.OpenDatabase(db.Config{
		Driver:     db.DriverSQLite,
		SQLitePath: tempDB.Name(),
	})
	if err != nil {
		t.Fatalf("open rate-limit test database: %v", err)
	}

	i18nManager := newRateLimitTestI18nManager(t)

	handler, err := api.NewHandler(
		"0123456789abcdef0123456789abcdef",
		templateDir,
		time.UTC,
		i18nManager,
		false,
		buildDependencies(db.NewRepositories(database), i18nManager, rateLimitSettings{
			LoginMax:             8,
			LoginWindow:          15 * time.Minute,
			ForgotPasswordMax:    8,
			ForgotPasswordWindow: time.Hour,
			APIMax:               300,
			APIWindow:            time.Minute,
		}, services.RegistrationModeOpen, security.OIDCConfig{}, "test-secret-key"),
	)
	if err != nil {
		t.Fatalf("init rate-limit test handler: %v", err)
	}
	return handler
}

func TestAuthRateLimitHandlerTreatsJSONContentTypeAsJSONRequest(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Post("/api/auth/login", newAuthRateLimitHandler(handler, authRateLimitConfig{
		ErrorCode: "too_many_login_attempts",
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"rate-limit@example.com"}`))
	request.Header.Set("Content-Type", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("auth rate-limit request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
	if got, ok := payload["error"].(string); !ok || got != "too_many_login_attempts" {
		t.Fatalf("expected stable auth rate-limit key, got %#v", payload)
	}
}

func TestAuthRateLimitHandlerRedirectUsesSealedFlashCookie(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Post("/api/auth/login", newAuthRateLimitHandler(handler, authRateLimitConfig{
		ErrorCode: "too_many_login_attempts",
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("email=rate-limit%40example.com"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("auth rate-limit redirect request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	flashCookie := testResponseCookie(response.Cookies(), "ovumcy_flash")
	if flashCookie == nil {
		t.Fatal("expected flash cookie in redirect response")
	}
	if strings.Contains(flashCookie.Value, "rate-limit@example.com") {
		t.Fatalf("did not expect sealed flash cookie to expose email in plaintext: %q", flashCookie.Value)
	}
}

func TestOIDCRateLimitHandlerRedirectUsesSealedFlashCookie(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get("/auth/oidc/start", newAuthRateLimitHandler(handler, authRateLimitConfig{
		ErrorCode: "too_many_sso_attempts",
	}))

	request := httptest.NewRequest(http.MethodGet, "/auth/oidc/start?error=access_denied", nil)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("oidc rate-limit redirect request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	flashCookie := testResponseCookie(response.Cookies(), "ovumcy_flash")
	if flashCookie == nil {
		t.Fatal("expected flash cookie in redirect response")
	}
	if strings.Contains(flashCookie.Value, "access_denied") {
		t.Fatalf("did not expect sealed flash cookie to expose provider error in plaintext: %q", flashCookie.Value)
	}
}

func TestSettingsAPIRateLimitHandlerRedirectsToSettings(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Post("/api/settings/profile", newAPIRateLimitHandler(handler))

	request := httptest.NewRequest(http.MethodPost, "/api/settings/profile", strings.NewReader("display_name=Owner"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("settings rate-limit request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}
}

func TestAPIRateLimitHandlerReturnsStatusErrorMarkupForHTMX(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Post("/api/stats/overview", newAPIRateLimitHandler(handler))

	request := httptest.NewRequest(http.MethodPost, "/api/stats/overview", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("api rate-limit htmx request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, `class="status-error"`) {
		t.Fatalf("expected status-error markup, got %q", rendered)
	}
	if !strings.Contains(rendered, "Too many requests.") {
		t.Fatalf("expected localized generic rate-limit message, got %q", rendered)
	}
}

func TestAPIRateLimitHandlerReturnsJSONForGenericBrowserRequests(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Post("/api/days/2026-02-17", newAPIRateLimitHandler(handler))

	request := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-17", strings.NewReader("notes=test"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("generic api rate-limit request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode generic rate-limit response: %v", err)
	}
	if got, ok := payload["error"].(string); !ok || got != "too many requests" {
		t.Fatalf("expected generic rate-limit error key, got %#v", payload)
	}
}
