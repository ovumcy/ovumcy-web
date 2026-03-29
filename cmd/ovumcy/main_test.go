package main

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestResolveSecretKey(t *testing.T) {
	valid := "0123456789abcdef0123456789abcdef"
	t.Run("requires a secret source", func(t *testing.T) {
		t.Setenv("SECRET_KEY", "")
		t.Setenv("SECRET_KEY_FILE", "")
		assertResolveSecretKeyError(t, "SECRET_KEY is required")
	})

	t.Run("rejects insecure placeholder values from environment", func(t *testing.T) {
		t.Setenv("SECRET_KEY_FILE", "")
		t.Setenv("SECRET_KEY", "change_me_in_production")
		assertResolveSecretKeyError(t, "placeholder value")

		t.Setenv("SECRET_KEY", "replace_with_at_least_32_random_characters")
		assertResolveSecretKeyError(t, "placeholder value")
	})

	t.Run("rejects short environment secrets", func(t *testing.T) {
		t.Setenv("SECRET_KEY_FILE", "")
		t.Setenv("SECRET_KEY", "too-short-secret")
		assertResolveSecretKeyError(t, "at least 32 characters")
	})

	t.Run("accepts a valid environment secret", func(t *testing.T) {
		t.Setenv("SECRET_KEY_FILE", "")
		t.Setenv("SECRET_KEY", valid)

		secret, err := resolveSecretKey()
		if err != nil {
			t.Fatalf("expected valid secret, got error: %v", err)
		}
		if secret != valid {
			t.Fatalf("expected %q, got %q", valid, secret)
		}
	})

	t.Run("reads and trims SECRET_KEY_FILE", func(t *testing.T) {
		t.Setenv("SECRET_KEY", "")
		t.Setenv("SECRET_KEY_FILE", writeSecretKeyFile(t, valid+"\n"))

		secret, err := resolveSecretKey()
		if err != nil {
			t.Fatalf("expected valid secret from file, got error: %v", err)
		}
		if secret != valid {
			t.Fatalf("expected %q from file, got %q", valid, secret)
		}
	})

	t.Run("SECRET_KEY takes precedence over SECRET_KEY_FILE", func(t *testing.T) {
		t.Setenv("SECRET_KEY", valid)
		t.Setenv("SECRET_KEY_FILE", filepath.Join(t.TempDir(), "missing-secret.txt"))

		secret, err := resolveSecretKey()
		if err != nil {
			t.Fatalf("expected env secret to win, got error: %v", err)
		}
		if secret != valid {
			t.Fatalf("expected %q from env, got %q", valid, secret)
		}
	})

	t.Run("fails when SECRET_KEY_FILE cannot be read", func(t *testing.T) {
		t.Setenv("SECRET_KEY", "")
		missingPath := filepath.Join(t.TempDir(), "missing-secret.txt")
		t.Setenv("SECRET_KEY_FILE", missingPath)
		assertResolveSecretKeyError(t, "failed to read SECRET_KEY_FILE")
		assertResolveSecretKeyError(t, missingPath)
	})

	t.Run("rejects directory SECRET_KEY_FILE paths", func(t *testing.T) {
		t.Setenv("SECRET_KEY", "")
		t.Setenv("SECRET_KEY_FILE", t.TempDir())
		assertResolveSecretKeyError(t, "regular file")
	})

	t.Run("rejects oversized SECRET_KEY_FILE values", func(t *testing.T) {
		t.Setenv("SECRET_KEY", "")
		t.Setenv("SECRET_KEY_FILE", writeSecretKeyFile(t, strings.Repeat("a", int(maxSecretKeyFileBytes)+1)))
		assertResolveSecretKeyError(t, "at most")
	})

	t.Run("rejects whitespace-only SECRET_KEY_FILE values", func(t *testing.T) {
		t.Setenv("SECRET_KEY", "")
		t.Setenv("SECRET_KEY_FILE", writeSecretKeyFile(t, " \n\t "))
		assertResolveSecretKeyError(t, "SECRET_KEY is required")
	})

	t.Run("rejects insecure placeholder values from SECRET_KEY_FILE", func(t *testing.T) {
		t.Setenv("SECRET_KEY", "")
		t.Setenv("SECRET_KEY_FILE", writeSecretKeyFile(t, "change_me_in_production\n"))
		assertResolveSecretKeyError(t, "placeholder value")
	})

	t.Run("rejects short SECRET_KEY_FILE values", func(t *testing.T) {
		t.Setenv("SECRET_KEY", "")
		t.Setenv("SECRET_KEY_FILE", writeSecretKeyFile(t, "too-short-secret\n"))
		assertResolveSecretKeyError(t, "at least 32 characters")
	})
}

