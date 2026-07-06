package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
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
	handler := newRateLimitTestHandler(t)
	secureConfig := csrfMiddlewareConfig(true, handler)
	if !secureConfig.CookieSecure {
		t.Fatal("expected csrf cookie secure flag to be enabled")
	}
	if !secureConfig.CookieHTTPOnly {
		t.Fatal("expected csrf cookie to be httpOnly")
	}
	if secureConfig.CookieName != "ovumcy_csrf" {
		t.Fatalf("expected csrf cookie name ovumcy_csrf, got %q", secureConfig.CookieName)
	}
	if secureConfig.Extractor.Extract == nil {
		t.Fatal("expected csrf extractor to be wired")
	}
	if secureConfig.Extractor.Key != "csrf_token" {
		t.Fatalf("expected csrf extractor key csrf_token, got %q", secureConfig.Extractor.Key)
	}

	insecureConfig := csrfMiddlewareConfig(false, handler)
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
	t.Run("disabled by default", testResolveOIDCConfigDisabled)
	t.Run("enabled requires secure cookies and valid URLs", testResolveOIDCConfigRequiresSecureCookies)
	t.Run("enabled accepts valid hybrid config", testResolveOIDCConfigAcceptsValidConfig)
	runOIDCConfigValidationCases(t)
}

func testResolveOIDCConfigDisabled(t *testing.T) {
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
}

func testResolveOIDCConfigRequiresSecureCookies(t *testing.T) {
	setValidOIDCTestEnv(t)
	assertResolveOIDCConfigError(t, false, services.RegistrationModeOpen, "COOKIE_SECURE=true", nil)
}

func testResolveOIDCConfigAcceptsValidConfig(t *testing.T) {
	setValidOIDCTestEnv(t)
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
}

func runOIDCConfigValidationCases(t *testing.T) {
	t.Helper()

	invalidCAPath := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "invalid-ca.pem")
		if err := os.WriteFile(path, []byte("not a pem bundle"), 0o600); err != nil {
			t.Fatalf("write invalid oidc ca file: %v", err)
		}
		return path
	}

	cases := []struct {
		name             string
		cookieSecure     bool
		registrationMode services.RegistrationMode
		wantContains     string
		setup            func(t *testing.T)
	}{
		{
			name:             "rejects auto provision",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeClosed,
			wantContains:     "REGISTRATION_MODE=open",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_AUTO_PROVISION", "true")
			},
		},
		{
			name:             "rejects insecure issuer url",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "OIDC_ISSUER_URL must use https",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_ISSUER_URL", "http://id.example.com")
			},
		},
		{
			name:             "rejects issuer query",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "OIDC_ISSUER_URL must not include query or fragment",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_ISSUER_URL", "https://id.example.com?tenant=main")
			},
		},
		{
			name:             "rejects redirect fragment",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "OIDC_REDIRECT_URL must not include query or fragment",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_REDIRECT_URL", "https://ovumcy.example.com"+security.OIDCCallbackPath+"#done")
			},
		},
		{
			name:             "rejects redirect path outside callback",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     security.OIDCCallbackPath,
			setup: func(t *testing.T) {
				t.Setenv("OIDC_REDIRECT_URL", "https://ovumcy.example.com/auth/callback")
			},
		},
		{
			name:             "rejects invalid login mode",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "OIDC_LOGIN_MODE",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_LOGIN_MODE", "sso_only")
			},
		},
		{
			name:             "rejects invalid logout mode",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "OIDC_LOGOUT_MODE",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_LOGOUT_MODE", "idp")
			},
		},
		{
			name:             "rejects post logout redirect on another origin",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "match the OIDC redirect origin",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_POST_LOGOUT_REDIRECT_URL", "https://elsewhere.example.com/login")
			},
		},
		{
			name:             "rejects invalid auto provision allowlist",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "OIDC_AUTO_PROVISION_ALLOWED_DOMAINS",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_AUTO_PROVISION_ALLOWED_DOMAINS", "example.com, bad domain")
			},
		},
		{
			name:             "rejects unreadable oidc ca file",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "OIDC_CA_FILE",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_CA_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))
			},
		},
		{
			name:             "rejects invalid oidc ca file contents",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "OIDC_CA_FILE",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_CA_FILE", invalidCAPath(t))
			},
		},
		{
			name:             "rejects directory oidc ca path",
			cookieSecure:     true,
			registrationMode: services.RegistrationModeOpen,
			wantContains:     "OIDC_CA_FILE",
			setup: func(t *testing.T) {
				t.Setenv("OIDC_CA_FILE", t.TempDir())
			},
		},
	}

	for _, tc := range cases {

		t.Run(tc.name, func(t *testing.T) {
			setValidOIDCTestEnv(t)
			assertResolveOIDCConfigError(t, tc.cookieSecure, tc.registrationMode, tc.wantContains, tc.setup)
		})
	}
}

