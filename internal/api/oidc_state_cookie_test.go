package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

func TestPopOIDCStateCookieRejectsExpiredPayload(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		t.Fatalf("newSecureCookieCodec() error: %v", err)
	}

	payload, err := json.Marshal(oidcAuthState{
		State:        "state-value",
		Nonce:        "nonce-value",
		CodeVerifier: "verifier-value",
		ExpiresAt:    time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("marshal state payload: %v", err)
	}
	sealed, err := codec.seal(oidcStateCookieName, payload)
	if err != nil {
		t.Fatalf("seal state payload: %v", err)
	}

	app := fiber.New()
	app.Get(security.OIDCCallbackPath, func(c fiber.Ctx) error {
		state := handler.popOIDCStateCookie(c)
		if state.State != "" || state.Nonce != "" || state.CodeVerifier != "" {
			t.Fatalf("expected expired OIDC state cookie to be rejected, got %+v", state)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	request := httptest.NewRequest("GET", security.OIDCCallbackPath, nil)
	request.Header.Set("Cookie", oidcStateCookieName+"="+sealed)
	response, testErr := app.Test(request, testConfigNoTimeout)
	if testErr != nil {
		t.Fatalf("request failed: %v", testErr)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}
}

func TestOIDCStateCookieRoundTripPreservesPayload(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	state, err := newOIDCAuthState(time.Now().UTC())
	if err != nil {
		t.Fatalf("new oidc auth state: %v", err)
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setOIDCStateCookie(c, state); err != nil {
			t.Fatalf("set oidc state cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get(security.OIDCCallbackPath, func(c fiber.Ctx) error {
		recovered := handler.popOIDCStateCookie(c)
		if recovered.State != state.State || recovered.Nonce != state.Nonce || recovered.CodeVerifier != state.CodeVerifier {
			t.Fatalf("expected oidc state to round-trip, got %+v", recovered)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), oidcStateCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed oidc state cookie in response")
	}

	openRequest := httptest.NewRequest("GET", security.OIDCCallbackPath, nil)
	openRequest.Header.Set("Cookie", oidcStateCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

func TestOIDCStateCookieRejectsForeignKey(t *testing.T) {
	t.Parallel()

	sealingHandler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	openingHandler := &Handler{
		secretKey:    []byte("ffffffffffffffffffffffffffffffff"),
		cookieSecure: true,
	}
	state, err := newOIDCAuthState(time.Now().UTC())
	if err != nil {
		t.Fatalf("new oidc auth state: %v", err)
	}

	sealingApp := fiber.New()
	sealingApp.Get("/seal", func(c fiber.Ctx) error {
		if err := sealingHandler.setOIDCStateCookie(c, state); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	openingApp := fiber.New()
	openingApp.Get(security.OIDCCallbackPath, func(c fiber.Ctx) error {
		recovered := openingHandler.popOIDCStateCookie(c)
		if recovered.State != "" || recovered.Nonce != "" || recovered.CodeVerifier != "" {
			t.Fatalf("expected rotated-key handler to reject sealed state cookie, got %+v", recovered)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := sealingApp.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), oidcStateCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed oidc state cookie in response")
	}

	openRequest := httptest.NewRequest("GET", security.OIDCCallbackPath, nil)
	openRequest.Header.Set("Cookie", oidcStateCookieName+"="+cookieValue)
	openResponse, err := openingApp.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

func TestOIDCStateCookieRejectsTamperedByte(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	state, err := newOIDCAuthState(time.Now().UTC())
	if err != nil {
		t.Fatalf("new oidc auth state: %v", err)
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setOIDCStateCookie(c, state); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get(security.OIDCCallbackPath, func(c fiber.Ctx) error {
		recovered := handler.popOIDCStateCookie(c)
		if recovered.State != "" || recovered.Nonce != "" || recovered.CodeVerifier != "" {
			t.Fatalf("expected tampered oidc state cookie to be rejected, got %+v", recovered)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), oidcStateCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed oidc state cookie in response")
	}

	tampered := flipLastBaseEncodedByte(t, cookieValue)
	openRequest := httptest.NewRequest("GET", security.OIDCCallbackPath, nil)
	openRequest.Header.Set("Cookie", oidcStateCookieName+"="+tampered)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open tampered request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}
