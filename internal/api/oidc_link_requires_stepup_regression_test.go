package api

import (
	"context"
	"encoding/json"
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

const r3ForeignSubject = "r3-foreign-subject"

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
	fixture.oidcStub.identityLinkClaims = security.OIDCClaims{Issuer: fixture.stepupIssuer, Subject: r3ForeignSubject}
	giveLinkFixtureAPassword(t, fixture)
	return fixture
}

func oidcIdentityRowCount(t *testing.T, fixture *oidcStepupFixture) int64 {
	t.Helper()
	var count int64
	if err := fixture.database.Model(&models.OIDCIdentity{}).Count(&count).Error; err != nil {
		t.Fatalf("count oidc_identities: %v", err)
	}
	return count
}

// TestOIDCLinkNeverBindsAForeignSubjectWithoutAFreshStepUp pins R3 directly:
// holding only the owner's live session cookie and CSRF token, with no fresh
// step-up proof, no link entry binds a foreign provider subject. Each entry is
// refused at its own guard (the refusal is asserted, not only the absent row),
// and oidc_identities and AuthSessionVersion are unchanged after it.
// TestOIDCLinkBindsTheSubjectThroughTheLegitimateStepUp is the positive
// control: the same fixture, the same claims, the full step-up path writes
// exactly one row and bumps the version once.
func TestOIDCLinkNeverBindsAForeignSubjectWithoutAFreshStepUp(t *testing.T) {
	t.Parallel()

	staleSpec := settingsOIDCReauthStaleErrorSpec()

	// sealPending seals at the session's CURRENT version, so the owner binding
	// (or the key) is the only thing left that can refuse it.
	sealPending := func(t *testing.T, secret []byte, userID uint, version int) string {
		t.Helper()
		serialized, err := json.Marshal(oidcLinkPendingCookiePayload{
			UserID:         userID,
			SessionVersion: version,
			Issuer:         "https://id.example.com",
			Subject:        r3ForeignSubject,
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

	confirmWith := func(t *testing.T, fixture *oidcStepupFixture, pending string) *http.Response {
		t.Helper()
		csrfCookie, csrfToken := fixture.settingsCSRF(t)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/oidc/link/confirm", strings.NewReader(url.Values{"csrf_token": {csrfToken}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", joinCookieHeader(settingsCookieHeader(fixture.authCookie, csrfCookie), pending))
		return mustAppResponse(t, fixture.app, request)
	}
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
			assertLinkStartRefusedBeforeAnyStepup(t, fixture, response)
		}},
		{"step-up start with a wrong password", func(t *testing.T, fixture *oidcStepupFixture) {
			response := postOIDCIdentityLinkStepupStartWithPassword(t, fixture, "WrongPass1")
			defer func() { _ = response.Body.Close() }()
			assertLinkStartRefusedBeforeAnyStepup(t, fixture, response)
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
			response := postOIDCStepupCallback(t, fixture, readStepupCookie(t, start), "another-state", "attacker-code")
			defer func() { _ = response.Body.Close() }()
			if cookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName); cookie != nil && cookie.Value != "" {
				t.Fatal("a callback for another state parked an identity for the owner to confirm")
			}
			if fixture.oidcStub.lastIdentityLinkCode != "" {
				t.Fatal("a callback for another state reached the link verification")
			}
			if fixture.oidcStub.lastAuthCode != "" {
				t.Fatal("a callback for another state was treated as an ordinary sign-in")
			}
		}},
		{"confirm with no pending cookie", func(t *testing.T, fixture *oidcStepupFixture) {
			response := confirmWith(t, fixture, "")
			defer func() { _ = response.Body.Close() }()
			assertLinkConfirmRefused(t, response, staleSpec)
		}},
		{"confirm with a forged pending cookie", func(t *testing.T, fixture *oidcStepupFixture) {
			response := confirmWith(t, fixture, oidcLinkPendingCookieName+"=forged-not-sealed")
			defer func() { _ = response.Body.Close() }()
			assertLinkConfirmRefused(t, response, staleSpec)
		}},
		{"confirm with a pending cookie sealed under another key", func(t *testing.T, fixture *oidcStepupFixture) {
			response := confirmWith(t, fixture, sealPending(t, []byte("another-secret-key"), fixture.user.ID, storedAuthSessionVersion(t, fixture.database, fixture.user.ID)))
			defer func() { _ = response.Body.Close() }()
			assertLinkConfirmRefused(t, response, staleSpec)
		}},
		{"confirm with another account's pending cookie", func(t *testing.T, fixture *oidcStepupFixture) {
			response := confirmWith(t, fixture, sealPending(t, []byte(testAppSecretKey), fixture.user.ID+100, storedAuthSessionVersion(t, fixture.database, fixture.user.ID)))
			defer func() { _ = response.Body.Close() }()
			assertLinkConfirmRefused(t, response, staleSpec)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fixture := newOIDCLinkRealWriteFixture(t, "r3-direct-"+strings.NewReplacer(" ", "-", "'", "").Replace(tc.name)+"@example.com")
			rowsBefore := oidcIdentityRowCount(t, fixture)
			versionBefore := storedAuthSessionVersion(t, fixture.database, fixture.user.ID)
			if rowsBefore != 1 {
				t.Fatalf("fixture: expected the owner's one linked identity, found %d rows", rowsBefore)
			}

			tc.run(t, fixture)

			if got := oidcIdentityRowCount(t, fixture); got != rowsBefore {
				t.Fatalf("oidc_identities changed from %d to %d rows without a fresh step-up", rowsBefore, got)
			}
			_, linked, err := db.NewRepositories(fixture.database).OIDCIdentities.FindByIssuerSubject(context.Background(), fixture.stepupIssuer, r3ForeignSubject)
			if err != nil || linked {
				t.Fatalf("the foreign subject is bound without a fresh step-up: linked=%v err=%v", linked, err)
			}
			if got := storedAuthSessionVersion(t, fixture.database, fixture.user.ID); got != versionBefore {
				t.Fatalf("AuthSessionVersion moved from %d to %d without a fresh step-up", versionBefore, got)
			}
		})
	}
}

