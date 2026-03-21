package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
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
	"gorm.io/gorm"
)

type runtimeConfig struct {
	Location         *time.Location
	SecretKey        string
	DatabaseConfig   db.Config
	Port             string
	DefaultLanguage  string
	RegistrationMode services.RegistrationMode
	CookieSecure     bool
	RateLimits       rateLimitSettings
	Proxy            proxySettings
}

type rateLimitSettings struct {
	LoginMax             int
	LoginWindow          time.Duration
	ForgotPasswordMax    int
	ForgotPasswordWindow time.Duration
	APIMax               int
	APIWindow            time.Duration
}

type proxySettings struct {
	Enabled        bool
	Header         string
	TrustedProxies []string
}

const (
	headerXContentTypeOptions     = "X-Content-Type-Options"
	headerReferrerPolicy          = "Referrer-Policy"
	headerPermissionsPolicy       = "Permissions-Policy"
	headerXFrameOptions           = "X-Frame-Options"
	headerContentSecurityPolicy   = "Content-Security-Policy"
	headerStrictTransportSecurity = "Strict-Transport-Security"

	xContentTypeOptionsNoSniff     = "nosniff"
	referrerPolicyStrictOrigin     = "strict-origin-when-cross-origin"
	permissionsPolicyDefault       = "geolocation=(), camera=(), microphone=()"
	xFrameOptionsDeny              = "DENY"
	contentSecurityPolicyDefault   = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; worker-src 'none'"
	strictTransportSecurityDefault = "max-age=31536000"
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

	config := mustLoadRuntimeConfig(location)
	database := mustOpenDatabase(config.DatabaseConfig)
	i18nManager := mustNewI18nManager(config.DefaultLanguage)
	dependencies := buildDependencies(db.NewRepositories(database), i18nManager, config.RateLimits, config.RegistrationMode)
	handler := mustNewHandler(config, i18nManager, dependencies)
	app := newFiberApp(config, handler)
	stopSignals := installGracefulShutdown(app)
	defer stopSignals()

	logStartup(config)
	if err := app.Listen(":" + config.Port); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

func mustLoadRuntimeConfig(location *time.Location) runtimeConfig {
	config, err := loadRuntimeConfig(location)
	if err != nil {
		log.Fatal(err)
	}
	return config
}

func loadRuntimeConfig(location *time.Location) (runtimeConfig, error) {
	secretKey, err := resolveSecretKey()
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("invalid SECRET_KEY: %w", err)
	}

	databaseConfig, err := resolveDatabaseConfig()
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("invalid database config: %w", err)
	}

	port, err := resolvePort()
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("invalid PORT: %w", err)
	}

	proxy, err := resolveProxySettings()
	if err != nil {
		return runtimeConfig{}, err
	}

	registrationMode, err := resolveRegistrationMode()
	if err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		Location:         location,
		SecretKey:        secretKey,
		DatabaseConfig:   databaseConfig,
		Port:             port,
		DefaultLanguage:  getEnv("DEFAULT_LANGUAGE", "en"),
		RegistrationMode: registrationMode,
		CookieSecure:     getEnvBool("COOKIE_SECURE", false),
		RateLimits: rateLimitSettings{
			LoginMax:             getEnvInt("RATE_LIMIT_LOGIN_MAX", 8),
			LoginWindow:          getEnvDuration("RATE_LIMIT_LOGIN_WINDOW", 15*time.Minute),
			ForgotPasswordMax:    getEnvInt("RATE_LIMIT_FORGOT_PASSWORD_MAX", 8),
			ForgotPasswordWindow: getEnvDuration("RATE_LIMIT_FORGOT_PASSWORD_WINDOW", time.Hour),
			APIMax:               getEnvInt("RATE_LIMIT_API_MAX", 300),
			APIWindow:            getEnvDuration("RATE_LIMIT_API_WINDOW", time.Minute),
		},
		Proxy: proxy,
	}, nil
}

func resolveRegistrationMode() (services.RegistrationMode, error) {
	mode, err := services.ParseRegistrationMode(getEnv("REGISTRATION_MODE", string(services.RegistrationModeOpen)))
	if err != nil {
		return "", err
	}
	return mode, nil
}

