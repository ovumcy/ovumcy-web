package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestRecoveryCodeCookieIsNotPlaintextJSON(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	_, recoveryCookie := registerAndExtractRecoveryCookies(
		t,
		app,
		"recovery-cookie-encoding@example.com",
		"StrongPass1",
	)
	if recoveryCookie == "" {
		t.Fatal("expected recovery cookie in register response")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(recoveryCookie)
	if err == nil {
		payload := recoveryCodePagePayload{}
		if json.Unmarshal(decoded, &payload) == nil {
			t.Fatalf("expected recovery cookie to be sealed; got plaintext payload: %#v", payload)
		}
	}

	if strings.Contains(recoveryCookie, "OVUM-") {
		t.Fatalf("expected recovery cookie value not to expose plaintext recovery code")
	}
}

func TestRecoveryCodeCookieRoundTripPreservesPayload(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setRecoveryCodeIssuanceCookie(c, 99, "OVUM-TESTCODE-9999", "/settings", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("seal recovery cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readRecoveryCodeDisplayState(c, 99, "/dashboard")
		if state.RecoveryCode != "OVUM-TESTCODE-9999" {
			t.Fatalf("expected recovery code to round-trip, got %q", state.RecoveryCode)
		}
		if state.ContinuePath != "/settings" {
			t.Fatalf("expected continue path /settings, got %q", state.ContinuePath)
		}
		if state.ContinueTarget != recoveryCodeContinueTargetSettings {
			t.Fatalf("expected settings continue target, got %q", state.ContinueTarget)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer sealResponse.Body.Close()

	cookieValue := responseCookieValue(sealResponse.Cookies(), recoveryCodeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed recovery cookie in response")
	}

	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", recoveryCodeCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer openResponse.Body.Close()
}

func TestRecoveryCodeCookieRejectsTamperedByte(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setRecoveryCodeIssuanceCookie(c, 99, "OVUM-TAMPER-CODE0", "/settings", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		state := handler.readRecoveryCodeDisplayState(c, 99, "/dashboard")
		if state.RecoveryCode != "" {
			t.Fatalf("expected tampered recovery cookie to yield empty code, got %q", state.RecoveryCode)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer sealResponse.Body.Close()

	cookieValue := responseCookieValue(sealResponse.Cookies(), recoveryCodeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed recovery cookie in response")
	}

	tampered := flipLastBaseEncodedByte(t, cookieValue)
	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", recoveryCodeCookieName+"="+tampered)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open tampered request: %v", err)
	}
	defer openResponse.Body.Close()
}

func TestRecoveryCodeCookieRejectsForeignKey(t *testing.T) {
	t.Parallel()

	sealingHandler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}
	openingHandler := &Handler{
		secretKey:    []byte("ffffffffffffffffffffffffffffffff"),
		cookieSecure: true,
	}

	sealingApp := fiber.New()
	sealingApp.Get("/seal", func(c fiber.Ctx) error {
		if err := sealingHandler.setRecoveryCodeIssuanceCookie(c, 99, "OVUM-FOREIGN-CODE", "/settings", recoveryCodeSurfaceDedicated); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	openingApp := fiber.New()
	openingApp.Get("/open", func(c fiber.Ctx) error {
		state := openingHandler.readRecoveryCodeDisplayState(c, 99, "/dashboard")
		if state.RecoveryCode != "" {
			t.Fatalf("expected rotated-key handler to reject sealed recovery cookie, got %q", state.RecoveryCode)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := sealingApp.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer sealResponse.Body.Close()

	cookieValue := responseCookieValue(sealResponse.Cookies(), recoveryCodeCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed recovery cookie in response")
	}

	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", recoveryCodeCookieName+"="+cookieValue)
	openResponse, err := openingApp.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer openResponse.Body.Close()
}
