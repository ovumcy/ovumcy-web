package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The PATCH handler parses the values before the bases, so a malformed body is
// refused there first; the base reader's own refusal is held here directly, so
// a base read that swallowed a decode error would not pass as "no bases sent".
func TestParseDayPayloadBaseRefusesAMalformedJSONBody(t *testing.T) {
	t.Parallel()

	user := &models.User{TemperatureUnit: services.DefaultTemperatureUnit}
	app := fiber.New()
	app.Patch("/day", func(c fiber.Ctx) error {
		if _, _, err := parseDayPayloadBase(c, user, false, hiddenDayFields(user, false)); err != nil {
			return c.SendStatus(fiber.StatusBadRequest)
		}
		return c.SendStatus(fiber.StatusOK)
	})

	for _, body := range []string{`{"base_mood":`, `[1]`} {
		request := httptest.NewRequest(http.MethodPatch, "/day", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if status := mustAppResponse(t, app, request).StatusCode; status != fiber.StatusBadRequest {
			t.Fatalf("body %q: expected 400 from the base reader, got %d", body, status)
		}
	}
}
