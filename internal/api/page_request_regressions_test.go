package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestOptionalAuthenticatedUserWithoutCookieReturnsNil(t *testing.T) {
	t.Parallel()

	handler := &Handler{secretKey: []byte("secret")}
	app := fiber.New()

	app.Get("/", func(c fiber.Ctx) error {
		user := handler.optionalAuthenticatedUser(c)
		return c.JSON(fiber.Map{"has_user": user != nil})
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("app test failed: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}
	if payload["has_user"] != false {
		t.Fatalf("expected has_user=false, got %#v", payload["has_user"])
	}
}

func TestRedirectAuthenticatedUserIfPresentRedirectsAuthenticatedRequest(t *testing.T) {
	t.Parallel()

	handler, database := newDataAccessTestHandler(t)
	handler.secretKey = []byte("test-secret")
	user := createDataAccessTestUser(t, database, "redirect-helper@example.com")

	token, _, err := handler.buildTokenWithSessionID(&user, time.Hour)
	if err != nil {
		t.Fatalf("buildToken returned error: %v", err)
	}
	sealedToken, err := handler.encodeAuthCookieToken(token)
	if err != nil {
		t.Fatalf("encodeAuthCookieToken returned error: %v", err)
	}

	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		redirected, redirectErr := handler.redirectAuthenticatedUserIfPresent(c)
		if redirectErr != nil {
			return redirectErr
		}
		if redirected {
			return nil
		}
		return c.SendStatus(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: authCookieName, Value: sealedToken})

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("app test failed: %v", err)
	}
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if response.Header.Get("Location") != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", response.Header.Get("Location"))
	}
}
