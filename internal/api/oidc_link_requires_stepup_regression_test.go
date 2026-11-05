package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const linkForeignSubject = "link-foreign-subject"

// newOIDCLinkRealWriteFixture is the identity-link fixture with the write the
// link ends in routed through the REAL service and repository: the provider
// round trip stays the stub's, the identity row and the session-version bump
// are the production ones, so a refusal that leaves both untouched is not the
// write-nothing stub declining to write. The provider answers the foreign
// subject, so any path that reached the write would bind it.
func newOIDCLinkRealWriteFixture(t *testing.T, email string) *oidcStepupFixture {
	t.Helper()

	var workflow *realIdentityOIDCWorkflowService
	fixture := newOIDCStepupFixtureWithOptions(t, email, onboardingTestAppOptions{}, func(stub *stubOIDCWorkflowService) OIDCWorkflowService {
		workflow = &realIdentityOIDCWorkflowService{stubOIDCWorkflowService: stub}
		return workflow
	})
	repositories := db.NewRepositories(fixture.database)
	workflow.real = services.NewOIDCLoginService(enabledOIDCProviderForLinkTest{}, repositories.OIDCIdentities, repositories.Users, nil)
	fixture.oidcStub.identityLinkClaims = security.OIDCClaims{Issuer: fixture.stepupIssuer, Subject: linkForeignSubject}
	giveLinkFixtureAPassword(t, fixture)
	return fixture
}

// sealPendingLinkCookie seals a pending-link cookie for userID at version under
// secret, so the owner binding, the version and the key are the only things
// that can refuse it.
func sealPendingLinkCookie(t *testing.T, secret []byte, userID uint, version int) string {
	t.Helper()
	serialized, err := json.Marshal(oidcLinkPendingCookiePayload{
		UserID:         userID,
		SessionVersion: version,
		Issuer:         "https://id.example.com",
		Subject:        linkForeignSubject,
		ExpiresAt:      time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("marshal pending payload: %v", err)
	}
	codec, err := newSecureCookieCodec(secret)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	sealed, err := codec.seal(oidcLinkPendingCookieName, serialized)
	if err != nil {
		t.Fatalf("seal pending: %v", err)
	}
	return oidcLinkPendingCookieName + "=" + sealed
}

// confirmLinkWithPendingCookie posts the interstitial's confirm as the
// fixture's owner, carrying the given pending-link cookie pair.
func confirmLinkWithPendingCookie(t *testing.T, fixture *oidcStepupFixture, pending string) *http.Response {
	t.Helper()
	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/oidc/link/confirm", strings.NewReader(url.Values{"csrf_token": {csrfToken}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", joinCookieHeader(settingsCookieHeader(fixture.authCookie, csrfCookie), pending))
	return mustAppResponse(t, fixture.app, request)
}

func oidcIdentityRowCount(t *testing.T, fixture *oidcStepupFixture) int64 {
	t.Helper()
	var count int64
	if err := fixture.database.Model(&models.OIDCIdentity{}).Count(&count).Error; err != nil {
		t.Fatalf("count oidc_identities: %v", err)
	}
	return count
}