func assertResolveSecretKeyError(t *testing.T, expectedSubstring string) {
	t.Helper()

	_, err := resolveSecretKey()
	if err == nil {
		t.Fatalf("expected error containing %q", expectedSubstring)
	}
	if !strings.Contains(err.Error(), expectedSubstring) {
		t.Fatalf("expected error containing %q, got %v", expectedSubstring, err)
	}
}

func writeSecretKeyFile(t *testing.T, contents string) string {
	t.Helper()

	filePath := filepath.Join(t.TempDir(), "secret_key.txt")
	if err := os.WriteFile(filePath, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed to write secret key file: %v", err)
	}
	return filePath
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

func TestResolveRegistrationMode(t *testing.T) {
	t.Setenv("REGISTRATION_MODE", "")
	mode, err := resolveRegistrationMode()
	if err != nil {
		t.Fatalf("expected default registration mode, got error: %v", err)
	}
	if mode != services.RegistrationModeOpen {
		t.Fatalf("expected default registration mode open, got %q", mode)
	}

	t.Setenv("REGISTRATION_MODE", "closed")
	mode, err = resolveRegistrationMode()
	if err != nil {
		t.Fatalf("expected closed registration mode, got error: %v", err)
	}
	if mode != services.RegistrationModeClosed {
		t.Fatalf("expected registration mode closed, got %q", mode)
	}

	t.Setenv("REGISTRATION_MODE", "invite_only")
	if _, err := resolveRegistrationMode(); err == nil {
		t.Fatal("expected invalid registration mode to fail")
	}
}

func TestResolveOIDCConfig(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		t.Setenv("OIDC_ENABLED", "")
		t.Setenv("OIDC_ISSUER_URL", "")
		t.Setenv("OIDC_CLIENT_ID", "")
		t.Setenv("OIDC_CLIENT_SECRET", "")
		t.Setenv("OIDC_REDIRECT_URL", "")
		t.Setenv("OIDC_AUTO_PROVISION", "")

		config, err := resolveOIDCConfig(false, services.RegistrationModeOpen)
		if err != nil {
			t.Fatalf("expected disabled OIDC config to validate, got %v", err)
		}
		if config.Enabled {
			t.Fatal("expected OIDC to be disabled by default")
		}
	})

	t.Run("enabled requires secure cookies and valid URLs", func(t *testing.T) {
		t.Setenv("OIDC_ENABLED", "true")
		t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
		t.Setenv("OIDC_CLIENT_ID", "ovumcy")
		t.Setenv("OIDC_CLIENT_SECRET", "secret")
		t.Setenv("OIDC_REDIRECT_URL", "https://ovumcy.example.com"+security.OIDCCallbackPath)

		if _, err := resolveOIDCConfig(false, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "COOKIE_SECURE=true") {
			t.Fatalf("expected secure-cookie validation error, got %v", err)
		}
	})

	t.Run("enabled accepts valid hybrid config", func(t *testing.T) {
		t.Setenv("OIDC_ENABLED", "true")
		t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
		t.Setenv("OIDC_CLIENT_ID", "ovumcy")
		t.Setenv("OIDC_CLIENT_SECRET", "secret")
		t.Setenv("OIDC_REDIRECT_URL", "https://ovumcy.example.com"+security.OIDCCallbackPath)
		t.Setenv("OIDC_AUTO_PROVISION", "false")
		t.Setenv("OIDC_LOGIN_MODE", "oidc_only")
		t.Setenv("OIDC_LOGOUT_MODE", "provider")
		t.Setenv("OIDC_POST_LOGOUT_REDIRECT_URL", "https://ovumcy.example.com/login")
		t.Setenv("OIDC_AUTO_PROVISION_ALLOWED_DOMAINS", "example.com, staff.example.com")
		t.Setenv("OIDC_CA_FILE", writeOIDCCATestFile(t))

		config, err := resolveOIDCConfig(true, services.RegistrationModeOpen)
		if err != nil {
			t.Fatalf("expected valid OIDC config, got %v", err)
		}
		if !config.Enabled {
			t.Fatal("expected OIDC to be enabled")
		}
		if config.IssuerURL != "https://id.example.com" {
			t.Fatalf("expected issuer URL to be preserved, got %q", config.IssuerURL)
		}
		if config.RedirectURL != "https://ovumcy.example.com"+security.OIDCCallbackPath {
			t.Fatalf("expected redirect URL to be preserved, got %q", config.RedirectURL)
		}
		if config.LoginMode != security.OIDCLoginModeOIDCOnly {
			t.Fatalf("expected oidc_only login mode, got %q", config.LoginMode)
		}
		if config.LogoutMode != security.OIDCLogoutModeProvider {
			t.Fatalf("expected provider logout mode, got %q", config.LogoutMode)
		}
		if config.PostLogoutRedirectURL != "https://ovumcy.example.com/login" {
			t.Fatalf("expected post-logout redirect URL to be preserved, got %q", config.PostLogoutRedirectURL)
		}
		if config.CAFile == "" {
			t.Fatal("expected OIDC CA file to be preserved")
		}
		if len(config.AutoProvisionAllowedDomains) != 2 || config.AutoProvisionAllowedDomains[0] != "example.com" || config.AutoProvisionAllowedDomains[1] != "staff.example.com" {
			t.Fatalf("expected normalized domain allowlist, got %#v", config.AutoProvisionAllowedDomains)
		}
	})

	t.Run("rejects auto provision", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_AUTO_PROVISION", "true")

		if _, err := resolveOIDCConfig(true, services.RegistrationModeClosed); err == nil || !strings.Contains(err.Error(), "REGISTRATION_MODE=open") {
			t.Fatalf("expected auto-provision registration-mode validation error, got %v", err)
		}
	})

	t.Run("rejects insecure issuer url", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_ISSUER_URL", "http://id.example.com")

		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "OIDC_ISSUER_URL must use https") {
			t.Fatalf("expected insecure issuer validation error, got %v", err)
		}
	})

	t.Run("rejects issuer query and redirect fragment", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_ISSUER_URL", "https://id.example.com?tenant=main")
		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "OIDC_ISSUER_URL must not include query or fragment") {
			t.Fatalf("expected issuer query validation error, got %v", err)
		}

		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_REDIRECT_URL", "https://ovumcy.example.com"+security.OIDCCallbackPath+"#done")
		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "OIDC_REDIRECT_URL must not include query or fragment") {
			t.Fatalf("expected redirect fragment validation error, got %v", err)
		}
	})

	t.Run("rejects redirect path outside callback", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_REDIRECT_URL", "https://ovumcy.example.com/auth/callback")

		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), security.OIDCCallbackPath) {
			t.Fatalf("expected redirect path validation error, got %v", err)
		}
	})

	t.Run("rejects invalid login mode", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_LOGIN_MODE", "sso_only")

		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "OIDC_LOGIN_MODE") {
			t.Fatalf("expected login mode validation error, got %v", err)
		}
	})

	t.Run("rejects invalid logout mode", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_LOGOUT_MODE", "idp")

		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "OIDC_LOGOUT_MODE") {
			t.Fatalf("expected logout mode validation error, got %v", err)
		}
	})

	t.Run("rejects post logout redirect on another origin", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_POST_LOGOUT_REDIRECT_URL", "https://elsewhere.example.com/login")

		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "match the OIDC redirect origin") {
			t.Fatalf("expected post-logout origin validation error, got %v", err)
		}
	})

	t.Run("rejects invalid auto provision allowlist", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_AUTO_PROVISION_ALLOWED_DOMAINS", "example.com, bad domain")

		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "OIDC_AUTO_PROVISION_ALLOWED_DOMAINS") {
			t.Fatalf("expected auto-provision allowlist validation error, got %v", err)
		}
	})

	t.Run("rejects unreadable oidc ca file", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_CA_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))

		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "OIDC_CA_FILE") {
			t.Fatalf("expected OIDC CA file validation error, got %v", err)
		}
	})

	t.Run("rejects invalid oidc ca file contents", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		invalidPath := filepath.Join(t.TempDir(), "invalid-ca.pem")
		if err := os.WriteFile(invalidPath, []byte("not a pem bundle"), 0o600); err != nil {
			t.Fatalf("write invalid oidc ca file: %v", err)
		}
		t.Setenv("OIDC_CA_FILE", invalidPath)

		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "OIDC_CA_FILE") {
			t.Fatalf("expected invalid OIDC CA file contents error, got %v", err)
		}
	})

	t.Run("rejects directory oidc ca path", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("OIDC_CA_FILE", t.TempDir())

		if _, err := resolveOIDCConfig(true, services.RegistrationModeOpen); err == nil || !strings.Contains(err.Error(), "OIDC_CA_FILE") {
			t.Fatalf("expected directory OIDC CA file validation error, got %v", err)
		}
	})
}

