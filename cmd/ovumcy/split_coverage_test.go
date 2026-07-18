package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
)

// setValidBootEnv installs a minimal, valid runtime environment so a
// loadRuntimeConfig call succeeds; individual tests then break one variable to
// exercise a specific error branch.
func setValidBootEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SECRET_KEY_FILE", "")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", "data/ovumcy.db")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PORT", "8080")
	t.Setenv("TRUST_PROXY_ENABLED", "false")
	t.Setenv("REGISTRATION_MODE", "closed")
	t.Setenv("OIDC_ENABLED", "false")
}

func TestGetEnvFallbacksOnInvalidValue(t *testing.T) {
	t.Setenv("OVUMCY_TEST_INT", "not-a-number")
	if got := getEnvInt("OVUMCY_TEST_INT", 7); got != 7 {
		t.Fatalf("getEnvInt: expected fallback 7, got %d", got)
	}

	t.Setenv("OVUMCY_TEST_DURATION", "not-a-duration")
	if got := getEnvDuration("OVUMCY_TEST_DURATION", 30*time.Second); got != 30*time.Second {
		t.Fatalf("getEnvDuration: expected fallback 30s, got %s", got)
	}

	t.Setenv("OVUMCY_TEST_BOOL", "maybe")
	if got := getEnvBool("OVUMCY_TEST_BOOL", true); got != true {
		t.Fatalf("getEnvBool: expected fallback true, got %t", got)
	}
}

func TestMustLoadLocationFallsBackToUTC(t *testing.T) {
	if loc := mustLoadLocation("Definitely/Not-A-Zone"); loc != time.UTC {
		t.Fatalf("expected UTC fallback for an invalid TZ, got %v", loc)
	}
}

func TestResolveProxySettingsDefaultsHeaderWhenBlank(t *testing.T) {
	t.Setenv("TRUST_PROXY_ENABLED", "true")
	t.Setenv("PROXY_HEADER", "   ")
	t.Setenv("TRUSTED_PROXIES", "127.0.0.1")

	settings, err := resolveProxySettings()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if settings.Header != fiber.HeaderXForwardedFor {
		t.Fatalf("expected blank PROXY_HEADER to default to %q, got %q", fiber.HeaderXForwardedFor, settings.Header)
	}
}

func TestLoadRuntimeConfigRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T)
	}{
		{"invalid secret key", func(t *testing.T) { t.Setenv("SECRET_KEY", "too-short") }},
		{"invalid database config", func(t *testing.T) { t.Setenv("DB_DRIVER", "bogus") }},
		{"invalid port", func(t *testing.T) { t.Setenv("PORT", "70000") }},
		{"invalid proxy config", func(t *testing.T) {
			t.Setenv("TRUST_PROXY_ENABLED", "true")
			// A non-empty value that parseCSV reduces to zero entries: an empty
			// string would fall back to the default TRUSTED_PROXIES via getEnv.
			t.Setenv("TRUSTED_PROXIES", ",")
		}},
		{"invalid registration mode", func(t *testing.T) { t.Setenv("REGISTRATION_MODE", "bogus") }},
		{"invalid oidc config", func(t *testing.T) {
			t.Setenv("OIDC_ENABLED", "true")
			t.Setenv("OIDC_ISSUER_URL", "")
			t.Setenv("OIDC_CLIENT_ID", "")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setValidBootEnv(t)
			tc.mutate(t)
			if _, err := loadRuntimeConfig(time.UTC); err == nil {
				t.Fatalf("expected loadRuntimeConfig to reject %s", tc.name)
			}
		})
	}
}

func TestMustLoadRuntimeConfigReturnsConfigOnValidEnv(t *testing.T) {
	setValidBootEnv(t)
	config := mustLoadRuntimeConfig(time.UTC)
	if config.Port != "8080" {
		t.Fatalf("expected port 8080, got %q", config.Port)
	}
}

func TestTryRunCLICommandWithHandlersReturnsUnhandledForNoOrUnknownArgs(t *testing.T) {
	if handled, err := tryRunCLICommandWithHandlers(nil, cliCommandHandlers{}); handled || err != nil {
		t.Fatalf("empty args: expected (false, nil), got (%t, %v)", handled, err)
	}
	if handled, err := tryRunCLICommandWithHandlers([]string{"totally-unknown"}, cliCommandHandlers{}); handled || err != nil {
		t.Fatalf("unknown command: expected (false, nil), got (%t, %v)", handled, err)
	}
}

