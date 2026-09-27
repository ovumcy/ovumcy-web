package api

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/apideps"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// WEB-64: GET /register/welcome used to spend the single-use pickup token
// (marking register_pickup_tokens.consumed_at) and only then seal the auth
// cookie and the recovery-code reveal, so a sealing failure between the spend
// and the reveal cost the owner the code with no way to retry the pickup.
// The session and the reveal are now sealed BEFORE the token is consumed
// (PickupRegister's Peek-then-Consume order); this pins, with the handler's
// sessionIssuanceFault / recoveryCodeIssuanceFault seams standing in for the
// crypto/codec failure no request can provoke, that the token row is still
// unconsumed, that neither cookie went out, and that the very same pickup
// cookie — as the browser's jar actually holds it after the refusal, not the
// value the test happened to mint it with — redeems once issuance works
// again.
//
// The DB-side row staying unconsumed only pays off for a real client if the
// pickup cookie carrying its nonce+RC also survives the refused response:
// popRegisterPickupCookie retracts it unconditionally as it reads it, so
// PickupRegister has to hand it back for these two branches specifically
// (redirectToPostRegisterSigninKeepingPickupCookie). Both regressions below
// assert that directly: no expiring Set-Cookie for registerPickupCookieName
// on the refused response.

var errInjectedRecoveryCodeIssuance = errors.New("injected recovery code issuance failure")

func armableRecoveryCodeIssuanceFault() (*atomic.Bool, func() error) {
	armed := &atomic.Bool{}
	return armed, func() error {
		if armed.Load() {
			return errInjectedRecoveryCodeIssuance
		}
		return nil
	}
}

func loadRegisterPickupTokenRowForUser(t *testing.T, database *gorm.DB, userID uint) models.RegisterPickupToken {
	t.Helper()
	var row models.RegisterPickupToken
	if err := database.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatalf("load register_pickup_tokens row for user %d: %v", userID, err)
	}
	return row
}

// assertPickupCookieKeptForRetry pins the fix half of the finding: the
// refused response must carry the pickup cookie as a LIVE Set-Cookie (a
// fresh value, no past expiry) rather than the expiring one every other
// pickup exit emits. It returns the value the browser's jar would hold next,
// so the caller drives its retry from what the response actually left behind
// instead of the cookie the test originally minted.
func assertPickupCookieKeptForRetry(t *testing.T, response *http.Response) string {
	t.Helper()

	cookie := responseCookie(response.Cookies(), registerPickupCookieName)
	if cookie == nil {
		t.Fatal("expected the seal failure to leave a pickup Set-Cookie in the response for retry, got none at all")
	}
	if strings.TrimSpace(cookie.Value) == "" {
		t.Fatal("expected the seal failure to leave a LIVE pickup cookie, got an emptied (cleared) Set-Cookie")
	}
	if !cookie.Expires.IsZero() && cookie.Expires.Before(time.Now()) {
		t.Fatalf("expected the seal failure's pickup cookie to expire in the future, got an expiring Set-Cookie (expiry %s)", cookie.Expires)
	}
	return cookie.Value
}

