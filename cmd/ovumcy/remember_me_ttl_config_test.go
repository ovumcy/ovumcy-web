package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestLoadRuntimeConfigReadsRememberMeTTLDays(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{value: "", want: api.RememberMeTTLDaysDefault},
		{value: " 365 ", want: 365},
		{value: "8", want: 8},
	} {
		t.Run("accepts "+tc.value, func(t *testing.T) {
			minimalRuntimeEnv(t)
			t.Setenv("REMEMBER_ME_TTL_DAYS", tc.value)
			config, err := loadRuntimeConfig(time.UTC)
			if err != nil {
				t.Fatalf("load runtime config: %v", err)
			}
			if config.RememberMeTTLDays != tc.want {
				t.Fatalf("RememberMeTTLDays = %d, want %d", config.RememberMeTTLDays, tc.want)
			}
		})
	}

	// A fallback would hand an operator who mistyped a shorter lifetime the
	// 30-day default, so a set value that is not a day count in range refuses
	// the boot and names itself.
	for _, value := range []string{"7", "0", "366", "365d", "thirty"} {
		t.Run("refuses "+value, func(t *testing.T) {
			minimalRuntimeEnv(t)
			t.Setenv("REMEMBER_ME_TTL_DAYS", value)
			_, err := loadRuntimeConfig(time.UTC)
			if err == nil {
				t.Fatalf("REMEMBER_ME_TTL_DAYS=%q booted", value)
			}
			if !strings.Contains(err.Error(), "REMEMBER_ME_TTL_DAYS") || !strings.Contains(err.Error(), value) {
				t.Fatalf("the refusal does not name the key and value: %v", err)
			}
		})
	}
}

// newHandlerFromConfig is the constructor the binary runs. A version that
// dropped the lifetime on the way would accept an out-of-range value too, so
// the refusal beside the acceptance is what proves the value got there.
func TestNewHandlerFromConfigHandsTheLifetimeToTheHandler(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "ovumcy.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	// Close before TempDir's cleanup runs: Windows refuses to remove an open file.
	if sqlDB, err := database.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	i18nManager := newRateLimitTestI18nManager(t)
	dependencies := bootstrap.BuildDependencies(db.NewRepositories(database), []byte(rateLimitTestSecretKey), i18nManager, bootstrap.Options{
		RegistrationMode: services.RegistrationModeOpen,
		LoginAttempts:    bootstrap.AttemptLimit{Max: 8, Window: 15 * time.Minute},
		RecoveryAttempts: bootstrap.AttemptLimit{Max: 8, Window: time.Hour},
		LogoutAttempts:   &bootstrap.AttemptLimit{},
	})
	config := runtimeConfig{SecretKey: rateLimitTestHandlerSecretKey, Location: time.UTC}

	config.RememberMeTTLDays = api.RememberMeTTLDaysMax
	if _, err := newHandlerFromConfig(config, i18nManager, dependencies); err != nil {
		t.Fatalf("%d days refused: %v", config.RememberMeTTLDays, err)
	}
	config.RememberMeTTLDays = api.RememberMeTTLDaysMin - 1
	if _, err := newHandlerFromConfig(config, i18nManager, dependencies); err == nil {
		t.Fatal("an out-of-range lifetime was accepted: the value is not reaching SetRememberMeTTLDays")
	}
	config.RememberMeTTLDays = api.RememberMeTTLDaysDefault
	config.SecretKey = ""
	if _, err := newHandlerFromConfig(config, i18nManager, dependencies); err == nil {
		t.Fatal("a handler without a secret key was built: api.NewHandler's refusal is not passed on")
	}
}
