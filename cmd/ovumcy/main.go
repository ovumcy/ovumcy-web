package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/terraincognita07/ovumcy/internal/api"
	"github.com/terraincognita07/ovumcy/internal/cli"
	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/i18n"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func main() {
	handled, err := tryRunCLICommand()
	if err != nil {
		log.Fatal(err)
	}
	if handled {
		return
	}

	location := mustLoadLocation(getEnv("TZ", "Local"))
	time.Local = location

	secretKey, err := resolveSecretKey()
	if err != nil {
		log.Fatalf("invalid SECRET_KEY: %v", err)
	}
	dbPath := getEnv("DB_PATH", filepath.Join("data", "ovumcy.db"))
	port, err := resolvePort()
	if err != nil {
		log.Fatalf("invalid PORT: %v", err)
	}
	defaultLanguage := getEnv("DEFAULT_LANGUAGE", "ru")
	cookieSecure := getEnvBool("COOKIE_SECURE", false)

	loginLimitMax := getEnvInt("RATE_LIMIT_LOGIN_MAX", 8)
	loginLimitWindow := getEnvDuration("RATE_LIMIT_LOGIN_WINDOW", 15*time.Minute)
	forgotLimitMax := getEnvInt("RATE_LIMIT_FORGOT_PASSWORD_MAX", 8)
	forgotLimitWindow := getEnvDuration("RATE_LIMIT_FORGOT_PASSWORD_WINDOW", time.Hour)
	apiLimitMax := getEnvInt("RATE_LIMIT_API_MAX", 300)
	apiLimitWindow := getEnvDuration("RATE_LIMIT_API_WINDOW", time.Minute)

	trustProxyEnabled := getEnvBool("TRUST_PROXY_ENABLED", false)
	proxyHeader := strings.TrimSpace(getEnv("PROXY_HEADER", fiber.HeaderXForwardedFor))
	trustedProxies := parseCSV(getEnv("TRUSTED_PROXIES", "127.0.0.1,::1"))
	if trustProxyEnabled {
		if proxyHeader == "" {
			proxyHeader = fiber.HeaderXForwardedFor
		}
		if len(trustedProxies) == 0 {
			log.Fatal("TRUST_PROXY_ENABLED=true requires at least one TRUSTED_PROXIES entry")
		}
	}

	database, err := db.OpenSQLite(dbPath)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}

	i18nManager, err := i18n.NewManager(defaultLanguage, filepath.Join("internal", "i18n", "locales"))
	if err != nil {
		log.Fatalf("i18n init failed: %v", err)
	}

	repositories := db.NewRepositories(database)
	authService := services.NewAuthService(repositories.Users)
	attemptLimiter := services.NewAttemptLimiter()
	passwordResetService := services.NewPasswordResetService(authService, attemptLimiter)
	loginService := services.NewLoginService(authService, passwordResetService)
	dayService := services.NewDayService(repositories.DailyLogs, repositories.Users)
	symptomService := services.NewSymptomService(repositories.Symptoms, repositories.DailyLogs)
	registrationService := services.NewRegistrationService(authService, repositories.Users)
	viewerService := services.NewViewerService(dayService, symptomService)
	statsService := services.NewStatsService(dayService, symptomService)
	calendarViewService := services.NewCalendarViewService(dayService, statsService)
	dashboardViewService := services.NewDashboardViewService(statsService, viewerService, dayService)
	exportService := services.NewExportService(dayService, symptomService)
	settingsService := services.NewSettingsService(repositories.Users)
	notificationService := services.NewNotificationService()
	settingsViewService := services.NewSettingsViewService(settingsService, notificationService, exportService)
	onboardingService := services.NewOnboardingService(repositories.Users)
	setupService := services.NewSetupService(repositories.Users)

	dependencies := api.Dependencies{
		AuthService:          authService,
		RegistrationService:  registrationService,
		PasswordResetService: passwordResetService,
		LoginService:         loginService,
		DayService:           dayService,
		SymptomService:       symptomService,
		ViewerService:        viewerService,
		StatsService:         statsService,
		CalendarViewService:  calendarViewService,
		DashboardViewService: dashboardViewService,
		ExportService:        exportService,
		SettingsService:      settingsService,
		SettingsViewService:  settingsViewService,
		OnboardingService:    onboardingService,
		SetupService:         setupService,
	}

	handler, err := api.NewHandler(secretKey, filepath.Join("internal", "templates"), location, i18nManager, cookieSecure, dependencies)
	if err != nil {
		log.Fatalf("handler init failed: %v", err)
	}

	appConfig := fiber.Config{
		AppName:               "Ovumcy",
		DisableStartupMessage: true,
	}
	if trustProxyEnabled {
		appConfig.ProxyHeader = proxyHeader
		appConfig.EnableTrustedProxyCheck = true
		appConfig.EnableIPValidation = true
		appConfig.TrustedProxies = trustedProxies
	}
	app := fiber.New(appConfig)

	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(compress.New())
	app.Use("/api/auth/login", limiter.New(limiter.Config{
		Max:        loginLimitMax,
		Expiration: loginLimitWindow,
		LimitReached: newAuthRateLimitHandler(i18nManager, authRateLimitConfig{
			RedirectPath: "/login",
			ErrorCode:    "too_many_login_attempts",
			MessageKey:   "auth.error.too_many_login_attempts",
		}, cookieSecure),
	}))
	app.Use("/api/auth/forgot-password", limiter.New(limiter.Config{
		Max:        forgotLimitMax,
		Expiration: forgotLimitWindow,
		LimitReached: newAuthRateLimitHandler(i18nManager, authRateLimitConfig{
			RedirectPath: "/forgot-password",
			ErrorCode:    "too_many_forgot_password_attempts",
			MessageKey:   "auth.error.too_many_forgot_password_attempts",
		}, cookieSecure),
	}))
	app.Use("/api", limiter.New(limiter.Config{
		Max:          apiLimitMax,
		Expiration:   apiLimitWindow,
		LimitReached: newAPIRateLimitHandler(i18nManager),
	}))
	app.Use(handler.LanguageMiddleware)
	app.Use(csrf.New(csrfMiddlewareConfig(cookieSecure)))

	app.Static("/static", filepath.Join("web", "static"))
	api.RegisterRoutes(app, handler)
	app.Use(handler.NotFound)

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	go func() {
		<-sigCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			log.Printf("server shutdown failed: %v", err)
		}
	}()

	log.Printf(
		"Ovumcy listening on http://0.0.0.0:%s (rev: %s, tz: %s, rate_limits: login=%d/%s forgot=%d/%s api=%d/%s, trusted_proxy=%t)",
		port,
		buildRevision(),
		location.String(),
		loginLimitMax,
		loginLimitWindow,
		forgotLimitMax,
		forgotLimitWindow,
		apiLimitMax,
		apiLimitWindow,
		trustProxyEnabled,
	)
	if trustProxyEnabled {
		log.Printf("trusted proxy config: header=%s trusted_proxy_count=%d", proxyHeader, len(trustedProxies))
	}
	if err := app.Listen(":" + port); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "unknown"
	}

	revision := "unknown"
	modified := "false"
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if strings.TrimSpace(setting.Value) != "" {
				revision = setting.Value
			}
		case "vcs.modified":
			modified = strings.TrimSpace(setting.Value)
		}
	}

	if modified == "true" {
		return revision + "-dirty"
	}
	return revision
}

