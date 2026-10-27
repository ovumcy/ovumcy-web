package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// unreadableAccountRepo is an auth repository whose by-id read refuses: the
// state a settings answer meets when the account row cannot be re-read after
// the write (or the refusal) that asked for the answer.
type unreadableAccountRepo struct{ stubLogoutAuthRepo }

func (unreadableAccountRepo) FindByID(context.Context, uint) (models.User, error) {
	return models.User{}, errors.New("account read refused")
}

func inPlaceAnswerProbe(t *testing.T, route func(handler *Handler, c fiber.Ctx, user *models.User) error) (*http.Response, string) {
	t.Helper()

	handler := &Handler{
		secretKey:   []byte("test-secret-key"),
		authService: services.NewAuthService(unreadableAccountRepo{}),
	}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		// A locale that carries copy for nothing the probes ask for: the request
		// language is already resolved, so no catalogue is consulted.
		c.Locals(contextLanguageKey, "en")
		c.Locals(contextMessagesKey, map[string]string{"unrelated.key": "unrelated"})
		return c.Next()
	})
	app.Post("/probe", func(c fiber.Ctx) error {
		return route(handler, c, &models.User{ID: 7, Role: models.RoleOwner})
	})

	request := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(""))
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept", "text/html")
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("probe request failed: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response, mustReadBodyString(t, response.Body)
}

// A committed 2FA change whose panel cannot be rebuilt answers the shared
// settings-load fault, and does not retarget the response at the panel: the
// client would otherwise swap an error into the panel's place as if it were
// the new state.
func TestRespondTOTPPanelAnswersTheLoadFaultWhenTheAccountCannotBeRead(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{totpPanelEnabled, totpPanelDisabled} {
		response, body := inPlaceAnswerProbe(t, func(handler *Handler, c fiber.Ctx, user *models.User) error {
			return handler.respondTOTPPanel(c, user, outcome)
		})
		if response.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("outcome %q: expected the load fault status 500, got %d: %s", outcome, response.StatusCode, body)
		}
		if retarget := response.Header.Get("HX-Retarget"); retarget != "" {
			t.Fatalf("outcome %q: a load fault must not be retargeted at the panel, got %q", outcome, retarget)
		}
		if strings.Contains(body, "data-settings-2fa") {
			t.Fatalf("outcome %q: no panel markup may accompany the fault, got %q", outcome, body)
		}
	}
}

// A refused recovery-code regeneration whose block cannot be rebuilt falls back
// to the shared settings-load fault instead of rendering the block from a
// stale or empty user.
func TestRespondRecoveryRefusalAnswersTheLoadFaultWhenTheAccountCannotBeRead(t *testing.T) {
	t.Parallel()

	response, body := inPlaceAnswerProbe(t, func(handler *Handler, c fiber.Ctx, user *models.User) error {
		return handler.respondRecoveryRefusal(c, user, settingsInvalidPasswordErrorSpec())
	})
	if response.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("expected the load fault status 500, got %d: %s", response.StatusCode, body)
	}
	if strings.Contains(body, "settings_recovery_code_section") || strings.Contains(body, "recovery-code") {
		t.Fatalf("no recovery block may accompany the fault, got %q", body)
	}
}

// The password-changed answer keeps a readable status when the active locale
// carries no copy for it: the owner still learns the change committed.
func TestRespondPasswordChangedFallsBackToEnglishWhenTheLocaleHasNoCopy(t *testing.T) {
	t.Parallel()

	response, body := inPlaceAnswerProbe(t, func(handler *Handler, c fiber.Ctx, _ *models.User) error {
		return handler.respondPasswordChanged(c)
	})
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.StatusCode, body)
	}
	if !strings.Contains(body, "Password changed successfully.") || !strings.Contains(body, "status-ok") {
		t.Fatalf("expected the English fallback in status-ok markup, got %q", body)
	}
}