func setValidOIDCTestEnv(t *testing.T) {
	t.Helper()

	t.Setenv("OIDC_ENABLED", "true")
	t.Setenv("OIDC_ISSUER_URL", "https://id.example.com")
	t.Setenv("OIDC_CLIENT_ID", "ovumcy")
	t.Setenv("OIDC_CLIENT_SECRET", "secret")
	t.Setenv("OIDC_REDIRECT_URL", "https://ovumcy.example.com"+security.OIDCCallbackPath)
	t.Setenv("OIDC_AUTO_PROVISION", "false")
	t.Setenv("OIDC_LOGIN_MODE", "hybrid")
	t.Setenv("OIDC_LOGOUT_MODE", "local")
	t.Setenv("OIDC_POST_LOGOUT_REDIRECT_URL", "")
	t.Setenv("OIDC_AUTO_PROVISION_ALLOWED_DOMAINS", "")
	t.Setenv("OIDC_CA_FILE", "")
}

func writeOIDCCATestFile(t *testing.T) string {
	t.Helper()

	const certPEM = `-----BEGIN CERTIFICATE-----
MIIC9DCCAdygAwIBAgIBATANBgkqhkiG9w0BAQsFADAUMRIwEAYDVQQDEwkxMjcu
MC4wLjEwHhcNMjYwMzI4MTczMDIzWhcNMzYwMzI4MTgzMDIzWjAUMRIwEAYDVQQD
EwkxMjcuMC4wLjEwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCfTEzc
4zujGs9PmM6PHzFQIphEOjPAQmcaoUmCBRWjSWiVA1hIzbe0AM2wKGVE7pz8xDeY
qGAgrXP0aUF98U+gFcNLihw8xMVkAW6R+FkV+PXAuMW7ZQAmrvq6fOkHfMWEA3/3
4pA73uNPQDtWPOIjTz77jNRNOvymCvaUhy/bt3PqvnEzWNa9PdVSOTcLTaydGkx+
9eq8b/Do/Tlca8pncZ7Luy+SEQAQlTPVMe4h8WWKSlyW1YVloZm5XX5Wvj4xzMmh
oHwDwLU+wojt9hl2I6nEF8LJi3YMfYcuaUXrxC9DxToI13gzWXJqAnH40fJC7QC2
wMZPD73wTU/nb36TAgMBAAGjUTBPMA4GA1UdDwEB/wQEAwIFoDATBgNVHSUEDDAK
BggrBgEFBQcDATAMBgNVHRMBAf8EAjAAMBoGA1UdEQQTMBGCCWxvY2FsaG9zdIcE
fwAAATANBgkqhkiG9w0BAQsFAAOCAQEANg0b76OBe8BtSG4JcCeQhT2IiIeIWmVs
KLD4o/u7EzQ5d9PRodCFkkBVkP2B6fg7z1GAl/H1tKKBFidovJAbXQ/yJHqhT7IC
QrCubmlgRkIl9YUJvaOsW0rPBLlWqz2emJH0xftH3QNPWHBVnP3R3BjrIqUG/1xU
ADS7/yMYzyqEmi2+/nnyVMcDvPQfA9K+D32fHHteAsX8HhF2W4YAg1TlsUjpIsYc
lILxHyF4qIl18bap1H7cTSH4ABA2fmMkIl7uqGapSzeJaMkxgSq8RxUy6k43dWOm
Al6FYKyHksUwdVrLUsSoFtlfM7w8UhjdXDF/fvAvqvwWm9bPVCahEg==
-----END CERTIFICATE-----`

	path := filepath.Join(t.TempDir(), "oidc-ca.pem")
	if err := os.WriteFile(path, []byte(certPEM), 0o600); err != nil {
		t.Fatalf("write oidc ca test file: %v", err)
	}
	return path
}

