package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// End-to-end coverage of the erasure step-up, reusing the fixture built for the
// local-password step-up: an OIDC-only owner with a linked identity and a
// stubbed provider whose re-auth verdict the test controls.
//
// The flow's whole point is that nothing is destroyed until the provider sends
// the browser back, so every test here reads the data twice — once after the
// start, once after the callback — and the failing re-auth cases are anchored
// by the succeeding one directly above them.

func postErasureStepupStart(t *testing.T, fixture *oidcStepupFixture, path string) *http.Response {
	t.Helper()

	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))
	return mustAppResponse(t, fixture.app, request)
}

func seedStepupDayEntry(t *testing.T, fixture *oidcStepupFixture) {
	t.Helper()

	entry := models.DailyLog{
		UserID: fixture.user.ID,
		Date:   time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
	}
	if err := fixture.database.Create(&entry).Error; err != nil {
		t.Fatalf("seed day entry: %v", err)
	}
}

func countStepupDayEntries(t *testing.T, fixture *oidcStepupFixture) int64 {
	t.Helper()

	var count int64
	if err := fixture.database.Model(&models.DailyLog{}).Where("user_id = ?", fixture.user.ID).Count(&count).Error; err != nil {
		t.Fatalf("count day entries: %v", err)
	}
	return count
}

func countStepupAccounts(t *testing.T, fixture *oidcStepupFixture) int64 {
	t.Helper()

	var count int64
	if err := fixture.database.Model(&models.User{}).Where("id = ?", fixture.user.ID).Count(&count).Error; err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	return count
}

// TestErasureStepupClearsDataOnlyAfterAFreshReauth is the positive anchor for
// the whole flow, and it asserts the ordering that makes the flow safe: the
// start issues a redirect and leaves the diary intact, and only the callback
// wipes it.
func TestErasureStepupClearsDataOnlyAfterAFreshReauth(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-erasure-clear-complete@example.com")
	fixture.oidcStub.reauthErr = nil
	seedStepupDayEntry(t, fixture)

	startResponse := postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
	defer func() { _ = startResponse.Body.Close() }()
	if startResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from the clear-data step-up start, got %d", startResponse.StatusCode)
	}
	if got := countStepupDayEntries(t, fixture); got != 1 {
		t.Fatalf("starting the flow must not erase anything, day entries = %d", got)
	}

	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture, stepupCookie)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()
	if callbackResponse.StatusCode != http.StatusOK && callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 200/303 after a fresh reauth, got %d", callbackResponse.StatusCode)
	}

	if got := countStepupDayEntries(t, fixture); got != 0 {
		t.Fatalf("expected the diary to be wiped after the callback, day entries = %d", got)
	}
	if got := countStepupAccounts(t, fixture); got != 1 {
		t.Fatal("clear-data must keep the account itself")
	}
	if fixture.oidcStub.lastReauthUserID != fixture.user.ID {
		t.Fatalf("expected the reauth to be validated for user %d, got %d", fixture.user.ID, fixture.oidcStub.lastReauthUserID)
	}
}

// TestErasureStepupDeletesAccountOnlyAfterAFreshReauth is the delete-account
// half of the same contract.
func TestErasureStepupDeletesAccountOnlyAfterAFreshReauth(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-erasure-delete-complete@example.com")
	fixture.oidcStub.reauthErr = nil
	seedStepupDayEntry(t, fixture)

	startResponse := postErasureStepupStart(t, fixture, "/api/v1/users/current/deletion/step-up")
	defer func() { _ = startResponse.Body.Close() }()
	if startResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from the delete-account step-up start, got %d", startResponse.StatusCode)
	}
	if got := countStepupAccounts(t, fixture); got != 1 {
		t.Fatal("starting the flow must not delete the account")
	}

	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture, stepupCookie)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()
	if callbackResponse.StatusCode != http.StatusOK && callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 200/303 after a fresh reauth, got %d", callbackResponse.StatusCode)
	}

	if got := countStepupAccounts(t, fixture); got != 0 {
		t.Fatalf("expected the account to be deleted after the callback, accounts = %d", got)
	}
}

// TestErasureStepupKeepsDataWhenTheReauthIsRefused covers the branches that
// decide NOT to erase. Each one is a negative — nothing happened — so it is
// paired with the successful wipe above: that test proves the same fixture,
// route and callback do erase when the provider vouches for the owner, which
// is what makes "still 1 entry" here evidence of a refusal rather than of a
// dead flow.
func TestErasureStepupKeepsDataWhenTheReauthIsRefused(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		reauthErr error
	}{
		{name: "stale reauth", reauthErr: services.ErrOIDCReauthStale},
		{name: "identity mismatch", reauthErr: services.ErrOIDCReauthIdentityMismatch},
		{name: "provider unavailable", reauthErr: services.ErrOIDCUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := newOIDCStepupFixture(t, "settings-erasure-refused-"+strings.ReplaceAll(testCase.name, " ", "-")+"@example.com")
			fixture.oidcStub.reauthErr = nil
			seedStepupDayEntry(t, fixture)

			startResponse := postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
			defer func() { _ = startResponse.Body.Close() }()
			stepupCookie := readStepupCookie(t, startResponse)
			state := extractStepupCallbackState(t, fixture, stepupCookie)

			// Refuse only at the exchange, so the request reaches the same place
			// the successful case does and diverges exactly at the verdict.
			fixture.oidcStub.reauthErr = testCase.reauthErr

			callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
			defer func() { _ = callbackResponse.Body.Close() }()

			if got := countStepupDayEntries(t, fixture); got != 1 {
				t.Fatalf("a refused reauth must erase nothing, day entries = %d", got)
			}
			if got := countStepupAccounts(t, fixture); got != 1 {
				t.Fatal("a refused reauth must leave the account in place")
			}
		})
	}
}

// TestErasureStepupCallbackRefusesAMismatchedState pins the state check on the
// callback: a step-up cookie presented with someone else's state parameter must
// not authorize the erasure it names.
func TestErasureStepupCallbackRefusesAMismatchedState(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-erasure-state-mismatch@example.com")
	fixture.oidcStub.reauthErr = nil
	seedStepupDayEntry(t, fixture)

	startResponse := postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, "not-the-state-that-was-minted", "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if got := countStepupDayEntries(t, fixture); got != 1 {
		t.Fatalf("a mismatched state must erase nothing, day entries = %d", got)
	}
}
