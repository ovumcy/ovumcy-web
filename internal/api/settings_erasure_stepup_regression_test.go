package api

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// seedErasureDayEntry writes one day entry directly, so the erasure tests have
// a record whose survival is observable without driving the day API.
func seedErasureDayEntry(t *testing.T, ctx settingsSecurityTestContext) {
	t.Helper()

	entry := models.DailyLog{
		UserID: ctx.user.ID,
		Date:   time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
	}
	if err := ctx.database.Create(&entry).Error; err != nil {
		t.Fatalf("seed day entry: %v", err)
	}
}

func countErasureDayEntries(t *testing.T, ctx settingsSecurityTestContext) int64 {
	t.Helper()

	var count int64
	if err := ctx.database.Model(&models.DailyLog{}).Where("user_id = ?", ctx.user.ID).Count(&count).Error; err != nil {
		t.Fatalf("count day entries: %v", err)
	}
	return count
}

// Erasure on an account with no local password is confirmed at the identity
// provider instead of with a password. The two tests below pin the halves that
// carry the security weight:
//
//   - the start endpoints refuse an account that HAS a local password, so the
//     SSO route can never stand in for a password gate that applies, and
//   - starting the flow erases nothing by itself: the wipe happens only after
//     the provider callback, so a request that stops at the start endpoint must
//     leave the account's data untouched.

// TestErasureStepupRefusesAccountsThatHaveALocalPassword is the no-downgrade
// gate. An account holding a local password confirms an erasure with that
// password; if the step-up route accepted it too, a hijacked session on such an
// account with a linked identity could erase everything without ever knowing
// the password — the SSO path would become a way AROUND the password gate
// rather than a substitute for owners who have none.
//
// The OIDC-only positive anchor in the neighbouring test is what proves this
// refusal is about the local password and not about the route being dead.
func TestErasureStepupRefusesAccountsThatHaveALocalPassword(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "settings-erasure-stepup-has-password@example.com")

	for _, testCase := range []struct {
		name string
		path string
	}{
		{name: "clear data", path: "/api/v1/users/current/data-wipe/step-up"},
		{name: "delete account", path: "/api/v1/users/current/deletion/step-up"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, testCase.path, url.Values{}, map[string]string{
				"Accept": "application/json",
			})
			defer func() { _ = response.Body.Close() }()

			// Pinned to the refusal's own key, not merely to "not 200": with the
			// gate removed the request runs on into the OIDC machinery and fails
			// for an unrelated reason (no provider configured in this context),
			// which a status-class assertion would accept as success. The key is
			// what says the account was turned away for holding a password.
			body := readAPIError(t, response.Body)
			if response.StatusCode != http.StatusBadRequest || body != "invalid settings input" {
				t.Fatalf("expected %s step-up to refuse an account that has a local password with 400 %q, got %d %q",
					testCase.name, "invalid settings input", response.StatusCode, body)
			}
		})
	}
}

// TestErasureStepupStartDoesNotEraseAnything pins that the start endpoint only
// mints the re-auth: the data is destroyed by the provider callback, never by
// the request that begins the flow. Without this, a refused or abandoned
// step-up could still have cost the owner their records.
//
// The account here is OIDC-only, so it reaches the endpoint proper rather than
// the no-downgrade refusal above — that is also the positive anchor proving the
// route is live.
func TestErasureStepupStartDoesNotEraseAnything(t *testing.T) {
	t.Parallel()

	ctx := newOIDCOnlySettingsSecurityTestContext(t, "settings-erasure-stepup-start@example.com")

	seedErasureDayEntry(t, ctx)
	before := countErasureDayEntries(t, ctx)
	if before == 0 {
		t.Fatal("expected the seeded day entry to exist before the step-up starts")
	}

	for _, path := range []string{
		"/api/v1/users/current/data-wipe/step-up",
		"/api/v1/users/current/deletion/step-up",
	} {
		response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, path, url.Values{}, map[string]string{
			"Accept": "application/json",
		})
		_ = response.Body.Close()

		// Whatever the OIDC provider stub answers, the one thing that must not
		// happen is data loss: nothing is erased until the callback returns.
		if got := countErasureDayEntries(t, ctx); got != before {
			t.Fatalf("starting %s changed the day-entry count from %d to %d; the start endpoint must erase nothing", path, before, got)
		}
	}
}
