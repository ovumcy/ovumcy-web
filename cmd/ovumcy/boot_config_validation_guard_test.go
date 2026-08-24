package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

// Boot-time refusal guards for the two runtime-config values an operator can
// mistype into a silently degraded security posture.
//
// Deliberately narrow: only TRUSTED_PROXIES and the four security-relevant
// booleans (COOKIE_SECURE, HSTS_ENABLED, TRUST_PROXY_ENABLED,
// WEBHOOK_BLOCK_PRIVATE_ADDRESSES) refuse the boot. The lenient getEnvBool /
// getEnvInt / getEnvDuration fallback still governs every other key, so nothing
// here may be read as "every invalid env value stops the process".

// TestResolveProxySettingsRejectsAMalformedTrustedProxyEntry pins that a
// TRUSTED_PROXIES entry the trusted-proxy matcher could never use refuses the
// boot instead of being dropped. An unparseable CIDR was discarded and a non-IP
// literal was stored as an exact match nothing can equal, so the proxy stayed
// untrusted and every client behind it shared one rate-limit bucket.
func TestResolveProxySettingsRejectsAMalformedTrustedProxyEntry(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		rejected string
	}{
		{"cidr typo", "10.0.0.0/8x", "10.0.0.0/8x"},
		{"non-ip literal", "192.168.0.0/16,bogus", "bogus"},
		{"out-of-range octet", "127.0.0.256", "127.0.0.256"},
		{"host bits without a mask width", "10.0.0.1/", "10.0.0.1/"},
		// Parses as an IP, but the matcher keys its exact set on the entry as
		// written and looks up net.IP.String(), so a non-canonical spelling is
		// an entry contains() can never match — the same silent miss as "bogus".
		{"non-canonical ipv6", "2001:DB8::1", "2001:DB8::1"},
		{"ipv4-mapped ipv6", "::ffff:10.0.0.1", "::ffff:10.0.0.1"},
		// Both spellings are well formed, so only the repetition itself is the
		// defect: the matcher's exact set collapses them and the banner counts
		// a trusted proxy the matcher does not hold.
		{"repeated ip", "203.0.113.7,203.0.113.7", "203.0.113.7"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRUST_PROXY_ENABLED", "true")
			t.Setenv("PROXY_HEADER", "X-Real-IP")
			t.Setenv("TRUSTED_PROXIES", tc.value)

			_, err := resolveProxySettings()
			if err == nil {
				t.Fatalf("expected TRUSTED_PROXIES=%q to refuse the boot", tc.value)
			}
			if !strings.Contains(err.Error(), "TRUSTED_PROXIES") || !strings.Contains(err.Error(), tc.rejected) {
				t.Fatalf("expected the error to name TRUSTED_PROXIES and the rejected entry %q, got: %v", tc.rejected, err)
			}
		})
	}

	// Positive anchor: a well-formed list of both shapes still boots, so the
	// cases above fail on the malformed entry and not on the check itself.
	t.Run("well-formed list still boots", func(t *testing.T) {
		t.Setenv("TRUST_PROXY_ENABLED", "true")
		t.Setenv("PROXY_HEADER", "X-Real-IP")
		t.Setenv("TRUSTED_PROXIES", "127.0.0.1, ::1 ,10.0.0.0/8,2001:db8::/32")

		settings, err := resolveProxySettings()
		if err != nil {
			t.Fatalf("expected a well-formed TRUSTED_PROXIES list to be accepted, got: %v", err)
		}
		if len(settings.TrustedProxies) != 4 {
			t.Fatalf("expected 4 trusted proxy entries, got %#v", settings.TrustedProxies)
		}
	})
}

// TestTrustedProxyCountTheBannerPrintsMatchesTheMatcher pins the disagreement
// R2-0009 rests on: logStartup reports trusted_proxy_count=len(TrustedProxies)
// — the raw CSV — while the rate-limit key generator only ever consults what
// newTrustedProxyMatcher managed to parse. With "10.0.0.0/8x,192.168.0.0/16,
// bogus" the banner said 3 and the matcher held 2, so the startup line that
// exists to confirm the setting agreed with the operator that the typo was
// fine. Either the boot is refused (the banner never prints) or the two counts
// agree; nothing in between.
func TestTrustedProxyCountTheBannerPrintsMatchesTheMatcher(t *testing.T) {
	values := []string{
		"127.0.0.1,::1",
		"10.0.0.0/8x,192.168.0.0/16,bogus",
		"203.0.113.7,10.0.0.0/8",
		// A repeated entry is well-formed twice over, so nothing above refuses
		// it — yet the matcher keys its exact set by the entry and collapses the
		// pair into one, which is the same banner-vs-matcher lie as a typo.
		"203.0.113.7,203.0.113.7",
		// Scoping anchor for the duplicate check: ranges are appended to a
		// slice rather than keyed, so a repeated CIDR keeps both copies and the
		// two counts already agree — it must still boot.
		"10.0.0.0/8,10.0.0.0/8",
	}

	accepted := 0
	for _, value := range values {
		t.Setenv("TRUST_PROXY_ENABLED", "true")
		t.Setenv("PROXY_HEADER", "X-Real-IP")
		t.Setenv("TRUSTED_PROXIES", value)

		settings, err := resolveProxySettings()
		if err != nil {
			continue // refused at boot: logStartup never runs, so no count is printed
		}
		accepted++

		matcher := newTrustedProxyMatcher(settings.TrustedProxies)
		banner := len(settings.TrustedProxies)
		usable := len(matcher.exact) + len(matcher.ranges)
		if banner != usable {
			t.Fatalf("TRUSTED_PROXIES=%q: banner would print trusted_proxy_count=%d while the matcher holds %d usable entries", value, banner, usable)
		}
	}

	// Anti-vacuity: the two well-formed lists and the repeated-CIDR list must
	// have been accepted, so the comparison above actually ran — and a blanket
	// duplicate check that also refused the CIDR pair would fail here rather
	// than pass by refusing everything.
	if accepted < 3 {
		t.Fatalf("expected the two well-formed lists and the repeated-CIDR list to be accepted, got %d", accepted)
	}
}

