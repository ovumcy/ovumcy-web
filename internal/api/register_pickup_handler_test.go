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
	// This request models the browser's own top-level navigation back to
	// /register/welcome, following the redirect POST /api/v1/users just sent
	// it — a stated same-origin Sec-Fetch-Site, which is the one case
	// setFlashCookieForRequestOrigin (flash.go, WEB-40 round 3) sends to the
	// page slot rather than the CSRF-exempt one. Without this header the
	// request is indistinguishable from one with no first-party proof at all,
	// and PickupRegister's redirectToPostRegisterSignin now writes the exempt
	// slot for that shape instead (TestRegisterPickupMissingWithMissingFetchMetadataDoesNotClobberAPendingPageFlash).
	sameOriginNavigation.applyTo(pickupRequest)

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

	// Replay: same pickup value submitted again must be rejected. The nonce
	// inside the sealed cookie maps to a server-side register_pickup_tokens
	// row that the first /register/welcome call atomically marked consumed,
	// so the second consume returns "not found / already used" and we fall
	// through to the same neutral /login redirect as a stale or decoy
	// pickup. This is the runtime contract that closes Finding #3
	// (register-pickup replay window).
	replayRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	replayRequest.Header.Set("Accept-Language", "en")
	replayRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickup)
	replayResponse := mustAppResponse(t, app, replayRequest)
	if location := replayResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected replay to redirect to /login, got %q", location)
	}
	if cookie := responseCookieValue(replayResponse.Cookies(), authCookieName); cookie != "" {
		t.Fatalf("replay must not mint a second auth cookie; got %q", cookie)
	}
	if cookie := responseCookieValue(replayResponse.Cookies(), recoveryCodeCookieName); cookie != "" {
		t.Fatalf("replay must not mint a recovery-code cookie; got %q", cookie)
	}
}

func TestPickupRegisterTamperedCookieRedirectsToLogin(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Accept-Language", "en")
	pickupRequest.Header.Set("Cookie", registerPickupCookieName+"=v2.tampered-garbage")
	// Same reasoning as the decoy-email case above: a stated same-origin
	// Sec-Fetch-Site is what sends this refusal to the page slot rather than
	// the CSRF-exempt one (flash.go's setFlashCookieForRequestOrigin, WEB-40
	// round 3).
	sameOriginNavigation.applyTo(pickupRequest)

	pickupResponse := mustAppResponse(t, app, pickupRequest)
	assertStatusCode(t, pickupResponse, http.StatusSeeOther)
	if location := pickupResponse.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected tampered pickup to redirect to /login, got %q", location)
	}
	if flash := responseCookieValue(pickupResponse.Cookies(), flashCookieName); flash == "" {
		t.Fatal("expected neutral flash cookie after tampered pickup")
	}
}