// TestOIDCLinkNeverBindsAForeignSubjectWithoutAFreshStepUp pins that an OIDC
// identity link needs a fresh step-up: holding only the owner's live session cookie and CSRF token, with no fresh
// step-up proof, no link entry binds a foreign provider subject. Each entry is
// refused at its own guard (the refusal is asserted, not only the absent row),
// and oidc_identities and AuthSessionVersion are unchanged after it.
// TestOIDCLinkBindsTheSubjectThroughTheLegitimateStepUp is the positive
// control: the same fixture, the same claims, the full step-up path writes
// exactly one row and bumps the version once.
func TestOIDCLinkNeverBindsAForeignSubjectWithoutAFreshStepUp(t *testing.T) {
	t.Parallel()

	staleSpec := settingsOIDCReauthStaleErrorSpec()

	// callbackWith posts a callback that carries a VALID sealed sign-in state
	// (minted by the real start route, state and code matching it) and the
	// owner's session, with only the step-up cookie omitted or replaced. Past
	// the state guard the only thing left deciding between a sign-in and a link
	// is the step-up cookie.
	callbackWith := func(t *testing.T, fixture *oidcStepupFixture, stepup string) *http.Response {
		t.Helper()
		start := mustAppResponse(t, fixture.app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
		defer func() { _ = start.Body.Close() }()
		stateCookie := responseCookie(start.Cookies(), oidcStateCookieName)
		if stateCookie == nil || fixture.oidcStub.lastStartState == "" {
			t.Fatal("fixture: the start route minted no sign-in state")
		}
		form := url.Values{"state": {fixture.oidcStub.lastStartState}, "code": {"attacker-code"}}
		request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, cookiePair(stateCookie), stepup))
		return mustAppResponse(t, fixture.app, request)
	}

	cases := []struct {
		name string
		run  func(t *testing.T, fixture *oidcStepupFixture)
	}{
		{"step-up start without the password", func(t *testing.T, fixture *oidcStepupFixture) {
			response := postOIDCIdentityLinkStepupStartWithPassword(t, fixture, "")
			defer func() { _ = response.Body.Close() }()
			assertLinkStartRefusedBeforeAnyStepup(t, fixture, response, settingsMissingPasswordErrorSpec())
		}},
		{"step-up start with a wrong password", func(t *testing.T, fixture *oidcStepupFixture) {
			response := postOIDCIdentityLinkStepupStartWithPassword(t, fixture, "WrongPass1")
			defer func() { _ = response.Body.Close() }()
			assertLinkStartRefusedBeforeAnyStepup(t, fixture, response, settingsInvalidPasswordErrorSpec())
		}},
		{"callback with a valid sign-in state and no step-up cookie", func(t *testing.T, fixture *oidcStepupFixture) {
			response := callbackWith(t, fixture, "")
			defer func() { _ = response.Body.Close() }()
			assertCallbackWasAnOrdinarySignIn(t, fixture, response)
		}},
		{"callback with a valid sign-in state and a forged step-up cookie", func(t *testing.T, fixture *oidcStepupFixture) {
			response := callbackWith(t, fixture, oidcStepupCookieName+"=forged-not-sealed")
			defer func() { _ = response.Body.Close() }()
			assertCallbackWasAnOrdinarySignIn(t, fixture, response)
		}},
		{"callback answering another state than the owner's step-up", func(t *testing.T, fixture *oidcStepupFixture) {
			// The owner's genuine step-up is outstanding; a callback that does
			// not answer its state (a cross-site post with another code) must
			// neither complete it nor park the identity.
			start := postOIDCIdentityLinkStepupStart(t, fixture)
			defer func() { _ = start.Body.Close() }()
			stepup := readStepupCookie(t, start)
			response := postOIDCStepupCallback(t, fixture, stepup, "another-state", "attacker-code")
			defer func() { _ = response.Body.Close() }()
			assertSettingsRefusalRedirect(t, response, authOIDCAuthenticationFailedErrorSpec())
			assertNoPendingLinkParked(t, response)
			if fixture.oidcStub.lastIdentityLinkCode != "" {
				t.Fatal("a callback for another state reached the link verification")
			}
			if fixture.oidcStub.lastAuthCode != "" {
				t.Fatal("a callback for another state was treated as an ordinary sign-in")
			}
			// Not the owner's return trip, so it must not spend the owner's
			// step-up: the cookie stays for the real callback.
			if retracted := responseCookie(response.Cookies(), oidcStepupCookieName); retracted != nil {
				t.Fatalf("a callback for another state touched the owner's step-up cookie: %+v", retracted)
			}

			// Positive anchor: the same step-up cookie answered with its own
			// state parks the identity, so the refusal above was the state and
			// not a cookie or fixture that never worked.
			answered := postOIDCStepupCallback(t, fixture, stepup, extractStepupCallbackState(t, fixture), "owner-code")
			defer func() { _ = answered.Body.Close() }()
			_ = readOIDCLinkPendingCookie(t, answered)
		}},
		{"callback carrying another owner's step-up under this owner's session", func(t *testing.T, fixture *oidcStepupFixture) {
			// The step-up was started by the fixture's owner; the callback
			// arrives with a different owner's live session. The session user
			// is not the one the sealed step-up names, so nothing is parked.
			start := postOIDCIdentityLinkStepupStart(t, fixture)
			defer func() { _ = start.Body.Close() }()
			stepup := readStepupCookie(t, start)

			other := models.User{
				Email:               "link-other-owner@example.com",
				LocalAuthEnabled:    false,
				Role:                models.RoleOwner,
				OnboardingCompleted: true,
				AuthSessionVersion:  1,
				CycleLength:         28,
				PeriodLength:        5,
				AutoPeriodFill:      true,
				CreatedAt:           time.Now().UTC(),
			}
			if err := fixture.database.Create(&other).Error; err != nil {
				t.Fatalf("create the second owner: %v", err)
			}
			rowsBefore := oidcIdentityRowCount(t, fixture)
			otherVersion := storedAuthSessionVersion(t, fixture.database, other.ID)

			form := url.Values{"state": {extractStepupCallbackState(t, fixture)}, "code": {"owner-code"}}
			request := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "application/json")
			request.Header.Set("Cookie", joinCookieHeader(issueAuthCookieForUser(t, other), stepup))
			response := mustAppResponse(t, fixture.app, request)
			defer func() { _ = response.Body.Close() }()

			assertSettingsRefusalRedirect(t, response, settingsOIDCReauthMismatchErrorSpec())
			assertNoPendingLinkParked(t, response)
			if fixture.oidcStub.lastIdentityLinkCode != "" {
				t.Fatal("a step-up under another owner's session reached the link verification")
			}
			if got := oidcIdentityRowCount(t, fixture); got != rowsBefore {
				t.Fatalf("oidc_identities changed from %d to %d rows", rowsBefore, got)
			}
			if got := storedAuthSessionVersion(t, fixture.database, other.ID); got != otherVersion {
				t.Fatalf("the second owner's AuthSessionVersion moved from %d to %d", otherVersion, got)
			}
		}},
		{"confirm with no pending cookie", func(t *testing.T, fixture *oidcStepupFixture) {
			response := confirmLinkWithPendingCookie(t, fixture, "")
			defer func() { _ = response.Body.Close() }()
			assertLinkConfirmRefused(t, response, staleSpec)
		}},
		{"confirm with a forged pending cookie", func(t *testing.T, fixture *oidcStepupFixture) {
			response := confirmLinkWithPendingCookie(t, fixture, oidcLinkPendingCookieName+"=forged-not-sealed")
			defer func() { _ = response.Body.Close() }()
			assertLinkConfirmRefused(t, response, staleSpec)
		}},
		{"confirm with a pending cookie sealed under another key", func(t *testing.T, fixture *oidcStepupFixture) {
			response := confirmLinkWithPendingCookie(t, fixture, sealPendingLinkCookie(t, []byte("another-secret-key"), fixture.user.ID, storedAuthSessionVersion(t, fixture.database, fixture.user.ID)))
			defer func() { _ = response.Body.Close() }()
			assertLinkConfirmRefused(t, response, staleSpec)
		}},
		{"confirm with a pending cookie bound to another user id", func(t *testing.T, fixture *oidcStepupFixture) {
			response := confirmLinkWithPendingCookie(t, fixture, sealPendingLinkCookie(t, []byte(testAppSecretKey), fixture.user.ID+100, storedAuthSessionVersion(t, fixture.database, fixture.user.ID)))
			defer func() { _ = response.Body.Close() }()
			assertLinkConfirmRefused(t, response, staleSpec)
		}},
	}

	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fixture := newOIDCLinkRealWriteFixture(t, fmt.Sprintf("link-direct-%d@example.com", index))
			rowsBefore := oidcIdentityRowCount(t, fixture)
			versionBefore := storedAuthSessionVersion(t, fixture.database, fixture.user.ID)
			if rowsBefore != 1 {
				t.Fatalf("fixture: expected the owner's one linked identity, found %d rows", rowsBefore)
			}

			tc.run(t, fixture)

			// The callback cases run through the stub's exchange, which writes
			// nothing, so these row and version checks cannot fail for them: the
			// callback cases are pinned by their own assertions above, and these
			// checks bind the start and confirm cases, which reach the real
			// service and repository.
			if got := oidcIdentityRowCount(t, fixture); got != rowsBefore {
				t.Fatalf("oidc_identities changed from %d to %d rows without a fresh step-up", rowsBefore, got)
			}
			_, linked, err := db.NewRepositories(fixture.database).OIDCIdentities.FindByIssuerSubject(context.Background(), fixture.stepupIssuer, linkForeignSubject)
			if err != nil || linked {
				t.Fatalf("the foreign subject is bound without a fresh step-up: linked=%v err=%v", linked, err)
			}
			if got := storedAuthSessionVersion(t, fixture.database, fixture.user.ID); got != versionBefore {
				t.Fatalf("AuthSessionVersion moved from %d to %d without a fresh step-up", versionBefore, got)
			}
		})
	}

	// A link parked at version 1, then the account's sessions revoked (version
	// 2) while this device keeps a session re-issued at 2: the owner's genuine
	// step-up predates the revocation, so the confirm is stale and writes
	// nothing. Without the freshness check the real write's own compare-and-set
	// would still refuse, but as a signed-out answer, not as the stale refusal.
	t.Run("confirm with a pending link parked before a revocation", func(t *testing.T) {
		t.Parallel()

		fixture := newOIDCLinkRealWriteFixture(t, "link-direct-stale-pending@example.com")
		pending := sealPendingLinkCookie(t, []byte(testAppSecretKey), fixture.user.ID, storedAuthSessionVersion(t, fixture.database, fixture.user.ID))
		if err := db.NewRepositories(fixture.database).Users.BumpAuthSessionVersion(context.Background(), fixture.user.ID); err != nil {
			t.Fatalf("revoke the account's sessions: %v", err)
		}
		revoked := fixture.user
		revoked.AuthSessionVersion = storedAuthSessionVersion(t, fixture.database, fixture.user.ID)
		fixture.authCookie = issueAuthCookieForUser(t, revoked)
		rowsBefore := oidcIdentityRowCount(t, fixture)

		response := confirmLinkWithPendingCookie(t, fixture, pending)
		defer func() { _ = response.Body.Close() }()
		assertLinkConfirmRefused(t, response, staleSpec)

		if got := oidcIdentityRowCount(t, fixture); got != rowsBefore {
			t.Fatalf("a stale pending link changed oidc_identities from %d to %d rows", rowsBefore, got)
		}
		if got := storedAuthSessionVersion(t, fixture.database, fixture.user.ID); got != revoked.AuthSessionVersion {
			t.Fatalf("a stale pending link moved AuthSessionVersion from %d to %d", revoked.AuthSessionVersion, got)
		}
	})
}

