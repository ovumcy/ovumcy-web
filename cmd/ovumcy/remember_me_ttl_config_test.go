package main

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/api"
)

func TestLoadRuntimeConfigReadsRememberMeTTLDays(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		{value: "", want: api.RememberMeTTLDaysDefault},
		{value: "365", want: 365},
		{value: "8", want: 8},
		// Out of range or not a day count: the default, never a clamp.
		{value: "7", want: api.RememberMeTTLDaysDefault},
		{value: "366", want: api.RememberMeTTLDaysDefault},
		{value: "365d", want: api.RememberMeTTLDaysDefault},
	}
	for _, tc := range cases {
		t.Run("REMEMBER_ME_TTL_DAYS="+tc.value, func(t *testing.T) {
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
}

// applyRuntimeSessionSettings is the one place the boot hands the value to the
// handler. A version that dropped it would accept an out-of-range value too,
// so the refusal is what proves the value got there.
func TestApplyRuntimeSessionSettingsHandsTheLifetimeToTheHandler(t *testing.T) {
	handler := newRateLimitTestHandler(t)
	if err := applyRuntimeSessionSettings(handler, runtimeConfig{RememberMeTTLDays: 365}); err != nil {
		t.Fatalf("365 days refused: %v", err)
	}
	if err := applyRuntimeSessionSettings(handler, runtimeConfig{RememberMeTTLDays: api.RememberMeTTLDaysMin - 1}); err == nil {
		t.Fatal("an out-of-range lifetime was accepted: the value is not reaching SetRememberMeTTLDays")
	}
}
