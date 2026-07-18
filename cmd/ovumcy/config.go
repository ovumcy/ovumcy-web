package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

type runtimeConfig struct {
	Location         *time.Location
	SecretKey        string
	DatabaseConfig   db.Config
	Port             string
	DefaultLanguage  string
	RegistrationMode services.RegistrationMode
	CookieSecure     bool
	HSTSEnabled      bool
	OIDC             security.OIDCConfig
	RateLimits       rateLimitSettings
	Proxy            proxySettings
	AuditLogEnabled  bool
	// WebhookBlockPrivate mirrors the off-by-default WEBHOOK_BLOCK_PRIVATE_ADDRESSES
	// egress gate the notify CLI reads, so the built-in scheduler wires the same
	// deliverer hardening.
	WebhookBlockPrivate bool
	ReminderScheduler   reminderSchedulerSettings
}

// reminderSchedulerSettings configures the optional built-in reminder scheduler
// (issue #125). Enabled is DEFAULT FALSE: the always-on outbound component ships
// off and is opted into explicitly via REMINDER_SCHEDULER_ENABLED. Hour is the
// LOCAL hour-of-day (0-23) the daily pass runs at (REMINDER_SCHEDULER_HOUR,
// default 9); the scheduler reuses config.Location, there is no separate
// reminder timezone.
type reminderSchedulerSettings struct {
	Enabled bool
	Hour    int
}

type rateLimitSettings struct {
	LoginMax             int
	LoginWindow          time.Duration
	ForgotPasswordMax    int
	ForgotPasswordWindow time.Duration
	RegisterMax          int
	RegisterWindow       time.Duration
	LogoutMax            int
	LogoutWindow         time.Duration
	APIMax               int
	APIWindow            time.Duration
}

type proxySettings struct {
	Enabled        bool
	Header         string
	TrustedProxies []string
}

// maxSecretKeyFileBytes caps the SECRET_KEY_FILE read (see resolveSecretKey).
const maxSecretKeyFileBytes int64 = 8 << 10

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

	cookieSecure := getEnvBool("COOKIE_SECURE", false)
	// HSTS defaults to COOKIE_SECURE (preserving the historical coupling where
	// enabling secure cookies also pinned HTTPS) but is an independent switch:
	// HSTS_ENABLED=false lets an operator run secure cookies without pinning
	// browsers to HTTPS for a year, and HSTS_ENABLED=true opts in explicitly.
	hstsEnabled := getEnvBool("HSTS_ENABLED", cookieSecure)
	oidcConfig, err := resolveOIDCConfig(cookieSecure, registrationMode)
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
		CookieSecure:     cookieSecure,
		HSTSEnabled:      hstsEnabled,
		OIDC:             oidcConfig,
		RateLimits: rateLimitSettings{
			LoginMax:             getEnvInt("RATE_LIMIT_LOGIN_MAX", 8),
			LoginWindow:          getEnvDuration("RATE_LIMIT_LOGIN_WINDOW", 15*time.Minute),
			RegisterMax:          getEnvInt("RATE_LIMIT_REGISTER_MAX", 8),
			RegisterWindow:       getEnvDuration("RATE_LIMIT_REGISTER_WINDOW", 15*time.Minute),
			ForgotPasswordMax:    getEnvInt("RATE_LIMIT_FORGOT_PASSWORD_MAX", 8),
			ForgotPasswordWindow: getEnvDuration("RATE_LIMIT_FORGOT_PASSWORD_WINDOW", time.Hour),
			LogoutMax:            getEnvInt("RATE_LIMIT_LOGOUT_MAX", 60),
			LogoutWindow:         getEnvDuration("RATE_LIMIT_LOGOUT_WINDOW", 15*time.Minute),
			APIMax:               getEnvInt("RATE_LIMIT_API_MAX", 300),
			APIWindow:            getEnvDuration("RATE_LIMIT_API_WINDOW", time.Minute),
		},
		Proxy:               proxy,
		AuditLogEnabled:     getEnvBool("AUDIT_LOG_ENABLED", false),
		WebhookBlockPrivate: getEnvBool("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", false),
		ReminderScheduler: reminderSchedulerSettings{
			Enabled: getEnvBool("REMINDER_SCHEDULER_ENABLED", false),
			// getEnvInt rejects values <1, so hour 0 (valid) would be lost; use a
			// dedicated range helper that accepts the full 0-23 clock.
			Hour: getEnvIntInRange("REMINDER_SCHEDULER_HOUR", 9, 0, 23),
		},
	}, nil
}

func resolveRegistrationMode() (services.RegistrationMode, error) {
	mode, err := services.ParseRegistrationMode(getEnv("REGISTRATION_MODE", string(services.RegistrationModeOpen)))
	if err != nil {
		return "", err
	}
	return mode, nil
}

func resolveOIDCConfig(cookieSecure bool, registrationMode services.RegistrationMode) (security.OIDCConfig, error) {
	config := security.OIDCConfig{
		Enabled:                     getEnvBool("OIDC_ENABLED", false),
		IssuerURL:                   getEnv("OIDC_ISSUER_URL", ""),
		ClientID:                    getEnv("OIDC_CLIENT_ID", ""),
		ClientSecret:                getEnv("OIDC_CLIENT_SECRET", ""),
		RedirectURL:                 getEnv("OIDC_REDIRECT_URL", ""),
		CAFile:                      getEnv("OIDC_CA_FILE", ""),
		AutoProvision:               getEnvBool("OIDC_AUTO_PROVISION", false),
		LoginMode:                   security.OIDCLoginMode(getEnv("OIDC_LOGIN_MODE", string(security.OIDCLoginModeHybrid))),
		LogoutMode:                  security.OIDCLogoutMode(getEnv("OIDC_LOGOUT_MODE", string(security.OIDCLogoutModeLocal))),
		ResponseMode:                security.OIDCResponseMode(getEnv("OIDC_RESPONSE_MODE", string(security.OIDCResponseModeFormPost))),
		PostLogoutRedirectURL:       getEnv("OIDC_POST_LOGOUT_REDIRECT_URL", ""),
		AutoProvisionAllowedDomains: parseCSV(getEnv("OIDC_AUTO_PROVISION_ALLOWED_DOMAINS", "")),
	}
	if err := config.Validate(cookieSecure, registrationMode == services.RegistrationModeOpen); err != nil {
		return security.OIDCConfig{}, err
	}
	return config, nil
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

func resolveSecretKey() (string, error) {
	secret := strings.TrimSpace(os.Getenv("SECRET_KEY"))
	if secret == "" {
		keyFilePath := strings.TrimSpace(os.Getenv("SECRET_KEY_FILE"))
		if keyFilePath != "" {
			content, err := readSecretKeyFile(keyFilePath)
			if err != nil {
				return "", fmt.Errorf("failed to read SECRET_KEY_FILE: %w", err)
			}
			secret = strings.TrimSpace(content)
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

func readSecretKeyFile(path string) (string, error) {
	content, err := security.ReadBoundedRegularFile(path, "SECRET_KEY_FILE", maxSecretKeyFileBytes)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func resolvePort() (string, error) {
	raw := strings.TrimSpace(getEnv("PORT", "8080"))
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("PORT must be a number between 1 and 65535")
	}
	return strconv.Itoa(port), nil
}