// TestBootRefusesAnUnparseableSecurityBoolean pins that a typo in one of the
// four security-relevant booleans stops the process with an error naming the
// key and the rejected value, instead of silently running on the fallback:
// COOKIE_SECURE=ture used to yield cookies without the Secure flag and a boot
// that looked normal.
func TestBootRefusesAnUnparseableSecurityBoolean(t *testing.T) {
	cases := []struct {
		key   string
		value string
	}{
		{"COOKIE_SECURE", "ture"},
		{"HSTS_ENABLED", "yess"},
		{"TRUST_PROXY_ENABLED", "tru"},
		{"WEBHOOK_BLOCK_PRIVATE_ADDRESSES", "onn"},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			setValidBootEnv(t)
			t.Setenv(tc.key, tc.value)

			_, err := loadRuntimeConfig(time.UTC)
			if err == nil {
				t.Fatalf("expected boot to refuse %s=%q", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), tc.key) || !strings.Contains(err.Error(), tc.value) {
				t.Fatalf("expected the error to name %s and the rejected value %q, got: %v", tc.key, tc.value, err)
			}
		})
	}

	// WEBHOOK_BLOCK_PRIVATE_ADDRESSES is read twice — once at server boot and
	// once by the `notify` CLI pass that actually makes the outbound calls. The
	// two must refuse the same value, or the egress gate is strict where it is
	// only recorded and lenient where it is enforced.
	t.Run("notify CLI refuses the same unparseable egress gate", func(t *testing.T) {
		setValidBootEnv(t)
		t.Setenv("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", "onn")

		called := false
		handled, err := tryRunCLICommandWithHandlers([]string{"notify"}, cliCommandHandlers{
			runNotify: func(db.Config, string, string, *time.Location, bool, []string) error {
				called = true
				return nil
			},
		})
		if !handled || err == nil {
			t.Fatalf("expected notify to refuse WEBHOOK_BLOCK_PRIVATE_ADDRESSES=%q, got (%t, %v)", "onn", handled, err)
		}
		if called {
			t.Fatal("expected notify to refuse before running a delivery pass")
		}
		if !strings.Contains(err.Error(), "WEBHOOK_BLOCK_PRIVATE_ADDRESSES") || !strings.Contains(err.Error(), "onn") {
			t.Fatalf("expected the error to name the key and the rejected value, got: %v", err)
		}
	})

	t.Run("notify CLI still runs on a well-formed egress gate", func(t *testing.T) {
		setValidBootEnv(t)
		t.Setenv("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", "on")

		blocked := false
		handled, err := tryRunCLICommandWithHandlers([]string{"notify"}, cliCommandHandlers{
			runNotify: func(_ db.Config, _ string, _ string, _ *time.Location, blockPrivate bool, _ []string) error {
				blocked = blockPrivate
				return nil
			},
		})
		if !handled || err != nil {
			t.Fatalf("expected notify to run, got (%t, %v)", handled, err)
		}
		if !blocked {
			t.Fatal("expected WEBHOOK_BLOCK_PRIVATE_ADDRESSES=on to reach the delivery pass as true")
		}
	})

	// Positive anchor: the same keys spelled correctly boot and land on the
	// values the operator asked for.
	t.Run("correctly spelled values still boot", func(t *testing.T) {
		setValidBootEnv(t)
		t.Setenv("COOKIE_SECURE", "true")
		t.Setenv("HSTS_ENABLED", "off")
		t.Setenv("TRUST_PROXY_ENABLED", "yes")
		t.Setenv("TRUSTED_PROXIES", "127.0.0.1")
		t.Setenv("WEBHOOK_BLOCK_PRIVATE_ADDRESSES", "1")

		config, err := loadRuntimeConfig(time.UTC)
		if err != nil {
			t.Fatalf("expected well-formed booleans to be accepted, got: %v", err)
		}
		if !config.CookieSecure || config.HSTSEnabled || !config.Proxy.Enabled || !config.WebhookBlockPrivate {
			t.Fatalf("unexpected parsed booleans: cookie_secure=%t hsts=%t trust_proxy=%t webhook_block_private=%t",
				config.CookieSecure, config.HSTSEnabled, config.Proxy.Enabled, config.WebhookBlockPrivate)
		}
	})
}
