package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/terraincognita07/ovumcy/internal/db"
)

func TestResolveSecretKey(t *testing.T) {
	t.Setenv("SECRET_KEY", "")
	if _, err := resolveSecretKey(); err == nil {
		t.Fatal("expected error when SECRET_KEY is empty")
	}

	t.Setenv("SECRET_KEY", "change_me_in_production")
	if _, err := resolveSecretKey(); err == nil {
		t.Fatal("expected error when SECRET_KEY uses insecure placeholder")
	}

	t.Setenv("SECRET_KEY", "replace_with_at_least_32_random_characters")
	if _, err := resolveSecretKey(); err == nil {
		t.Fatal("expected error when SECRET_KEY uses example placeholder")
	}

	t.Setenv("SECRET_KEY", "too-short-secret")
	if _, err := resolveSecretKey(); err == nil {
		t.Fatal("expected error when SECRET_KEY is too short")
	}

	valid := "0123456789abcdef0123456789abcdef"
	t.Setenv("SECRET_KEY", valid)

	secret, err := resolveSecretKey()
	if err != nil {
		t.Fatalf("expected valid secret, got error: %v", err)
	}
	if secret != valid {
		t.Fatalf("expected %q, got %q", valid, secret)
	}
}

func TestResolveDatabaseConfigDefaultsToSQLite(t *testing.T) {
	t.Setenv("DB_DRIVER", "")
	t.Setenv("DB_PATH", "")
	t.Setenv("DATABASE_URL", "")

	config, err := resolveDatabaseConfig()
	if err != nil {
		t.Fatalf("expected default sqlite config, got error: %v", err)
	}
	if config.Driver != "sqlite" {
		t.Fatalf("expected sqlite driver, got %q", config.Driver)
	}
	if config.SQLitePath != "data\\ovumcy.db" && config.SQLitePath != "data/ovumcy.db" {
		t.Fatalf("expected default sqlite path, got %q", config.SQLitePath)
	}
}

func TestResolveDatabaseConfigRequiresDatabaseURLForPostgres(t *testing.T) {
	t.Setenv("DB_DRIVER", "postgres")
	t.Setenv("DATABASE_URL", "")

	if _, err := resolveDatabaseConfig(); err == nil {
		t.Fatal("expected postgres config without DATABASE_URL to fail")
	}
}

func TestResolveDatabaseConfigAcceptsPostgres(t *testing.T) {
	t.Setenv("DB_DRIVER", "postgres")
	t.Setenv("DATABASE_URL", "host=127.0.0.1 port=5432 user=ovumcy password=ovumcy dbname=ovumcy sslmode=disable")

	config, err := resolveDatabaseConfig()
	if err != nil {
		t.Fatalf("expected postgres config, got error: %v", err)
	}
	if config.Driver != "postgres" {
		t.Fatalf("expected postgres driver, got %q", config.Driver)
	}
	if config.PostgresURL == "" {
		t.Fatal("expected postgres url to be preserved")
	}
}

func TestCSRFMiddlewareConfigUsesCookieSecureFlag(t *testing.T) {
	secureConfig := csrfMiddlewareConfig(true)
	if !secureConfig.CookieSecure {
		t.Fatal("expected csrf cookie secure flag to be enabled")
	}
	if !secureConfig.CookieHTTPOnly {
		t.Fatal("expected csrf cookie to be httpOnly")
	}
	if secureConfig.CookieName != "ovumcy_csrf" {
		t.Fatalf("expected csrf cookie name ovumcy_csrf, got %q", secureConfig.CookieName)
	}
	if secureConfig.KeyLookup != "form:csrf_token" {
		t.Fatalf("expected csrf key lookup form:csrf_token, got %q", secureConfig.KeyLookup)
	}

	insecureConfig := csrfMiddlewareConfig(false)
	if insecureConfig.CookieSecure {
		t.Fatal("expected csrf cookie secure flag to be disabled")
	}
}