func TestRegisterPickupSessionMintFailureLeavesTheTokenRedeemable(t *testing.T) {
	t.Parallel()

	armed, fault := armableSessionIssuanceFault()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{sessionIssuanceFault: fault})
	email := "web64-pickup-seal-order@example.com"

	registerResponse := mustAppResponse(t, app, registerRequest(email))
	if registerResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected registration to redirect, got %d", registerResponse.StatusCode)
	}
	pickup := responseCookieValue(registerResponse.Cookies(), registerPickupCookieName)
	if pickup == "" {
		t.Fatalf("expected pickup cookie after register")
	}

	var user models.User
	if err := database.Where("email = ?", email).First(&user).Error; err != nil {
		t.Fatalf("load registered user: %v", err)
	}

	pickupRequestWith := func(cookieValue string) *http.Response {
		request := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", registerPickupCookieName+"="+cookieValue)
		return mustAppResponse(t, app, request)
	}

	armed.Store(true)
	refused := pickupRequestWith(pickup)
	defer func() { _ = refused.Body.Close() }()

	if location := refused.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected the failed pickup to redirect to /login, got %q", location)
	}
	if cookie := responseCookieValue(refused.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("a pickup whose session minting failed must not set an auth cookie; got %q", cookie)
	}
	if cookie := responseCookieValue(refused.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("a pickup whose session minting failed must not set a recovery-code cookie; got %q", cookie)
	}

	// The pickup cookie itself must survive this response LIVE: popRegisterPickupCookie
	// retracted the one the request presented as it read it, and only
	// PickupRegister's seal-failure exit re-issues it. Drive the retry from
	// exactly the value this response leaves the jar holding, not the one the
	// test minted at registration — that is the whole point of the fix.
	retryPickup := assertPickupCookieKeptForRetry(t, refused)

	row := loadRegisterPickupTokenRowForUser(t, database, user.ID)
	if row.ConsumedAt != nil {
		t.Fatal("the pickup token must still be unconsumed after a rolled-back redemption")
	}

	// Nothing was spent: the pickup cookie the jar now holds redeems once
	// issuance works again.
	armed.Store(false)
	accepted := pickupRequestWith(retryPickup)
	defer func() { _ = accepted.Body.Close() }()

	if location := accepted.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected the retried pickup to succeed, got redirect to %q (status %d)", location, accepted.StatusCode)
	}
	if cookie := responseCookieValue(accepted.Cookies(), authCookieName); cookie == "" {
		t.Fatal("expected an auth cookie once the retried pickup succeeds")
	}
	if cookie := responseCookieValue(accepted.Cookies(), recoveryCodeCookieName); cookie == "" {
		t.Fatal("expected a recovery-code cookie once the retried pickup succeeds")
	}

	row = loadRegisterPickupTokenRowForUser(t, database, user.ID)
	if row.ConsumedAt == nil {
		t.Fatal("the retried pickup must consume the token")
	}
}

// TestRegisterPickupRecoveryCodeRevealSealFailureLeavesTheTokenRedeemable is
// TestRegisterPickupSessionMintFailureLeavesTheTokenRedeemable's twin for the
// OTHER post-Peek seal: sealRecoveryCodeIssuanceCookie failing after the auth
// cookie already sealed successfully. Before this fix that branch also fell
// through redirectToPostRegisterSignin, so an owner hitting it lost the
// pickup cookie carrying her only recovery code even though the DB row (and
// now, the sealed auth cookie the code sat beside) was never written either.
func TestRegisterPickupRecoveryCodeRevealSealFailureLeavesTheTokenRedeemable(t *testing.T) {
	t.Parallel()

	armed, fault := armableRecoveryCodeIssuanceFault()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{recoveryCodeIssuanceFault: fault})
	email := "web64-pickup-reveal-seal-order@example.com"

	registerResponse := mustAppResponse(t, app, registerRequest(email))
	if registerResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected registration to redirect, got %d", registerResponse.StatusCode)
	}
	pickup := responseCookieValue(registerResponse.Cookies(), registerPickupCookieName)
	if pickup == "" {
		t.Fatalf("expected pickup cookie after register")
	}

	var user models.User
	if err := database.Where("email = ?", email).First(&user).Error; err != nil {
		t.Fatalf("load registered user: %v", err)
	}

	pickupRequestWith := func(cookieValue string) *http.Response {
		request := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", registerPickupCookieName+"="+cookieValue)
		return mustAppResponse(t, app, request)
	}

	armed.Store(true)
	refused := pickupRequestWith(pickup)
	defer func() { _ = refused.Body.Close() }()

	if location := refused.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected the failed pickup to redirect to /login, got %q", location)
	}
	if cookie := responseCookieValue(refused.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("a pickup whose reveal sealing failed must not set an auth cookie; got %q", cookie)
	}
	if cookie := responseCookieValue(refused.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("a pickup whose reveal sealing failed must not set a recovery-code cookie; got %q", cookie)
	}

	retryPickup := assertPickupCookieKeptForRetry(t, refused)

	row := loadRegisterPickupTokenRowForUser(t, database, user.ID)
	if row.ConsumedAt != nil {
		t.Fatal("the pickup token must still be unconsumed after a reveal sealing failure")
	}

	armed.Store(false)
	accepted := pickupRequestWith(retryPickup)
	defer func() { _ = accepted.Body.Close() }()

	if location := accepted.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected the retried pickup to succeed, got redirect to %q (status %d)", location, accepted.StatusCode)
	}
	if cookie := responseCookieValue(accepted.Cookies(), authCookieName); cookie == "" {
		t.Fatal("expected an auth cookie once the retried pickup succeeds")
	}
	if cookie := responseCookieValue(accepted.Cookies(), recoveryCodeCookieName); cookie == "" {
		t.Fatal("expected a recovery-code cookie once the retried pickup succeeds")
	}

	row = loadRegisterPickupTokenRowForUser(t, database, user.ID)
	if row.ConsumedAt == nil {
		t.Fatal("the retried pickup must consume the token")
	}
}

