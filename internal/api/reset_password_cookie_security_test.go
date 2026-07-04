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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			app, database := newOnboardingTestAppWithCookieSecure(t, tc.cookieSecure)
			user := createOnboardingTestUser(t, database, "reset-cookie-flags-"+tc.name+"@example.com", "StrongPass1", true)
			recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)

			form := url.Values{
				"email":         {user.Email},
				"recovery_code": {recoveryCode},
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
		if err := handler.setResetPasswordCookie(c, "reset-token-xyz", true); err != nil {
			t.Fatalf("seal reset password cookie: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		token, forced := handler.readResetPasswordCookie(c)
		if token != "reset-token-xyz" {
			t.Fatalf("expected reset token to round-trip, got %q", token)
		}
		if !forced {
			t.Fatalf("expected forced flag to round-trip as true")
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
		if err := handler.setResetPasswordCookie(c, "reset-tamper", false); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/open", func(c fiber.Ctx) error {
		token, forced := handler.readResetPasswordCookie(c)
		if token != "" || forced {
			t.Fatalf("expected tampered reset password cookie to yield empty token, got %q forced=%t", token, forced)
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
		if err := sealingHandler.setResetPasswordCookie(c, "reset-foreign", true); err != nil {
			t.Fatalf("seal: %v", err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	openingApp := fiber.New()
	openingApp.Get("/open", func(c fiber.Ctx) error {
		token, forced := openingHandler.readResetPasswordCookie(c)
		if token != "" || forced {
			t.Fatalf("expected rotated-key handler to reject sealed cookie, got %q forced=%t", token, forced)
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