// TestOIDCLinkConfirmAcceptsAPendingCookieSealedForTheOwner is the acceptance
// proof for the sealed-cookie helper the refusal cases use: the same helper,
// sealed under the app key for the session's own user at its current version,
// is accepted by confirm and links exactly one row. Without it, every confirm
// refusal above could be the helper producing a cookie nothing accepts.
func TestOIDCLinkConfirmAcceptsAPendingCookieSealedForTheOwner(t *testing.T) {
	t.Parallel()

	fixture := newOIDCLinkRealWriteFixture(t, "link-direct-sealed-accept@example.com")
	rowsBefore := oidcIdentityRowCount(t, fixture)
	version := storedAuthSessionVersion(t, fixture.database, fixture.user.ID)

	response := confirmLinkWithPendingCookie(t, fixture, sealPendingLinkCookie(t, []byte(testAppSecretKey), fixture.user.ID, version))
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= http.StatusBadRequest {
		t.Fatalf("a pending cookie sealed for the owner must be accepted, got %d: %s", response.StatusCode, mustReadBodyString(t, response.Body))
	}
	if got := oidcIdentityRowCount(t, fixture); got != rowsBefore+1 {
		t.Fatalf("expected exactly one new oidc_identities row (%d -> %d), got %d", rowsBefore, rowsBefore+1, got)
	}
	_, linked, err := db.NewRepositories(fixture.database).OIDCIdentities.FindByIssuerSubject(context.Background(), fixture.stepupIssuer, linkForeignSubject)
	if err != nil || !linked {
		t.Fatalf("the sealed subject must be bound: linked=%v err=%v", linked, err)
	}
}

