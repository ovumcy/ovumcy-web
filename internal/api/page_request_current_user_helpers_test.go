package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestCurrentUserOrRedirectToLoginRedirectsWhenMissing(t *testing.T) {
	t.Parallel()

	handler := &Handler{}
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		user, handled, err := handler.currentUserOrRedirectToLogin(c)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
		return c.SendString(user.Email)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("app test failed: %v", err)
	}
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if response.Header.Get("Location") != "/login" {
		t.Fatalf("expected redirect to /login, got %q", response.Header.Get("Location"))
	}
}

func TestCurrentUserOrRedirectToLoginReturnsUserWhenPresent(t *testing.T) {
	t.Parallel()

	handler := &Handler{}
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		c.Locals(contextUserKey, &models.User{Email: "user@example.com"})
		user, handled, err := handler.currentUserOrRedirectToLogin(c)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
		return c.SendString(user.Email)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("app test failed: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
}

func TestCurrentUserOrUnauthorizedWhenMissing(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		_, _, err := currentUserOrUnauthorized(c)
		return err
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("app test failed: %v", err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.StatusCode)
	}
}
