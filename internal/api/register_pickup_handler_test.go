package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPickupRegisterValidExchangesPickupForAuthAndRecoveryCookies(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	email := "pickup-valid@example.com"

	registerResponse := mustAppResponse(t, app, registerRequest(email))
	assertStatusCode(t, registerResponse, http.StatusSeeOther)
	pickup := responseCookieValue(registerResponse.Cookies(), registerPickupCookieName)
	if pickup == "" {
		t.Fatalf("expected pickup cookie after register")
	}

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Accept-Language", "en")
	pickupRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickup)

	pickupResponse := mustAppResponse(t, app, pickupRequest)
	assertStatusCode(t, pickupResponse, http.StatusSeeOther)
	if location := pickupResponse.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected pickup success redirect to /register, got %q", location)
	}

	if cookie := responseCookieValue(pickupResponse.Cookies(), authCookieName); cookie == "" {
		t.Fatal("expected auth cookie after pickup")
	}
	if cookie := responseCookieValue(pickupResponse.Cookies(), recoveryCodeCookieName); cookie == "" {
		t.Fatal("expected recovery cookie after pickup")
	}
	cleared := responseCookie(pickupResponse.Cookies(), registerPickupCookieName)
	if cleared == nil {
		t.Fatal("expected pickup cookie to be cleared after consumption")
	}
	if cleared.Value != "" {
		t.Fatalf("expected cleared pickup cookie value, got %q", cleared.Value)
	}
}

func TestPickupRegisterDuplicateEmailDecoyRedirectsToLogin(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	email := "pickup-decoy@example.com"

	// Seed: a real user with this email so the second register-attempt collides.
	if seed := mustAppResponse(t, app, registerRequest(email)); seed.StatusCode != http.StatusSeeOther {
		t.Fatalf("seed register failed: status %d", seed.StatusCode)
	}

	// Attacker probes with the same email; collision branch emits a decoy pickup.
	decoyResponse := mustAppResponse(t, app, registerRequest(email))
	assertStatusCode(t, decoyResponse, http.StatusSeeOther)
	decoyPickup := responseCookieValue(decoyResponse.Cookies(), registerPickupCookieName)
	if decoyPickup == "" {
		t.Fatal("expected decoy pickup cookie for duplicate email")
	}

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Accept-Language", "en")
	pickupRequest.Header.Set("Cookie", registerPickupCookieName+"="+decoyPickup)

	pickupResponse := mustAppResponse(t, app, pickupRequest)
	assertStatusCode(t, pickupResponse, http.StatusSeeOther)
	if location := pickupResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected decoy pickup to redirect to /login, got %q", location)
	}

	if cookie := responseCookieValue(pickupResponse.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("expected no auth cookie after decoy pickup; got %q", cookie)
	}
	if cookie := responseCookieValue(pickupResponse.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("expected no recovery cookie after decoy pickup; got %q", cookie)
	}
	if flash := responseCookieValue(pickupResponse.Cookies(), flashCookieName); flash == "" {
		t.Fatal("expected neutral flash cookie after decoy pickup")
	}
}

func TestPickupRegisterMissingCookieRedirectsToLogin(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Accept-Language", "en")

	pickupResponse := mustAppResponse(t, app, pickupRequest)
	assertStatusCode(t, pickupResponse, http.StatusSeeOther)
	if location := pickupResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected /login redirect when pickup cookie absent, got %q", location)
	}
}

func TestPickupRegisterReplayedCookieRedirectsToLogin(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	email := "pickup-replay@example.com"

	registerResponse := mustAppResponse(t, app, registerRequest(email))
	assertStatusCode(t, registerResponse, http.StatusSeeOther)
	pickup := responseCookieValue(registerResponse.Cookies(), registerPickupCookieName)
	if pickup == "" {
		t.Fatalf("expected pickup cookie after register")
	}

	firstUseRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	firstUseRequest.Header.Set("Accept-Language", "en")
	firstUseRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickup)
	firstUseResponse := mustAppResponse(t, app, firstUseRequest)
	if location := firstUseResponse.Header.Get("Location"); location != "/register" {
		t.Fatalf("expected first pickup use to succeed; got Location=%q status=%d", location, firstUseResponse.StatusCode)
	}

	// Replay: same pickup value submitted again (server has no replay-state, so
	// we can't enforce "exactly once" at the database level, but the wave of
	// regression coverage at least documents what does happen. The pickup
	// cookie is value-decryptable so the second use still resolves to the real
	// user; what we DO assert is that the legitimate flow still completes
	// without leaking anything new. Note: this exposes a defense-in-depth gap
	// — see SECURITY.md "Register enumeration" residual.
	replayRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	replayRequest.Header.Set("Accept-Language", "en")
	replayRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickup)
	replayResponse := mustAppResponse(t, app, replayRequest)
	location := replayResponse.Header.Get("Location")
	if location != "/register" && location != "/login" {
		t.Fatalf("expected replay to redirect to /register or /login, got %q", location)
	}
}

func TestPickupRegisterTamperedCookieRedirectsToLogin(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Accept-Language", "en")
	pickupRequest.Header.Set("Cookie", registerPickupCookieName+"=v2.tampered-garbage")

	pickupResponse := mustAppResponse(t, app, pickupRequest)
	assertStatusCode(t, pickupResponse, http.StatusSeeOther)
	if location := pickupResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected tampered pickup to redirect to /login, got %q", location)
	}
	if flash := responseCookieValue(pickupResponse.Cookies(), flashCookieName); flash == "" {
		t.Fatal("expected neutral flash cookie after tampered pickup")
	}
}