func TestResolvePort(t *testing.T) {
	t.Setenv("PORT", "")
	port, err := resolvePort()
	if err != nil {
		t.Fatalf("expected default port, got error: %v", err)
	}
	if port != "8080" {
		t.Fatalf("expected default port 8080, got %q", port)
	}

	t.Setenv("PORT", "9090")
	port, err = resolvePort()
	if err != nil {
		t.Fatalf("expected valid port, got error: %v", err)
	}
	if port != "9090" {
		t.Fatalf("expected port 9090, got %q", port)
	}

	t.Setenv("PORT", "0")
	if _, err := resolvePort(); err == nil {
		t.Fatal("expected invalid port 0 to fail")
	}

	t.Setenv("PORT", "70000")
	if _, err := resolvePort(); err == nil {
		t.Fatal("expected invalid high port to fail")
	}

	t.Setenv("PORT", "not-a-number")
	if _, err := resolvePort(); err == nil {
		t.Fatal("expected invalid non-numeric port to fail")
	}
}

func TestResolveProxySettingsDefaultsWhenDisabled(t *testing.T) {
	t.Setenv("TRUST_PROXY_ENABLED", "")
	t.Setenv("PROXY_HEADER", "")
	t.Setenv("TRUSTED_PROXIES", "")

	settings, err := resolveProxySettings()
	if err != nil {
		t.Fatalf("expected disabled proxy settings, got error: %v", err)
	}
	if settings.Enabled {
		t.Fatal("expected proxy settings to be disabled by default")
	}
	if settings.Header != fiber.HeaderXForwardedFor {
		t.Fatalf("expected default proxy header %q, got %q", fiber.HeaderXForwardedFor, settings.Header)
	}
	if len(settings.TrustedProxies) != 2 {
		t.Fatalf("expected default trusted proxies when env is empty and proxy is disabled, got %#v", settings.TrustedProxies)
	}
}

func TestResolveProxySettingsRequiresTrustedProxiesWhenEnabled(t *testing.T) {
	t.Setenv("TRUST_PROXY_ENABLED", "true")
	t.Setenv("PROXY_HEADER", "")
	t.Setenv("TRUSTED_PROXIES", " , ")

	if _, err := resolveProxySettings(); err == nil {
		t.Fatal("expected enabled proxy settings without trusted proxies to fail")
	}
}

func TestLoadRuntimeConfigBuildsExpectedSettings(t *testing.T) {
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", "data\\custom.db")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PORT", "9090")
	t.Setenv("DEFAULT_LANGUAGE", "ru")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("RATE_LIMIT_LOGIN_MAX", "12")
	t.Setenv("RATE_LIMIT_LOGIN_WINDOW", "20m")
	t.Setenv("RATE_LIMIT_FORGOT_PASSWORD_MAX", "9")
	t.Setenv("RATE_LIMIT_FORGOT_PASSWORD_WINDOW", "90m")
	t.Setenv("RATE_LIMIT_API_MAX", "700")
	t.Setenv("RATE_LIMIT_API_WINDOW", "2m")
	t.Setenv("TRUST_PROXY_ENABLED", "true")
	t.Setenv("PROXY_HEADER", "X-Forwarded-For")
	t.Setenv("TRUSTED_PROXIES", "127.0.0.1, ::1")

	location := time.FixedZone("UTC+3", 3*60*60)
	config, err := loadRuntimeConfig(location)
	if err != nil {
		t.Fatalf("expected valid runtime config, got error: %v", err)
	}

	assertBaseRuntimeConfig(t, config, location)
	assertRateLimitRuntimeConfig(t, config)
	assertProxyRuntimeConfig(t, config)
}

func assertBaseRuntimeConfig(t *testing.T, config runtimeConfig, location *time.Location) {
	t.Helper()

	if config.Location != location {
		t.Fatalf("expected runtime location to be preserved")
	}
	if config.SecretKey != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("expected secret key to be preserved, got %q", config.SecretKey)
	}
	if config.DatabaseConfig.Driver != db.DriverSQLite {
		t.Fatalf("expected sqlite driver, got %q", config.DatabaseConfig.Driver)
	}
	if config.DatabaseConfig.SQLitePath != "data\\custom.db" && config.DatabaseConfig.SQLitePath != "data/custom.db" {
		t.Fatalf("expected sqlite path to be preserved, got %q", config.DatabaseConfig.SQLitePath)
	}
	if config.Port != "9090" {
		t.Fatalf("expected port 9090, got %q", config.Port)
	}
	if config.DefaultLanguage != "ru" {
		t.Fatalf("expected default language ru, got %q", config.DefaultLanguage)
	}
	if !config.CookieSecure {
		t.Fatal("expected cookie secure=true")
	}
}

