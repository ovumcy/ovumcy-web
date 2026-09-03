package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestResetPasswordCookieFlagsFollowCookieSecureConfig(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		cookieSecure     bool
		expectedSecure   bool
		expectedSameSite http.SameSite
	}{
		{
			name:             "cookie_secure_disabled",
			cookieSecure:     false,
			expectedSecure:   false,
			expectedSameSite: http.SameSiteLaxMode,
		},
		{
			name:             "cookie_secure_enabled",
			cookieSecure:     true,
			expectedSecure:   true,
			expectedSameSite: http.SameSiteLaxMode,
		},
	}

	for _, tc := range testCases {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			app, database := newOnboardingTestAppWithCookieSecure(t, tc.cookieSecure)
			user := createOnboardingTestUser(t, database, "reset-cookie-flags-"+tc.name+"@example.com", "StrongPass1", true)
			recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)

			form := url.Values{
				"email":         {user.Email},
				"recovery_code": {recoveryCode},
				"password":      {"StrongPass1"},
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("forgot-password request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != http.StatusSeeOther {
				t.Fatalf("expected status 303, got %d", response.StatusCode)
			}

			resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
			if resetCookie == nil {
				t.Fatalf("expected reset-password cookie in response")
			}
			if !resetCookie.HttpOnly {
				t.Fatalf("expected reset-password cookie HttpOnly=true")
			}
			if resetCookie.Secure != tc.expectedSecure {
				t.Fatalf("expected reset-password cookie Secure=%t, got %t", tc.expectedSecure, resetCookie.Secure)
			}
			if resetCookie.SameSite != tc.expectedSameSite {
				t.Fatalf("expected reset-password cookie SameSite=%v, got %v", tc.expectedSameSite, resetCookie.SameSite)
			}
		})
	}
}

func TestResetPasswordCookieRoundTripPreservesPayload(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setResetPasswordCookie(c, "reset-token-xyz"); err != nil {
			t.Fatalf("seal reset password cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		token := handler.readResetPasswordCookie(c)
		if token != "reset-token-xyz" {
			t.Fatalf("expected reset token to round-trip, got %q", token)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), resetPasswordCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed reset password cookie in response")
	}

	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", resetPasswordCookieName+"="+cookieValue)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

func TestResetPasswordCookieRejectsTamperedByte(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		secretKey:    []byte("0123456789abcdef0123456789abcdef"),
		cookieSecure: true,
	}

	app := fiber.New()
	app.Get("/seal", func(c fiber.Ctx) error {
		if err := handler.setResetPasswordCookie(c, "reset-tamper"); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		token := handler.readResetPasswordCookie(c)
		if token != "" {
			t.Fatalf("expected tampered reset password cookie to yield empty token, got %q", token)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := app.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), resetPasswordCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed reset password cookie in response")
	}

	tampered := flipLastBaseEncodedByte(t, cookieValue)
	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", resetPasswordCookieName+"="+tampered)
	openResponse, err := app.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open tampered request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

func TestResetPasswordCookieRejectsForeignKey(t *testing.T) {
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
		if err := sealingHandler.setResetPasswordCookie(c, "reset-foreign"); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	openingApp := fiber.New()
	openingApp.Get("/open", func(c fiber.Ctx) error {
		token := openingHandler.readResetPasswordCookie(c)
		if token != "" {
			t.Fatalf("expected rotated-key handler to reject sealed cookie, got %q", token)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	sealResponse, err := sealingApp.Test(httptest.NewRequest("GET", "/seal", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seal request: %v", err)
	}
	defer func() { _ = sealResponse.Body.Close() }()

	cookieValue := responseCookieValue(sealResponse.Cookies(), resetPasswordCookieName)
	if cookieValue == "" {
		t.Fatal("expected sealed reset password cookie in response")
	}

	openRequest := httptest.NewRequest("GET", "/open", nil)
	openRequest.Header.Set("Cookie", resetPasswordCookieName+"="+cookieValue)
	openResponse, err := openingApp.Test(openRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = openResponse.Body.Close() }()
}

// TestResetPasswordCookieReaderClearsWhenCodecUnavailable drives
// readResetPasswordCookie's handler.cookieCodec() failure branch. A bare
// Handler with no secretKey never gets past it: newSecureCookieCodec refuses
// an empty key before any AEAD is built. NewHandler requires a non-empty
// SECRET_KEY at boot, so a fully composed app can never reach this in
// production — but the reader still has to fail closed rather than trust an
// uninitialized codec's zero value if it is ever invoked before that
// invariant holds, which is exactly what this drives directly.
func TestResetPasswordCookieReaderClearsWhenCodecUnavailable(t *testing.T) {
	t.Parallel()

	handler := &Handler{}

	app := fiber.New()
	app.Get("/open", func(c fiber.Ctx) error {
		token := handler.readResetPasswordCookie(c)
		if token != "" {
			t.Fatalf("expected no token when the cookie codec is unavailable, got %q", token)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	request := httptest.NewRequest("GET", "/open", nil)
	request.Header.Set("Cookie", resetPasswordCookieName+"=anything")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
}

// TestResetPasswordCookieReaderClearsOnNonJSONPlaintext drives
// readResetPasswordCookie's json.Unmarshal failure branch: a value that opens
// under the AEAD (so it was minted with the right key and not tampered) but
// whose plaintext is not the payload JSON at all. setResetPasswordCookie
// itself can never produce this — it always marshals resetPasswordCookiePayload
// — so this seals the malformed plaintext directly, the same technique the
// tamper/foreign-key tests above use to reach the reader's other failure arms.
func TestResetPasswordCookieReaderClearsOnNonJSONPlaintext(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	handler := &Handler{secretKey: secret, cookieSecure: true}

	codec, err := newSecureCookieCodec(secret)
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}
	sealed, err := codec.seal(resetPasswordCookieName, []byte("not-the-payload-json"))
	if err != nil {
		t.Fatalf("seal malformed plaintext: %v", err)
	}

	app := fiber.New()
	app.Get("/open", func(c fiber.Ctx) error {
		token := handler.readResetPasswordCookie(c)
		if token != "" {
			t.Fatalf("expected no token for a sealed value whose plaintext is not payload JSON, got %q", token)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	request := httptest.NewRequest("GET", "/open", nil)
	request.Header.Set("Cookie", resetPasswordCookieName+"="+sealed)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
}

// TestResetPasswordCookieReaderClearsOnBlankToken drives readResetPasswordCookie's
// blank-token branch: the payload decodes as valid JSON but its Token field is
// empty (or all whitespace). setResetPasswordCookie's own guard refuses to
// seal an empty token, so this reaches the branch the same way the tests
// above reach the reader's other arms — sealing the payload directly rather
// than through the setter.
func TestResetPasswordCookieReaderClearsOnBlankToken(t *testing.T) {
	t.Parallel()

	secret := []byte("0123456789abcdef0123456789abcdef")
	handler := &Handler{secretKey: secret, cookieSecure: true}

	cookieValue := mustSealResetCookieValueForTest(t, secret, "   ")

	app := fiber.New()
	app.Get("/open", func(c fiber.Ctx) error {
		token := handler.readResetPasswordCookie(c)
		if token != "" {
			t.Fatalf("expected no token for a payload whose Token field is blank, got %q", token)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	request := httptest.NewRequest("GET", "/open", nil)
	request.Header.Set("Cookie", resetPasswordCookieName+"="+cookieValue)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
}