func TestLoadRuntimeConfigBuildsExpectedSettings(t *testing.T) {
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", "data\\custom.db")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PORT", "9090")
	t.Setenv("DEFAULT_LANGUAGE", "ru")
	t.Setenv("REGISTRATION_MODE", "closed")
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
	if config.RegistrationMode != services.RegistrationModeClosed {
		t.Fatalf("expected registration mode closed, got %q", config.RegistrationMode)
	}
	if !config.CookieSecure {
		t.Fatal("expected cookie secure=true")
	}
	if config.OIDC.Enabled {
		t.Fatal("expected OIDC to remain disabled when env is unset")
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
	app.Use(securityHeadersMiddleware(false))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("html request failed: %v", err)
	}
	defer response.Body.Close()

	assertDefaultSecurityHeaders(t, response, false)
}

func TestSecurityHeadersMiddlewareSetsHeadersOnAPIResponses(t *testing.T) {
	app := fiber.New()
	app.Use(securityHeadersMiddleware(false))
	app.Get("/api/ping", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	request := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("api request failed: %v", err)
	}
	defer response.Body.Close()

	assertDefaultSecurityHeaders(t, response, false)
}

func TestSecurityHeadersMiddlewareAddsHSTSWhenSecureCookiesEnabled(t *testing.T) {
	app := fiber.New()
	app.Use(securityHeadersMiddleware(true))
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("secure html request failed: %v", err)
	}
	defer response.Body.Close()

	assertDefaultSecurityHeaders(t, response, true)
}