func assertRateLimitRuntimeConfig(t *testing.T, config runtimeConfig) {
	t.Helper()

	if config.RateLimits.LoginMax != 12 || config.RateLimits.LoginWindow != 20*time.Minute {
		t.Fatalf("unexpected login rate limit settings: %+v", config.RateLimits)
	}
	if config.RateLimits.ForgotPasswordMax != 9 || config.RateLimits.ForgotPasswordWindow != 90*time.Minute {
		t.Fatalf("unexpected forgot-password rate limit settings: %+v", config.RateLimits)
	}
	if config.RateLimits.APIMax != 700 || config.RateLimits.APIWindow != 2*time.Minute {
		t.Fatalf("unexpected api rate limit settings: %+v", config.RateLimits)
	}
}

func assertProxyRuntimeConfig(t *testing.T, config runtimeConfig) {
	t.Helper()

	if !config.Proxy.Enabled {
		t.Fatal("expected proxy settings enabled")
	}
	if config.Proxy.Header != "X-Forwarded-For" {
		t.Fatalf("expected explicit proxy header, got %q", config.Proxy.Header)
	}
	if len(config.Proxy.TrustedProxies) != 2 {
		t.Fatalf("expected two trusted proxies, got %#v", config.Proxy.TrustedProxies)
	}
}

func TestFiberConfigAppliesTrustedProxySettings(t *testing.T) {
	config := fiberConfig(proxySettings{
		Enabled:        true,
		Header:         "X-Forwarded-For",
		TrustedProxies: []string{"127.0.0.1", "::1"},
	})

	if config.ProxyHeader != "X-Forwarded-For" {
		t.Fatalf("expected proxy header to be applied, got %q", config.ProxyHeader)
	}
	if !config.EnableTrustedProxyCheck {
		t.Fatal("expected trusted proxy check to be enabled")
	}
	if !config.EnableIPValidation {
		t.Fatal("expected IP validation to be enabled")
	}
	if len(config.TrustedProxies) != 2 {
		t.Fatalf("expected trusted proxies to be applied, got %#v", config.TrustedProxies)
	}
}

func TestSecurityHeadersMiddlewareSetsHeadersOnHTMLResponses(t *testing.T) {
	app := fiber.New()
	app.Use(securityHeadersMiddleware())
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("html request failed: %v", err)
	}
	defer response.Body.Close()

	assertDefaultSecurityHeaders(t, response)
}

func TestSecurityHeadersMiddlewareSetsHeadersOnAPIResponses(t *testing.T) {
	app := fiber.New()
	app.Use(securityHeadersMiddleware())
	app.Get("/api/ping", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	request := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("api request failed: %v", err)
	}
	defer response.Body.Close()

	assertDefaultSecurityHeaders(t, response)
}

func assertDefaultSecurityHeaders(t *testing.T, response *http.Response) {
	t.Helper()

	if value := response.Header.Get(headerXContentTypeOptions); value != xContentTypeOptionsNoSniff {
		t.Fatalf("expected %s=%q, got %q", headerXContentTypeOptions, xContentTypeOptionsNoSniff, value)
	}
	if value := response.Header.Get(headerReferrerPolicy); value != referrerPolicyStrictOrigin {
		t.Fatalf("expected %s=%q, got %q", headerReferrerPolicy, referrerPolicyStrictOrigin, value)
	}
	if value := response.Header.Get(headerPermissionsPolicy); value != permissionsPolicyDefault {
		t.Fatalf("expected %s=%q, got %q", headerPermissionsPolicy, permissionsPolicyDefault, value)
	}
	if value := response.Header.Get(headerXFrameOptions); value != xFrameOptionsDeny {
		t.Fatalf("expected %s=%q, got %q", headerXFrameOptions, xFrameOptionsDeny, value)
	}
	if value := response.Header.Get("Access-Control-Allow-Origin"); value != "" {
		t.Fatalf("did not expect Access-Control-Allow-Origin by default, got %q", value)
	}
}

