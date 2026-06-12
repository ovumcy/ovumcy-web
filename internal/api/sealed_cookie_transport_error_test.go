package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Error tails of the sealed-cookie transport: a handler with an empty
// secret cannot build the AEAD codec, so writing, opening, and the auth
// set path must fail closed instead of emitting an unsealed cookie.

func TestSealedCookieTransportFailsClosedWithoutSecret(t *testing.T) {
	broken := &Handler{}

	app := fiber.New()
	app.Get("/write", func(c *fiber.Ctx) error {
		if err := broken.writeSealedCookie(c, flashCookieSpec, []byte("payload"), time.Now().Add(time.Minute)); err == nil {
			t.Error("writeSealedCookie with an empty secret must fail")
		}
		if _, err := broken.openCookieValue(flashCookieName, "sealed"); err == nil {
			t.Error("openCookieValue with an empty secret must fail")
		}
		if _, err := broken.setAuthCookie(c, &models.User{ID: 1, Role: models.RoleOwner}, false); err == nil {
			t.Error("setAuthCookie with an empty secret must fail")
		}
		return c.SendStatus(http.StatusNoContent)
	})

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/write", nil), -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	for _, cookie := range response.Cookies() {
		if cookie.Name == flashCookieName || cookie.Name == authCookieName {
			if cookie.Value != "" {
				t.Fatalf("no sealed cookie may be written when sealing fails, got %s=%q", cookie.Name, cookie.Value)
			}
		}
	}
}

// setFlashCookie with an empty payload must clear the flash cookie rather
// than write an empty sealed value.
func TestSetFlashCookieClearsOnEmptyPayload(t *testing.T) {
	handler := &Handler{secretKey: []byte("test-secret-key")}

	app := fiber.New()
	app.Get("/flash", func(c *fiber.Ctx) error {
		handler.setFlashCookie(c, FlashPayload{AuthError: "   "})
		return c.SendStatus(http.StatusNoContent)
	})

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/flash", nil), -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	cookie := responseCookie(response.Cookies(), flashCookieName)
	if cookie == nil {
		t.Fatal("expected a clearing Set-Cookie for the flash cookie")
	}
	if cookie.Value != "" {
		t.Fatalf("expected the flash cookie cleared, got value %q", cookie.Value)
	}
	if !cookie.Expires.Before(time.Now()) {
		t.Fatalf("expected an already-expired clearing cookie, got %s", cookie.Expires)
	}
}

// htmxSettingsSuccessMarkup must fall back to the supplied default message
// when the status key has no translation in the request's messages.
func TestHTMXSettingsSuccessMarkupFallsBackWithoutTranslation(t *testing.T) {
	app := fiber.New()
	app.Get("/markup", func(c *fiber.Ctx) error {
		return c.SendString(htmxSettingsSuccessMarkup(c, "tracking_updated", "Fallback message."))
	})

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/markup", nil), -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	body := mustReadBodyString(t, response.Body)
	if !strings.Contains(body, "Fallback message.") {
		t.Fatalf("expected the default message in the markup, got %q", body)
	}
	if !strings.Contains(body, "status-ok") {
		t.Fatalf("expected dismissible status-ok markup, got %q", body)
	}
}
