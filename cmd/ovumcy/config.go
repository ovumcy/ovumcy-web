package main

import (
	"fmt"
	"log"
	"net"
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
	// CalendarFeedFencePath is CALENDAR_FEED_FENCE_PATH: the file the boot-time
	// restore fence keeps its non-rollbackable half in. It has to live OUTSIDE
	// whatever a database backup captures — that is the whole mechanism — so it
	// is an operator-supplied path rather than one the app derives: only the
	// operator knows which of their mounts their backups skip. Empty (the
	// default for a bare binary; the image sets it) means no fence, which fails
	// closed: every armed calendar feed is disarmed on every boot.
	CalendarFeedFencePath string
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
	// LogoutMax/LogoutWindow size the per-IP edge limiter in front of
	// DELETE /api/v1/sessions/current. LogoutAccountMax/LogoutAccountWindow
	// size the per-account, identity-keyed budget AuthService enforces behind
	// it. They are deliberately separate pairs: one bounds requests from an
	// address, the other bounds failures against one account, and a household
	// instance behind one address needs the first wide while the second stays
	// tight. Wiring both from one pair — which is what shipped until this split
	// — silently ran the account budget at the per-IP number.
	LogoutMax           int
	LogoutWindow        time.Duration
	LogoutAccountMax    int
	LogoutAccountWindow time.Duration
	APIMax              int
	APIWindow           time.Duration
	// CalendarFeed is deliberately NOT the API budget. The feed is a cookieless
	// unauthenticated endpoint and this is the only cap on it; it was sized when
	// every well-formed request cost a bcrypt compare, and since migration 032
	// moved verification (and its timing equalization) to a keyed MAC it bounds
	// only the residual bcrypt of a row minted before that. A calendar client
	// polls once per refresh interval, so a small budget is generous. See
	// docs/security/auth-policy-and-rate-limits.md.
	CalendarFeedMax    int
	CalendarFeedWindow time.Duration
}

type proxySettings struct {
	Enabled        bool
	Header         string
	TrustedProxies []string
}

// maxSecretKeyFileBytes caps the SECRET_KEY_FILE read (see resolveSecretKey).
const maxSecretKeyFileBytes int64 = 8 << 10

// maxDatabaseURLFileBytes caps the DATABASE_URL_FILE read (see
// resolveDatabaseConfig). A Postgres DSN with a long password and several
// query parameters comfortably fits well under this.
const maxDatabaseURLFileBytes int64 = 8 << 10

// maxOIDCClientSecretFileBytes caps the OIDC_CLIENT_SECRET_FILE read (see
// resolveOIDCConfig).
const maxOIDCClientSecretFileBytes int64 = 8 << 10

// codecov:ignore:start -- main() composition-root wiring: fatal-exits on an
// invalid runtime config at boot. loadRuntimeConfig (the resolution logic) is
// unit-tested directly; the log.Fatal path cannot run under `go test`.
func mustLoadRuntimeConfig(location *time.Location) runtimeConfig {
	config, err := loadRuntimeConfig(location)
	if err != nil {
		log.Fatal(err)
	}
	return config
}

// codecov:ignore:end

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

	cookieSecure, err := getEnvBoolStrict("COOKIE_SECURE", false)
	if err != nil {
		return runtimeConfig{}, err
	}
	// HSTS defaults to COOKIE_SECURE (preserving the historical coupling where
	// enabling secure cookies also pinned HTTPS) but is an independent switch:
	// HSTS_ENABLED=false lets an operator run secure cookies without pinning
	// browsers to HTTPS for a year, and HSTS_ENABLED=true opts in explicitly.
	hstsEnabled, err := getEnvBoolStrict("HSTS_ENABLED", cookieSecure)
	if err != nil {
		return runtimeConfig{}, err
	}
	webhookBlockPrivate, err := getEnvBoolStrict("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", false)
	if err != nil {
		return runtimeConfig{}, err
	}
	oidcConfig, err := resolveOIDCConfig(cookieSecure, registrationMode)
	if err != nil {
		return runtimeConfig{}, err
	}

	calendarFeedFencePath, err := resolveCalendarFeedFencePath()
	if err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		Location:              location,
		SecretKey:             secretKey,
		DatabaseConfig:        databaseConfig,
		Port:                  port,
		DefaultLanguage:       getEnv("DEFAULT_LANGUAGE", "en"),
		CalendarFeedFencePath: calendarFeedFencePath,
		RegistrationMode:      registrationMode,
		CookieSecure:          cookieSecure,
		HSTSEnabled:           hstsEnabled,
		OIDC:                  oidcConfig,
		RateLimits: rateLimitSettings{
			LoginMax:             getEnvInt("RATE_LIMIT_LOGIN_MAX", 8),
			LoginWindow:          getEnvDuration("RATE_LIMIT_LOGIN_WINDOW", 15*time.Minute),
			RegisterMax:          getEnvInt("RATE_LIMIT_REGISTER_MAX", 8),
			RegisterWindow:       getEnvDuration("RATE_LIMIT_REGISTER_WINDOW", 15*time.Minute),
			ForgotPasswordMax:    getEnvInt("RATE_LIMIT_FORGOT_PASSWORD_MAX", 8),
			ForgotPasswordWindow: getEnvDuration("RATE_LIMIT_FORGOT_PASSWORD_WINDOW", time.Hour),
			LogoutMax:            getEnvInt("RATE_LIMIT_LOGOUT_MAX", 60),
			LogoutWindow:         getEnvDuration("RATE_LIMIT_LOGOUT_WINDOW", 15*time.Minute),
			// Defaulted from the service constants so the documented account
			// budget and the code cannot drift apart.
			LogoutAccountMax:    getEnvInt("RATE_LIMIT_LOGOUT_ACCOUNT_MAX", services.DefaultLogoutAttemptsLimit),
			LogoutAccountWindow: getEnvDuration("RATE_LIMIT_LOGOUT_ACCOUNT_WINDOW", services.DefaultLogoutAttemptsWindow),
			APIMax:              getEnvInt("RATE_LIMIT_API_MAX", 300),
			APIWindow:           getEnvDuration("RATE_LIMIT_API_WINDOW", time.Minute),
			CalendarFeedMax:     getEnvInt("RATE_LIMIT_CALENDAR_FEED_MAX", 20),
			CalendarFeedWindow:  getEnvDuration("RATE_LIMIT_CALENDAR_FEED_WINDOW", time.Minute),
		},
		Proxy:               proxy,
		AuditLogEnabled:     getEnvBool("AUDIT_LOG_ENABLED", false),
		WebhookBlockPrivate: webhookBlockPrivate,
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

