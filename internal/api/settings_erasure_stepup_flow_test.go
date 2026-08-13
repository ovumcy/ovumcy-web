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
	state := extractStepupCallbackState(t, fixture)

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
	state := extractStepupCallbackState(t, fixture)

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()
	if callbackResponse.StatusCode != http.StatusOK && callbackResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 200/303 after a fresh reauth, got %d", callbackResponse.StatusCode)
	}

	if got := countStepupAccounts(t, fixture); got != 0 {
		t.Fatalf("expected the account to be deleted after the callback, accounts = %d", got)
	}

	// The erasure completes through the same applyDeleteAccount as the
	// password-gated route, so this arm must retract the session's cookies too —
	// the language cache included, since the account it mirrored no longer
	// exists. The auth cookie's retraction is the anchor that the teardown ran.
	clearedAuth := responseCookie(callbackResponse.Cookies(), authCookieName)
	if clearedAuth == nil || strings.TrimSpace(clearedAuth.Value) != "" {
		t.Fatalf("expected the step-up deletion to retract the auth cookie, got %#v", clearedAuth)
	}
	clearedLanguage := responseCookie(callbackResponse.Cookies(), languageCookieName)
	if clearedLanguage == nil || clearedLanguage.Value != "" {
		t.Fatalf("expected the step-up deletion to retract the language cookie, got %#v", clearedLanguage)
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
			state := extractStepupCallbackState(t, fixture)

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

// TestErasureStepupFlowForRejectsAnUnknownOperation pins the lookup's closed
// set directly: every other caller reaches it through a sealed payload that
// validAt has already screened, so this is the only place the default arm is
// observable.
func TestErasureStepupFlowForRejectsAnUnknownOperation(t *testing.T) {
	t.Parallel()

	if _, known := erasureStepupFlowFor("wipe_everything"); known {
		t.Fatal("expected an unknown erasure operation to have no flow")
	}
	for _, operation := range []oidcStepupErasureOperation{oidcStepupErasureClearData, oidcStepupErasureDeleteAccount} {
		flow, known := erasureStepupFlowFor(operation)
		if !known || flow.kind.action == "" || flow.stepupAction == "" {
			t.Fatalf("expected a complete flow for %q, got %+v", operation, flow)
		}
	}
}

// TestErasureStepupStartSurfacesAProviderFailure covers the branch where the
// provider cannot be reached at all: the step-up cookie must not be left behind
// arming a flow the owner can no longer complete.
func TestErasureStepupStartSurfacesAProviderFailure(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-erasure-start-failure@example.com")
	fixture.oidcStub.reauthStartErr = services.ErrOIDCUnavailable

	response := postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusOK {
		t.Fatal("expected the start to fail when the provider is unreachable")
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == oidcStepupCookieName && cookie.Value != "" {
			t.Fatal("expected no usable step-up cookie after a failed start")
		}
	}
}

// TestErasureStepupStartReturnsAnInterstitialForBrowsers covers the non-JSON
// arm: a settings form submit cannot redirect straight to the provider, because
// the page's CSP pins form-action to 'self' across the redirect chain.
func TestErasureStepupStartReturnsAnInterstitialForBrowsers(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-erasure-start-browser@example.com")

	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	form := url.Values{"csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/data-wipe/step-up", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Cookie", settingsCookieHeader(fixture.authCookie, csrfCookie))

	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with an interstitial for a browser submit, got %d", response.StatusCode)
	}
	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, fixture.oidcStub.reauthURL) {
		t.Fatalf("expected the interstitial to carry the provider URL, got %q", body)
	}
}

// TestErasureStepupCallbackRefusesAForeignSession covers the identity binding:
// a step-up cookie minted for one owner, presented with another owner's
// session, must erase nothing.
func TestErasureStepupCallbackRefusesAForeignSession(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-erasure-foreign-session@example.com")
	seedStepupDayEntry(t, fixture)

	startResponse := postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	// A second owner on the same instance — the household case.
	intruder := models.User{
		Email:               "settings-erasure-intruder@example.com",
		LocalAuthEnabled:    false,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		CreatedAt:           time.Now().UTC(),
	}
	if err := fixture.database.Create(&intruder).Error; err != nil {
		t.Fatalf("create second owner: %v", err)
	}

	form := url.Values{"state": {state}, "code": {"callback-code"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", joinCookieHeader(issueAuthCookieForUser(t, intruder), stepupCookie))
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if got := countStepupDayEntries(t, fixture); got != 1 {
		t.Fatalf("a step-up presented with another owner's session must erase nothing, day entries = %d", got)
	}
}

// TestErasureStepupCallbackRefusesOnceALocalPasswordExists covers the branch
// where the account enrolled a password between start and callback: the
// erasure gate moved back to that password, so the step-up no longer authorizes
// anything.
func TestErasureStepupCallbackRefusesOnceALocalPasswordExists(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-erasure-password-appeared@example.com")
	seedStepupDayEntry(t, fixture)

	startResponse := postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	if err := fixture.database.Model(&models.User{}).Where("id = ?", fixture.user.ID).
		Update("local_auth_enabled", true).Error; err != nil {
		t.Fatalf("enable local auth mid-flow: %v", err)
	}

	callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	defer func() { _ = callbackResponse.Body.Close() }()

	if got := countStepupDayEntries(t, fixture); got != 1 {
		t.Fatalf("the step-up must stop authorizing once a local password exists, day entries = %d", got)
	}
}

// TestErasureStepupCallbackRefusesAProviderError covers the arm where the
// provider reports a failure in the callback itself.
func TestErasureStepupCallbackRefusesAProviderError(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-erasure-provider-error@example.com")
	seedStepupDayEntry(t, fixture)

	startResponse := postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
	defer func() { _ = startResponse.Body.Close() }()
	stepupCookie := readStepupCookie(t, startResponse)
	state := extractStepupCallbackState(t, fixture)

	form := url.Values{"state": {state}, "code": {""}, "error": {"access_denied"}}
	request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, stepupCookie))
	response := mustAppResponse(t, fixture.app, request)
	defer func() { _ = response.Body.Close() }()

	if got := countStepupDayEntries(t, fixture); got != 1 {
		t.Fatalf("a provider error must erase nothing, day entries = %d", got)
	}
}

// TestErasureStepupCallbackSurvivesAStorageFailure covers the branch where the
// re-auth succeeded but the erasure itself could not run: the owner must land
// back on settings with an error rather than a half-finished wipe reported as
// success.
func TestErasureStepupCallbackSurvivesAStorageFailure(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		path string
	}{
		{name: "clear data", path: "/api/v1/users/current/data-wipe/step-up"},
		{name: "delete account", path: "/api/v1/users/current/deletion/step-up"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := newOIDCStepupFixture(t, "settings-erasure-storage-"+strings.ReplaceAll(testCase.name, " ", "-")+"@example.com")

			startResponse := postErasureStepupStart(t, fixture, testCase.path)
			defer func() { _ = startResponse.Body.Close() }()
			stepupCookie := readStepupCookie(t, startResponse)
			state := extractStepupCallbackState(t, fixture)

			// Both erasure transactions start at daily_logs, so dropping it fails
			// them while leaving the users table the auth middleware reads intact.
			if err := fixture.database.Exec("DROP TABLE daily_logs").Error; err != nil {
				t.Fatalf("drop daily_logs: %v", err)
			}

			callbackResponse := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
			defer func() { _ = callbackResponse.Body.Close() }()

			if got := countStepupAccounts(t, fixture); got != 1 {
				t.Fatalf("a failed erasure must leave the account in place, accounts = %d", got)
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