func TestOIDCLinkBindsTheSubjectThroughTheLegitimateStepUp(t *testing.T) {
	t.Parallel()

	fixture := newOIDCLinkRealWriteFixture(t, "r3-direct-control@example.com")
	rowsBefore := oidcIdentityRowCount(t, fixture)
	versionBefore := storedAuthSessionVersion(t, fixture.database, fixture.user.ID)

	response := confirmOIDCIdentityLinkThroughStepup(t, fixture)
	if response.StatusCode >= http.StatusBadRequest {
		t.Fatalf("control: the legitimate step-up must link, got %d: %s", response.StatusCode, mustReadBodyString(t, response.Body))
	}

	if got := oidcIdentityRowCount(t, fixture); got != rowsBefore+1 {
		t.Fatalf("control: expected exactly one new oidc_identities row (%d -> %d), got %d", rowsBefore, rowsBefore+1, got)
	}
	_, linked, err := db.NewRepositories(fixture.database).OIDCIdentities.FindByIssuerSubject(context.Background(), fixture.stepupIssuer, r3ForeignSubject)
	if err != nil || !linked {
		t.Fatalf("control: the stepped-up subject must be bound: linked=%v err=%v", linked, err)
	}
	if got := storedAuthSessionVersion(t, fixture.database, fixture.user.ID); got != versionBefore+1 {
		t.Fatalf("control: expected AuthSessionVersion %d, got %d", versionBefore+1, got)
	}
}

// assertLinkStartRefusedBeforeAnyStepup requires the start to stop at the
// password gate: refused, no step-up cookie, and the provider never asked to
// start a re-authentication.
func assertLinkStartRefusedBeforeAnyStepup(t *testing.T, fixture *oidcStepupFixture, response *http.Response) {
	t.Helper()
	if response.StatusCode < http.StatusBadRequest {
		t.Fatalf("the link step-up started without the account password: status %d", response.StatusCode)
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