func assertResolveOIDCConfigError(t *testing.T, cookieSecure bool, registrationMode services.RegistrationMode, wantContains string, setup func(t *testing.T)) {
	t.Helper()

	if setup != nil {
		setup(t)
	}
	if _, err := resolveOIDCConfig(cookieSecure, registrationMode); err == nil || !strings.Contains(err.Error(), wantContains) {
		t.Fatalf("expected OIDC config validation error containing %q, got %v", wantContains, err)
	}
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
	if !config.HSTSEnabled {
		t.Fatal("expected HSTS to inherit COOKIE_SECURE=true when HSTS_ENABLED is unset")
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

// TestLoadRuntimeConfigDefaultsAuditLogOff locks the privacy-first default:
// when AUDIT_LOG_ENABLED is unset the runtime must NOT emit per-action audit
// logs. This matches SECURITY.md and .env.example, both of which
// state audit logging is off by default. (The api-package audit-flag test
// covers the request path; this one exercises the startup default.)
func TestLoadRuntimeConfigDefaultsAuditLogOff(t *testing.T) {
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", "data/ovumcy.db")
	t.Setenv("AUDIT_LOG_ENABLED", "")

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if config.AuditLogEnabled {
		t.Fatal("AUDIT_LOG_ENABLED must default to false (off by default per SECURITY.md / .env.example)")
	}
}

func TestLoadRuntimeConfigHonorsAuditLogEnabled(t *testing.T) {
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", "data/ovumcy.db")
	t.Setenv("AUDIT_LOG_ENABLED", "true")

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if !config.AuditLogEnabled {
		t.Fatal("AUDIT_LOG_ENABLED=true must enable audit logging")
	}
}

// TestLoadRuntimeConfigDefaultsReminderSchedulerOff locks the HIGH-RISK default:
// the built-in reminder scheduler (an always-on outbound component) ships OFF
// and runs at local hour 9 when its env is unset. This is the instant-rollback
// contract (REMINDER_SCHEDULER_ENABLED=false).
func TestLoadRuntimeConfigDefaultsReminderSchedulerOff(t *testing.T) {
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", "data/ovumcy.db")
	t.Setenv("REMINDER_SCHEDULER_ENABLED", "")
	t.Setenv("REMINDER_SCHEDULER_HOUR", "")

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if config.ReminderScheduler.Enabled {
		t.Fatal("REMINDER_SCHEDULER_ENABLED must default to false (always-on outbound component ships off)")
	}
	if config.ReminderScheduler.Hour != 9 {
		t.Fatalf("REMINDER_SCHEDULER_HOUR must default to 9, got %d", config.ReminderScheduler.Hour)
	}
}

// TestLoadRuntimeConfigHonorsReminderSchedulerSettings covers the enabled path
// and hour override, including hour 0 (midnight) which getEnvInt would have
// rejected — the dedicated range helper must accept it.
func TestLoadRuntimeConfigHonorsReminderSchedulerSettings(t *testing.T) {
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", "data/ovumcy.db")
	t.Setenv("REMINDER_SCHEDULER_ENABLED", "true")
	t.Setenv("REMINDER_SCHEDULER_HOUR", "0")

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	if !config.ReminderScheduler.Enabled {
		t.Fatal("REMINDER_SCHEDULER_ENABLED=true must enable the scheduler")
	}
	if config.ReminderScheduler.Hour != 0 {
		t.Fatalf("REMINDER_SCHEDULER_HOUR=0 (midnight) must be accepted, got %d", config.ReminderScheduler.Hour)
	}
}

// TestGetEnvIntInRange pins the helper the scheduler hour needs: it accepts the
// full inclusive range (crucially 0, unlike getEnvInt), and falls back on unset,
// non-numeric, or out-of-range input.
func TestGetEnvIntInRange(t *testing.T) {
	const key = "TEST_ENV_INT_IN_RANGE"
	cases := []struct {
		name  string
		value string
		set   bool
		want  int
	}{
		{name: "unset -> fallback", set: false, want: 9},
		{name: "zero accepted at lower bound", value: "0", set: true, want: 0},
		{name: "max accepted at upper bound", value: "23", set: true, want: 23},
		{name: "mid-range accepted", value: "14", set: true, want: 14},
		{name: "below range -> fallback", value: "-1", set: true, want: 9},
		{name: "above range -> fallback", value: "24", set: true, want: 9},
		{name: "non-numeric -> fallback", value: "noon", set: true, want: 9},
		{name: "blank -> fallback", value: "   ", set: true, want: 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(key, tc.value)
			} else if err := os.Unsetenv(key); err != nil {
				t.Fatalf("unset %s: %v", key, err)
			}
			if got := getEnvIntInRange(key, 9, 0, 23); got != tc.want {
				t.Fatalf("getEnvIntInRange(%q) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

// TestLogStartupNotesReminderSchedulerOnlyWhenEnabled pins the operator NOTE: the
// always-on outbound scheduler must announce itself (with its local run hour and
// the instant-rollback env) when enabled, and stay silent when off.
func TestLogStartupNotesReminderSchedulerOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		hour     int
		wantNote bool
	}{
		{name: "enabled announces the daily outbound pass", enabled: true, hour: 9, wantNote: true},
		{name: "disabled stays silent", enabled: false, hour: 9, wantNote: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalWriter := log.Writer()
			defer log.SetOutput(originalWriter)

			var output bytes.Buffer
			log.SetOutput(&output)

			logStartup(runtimeConfig{
				Location:          time.UTC,
				Port:              "9090",
				ReminderScheduler: reminderSchedulerSettings{Enabled: tt.enabled, Hour: tt.hour},
			})

			logLine := output.String()
			if got := strings.Contains(logLine, "NOTE: REMINDER_SCHEDULER_ENABLED=true"); got != tt.wantNote {
				t.Fatalf("scheduler note present = %t, want %t in startup log: %q", got, tt.wantNote, logLine)
			}
			if tt.wantNote && !strings.Contains(logLine, "REMINDER_SCHEDULER_ENABLED=false") {
				t.Fatalf("enabled note must cite the instant-rollback env, got %q", logLine)
			}
		})
	}
}

// TestLoadRuntimeConfigResolvesHSTSSwitch locks the HSTS_ENABLED contract: it
// defaults to COOKIE_SECURE (the historical coupling, zero breaking change) but
// an explicit true/false overrides in either direction, so an operator can run
// secure cookies without pinning HTTPS (or opt into the pin over plain HTTP).
func TestLoadRuntimeConfigResolvesHSTSSwitch(t *testing.T) {
	tests := []struct {
		name         string
		cookieSecure string
		hstsEnabled  string
		want         bool
	}{
		{name: "inherits cookie secure true", cookieSecure: "true", hstsEnabled: "", want: true},
		{name: "inherits cookie secure false", cookieSecure: "false", hstsEnabled: "", want: false},
		{name: "explicit false overrides secure cookies", cookieSecure: "true", hstsEnabled: "false", want: false},
		{name: "explicit true overrides insecure cookies", cookieSecure: "false", hstsEnabled: "true", want: true},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
			t.Setenv("DB_DRIVER", "sqlite")
			t.Setenv("DB_PATH", "data/ovumcy.db")
			t.Setenv("COOKIE_SECURE", tt.cookieSecure)
			t.Setenv("HSTS_ENABLED", tt.hstsEnabled)

			config, err := loadRuntimeConfig(time.UTC)
			if err != nil {
				t.Fatalf("load runtime config: %v", err)
			}
			if config.HSTSEnabled != tt.want {
				t.Fatalf("HSTSEnabled = %t, want %t (COOKIE_SECURE=%q, HSTS_ENABLED=%q)", config.HSTSEnabled, tt.want, tt.cookieSecure, tt.hstsEnabled)
			}
		})
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
	if !config.TrustProxy {
		t.Fatal("expected trusted proxy check to be enabled")
	}
	if !config.EnableIPValidation {
		t.Fatal("expected IP validation to be enabled")
	}
	if len(config.TrustProxyConfig.Proxies) != 2 {
		t.Fatalf("expected trusted proxies to be applied, got %#v", config.TrustProxyConfig.Proxies)
	}
}

// TestFiberConfigSetsImportSizedBodyLimit locks the transport body cap to the
// named import-sized constant. Without an explicit BodyLimit fiber falls back
// to its 4 MiB default, which is below a full services.MaxImportEntries JSON
// restore (~8-12 MiB) — the documented import capacity would be unreachable
// over HTTP.
func TestFiberConfigSetsImportSizedBodyLimit(t *testing.T) {
	config := fiberConfig(proxySettings{})

	if config.BodyLimit != maxRequestBodyBytes {
		t.Fatalf("expected BodyLimit=%d, got %d", maxRequestBodyBytes, config.BodyLimit)
	}
	if maxRequestBodyBytes <= fiber.DefaultBodyLimit {
		t.Fatalf("expected body limit above fiber default %d, got %d", fiber.DefaultBodyLimit, maxRequestBodyBytes)
	}
}

// TestOvumcyErrorHandlerMapsBodyLimitTo413 pins the mapping the top-level
// ErrorHandler applies to fiber's body-limit rejection. Production reaches this
// via App.serverErrorHandler, which maps fasthttp's ErrBodyTooLarge to a
// *fiber.Error with code 413 and then calls ErrorHandler. Returning that same
// fiber.ErrRequestEntityTooLarge from a handler routes through ErrorHandler the
// same way (mirroring how the sibling 403 case is tested) and exercises our
// branch: a JSON client must receive the stable {"error":"request_too_large"}
// envelope, never fasthttp's bare "Request Entity Too Large" string. (The
// in-memory app.Test transport surfaces the body-limit read error to the caller
// rather than routing it through serverErrorHandler — enforcement of the cap
// itself is covered by TestFiberAppEnforcesBodyLimit.)
func TestOvumcyErrorHandlerMapsBodyLimitTo413(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: ovumcyErrorHandler})
	app.Post("/api/v1/imports/json", func(c fiber.Ctx) error {
		return fiber.ErrRequestEntityTooLarge
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/imports/json", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", response.StatusCode)
	}
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(response.Body)
	if strings.Contains(body.String(), "Request Entity Too Large") {
		t.Fatalf("expected mapped envelope, got bare fasthttp message: %q", body.String())
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal JSON envelope %q: %v", body.String(), err)
	}
	if payload["error"] != "request_too_large" {
		t.Fatalf("error key: got %v want %q", payload["error"], "request_too_large")
	}
	detail, ok := payload["error_detail"].(map[string]any)
	if !ok {
		t.Fatalf("expected error_detail object, got %v", payload["error_detail"])
	}
	if detail["category"] != "too_large" {
		t.Fatalf("error_detail.category: got %v want %q", detail["category"], "too_large")
	}
}