// TestOIDCLinkBindsTheSubjectThroughTheLegitimateStepUp is the positive control
// for the refusal cases above: the same fixture and claims, the full step-up
// path (start, callback, confirm), writing exactly one row and bumping the
// session version once.
func TestOIDCLinkBindsTheSubjectThroughTheLegitimateStepUp(t *testing.T) {
	t.Parallel()

	fixture := newOIDCLinkRealWriteFixture(t, "link-direct-control@example.com")
	rowsBefore := oidcIdentityRowCount(t, fixture)
	versionBefore := storedAuthSessionVersion(t, fixture.database, fixture.user.ID)

	response := confirmOIDCIdentityLinkThroughStepup(t, fixture)
	if response.StatusCode >= http.StatusBadRequest {
		t.Fatalf("control: the legitimate step-up must link, got %d: %s", response.StatusCode, mustReadBodyString(t, response.Body))
	}

	if got := oidcIdentityRowCount(t, fixture); got != rowsBefore+1 {
		t.Fatalf("control: expected exactly one new oidc_identities row (%d -> %d), got %d", rowsBefore, rowsBefore+1, got)
	}
	_, linked, err := db.NewRepositories(fixture.database).OIDCIdentities.FindByIssuerSubject(context.Background(), fixture.stepupIssuer, linkForeignSubject)
	if err != nil || !linked {
		t.Fatalf("control: the stepped-up subject must be bound: linked=%v err=%v", linked, err)
	}
	if got := storedAuthSessionVersion(t, fixture.database, fixture.user.ID); got != versionBefore+1 {
		t.Fatalf("control: expected AuthSessionVersion %d, got %d", versionBefore+1, got)
	}
	if reissued := responseCookie(response.Cookies(), authCookieName); reissued == nil || strings.TrimSpace(reissued.Value) == "" {
		t.Fatal("control: the link must re-issue this device's session")
	}
}