func TestStaticManifestUsesWebManifestContentType(t *testing.T) {
	registerStaticContentTypes()

	app := fiber.New()
	app.Static("/static", filepath.Join("..", "..", "web", "static"))

	request := httptest.NewRequest(http.MethodGet, "/static/manifest.webmanifest", nil)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("manifest request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/manifest+json") {
		t.Fatalf("expected web manifest content type, got %q", contentType)
	}
}

func assertDefaultSecurityHeaders(t *testing.T, response *http.Response, expectStrictTransportSecurity bool) {
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
	if value := response.Header.Get(headerContentSecurityPolicy); value != contentSecurityPolicyDefault {
		t.Fatalf("expected %s=%q, got %q", headerContentSecurityPolicy, contentSecurityPolicyDefault, value)
	}
	if value := response.Header.Get(headerStrictTransportSecurity); expectStrictTransportSecurity {
		if value != strictTransportSecurityDefault {
			t.Fatalf("expected %s=%q, got %q", headerStrictTransportSecurity, strictTransportSecurityDefault, value)
		}
	} else if value != "" {
		t.Fatalf("did not expect %s by default, got %q", headerStrictTransportSecurity, value)
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

func TestTryRunCLICommandWithHandlersDispatchesUsersCommand(t *testing.T) {
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "cli-users.db"))
	t.Setenv("DATABASE_URL", "")

	called := false
	handled, err := tryRunCLICommandWithHandlers([]string{"users", "list"}, cliCommandHandlers{
		runResetPassword: func(db.Config, string) error {
			t.Fatal("did not expect reset-password handler")
			return nil
		},
		runUsers: func(databaseConfig db.Config, args []string) error {
			called = true
			if databaseConfig.Driver != db.DriverSQLite {
				t.Fatalf("expected sqlite driver, got %q", databaseConfig.Driver)
			}
			if len(args) != 1 || args[0] != "list" {
				t.Fatalf("unexpected users args: %#v", args)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handled {
		t.Fatal("expected CLI command to be handled")
	}
	if !called {
		t.Fatal("expected users handler to be called")
	}
}

func TestTryRunCLICommandWithHandlersRejectsMissingUsersSubcommand(t *testing.T) {
	handled, err := tryRunCLICommandWithHandlers([]string{"users"}, cliCommandHandlers{})
	if !handled {
		t.Fatal("expected users command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "usage: ovumcy users <list|delete>") {
		t.Fatalf("expected users usage error, got %v", err)
	}
}

func TestTryRunCLICommandWithHandlersPropagatesUsersError(t *testing.T) {
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "cli-users.db"))
	t.Setenv("DATABASE_URL", "")

	expectedErr := errors.New("delete failed")
	handled, err := tryRunCLICommandWithHandlers([]string{"users", "delete", "owner@example.com", "--yes"}, cliCommandHandlers{
		runResetPassword: func(db.Config, string) error {
			t.Fatal("did not expect reset-password handler")
			return nil
		},
		runUsers: func(db.Config, []string) error {
			return expectedErr
		},
	})
	if !handled {
		t.Fatal("expected users command to be handled")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected propagated users error, got %v", err)
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

func TestRequestLoggerUsesSafeRouteTemplateWithoutIP(t *testing.T) {
	var output bytes.Buffer
	app := fiber.New()
	app.Use(newRequestLogger(&output))
	app.Post("/api/days/:date", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-17", nil)
	request.RemoteAddr = "203.0.113.9:43123"

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, "/api/days/:date") {
		t.Fatalf("expected safe route template in request logs, got %q", logLine)
	}
	if strings.Contains(logLine, "2026-02-17") {
		t.Fatalf("did not expect concrete health date in request logs: %q", logLine)
	}
	if strings.Contains(logLine, "203.0.113.9") {
		t.Fatalf("did not expect raw client ip in request logs: %q", logLine)
	}
}

func TestRateLimitLogDoesNotLogQueryPII(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	const plaintextPassword = "PlaintextPassword123!"

	app := fiber.New()
	app.Post("/api/days/:date", func(c *fiber.Ctx) error {
		c.Response().Header.Set(fiber.HeaderRetryAfter, "60")
		logRateLimitHit(c)
		return c.SendStatus(http.StatusTooManyRequests)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/days/2026-02-17?token=plain-reset-token&email=user@example.com",
		strings.NewReader("email=user@example.com&password=PlaintextPassword123%21&token=plain-reset-token"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "203.0.113.9:43123"

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("rate limit request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, "path=/api/days/:date") {
		t.Fatalf("expected sanitized path without query string in rate-limit logs, got %q", logLine)
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
	if strings.Contains(logLine, "2026-02-17") {
		t.Fatalf("did not expect concrete health date in rate-limit logs: %q", logLine)
	}
	if strings.Contains(logLine, "203.0.113.9") {
		t.Fatalf("did not expect raw client ip in rate-limit logs: %q", logLine)
	}
}

func TestCSRFMiddlewareErrorHandlerLogsSecurityEventWithoutPII(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	app := fiber.New()
	app.Use(csrf.New(csrfMiddlewareConfig(false)))
	app.Post("/settings/change-password", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/settings/change-password?email=user@example.com",
		strings.NewReader("password=PlaintextPassword123%21"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("csrf request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, `security event: action="csrf" outcome="denied"`) {
		t.Fatalf("expected csrf security event in logs, got %q", logLine)
	}
	if strings.Contains(logLine, "user@example.com") {
		t.Fatalf("did not expect email in csrf security logs: %q", logLine)
	}
	if strings.Contains(logLine, "PlaintextPassword123!") {
		t.Fatalf("did not expect password in csrf security logs: %q", logLine)
	}
}

func TestAuthRateLimitHandlerLogsSecurityEventWithoutPII(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Post("/api/auth/login", newAuthRateLimitHandler(handler, authRateLimitConfig{
		ErrorCode: "too_many_login_attempts",
	}))

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/login?email=user@example.com",
		strings.NewReader("email=user@example.com&password=PlaintextPassword123%21"),
	)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("rate-limit handler request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, `security event: action="rate_limit" outcome="blocked"`) {
		t.Fatalf("expected rate-limit security event in logs, got %q", logLine)
	}
	if !strings.Contains(logLine, `scope="auth"`) {
		t.Fatalf("expected auth scope in rate-limit security logs, got %q", logLine)
	}
	if strings.Contains(logLine, "user@example.com") {
		t.Fatalf("did not expect email in rate-limit security logs: %q", logLine)
	}
	if strings.Contains(logLine, "PlaintextPassword123!") {
		t.Fatalf("did not expect password in rate-limit security logs: %q", logLine)
	}
}

func TestConfigureFiberMiddlewareRateLimitsOIDCStart(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	configureFiberMiddleware(app, runtimeConfig{
		CookieSecure: false,
		RateLimits: rateLimitSettings{
			LoginMax:             1,
			LoginWindow:          time.Minute,
			ForgotPasswordMax:    8,
			ForgotPasswordWindow: time.Hour,
			APIMax:               300,
			APIWindow:            time.Minute,
		},
	}, handler)
	app.Get("/auth/oidc/start", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil)
	request.RemoteAddr = "203.0.113.10:43123"
	first := mustRateLimitedResponse(t, app, request)
	if first.StatusCode != http.StatusNoContent {
		t.Fatalf("expected first OIDC start request to pass, got %d", first.StatusCode)
	}

	secondRequest := httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil)
	secondRequest.RemoteAddr = "203.0.113.10:43123"
	second := mustRateLimitedResponse(t, app, secondRequest)
	if second.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected second OIDC start request to be rate limited, got %d", second.StatusCode)
	}
	if location := second.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected OIDC start rate limit redirect to /login, got %q", location)
	}
}

func TestConfigureFiberMiddlewareRateLimitsOIDCCallback(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	configureFiberMiddleware(app, runtimeConfig{
		CookieSecure: false,
		RateLimits: rateLimitSettings{
			LoginMax:             1,
			LoginWindow:          time.Minute,
			ForgotPasswordMax:    8,
			ForgotPasswordWindow: time.Hour,
			APIMax:               300,
			APIWindow:            time.Minute,
		},
	}, handler)
	app.Post(security.OIDCCallbackPath, func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader("state=one&code=provider-code"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "203.0.113.11:43123"
	first := mustRateLimitedResponse(t, app, request)
	if first.StatusCode != http.StatusNoContent {
		t.Fatalf("expected first OIDC callback request to pass, got %d", first.StatusCode)
	}

	secondRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader("state=two&code=provider-code"))
	secondRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	secondRequest.RemoteAddr = "203.0.113.11:43123"
	second := mustRateLimitedResponse(t, app, secondRequest)
	if second.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected second OIDC callback request to be rate limited, got %d", second.StatusCode)
	}
	if location := second.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected OIDC callback rate limit redirect to /login, got %q", location)
	}
}

func mustRateLimitedResponse(t *testing.T, app *fiber.App, request *http.Request) *http.Response {
	t.Helper()

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("rate-limit request failed: %v", err)
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})
	return response
}