// TestFiberAppEnforcesBodyLimit proves the configured cap actually refuses an
// oversized body. fiber enforces BodyLimit in fasthttp's request reader, so the
// in-memory app.Test transport returns the body-limit error to the caller
// rather than a routed response — asserting that error confirms the request is
// rejected before any handler runs. A tiny BodyLimit keeps the body small.
func TestFiberAppEnforcesBodyLimit(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: ovumcyErrorHandler,
		BodyLimit:    16,
	})
	handlerReached := false
	app.Post("/api/v1/imports/json", func(c fiber.Ctx) error {
		handlerReached = true
		return c.SendStatus(fiber.StatusOK)
	})

	oversized := strings.Repeat("a", 64)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/imports/json", strings.NewReader(oversized))
	request.Header.Set("Content-Type", "application/json")

	_, err := app.Test(request, testConfigNoTimeout)
	if err == nil {
		t.Fatal("expected body-limit rejection error from oversized request")
	}
	if !strings.Contains(err.Error(), "body size exceeds") {
		t.Fatalf("expected body-size limit error, got %v", err)
	}
	if handlerReached {
		t.Fatal("handler must not run for a body exceeding the limit")
	}
}

func TestSecurityHeadersMiddlewareSetsHeadersOnHTMLResponses(t *testing.T) {
	app := fiber.New()
	app.Use(securityHeadersMiddleware(false))
	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("html request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	assertDefaultSecurityHeaders(t, response, false)
}

func TestSecurityHeadersMiddlewareSetsHeadersOnAPIResponses(t *testing.T) {
	app := fiber.New()
	app.Use(securityHeadersMiddleware(false))
	app.Get("/api/ping", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	request := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("api request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	assertDefaultSecurityHeaders(t, response, false)
}

func TestSecurityHeadersMiddlewareAddsHSTSWhenSecureCookiesEnabled(t *testing.T) {
	app := fiber.New()
	app.Use(securityHeadersMiddleware(true))
	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("secure html request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	assertDefaultSecurityHeaders(t, response, true)
}

func TestOvumcyErrorHandlerMasksRawErrorsAndPreservesFiberErrors(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: ovumcyErrorHandler})
	app.Get("/fiber-error", func(c fiber.Ctx) error {
		return fiber.ErrForbidden
	})
	app.Get("/raw-error", func(c fiber.Ctx) error {
		return errors.New("internal users table secret column leaked")
	})

	fiberErrResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/fiber-error", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("fiber-error request failed: %v", err)
	}
	defer func() { _ = fiberErrResp.Body.Close() }()
	if fiberErrResp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("fiber.Error status = %d, want 403", fiberErrResp.StatusCode)
	}
	fiberBody := new(bytes.Buffer)
	_, _ = fiberBody.ReadFrom(fiberErrResp.Body)
	if fiberBody.String() != "Forbidden" {
		t.Fatalf("fiber.Error body = %q, want %q (status/message preserved)", fiberBody.String(), "Forbidden")
	}

	rawErrResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/raw-error", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("raw-error request failed: %v", err)
	}
	defer func() { _ = rawErrResp.Body.Close() }()
	if rawErrResp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("raw error status = %d, want 500", rawErrResp.StatusCode)
	}
	rawBody := new(bytes.Buffer)
	_, _ = rawBody.ReadFrom(rawErrResp.Body)
	if rawBody.String() != "Internal Server Error" {
		t.Fatalf("raw error body = %q, want generic message", rawBody.String())
	}
	if strings.Contains(rawBody.String(), "secret column leaked") {
		t.Fatalf("raw error body leaked internal detail: %q", rawBody.String())
	}
}

