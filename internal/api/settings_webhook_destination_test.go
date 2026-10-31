package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestSettingsPageRendersNoPartOfTheStoredEndpointPath tests the invariant on
// the page, not the projection: the settings page shows at most the host of a
// stored endpoint. A Telegram bot URL carries its token in the first path
// segment, which an earlier masking rule rendered in clear.
func TestSettingsPageRendersNoPartOfTheStoredEndpointPath(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "egress-destination-telegram@example.com")
	seedEgressRow(t, owner.database, owner.user.ID, egressRenderRow{
		webhookURLPlaintext: "https://api.telegram.org/bot123456789:AAFixtureTokenValue0000/sendMessage?chat_id=42",
		webhookEnabled:      true,
		notifyPeriod:        true,
	})

	body := fetchPageBody(t, owner.app, "/settings", owner.authCookie)
	if !strings.Contains(body, "api.telegram.org") {
		t.Fatal("the settings page no longer names the stored host")
	}
	for _, leak := range []string{"123456789", "AAFixtureTokenValue0000", "sendMessage", "chat_id", "api.telegram.org/"} {
		if strings.Contains(body, leak) {
			t.Fatalf("the settings page rendered %q from the stored endpoint", leak)
		}
	}
}

// TestWebhookSaveWithAWhitespaceURLKeepsTheStoredEndpoint: a field holding only
// whitespace is the blank "keep" field. It used to count as a provided URL, so
// a save with delivery off stored no endpoint at all.
func TestWebhookSaveWithAWhitespaceURLKeepsTheStoredEndpoint(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			owner := newSettingsSecurityTestContext(t, fmt.Sprintf("egress-blank-url-%t@example.com", enabled))
			seedEgressRow(t, owner.database, owner.user.ID, egressRenderRow{
				webhookURLPlaintext: "https://ntfy.example.test/topic",
				webhookEnabled:      true,
				notifyPeriod:        true,
			})

			form := url.Values{"webhook_url": {"   \t "}, "webhook_notify_period": {"true"}}
			if enabled {
				form.Set("webhook_enabled", "true")
			}
			response := postWebhookSaveForm(t, owner, form.Encode())
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("expected the save to succeed, got %d", response.StatusCode)
			}

			after := reloadEgressUser(t, owner.database, owner.user.ID)
			if after.WebhookURL == "" {
				t.Fatal("a whitespace-only URL field cleared the stored endpoint")
			}
			if after.WebhookEnabled != enabled {
				t.Fatalf("enabled=%t, want %t", after.WebhookEnabled, enabled)
			}
		})
	}
}

func TestMapSettingsWebhookSaveErrorAnswersALostPinWithAConflict(t *testing.T) {
	spec := mapSettingsWebhookSaveError(fmt.Errorf("save: %w", services.ErrWebhookConfigVersionChanged))
	if spec.Status != fiber.StatusConflict {
		t.Fatalf("expected 409, got %d", spec.Status)
	}
	if key := services.AuthErrorTranslationKey(spec.Key); key != "settings.error.webhook_save_conflict" {
		t.Fatalf("expected the conflict to carry its own message key, got %q", key)
	}
}

func postWebhookSaveForm(t *testing.T, owner settingsSecurityTestContext, encoded string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/webhook", strings.NewReader(encoded))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", owner.authCookie)
	if owner.csrfCookie != nil {
		request.AddCookie(owner.csrfCookie)
	}
	request.Header.Set("X-CSRF-Token", owner.csrfToken)
	response, err := owner.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("webhook save request failed: %v", err)
	}
	return response
}