func resolveProxySettings() (proxySettings, error) {
	settings := proxySettings{
		Enabled:        getEnvBool("TRUST_PROXY_ENABLED", false),
		Header:         strings.TrimSpace(getEnv("PROXY_HEADER", fiber.HeaderXForwardedFor)),
		TrustedProxies: parseCSV(getEnv("TRUSTED_PROXIES", "127.0.0.1,::1")),
	}

	if !settings.Enabled {
		return settings, nil
	}
	if settings.Header == "" {
		settings.Header = fiber.HeaderXForwardedFor
	}
	if len(settings.TrustedProxies) == 0 {
		return proxySettings{}, fmt.Errorf("TRUST_PROXY_ENABLED=true requires at least one TRUSTED_PROXIES entry")
	}
	return settings, nil
}

func mustOpenDatabase(databaseConfig db.Config) *gorm.DB {
	database, err := db.OpenDatabase(databaseConfig)
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	return database
}

func mustNewI18nManager(defaultLanguage string) *i18n.Manager {
	i18nManager, err := i18n.NewManager(defaultLanguage, filepath.Join("internal", "i18n", "locales"))
	if err != nil {
		log.Fatalf("i18n init failed: %v", err)
	}
	return i18nManager
}

func buildDependencies(repositories *db.Repositories, i18nManager *i18n.Manager, rateLimits rateLimitSettings, registrationMode services.RegistrationMode) api.Dependencies {
	authService := services.NewAuthService(repositories.Users)
	attemptLimiter := services.NewAttemptLimiter()
	passwordResetService := services.NewPasswordResetService(authService, attemptLimiter)
	passwordResetService.ConfigureRecoveryAttemptLimits(rateLimits.ForgotPasswordMax, rateLimits.ForgotPasswordWindow)
	loginService := services.NewLoginService(authService, passwordResetService, attemptLimiter)
	loginService.ConfigureAttemptLimits(rateLimits.LoginMax, rateLimits.LoginWindow)
	dayService := services.NewDayService(repositories.DailyLogs, repositories.Users)
	symptomService := services.NewSymptomService(repositories.Symptoms, services.BuiltinSymptomReservedNames(i18nManager)...)
	registrationService := services.NewRegistrationService(authService, repositories.Users, registrationMode)
	viewerService := services.NewViewerService(dayService, symptomService)
	statsService := services.NewStatsService(dayService, symptomService)
	calendarViewService := services.NewCalendarViewService(dayService, statsService)
	dashboardViewService := services.NewDashboardViewService(statsService, viewerService, dayService)
	exportService := services.NewExportService(dayService, symptomService)
	settingsService := services.NewSettingsService(repositories.Users)
	notificationService := services.NewNotificationService()

	return api.Dependencies{
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
		SettingsViewService:  services.NewSettingsViewService(settingsService, notificationService, exportService, symptomService),
		OnboardingService:    services.NewOnboardingService(repositories.Users),
		SetupService:         services.NewSetupService(repositories.Users),
	}
}

func mustNewHandler(config runtimeConfig, i18nManager *i18n.Manager, dependencies api.Dependencies) *api.Handler {
	handler, err := api.NewHandler(config.SecretKey, filepath.Join("internal", "templates"), config.Location, i18nManager, config.CookieSecure, dependencies)
	if err != nil {
		log.Fatalf("handler init failed: %v", err)
	}
	return handler
}

func newFiberApp(config runtimeConfig, handler *api.Handler) *fiber.App {
	app := fiber.New(fiberConfig(config.Proxy))
	configureFiberMiddleware(app, config, handler)
	registerStaticContentTypes()
	app.Static("/static", filepath.Join("web", "static"))
	api.RegisterRoutes(app, handler)
	app.Use(handler.NotFound)
	return app
}

func registerStaticContentTypes() {
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		log.Printf("register .webmanifest MIME type: %v", err)
	}
}