// resolveCalendarFeedFencePath reads CALENDAR_FEED_FENCE_PATH for the boot-time
// restore fence.
//
// No default: an unset value must reach the fence as "not configured" so it
// fails closed. A default path would guess at a mount the operator never made
// and turn a loud refusal into a fence that silently vanishes.
//
// A relative path refuses the boot instead, because this server is not the only
// process that opens that file: `ovumcy users delete` and a forced `ovumcy
// reset-password` confirm and advance the SAME fence before removing an owner's
// calendar feed, and each resolves a relative path against its own working
// directory rather than this one's. Taken verbatim it would give the server a
// fence that works and every operator removal a refusal whose documented remedy
// — point the server at this path — is already satisfied. Refusing is deliberately
// not "treat it as unconfigured": that would disarm every armed feed on each
// start, which is worse than what a relative path does today.
func resolveCalendarFeedFencePath() (string, error) {
	fencePath := strings.TrimSpace(os.Getenv(security.CalendarFeedFencePathEnv))
	if fencePath != "" && !security.CalendarFeedFencePathRooted(fencePath) {
		return "", fmt.Errorf("invalid %s=%q: the operator CLI resolves a relative fence path against its own working directory, not the server's, and refuses it, so give an absolute path (for example /app/fence/calendar-feed.fence)",
			security.CalendarFeedFencePathEnv, fencePath)
	}
	return fencePath, nil
}

func resolveOIDCConfig(cookieSecure bool, registrationMode services.RegistrationMode) (security.OIDCConfig, error) {
	clientSecret, err := resolveSecretFromEnvOrFile("OIDC_CLIENT_SECRET", "OIDC_CLIENT_SECRET_FILE", maxOIDCClientSecretFileBytes)
	if err != nil {
		return security.OIDCConfig{}, err
	}

	config := security.OIDCConfig{
		Enabled:                     getEnvBool("OIDC_ENABLED", false),
		IssuerURL:                   getEnv("OIDC_ISSUER_URL", ""),
		ClientID:                    getEnv("OIDC_CLIENT_ID", ""),
		ClientSecret:                clientSecret,
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
	enabled, err := getEnvBoolStrict("TRUST_PROXY_ENABLED", false)
	if err != nil {
		return proxySettings{}, err
	}

	settings := proxySettings{
		Enabled:        enabled,
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
	if err := validateTrustedProxies(settings.TrustedProxies); err != nil {
		return proxySettings{}, err
	}
	return settings, nil
}

// validateTrustedProxies refuses a TRUSTED_PROXIES entry the trust boundary
// could never use, so the boot fails loudly instead of running on a smaller
// trusted set than the operator wrote.
//
// The rules are the ones newTrustedProxyMatcher (and fiber's own
// handleTrustedProxy) apply: an entry containing "/" is a CIDR range, anything
// else is matched by exact net.IP string — hence the canonical-spelling check,
// since "2001:DB8::1" parses fine yet never equals the "2001:db8::1" the
// matcher looks up. An entry that fails either rule used to be dropped (bad
// CIDR) or stored where nothing can match it (everything else): the proxy then
// stayed untrusted, every client behind it fell back to the socket peer and
// shared one rate-limit bucket, while the startup banner counted the raw CSV
// and reported the typo as configured.
//
// A repeated IP is refused for the same reason though both spellings are well
// formed: the matcher keys its exact set by the entry and collapses the pair
// into one, so the banner would count a trusted proxy the matcher does not
// hold — the same lie the typo told, minted by a copy-paste instead. A repeated
// CIDR is left alone: ranges are appended to a slice, so both copies survive
// and the two counts still agree.
func validateTrustedProxies(entries []string) error {
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if strings.Contains(entry, "/") {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				return fmt.Errorf("invalid TRUSTED_PROXIES entry %q: not a CIDR range (for example 10.0.0.0/8)", entry)
			}
			continue
		}
		parsed := net.ParseIP(entry)
		if parsed == nil {
			return fmt.Errorf("invalid TRUSTED_PROXIES entry %q: not an IP address or CIDR range", entry)
		}
		if canonical := parsed.String(); canonical != entry {
			return fmt.Errorf("invalid TRUSTED_PROXIES entry %q: an IP is matched literally, so it must be written in canonical form (%q)", entry, canonical)
		}
		if _, duplicate := seen[entry]; duplicate {
			return fmt.Errorf("invalid TRUSTED_PROXIES entry %q: listed more than once", entry)
		}
		seen[entry] = struct{}{}
	}
	return nil
}