func TestLogStartupDoesNotLogForgotPasswordRateLimitDetail(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	logStartup(runtimeConfig{
		Location: time.FixedZone("UTC+3", 3*60*60),
		Port:     "9090",
		RateLimits: rateLimitSettings{
			LoginMax:             12,
			LoginWindow:          20 * time.Minute,
			ForgotPasswordMax:    9,
			ForgotPasswordWindow: 90 * time.Minute,
			APIMax:               700,
			APIWindow:            2 * time.Minute,
		},
		Proxy: proxySettings{
			Enabled: false,
		},
	})

	logLine := output.String()
	if strings.Contains(logLine, "forgot=") {
		t.Fatalf("did not expect forgot-password rate limit detail in startup log: %q", logLine)
	}
	if strings.Contains(logLine, "90m0s") {
		t.Fatalf("did not expect forgot-password window in startup log: %q", logLine)
	}
	if !strings.Contains(logLine, "login=12/20m0s") {
		t.Fatalf("expected login rate limit detail in startup log, got %q", logLine)
	}
	if !strings.Contains(logLine, "api=700/2m0s") {
		t.Fatalf("expected api rate limit detail in startup log, got %q", logLine)
	}
}

func testResponseCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestDefaultRequestLoggerDoesNotLogQueryPII(t *testing.T) {
	var output bytes.Buffer
	app := fiber.New()
	app.Use(logger.New(logger.Config{
		Output: &output,
	}))
	app.Get("/reset-password", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusNoContent)
	})

	request := httptest.NewRequest(
		http.MethodGet,
		"/reset-password?token=plain-reset-token&email=user@example.com",
		nil,
	)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, "/reset-password") {
		t.Fatalf("expected request path in logs, got %q", logLine)
	}
	if strings.Contains(logLine, "plain-reset-token") {
		t.Fatalf("did not expect reset token in request logs: %q", logLine)
	}
	if strings.Contains(logLine, "user@example.com") {
		t.Fatalf("did not expect email in request logs: %q", logLine)
	}
}

func TestDefaultRequestLoggerDoesNotLogFormSecrets(t *testing.T) {
	const plaintextPassword = "PlaintextPassword123!"
	const plaintextToken = "plain-reset-token"

	var output bytes.Buffer
	app := fiber.New()
	app.Use(logger.New(logger.Config{
		Output: &output,
	}))
	app.Post("/api/auth/reset-password", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusNoContent)
	})

	form := "password=PlaintextPassword123%21&confirm_password=PlaintextPassword123%21&token=plain-reset-token"
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/reset-password",
		strings.NewReader(form),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, "/api/auth/reset-password") {
		t.Fatalf("expected request path in logs, got %q", logLine)
	}
	if strings.Contains(logLine, plaintextPassword) {
		t.Fatalf("did not expect plaintext password in request logs: %q", logLine)
	}
	if strings.Contains(logLine, plaintextToken) {
		t.Fatalf("did not expect reset token in request logs: %q", logLine)
	}
}

func TestRateLimitLogDoesNotLogQueryPII(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	const plaintextPassword = "PlaintextPassword123!"

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		logRateLimitHit(c)
		return c.SendStatus(http.StatusTooManyRequests)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/forgot-password?token=plain-reset-token&email=user@example.com",
		strings.NewReader("email=user@example.com&password=PlaintextPassword123%21&token=plain-reset-token"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("rate limit request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, "path=/api/auth/forgot-password") {
		t.Fatalf("expected path without query string in rate-limit logs, got %q", logLine)
	}
	if strings.Contains(logLine, "plain-reset-token") {
		t.Fatalf("did not expect reset token in rate-limit logs: %q", logLine)
	}
	if strings.Contains(logLine, "user@example.com") {
		t.Fatalf("did not expect email in rate-limit logs: %q", logLine)
	}
	if strings.Contains(logLine, plaintextPassword) {
		t.Fatalf("did not expect plaintext password in rate-limit logs: %q", logLine)
	}
}