func fiberConfig(proxy proxySettings) fiber.Config {
	appConfig := fiber.Config{
		AppName:               "Ovumcy",
		DisableStartupMessage: true,
	}
	if !proxy.Enabled {
		return appConfig
	}
	appConfig.ProxyHeader = proxy.Header
	appConfig.EnableTrustedProxyCheck = true
	appConfig.EnableIPValidation = true
	appConfig.TrustedProxies = proxy.TrustedProxies
	return appConfig
}

func configureFiberMiddleware(app *fiber.App, config runtimeConfig, handler *api.Handler) {
	app.Use(securityHeadersMiddleware(config.CookieSecure))
	app.Use(recover.New())
	app.Use(newRequestLogger(nil))
	app.Use(compress.New())
	app.Use("/api/auth/login", limiter.New(limiter.Config{
		Max:        config.RateLimits.LoginMax,
		Expiration: config.RateLimits.LoginWindow,
		LimitReached: newAuthRateLimitHandler(handler, authRateLimitConfig{
			ErrorCode: "too_many_login_attempts",
		}),
	}))
	app.Use("/api/auth/forgot-password", limiter.New(limiter.Config{
		Max:        config.RateLimits.ForgotPasswordMax,
		Expiration: config.RateLimits.ForgotPasswordWindow,
		LimitReached: newAuthRateLimitHandler(handler, authRateLimitConfig{
			ErrorCode: "too_many_forgot_password_attempts",
		}),
	}))
	app.Use("/api", limiter.New(limiter.Config{
		Max:          config.RateLimits.APIMax,
		Expiration:   config.RateLimits.APIWindow,
		LimitReached: newAPIRateLimitHandler(handler),
	}))
	app.Use(handler.LanguageMiddleware)
	app.Use(csrf.New(csrfMiddlewareConfig(config.CookieSecure)))
}

const requestLoggerFormat = "${time} | ${status} | ${latency} | ${method} | ${request_path} | ${error}\n"

func newRequestLogger(output io.Writer) fiber.Handler {
	config := logger.Config{
		Format: requestLoggerFormat,
		CustomTags: map[string]logger.LogFunc{
			"request_path": func(buffer logger.Buffer, c *fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				return buffer.WriteString(api.SafeRequestLogPath(c))
			},
		},
	}
	if output != nil {
		config.Output = output
	}
	return logger.New(config)
}

func securityHeadersMiddleware(enableStrictTransportSecurity bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(headerXContentTypeOptions, xContentTypeOptionsNoSniff)
		c.Set(headerReferrerPolicy, referrerPolicyStrictOrigin)
		c.Set(headerPermissionsPolicy, permissionsPolicyDefault)
		c.Set(headerXFrameOptions, xFrameOptionsDeny)
		c.Set(headerContentSecurityPolicy, contentSecurityPolicyDefault)
		if enableStrictTransportSecurity {
			c.Set(headerStrictTransportSecurity, strictTransportSecurityDefault)
		}
		return c.Next()
	}
}

func installGracefulShutdown(app *fiber.App) context.CancelFunc {
	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			log.Printf("server shutdown failed: %v", err)
		}
	}()
	return stopSignals
}