func tryRunCLICommand() (bool, error) {
	if len(os.Args) < 2 {
		return false, nil
	}

	command := strings.TrimSpace(os.Args[1])
	switch command {
	case "reset-password":
		if len(os.Args) != 3 {
			return true, fmt.Errorf("usage: ovumcy reset-password <email>")
		}
		dbPath := getEnv("DB_PATH", filepath.Join("data", "ovumcy.db"))
		email := strings.TrimSpace(os.Args[2])
		return true, cli.RunResetPasswordCommand(dbPath, email)
	default:
		return false, nil
	}
}

func mustLoadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		log.Printf("invalid TZ %q, falling back to UTC", name)
		return time.UTC
	}
	return location
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func resolveSecretKey() (string, error) {
	secret := strings.TrimSpace(os.Getenv("SECRET_KEY"))
	if secret == "" {
		return "", fmt.Errorf("SECRET_KEY is required")
	}

	lower := strings.ToLower(secret)
	switch lower {
	case "change_me_in_production", "replace_with_at_least_32_random_characters", "replace_me", "changeme":
		return "", fmt.Errorf("SECRET_KEY cannot use placeholder value %q", secret)
	}
	if len(secret) < 32 {
		return "", fmt.Errorf("SECRET_KEY must be at least 32 characters")
	}
	return secret, nil
}

func resolvePort() (string, error) {
	raw := strings.TrimSpace(getEnv("PORT", "8080"))
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("PORT must be a number between 1 and 65535")
	}
	return strconv.Itoa(port), nil
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		log.Printf("invalid %s=%q, using fallback %d", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < time.Second {
		log.Printf("invalid %s=%q, using fallback %s", key, value, fallback)
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Printf("invalid %s=%q, using fallback %t", key, value, fallback)
		return fallback
	}
}

func parseCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func csrfMiddlewareConfig(cookieSecure bool) csrf.Config {
	return csrf.Config{
		KeyLookup:      "form:csrf_token",
		CookieName:     "ovumcy_csrf",
		CookieSameSite: "Lax",
		CookieHTTPOnly: true,
		CookieSecure:   cookieSecure,
		ContextKey:     "csrf",
	}
}

type authRateLimitConfig struct {
	RedirectPath string
	ErrorCode    string
	MessageKey   string
}