func resolveDatabaseConfig() (db.Config, error) {
	driver := db.Driver(strings.ToLower(strings.TrimSpace(getEnv("DB_DRIVER", string(db.DriverSQLite)))))
	config := db.Config{
		Driver:     driver,
		SQLitePath: getEnv("DB_PATH", filepath.Join("data", "ovumcy.db")),
	}

	// Only resolve DATABASE_URL/DATABASE_URL_FILE when the driver actually
	// consumes it. The old plain os.Getenv("DATABASE_URL") read could never
	// fail; resolving through the bounded-file helper CAN fail (unreadable
	// file, directory path, oversized file), and a sqlite instance carrying a
	// stale DATABASE_URL_FILE — an old value left in .env, or a Swarm secret
	// mount that vanished on redeploy — must not have that turn into a reason
	// to refuse booting a database it never touches.
	if driver == db.DriverPostgres {
		databaseURL, err := resolveSecretFromEnvOrFile("DATABASE_URL", "DATABASE_URL_FILE", maxDatabaseURLFileBytes)
		if err != nil {
			return db.Config{}, err
		}
		config.PostgresURL = databaseURL
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
	secret, err := resolveSecretFromEnvOrFile("SECRET_KEY", "SECRET_KEY_FILE", maxSecretKeyFileBytes)
	if err != nil {
		return "", err
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

// resolveSecretFromEnvOrFile resolves one sensitive configuration value that
// may be supplied either directly via envVar or, for Docker Swarm/Compose
// secrets on the shell-free runtime image (distroless: no `sh -c 'export
// X=$(cat …)'` workaround is possible), via fileEnvVar naming a local file to
// read it from — generalizing the SECRET_KEY/SECRET_KEY_FILE pattern so every
// sensitive value resolves the same way instead of drifting per variable.
//
// envVar wins silently over fileEnvVar when both are set, matching SECRET_KEY's
// existing, tested precedence over SECRET_KEY_FILE — one behavior, reused
// rather than re-decided per variable. Silent is only safe once the operator
// can find out some other way: a wrong SECRET_KEY fails loudly at first use,
// but a wrong DATABASE_URL or OIDC_CLIENT_SECRET does not fail at all — it
// just points at the wrong database or provider — and there is no shell in
// the distroless runtime image to go looking. So every resolution logs one
// line at boot naming which variable supplied the value, and — when both were
// set — naming the fileEnvVar that was ignored. The resolved value itself is
// never included in any error or log line — only the variable names.
//
// The file is read through security.ReadBoundedRegularFile, which rejects
// directories/special files and caps the read at maxBytes; its content is
// trimmed the same way a plain env value already is, so a trailing newline
// from `echo secret > file` never leaks into the resolved value. A file that
// cannot be read aborts with an error naming fileEnvVar — callers that only
// consume the value on some other condition (a particular DB_DRIVER, OIDC
// enabled) must gate the call on that condition themselves, so a stale or
// dangling fileEnvVar for a value nothing reads never blocks boot.
func resolveSecretFromEnvOrFile(envVar, fileEnvVar string, maxBytes int64) (string, error) {
	value := strings.TrimSpace(os.Getenv(envVar))
	filePath := strings.TrimSpace(os.Getenv(fileEnvVar))

	if value != "" {
		if filePath != "" {
			log.Printf("config: %s supplied via environment; %s is also set but ignored (%s takes precedence)", envVar, fileEnvVar, envVar)
		} else {
			log.Printf("config: %s supplied via environment", envVar)
		}
		return value, nil
	}

	if filePath == "" {
		return "", nil
	}

	content, err := security.ReadBoundedRegularFile(filePath, fileEnvVar, maxBytes)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", fileEnvVar, err)
	}
	log.Printf("config: %s supplied via %s", envVar, fileEnvVar)
	return strings.TrimSpace(string(content)), nil
}

func resolvePort() (string, error) {
	raw := strings.TrimSpace(getEnv("PORT", "8080"))
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("PORT must be a number between 1 and 65535")
	}
	return strconv.Itoa(port), nil
}