// registerPickupConsumeFaultStore wraps the real RegisterPickupTokenStore the
// composition root builds, delegating Issue and Peek unchanged (embedding),
// and overrides Consume to force one of the two lost-race outcomes
// PickupRegister's post-Peek Consume call can hit: a store error, or a
// concurrent redeem that already won (consumed=false). Both outcomes are hit
// AFTER the session and the recovery-code reveal are already sealed
// (WEB-64's Peek-then-seal-then-Consume order), so the two regressions below
// pin that a losing Consume here discards those sealed values instead of
// writing them.
type registerPickupConsumeFaultStore struct {
	apideps.RegisterPickupTokenStore
	consumeErr error
	lostRace   bool
}

func (s *registerPickupConsumeFaultStore) Consume(ctx context.Context, nonce string, now time.Time) (uint, bool, error) {
	if s.consumeErr != nil {
		return 0, false, s.consumeErr
	}
	if s.lostRace {
		return 0, false, nil
	}
	return s.RegisterPickupTokenStore.Consume(ctx, nonce, now)
}

// registerPickupWithConsumeFault registers a fresh user through the real
// pipeline (so the register_pickup_tokens row and the sealed pickup cookie
// are both genuine), then replays that cookie against GET /register/welcome
// under the faulted Consume store, returning the (unclosed) response.
func registerPickupWithConsumeFault(t *testing.T, store *registerPickupConsumeFaultStore, email string) *http.Response {
	t.Helper()

	testApp, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		registerPickupTokenStoreWrap: func(real apideps.RegisterPickupTokenStore) apideps.RegisterPickupTokenStore {
			store.RegisterPickupTokenStore = real
			return store
		},
		auditLogEnabled: true,
	})

	registerResponse := mustAppResponse(t, testApp, registerRequest(email))
	if registerResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected registration to redirect, got %d", registerResponse.StatusCode)
	}
	pickupValue := responseCookieValue(registerResponse.Cookies(), registerPickupCookieName)
	if pickupValue == "" {
		t.Fatalf("expected pickup cookie after register")
	}

	var registeredUser models.User
	if err := database.Where("email = ?", email).First(&registeredUser).Error; err != nil {
		t.Fatalf("load registered user: %v", err)
	}

	welcomeRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	welcomeRequest.Header.Set("Accept-Language", "en")
	welcomeRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickupValue)
	return mustAppResponse(t, testApp, welcomeRequest)
}

