package api

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func htmxDismissibleSuccessStatusMarkup(messages map[string]string, message string) string {
	return httpx.DismissibleStatusOKMarkup(message, localizedStatusDismissLabel(messages))
}

// htmxSettingsSuccessMarkup resolves a settings success status into the
// localized dismissible status markup, falling back to defaultMessage when
// the status has no translation. The missing-translation policy for
// settings HTMX success responses lives here once; handlers must not
// re-implement the translate-and-fallback dance inline.
func htmxSettingsSuccessMarkup(c *fiber.Ctx, status string, defaultMessage string) string {
	messages := currentMessages(c)
	messageKey := services.SettingsStatusTranslationKey(status)
	message := translateMessage(messages, messageKey)
	if message == "" || message == messageKey {
		message = defaultMessage
	}
	return htmxDismissibleSuccessStatusMarkup(messages, message)
}

func localizedStatusDismissLabel(messages map[string]string) string {
	closeLabel := translateMessage(messages, "common.close")
	if closeLabel == "" || closeLabel == "common.close" {
		return "Close"
	}
	return closeLabel
}

func setEncodedResponseNotice(c *fiber.Ctx, message string) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return
	}
	c.Set("X-Ovumcy-Notice", url.QueryEscape(trimmed))
}

func (handler *Handler) sendDaySaveStatus(c *fiber.Ctx, messageKey string) error {
	timestamp := time.Now().In(handler.requestLocation(c)).Format("15:04")
	patternKey := messageKey
	if patternKey == "" {
		patternKey = "common.saved_at"
	}
	pattern := translateMessage(currentMessages(c), patternKey)
	if pattern == "" || pattern == patternKey {
		if patternKey == "common.saved_at" {
			pattern = "Saved at %s"
		} else {
			pattern = "Saved."
		}
	}
	message := pattern
	if patternKey == "common.saved_at" {
		message = fmt.Sprintf(pattern, timestamp)
	}
	return c.SendString(htmxDismissibleSuccessStatusMarkup(currentMessages(c), message))
}
