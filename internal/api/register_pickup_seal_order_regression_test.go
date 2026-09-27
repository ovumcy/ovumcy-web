package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