func TestTryRunCLICommandWithHandlersResetPassword(t *testing.T) {
	t.Run("rejects wrong argument count", func(t *testing.T) {
		handled, err := tryRunCLICommandWithHandlers([]string{"reset-password"}, cliCommandHandlers{})
		if !handled || err == nil {
			t.Fatalf("expected handled error, got (%t, %v)", handled, err)
		}
	})

	t.Run("requires a handler", func(t *testing.T) {
		handled, err := tryRunCLICommandWithHandlers([]string{"reset-password", "owner@example.com"}, cliCommandHandlers{})
		if !handled || err == nil {
			t.Fatalf("expected handled error, got (%t, %v)", handled, err)
		}
	})

	t.Run("reports invalid database config", func(t *testing.T) {
		setValidBootEnv(t)
		t.Setenv("DB_DRIVER", "bogus")
		handled, err := tryRunCLICommandWithHandlers([]string{"reset-password", "owner@example.com"}, cliCommandHandlers{
			runResetPassword: func(db.Config, string) error { return nil },
		})
		if !handled || err == nil {
			t.Fatalf("expected handled db-config error, got (%t, %v)", handled, err)
		}
	})

	t.Run("dispatches to the handler", func(t *testing.T) {
		setValidBootEnv(t)
		called := false
		handled, err := tryRunCLICommandWithHandlers([]string{"reset-password", "owner@example.com"}, cliCommandHandlers{
			runResetPassword: func(_ db.Config, email string) error {
				called = true
				if email != "owner@example.com" {
					t.Fatalf("unexpected email %q", email)
				}
				return nil
			},
		})
		if !handled || err != nil {
			t.Fatalf("expected clean dispatch, got (%t, %v)", handled, err)
		}
		if !called {
			t.Fatal("expected the reset-password handler to be invoked")
		}
	})
}

func TestTryRunCLICommandWithHandlersUsersAndHealthcheckErrorBranches(t *testing.T) {
	t.Run("users requires a handler", func(t *testing.T) {
		handled, err := tryRunCLICommandWithHandlers([]string{"users", "list"}, cliCommandHandlers{})
		if !handled || err == nil {
			t.Fatalf("expected handled error, got (%t, %v)", handled, err)
		}
	})

	t.Run("users reports invalid database config", func(t *testing.T) {
		setValidBootEnv(t)
		t.Setenv("DB_DRIVER", "bogus")
		handled, err := tryRunCLICommandWithHandlers([]string{"users", "list"}, cliCommandHandlers{
			runUsers: func(db.Config, []string) error { return errors.New("unreached") },
		})
		if !handled || err == nil {
			t.Fatalf("expected handled db-config error, got (%t, %v)", handled, err)
		}
	})

	t.Run("healthcheck requires a handler", func(t *testing.T) {
		handled, err := tryRunCLICommandWithHandlers([]string{"healthcheck"}, cliCommandHandlers{})
		if !handled || err == nil {
			t.Fatalf("expected handled error, got (%t, %v)", handled, err)
		}
	})

	t.Run("healthcheck reports invalid port", func(t *testing.T) {
		setValidBootEnv(t)
		t.Setenv("PORT", "70000")
		handled, err := tryRunCLICommandWithHandlers([]string{"healthcheck"}, cliCommandHandlers{
			runHealthcheck: func(string, time.Duration) error { return nil },
		})
		if !handled || err == nil {
			t.Fatalf("expected handled port error, got (%t, %v)", handled, err)
		}
	})
}

func TestIsV1AuthPathAcceptsRedeem(t *testing.T) {
	if !isV1AuthPath("/api/v1/password-resets/redeem") {
		t.Fatal("expected /api/v1/password-resets/redeem to be a v1 auth path")
	}
}

func TestRateLimitScopeClassifiesAuthPath(t *testing.T) {
	app := fiber.New()
	app.Get("/api/v1/sessions", func(c fiber.Ctx) error {
		return c.SendString(rateLimitScope(c))
	})

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil))
	if err != nil {
		t.Fatalf("probe request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if got := string(body); got != "auth" {
		t.Fatalf("expected rate-limit scope \"auth\", got %q", got)
	}
}
