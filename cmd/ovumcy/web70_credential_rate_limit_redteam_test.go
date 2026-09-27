package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// WEB-70 red-on-base / gutted-mutant proof: the 2FA login challenge and the
// password-reset redeem each verify a credential and must sit under their own
// edge rate ceiling, not only under the general /api catch-all (300/min).
// These two tests drive the REAL app (newFiberApp) with the credential
// ceiling's env knobs turned down to one request, and assert that the SECOND
// request to each route is refused with the route's OWN stable error key —
// never silently allowed through by the /api catch-all, and never merged into
// a different route's budget.
func TestTOTPChallengeCarriesItsOwnCredentialRateLimit(t *testing.T) {
	minimalRuntimeEnv(t)
	t.Setenv("RATE_LIMIT_TOTP_CHALLENGE_MAX", "1")
	t.Setenv("RATE_LIMIT_TOTP_CHALLENGE_WINDOW", "1m")

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	handler, _ := newRateLimitTestHandlerAndDBAtLocation(t, time.UTC)
	app := newFiberApp(config, handler)

	send := func() *http.Response {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge", strings.NewReader("code=000000"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		response, sendErr := app.Test(request, testConfigNoTimeout)
		if sendErr != nil {
			t.Fatalf("POST /api/v1/sessions/2fa-challenge: %v", sendErr)
		}
		return response
	}

	first := send()
	_ = first.Body.Close()

	second := send()
	defer func() { _ = second.Body.Close() }()
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second request past a 1-request credential budget answered %d, want 429 — the 2FA challenge has no edge rate ceiling of its own", second.StatusCode)
	}
	payload := struct {
		Error string `json:"error"`
	}{}
	if decodeErr := json.NewDecoder(second.Body).Decode(&payload); decodeErr != nil {
		t.Fatalf("decode 429 body: %v", decodeErr)
	}
	if payload.Error != "too_many_totp_challenge_attempts" {
		t.Fatalf("429 error key = %q, want %q — a different key means a broader limiter (the /api catch-all) answered instead of the route's own", payload.Error, "too_many_totp_challenge_attempts")
	}
}

func TestPasswordResetRedeemCarriesItsOwnCredentialRateLimit(t *testing.T) {
	minimalRuntimeEnv(t)
	t.Setenv("RATE_LIMIT_PASSWORD_RESET_REDEEM_MAX", "1")
	t.Setenv("RATE_LIMIT_PASSWORD_RESET_REDEEM_WINDOW", "1m")

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	handler, _ := newRateLimitTestHandlerAndDBAtLocation(t, time.UTC)
	app := newFiberApp(config, handler)

	send := func() *http.Response {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader("password=Sup3rSecret9&confirm_password=Sup3rSecret9"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		response, sendErr := app.Test(request, testConfigNoTimeout)
		if sendErr != nil {
			t.Fatalf("POST /api/v1/password-resets/redeem: %v", sendErr)
		}
		return response
	}

	first := send()
	_ = first.Body.Close()

	second := send()
	defer func() { _ = second.Body.Close() }()
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second request past a 1-request credential budget answered %d, want 429 — the password-reset redeem has no edge rate ceiling of its own", second.StatusCode)
	}
	payload := struct {
		Error string `json:"error"`
	}{}
	if decodeErr := json.NewDecoder(second.Body).Decode(&payload); decodeErr != nil {
		t.Fatalf("decode 429 body: %v", decodeErr)
	}
	if payload.Error != "too_many_password_reset_redeem_attempts" {
		t.Fatalf("429 error key = %q, want %q — a different key means a broader limiter (the /api catch-all) answered instead of the route's own", payload.Error, "too_many_password_reset_redeem_attempts")
	}
}