func newAuthRateLimitHandler(i18nManager *i18n.Manager, config authRateLimitConfig, cookieSecure bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logRateLimitHit(c)

		language := limiterLanguage(c, i18nManager)
		message := i18nManager.Translate(language, config.MessageKey)
		if message == config.MessageKey {
			message = "Too many requests. Please wait and try again."
		}

		if isHTMXRequest(c) {
			return c.Status(fiber.StatusTooManyRequests).SendString(
				fmt.Sprintf("<div class=\"status-error\">%s</div>", template.HTMLEscapeString(message)),
			)
		}

		if acceptsJSONRequest(c) {
			payload := fiber.Map{"error": message}
			if retryAfter := retryAfterSeconds(c); retryAfter > 0 {
				payload["retry_after_seconds"] = retryAfter
			}
			return c.Status(fiber.StatusTooManyRequests).JSON(payload)
		}

		return redirectWithErrorCode(c, config.RedirectPath, config.ErrorCode, cookieSecure)
	}
}

func newAPIRateLimitHandler(i18nManager *i18n.Manager) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logRateLimitHit(c)

		language := limiterLanguage(c, i18nManager)
		title := i18nManager.Translate(language, "rate_limit.title")
		if title == "rate_limit.title" {
			title = "Too Many Requests"
		}

		message := i18nManager.Translate(language, "common.error.too_many_requests")
		if message == "common.error.too_many_requests" {
			message = "Too many requests. Please wait and try again."
		}

		if isHTMXRequest(c) {
			return c.Status(fiber.StatusTooManyRequests).SendString(
				fmt.Sprintf("<div class=\"status-error\">%s</div>", template.HTMLEscapeString(message)),
			)
		}

		if acceptsJSONRequest(c) {
			payload := fiber.Map{"error": message}
			if retryAfter := retryAfterSeconds(c); retryAfter > 0 {
				payload["retry_after_seconds"] = retryAfter
			}
			return c.Status(fiber.StatusTooManyRequests).JSON(payload)
		}

		return c.
			Status(fiber.StatusTooManyRequests).
			Type("html", "utf-8").
			SendString(fmt.Sprintf(
				"<!doctype html><html lang=\"%s\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>%s</title></head><body style=\"font-family: sans-serif; background: #fff9f0; color: #5a4a3a; margin: 0; display: grid; place-items: center; min-height: 100vh;\"><main style=\"max-width: 32rem; width: 100%%; padding: 1.5rem;\"><h1 style=\"margin: 0 0 0.75rem;\">%s</h1><p style=\"margin: 0 0 1rem;\">%s</p><p style=\"margin: 0;\"><a href=\"/login\">%s</a></p></main></body></html>",
				template.HTMLEscapeString(language),
				template.HTMLEscapeString(title),
				template.HTMLEscapeString(title),
				template.HTMLEscapeString(message),
				template.HTMLEscapeString(i18nManager.Translate(language, "auth.back_to_login")),
			))
	}
}

func logRateLimitHit(c *fiber.Ctx) {
	ip := strings.TrimSpace(c.IP())
	if ip == "" {
		ip = "unknown"
	}

	retryAfter := strings.TrimSpace(string(c.Response().Header.Peek(fiber.HeaderRetryAfter)))
	if retryAfter == "" {
		retryAfter = "unknown"
	}

	log.Printf("rate limit reached: method=%s path=%s ip=%s retry_after=%s", c.Method(), c.Path(), ip, retryAfter)
}

func limiterLanguage(c *fiber.Ctx, i18nManager *i18n.Manager) string {
	language := strings.TrimSpace(c.Cookies("ovumcy_lang"))
	if language != "" {
		return i18nManager.NormalizeLanguage(language)
	}
	return i18nManager.DetectFromAcceptLanguage(c.Get("Accept-Language"))
}

func isHTMXRequest(c *fiber.Ctx) bool {
	return strings.EqualFold(c.Get("HX-Request"), "true")
}

func acceptsJSONRequest(c *fiber.Ctx) bool {
	accept := strings.ToLower(c.Get("Accept"))
	contentType := strings.ToLower(c.Get(fiber.HeaderContentType))
	return strings.Contains(accept, fiber.MIMEApplicationJSON) || strings.Contains(contentType, fiber.MIMEApplicationJSON)
}

func retryAfterSeconds(c *fiber.Ctx) int {
	value := strings.TrimSpace(string(c.Response().Header.Peek(fiber.HeaderRetryAfter)))
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 1 {
		return 0
	}
	return seconds
}

func redirectWithErrorCode(c *fiber.Ctx, path string, errorCode string, cookieSecure bool) error {
	if strings.TrimSpace(path) == "" {
		path = "/login"
	}
	flash := api.FlashPayload{AuthError: errorCode}
	if path == "/login" {
		flash.LoginEmail = strings.TrimSpace(c.FormValue("email"))
	}
	api.SetFlashCookieWithSecure(c, flash, cookieSecure)
	return c.Redirect(path, fiber.StatusSeeOther)
}
