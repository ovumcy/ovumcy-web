package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// WEB-64: GET /register/welcome used to spend the single-use pickup token
// (marking register_pickup_tokens.consumed_at) and only then seal the auth
// cookie and the recovery-code reveal, so a sealing failure between the spend
// and the reveal cost the owner the code with no way to retry the pickup.
// The session and the reveal are now sealed BEFORE the token is consumed
// (PickupRegister's Peek-then-Consume order); this pins, with the handler's
// sessionIssuanceFault seam standing in for the crypto/codec failure no
// request can provoke, that the token row is still unconsumed and that
// neither cookie went out, and that the very same pickup cookie redeems once
// issuance works again.

func loadRegisterPickupTokenRowForUser(t *testing.T, database *gorm.DB, userID uint) models.RegisterPickupToken {
	t.Helper()
	var row models.RegisterPickupToken
	if err := database.Where("user_id = ?", userID).First(&row).Error; err != nil {
		t.Fatalf("load register_pickup_tokens row for user %d: %v", userID, err)
	}
	return row
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

	pickupRequest := func() *http.Response {
		request := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", registerPickupCookieName+"="+pickup)
		return mustAppResponse(t, app, request)
	}

	armed.Store(true)
	refused := pickupRequest()
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

	row := loadRegisterPickupTokenRowForUser(t, database, user.ID)
	if row.ConsumedAt != nil {
		t.Fatal("the pickup token must still be unconsumed after a rolled-back redemption")
	}

	// Nothing was spent: the very same pickup cookie redeems once issuance
	// works again.
	armed.Store(false)
	accepted := pickupRequest()
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