func TestStaticManifestUsesWebManifestContentType(t *testing.T) {
	registerStaticContentTypes()

	app := fiber.New()
	app.Use("/static", newStaticAssetHandler())

	request := httptest.NewRequest(http.MethodGet, "/static/manifest.webmanifest", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("manifest request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/manifest+json") {
		t.Fatalf("expected web manifest content type, got %q", contentType)
	}
}

// TestStaticAssetsSendCacheControlMaxAge locks the cache policy for /static:
// fiber must emit a Cache-Control max-age (built from staticAssetMaxAgeSeconds)
// so versioned assets are cached instead of heuristically revalidated. Paired
// with the ?v=<build revision> cache-buster asserted in the api render tests, a
// release still invalidates stale bundles.
func TestStaticAssetsSendCacheControlMaxAge(t *testing.T) {
	app := fiber.New()
	app.Use("/static", newStaticAssetHandler())

	request := httptest.NewRequest(http.MethodGet, "/static/manifest.webmanifest", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("static asset request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	want := "public, max-age=" + strconv.Itoa(staticAssetMaxAgeSeconds)
	if got := response.Header.Get("Cache-Control"); got != want {
		t.Fatalf("expected Cache-Control=%q on static asset, got %q", want, got)
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
	if value := response.Header.Get(headerCrossOriginOpenerPolicy); value != crossOriginOpenerPolicyDefault {
		t.Fatalf("expected %s=%q, got %q", headerCrossOriginOpenerPolicy, crossOriginOpenerPolicyDefault, value)
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
	if value := response.Header.Get("Cache-Control"); value != "no-store" {
		t.Fatalf("expected Cache-Control=no-store on dynamic response, got %q", value)
	}
	if value := response.Header.Get("Access-Control-Allow-Origin"); value != "" {
		t.Fatalf("did not expect Access-Control-Allow-Origin by default, got %q", value)
	}
}

// TestSecurityHeadersMiddlewareSetsNoCacheOnDynamicRoutes verifies that
// authenticated / dynamic responses carry Cache-Control: no-store so that
// bfcache cannot restore health data after logout.
func TestSecurityHeadersMiddlewareSetsNoCacheOnDynamicRoutes(t *testing.T) {
	app := fiber.New()
	app.Use(securityHeadersMiddleware(false))
	app.Get("/dashboard", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dynamic route request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if value := response.Header.Get("Cache-Control"); value != "no-store" {
		t.Fatalf("expected Cache-Control=no-store on dynamic route, got %q", value)
	}
}

// TestSecurityHeadersMiddlewareDoesNotSetNoCacheOnStaticAssets verifies that
// the /static prefix is exempt from the no-store guard so static assets can
// use normal browser caching.
func TestSecurityHeadersMiddlewareDoesNotSetNoCacheOnStaticAssets(t *testing.T) {
	app := fiber.New()
	app.Use(securityHeadersMiddleware(false))
	app.Get("/static/app.js", func(c fiber.Ctx) error {
		return c.SendString("// js")
	})

	request := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("static asset request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if value := response.Header.Get("Cache-Control"); value == "no-store" {
		t.Fatalf("did not expect Cache-Control=no-store on /static asset, got %q", value)
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

func TestLogStartupNotesHSTSPinOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		name        string
		hstsEnabled bool
		wantNote    bool
	}{
		{name: "enabled logs the one-year pin note", hstsEnabled: true, wantNote: true},
		{name: "disabled stays silent about HSTS", hstsEnabled: false, wantNote: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalWriter := log.Writer()
			defer log.SetOutput(originalWriter)

			var output bytes.Buffer
			log.SetOutput(&output)

			logStartup(runtimeConfig{
				Location:    time.UTC,
				Port:        "9090",
				HSTSEnabled: tt.hstsEnabled,
			})

			logLine := output.String()
			if got := strings.Contains(logLine, "NOTE: HSTS_ENABLED=true"); got != tt.wantNote {
				t.Fatalf("HSTS pin note present = %t, want %t in startup log: %q", got, tt.wantNote, logLine)
			}
			if !strings.Contains(logLine, fmt.Sprintf("hsts=%t", tt.hstsEnabled)) {
				t.Fatalf("expected hsts=%t flag in startup log, got %q", tt.hstsEnabled, logLine)
			}
		})
	}
}

func TestProxyHeaderRateLimitWarning(t *testing.T) {
	tests := []struct {
		name     string
		proxy    proxySettings
		wantWarn bool
	}{
		{name: "trust proxy with X-Forwarded-For warns", proxy: proxySettings{Enabled: true, Header: "X-Forwarded-For"}, wantWarn: true},
		{name: "case/space insensitive header warns", proxy: proxySettings{Enabled: true, Header: " x-forwarded-for "}, wantWarn: true},
		{name: "trust proxy with X-Real-IP is safe", proxy: proxySettings{Enabled: true, Header: "X-Real-IP"}, wantWarn: false},
		{name: "disabled trust proxy never warns", proxy: proxySettings{Enabled: false, Header: "X-Forwarded-For"}, wantWarn: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := proxyHeaderRateLimitWarning(tc.proxy)
			if !tc.wantWarn {
				if got != "" {
					t.Fatalf("expected no note, got %q", got)
				}
				return
			}
			if got == "" {
				t.Fatal("expected a spoofable-header note, got none")
			}
			// The note must reflect the post-keygen reality: the limiters are
			// hardened (rightmost untrusted) and the actionable fix is X-Real-IP.
			// It must NOT claim the rate limiter itself is spoofable.
			if !strings.Contains(got, "rightmost untrusted") {
				t.Fatalf("note should state the limiter keys on the rightmost untrusted hop, got %q", got)
			}
			if !strings.Contains(got, "X-Real-IP") {
				t.Fatalf("note should recommend X-Real-IP, got %q", got)
			}
			if strings.Contains(got, "rate limiter keys on the leftmost") {
				t.Fatalf("note must not claim the rate limiter keys on the spoofable leftmost entry, got %q", got)
			}
		})
	}
}

func TestLogStartupWarnsOnSpoofableProxyHeader(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	logStartup(runtimeConfig{
		Location: time.UTC,
		Port:     "8080",
		Proxy: proxySettings{
			Enabled: true,
			Header:  "X-Forwarded-For",
		},
	})

	logged := output.String()
	if !strings.Contains(logged, "PROXY_HEADER=X-Forwarded-For") {
		t.Fatalf("expected spoofable-proxy-header note in startup log, got %q", logged)
	}
	// The note must describe the hardened keying, not the obsolete claim that
	// the limiter trusts the leftmost (spoofable) X-Forwarded-For entry.
	if !strings.Contains(logged, "rightmost untrusted") {
		t.Fatalf("expected startup note to mention the rightmost-untrusted keying, got %q", logged)
	}
}

func TestLogStartupAdvisesWhenTrustProxyDisabled(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	logStartup(runtimeConfig{
		Location: time.UTC,
		Port:     "8080",
		Proxy: proxySettings{
			Enabled: false,
		},
	})

	logged := output.String()
	if !strings.Contains(logged, "TRUST_PROXY_ENABLED=false") {
		t.Fatalf("expected proxy-disabled advisory in startup log, got %q", logged)
	}
	if !strings.Contains(logged, "reverse proxy") {
		t.Fatalf("expected advisory to mention reverse proxy, got %q", logged)
	}
}

func TestLogStartupDoesNotAdviseTrustProxyWhenEnabled(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	logStartup(runtimeConfig{
		Location: time.UTC,
		Port:     "8080",
		Proxy: proxySettings{
			Enabled:        true,
			Header:         "X-Real-IP",
			TrustedProxies: []string{"127.0.0.1"},
		},
	})

	logged := output.String()
	if strings.Contains(logged, "TRUST_PROXY_ENABLED=false") {
		t.Fatalf("did not expect proxy-disabled advisory when proxy is enabled, got %q", logged)
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
	if err == nil || !strings.Contains(err.Error(), "usage: ovumcy users <list|delete|create>") {
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

func TestTryRunCLICommandWithHandlersDispatchesHealthcheck(t *testing.T) {
	t.Setenv("PORT", "9876")

	var receivedPort string
	handled, err := tryRunCLICommandWithHandlers([]string{"healthcheck"}, cliCommandHandlers{
		runHealthcheck: func(port string, _ time.Duration) error {
			receivedPort = port
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handled {
		t.Fatal("expected healthcheck command to be handled")
	}
	if receivedPort != "9876" {
		t.Fatalf("expected port forwarded from PORT env, got %q", receivedPort)
	}
}

func TestTryRunCLICommandWithHandlersRejectsHealthcheckExtraArgs(t *testing.T) {
	handled, err := tryRunCLICommandWithHandlers([]string{"healthcheck", "extra"}, cliCommandHandlers{
		runHealthcheck: func(string, time.Duration) error {
			t.Fatal("did not expect healthcheck handler to be called")
			return nil
		},
	})
	if !handled {
		t.Fatal("expected healthcheck command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "usage: ovumcy healthcheck") {
		t.Fatalf("expected healthcheck usage error, got %v", err)
	}
}

func TestTryRunCLICommandWithHandlersPropagatesHealthcheckError(t *testing.T) {
	expectedErr := errors.New("probe failed")
	handled, err := tryRunCLICommandWithHandlers([]string{"healthcheck"}, cliCommandHandlers{
		runHealthcheck: func(string, time.Duration) error {
			return expectedErr
		},
	})
	if !handled {
		t.Fatal("expected healthcheck command to be handled")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected propagated healthcheck error, got %v", err)
	}
}

func TestTryRunCLICommandWithHandlersDispatchesNotify(t *testing.T) {
	// SECRET_KEY is required by the notify dispatch (it resolves the decrypt key
	// before invoking the handler); provide a valid one and the flags to forward.
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("TZ", "UTC")
	t.Setenv("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", "true")

	var (
		receivedArgs         []string
		receivedBlock        bool
		receivedSecretNonNil bool
		receivedLangNonEmpty bool
	)
	handled, err := tryRunCLICommandWithHandlers([]string{"notify", "--dry-run"}, cliCommandHandlers{
		runNotify: func(_ db.Config, secretKey string, defaultLanguage string, location *time.Location, blockPrivateAddresses bool, args []string) error {
			receivedArgs = args
			receivedBlock = blockPrivateAddresses
			receivedSecretNonNil = secretKey != ""
			receivedLangNonEmpty = defaultLanguage != ""
			if location == nil {
				t.Fatal("expected a non-nil location forwarded to the notify handler")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handled {
		t.Fatal("expected notify command to be handled")
	}
	if len(receivedArgs) != 1 || receivedArgs[0] != "--dry-run" {
		t.Fatalf("expected --dry-run forwarded as args, got %v", receivedArgs)
	}
	if !receivedBlock {
		t.Fatal("expected WEBHOOK_BLOCK_PRIVATE_ADDRESSES=true forwarded as true")
	}
	if !receivedSecretNonNil {
		t.Fatal("expected the resolved SECRET_KEY forwarded to the notify handler")
	}
	if !receivedLangNonEmpty {
		t.Fatal("expected a default language forwarded to the notify handler")
	}
}

func TestTryRunCLICommandWithHandlersNotifyRequiresHandler(t *testing.T) {
	handled, err := tryRunCLICommandWithHandlers([]string{"notify"}, cliCommandHandlers{})
	if !handled {
		t.Fatal("expected notify command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "notify handler is required") {
		t.Fatalf("expected notify-handler-required error, got %v", err)
	}
}

func TestTryRunCLICommandWithHandlersNotifyReportsInvalidSecretKey(t *testing.T) {
	// No SECRET_KEY set -> the notify dispatch fails at key resolution before the
	// handler runs.
	t.Setenv("SECRET_KEY", "")
	t.Setenv("SECRET_KEY_FILE", "")

	handled, err := tryRunCLICommandWithHandlers([]string{"notify"}, cliCommandHandlers{
		runNotify: func(db.Config, string, string, *time.Location, bool, []string) error {
			t.Fatal("did not expect the notify handler to be called without a SECRET_KEY")
			return nil
		},
	})
	if !handled {
		t.Fatal("expected notify command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "SECRET_KEY") {
		t.Fatalf("expected an invalid-SECRET_KEY error, got %v", err)
	}
}

func TestTryRunCLICommandWithHandlersNotifyReportsInvalidDatabaseConfig(t *testing.T) {
	// The notify dispatch resolves the database config first; an unsupported
	// DB_DRIVER fails validation before the handler runs.
	t.Setenv("DB_DRIVER", "mysql")

	handled, err := tryRunCLICommandWithHandlers([]string{"notify"}, cliCommandHandlers{
		runNotify: func(db.Config, string, string, *time.Location, bool, []string) error {
			t.Fatal("did not expect the notify handler to be called with an invalid DB config")
			return nil
		},
	})
	if !handled {
		t.Fatal("expected notify command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "invalid database config") {
		t.Fatalf("expected an invalid-database-config error, got %v", err)
	}
}

func TestTryRunCLICommandWithHandlersDispatchesWebhook(t *testing.T) {
	// SECRET_KEY is required by the webhook dispatch (it resolves the encrypt/
	// decrypt key before invoking the handler); provide a valid one.
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")

	var (
		receivedArgs         []string
		receivedSecretNonNil bool
	)
	handled, err := tryRunCLICommandWithHandlers([]string{"webhook", "show", "owner@example.com"}, cliCommandHandlers{
		runWebhook: func(_ db.Config, secretKey string, args []string) error {
			receivedArgs = args
			receivedSecretNonNil = secretKey != ""
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !handled {
		t.Fatal("expected webhook command to be handled")
	}
	if len(receivedArgs) != 2 || receivedArgs[0] != "show" || receivedArgs[1] != "owner@example.com" {
		t.Fatalf("expected show+email forwarded as args, got %v", receivedArgs)
	}
	if !receivedSecretNonNil {
		t.Fatal("expected the resolved SECRET_KEY forwarded to the webhook handler")
	}
}

func TestTryRunCLICommandWithHandlersRejectsMissingWebhookSubcommand(t *testing.T) {
	handled, err := tryRunCLICommandWithHandlers([]string{"webhook"}, cliCommandHandlers{})
	if !handled {
		t.Fatal("expected webhook command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "usage: ovumcy webhook") {
		t.Fatalf("expected webhook usage error, got %v", err)
	}
}

func TestTryRunCLICommandWithHandlersWebhookRequiresHandler(t *testing.T) {
	handled, err := tryRunCLICommandWithHandlers([]string{"webhook", "show", "owner@example.com"}, cliCommandHandlers{})
	if !handled {
		t.Fatal("expected webhook command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "webhook handler is required") {
		t.Fatalf("expected webhook-handler-required error, got %v", err)
	}
}

func TestTryRunCLICommandWithHandlersWebhookReportsInvalidDatabaseConfig(t *testing.T) {
	// The webhook dispatch resolves the database config first; an unsupported
	// DB_DRIVER fails validation before the handler runs.
	t.Setenv("DB_DRIVER", "mysql")

	handled, err := tryRunCLICommandWithHandlers([]string{"webhook", "show", "owner@example.com"}, cliCommandHandlers{
		runWebhook: func(db.Config, string, []string) error {
			t.Fatal("did not expect the webhook handler to be called with an invalid DB config")
			return nil
		},
	})
	if !handled {
		t.Fatal("expected webhook command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "invalid database config") {
		t.Fatalf("expected an invalid-database-config error, got %v", err)
	}
}

func TestTryRunCLICommandWithHandlersWebhookReportsInvalidSecretKey(t *testing.T) {
	// A valid DB driver but no SECRET_KEY -> the webhook dispatch fails at key
	// resolution before the handler runs.
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("SECRET_KEY", "")
	t.Setenv("SECRET_KEY_FILE", "")

	handled, err := tryRunCLICommandWithHandlers([]string{"webhook", "show", "owner@example.com"}, cliCommandHandlers{
		runWebhook: func(db.Config, string, []string) error {
			t.Fatal("did not expect the webhook handler to be called without a SECRET_KEY")
			return nil
		},
	})
	if !handled {
		t.Fatal("expected webhook command to be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "SECRET_KEY") {
		t.Fatalf("expected an invalid-SECRET_KEY error, got %v", err)
	}
}

func TestResolveBoolEnv(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		set      bool
		fallback bool
		want     bool
	}{
		{name: "unset returns fallback true", set: false, fallback: true, want: true},
		{name: "unset returns fallback false", set: false, fallback: false, want: false},
		{name: "true", value: "true", set: true, fallback: false, want: true},
		{name: "1", value: "1", set: true, fallback: false, want: true},
		{name: "false", value: "false", set: true, fallback: true, want: false},
		{name: "0", value: "0", set: true, fallback: true, want: false},
		{name: "blank returns fallback", value: "   ", set: true, fallback: true, want: true},
		{name: "unparseable returns fallback", value: "notabool", set: true, fallback: true, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const key = "OVUMCY_TEST_BOOL_ENV"
			if tc.set {
				t.Setenv(key, tc.value)
			} else {
				t.Setenv(key, "")
			}
			if got := resolveBoolEnv(key, tc.fallback); got != tc.want {
				t.Fatalf("resolveBoolEnv(%q, %v) = %v, want %v", tc.value, tc.fallback, got, tc.want)
			}
		})
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
		Stream: &output,
	}))
	app.Get("/reset-password", func(c fiber.Ctx) error {
		return c.SendStatus(http.StatusNoContent)
	})

	request := httptest.NewRequest(
		http.MethodGet,
		"/reset-password?token=plain-reset-token&email=user@example.com",
		nil,
	)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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
		Stream: &output,
	}))
	app.Post("/api/v1/password-resets/redeem", func(c fiber.Ctx) error {
		return c.SendStatus(http.StatusNoContent)
	})

	form := "password=PlaintextPassword123%21&confirm_password=PlaintextPassword123%21&token=plain-reset-token"
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/password-resets/redeem",
		strings.NewReader(form),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, "/api/v1/password-resets/redeem") {
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
	app.Put("/api/v1/days/:date", func(c fiber.Ctx) error {
		return c.SendStatus(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-17", nil)
	request.RemoteAddr = "203.0.113.9:43123"

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, "/api/v1/days/:date") {
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
	app.Put("/api/v1/days/:date", func(c fiber.Ctx) error {
		c.Response().Header.Set(fiber.HeaderRetryAfter, "60")
		logRateLimitHit(c)
		return c.SendStatus(http.StatusTooManyRequests)
	})

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/days/2026-02-17?token=plain-reset-token&email=user@example.com",
		strings.NewReader("email=user@example.com&password=PlaintextPassword123%21&token=plain-reset-token"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "203.0.113.9:43123"

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("rate limit request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, "path=/api/v1/days/:date") {
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

	handler := newRateLimitTestHandler(t)
	app := fiber.New()
	app.Use(csrf.New(csrfMiddlewareConfig(false, handler)))
	app.Post("/settings/change-password", func(c fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/settings/change-password?email=user@example.com",
		strings.NewReader("password=PlaintextPassword123%21"),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("csrf request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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
	app.Post("/api/v1/sessions", newAuthRateLimitHandler(handler, authRateLimitConfig{
		ErrorCode: "too_many_login_attempts",
	}))

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sessions?email=user@example.com",
		strings.NewReader("email=user@example.com&password=PlaintextPassword123%21"),
	)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("rate-limit handler request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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
	app.Get("/auth/oidc/start", func(c fiber.Ctx) error {
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
	app.Post(security.OIDCCallbackPath, func(c fiber.Ctx) error {
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

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("rate-limit request failed: %v", err)
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})
	return response
}

// TestCloseDatabaseClosesUnderlyingConnection verifies the close-on-exit
// path actually releases the database: after closeDatabase the underlying
// *sql.DB must reject further use, so SQLite has checkpointed its WAL and
// freed the file before process exit.
func TestCloseDatabaseClosesUnderlyingConnection(t *testing.T) {
	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "close-test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() unexpected error: %v", err)
	}

	closeDatabase(database)

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("database.DB() unexpected error: %v", err)
	}
	if err := sqlDB.Ping(); err == nil {
		t.Fatal("expected Ping to fail after closeDatabase, got nil")
	}

	// A second close must stay quiet (idempotent exit path).
	closeDatabase(database)
}

// TestRunServerReturnsListenError pins the failure exit path: when the listener
// cannot bind, runServer returns the error. runServer no longer closes the
// database itself (main sequences closeDatabase after the scheduler drain, on
// this same exit path); this test asserts the error is surfaced and that main's
// subsequent closeDatabase still checkpoints and closes it, so SQLite releases
// the file even on a failed start.
func TestRunServerReturnsListenError(t *testing.T) {
	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "runserver-err.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() unexpected error: %v", err)
	}
	app := fiber.New()

	if err := runServer(app, "256.256.256.256:0"); err == nil {
		t.Fatal("expected runServer to fail on an unbindable address")
	}

	// main closes the DB after runServer returns on both exit paths; mirror that.
	closeDatabase(database)

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("database.DB() unexpected error: %v", err)
	}
	if err := sqlDB.Ping(); err == nil {
		t.Fatal("expected database to be closed after main's post-runServer close")
	}
}

// TestRunServerReturnsAfterGracefulStop pins the graceful exit path: Listen
// returns nil after a graceful stop and hands control back to the caller. The
// stop is issued only after a full HTTP exchange against the bound listener
// completes. A bare dial is not enough: the kernel listener exists before
// Listen enters Serve, so a dial can succeed while fasthttp has no registered
// listener yet, and a Shutdown issued in that window returns nil without
// stopping anything (fasthttp's ShutdownWithContext no-ops when s.ln is nil) —
// the stop is lost and Listen hangs forever (the 30s CI flake). A served
// response proves the accept loop is running, which is the precondition for
// Shutdown to take effect. The DB close now happens in main after runServer
// returns; this test mirrors that final close and asserts the file is released.
func TestRunServerReturnsAfterGracefulStop(t *testing.T) {
	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "runserver-stop.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() unexpected error: %v", err)
	}
	app := fiber.New()

	addrCh := make(chan string, 1)
	app.Hooks().OnListen(func(listenData fiber.ListenData) error {
		addrCh <- net.JoinHostPort(listenData.Host, listenData.Port)
		return nil
	})
	go func() {
		address := <-addrCh
		for {
			conn, dialErr := net.Dial("tcp", address)
			if dialErr != nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			// HTTP/1.0 implies Connection: close, so the server ends the
			// exchange and io.Copy returns once the response is written.
			_, _ = conn.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
			served, _ := io.Copy(io.Discard, conn)
			_ = conn.Close()
			if served == 0 {
				// No response bytes: the accept loop may not be running
				// yet, so a stop now could be silently lost. Retry.
				time.Sleep(10 * time.Millisecond)
				continue
			}
			_ = app.Shutdown()
			return
		}
	}()

	done := make(chan error, 1)
	go func() { done <- runServer(app, "127.0.0.1:0") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServer after graceful stop: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runServer did not return within 30s of the graceful stop")
	}

	// main closes the DB after runServer returns (after the scheduler drain).
	closeDatabase(database)

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("database.DB() unexpected error: %v", err)
	}
	if err := sqlDB.Ping(); err == nil {
		t.Fatal("expected database to be closed after main's post-runServer close")
	}
}

// TestRetryShutdownBridgesBootWindow pins the boot-window hardening: a single
// app.ShutdownWithContext call is a silent no-op while fasthttp's s.ln is
// still nil (the gap between fiber's net.Listen and fasthttp's Serve
// registering the listener) — without a retry, Listen would serve forever
// and runServer would never return. retryShutdown is started against an
// app that has not been Listen'd yet, guaranteeing its first attempts land
// in that gap deterministically (no reliance on real signal timing, unlike
// the actual boot-window race), then Listen is started afterward; the retry
// loop must still notice and bridge the gap once Serve registers the
// listener.
func TestRetryShutdownBridgesBootWindow(t *testing.T) {
	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "boot-window-stop.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() unexpected error: %v", err)
	}
	app := fiber.New()

	served := make(chan struct{})
	go retryShutdown(app, context.Background(), served)

	// Give retryShutdown a few iterations against the not-yet-listening app,
	// so its early attempts are guaranteed genuine no-ops (s.ln == nil).
	time.Sleep(60 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		err := runServer(app, "127.0.0.1:0")
		close(served)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServer after boot-window stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServer did not return after a boot-window stop; retry did not bridge the gap")
	}

	// main closes the DB after runServer returns; mirror that final close.
	closeDatabase(database)

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("database.DB() unexpected error: %v", err)
	}
	if err := sqlDB.Ping(); err == nil {
		t.Fatal("expected database to be closed after main's post-runServer close")
	}
}

// TestRetryShutdownReturnsOnContextDeadline pins the ctx.Done() exit: a
// signal that arrives but never sees the server finish (served never
// closes) must still let retryShutdown return once its bounding context
// expires, rather than looping forever.
func TestRetryShutdownReturnsOnContextDeadline(t *testing.T) {
	app := fiber.New()
	served := make(chan struct{}) // deliberately never closed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		retryShutdown(app, ctx, served)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retryShutdown did not return after its context deadline expired")
	}
}

// TestRetryShutdownLogsPersistentShutdownError pins the error-return branch:
// once the shutdown call itself fails (in production, app.ShutdownWithContext
// returning its context error while a connection is still open), retryShutdown
// must log the failure and stop retrying rather than spinning on a shutdown
// that will never succeed.
//
// The shutdown func is injected (retryShutdownFunc, the exact loop production's
// retryShutdown runs) so the error is produced deterministically. The previous
// version drove a real listener + a raw connection and relied on fasthttp still
// counting that connection as open at the instant of the stop call to force the
// error; under a loaded runner the stop could win the race against fasthttp's
// accept loop, leave open == 0, and return nil — no error, no log — flaking the
// assertion. A stub shutdown removes that timing dependence entirely while still
// exercising the real loop: it no-ops twice (the boot-window path) before
// failing, proving retryShutdown retries, then logs and terminates on the error.
func TestRetryShutdownLogsPersistentShutdownError(t *testing.T) {
	var buffer bytes.Buffer
	log.SetOutput(&buffer)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	shutdownErr := errors.New("shutdown context deadline exceeded")
	var calls int
	shutdown := func(context.Context) error {
		calls++
		if calls <= 2 {
			return nil // boot-window no-op: keep retrying
		}
		return shutdownErr
	}

	served := make(chan struct{}) // never closed: only the error may end the loop
	done := make(chan struct{})
	go func() {
		defer close(done)
		// A tiny interval keeps the two no-op retries instant; the loop still
		// exits on the injected error, not on the tick or the context.
		retryShutdownFunc(shutdown, context.Background(), served, time.Millisecond)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("retryShutdown did not terminate after a persistent shutdown error")
	}

	if calls != 3 {
		t.Fatalf("shutdown call count = %d, want 3 (two boot-window no-ops then the failure)", calls)
	}
	if got := buffer.String(); !strings.Contains(got, "server shutdown failed") ||
		!strings.Contains(got, shutdownErr.Error()) {
		t.Fatalf("expected a logged shutdown failure carrying %q, got %q", shutdownErr, got)
	}
}

// TestInstallGracefulShutdownReturnsSignalContextAndStop covers the wiring
// contract cross-platform (the SIGTERM self-delivery test below is Linux-only):
// installGracefulShutdown returns a non-nil signal context (observed by the
// reminder scheduler) plus a stop function, and calling the stop function
// cancels that context and unblocks the internal goroutine. served is closed up
// front so the goroutine's retryShutdown returns promptly once the stop fires.
func TestInstallGracefulShutdownReturnsSignalContextAndStop(t *testing.T) {
	app := fiber.New()
	served := make(chan struct{})
	close(served) // retryShutdown returns as soon as it observes served closed

	sigCtx, stopSignals := installGracefulShutdown(app, served)
	if sigCtx == nil {
		t.Fatal("expected a non-nil signal context for the scheduler to observe")
	}
	if stopSignals == nil {
		t.Fatal("expected a non-nil stop function")
	}

	select {
	case <-sigCtx.Done():
		t.Fatal("signal context should still be live before stop is called or a signal arrives")
	default:
	}

	stopSignals() // cancels sigCtx and unblocks the internal goroutine

	select {
	case <-sigCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("stop function did not cancel the signal context")
	}
}

// TestInstallGracefulShutdownBridgesSIGTERM pins the signal-wiring itself:
// a real SIGTERM delivered to the process must reach retryShutdown through
// installGracefulShutdown's goroutine, not just via a direct call to
// retryShutdown (as the other regressions in this file exercise). os.Process
// only supports delivering os.Kill on windows (any other signal, including
// SIGTERM, returns "not supported by windows"), so this is validated on
// Linux (dev container / CI) and skipped on windows.
func TestInstallGracefulShutdownBridgesSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM self-delivery is not supported by the Go runtime on windows; validated in Linux CI")
	}

	database, err := db.OpenSQLite(filepath.Join(t.TempDir(), "sigterm-stop.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() unexpected error: %v", err)
	}
	app := fiber.New()

	served := make(chan struct{})
	_, stopSignals := installGracefulShutdown(app, served)
	defer stopSignals()

	addrCh := make(chan string, 1)
	app.Hooks().OnListen(func(listenData fiber.ListenData) error {
		addrCh <- net.JoinHostPort(listenData.Host, listenData.Port)
		return nil
	})

	done := make(chan error, 1)
	go func() {
		err := runServer(app, "127.0.0.1:0")
		close(served)
		done <- err
	}()
	<-addrCh

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("os.FindProcess() unexpected error: %v", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("failed to deliver SIGTERM to self: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServer after SIGTERM: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runServer did not return after SIGTERM within 15s")
	}

	// main closes the DB after runServer returns; mirror that final close.
	closeDatabase(database)

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("database.DB() unexpected error: %v", err)
	}
	if err := sqlDB.Ping(); err == nil {
		t.Fatal("expected database to be closed after a SIGTERM-triggered stop")
	}
}

// TestCloseDatabaseLogsWhenPoolUnavailable covers the defensive branch: a
// gorm handle without a connection pool cannot be closed, and closeDatabase
// must log instead of panicking.
func TestCloseDatabaseLogsWhenPoolUnavailable(t *testing.T) {
	var buffer bytes.Buffer
	log.SetOutput(&buffer)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	closeDatabase(&gorm.DB{Config: &gorm.Config{}})

	if !strings.Contains(buffer.String(), "database close:") {
		t.Fatalf("expected close failure to be logged, got %q", buffer.String())
	}
}

// testConfigNoTimeout restores fiber v2's app.Test(req, -1) "no timeout"
// semantics: v3's default TestConfig times out after 1s, which bcrypt-heavy
// tests exceed under coverage instrumentation.
var testConfigNoTimeout = fiber.TestConfig{Timeout: 0, FailOnTimeout: false}

// buildInfoWithSettings assembles the minimal debug.BuildInfo shape the
// asset-version helpers read: only Settings matters to them.
func buildInfoWithSettings(settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{Settings: settings}
}

func TestVCSRevisionFromBuildInfo(t *testing.T) {
	tests := []struct {
		name         string
		info         *debug.BuildInfo
		wantRevision string
		wantModified bool
	}{
		{name: "nil info", info: nil, wantRevision: "", wantModified: false},
		{name: "no settings", info: buildInfoWithSettings(), wantRevision: "", wantModified: false},
		{
			name:         "clean revision",
			info:         buildInfoWithSettings(debug.BuildSetting{Key: "vcs.revision", Value: "abcdef1234567890"}),
			wantRevision: "abcdef1234567890",
			wantModified: false,
		},
		{
			name: "blank revision value is unusable",
			info: buildInfoWithSettings(
				debug.BuildSetting{Key: "vcs.revision", Value: "   "},
				debug.BuildSetting{Key: "vcs.modified", Value: "false"},
			),
			wantRevision: "",
			wantModified: false,
		},
		{
			name: "modified true",
			info: buildInfoWithSettings(
				debug.BuildSetting{Key: "vcs.revision", Value: "abcdef1234567890"},
				debug.BuildSetting{Key: "vcs.modified", Value: "true"},
			),
			wantRevision: "abcdef1234567890",
			wantModified: true,
		},
		{
			name: "modified flag tolerates surrounding space",
			info: buildInfoWithSettings(
				debug.BuildSetting{Key: "vcs.revision", Value: "abcdef1234567890"},
				debug.BuildSetting{Key: "vcs.modified", Value: " true "},
			),
			wantRevision: "abcdef1234567890",
			wantModified: true,
		},
		{
			name: "unrelated settings ignored",
			info: buildInfoWithSettings(
				debug.BuildSetting{Key: "-ldflags", Value: "-s -w"},
				debug.BuildSetting{Key: "vcs.time", Value: "2026-07-05T00:00:00Z"},
			),
			wantRevision: "",
			wantModified: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			revision, modified := vcsRevisionFromBuildInfo(tt.info)
			if revision != tt.wantRevision || modified != tt.wantModified {
				t.Fatalf("vcsRevisionFromBuildInfo() = (%q, %t), want (%q, %t)",
					revision, modified, tt.wantRevision, tt.wantModified)
			}
		})
	}
}

func TestAssetCacheBustTokenFallbackChain(t *testing.T) {
	fullRevision := "0123456789abcdef0123456789abcdef01234567"
	processStart := time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)
	infoWithRevision := buildInfoWithSettings(debug.BuildSetting{Key: "vcs.revision", Value: fullRevision})

	tests := []struct {
		name           string
		ldflagsVersion string
		info           *debug.BuildInfo
		want           string
	}{
		{
			name:           "ldflags version wins over VCS revision",
			ldflagsVersion: "v1.7.0",
			info:           infoWithRevision,
			want:           "v1.7.0",
		},
		{
			name:           "whitespace-only ldflags version falls through to VCS",
			ldflagsVersion: "   ",
			info:           infoWithRevision,
			want:           fullRevision[:assetVersionShortRevisionLength],
		},
		{
			name: "clean revision is shortened",
			info: infoWithRevision,
			want: fullRevision[:assetVersionShortRevisionLength],
		},
		{
			name: "revision shorter than the cap is kept whole",
			info: buildInfoWithSettings(debug.BuildSetting{Key: "vcs.revision", Value: "abc123"}),
			want: "abc123",
		},
		{
			name: "dirty revision keeps the marker within the api token cap",
			info: buildInfoWithSettings(
				debug.BuildSetting{Key: "vcs.revision", Value: fullRevision},
				debug.BuildSetting{Key: "vcs.modified", Value: "true"},
			),
			want: fullRevision[:assetVersionShortRevisionLength] + "-dirty",
		},
		{
			name: "nil build info falls back to process start",
			info: nil,
			want: "dev-" + strconv.FormatInt(processStart.Unix(), 10),
		},
		{
			name: "build info without VCS settings falls back to process start",
			info: buildInfoWithSettings(debug.BuildSetting{Key: "-ldflags", Value: "-s -w"}),
			want: "dev-" + strconv.FormatInt(processStart.Unix(), 10),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := assetCacheBustToken(tt.ldflagsVersion, tt.info, processStart)
			if token != tt.want {
				t.Fatalf("assetCacheBustToken(%q, ...) = %q, want %q", tt.ldflagsVersion, token, tt.want)
			}
		})
	}
}

// TestAssetCacheBustTokenChangesAcrossProcessStarts pins the reason the
// timestamp fallback exists: two source-only deployments (no ldflags, no VCS
// stamping — `go run`'s case) must not share one constant token, or assets
// cached against the first deployment survive the second.
func TestAssetCacheBustTokenChangesAcrossProcessStarts(t *testing.T) {
	first := assetCacheBustToken("", nil, time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC))
	second := assetCacheBustToken("", nil, time.Date(2026, time.July, 5, 12, 0, 1, 0, time.UTC))
	if first == second {
		t.Fatalf("tokens for distinct process starts must differ, both = %q", first)
	}
}

// TestResolveAssetVersionNeverUnknown locks the composition-root behavior the
// fallback chain was added for: whatever environment the binary finds itself
// in, the cache-bust token is non-empty and never the shared constant
// "unknown" that pre-fallback from-source builds served.
func TestResolveAssetVersionNeverUnknown(t *testing.T) {
	token := resolveAssetVersion()
	if token == "" || token == "unknown" {
		t.Fatalf("resolveAssetVersion() = %q, want a non-empty, non-\"unknown\" token", token)
	}
}
