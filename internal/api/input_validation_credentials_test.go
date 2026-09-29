package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestParseCredentialsValidation(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Post("/credentials", func(c fiber.Ctx) error {
		credentials, err := parseCredentials(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(credentials)
	})

	t.Run("valid form input", func(t *testing.T) {
		form := url.Values{}
		form.Set("email", "USER@EXAMPLE.COM")
		form.Set("password", "StrongPass1")
		form.Set("confirm_password", "StrongPass1")
		form.Set("remember_me", "1")

		req := httptest.NewRequest(http.MethodPost, "/credentials", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := app.Test(req, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}

		var payload credentialsInput
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if payload.Email != "user@example.com" {
			t.Fatalf("expected normalized email, got %q", payload.Email)
		}
		if !payload.RememberMe {
			t.Fatal("expected remember_me=true from form value")
		}
	})

	// Query pollution: a member planted in the URL is not a submission. Each row
	// sends the body named by `body` with `query` appended to the URL.
	for _, tc := range []struct {
		name         string
		contentType  string
		body         string
		query        string
		wantStatus   int
		wantRemember bool
	}{
		{
			name: "json remember_me false is not overridden by the query", contentType: "application/json",
			body: `{"email":"user@example.com","password":"StrongPass1","remember_me":false}`, query: "remember_me=1",
			wantStatus: http.StatusOK, wantRemember: false,
		},
		{
			name: "json without remember_me ignores the query", contentType: "application/json",
			body: `{"email":"user@example.com","password":"StrongPass1"}`, query: "remember_me=true",
			wantStatus: http.StatusOK, wantRemember: false,
		},
		{
			name: "form without remember_me ignores the query", contentType: "application/x-www-form-urlencoded",
			body: url.Values{"email": {"user@example.com"}, "password": {"StrongPass1"}}.Encode(), query: "remember_me=1",
			wantStatus: http.StatusOK, wantRemember: false,
		},
		{
			name: "form remember_me in the body still counts", contentType: "application/x-www-form-urlencoded",
			body: url.Values{"email": {"user@example.com"}, "password": {"StrongPass1"}, "remember_me": {"on"}}.Encode(), query: "remember_me=0",
			wantStatus: http.StatusOK, wantRemember: true,
		},
		{
			name: "email and password only in the query are refused", contentType: "application/x-www-form-urlencoded",
			body: "", query: "email=user%40example.com&password=StrongPass1",
			wantStatus: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/credentials?"+tc.query, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)

			resp, err := app.Test(req, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, resp.StatusCode)
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var payload credentialsInput
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.RememberMe != tc.wantRemember {
				t.Fatalf("RememberMe = %t, want %t", payload.RememberMe, tc.wantRemember)
			}
		})
	}

	t.Run("invalid email is rejected", func(t *testing.T) {
		form := url.Values{}
		form.Set("email", "not-email")
		form.Set("password", "StrongPass1")

		req := httptest.NewRequest(http.MethodPost, "/credentials", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := app.Test(req, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", resp.StatusCode)
		}
	})
}