// assertSettingsRefusalRedirect requires a callback refusal to land on
// /settings as a 303 carrying the spec's key in the sealed flash.
func assertSettingsRefusalRedirect(t *testing.T, response *http.Response, want APIErrorSpec) {
	t.Helper()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/settings" {
		t.Fatalf("expected a 303 to /settings, got %d %q", response.StatusCode, response.Header.Get("Location"))
	}
	// A request that does not state Sec-Fetch-Site: same-origin lands its
	// refusal in the CSRF-exempt flash slot, a stated one in the ordinary slot.
	flash := mustReadExemptFlashPayload
	if responseCookie(response.Cookies(), flashCookieName) != nil {
		flash = mustReadFlashPayload
	}
	if got := flash(t, []byte(testAppSecretKey), response.Cookies()).SettingsError; got != want.Key {
		t.Fatalf("expected the settings flash %q, got %q", want.Key, got)
	}
}

// assertLinkStartRefusedBeforeAnyStepup requires the start to stop at the
// password gate with the exact refusal: no step-up cookie, and the provider
// never asked to start a re-authentication.
func assertLinkStartRefusedBeforeAnyStepup(t *testing.T, fixture *oidcStepupFixture, response *http.Response, want APIErrorSpec) {
	t.Helper()
	if response.StatusCode != want.Status {
		t.Fatalf("expected the link step-up refused with %d, got %d", want.Status, response.StatusCode)
	}
	if cookie := responseCookie(response.Cookies(), oidcStepupCookieName); cookie != nil && cookie.Value != "" {
		t.Fatal("a step-up cookie was minted without the account password")
	}
	if fixture.oidcStub.lastReauthState != "" {
		t.Fatal("the provider re-authentication was started without the account password")
	}
}

// assertCallbackWasAnOrdinarySignIn requires a callback without a usable
// step-up cookie to have run as an ordinary sign-in: the code reached the
// sign-in exchange (so it passed the state guard and was not turned away
// earlier), the link verification never ran, and no identity was parked for a
// later confirm.
func assertCallbackWasAnOrdinarySignIn(t *testing.T, fixture *oidcStepupFixture, response *http.Response) {
	t.Helper()
	assertNoPendingLinkParked(t, response)
	if fixture.oidcStub.lastAuthCode != "attacker-code" {
		t.Fatalf("expected the callback to run as an ordinary sign-in exchange, the exchange saw code %q", fixture.oidcStub.lastAuthCode)
	}
	if fixture.oidcStub.lastIdentityLinkCode != "" {
		t.Fatal("a callback without a step-up cookie reached the link verification")
	}
}

func assertNoPendingLinkParked(t *testing.T, response *http.Response) {
	t.Helper()
	if cookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName); cookie != nil && cookie.Value != "" {
		t.Fatal("the callback parked an identity for the owner to confirm")
	}
}

func assertLinkConfirmRefused(t *testing.T, response *http.Response, want APIErrorSpec) {
	t.Helper()
	if response.StatusCode != want.Status {
		t.Fatalf("expected the confirm refused with %d, got %d", want.Status, response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != want.Key {
		t.Fatalf("expected the confirm refused as %q, got %q", want.Key, got)
	}
}