// TestRegisterPickupConsumeStoreErrorDiscardsSealedSessionAndReveal pins the
// "consume_failed" branch: Consume returning a store error after a
// successful Peek. The session and the recovery-code reveal are already
// sealed at that point (WEB-64), and this asserts they are discarded rather
// than written — the request must land exactly where a missing/tampered
// pickup does: /login, no auth cookie, no recovery-code cookie, the pickup
// cookie cleared (this is not one of the two seal failures that keep it
// live for retry), and the neutral flash.
func TestRegisterPickupConsumeStoreErrorDiscardsSealedSessionAndReveal(t *testing.T) {
	t.Parallel()

	store := &registerPickupConsumeFaultStore{consumeErr: errors.New("injected consume store failure")}
	refused := registerPickupWithConsumeFault(t, store, "web64-pickup-consume-error@example.com")
	defer func() { _ = refused.Body.Close() }()

	if location := refused.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected the failed pickup to redirect to /login, got %q", location)
	}
	if cookie := responseCookieValue(refused.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("a pickup whose Consume errored must not set an auth cookie; got %q", cookie)
	}
	if cookie := responseCookieValue(refused.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("a pickup whose Consume errored must not set a recovery-code cookie; got %q", cookie)
	}

	// consume_failed is not one of the two post-Peek seal failures
	// (redirectToPostRegisterSigninKeepingPickupCookie): the pickup cookie must
	// be actively CLEARED here, same as every other non-seal exit.
	cleared := responseCookie(refused.Cookies(), registerPickupCookieName)
	if cleared == nil {
		t.Fatal("expected a Set-Cookie clearing the pickup cookie on a Consume store error")
	}
	if strings.TrimSpace(cleared.Value) != "" {
		t.Fatalf("expected the pickup cookie cleared (empty value) on a Consume store error, got %q", cleared.Value)
	}

	flash := mustReadFlashPayload(t, []byte(testAppSecretKey), refused.Cookies())
	if flash.AuthError == "" {
		t.Fatal("expected the neutral flash AuthError on a Consume store error")
	}
}

// TestRegisterPickupConsumeLostRaceDiscardsSealedSessionAndReveal pins the
// "decoy_or_replay" branch reached from the Consume stage: Consume succeeding
// but reporting the grant was NOT spent (a concurrent redeem already won).
// Same assertions as the store-error twin above, plus the security-log
// reason, since the redirect_signin event already carries it (see
// TestRegisterPickupFailureLogsRedirectSigninReason in this package).
func TestRegisterPickupConsumeLostRaceDiscardsSealedSessionAndReveal(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var logged bytes.Buffer
	log.SetOutput(&logged)

	store := &registerPickupConsumeFaultStore{lostRace: true}
	refused := registerPickupWithConsumeFault(t, store, "web64-pickup-consume-lost-race@example.com")
	defer func() { _ = refused.Body.Close() }()

	if location := refused.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected the lost-race pickup to redirect to /login, got %q", location)
	}
	if cookie := responseCookieValue(refused.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("a pickup that lost the Consume race must not set an auth cookie; got %q", cookie)
	}
	if cookie := responseCookieValue(refused.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("a pickup that lost the Consume race must not set a recovery-code cookie; got %q", cookie)
	}

	cleared := responseCookie(refused.Cookies(), registerPickupCookieName)
	if cleared == nil {
		t.Fatal("expected a Set-Cookie clearing the pickup cookie on a lost Consume race")
	}
	if strings.TrimSpace(cleared.Value) != "" {
		t.Fatalf("expected the pickup cookie cleared (empty value) on a lost Consume race, got %q", cleared.Value)
	}

	flash := mustReadFlashPayload(t, []byte(testAppSecretKey), refused.Cookies())
	if flash.AuthError == "" {
		t.Fatal("expected the neutral flash AuthError on a lost Consume race")
	}

	output := logged.String()
	if !strings.Contains(output, `action="auth.register_pickup"`) || !strings.Contains(output, `outcome="redirect_signin"`) {
		t.Fatalf("expected a register_pickup redirect_signin security event, got %q", output)
	}
	if !strings.Contains(output, `reason="decoy_or_replay"`) {
		t.Fatalf("expected the redirect_signin event to carry the decoy_or_replay reason, got %q", output)
	}
}
