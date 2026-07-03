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
		defer resp.Body.Close()

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
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", resp.StatusCode)
		}
	})
}