func logStartup(config runtimeConfig) {
	log.Printf(
		"Ovumcy listening on http://0.0.0.0:%s (rev: %s, tz: %s, registration=%s, rate_limits: login=%d/%s api=%d/%s, trusted_proxy=%t)",
		config.Port,
		buildRevision(),
		config.Location.String(),
		config.RegistrationMode,
		config.RateLimits.LoginMax,
		config.RateLimits.LoginWindow,
		config.RateLimits.APIMax,
		config.RateLimits.APIWindow,
		config.Proxy.Enabled,
	)
	if config.Proxy.Enabled {
		log.Printf("trusted proxy config: header=%s trusted_proxy_count=%d", config.Proxy.Header, len(config.Proxy.TrustedProxies))
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
	return tryRunCLICommandWithHandlers(os.Args[1:], cliCommandHandlers{
		runResetPassword: cli.RunResetPasswordCommand,
		runUsers:         cli.RunUsersCommand,
	})
}

type cliCommandHandlers struct {
	runResetPassword func(databaseConfig db.Config, email string) error
	runUsers         func(databaseConfig db.Config, args []string) error
}

func tryRunCLICommandWithHandlers(args []string, handlers cliCommandHandlers) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}

	command := strings.TrimSpace(args[0])
	switch command {
	case "reset-password":
		if len(args) != 2 {
			return true, fmt.Errorf("usage: ovumcy reset-password <email>")
		}
		if handlers.runResetPassword == nil {
			return true, fmt.Errorf("reset-password handler is required")
		}
		databaseConfig, err := resolveDatabaseConfig()
		if err != nil {
			return true, fmt.Errorf("invalid database config: %w", err)
		}
		email := strings.TrimSpace(args[1])
		return true, handlers.runResetPassword(databaseConfig, email)
	case "users":
		if len(args) < 2 {
			return true, fmt.Errorf("usage: ovumcy users <list|delete>")
		}
		if handlers.runUsers == nil {
			return true, fmt.Errorf("users handler is required")
		}
		databaseConfig, err := resolveDatabaseConfig()
		if err != nil {
			return true, fmt.Errorf("invalid database config: %w", err)
		}
		return true, handlers.runUsers(databaseConfig, args[1:])
	default:
		return false, nil
	}
}

func resolveDatabaseConfig() (db.Config, error) {
	driver := db.Driver(strings.ToLower(strings.TrimSpace(getEnv("DB_DRIVER", string(db.DriverSQLite)))))
	config := db.Config{
		Driver:      driver,
		SQLitePath:  getEnv("DB_PATH", filepath.Join("data", "ovumcy.db")),
		PostgresURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),
	}
	if err := config.Validate(); err != nil {
		return db.Config{}, err
	}
	return config, nil
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
		keyFilePath := strings.TrimSpace(os.Getenv("SECRET_KEY_FILE"))
		if keyFilePath != "" {
			content, err := os.ReadFile(keyFilePath)
			if err != nil {
				return "", fmt.Errorf("failed to read SECRET_KEY_FILE: %w", err)
			}
			secret = strings.TrimSpace(string(content))
		}
	}

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
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			api.LogSecurityEvent(c, "csrf", "denied", api.SecurityEventField{
				Key:   "reason",
				Value: csrfFailureReason(err),
			})
			return fiber.ErrForbidden
		},
	}
}

func csrfFailureReason(err error) string {
	switch {
	case errors.Is(err, csrf.ErrTokenInvalid):
		return "invalid token"
	case errors.Is(err, csrf.ErrTokenNotFound):
		return "missing token"
	case errors.Is(err, csrf.ErrNoReferer), errors.Is(err, csrf.ErrBadReferer):
		return "invalid referer"
	default:
		return "csrf rejected"
	}
}

type authRateLimitConfig struct {
	ErrorCode string
}

func newAuthRateLimitHandler(handler *api.Handler, config authRateLimitConfig) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logRateLimitHit(c)
		api.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: "auth"},
			api.SecurityEventField{Key: "reason", Value: config.ErrorCode},
		)
		return handler.RespondAuthRateLimited(c, config.ErrorCode)
	}
}

func newAPIRateLimitHandler(handler *api.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		logRateLimitHit(c)
		api.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: rateLimitScope(c)},
			api.SecurityEventField{Key: "reason", Value: "too many requests"},
		)
		return handler.RespondAPIRateLimited(c)
	}
}

func rateLimitScope(c *fiber.Ctx) string {
	switch {
	case strings.HasPrefix(c.Path(), "/api/settings/"), strings.HasPrefix(c.Path(), "/settings/"):
		return "settings"
	case strings.HasPrefix(c.Path(), "/api/auth/"):
		return "auth"
	default:
		return "api"
	}
}

func logRateLimitHit(c *fiber.Ctx) {
	retryAfter := strings.TrimSpace(string(c.Response().Header.Peek(fiber.HeaderRetryAfter)))
	if retryAfter == "" {
		retryAfter = "unknown"
	}

	log.Printf("rate limit reached: method=%s path=%s retry_after=%s", c.Method(), api.SafeRequestLogPath(c), retryAfter)
}
