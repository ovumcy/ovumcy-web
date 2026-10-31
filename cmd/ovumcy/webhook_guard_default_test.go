package main

import (
	"testing"
	"time"
)

// TestLoadRuntimeConfigWebhookGuardFollowsRegistrationMode pins WEB-398 on the
// server path: an unset WEBHOOK_BLOCK_PRIVATE_ADDRESSES turns the private-address
// guard on under open registration and leaves it off under closed, and
// an explicit value wins either way. The notify CLI half is
// TestTryRunCLICommandWithHandlersNotifyParsesWebhookBlockPrivateAddresses.
func TestLoadRuntimeConfigWebhookGuardFollowsRegistrationMode(t *testing.T) {
	cases := []struct {
		registration string
		value        string
		want         bool
	}{
		{"open", "", true},
		{"", "", true},
		{"closed", "", false},
		{"open", "false", false},
		{"closed", "true", true},
	}
	for _, tc := range cases {
		t.Run(tc.registration+"/"+tc.value, func(t *testing.T) {
			setValidBootEnv(t)
			t.Setenv("REGISTRATION_MODE", tc.registration)
			t.Setenv("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", tc.value)

			config, err := loadRuntimeConfig(time.UTC)
			if err != nil {
				t.Fatalf("loadRuntimeConfig: %v", err)
			}
			if config.WebhookBlockPrivate != tc.want {
				t.Fatalf("REGISTRATION_MODE=%q WEBHOOK_BLOCK_PRIVATE_ADDRESSES=%q: guard=%t, want %t",
					tc.registration, tc.value, config.WebhookBlockPrivate, tc.want)
			}
		})
	}
}
